package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/gogen"
	"github.com/simon-lentz/yammm/schema/markdown"
)

func newGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen <schema.yammm>",
		Short: "Generate source artifacts from a schema",
		Long: `Generate artifacts from a yammm schema.

Targets:

  --to go          A single file of typed Go structs for every schema type
                   (including imported types), named Enum/DataType types and
                   their value constants, a Date type when a position emits
                   Date, EDGE_ association structs, a Graph aggregate, and the
                   embedded schema source with its SchemaHash. Output is
                   stdlib-only. Use --initialisms to upper-case extra acronyms
                   (e.g. JWT,OIDC) in generated identifiers; they merge with
                   the default golint acronym set.

  --to md          A self-contained Markdown reference document: a Mermaid
                   class diagram of every type in the import closure, per-type
                   sections (property tables, relations, invariants), and
                   data-type tables. "markdown" is accepted as an alias. Use
                   --no-class-diagram to omit the diagram section, or
                   --no-class-members to keep the diagram and drop the
                   member lines inside each class.

Use --module-root to resolve module-style imports against a root directory other
than the schema's own (e.g. a repository root); for the go target the embedded
source keys are relative to that root.`,
		Args: cobra.ExactArgs(1),
		RunE: withDiagnostics(runGen),
	}
	cmd.Flags().String("to", "", "target: go or md (required)")
	cmd.Flags().String("package", "", "go target: generated package name (default: derived from schema name)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().StringSlice("initialisms", nil, "go target: extra acronyms to upper-case in generated names, e.g. JWT,OIDC")
	registerModuleRootFlag(cmd)
	cmd.Flags().Bool("no-class-diagram", false, "md target: omit the Mermaid class-diagram section")
	cmd.Flags().Bool("no-class-members", false, "md target: omit the member lines inside each diagram class")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// rejectInapplicableFlags fails with a usage error when a per-target flag was
// explicitly set for a different target, so a flag that would be silently
// ignored is surfaced instead. Shared flags (--output, --module-root) are not
// listed. Checked on Changed, not value, so an explicit empty value is
// rejected too.
func rejectInapplicableFlags(cmd *cobra.Command, target string) error {
	perTarget := []struct{ flag, target string }{
		{"package", "go"},
		{"initialisms", "go"},
		{"no-class-diagram", "md"},
		{"no-class-members", "md"},
	}
	for _, pf := range perTarget {
		if pf.target != target && cmd.Flags().Changed(pf.flag) {
			return cli.Usagef("flag --%s applies only to --to %s", pf.flag, pf.target)
		}
	}
	return nil
}

func runGen(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	toFormat, _ := cmd.Flags().GetString("to")
	outputPath, _ := cmd.Flags().GetString("output")

	target := strings.ToLower(toFormat)
	if target == "markdown" {
		target = "md"
	}
	if target != "go" && target != "md" {
		return cli.Usagef("unsupported gen target %q: must be go or md", toFormat)
	}
	if err := rejectInapplicableFlags(cmd, target); err != nil {
		return err
	}
	// A flag refused before any lookup is a usage error whatever the schema
	// holds, so --package is judged before the load, and when given empty too.
	if pkgName, _ := cmd.Flags().GetString("package"); cmd.Flags().Changed("package") {
		if err := gogen.CheckPackageName(pkgName); err != nil {
			return cli.Usagef("invalid --package %q: a Go package name is an identifier that is not a keyword and not \"_\"", pkgName)
		}
	}

	absSchemaPath, err := schemaOperand(args[0])
	if err != nil {
		return err
	}

	moduleRootAbs, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRootAbs, absSchemaPath, schemaResult); err != nil {
		return err
	}
	// Nothing downstream reports through diag, so the load's residual warnings
	// are all this command has and they precede the generated artifact.
	sink.Flush()

	var data []byte
	switch target {
	case "go":
		pkgName, _ := cmd.Flags().GetString("package")
		initialisms, _ := cmd.Flags().GetStringSlice("initialisms")
		var opts []gogen.Option
		if pkgName != "" {
			opts = append(opts, gogen.WithPackageName(pkgName))
		}
		if len(initialisms) > 0 {
			opts = append(opts, gogen.WithInitialisms(initialisms...))
		}
		data, err = gogen.Marshal(s, opts...)
	case "md":
		noDiagram, _ := cmd.Flags().GetBool("no-class-diagram")
		noMembers, _ := cmd.Flags().GetBool("no-class-members")
		data, err = markdown.Marshal(s,
			markdown.WithClassDiagram(!noDiagram),
			markdown.WithClassMembers(!noMembers))
	}
	if err != nil {
		return cli.Runtimef("generate %s: %v", target, err)
	}

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write output: %v", err)
	}
	return nil
}

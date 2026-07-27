package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/adapter/jschema"
	"github.com/simon-lentz/yammm/adapter/markdown"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen <schema.yammm>",
		Short: "Generate source artifacts from a schema",
		Long: `Generate artifacts from a yammm schema.

Targets:

  --to go          A single file of typed Go structs for every schema type
                   (including imported types), named Enum/DataType types, EDGE_
                   association structs, a Graph aggregate, and an embedded
                   SerializedModel. Output is stdlib-only. Use --initialisms to
                   upper-case extra acronyms (e.g. GUID,JWT) in generated
                   identifiers; they merge with the default golint acronym set.

  --to jsonschema  A JSON Schema draft 2020-12 document describing the
                   instance-data JSON accepted by 'yammm check': one key per
                   concrete type, each an array of instances. Wire it into an
                   editor (e.g. a yaml-language-server or JSON $schema header)
                   for completion and validation while authoring data files.
                   Use --schema-id to set the document's "$id" (omitted when
                   unset).

  --to md          A self-contained Markdown reference document: a Mermaid
                   class diagram of every type in the import closure, per-type
                   sections (property tables, relations, invariants), and
                   data-type tables. "markdown" is accepted as an alias. Use
                   --no-class-diagram to omit the diagram section.

Use --module-root to resolve module-style imports against a root directory other
than the schema's own (e.g. a repository root); for the go target the embedded
SerializedModel keys are relative to that root.`,
		Args: cobra.ExactArgs(1),
		RunE: runGen,
	}
	cmd.Flags().String("to", "", "target: go, jsonschema, or md (required)")
	cmd.Flags().String("package", "", "go target: generated package name (default: derived from schema name)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().StringSlice("initialisms", nil, "go target: extra acronyms to upper-case in generated names, e.g. GUID,JWT")
	cmd.Flags().String("module-root", "", "root directory for module-style imports (default: the schema's directory)")
	cmd.Flags().String("schema-id", "", `jsonschema target: value for the emitted "$id" (omitted when unset)`)
	cmd.Flags().Bool("no-class-diagram", false, "md target: omit the Mermaid class-diagram section")
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
		{"schema-id", "jsonschema"},
		{"no-class-diagram", "md"},
	}
	for _, pf := range perTarget {
		if pf.target != target && cmd.Flags().Changed(pf.flag) {
			fmt.Fprintf(os.Stderr, "error: flag --%s applies only to --to %s\n", pf.flag, pf.target)
			return &cli.ExitError{Code: cli.ExitUsage}
		}
	}
	return nil
}

func runGen(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	toFormat, _ := cmd.Flags().GetString("to")
	outputPath, _ := cmd.Flags().GetString("output")
	moduleRoot, _ := cmd.Flags().GetString("module-root")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	target := strings.ToLower(toFormat)
	if target == "markdown" {
		target = "md"
	}
	if target != "go" && target != "jsonschema" && target != "md" {
		fmt.Fprintf(os.Stderr, "error: unsupported gen target %q: must be go, jsonschema, or md\n", toFormat)
		return &cli.ExitError{Code: cli.ExitUsage}
	}
	if err := rejectInapplicableFlags(cmd, target); err != nil {
		return err
	}

	absSchemaPath, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", args[0], err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	var loadOpts []schema.LoadOption
	moduleRootAbs := ""
	if moduleRoot != "" {
		absRoot, err := filepath.Abs(moduleRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: resolve module root %q: %v\n", moduleRoot, err)
			return &cli.ExitError{Code: cli.ExitUsage}
		}
		loadOpts = append(loadOpts, schema.WithModuleRoot(absRoot))
		moduleRootAbs = absRoot
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	pending, failed := reportSchemaLoad(cmd, outputFormat, noColor, s, moduleRootAbs, absSchemaPath, schemaResult)
	if failed {
		return &cli.ExitError{Code: cli.ExitValidation}
	}
	// The load's residual warnings are this command's only diagnostics — nothing
	// downstream reports through diag — so they render here, once.
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, moduleRootAbs, absSchemaPath), pending)

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
	case "jsonschema":
		schemaID, _ := cmd.Flags().GetString("schema-id")
		var opts []jschema.Option
		if schemaID != "" {
			opts = append(opts, jschema.WithSchemaID(schemaID))
		}
		data, err = jschema.Marshal(s, opts...)
	case "md":
		noDiagram, _ := cmd.Flags().GetBool("no-class-diagram")
		var opts []markdown.Option
		if noDiagram {
			opts = append(opts, markdown.WithClassDiagram(false))
		}
		data, err = markdown.Marshal(s, opts...)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate %s: %v\n", target, err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	return nil
}

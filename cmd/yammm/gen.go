package main

import (
	"encoding/csv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/ident"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
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
                   the default golint acronym set, and each is one or more
                   letters.

  --to cypher      One Cypher script that provisions the schema in Neo4j:
                   every constraint, then every index, one statement per line,
                   each ended by ";", for cypher-shell or a driver session.
                   --edition, --node-keys and --type-constraints shape the
                   constraints; --prefix and --separator compose the labels,
                   and must match the graph's for neo4j diff to compare it.

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
	cmd.Flags().String("to", "", "target: go, cypher or md (required)")
	cmd.Flags().String("package", "", "go target: generated package name (default: derived from schema name)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().Var(&acronymsValue{}, "initialisms", "go target: extra acronyms to upper-case in generated names, e.g. JWT,OIDC")
	registerModuleRootFlag(cmd)
	cmd.Flags().Bool("no-class-diagram", false, "md target: omit the Mermaid class-diagram section")
	cmd.Flags().Bool("no-class-members", false, "md target: omit the member lines inside each diagram class")
	registerLabelFlags(cmd, "cypher target: ")
	registerConstraintFlags(cmd, "cypher target: ")
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
		{"separator", "cypher"},
		{"prefix", "cypher"},
		{"edition", "cypher"},
		{"node-keys", "cypher"},
		{"type-constraints", "cypher"},
	}
	for _, pf := range perTarget {
		if pf.target != target && cmd.Flags().Changed(pf.flag) {
			return cli.Usagef("flag --%s applies only to --to %s", pf.flag, pf.target)
		}
	}
	return nil
}

// acronymsValue is --initialisms' value, a [cobra.SliceValue] as StringSlice
// is, so completion offers the flag again. It reads an occurrence as
// StringSlice does, save that an empty one is one empty acronym for runGen to
// refuse, where StringSlice drops it.
type acronymsValue struct{ vals []string }

var _ cobra.SliceValue = (*acronymsValue)(nil)

// String renders no acronyms as "", the default pflag's help prints no
// "(default ...)" for.
func (a *acronymsValue) String() string {
	if len(a.vals) == 0 {
		return ""
	}
	return "[" + strings.Join(a.vals, ",") + "]"
}

func (a *acronymsValue) Type() string { return "strings" }

func (a *acronymsValue) Set(s string) error {
	if s == "" {
		a.vals = append(a.vals, "")
		return nil
	}
	fields, err := csv.NewReader(strings.NewReader(s)).Read()
	if err != nil {
		return err
	}
	a.vals = append(a.vals, fields...)
	return nil
}

func (a *acronymsValue) GetSlice() []string { return a.vals }

func runGen(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	toFormat, _ := cmd.Flags().GetString("to")
	outputPath, _ := cmd.Flags().GetString("output")

	target := strings.ToLower(toFormat)
	if target == "markdown" {
		target = "md"
	}
	if target != "go" && target != "md" && target != "cypher" {
		return cli.Usagef("unsupported gen target %q: must be go, cypher or md", toFormat)
	}
	if err := rejectInapplicableFlags(cmd, target); err != nil {
		return err
	}
	var cypherConfig cypher.Config
	if target == "cypher" {
		c, err := constraintConfig(cmd)
		if err != nil {
			return err
		}
		cypherConfig = c
	}
	// A flag refused before any lookup is a usage error whatever the schema
	// holds, so --package and --initialisms are judged before the load, and
	// when given empty too.
	if pkgName, _ := cmd.Flags().GetString("package"); cmd.Flags().Changed("package") {
		if err := gogen.CheckPackageName(pkgName); err != nil {
			return cli.Usagef("invalid --package %q: a Go package name is an identifier that is not a keyword and not \"_\"", pkgName)
		}
	}
	initialisms := cmd.Flags().Lookup("initialisms").Value.(cobra.SliceValue).GetSlice()
	for _, a := range initialisms {
		if !ident.IsInitialism(a) {
			return cli.Usagef("invalid --initialisms %q: an acronym is one or more letters", a)
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
	var data []byte
	if target == "cypher" {
		var genResult diag.Result
		data, genResult = cypherScript(cypherConfig, s)
		sink.Add(genResult)
		if genResult.HasErrors() {
			return cli.Exit(cli.ExitForResult(sink.Result()))
		}
	}
	// The load's residual warnings, and the cypher generators', precede the
	// generated artifact.
	sink.Flush()

	switch target {
	case "go":
		pkgName, _ := cmd.Flags().GetString("package")
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

// generateCypher runs both generators for s under c. A constraint error
// returns the constraint result alone, so a break both generators judge is
// reported once; otherwise the result holds both generators' issues.
// [neo4j.Diff] returns these issues for the same c and s, beside its issues
// for the remote rows it cannot plan from.
func generateCypher(c cypher.Config, s schema.Schema) ([]cypher.Constraint, []cypher.Index, diag.Result) {
	constraints, constraintResult := c.Constraints(s)
	if constraintResult.HasErrors() {
		return nil, nil, constraintResult
	}
	indexes, indexResult := c.Indexes(s)
	collector := diag.NewCollector(0)
	collector.Merge(constraintResult)
	collector.Merge(indexResult)
	return constraints, indexes, collector.Result()
}

// cypherScript renders the provisioning script for s under c: every
// constraint, then every index, each statement on its own line and ended by
// ";". The result is [generateCypher]'s; on an error the script is nil.
func cypherScript(c cypher.Config, s schema.Schema) ([]byte, diag.Result) {
	constraints, indexes, result := generateCypher(c, s)
	if result.HasErrors() {
		return nil, result
	}
	var b strings.Builder
	for _, con := range constraints {
		b.WriteString(con.Statement + ";\n")
	}
	for _, idx := range indexes {
		b.WriteString(idx.Statement + ";\n")
	}
	return []byte(b.String()), result
}

package main

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func newLoadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load <schema.yammm> <data-file>",
		Short: "Load data into an in-memory graph and validate",
		Long:  "Load JSON or CSV data into a schema-validated graph. Reports diagnostics and a summary. Useful for validation-only workflows.",
		Args:  cobra.ExactArgs(2),
		RunE:  withDiagnostics(runLoad),
	}

	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")

	registerModuleRootFlag(cmd)
	return cmd
}

func runLoad(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")

	schemaPath := args[0]
	dataPath := args[1]
	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return cli.Usagef("resolve path %q: %v", schemaPath, err)
	}

	// Load schema
	moduleRoot, loadOpts, err := moduleRootOptions(cmd)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	// Parse, validate, and build graph
	graphResult, _, err := loadGraph(cmd, sink, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		return err
	}
	sink.Add(graphResult)

	if exitCode := cli.ExitForResult(sink.Result()); exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}

// bindSchemaSource points the sink at a loaded schema, so every diagnostic the
// invocation renders resolves excerpts and relativizes paths the way the loader
// itself would.
//
// It is called as soon as the load returns rather than at the render, because a
// command that fails in a later phase renders through the wrapper's deferred
// call and never reaches its own code again.
//
// explicitRoot is the command's module-root flag where it has one, and "" where
// it does not; it selects the root together with the schema path (see
// [diagRootFor]).
func bindSchemaSource(sink *cli.DiagnosticSink, s *schema.Schema, explicitRoot, absSchemaPath string) {
	var provider diag.SourceProvider
	if s != nil && s.HasSourceProvider() {
		provider = s.Sources()
	}
	sink.SetSource(provider, diagRootFor(s, explicitRoot, absSchemaPath))
}

// reportSchemaLoad hands a schema load's diagnostics to the sink and reports
// whether the load failed — an unreadable schema file is an I/O failure, not a
// validation one.
//
// It returns no residual result. A successful load's warnings are already the
// sink's, and handing them back invited a caller to render them beside its own,
// which is the two-document shape this replaces. A load warning matters — it
// exists because the loader chose not to reject, and W_ANNOTATION_SHADOWED is
// the only signal that a subtype silently dropped an inherited annotation — and
// it now reaches the operator on every path, including the ones that return
// before the command's own phase runs.
func reportSchemaLoad(sink *cli.DiagnosticSink, s *schema.Schema, explicitRoot, absSchemaPath string, result diag.Result) error {
	bindSchemaSource(sink, s, explicitRoot, absSchemaPath)
	sink.Add(result)
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitForResult(result)}
	}
	return nil
}

// diagRootFor selects the root that rendered diagnostic locations are
// relativized against. A completed load is authoritative: the schema's
// recorded ModuleRoot is canonical (symlink-resolved), so it textually
// prefixes every file-backed SourceID the load produced — unless it is a
// synthetic root, which is a scheme string rather than a path and would
// relativize nothing. The CLI never synthetic-loads, so that arm is defence.
//
// When no schema is available (the load failed before producing one), the
// root is derived the way the loader derives its own: the explicit module
// root for commands that accept one, then the nearest ancestor holding a
// module-root marker, then the schema file's directory — each canonicalized
// the same way, so locations still relativize. Without the discovery step a
// failed load's diagnostics would relativize against a root the loader did
// not use.
func diagRootFor(s *schema.Schema, explicitRoot, absSchemaPath string) string {
	if s != nil {
		if root := s.ModuleRoot(); root != "" && filepath.IsAbs(root) {
			return root
		}
	}
	base := explicitRoot
	if base == "" {
		base = filepath.Dir(absSchemaPath)
		// A discovery error is deliberately ignored: the load already
		// reported it, and the renderer's job is to relativize what it can.
		if root, found, err := schema.FindModuleRoot(base); err == nil && found {
			return root
		}
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		return resolved
	}
	return filepath.Clean(base)
}

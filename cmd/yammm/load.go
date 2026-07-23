package main

import (
	"fmt"
	"os"
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
		RunE:  runLoad,
	}

	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")

	return cmd
}

func runLoad(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	dataPath := args[1]
	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", schemaPath, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Load schema
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath)
	if renderLoadDiagnostics(cmd, outputFormat, noColor, s, "", absSchemaPath, schemaResult) {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Parse, validate, and build graph
	result, _, err := loadGraph(cmd, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Render diagnostics
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}

// renderDiagnostics renders a diagnostic result to cobra's error writer.
// Derive moduleRoot via [diagRootFor] for schema-sourced diagnostics, or
// pass "" for diagnostics without source locations (e.g., constraint
// generation errors).
func renderDiagnostics(cmd *cobra.Command, outputFormat cli.OutputFormat, noColor bool, s *schema.Schema, moduleRoot string, result diag.Result) {
	w := cmd.ErrOrStderr()
	isTTY := cli.IsTTY(os.Stderr.Fd())
	var provider diag.SourceProvider
	if s != nil && s.HasSourceProvider() {
		provider = s.Sources()
	}
	renderer := cli.NewRenderer(outputFormat, isTTY, noColor, provider, moduleRoot)
	_ = cli.RenderResult(w, renderer, outputFormat, result)
}

// renderLoadDiagnostics renders a schema load's diagnostics and reports whether
// the load failed.
//
// It renders whenever the load produced anything to say — warnings included, not
// only errors. A load warning exists precisely because the loader chose not to
// reject (W_ANNOTATION_SHADOWED, for one, is the only signal that a subtype
// silently dropped an inherited annotation), so a command that rendered only on
// failure would make it unreachable. Diagnostics go to stderr, so surfacing them
// leaves every command's stdout contract intact.
//
// explicitRoot is the command's module-root flag where it has one, and "" where
// it does not; it selects the root diagnostics relativize against together with
// the schema path (see [diagRootFor]).
func renderLoadDiagnostics(
	cmd *cobra.Command,
	outputFormat cli.OutputFormat,
	noColor bool,
	s *schema.Schema,
	explicitRoot, absSchemaPath string,
	result diag.Result,
) bool {
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, explicitRoot, absSchemaPath), result)
	return result.HasErrors()
}

// diagRootFor selects the root that rendered diagnostic locations are
// relativized against. A completed load is authoritative: the schema's
// recorded ModuleRoot is canonical (symlink-resolved), so it textually
// prefixes every file-backed SourceID the load produced. When no schema is
// available (the load failed before producing one), the explicit module
// root — for commands that accept one — or the schema file's directory is
// canonicalized the same way the loader canonicalizes its module root, so
// locations still relativize.
func diagRootFor(s *schema.Schema, explicitRoot, absSchemaPath string) string {
	if s != nil {
		if root := s.ModuleRoot(); root != "" {
			return root
		}
	}
	base := explicitRoot
	if base == "" {
		base = filepath.Dir(absSchemaPath)
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		return resolved
	}
	return filepath.Clean(base)
}

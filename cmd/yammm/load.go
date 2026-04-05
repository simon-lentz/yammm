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
	if schemaResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, filepath.Dir(absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Parse, validate, and build graph
	result, _, err := loadGraph(cmd, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Render diagnostics
	renderDiagnostics(cmd, outputFormat, noColor, s, filepath.Dir(absSchemaPath), result)

	exitCode := cli.ExitForResult(result)
	if exitCode != cli.ExitOK {
		return &cli.ExitError{Code: exitCode}
	}
	return nil
}

// renderDiagnostics renders a diagnostic result to cobra's error writer.
// Pass moduleRoot as filepath.Dir(absSchemaPath) for schema-sourced diagnostics,
// or "" for diagnostics without source locations (e.g., constraint generation errors).
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

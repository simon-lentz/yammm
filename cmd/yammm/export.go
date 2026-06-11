package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/csv"
	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <schema.yammm> <data-file>",
		Short: "Load data and export to a target format",
		Long: `Load JSON or CSV data into a schema-validated graph, then export to JSON, CSV, or Cypher.

The --to cypher format produces parameterized Cypher statements intended for
inspection and integration into application code or migration tooling.
The output contains UNWIND/MERGE patterns with parameter placeholders ($key_id,
$props, $rows) and is not directly executable in Neo4j Browser or cypher-shell.`,
		Args: cobra.ExactArgs(2),
		RunE: runExport,
	}

	cmd.Flags().String("to", "", "output format: json, csv, or cypher (required)")
	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().String("output-dir", "", "output directory for CSV multi-type export (one file per type)")

	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func runExport(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	toFormat, _ := cmd.Flags().GetString("to")
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")
	outputPath, _ := cmd.Flags().GetString("output")
	outputDir, _ := cmd.Flags().GetString("output-dir")

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
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Check if the data file is a persisted snapshot.
	isSnapshot, err := cli.IsSnapshotFile(dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read data file: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	if isSnapshot {
		return exportFromSnapshot(cmd, s, dataPath, outputFormat, noColor, absSchemaPath, toFormat, outputPath, outputDir)
	}

	// Parse, validate, and build graph
	result, g, err := loadGraph(cmd, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	if result.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), result)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	snapshot := g.Snapshot()

	// Export
	switch strings.ToLower(toFormat) {
	case "json":
		return exportJSON(cmd, snapshot, outputPath)
	case "csv":
		return exportCSV(cmd, snapshot, s, outputPath, outputDir)
	case "cypher":
		return exportCypher(cmd, snapshot, s, outputPath)
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported export format %q: must be json, csv, or cypher\n", toFormat)
		return &cli.ExitError{Code: cli.ExitUsage}
	}
}

func exportJSON(cmd *cobra.Command, snapshot *graph.Snapshot, outputPath string) error {
	adapter, err := adapterjson.New(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create json adapter: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	data, err := adapter.MarshalObject(cmd.Context(), snapshot, adapterjson.WithIndent("\t"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal json: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	data = append(data, '\n')

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	return nil
}

func exportCSV(cmd *cobra.Command, snapshot *graph.Snapshot, _ *schema.Schema, outputPath, outputDir string) error {
	adapter := csv.New()

	types := snapshot.Types()

	// If --output-dir specified, always use directory output
	if outputDir != "" {
		return exportCSVToDir(cmd, adapter, snapshot, types, outputDir)
	}

	// Single type or --output: write to one destination
	if len(types) <= 1 || outputPath != "" {
		data, err := adapter.MarshalSnapshot(cmd.Context(), snapshot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: marshal csv: %v\n", err)
			return &cli.ExitError{Code: cli.ExitRuntime}
		}

		// If only one type, write it
		w := cmd.OutOrStdout()
		if outputPath != "" {
			f, err := os.Create(outputPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: create output file: %v\n", err)
				return &cli.ExitError{Code: cli.ExitRuntime}
			}
			defer f.Close()
			w = f
		}

		for _, typeData := range data {
			if _, err := w.Write(typeData); err != nil {
				fmt.Fprintf(os.Stderr, "error: write csv: %v\n", err)
				return &cli.ExitError{Code: cli.ExitRuntime}
			}
		}
		return nil
	}

	// Multi-type without --output-dir
	fmt.Fprintf(os.Stderr, "error: CSV export with multiple types requires --output-dir\n")
	return &cli.ExitError{Code: cli.ExitUsage}
}

func exportCSVToDir(cmd *cobra.Command, adapter *csv.Adapter, snapshot *graph.Snapshot, types []string, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "error: create output directory: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()

	writerFor := func(typeName string) (io.Writer, error) {
		path := filepath.Join(outputDir, typeName+".csv")
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		return f, nil
	}

	if err := adapter.WriteSnapshot(cmd.Context(), writerFor, snapshot); err != nil {
		fmt.Fprintf(os.Stderr, "error: write csv snapshot: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	fmt.Fprintf(os.Stderr, "wrote %d CSV files to %s\n", len(types), outputDir)
	return nil
}

// exportCypher writes parameterized Cypher statements to the output.
// Only Statement fields are written; Params are intentionally omitted because
// the output serves as a readable reference for integration, not as directly
// executable Cypher. Use the Go adapter API for programmatic execution with parameters.
func exportCypher(cmd *cobra.Command, snapshot *graph.Snapshot, s *schema.Schema, outputPath string) error {
	adapter := adaptern4j.New()

	// Generate shape for the schema
	shapes, result := adapter.ShapeForSchema(cmd.Context(), s)
	if result.HasErrors() {
		fmt.Fprintf(os.Stderr, "error: generate neo4j shape: %v\n", result.Err())
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	nodeQueries, err := adapter.BatchNodeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate node queries: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	edgeQueries, err := adapter.BatchEdgeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate edge queries: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	w := cmd.OutOrStdout()
	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create output file: %v\n", err)
			return &cli.ExitError{Code: cli.ExitRuntime}
		}
		defer f.Close()
		w = f
	}

	// Write node queries
	for _, nq := range nodeQueries {
		fmt.Fprintln(w, nq.Statement)
		fmt.Fprintln(w)
	}

	// Write edge queries
	for _, eq := range edgeQueries {
		fmt.Fprintf(w, "// %s\n", eq.RelationType)
		fmt.Fprintln(w, eq.Statement)
		fmt.Fprintln(w)
	}

	return nil
}

func exportFromSnapshot(cmd *cobra.Command, s *schema.Schema, dataPath string, outputFormat cli.OutputFormat, noColor bool, absSchemaPath string, toFormat, outputPath, outputDir string) error {
	snap, result, err := cli.LoadSnapshotFile(cmd.Context(), dataPath, s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Render warnings (e.g., unsupported hash algorithm).
	if !result.OK() {
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), result)
	}
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Route to adapter — same as raw data path.
	switch strings.ToLower(toFormat) {
	case "json":
		return exportJSON(cmd, snap, outputPath)
	case "csv":
		return exportCSV(cmd, snap, s, outputPath, outputDir)
	case "cypher":
		return exportCypher(cmd, snap, s, outputPath)
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported export format %q: must be json, csv, or cypher\n", toFormat)
		return &cli.ExitError{Code: cli.ExitUsage}
	}
}

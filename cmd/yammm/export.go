package main

import (
	"bytes"
	"fmt"
	"io"
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
		RunE: withDiagnostics(runExport),
	}

	cmd.Flags().String("to", "", "output format: json, csv, or cypher (required)")
	cmd.Flags().String("from", "", "input format override: json or csv")
	cmd.Flags().String("type", "", "type name for CSV data (required for single-type CSV)")
	cmd.Flags().String("type-column", "", "column name containing type names (for multi-type CSV)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	cmd.Flags().String("output-dir", "", "output directory for CSV multi-type export (one file per type)")

	_ = cmd.MarkFlagRequired("to")

	registerModuleRootFlag(cmd)
	return cmd
}

func runExport(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	toFormat, _ := cmd.Flags().GetString("to")
	fromFormat, _ := cmd.Flags().GetString("from")
	typeName, _ := cmd.Flags().GetString("type")
	typeColumn, _ := cmd.Flags().GetString("type-column")
	outputPath, _ := cmd.Flags().GetString("output")
	outputDir, _ := cmd.Flags().GetString("output-dir")

	target := strings.ToLower(toFormat)
	if err := validateExportFlags(toFormat, target, outputPath, outputDir); err != nil {
		return err
	}

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

	// Check if the data file is a persisted snapshot.
	isSnapshot, err := cli.IsSnapshotFile(dataPath)
	if err != nil {
		return cli.Runtimef("read data file: %v", err)
	}

	if isSnapshot {
		return exportFromSnapshot(cmd, sink, s, dataPath, target, outputPath, outputDir)
	}

	// Parse, validate, and build graph
	graphResult, g, err := loadGraph(cmd, sink, s, dataPath, fromFormat, typeName, typeColumn)
	if err != nil {
		return err
	}

	sink.Add(graphResult)
	sink.Render()
	if sink.Result().HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	return writeExport(cmd, sink, g.Snapshot(), s, target, outputPath, outputDir)
}

// validateExportFlags refuses a contradictory or inapplicable flag set before
// any work runs.
//
// Every one of these was previously discovered after the schema had loaded, the
// data had parsed and validated, the graph had been built and diagnostics had
// rendered — so an operator read a page of progress and then a usage error, or
// read no error at all because the flag was silently ignored and the output
// went somewhere else.
func validateExportFlags(raw, target, outputPath, outputDir string) error {
	switch target {
	case "json", "csv", "cypher":
	default:
		return cli.Usagef("unsupported export format %q: must be json, csv, or cypher", raw)
	}
	if outputPath != "" && outputDir != "" {
		return cli.Usagef("--output and --output-dir are mutually exclusive")
	}
	if outputDir != "" && target != "csv" {
		return cli.Usagef("--output-dir applies only to --to csv")
	}
	return nil
}

// writeExport routes a snapshot to the adapter for target, which
// [validateExportFlags] has already admitted.
func writeExport(cmd *cobra.Command, sink *cli.DiagnosticSink, snap *graph.Snapshot, s *schema.Schema, target, outputPath, outputDir string) error {
	switch target {
	case "json":
		return exportJSON(cmd, snap, outputPath)
	case "csv":
		return exportCSV(cmd, sink, snap, s, outputPath, outputDir)
	default:
		return exportCypher(cmd, snap, s, outputPath)
	}
}

func exportJSON(cmd *cobra.Command, snapshot *graph.Snapshot, outputPath string) error {
	data, err := adapterjson.New().MarshalObject(cmd.Context(), snapshot, adapterjson.WithIndent("\t"))
	if err != nil {
		return cli.Runtimef("marshal json: %v", err)
	}
	data = append(data, '\n')

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write output: %v", err)
	}
	return nil
}

func exportCSV(cmd *cobra.Command, sink *cli.DiagnosticSink, snapshot *graph.Snapshot, _ *schema.Schema, outputPath, outputDir string) error {
	adapter := csv.New()

	types := snapshot.Types()

	// If --output-dir specified, always use directory output
	if outputDir != "" {
		return exportCSVToDir(cmd, sink, adapter, snapshot, types, outputDir)
	}

	// One destination holds one type. --output does not change that: the
	// adapter emits a header row per type, so several types written to one file
	// interleave into a document no CSV reader can read back, in an order that
	// varies between runs.
	if len(types) > 1 {
		return cli.Usagef("CSV export with multiple types requires --output-dir")
	}

	data, err := adapter.MarshalSnapshot(cmd.Context(), snapshot)
	if err != nil {
		return cli.Runtimef("marshal csv: %v", err)
	}

	var out []byte
	for _, typeData := range data {
		out = append(out, typeData...)
	}
	if err := cli.WriteTo(out, outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write csv: %v", err)
	}
	return nil
}

func exportCSVToDir(cmd *cobra.Command, sink *cli.DiagnosticSink, adapter *csv.Adapter, snapshot *graph.Snapshot, types []schema.TypeID, outputDir string) error {
	staged, err := cli.NewStagedFiles(outputDir)
	if err != nil {
		return cli.Runtimef("%v", err)
	}
	defer staged.Rollback()

	writerFor := func(typeName string) (io.Writer, error) {
		return staged.Create(typeName + ".csv")
	}

	if err := adapter.WriteSnapshot(cmd.Context(), writerFor, snapshot); err != nil {
		return cli.Runtimef("write csv snapshot: %v", err)
	}
	if err := staged.Commit(); err != nil {
		return cli.Runtimef("write csv snapshot: %v", err)
	}

	sink.Statusf("wrote %d CSV files to %s\n", len(types), outputDir)
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
		return cli.Validationf("generate neo4j shape: %v", result.Err())
	}

	nodeQueries, err := adapter.BatchNodeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		return cli.Runtimef("generate node queries: %v", err)
	}

	edgeQueries, err := adapter.BatchEdgeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		return cli.Runtimef("generate edge queries: %v", err)
	}

	// Rendered whole before anything is written: the queries are already in
	// memory, so buffering costs nothing and makes the write one checked
	// operation rather than a run of unchecked Fprintf calls.
	var buf bytes.Buffer

	// Write node queries, a phase marker at each Kind boundary.
	lastKind := adaptern4j.NodeQueryKind(-1)
	for _, nq := range nodeQueries {
		if nq.Kind != lastKind {
			fmt.Fprintf(&buf, "// -- %s --\n", nq.Kind)
			lastKind = nq.Kind
		}
		fmt.Fprintln(&buf, nq.Statement)
		fmt.Fprintln(&buf)
	}

	// Write edge queries
	for _, eq := range edgeQueries {
		fmt.Fprintf(&buf, "// %s\n", eq.RelationType)
		fmt.Fprintln(&buf, eq.Statement)
		fmt.Fprintln(&buf)
	}

	if err := cli.WriteTo(buf.Bytes(), outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write output: %v", err)
	}
	return nil
}

// exportFromSnapshot exports a persisted .ys snapshot.
//
// A warning here — an unsupported snapshot hash algorithm, a provenance path
// that would not parse — means integrity was NOT fully verified, and the
// operator must read it before the export lands, which is why the render is
// explicit rather than left to the wrapper.
func exportFromSnapshot(cmd *cobra.Command, sink *cli.DiagnosticSink, s *schema.Schema, dataPath, target, outputPath, outputDir string) error {
	snap, snapResult, err := cli.LoadSnapshotFile(cmd.Context(), dataPath, s)
	if err != nil {
		return cli.Runtimef("%v", err)
	}

	sink.Add(snapResult)
	sink.Render()
	if sink.Result().HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	return writeExport(cmd, sink, snap, s, target, outputPath, outputDir)
}

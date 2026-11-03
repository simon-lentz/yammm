package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <schema.yammm> <data-file>",
		Short: "Load data and export to a target format",
		Long: `Load JSON data into a schema-validated graph, then export to JSON or Cypher.

The --to cypher format produces parameterized Cypher statements intended for
inspection and integration into application code or migration tooling.
The output contains UNWIND statements over one parameter placeholder, $rows,
whose rows carry the keys and properties each statement reads (a node merge's as
row.key_<name> and row.props, and row.update_props for a type with @writeOnce
properties), and is not directly executable in Neo4j Browser or cypher-shell.`,
		Args: cobra.ExactArgs(2),
		RunE: withDiagnostics(runExport),
	}

	cmd.Flags().String("to", "", "output format: json or cypher (required)")
	cmd.Flags().String("output", "", "output file path (default: stdout)")
	registerLabelFlags(cmd)

	_ = cmd.MarkFlagRequired("to")

	registerModuleRootFlag(cmd)
	return cmd
}

func runExport(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	toFormat, _ := cmd.Flags().GetString("to")
	outputPath, _ := cmd.Flags().GetString("output")

	target := strings.ToLower(toFormat)
	if err := validateExportFlags(cmd, toFormat, target); err != nil {
		return err
	}

	schemaPath := args[0]
	dataPath := args[1]
	absSchemaPath, err := schemaOperand(schemaPath)
	if err != nil {
		return err
	}
	if err := cli.CheckOperand("data file", dataPath); err != nil {
		return err
	}

	// Load schema
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	// Read once, so a pipe gives the parse the bytes the snapshot probe looked at.
	data, err := cli.ReadFile(dataPath)
	if err != nil {
		return cli.Runtimef("read data file: %v", err)
	}
	if cli.IsSnapshot(data) {
		return exportFromSnapshot(cmd, sink, s, data, target, outputPath)
	}

	// A snapshot may hold any name, since no identity is derived from it.
	if err := checkDataOperands(dataPath); err != nil {
		return err
	}
	parsed, parseResult, err := cli.ParseJSON(cmd.Context(), dataPath, data)
	if err != nil {
		return err
	}
	sink.Add(parseResult)
	a := assembleParsed(cmd, sink, s, parsed, nil)
	reportLoaded(sink, a)

	sink.Flush()
	if sink.Result().HasErrors() {
		return cli.Exit(cli.ExitForResult(sink.Result()))
	}

	return writeExport(cmd, sink, a.graph.Snapshot(), s, target, outputPath)
}

// validateExportFlags refuses a target or a label flag it cannot apply before
// any work runs, so an operator never reads a page of progress and then a usage
// error.
func validateExportFlags(cmd *cobra.Command, raw, target string) error {
	switch target {
	case "json", "cypher":
	default:
		return cli.Usagef("unsupported export format %q: must be json or cypher", raw)
	}
	// The label flags shape Neo4j labels, which only the cypher target emits.
	for _, flag := range []string{"separator", "prefix"} {
		if target != "cypher" && cmd.Flags().Changed(flag) {
			return cli.Usagef("--%s applies only to --to cypher", flag)
		}
	}
	if target == "cypher" {
		if _, err := labelOptions(cmd); err != nil {
			return err
		}
	}
	return nil
}

// writeExport routes a snapshot to the adapter for target, which
// [validateExportFlags] has already admitted.
func writeExport(cmd *cobra.Command, sink *cli.DiagnosticSink, snap graph.Snapshot, s schema.Schema, target, outputPath string) error {
	if target == "json" {
		return exportJSON(cmd, snap, outputPath)
	}
	return exportCypher(cmd, sink, snap, s, outputPath)
}

func exportJSON(cmd *cobra.Command, snapshot graph.Snapshot, outputPath string) error {
	data, err := adapterjson.New().MarshalObject(cmd.Context(), snapshot, adapterjson.WithIndent("\t"))
	if err != nil {
		return writerFailure("marshal json", err)
	}
	data = append(data, '\n')

	if err := cli.WriteTo(data, outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write output: %v", err)
	}
	return nil
}

// writerFailure reports a writer's error as the exit it earns: a refusal of
// the data, a value the format cannot represent, exits 1 as any other refusal
// of the input does, and anything else is a failure outside the input, 3.
func writerFailure(what string, err error) error {
	if errors.Is(err, adapterjson.ErrUnrepresentable) || errors.Is(err, adaptern4j.ErrUnrepresentable) {
		return cli.Validationf("%s: %v", what, err)
	}
	return cli.Runtimef("%s: %v", what, err)
}

// exportCypher writes parameterized Cypher statements to the output.
// Only Statement fields are written; Params are intentionally omitted because
// the output serves as a readable reference for integration, not as directly
// executable Cypher. Use the Go adapter API for programmatic execution with parameters.
func exportCypher(cmd *cobra.Command, sink *cli.DiagnosticSink, snapshot graph.Snapshot, s schema.Schema, outputPath string) error {
	// The statements name labels, and a label composed differently from the one
	// the target graph carries writes data no constraint guards. The sibling
	// neo4j commands take these flags for the same reason.
	labelOpts, err := labelOptions(cmd)
	if err != nil {
		return err
	}
	adapter := adaptern4j.New(labelOpts...)

	// The shape's diagnostics are the command's own, so they reach the sink —
	// and precede the statements — rather than being printed as an error.
	shapes, result := adapter.ShapeForSchema(cmd.Context(), s)
	sink.Add(result)
	if result.HasErrors() {
		return cli.Exit(cli.ExitForResult(result))
	}
	sink.Flush()

	nodeQueries, err := adapter.BatchNodeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		return writerFailure("generate node queries", err)
	}

	edgeQueries, err := adapter.BatchEdgeQueries(cmd.Context(), snapshot, shapes)
	if err != nil {
		return writerFailure("generate edge queries", err)
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
// operator must read it before the export lands, which is why the flush is
// explicit rather than left to the wrapper.
func exportFromSnapshot(cmd *cobra.Command, sink *cli.DiagnosticSink, s schema.Schema, data []byte, target, outputPath string) error {
	snap, snapResult := snapshot.Load(cmd.Context(), data, s)

	sink.Add(snapResult)
	sink.Flush()
	if sink.Result().HasErrors() {
		return cli.Exit(cli.ExitForResult(sink.Result()))
	}

	return writeExport(cmd, sink, snap, s, target, outputPath)
}

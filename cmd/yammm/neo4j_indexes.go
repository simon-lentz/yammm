package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newNeo4jIndexesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "indexes <schema.yammm>",
		Short: "Generate Neo4j index Cypher statements from a schema's annotations",
		Args:  cobra.ExactArgs(1),
		RunE:  withDiagnostics(runNeo4jIndexes),
	}

	// Index names are always emitted and indexes apply to every edition, so the
	// constraint-shape flags do not apply here. The label flags do: a graph
	// generated with a prefix or a different separator carries labels this
	// command must reproduce exactly, or `yammm neo4j diff` compares index DDL
	// against a disjoint set of labels.
	registerLabelFlags(cmd)

	registerModuleRootFlag(cmd)
	return cmd
}

func runNeo4jIndexes(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	schemaPath := args[0]
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

	// Configure adapter
	adapter := neo4j.New(labelOptions(cmd)...)

	statements, indexResult := adapter.IndexesForSchema(cmd.Context(), s)
	sink.Add(indexResult)
	sink.Flush()
	if sink.Result().HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Print each statement
	w := cmd.OutOrStdout()
	for _, stmt := range statements {
		fmt.Fprintln(w, stmt)
	}

	return nil
}

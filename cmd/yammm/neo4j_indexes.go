package main

import (
	"fmt"
	"os"
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
		RunE:  runNeo4jIndexes,
	}

	// Index names are always emitted and indexes apply to every edition, so the
	// constraint-shape flags do not apply here. The label flags do: a graph
	// generated with a prefix or a different separator carries labels this
	// command must reproduce exactly, or `yammm neo4j diff` compares index DDL
	// against a disjoint set of labels.
	registerLabelFlags(cmd)

	return cmd
}

func runNeo4jIndexes(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")

	outputFormat, err := cli.ParseOutputFormat(formatStr)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	absSchemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve path %q: %v\n", schemaPath, err)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Load schema
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath)
	pending, failed := reportSchemaLoad(cmd, outputFormat, noColor, s, "", absSchemaPath, schemaResult)
	if failed {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Configure adapter
	adapter := neo4j.New(labelOptions(cmd)...)

	// Generate index statements. The load's residual warnings fold in so one
	// invocation writes one result on either path.
	statements, indexResult := adapter.IndexesForSchema(cmd.Context(), s)
	result := cli.MergeResults(pending, indexResult)
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), result)
	if result.HasErrors() {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Print each statement
	w := cmd.OutOrStdout()
	for _, stmt := range statements {
		fmt.Fprintln(w, stmt)
	}

	return nil
}

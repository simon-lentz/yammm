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

	// Index names are always emitted and indexes apply to every edition, so
	// there is no --named or --edition flag; --separator mirrors the
	// constraints command.
	cmd.Flags().String("separator", "__", "label separator between schema name and type name")

	return cmd
}

func runNeo4jIndexes(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	separator, _ := cmd.Flags().GetString("separator")

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
	if schemaResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Configure adapter
	adapter := neo4j.New(neo4j.WithLabelSeparator(separator))

	// Generate index statements
	statements, result := adapter.IndexesForSchema(cmd.Context(), s)
	if result.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", result)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Print each statement
	w := cmd.OutOrStdout()
	for _, stmt := range statements {
		fmt.Fprintln(w, stmt)
	}

	return nil
}

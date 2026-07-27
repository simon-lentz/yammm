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

func newNeo4jConstraintsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "constraints <schema.yammm>",
		Short: "Generate Neo4j constraint Cypher statements from a schema",
		Args:  cobra.ExactArgs(1),
		RunE:  runNeo4jConstraints,
	}

	// Shared with `yammm neo4j diff`, whose desired side is this command's
	// output — see registerConstraintFlags.
	registerLabelFlags(cmd)
	registerConstraintFlags(cmd)

	return cmd
}

func runNeo4jConstraints(cmd *cobra.Command, args []string) error {
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
	opts, err := constraintOptions(cmd)
	if err != nil {
		return err
	}
	adapter := neo4j.New(opts...)

	// Generate constraints. The load's residual warnings fold in so one
	// invocation writes one result on either path.
	statements, constraintResult := adapter.ConstraintsForSchema(cmd.Context(), s)
	result := cli.MergeResults(pending, constraintResult)
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

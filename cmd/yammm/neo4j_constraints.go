package main

import (
	"fmt"
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
		RunE:  withDiagnostics(runNeo4jConstraints),
	}

	// Shared with `yammm neo4j diff`, whose desired side is this command's
	// output — see registerConstraintFlags.
	registerLabelFlags(cmd)
	registerConstraintFlags(cmd)

	registerModuleRootFlag(cmd)
	return cmd
}

func runNeo4jConstraints(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	opts, err := constraintOptions(cmd)
	if err != nil {
		return err
	}

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

	adapter := neo4j.New(opts...)

	statements, constraintResult := adapter.ConstraintsForSchema(cmd.Context(), s)
	sink.Add(constraintResult)
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

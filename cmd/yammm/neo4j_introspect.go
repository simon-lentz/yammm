package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func newNeo4jIntrospectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "introspect",
		Short: "Infer a .yammm schema from a live Neo4j database",
		Long:  "Connects to a Neo4j database, discovers constraints and relationships, and generates a .yammm schema scaffold.",
		Args:  cobra.NoArgs,
		RunE:  runNeo4jIntrospect,
	}

	cmd.Flags().String("schema", "", "filter inference to a specific schema name prefix")
	cmd.Flags().String("output", "", "output file path (default: stdout)")

	return cmd
}

func runNeo4jIntrospect(cmd *cobra.Command, _ []string) error {
	uri, _ := cmd.Flags().GetString("uri")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	database, _ := cmd.Flags().GetString("database")
	schemaFilter, _ := cmd.Flags().GetString("schema")
	outputPath, _ := cmd.Flags().GetString("output")

	if uri == "" {
		fmt.Fprintf(os.Stderr, "error: --uri is required (or set YAMMM_NEO4J_URI)\n")
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	ctx := cmd.Context()

	// Connect to database
	driver, err := cli.ConnectNeo4j(ctx, uri, username, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	defer driver.Close(ctx)

	// Fetch constraints
	constraintRecords, err := cli.RunQuery(ctx, driver, database, adaptern4j.IntrospectConstraintsQuery(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch constraints: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	constraints, err := adaptern4j.ParseRemoteConstraints(constraintRecords)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse constraints: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Fetch relationships
	adapter := adaptern4j.New()
	relQuery, relParams := adapter.IntrospectRelationshipsQueryFor(schemaFilter)
	relRecords, err := cli.RunQuery(ctx, driver, database, relQuery, relParams)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch relationships: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	relationships, err := adaptern4j.ParseRemoteRelationships(relRecords)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse relationships: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Infer schema
	dsl, err := adapter.InferSchema(constraints, relationships, schemaFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: infer schema: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Write output
	if err := cli.WriteTo([]byte(dsl), outputPath, cmd.OutOrStdout()); err != nil {
		fmt.Fprintf(os.Stderr, "error: write output: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	return nil
}

package main

import (
	"context"

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
		RunE:  withDiagnostics(runNeo4jIntrospect),
	}

	registerLabelFlags(cmd)
	cmd.Flags().String("schema", "", "filter inference to a specific schema name prefix")
	cmd.Flags().String("output", "", "output file path (default: stdout)")

	return cmd
}

func runNeo4jIntrospect(cmd *cobra.Command, _ []string, _ *cli.DiagnosticSink) error {
	uri, _ := cmd.Flags().GetString("uri")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	database, _ := cmd.Flags().GetString("database")
	schemaFilter, _ := cmd.Flags().GetString("schema")
	outputPath, _ := cmd.Flags().GetString("output")

	if uri == "" {
		return cli.Usagef("--uri is required (or set YAMMM_NEO4J_URI)")
	}
	labelOpts, err := labelOptions(cmd)
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	// Connect to database
	driver, err := cli.ConnectNeo4j(ctx, uri, username, password)
	if err != nil {
		return cli.Runtimef("%v", err)
	}
	defer driver.Close(ctx)

	dsl, err := introspectSchema(ctx, cli.DriverQueries(driver), database, schemaFilter, labelOpts...)
	if err != nil {
		return err
	}

	// Write output
	if err := cli.WriteTo([]byte(dsl), outputPath, cmd.OutOrStdout()); err != nil {
		return cli.Runtimef("write output: %v", err)
	}

	return nil
}

// introspectSchema reads a database's constraints and relationships through run
// and returns the .yammm scaffold they infer.
//
// It takes the runner rather than a driver because everything from here on is
// pure: the two queries' shapes, both parsers and the emitter are exercisable
// against recorded records, and nothing past the command's --uri guard was
// reachable by any test before this seam existed.
func introspectSchema(
	ctx context.Context,
	run cli.QueryRunner,
	database, schemaFilter string,
	opts ...adaptern4j.Option,
) (string, error) {
	constraintRecords, err := run(ctx, database, adaptern4j.IntrospectConstraintsQuery(), nil)
	if err != nil {
		return "", cli.Runtimef("fetch constraints: %v", err)
	}

	constraints, err := adaptern4j.ParseRemoteConstraints(constraintRecords)
	if err != nil {
		return "", cli.Runtimef("parse constraints: %v", err)
	}
	// The same projection `neo4j diff` guards, guarded for the same reason:
	// this command issued the full SHOW CONSTRAINTS projection, so a parsed
	// constraint carrying a name and no type means the type column did not
	// arrive. Inferring from it scaffolds a schema with no primary keys, which
	// reads as a database that has none.
	if n := untypedRemoteObjects(len(constraints), func(i int) string { return constraints[i].Type }); n > 0 {
		return "", unreadableProjection(n, len(constraints), "constraint", "SHOW CONSTRAINTS")
	}

	adapter := adaptern4j.New(opts...)
	relQuery, relParams := adapter.IntrospectRelationshipsQueryFor(schemaFilter)
	relRecords, err := run(ctx, database, relQuery, relParams)
	if err != nil {
		return "", cli.Runtimef("fetch relationships: %v", err)
	}

	relationships, err := adaptern4j.ParseRemoteRelationships(relRecords)
	if err != nil {
		return "", cli.Runtimef("parse relationships: %v", err)
	}

	// A starting point for a human to edit, not a schema expected to load:
	// a database does not record type identity, association-versus-composition
	// or import structure, so every place the command guessed is a TODO in the
	// output rather than a failure here.
	return adapter.InferSchema(constraints, relationships, schemaFilter), nil
}

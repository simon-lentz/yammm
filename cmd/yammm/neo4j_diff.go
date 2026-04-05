package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newNeo4jDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <schema.yammm>",
		Short: "Compare schema constraints against a live Neo4j database",
		Long:  "Performs a semantic four-way diff between desired schema constraints and actual database constraints.",
		Args:  cobra.ExactArgs(1),
		RunE:  runNeo4jDiff,
	}

	return cmd
}

func runNeo4jDiff(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	uri, _ := cmd.Flags().GetString("uri")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	database, _ := cmd.Flags().GetString("database")

	if uri == "" {
		fmt.Fprintf(os.Stderr, "error: --uri is required (or set YAMMM_NEO4J_URI)\n")
		return &cli.ExitError{Code: cli.ExitUsage}
	}

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
		renderDiagnostics(cmd, outputFormat, noColor, s, absSchemaPath, schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Generate desired constraints
	adapter := adaptern4j.New()
	desired, constraintResult := adapter.ConstraintsStructured(cmd.Context(), s)
	if constraintResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", constraintResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Connect to database
	ctx := cmd.Context()
	driver, err := cli.ConnectNeo4j(ctx, uri, username, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}
	defer driver.Close(ctx)

	// Fetch actual constraints
	records, err := cli.RunQuery(ctx, driver, database, adaptern4j.IntrospectConstraintsQuery(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch constraints: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	actual, err := adaptern4j.ParseRemoteConstraints(records)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse constraints: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Diff
	diff := adapter.DiffConstraints(desired, actual, s.Name())

	// Print diff result
	w := cmd.OutOrStdout()
	printDiffResult(w, diff)

	// Exit code: 1 if any drift, create, or drop
	if len(diff.Drift) > 0 || len(diff.Create) > 0 || len(diff.Drop) > 0 {
		return &cli.ExitError{Code: cli.ExitValidation}
	}
	return nil
}

func printDiffResult(w interface{ Write([]byte) (int, error) }, diff *adaptern4j.ConstraintDiffResult) {
	if len(diff.Match) > 0 {
		fmt.Fprintf(w, "matched: %d constraints\n", len(diff.Match))
	}

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "drift: %s (%s)\n", d.Desired.Name, d.Reason)
	}

	for _, c := range diff.Create {
		fmt.Fprintf(w, "create: %s\n", c.Statement)
	}

	for _, d := range diff.Drop {
		fmt.Fprintf(w, "drop: %s (name: %s)\n", d.CreateStatement, d.Name)
	}

	total := len(diff.Match) + len(diff.Drift) + len(diff.Create) + len(diff.Drop)
	fmt.Fprintf(w, "\nsummary: %d matched, %d drifted, %d to create, %d to drop (total: %d)\n",
		len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop), total)
}

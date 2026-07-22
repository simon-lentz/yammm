package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	adaptern4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/schema"
)

func newNeo4jDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <schema.yammm>",
		Short: "Compare schema constraints and indexes against a live Neo4j database",
		Long:  "Performs a semantic four-way diff between desired schema constraints and indexes and their actual database state.",
		Args:  cobra.ExactArgs(1),
		RunE:  runNeo4jDiff,
	}

	// Match how the target graph was generated so the desired side of the diff
	// uses the same labels and edition-gated constraints (mirroring the
	// constraints command's flags).
	cmd.Flags().String("edition", "enterprise", "target Neo4j edition: enterprise or community")
	cmd.Flags().String("separator", "__", "label separator between schema name and type name")
	cmd.Flags().Bool("named", true, "generate named constraints in the diff's create output")

	return cmd
}

func runNeo4jDiff(cmd *cobra.Command, args []string) error {
	formatStr, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")
	uri, _ := cmd.Flags().GetString("uri")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	database, _ := cmd.Flags().GetString("database")
	edition, _ := cmd.Flags().GetString("edition")
	separator, _ := cmd.Flags().GetString("separator")
	named, _ := cmd.Flags().GetBool("named")

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
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), schemaResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Configure the adapter to match the target graph's generation settings.
	var opts []adaptern4j.Option
	opts = append(opts, adaptern4j.WithLabelSeparator(separator))
	opts = append(opts, adaptern4j.WithNamedConstraints(named))
	switch strings.ToLower(edition) {
	case "enterprise":
		opts = append(opts, adaptern4j.WithEdition(adaptern4j.Enterprise))
	case "community":
		opts = append(opts, adaptern4j.WithEdition(adaptern4j.Community))
	default:
		fmt.Fprintf(os.Stderr, "error: invalid edition %q: must be \"enterprise\" or \"community\"\n", edition)
		return &cli.ExitError{Code: cli.ExitUsage}
	}

	// Generate desired constraints
	adapter := adaptern4j.New(opts...)
	desired, constraintResult := adapter.ConstraintsStructured(cmd.Context(), s)
	if constraintResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", constraintResult)
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Generate desired indexes
	desiredIndexes, indexResult := adapter.IndexesStructured(cmd.Context(), s)
	if indexResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, nil, "", indexResult)
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

	// Diff constraints
	diff := adapter.DiffConstraints(desired, actual, s.Name())

	// Print constraint diff result
	w := cmd.OutOrStdout()
	printDiffResult(w, diff)

	// Fetch actual indexes
	indexRecords, err := cli.RunQuery(ctx, driver, database, adaptern4j.IntrospectIndexesQuery(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch indexes: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	actualIndexes, err := adaptern4j.ParseRemoteIndexes(indexRecords)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse indexes: %v\n", err)
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Diff indexes (always on — a schema-owned remote index with no declaration
	// surfaces as a drop).
	indexDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, s.Name())
	printIndexDiffResult(w, indexDiff)

	// Exit code: 1 if any constraint or index drift, create, or drop
	if len(diff.Drift) > 0 || len(diff.Create) > 0 || len(diff.Drop) > 0 ||
		len(indexDiff.Drift) > 0 || len(indexDiff.Create) > 0 || len(indexDiff.Drop) > 0 {
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

func printIndexDiffResult(w interface{ Write([]byte) (int, error) }, diff *adaptern4j.IndexDiffResult) {
	if len(diff.Match) > 0 {
		fmt.Fprintf(w, "matched: %d indexes\n", len(diff.Match))
	}

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "index drift: %s (%s)\n", d.Desired.Name, d.Reason)
	}

	for _, c := range diff.Create {
		fmt.Fprintf(w, "index create: %s\n", c.Statement)
	}

	for _, d := range diff.Drop {
		label := ""
		if len(d.LabelsOrTypes) > 0 {
			label = d.LabelsOrTypes[0]
		}
		fmt.Fprintf(w, "index drop: %s (%s on %s)\n", d.Name, d.Type, label)
	}

	total := len(diff.Match) + len(diff.Drift) + len(diff.Create) + len(diff.Drop)
	fmt.Fprintf(w, "\nindex summary: %d matched, %d drifted, %d to create, %d to drop (total: %d)\n",
		len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop), total)
}

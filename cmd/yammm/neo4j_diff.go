package main

import (
	"fmt"
	"io"
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
		Long: "Performs a semantic four-way diff between desired schema constraints and indexes and their actual database state.\n\n" +
			"Any drift, missing definition, or undeclared schema-owned definition exits 1; nothing is applied. " +
			"An index in the database that the schema does not declare counts as drift — declare it with @index/@@index/@vector, " +
			"or pass --indexes=false for a constraints-only diff.",
		Args: cobra.ExactArgs(1),
		RunE: runNeo4jDiff,
	}

	// Match how the target graph was generated so the desired side of the diff
	// uses the same labels and edition-gated constraints (mirroring the
	// constraints command's flags).
	cmd.Flags().String("edition", "enterprise", "target Neo4j edition: enterprise or community")
	cmd.Flags().String("separator", "__", "label separator between schema name and type name")
	cmd.Flags().Bool("named", true, "generate named constraints in the diff's create output")
	cmd.Flags().Bool("indexes", true, "include index drift in the diff and exit code; --indexes=false is constraints-only")

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
	indexesEnabled, _ := cmd.Flags().GetBool("indexes")

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
	if renderLoadDiagnostics(cmd, outputFormat, noColor, s, "", absSchemaPath, schemaResult) {
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

	// Generate desired indexes (skipped in constraints-only mode so index-annotation
	// errors do not block a constraints-only diff).
	var desiredIndexes []adaptern4j.Index
	if indexesEnabled {
		di, indexResult := adapter.IndexesStructured(cmd.Context(), s)
		if indexResult.HasErrors() {
			renderDiagnostics(cmd, outputFormat, noColor, nil, "", indexResult)
			return &cli.ExitError{Code: cli.ExitValidation}
		}
		desiredIndexes = di
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

	// Diff indexes unless disabled. A schema-owned remote index with no
	// declaration surfaces as a drop; on a server that cannot report indexes the
	// diff degrades to constraints-only rather than discarding the constraint diff
	// already printed above — but it says so, and does not exit 0.
	indexes := indexDiffSkipped
	if indexesEnabled {
		switch indexRecords, err := cli.RunQuery(ctx, driver, database, adaptern4j.IntrospectIndexesQuery(), nil); {
		case err != nil:
			fmt.Fprintf(os.Stderr, "warning: index state was NOT compared: fetch indexes: %v\n", err)
			indexes = indexDiffUnavailable
		default:
			actualIndexes, parseErr := adaptern4j.ParseRemoteIndexes(indexRecords)
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "warning: index state was NOT compared: parse indexes: %v\n", parseErr)
				indexes = indexDiffUnavailable
				break
			}
			indexDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, s.Name())
			printIndexDiffResult(w, indexDiff)
			indexes = indexDiffClean
			if len(indexDiff.Drift) > 0 || len(indexDiff.Create) > 0 || len(indexDiff.Drop) > 0 {
				indexes = indexDiffDrifted
			}
		}
	}

	constraintDrift := len(diff.Drift) > 0 || len(diff.Create) > 0 || len(diff.Drop) > 0
	if code := neo4jDiffExit(constraintDrift, indexes); code != cli.ExitOK {
		return &cli.ExitError{Code: code}
	}
	return nil
}

// indexDiffOutcome records what the index half of a diff produced, so the exit
// code can tell "compared, found drift" apart from "never compared".
type indexDiffOutcome int

const (
	indexDiffSkipped     indexDiffOutcome = iota // --indexes=false: the caller opted out
	indexDiffClean                               // compared; no drift found
	indexDiffDrifted                             // compared; drift, creates, or drops found
	indexDiffUnavailable                         // requested, but index introspection failed
)

// neo4jDiffExit maps the two halves of the diff onto an exit code.
//
// Drift wins: it is the actionable signal, and a caller that sees exit 1 goes
// looking at the printed diff either way. An unavailable index diff is the
// interesting case — it must NOT exit 0. Degrading to constraints-only is right
// (the constraint diff already printed is complete and worth keeping), but the
// caller asked for index drift and did not get it, so reporting success would
// let a drift gate read "no drift" from a comparison that never ran. Opting out
// with --indexes=false is a different thing entirely: nothing was asked for, so
// nothing is owed.
func neo4jDiffExit(constraintDrift bool, indexes indexDiffOutcome) int {
	if constraintDrift || indexes == indexDiffDrifted {
		return cli.ExitValidation
	}
	if indexes == indexDiffUnavailable {
		return cli.ExitRuntime
	}
	return cli.ExitOK
}

// printMatchedLine writes the leading "matched: N <noun>" line when there are
// matches. Shared bookend of the constraint and index diff reports.
func printMatchedLine(w io.Writer, noun string, matched int) {
	if matched > 0 {
		fmt.Fprintf(w, "matched: %d %s\n", matched, noun)
	}
}

// printDiffSummary writes the trailing tally line. prefix is "" for constraints
// and "index " for indexes; the format is otherwise identical, so a layout
// change lands in one place. Shared bookend of the constraint and index reports.
func printDiffSummary(w io.Writer, prefix string, match, drift, create, drop int) {
	total := match + drift + create + drop
	fmt.Fprintf(w, "\n%ssummary: %d matched, %d drifted, %d to create, %d to drop (total: %d)\n",
		prefix, match, drift, create, drop, total)
}

func printDiffResult(w io.Writer, diff *adaptern4j.ConstraintDiffResult) {
	printMatchedLine(w, "constraints", len(diff.Match))

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "drift: %s (%s)\n", d.Desired.Name, d.Reason)
	}

	for _, c := range diff.Create {
		fmt.Fprintf(w, "create: %s\n", c.Statement)
	}

	for _, d := range diff.Drop {
		fmt.Fprintf(w, "drop: %s (name: %s)\n", d.CreateStatement, d.Name)
	}

	printDiffSummary(w, "", len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop))
}

func printIndexDiffResult(w io.Writer, diff *adaptern4j.IndexDiffResult) {
	printMatchedLine(w, "indexes", len(diff.Match))

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

	// Unverified indexes exist but could not be fully compared. They are reported
	// separately from matches so an unchecked index is never read as in sync, and
	// separately from the summary tally, whose four buckets are the verified
	// classification shared with the constraint report.
	for _, u := range diff.Unverified {
		fmt.Fprintf(w, "index unverified: %s (%s)\n", u.Desired.Name, u.Reason)
	}

	printDiffSummary(w, "index ", len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop))

	if n := len(diff.Unverified); n > 0 {
		fmt.Fprintf(w, "index note: %d index(es) could not be fully verified\n", n)
	}
}

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
		Long: "Performs a semantic five-way diff between desired schema constraints and indexes and their actual database state:\n" +
			"matched, drifted, to-create, to-drop, and unverified (present, but the server did not report enough to compare it).\n\n" +
			"Any drift, missing definition, or undeclared schema-owned definition exits 1; an unverifiable definition exits 3; nothing is applied. " +
			"An index in the database that the schema does not declare counts as drift — declare it with @index/@@index/@vector, " +
			"or pass --indexes=false for a constraints-only diff.",
		Args: cobra.ExactArgs(1),
		RunE: runNeo4jDiff,
	}

	// The desired side of the diff is exactly what `yammm neo4j constraints` and
	// `yammm neo4j indexes` emit, so this command takes their whole flag set —
	// see registerConstraintFlags. A flag one command has and another lacks is a
	// plan reporting drift the operator never introduced.
	registerLabelFlags(cmd)
	registerConstraintFlags(cmd)
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
	pending, failed := reportSchemaLoad(cmd, outputFormat, noColor, s, "", absSchemaPath, schemaResult)
	if failed {
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Configure the adapter to match the target graph's generation settings.
	opts, err := constraintOptions(cmd)
	if err != nil {
		return err
	}

	// Generate desired constraints. The load's residual warnings fold into
	// whichever result this command renders, so one invocation writes one.
	adapter := adaptern4j.New(opts...)
	desired, constraintResult := adapter.ConstraintsStructured(cmd.Context(), s)
	if constraintResult.HasErrors() {
		renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath),
			cli.MergeResults(pending, constraintResult))
		return &cli.ExitError{Code: cli.ExitValidation}
	}

	// Generate desired indexes (skipped in constraints-only mode so index-annotation
	// errors do not block a constraints-only diff).
	var desiredIndexes []adaptern4j.Index
	if indexesEnabled {
		di, indexResult := adapter.IndexesStructured(cmd.Context(), s)
		if indexResult.HasErrors() {
			renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath),
				cli.MergeResults(pending, indexResult))
			return &cli.ExitError{Code: cli.ExitValidation}
		}
		desiredIndexes = di
	}

	// Nothing downstream reports through diag, so the load's residuals render
	// here — before the diff output, and exactly once.
	renderDiagnostics(cmd, outputFormat, noColor, s, diagRootFor(s, "", absSchemaPath), pending)

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
	if n := untypedRemoteObjects(len(actual), func(i int) string { return actual[i].Type }); n > 0 {
		reportUnreadableProjection(os.Stderr, n, len(actual), "constraint", "SHOW CONSTRAINTS")
		return &cli.ExitError{Code: cli.ExitRuntime}
	}

	// Fetch actual indexes BEFORE diffing constraints, and do it whatever
	// --indexes says: index and constraint names share one namespace, so the
	// CONSTRAINT diff needs the index names to know whether a CREATE CONSTRAINT
	// would silently no-op. --indexes=false opts out of comparing indexes, not
	// out of classifying constraints accurately — reading it as the latter makes
	// the same declaration print as an unfixable Create under one flag and
	// actionable drift under the other. On a server that cannot report indexes
	// the diff degrades rather than discarding the constraint diff, but it says
	// so, and does not exit 0 when indexes were asked for.
	indexes := indexDiffSkipped
	var actualIndexes []adaptern4j.RemoteIndex
	indexFetchFailed := false
	indexRecords, indexErr := cli.RunQuery(ctx, driver, database, adaptern4j.IntrospectIndexesQuery(), nil)
	switch {
	case indexErr != nil:
		fmt.Fprintf(os.Stderr, "warning: indexes could NOT be read (%v); a constraint whose name an index holds is reported as a create that will not take effect\n", indexErr)
		indexFetchFailed = true
	default:
		parsed, parseErr := adaptern4j.ParseRemoteIndexes(indexRecords)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "warning: indexes could NOT be read (parse indexes: %v); a constraint whose name an index holds is reported as a create that will not take effect\n", parseErr)
			indexFetchFailed = true
			break
		}
		if n := untypedRemoteObjects(len(parsed), func(i int) string { return parsed[i].Type }); n > 0 {
			reportUnreadableProjection(os.Stderr, n, len(parsed), "index", "SHOW INDEXES")
			return &cli.ExitError{Code: cli.ExitRuntime}
		}
		actualIndexes = parsed
	}
	if indexesEnabled && indexFetchFailed {
		indexes = indexDiffUnavailable
	}

	// Diff constraints
	// The label set is computed once from the schema and scopes both halves of
	// the diff, so ownership is exact membership rather than a guess parsed back
	// out of each remote object's label.
	owned := adapter.OwnedLabels(cmd.Context(), s)

	diff := adapter.DiffConstraints(desired, actual, owned, actualIndexes...)

	// Print constraint diff result
	w := cmd.OutOrStdout()
	printDiffResult(w, diff)

	// A schema-owned remote index with no declaration surfaces as a drop. The
	// remote constraints go the other way across the shared name namespace: a
	// NOT NULL or TYPE constraint has no backing index, so it appears in no
	// SHOW INDEXES row and only reaches the index diff this way.
	if indexesEnabled && indexes != indexDiffUnavailable {
		indexDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, owned, actual...)
		printIndexDiffResult(w, indexDiff, owned)
		switch {
		case len(indexDiff.Drift) > 0 || len(indexDiff.Create) > 0 || len(indexDiff.Drop) > 0:
			indexes = indexDiffDrifted
		case len(indexDiff.Unverified) > 0:
			indexes = indexDiffUnverified
		default:
			indexes = indexDiffClean
		}
	}

	constraintDrift := len(diff.Drift) > 0 || len(diff.Create) > 0 || len(diff.Drop) > 0
	if code := neo4jDiffExit(constraintDrift, len(diff.Unverified) > 0, indexes); code != cli.ExitOK {
		return &cli.ExitError{Code: code}
	}
	return nil
}

// untypedRemoteObjects counts parsed remote objects that carry a name but no
// type, reading each object's type through typeAt.
//
// A named object with no type is not something a server produces: the parsers
// reject a record with no name, so a record that got that far and still has an
// empty type means the TYPE COLUMN did not arrive — a renamed or absent column
// in the projection this command issued. That is the one unambiguous symptom of
// the introspection query having gone stale against the server.
//
// It is checked here rather than in the parsers because the parsers are a public
// surface that accepts records from any source, including a caller projecting
// fewer columns on purpose; only this command knows it asked for the full
// projection and is therefore entitled to treat a missing column as broken.
//
// A count of EXCLUDED objects would be the wrong signal despite looking sharper.
// The diffs fold "not on a label this schema owns" and "of a kind this
// configuration cannot declare" into one Excluded counter, so an all-excluded
// comparison is also what a first provision against a database shared with other
// applications looks like — healthy, and every declaration a legitimate create.
func untypedRemoteObjects(n int, typeAt func(int) string) int {
	untyped := 0
	for i := range n {
		if typeAt(i) == "" {
			untyped++
		}
	}
	return untyped
}

// reportUnreadableProjection explains an unreadable type column and what it
// costs, on stderr. The caller exits ExitRuntime: every such object is
// unclassifiable, so the comparison silently shrinks to the ones that did parse
// and would otherwise print a confident plan built from a partial reading —
// the same "a comparison that never ran must not report success" rule the
// unverified and failed-introspection paths already follow.
func reportUnreadableProjection(w io.Writer, untyped, total int, object, query string) {
	fmt.Fprintf(w,
		"error: %d of %d %s record(s) came back with no type; the %s projection this command issues did not return a readable 'type' column\n",
		untyped, total, object, query)
	fmt.Fprintf(w,
		"       every such %s is unclassifiable, so no comparison is reported rather than one built from a partial reading; this usually means the server is newer than this yammm build\n",
		object)
}

// indexDiffOutcome records what the index half of a diff produced, so the exit
// code can tell "compared, found drift" apart from "never compared".
type indexDiffOutcome int

const (
	indexDiffSkipped     indexDiffOutcome = iota // --indexes=false: the caller opted out
	indexDiffClean                               // compared; every index verified in sync
	indexDiffDrifted                             // compared; drift, creates, or drops found
	indexDiffUnverified                          // compared, but an index's definition could not be checked
	indexDiffUnavailable                         // requested, but index introspection failed
)

// neo4jDiffExit maps the two halves of the diff onto an exit code.
//
// Drift wins: it is the actionable signal, and a caller that sees exit 1 goes
// looking at the printed diff either way. The incomplete-comparison outcomes are
// the interesting case — none may exit 0. Degrading to constraints-only on a
// failed introspection is right (the constraint diff already printed is complete
// and worth keeping), and so is reporting an object whose definition the server
// would not disclose; but in each the caller asked for drift and did not get it,
// so reporting success would let a drift gate read "no drift" from a comparison
// that never ran. That reasoning is not specific to indexes: a TYPE constraint
// on a server too old to report its property type is unverified in exactly the
// same sense, and is treated the same. Opting out with --indexes=false is a
// different thing entirely: nothing was asked for, so nothing is owed.
func neo4jDiffExit(constraintDrift, constraintUnverified bool, indexes indexDiffOutcome) int {
	if constraintDrift || indexes == indexDiffDrifted {
		return cli.ExitValidation
	}
	if constraintUnverified || indexes == indexDiffUnavailable || indexes == indexDiffUnverified {
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
//
// excluded is reported on its own line rather than folded into the total: the
// five buckets are what the diff compared, and objects it could not compare do
// not belong in that count. Naming them is what stops "0 to drop" from reading
// as "the database is accounted for" — a type deleted since the last apply
// leaves objects that land here and nowhere else.
func printDiffSummary(w io.Writer, prefix, noun string, match, drift, create, drop, unverified, excluded int) {
	total := match + drift + create + drop + unverified
	fmt.Fprintf(w, "\n%ssummary: %d matched, %d drifted, %d to create, %d to drop, %d unverified (total: %d)\n",
		prefix, match, drift, create, drop, unverified, total)
	if excluded > 0 {
		fmt.Fprintf(w, "%snote: %d database %s were not compared — on a label this schema does not declare, or of a kind it cannot express\n",
			prefix, excluded, noun)
	}
}

// constraintLabel identifies a constraint in report output. Names are suppressed
// under --named=false, where printing Name alone yields a blank identifier and a
// list of drift lines an operator cannot tell apart; the label and members
// always identify it.
func constraintLabel(c adaptern4j.Constraint) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Label + "(" + strings.Join(c.Properties, ", ") + ")"
}

// ownedLabelOf reports which of a remote object's labels the schema owns — the
// same label the diff scoped it by.
//
// Its only caller passes [adaptern4j.IndexDiffResult].Drop entries, every one of
// which the diff admitted through ownership, so the lookup succeeds. The
// fallback exists because IndexDiffResult is an exported type a consumer may
// build or filter itself: naming every label the object carries beats printing
// a blank where an identifier belongs.
func ownedLabelOf(owned *adaptern4j.OwnedLabels, labels []string) string {
	if l, ok := owned.LabelOf(labels); ok {
		return l
	}
	return strings.Join(labels, "|")
}

func printDiffResult(w io.Writer, diff *adaptern4j.ConstraintDiffResult) {
	printMatchedLine(w, "constraints", len(diff.Match))

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "drift: %s%s (%s)\n", constraintLabel(d.Desired), remoteSuffix(d.Actual.Name, constraintLabel(d.Desired)), d.Reason)
	}

	for _, c := range diff.Create {
		fmt.Fprintf(w, "create: %s\n", c.Statement)
	}

	for _, d := range diff.Drop {
		fmt.Fprintf(w, "drop: %s (name: %s)\n", d.CreateStatement, d.Name)
	}

	// Unverified constraints exist but could not be fully compared — listed
	// separately from matches so an unchecked constraint is never read as in
	// sync, and counted as their own bucket in the summary.
	for _, u := range diff.Unverified {
		fmt.Fprintf(w, "unverified: %s (%s)\n", constraintLabel(u.Desired), u.Reason)
	}

	printDiffSummary(w, "", "constraint(s)",
		len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop), len(diff.Unverified), diff.Excluded)

	if n := len(diff.Unverified); n > 0 {
		fmt.Fprintf(w, "note: %d constraint(s) could not be fully verified\n", n)
	}
}

func printIndexDiffResult(w io.Writer, diff *adaptern4j.IndexDiffResult, owned *adaptern4j.OwnedLabels) {
	printMatchedLine(w, "indexes", len(diff.Match))

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "index drift: %s%s (%s)\n", d.Desired.Name, remoteSuffix(d.Actual.Name, d.Desired.Name), d.Reason)
	}

	for _, c := range diff.Create {
		fmt.Fprintf(w, "index create: %s\n", c.Statement)
	}

	for _, d := range diff.Drop {
		fmt.Fprintf(w, "index drop: %s (%s on %s)\n", d.Name, d.Type, ownedLabelOf(owned, d.LabelsOrTypes))
	}

	// Unverified indexes exist but could not be fully compared. They are listed
	// separately from matches so an unchecked index is never read as in sync, and
	// counted as their own bucket in the summary.
	for _, u := range diff.Unverified {
		fmt.Fprintf(w, "index unverified: %s (%s)\n", u.Desired.Name, u.Reason)
	}

	printDiffSummary(w, "index ", "index(es)",
		len(diff.Match), len(diff.Drift), len(diff.Create), len(diff.Drop), len(diff.Unverified), diff.Excluded)

	if n := len(diff.Unverified); n > 0 {
		fmt.Fprintf(w, "index note: %d index(es) could not be fully verified\n", n)
	}
}

// remoteSuffix names the database object a drift line refers to, when its name
// differs from the desired object's. Pairing matches on identity as well as
// name, so a drifted object frequently carries an operator's own name — and it
// appears in no other line of the report, leaving nothing to act on.
func remoteSuffix(remote, desired string) string {
	if remote == "" || remote == desired {
		return ""
	}
	return " [database: " + remote + "]"
}

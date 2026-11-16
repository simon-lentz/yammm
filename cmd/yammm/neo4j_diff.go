package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/neo4j"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

func newNeo4jDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <schema.yammm>",
		Short: "Compare schema constraints and indexes against a live Neo4j database",
		Long: "Performs a semantic five-way diff between desired schema constraints and indexes and their actual database state:\n" +
			"matched, drifted, to-create, to-drop, and unverified (present, but the server did not report enough to compare it, or an index still populating).\n\n" +
			"A plan with any drift, create or drop, in either half, exits 1, and outranks an unverifiable definition, " +
			"which exits 3; a database whose constraints or indexes cannot be read exits 3; nothing is applied. " +
			"An undeclared index on a schema-owned label can be planned for a drop — declare it with @index/@@index/@vector/@fulltext/@@fulltext to keep it.",
		Args: cobra.ExactArgs(1),
		RunE: withDiagnostics(runNeo4jDiff),
	}

	// The desired side of the diff is exactly what `yammm neo4j constraints` and
	// `yammm neo4j indexes` emit, so this command takes their whole flag set —
	// see registerConstraintFlags. A flag one command has and another lacks is a
	// plan reporting drift the operator never introduced.
	registerConnectionFlags(cmd)
	registerLabelFlags(cmd)
	registerConstraintFlags(cmd)

	registerModuleRootFlag(cmd)
	return cmd
}

func runNeo4jDiff(cmd *cobra.Command, args []string, sink *cli.DiagnosticSink) error {
	uri, _ := cmd.Flags().GetString("uri")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	database, _ := cmd.Flags().GetString("database")

	if uri == "" {
		return cli.Usagef("--uri is required (or set YAMMM_NEO4J_URI)")
	}
	c, err := constraintConfig(cmd)
	if err != nil {
		return err
	}

	schemaPath := args[0]
	absSchemaPath, err := schemaOperand(schemaPath)
	if err != nil {
		return err
	}

	// Load schema
	moduleRoot, loadOpts, err := moduleRootOptions(cmd, sink)
	if err != nil {
		return err
	}
	s, schemaResult := schema.Load(cmd.Context(), absSchemaPath, loadOpts...)
	if err := reportSchemaLoad(sink, s, moduleRoot, absSchemaPath, schemaResult); err != nil {
		return err
	}

	// The desired side is judged before any connection, so a schema the
	// generators refuse is reported without a server.
	if _, result := c.Constraints(s); result.HasErrors() {
		sink.Add(result)
		return cli.Exit(cli.ExitForResult(result))
	}
	if _, result := c.Indexes(s); result.HasErrors() {
		sink.Add(result)
		return cli.Exit(cli.ExitForResult(result))
	}

	// Connect to database
	ctx := cmd.Context()
	driver, err := cli.ConnectNeo4j(ctx, uri, username, password)
	if err != nil {
		return err
	}
	defer driver.Close(ctx)

	state, err := fetchRemoteState(ctx, cli.DriverQueries(driver), database)
	if err != nil {
		return err
	}
	plan, result := neo4j.Diff(c, s, state.constraints, state.indexes)
	if result.HasErrors() {
		sink.Add(result)
		return cli.Exit(cli.ExitForResult(result))
	}
	sink.Flush()

	// Diff scopes the plan by its own owned set; this one names an index Drop's
	// owned label in the report, by membership rather than a guess parsed back
	// out of the remote object's label.
	reportPlan(cmd.OutOrStdout(), plan, c.OwnedLabels(s))
	return cli.Exit(neo4jDiffExit(plan))
}

// reportPlan writes both halves of plan, the constraints' first.
func reportPlan(w io.Writer, plan neo4j.Plan, owned *cypher.OwnedLabels) {
	printConstraintDiff(w, &plan.Constraints)
	printIndexDiff(w, &plan.Indexes, owned)
}

// remoteState is the database side of a diff: the constraints and the indexes
// it reported.
type remoteState struct {
	constraints []neo4j.RemoteConstraint
	indexes     []neo4j.RemoteIndex
}

// fetchRemoteState reads both halves of the database side through run.
//
// It takes the runner rather than a driver so both queries, both parsers and
// the projection check are reachable without a server — the absence that left
// this whole command unasserted past its --uri guard.
func fetchRemoteState(ctx context.Context, run cli.QueryRunner, database string) (remoteState, error) {
	records, err := run(ctx, database, neo4j.ShowConstraints)
	if err != nil {
		return remoteState{}, cli.Runtimef("fetch constraints: %v", err)
	}

	constraints, err := neo4j.ParseConstraints(records)
	if err != nil {
		return remoteState{}, cli.Runtimef("parse constraints: %v", err)
	}
	if n := untypedRemoteObjects(len(constraints), func(i int) string { return constraints[i].Type }); n > 0 {
		return remoteState{}, unreadableProjection(n, len(constraints), "constraint", "SHOW CONSTRAINTS")
	}

	// Index and constraint names share one namespace, so the plan's CONSTRAINT
	// half needs the index rows to know whether a CREATE CONSTRAINT would be
	// refused. A diff without them is no diff, so a failed read fails the
	// command as a failed constraint read does.
	indexRecords, err := run(ctx, database, neo4j.ShowIndexes)
	if err != nil {
		return remoteState{}, cli.Runtimef("fetch indexes: %v", err)
	}
	indexes, err := neo4j.ParseIndexes(indexRecords)
	if err != nil {
		return remoteState{}, cli.Runtimef("parse indexes: %v", err)
	}
	if n := untypedRemoteObjects(len(indexes), func(i int) string { return indexes[i].Type }); n > 0 {
		return remoteState{}, unreadableProjection(n, len(indexes), "index", "SHOW INDEXES")
	}
	return remoteState{constraints: constraints, indexes: indexes}, nil
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
// Each half of the plan folds "not on a label this schema owns" and "of a kind
// this configuration cannot declare" into one Excluded counter, so an all-excluded
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

// unreadableProjection explains an unreadable type column and what it costs.
//
// It is ExitRuntime because every such object is unclassifiable: the comparison
// silently shrinks to the ones that did parse and would otherwise print a
// confident plan built from a partial reading — the same "a comparison that
// never ran must not report success" rule the unverified and
// failed-introspection paths already follow.
//
// The message is two lines, each printed with its own "error: " prefix.
func unreadableProjection(untyped, total int, object, query string) error {
	return cli.Runtimef(
		"%d of %d %s record(s) came back with no type; the %s projection this command issues did not return a readable 'type' column\n"+
			"every such %s is unclassifiable, so no comparison is reported rather than one built from a partial reading; this usually means the server is newer than this yammm build",
		untyped, total, object, query, object)
}

// neo4jDiffExit maps the plan onto an exit code, reading both halves.
//
// Drift wins: it is the actionable signal, and a caller that sees exit 1 goes
// looking at the printed diff either way. An unverified object — one whose
// definition the server would not disclose, such as a TYPE constraint on a
// server too old to report its property type — must not exit 0: the caller
// asked for drift and did not get it, so reporting success would let a drift
// gate read "no drift" from a comparison that never ran.
func neo4jDiffExit(plan neo4j.Plan) int {
	cs, is := plan.Constraints, plan.Indexes
	if len(cs.Drift)+len(cs.Create)+len(cs.Drop)+len(is.Drift)+len(is.Create)+len(is.Drop) > 0 {
		return cli.ExitValidation
	}
	if len(cs.Unverified)+len(is.Unverified) > 0 {
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
// leaves objects that land here, or on a blocker line where one holds a
// declared name.
func printDiffSummary(w io.Writer, prefix, noun string, match, drift, create, drop, unverified, excluded int) {
	total := match + drift + create + drop + unverified
	fmt.Fprintf(w, "\n%ssummary: %d matched, %d drifted, %d to create, %d to drop, %d unverified (total: %d)\n",
		prefix, match, drift, create, drop, unverified, total)
	if excluded > 0 {
		fmt.Fprintf(w, "%snote: %d database %s were not compared — on a label this schema does not declare, or of a kind it cannot express\n",
			prefix, excluded, noun)
	}
}

// constraintLabel identifies a constraint in report output. A hand-built
// constraint can have no name, where printing Name alone yields a blank
// identifier and a list of drift lines an operator cannot tell apart; the label
// and members always identify it.
func constraintLabel(c cypher.Constraint) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Label + "(" + strings.Join(c.Properties, ", ") + ")"
}

// ownedLabelOf reports which of a remote object's labels the schema owns — the
// same label the diff scoped it by.
//
// Its only caller passes [neo4j.IndexDiff].Drop entries, every one of
// which the diff admitted through ownership, so the lookup succeeds. The
// fallback exists because IndexDiff is an exported type a consumer may
// build or filter itself: naming every label the object carries beats printing
// a blank where an identifier belongs.
func ownedLabelOf(owned *cypher.OwnedLabels, labels []string) string {
	if l, ok := owned.LabelOf(labels); ok {
		return l
	}
	return strings.Join(labels, "|")
}

func printConstraintDiff(w io.Writer, diff *neo4j.ConstraintDiff) {
	printMatchedLine(w, "constraints", len(diff.Match))

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "drift: %s (%s)\n", constraintLabel(d.Desired), d.Reason)
		printBlockers(w, "  blocker: ", d.Blockers)
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

func printIndexDiff(w io.Writer, diff *neo4j.IndexDiff, owned *cypher.OwnedLabels) {
	printMatchedLine(w, "indexes", len(diff.Match))

	for _, d := range diff.Drift {
		fmt.Fprintf(w, "index drift: %s (%s)\n", d.Desired.Name, d.Reason)
		printBlockers(w, "  index blocker: ", d.Blockers)
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

// printBlockers writes one line per object a drift's create needs dropped, the
// constraints first, and for a constraint's backing index the constraint to drop.
// Pairing matches on identity as well as name, so a blocker often carries an
// operator's own name and appears in no other line of the report.
func printBlockers(w io.Writer, prefix string, b neo4j.Blockers) {
	for _, c := range b.Constraints {
		fmt.Fprintf(w, "%s%s %q on %s(%s)\n", prefix, blockerKind(c.Type, "constraint"), c.Name,
			strings.Join(c.LabelsOrTypes, "|"), strings.Join(c.Properties, ", "))
	}
	for _, i := range b.Indexes {
		line := fmt.Sprintf("%s%s %q on %s(%s)", prefix, blockerKind(i.Type, "index"), i.Name,
			strings.Join(i.LabelsOrTypes, "|"), strings.Join(i.Properties, ", "))
		if i.OwningConstraint != "" {
			line += fmt.Sprintf(", which backs constraint %q: drop the constraint", i.OwningConstraint)
		}
		fmt.Fprintln(w, line)
	}
}

func blockerKind(typ, noun string) string {
	if typ == "" {
		return noun
	}
	return typ + " " + noun
}

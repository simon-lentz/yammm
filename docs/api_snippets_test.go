package docs_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// The Go snippets in API.md are prose: nothing compiles them, so a signature
// change leaves them stating a call that no longer exists, and the next reader
// copies it. TestSpecExamples does not cover them — it reads SPEC.md only, it
// extracts ```yammm fences only, and it checks with the DSL parser, which has
// nothing to say about Go.
//
// These tests close that gap for the neo4j diff and introspection surface,
// whose call shapes appear in three places that must agree: docs/API.md
// ("Constraint Diffing", "Index Diffing", "Introspection Queries"), the package
// documentation in adapter/neo4j/doc.go ("Diff Scope and Name Blocking"), and
// claude-plugin/skills/yammm/references/adapters.md ("Diffing Against a Live
// Database"). A change here that breaks compilation is the signal to update all
// three.

// probeSchema is the smallest schema exercising both diff halves: a primary key
// (which emits constraints) and an annotated property (which emits an index).
const probeSchema = `schema "Probe"

type Document {
  content_hash String primary
  state        String @index
}
`

func loadProbeSchema(t *testing.T, ctx context.Context) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(ctx, probeSchema, "probe.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("loading probe schema: %v", err)
	}
	return s
}

// TestDocumentedDiffCallShapes pins the diff call shapes the documentation
// shows: ownership computed once via OwnedLabels and passed to both halves,
// each half taking the other's remote objects as the variadic blocker list, and
// six result buckets on each. It is a compile-time assertion first — the
// runtime assertions only guard against the entry points silently returning
// nothing to classify.
func TestDocumentedDiffCallShapes(t *testing.T) {
	ctx := context.Background()
	s := loadProbeSchema(t, ctx)
	adapter := neo4j.New()

	desiredConstraints, cResult := adapter.ConstraintsStructured(ctx, s)
	if err := cResult.Err(); err != nil {
		t.Fatalf("generating constraints: %v", err)
	}
	desiredIndexes, iResult := adapter.IndexesStructured(ctx, s)
	if err := iResult.Err(); err != nil {
		t.Fatalf("generating indexes: %v", err)
	}

	// An empty database: every declaration is a create, which is enough to
	// exercise the signatures and the bucket names.
	var actualConstraints []neo4j.RemoteConstraint
	var actualIndexes []neo4j.RemoteIndex

	owned := adapter.OwnedLabels(ctx, s)

	cDiff := adapter.DiffConstraints(desiredConstraints, actualConstraints, owned, actualIndexes...)
	iDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, owned, actualConstraints...)

	// Every bucket the documentation names, on both results.
	cBuckets := len(cDiff.Match) + len(cDiff.Drift) + len(cDiff.Create) + len(cDiff.Drop) +
		len(cDiff.Unverified) + cDiff.Excluded
	iBuckets := len(iDiff.Match) + len(iDiff.Drift) + len(iDiff.Create) + len(iDiff.Drop) +
		len(iDiff.Unverified) + iDiff.Excluded

	if len(desiredConstraints) == 0 || cBuckets == 0 {
		t.Errorf("probe schema classified no constraints: %d desired, %d bucketed",
			len(desiredConstraints), cBuckets)
	}
	if len(desiredIndexes) == 0 || iBuckets == 0 {
		t.Errorf("probe schema classified no indexes: %d desired, %d bucketed",
			len(desiredIndexes), iBuckets)
	}

	// The in-sync predicate API.md and adapters.md both show. Against an empty
	// database it must be false — a gate that reads "in sync" here would report
	// an unprovisioned database as provisioned.
	inSync := len(iDiff.Drift) == 0 && len(iDiff.Create) == 0 && len(iDiff.Drop) == 0 &&
		len(iDiff.Unverified) == 0
	if inSync {
		t.Error("in-sync predicate reported an empty database as in sync")
	}

	// OwnedLabels accessors, which a caller reporting on a drop or drift needs
	// to resolve a remote object's labels the way the diff did.
	if _, ok := owned.LabelOf([]string{"Probe__Document"}); !ok {
		t.Error("OwnedLabels does not own the label the adapter emits for Document")
	}
	if !owned.Contains("Probe__Document") {
		t.Error("OwnedLabels.Contains disagrees with LabelOf")
	}
}

// TestDocumentedRemoteIndexSurface pins the RemoteIndex fields and accessors
// API.md's "Introspection Queries" section lists.
func TestDocumentedRemoteIndexSurface(t *testing.T) {
	ri := neo4j.RemoteIndex{
		Name:             "Probe__Document_state_idx",
		Type:             "RANGE",
		EntityType:       "NODE",
		LabelsOrTypes:    []string{"Probe__Document"},
		Properties:       []string{"state"},
		Options:          nil,
		State:            "",
		OwningConstraint: "",
	}

	// An unreported state counts as online, so older servers and hand-built
	// records do not read as drift.
	if !ri.IsOnline() {
		t.Error("IsOnline is false for an unreported state")
	}
	if _, ok := ri.VectorDimensions(); ok {
		t.Error("VectorDimensions reported a dimension for a range index")
	}
	if _, ok := ri.VectorSimilarity(); ok {
		t.Error("VectorSimilarity reported a function for a range index")
	}
}

// TestDocumentedIntrospectionQueries pins the introspection queries against the
// descriptions in API.md's "Introspection Queries" table. The index query's
// exact projection is documented because a caller running it against a server
// that does not support a column fails, so the column list is a contract.
func TestDocumentedIntrospectionQueries(t *testing.T) {
	if got, want := neo4j.IntrospectConstraintsQuery(), "SHOW CONSTRAINTS YIELD *"; got != want {
		t.Errorf("IntrospectConstraintsQuery() = %q, documented as %q", got, want)
	}

	indexQuery := neo4j.IntrospectIndexesQuery()
	for _, column := range []string{
		"name", "type", "entityType", "labelsOrTypes",
		"properties", "options", "state", "owningConstraint",
	} {
		if !strings.Contains(indexQuery, column) {
			t.Errorf("IntrospectIndexesQuery() omits the documented column %q: %s", column, indexQuery)
		}
	}
	// Constraint-backing indexes are deliberately returned; only LOOKUP indexes
	// are filtered out. A reinstated owningConstraint filter would silently
	// remove the rows the diff needs to detect a blocked CREATE INDEX.
	if !strings.Contains(indexQuery, "type <> 'LOOKUP'") {
		t.Errorf("IntrospectIndexesQuery() does not filter LOOKUP indexes: %s", indexQuery)
	}
	if strings.Contains(indexQuery, "owningConstraint IS NULL") {
		t.Errorf("IntrospectIndexesQuery() filters out constraint-backing indexes, "+
			"which the documented diff behavior depends on receiving: %s", indexQuery)
	}
}

//go:build neo4j_integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	n4j "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// newAdapter builds the adapter under test with its edition matched to the
// server's.
//
// Both editions are configurations the library ships, and the edition gating
// exists precisely so a Community server is a supported target. Pinning
// Enterprise here instead would make every caller Enterprise-only — including
// the index, introspection and round-trip tests, whose claims hold on Community
// — and take the documented Community mode out of the suite entirely.
//
// An explicit WithEdition in opts still wins: it is appended after, so a test
// that means to exercise a specific edition against this server can.
func newAdapter(t *testing.T, ctx context.Context, opts ...n4j.Option) *n4j.Adapter {
	t.Helper()
	edition := n4j.Enterprise
	if !isEnterprise(t, ctx) {
		edition = n4j.Community
	}
	return n4j.New(append([]n4j.Option{n4j.WithEdition(edition)}, opts...)...)
}

func loadRoundTripSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.Load(context.Background(), filepath.Join("testdata", "roundtrip.yammm"))
	if res.HasErrors() {
		t.Fatalf("loading the round-trip schema: %v", res.Err())
	}
	return s
}

// applySchemaDDL emits and executes every constraint and index the schema
// declares, then waits for population to finish.
//
// It applies whatever the ADAPTER emits, so an adapter built by [newAdapter]
// against a Community server emits UNIQUE alone and every statement executes.
// Gating the whole helper on Enterprise instead would skip every caller,
// including the index, introspection and round-trip tests whose claims hold on
// both editions.
func applySchemaDDL(t *testing.T, ctx context.Context, a *n4j.Adapter, s *schema.Schema) {
	t.Helper()
	constraints, cRes := a.ConstraintsForSchema(ctx, s)
	if cRes.HasErrors() {
		t.Fatalf("emitting constraints: %v", cRes.Err())
	}
	indexes, iRes := a.IndexesForSchema(ctx, s)
	if iRes.HasErrors() {
		t.Fatalf("emitting indexes: %v", iRes.Err())
	}
	for _, stmt := range append(append([]string{}, constraints...), indexes...) {
		run(t, ctx, stmt)
	}
	awaitIndexes(t, ctx)
}

// Every statement the adapter emits must execute. This is the claim no unit
// test can make: the fixtures pin the string, the server decides whether it is
// Cypher.
func TestEmittedDDLExecutes(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	// The CREATE VECTOR INDEX ... OPTIONS form is the one with a stated version
	// floor, so confirm the server really did create a VECTOR index.
	var sawVector bool
	for _, rec := range query(t, ctx, "SHOW INDEXES YIELD name, type") {
		if typ, _ := rec["type"].(string); typ == "VECTOR" {
			sawVector = true
		}
	}
	if !sawVector {
		t.Error("no VECTOR index exists after applying the schema's DDL")
	}
}

// Applying the schema's own DDL and then diffing against the database must
// report everything as matched. This exercises ownership, identity, and pairing
// end to end against real introspection output — if any of the three disagrees
// with what the server reports, it shows up here as a spurious create or drop.
func TestRoundTripDiffIsClean(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	owned := a.OwnedLabels(ctx, s)

	desiredConstraints, _ := a.ConstraintsStructured(ctx, s)
	actualConstraints, err := n4j.ParseRemoteConstraints(query(t, ctx, n4j.IntrospectConstraintsQuery()))
	if err != nil {
		t.Fatalf("parsing constraints: %v", err)
	}
	cd := a.DiffConstraints(desiredConstraints, actualConstraints, owned)
	if len(cd.Match) != len(desiredConstraints) {
		t.Errorf("constraints: matched %d of %d", len(cd.Match), len(desiredConstraints))
	}
	for _, unwanted := range []struct {
		name string
		n    int
	}{
		{"drift", len(cd.Drift)},
		{"create", len(cd.Create)},
		{"drop", len(cd.Drop)},
		{"unverified", len(cd.Unverified)},
	} {
		if unwanted.n != 0 {
			t.Errorf("constraints: %d %s, want 0", unwanted.n, unwanted.name)
		}
	}
	for _, d := range cd.Drift {
		t.Logf("  constraint drift: %s", d.Reason)
	}
	for _, c := range cd.Create {
		t.Logf("  constraint create: %s", c.Statement)
	}
	for _, d := range cd.Drop {
		t.Logf("  constraint drop: %s", d.Name)
	}

	desiredIndexes, _ := a.IndexesStructured(ctx, s)
	actualIndexes, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("parsing indexes: %v", err)
	}
	id := a.DiffIndexes(desiredIndexes, actualIndexes, owned)
	if len(id.Match) != len(desiredIndexes) {
		t.Errorf("indexes: matched %d of %d", len(id.Match), len(desiredIndexes))
	}
	for _, unwanted := range []struct {
		name string
		n    int
	}{
		{"drift", len(id.Drift)},
		{"create", len(id.Create)},
		{"drop", len(id.Drop)},
		{"unverified", len(id.Unverified)},
	} {
		if unwanted.n != 0 {
			t.Errorf("indexes: %d %s, want 0", unwanted.n, unwanted.name)
		}
	}
	for _, d := range id.Drift {
		t.Logf("  index drift: %s", d.Reason)
	}
	for _, c := range id.Create {
		t.Logf("  index create: %s", c.Statement)
	}
	for _, d := range id.Drop {
		t.Logf("  index drop: %s (%s)", d.Name, d.Type)
	}
	for _, u := range id.Unverified {
		t.Logf("  index unverified: %s", u.Reason)
	}
}

// The introspection query must parse, and every column the parser reads must be
// present with the Go type it type-asserts on. A wrong column name or an
// unexpected type degrades silently: the field reads as its zero value and the
// diff quietly stops comparing on it.
func TestIntrospectIndexesQuery_ShapeIsAsParsed(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	records := query(t, ctx, n4j.IntrospectIndexesQuery())
	if len(records) == 0 {
		t.Fatal("the introspection query returned no rows for a schema that just created indexes")
	}

	for _, rec := range records {
		for _, col := range []string{"name", "type", "entityType", "labelsOrTypes", "properties", "options", "state", "owningConstraint"} {
			if _, present := rec[col]; !present {
				t.Errorf("column %q is absent from the yielded record; the query and the parser disagree", col)
			}
		}
		assertType[string](t, rec, "name")
		assertType[string](t, rec, "type")
		assertType[string](t, rec, "entityType")
		assertType[string](t, rec, "state")
		assertSliceOfString(t, rec, "labelsOrTypes")
		assertSliceOfString(t, rec, "properties")
		if _, ok := rec["options"].(map[string]any); !ok {
			t.Errorf("options is %T, but parseMapField asserts map[string]any", rec["options"])
		}
	}

	// The parser must survive the real records unchanged.
	parsed, err := n4j.ParseRemoteIndexes(records)
	if err != nil {
		t.Fatalf("ParseRemoteIndexes on live records: %v", err)
	}
	if len(parsed) != len(records) {
		t.Errorf("parsed %d of %d live records", len(parsed), len(records))
	}
	for _, ri := range parsed {
		if ri.Name == "" || ri.Type == "" || len(ri.LabelsOrTypes) == 0 || len(ri.Properties) == 0 {
			t.Errorf("a live index parsed with empty fields: %+v", ri)
		}
		if ri.State == "" {
			t.Errorf("index %q parsed with no State; IsOnline() would treat it as online", ri.Name)
		}
	}
}

// The vector configuration the adapter reads must be present, and its values
// must be the Go types and the CASING the code assumes. vectorDrift compares
// the similarity function with EqualFold specifically because the server is
// believed to echo it uppercased — if that belief is wrong in either direction
// the comparison is either needlessly loose or silently broken.
func TestVectorIndexConfig_TypesAndCasing(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}

	var found bool
	for _, ri := range parsed {
		if ri.Type != "VECTOR" {
			continue
		}
		found = true

		dim, ok := ri.VectorDimensions()
		if !ok {
			t.Errorf("index %q: VectorDimensions() unreadable from live options %#v", ri.Name, ri.Options)
		} else if dim != 4 {
			t.Errorf("index %q: dimension = %d, want 4 (the schema's Vector[4])", ri.Name, dim)
		}

		sim, ok := ri.VectorSimilarity()
		if !ok {
			t.Errorf("index %q: VectorSimilarity() unreadable from live options %#v", ri.Name, ri.Options)
			continue
		}
		// The schema side is lowercase; the comparison must survive whatever the
		// server returns. Record the actual casing so a future change is visible.
		t.Logf("server reports vector.similarity_function as %q (schema declares %q)", sim, "cosine")
		if !strings.EqualFold(sim, "cosine") {
			t.Errorf("similarity = %q; want cosine ignoring case", sim)
		}
		if sim == "cosine" {
			t.Logf("NOTE: the server echoed the similarity in the schema's own casing; " +
				"vectorDrift's EqualFold is then belt-and-braces rather than load-bearing")
		}
	}
	if !found {
		t.Fatal("no VECTOR index was introspected")
	}
}

// A constraint's backing index must carry owningConstraint, which is what makes
// declarableRemoteIndex able to keep it out of the index diff. The adapter's own
// introspection query filters these server-side, so the assertion is made
// against an unfiltered SHOW INDEXES — the shape a consumer using `YIELD *`
// hands in.
func TestConstraintBackedIndex_CarriesOwningConstraint(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	records := query(t, ctx,
		"SHOW INDEXES YIELD name, type, entityType, labelsOrTypes, properties, options, state, owningConstraint "+
			"WHERE owningConstraint IS NOT NULL")
	if len(records) == 0 {
		t.Fatal("no constraint-backing index exists; the schema's UNIQUE constraints should have created one")
	}

	parsed, err := n4j.ParseRemoteIndexes(records)
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	for _, ri := range parsed {
		if ri.OwningConstraint == "" {
			t.Errorf("index %q backs a constraint but parsed OwningConstraint empty", ri.Name)
		}
	}

	// Fed in unfiltered, none of them may be reported as an orphan to drop.
	owned := a.OwnedLabels(ctx, s)
	diff := a.DiffIndexes(nil, parsed, owned)
	if len(diff.Drop) != 0 {
		t.Errorf("%d constraint-backing index(es) reported as drops; dropping one removes a primary key's index",
			len(diff.Drop))
	}
}

// The state column must report the values the classification switches on.
func TestIndexState_ReportsOnline(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	parsed, err := n4j.ParseRemoteIndexes(query(t, ctx, n4j.IntrospectIndexesQuery()))
	if err != nil {
		t.Fatalf("ParseRemoteIndexes: %v", err)
	}
	for _, ri := range parsed {
		if !strings.EqualFold(ri.State, "ONLINE") {
			t.Errorf("index %q is in state %q after awaiting population; want ONLINE", ri.Name, ri.State)
		}
		if !ri.IsOnline() {
			t.Errorf("index %q: IsOnline() false for state %q", ri.Name, ri.State)
		}
	}
}

// SHOW CONSTRAINTS must yield the columns ParseRemoteConstraints reads.
func TestIntrospectConstraintsQuery_ShapeIsAsParsed(t *testing.T) {
	ctx := context.Background()
	driver(t)
	dropAll(t, ctx)
	t.Cleanup(func() { dropAll(t, ctx) })

	a := newAdapter(t, ctx)
	s := loadRoundTripSchema(t)
	applySchemaDDL(t, ctx, a, s)

	records := query(t, ctx, n4j.IntrospectConstraintsQuery())
	if len(records) == 0 {
		t.Fatal("no constraints were introspected")
	}
	for _, rec := range records {
		for _, col := range []string{"name", "type", "entityType", "labelsOrTypes", "properties", "createStatement"} {
			if _, present := rec[col]; !present {
				t.Errorf("column %q absent from SHOW CONSTRAINTS YIELD *; ParseRemoteConstraints reads it", col)
			}
		}
	}

	parsed, err := n4j.ParseRemoteConstraints(records)
	if err != nil {
		t.Fatalf("ParseRemoteConstraints on live records: %v", err)
	}
	for _, rc := range parsed {
		if rc.Name == "" || rc.Type == "" || len(rc.LabelsOrTypes) == 0 {
			t.Errorf("a live constraint parsed with empty fields: %+v", rc)
		}
		if rc.CreateStatement == "" {
			t.Errorf("constraint %q parsed with no CreateStatement; the CLI prints it on a drop line", rc.Name)
		}
	}
}

func assertType[T any](t *testing.T, rec map[string]any, col string) {
	t.Helper()
	if _, ok := rec[col].(T); !ok {
		var want T
		t.Errorf("column %q is %T, but the parser asserts %T", col, rec[col], want)
	}
}

func assertSliceOfString(t *testing.T, rec map[string]any, col string) {
	t.Helper()
	raw, ok := rec[col].([]any)
	if !ok {
		t.Errorf("column %q is %T, but parseStringSliceField asserts []any", col, rec[col])
		return
	}
	for i, v := range raw {
		if _, ok := v.(string); !ok {
			t.Errorf("column %q element %d is %T, but parseStringSliceField asserts string", col, i, v)
		}
	}
}

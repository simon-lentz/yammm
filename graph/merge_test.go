package graph_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// mergeSchema has one type listed under several parents: an Issue names its
// issuers through an optional (many) association with an edge property and an
// optional (one), and its pages through a required (many) and a required (one).
func mergeSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "merge"

type Issuer {
	id String primary
	name String
}

type Page {
	id String primary
}

type Issue {
	id String primary
	title String
	--> ISSUED_BY (many) Issuer {
		role String
	}
	--> LISTED_ON (one:many) Page
	--> LEAD (_:one) Issuer
	--> HOME (one) Page
	*-> NOTES (many) Note
}

part type Note {
	id String primary
}
`, "merge.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s
}

// target is one association target of an issue fixture: its key and edge
// properties.
type target struct {
	key   string
	props map[string]any
}

// issue builds an Issue with the given title and association targets, each
// relation present only when edges names it.
func issue(t *testing.T, s schema.Schema, id, title string, edges map[string][]target) instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type("Issue")
	if !ok {
		t.Fatal("no type Issue")
	}
	data := map[string]any{"id": id, "title": title}
	for name, targets := range edges {
		rel, ok := typ.Relation(name)
		if !ok {
			t.Fatalf("no relation %s on Issue", name)
		}
		objects := make([]any, len(targets))
		for i, tg := range targets {
			objects[i] = targetObject(t, s, rel, []any{tg.key}, tg.props)
		}
		if rel.IsMany() {
			data[rel.FieldName()] = objects
			continue
		}
		if len(objects) != 1 {
			t.Fatalf("(one) association %s given %d targets", name, len(objects))
		}
		data[rel.FieldName()] = objects[0]
	}
	return instancetest.Valid(t, s, "Issue", data)
}

// edgesOf renders every edge of snap as `relation->key{name: "value"}`,
// sorted.
func edgesOf(snap graph.Snapshot) []string {
	var out []string
	for e := range snap.Edges() {
		var props []string
		for name := range e.Properties().SortedKeys() {
			v, _ := e.Property(name)
			props = append(props, fmt.Sprintf("%s: %q", name, v.Unwrap()))
		}
		out = append(out, fmt.Sprintf("%s->%s{%s}", e.Relation(), e.Target().PrimaryKey(), strings.Join(props, ", ")))
	}
	slices.Sort(out)
	return out
}

// unresolvedOf renders every unresolved record of snap as
// "relation:reason:key", sorted.
func unresolvedOf(snap graph.Snapshot) []string {
	var out []string
	for u := range snap.Unresolved() {
		out = append(out, u.Relation()+":"+u.Reason()+":"+u.TargetKey())
	}
	slices.Sort(out)
	return out
}

// mustAdd adds each instance through Add and fails on any error.
func mustAdd(t *testing.T, g *graph.Graph, insts ...instance.ValidInstance) {
	t.Helper()
	for _, inst := range insts {
		if res := g.Add(t.Context(), inst); res.HasErrors() {
			t.Fatalf("Add %s: %s", inst.PrimaryKey(), res)
		}
	}
}

// mustMerge merges inst and fails unless it merged cleanly.
func mustMerge(t *testing.T, g *graph.Graph, inst instance.ValidInstance) {
	t.Helper()
	merged, res := g.AddOrMerge(t.Context(), inst)
	if res.HasErrors() || !merged {
		t.Fatalf("merge %s: merged %v, %s", inst.PrimaryKey(), merged, res)
	}
}

// roots returns the Issuer and Page roots every issue fixture may name.
func roots(t *testing.T, s schema.Schema) []instance.ValidInstance {
	t.Helper()
	return []instance.ValidInstance{
		mustValidInstance(t, s, "Issuer", []any{"a"}, map[string]any{"name": "A"}),
		mustValidInstance(t, s, "Issuer", []any{"b"}, map[string]any{"name": "B"}),
		mustValidInstance(t, s, "Page", []any{"p1"}, nil),
		mustValidInstance(t, s, "Page", []any{"p2"}, nil),
	}
}

// required gives an issue the required associations HOME and LISTED_ON at
// page p1, so Check has nothing to report unless a test removes one.
func required() map[string][]target {
	return map[string][]target{"HOME": {{key: "p1"}}, "LISTED_ON": {{key: "p1"}}}
}

// with returns base with rel's targets set.
func with(base map[string][]target, rel string, targets ...target) map[string][]target {
	out := make(map[string][]target, len(base)+1)
	maps.Copy(out, base)
	out[rel] = targets
	return out
}

// A key the graph does not hold is added, as Add adds it.
func TestMerge_AddsAnInstanceTheGraphDoesNotHold(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i1", "first", required()))
	if res.HasErrors() || merged {
		t.Fatalf("merged %v, %s; want an add", merged, res)
	}
	if n := len(slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, s, "Issue")))); n != 1 {
		t.Errorf("%d issues, want 1", n)
	}
}

// A second listing installs its edges on the held issue, carrying their edge
// properties; the held properties stand, and nothing is recorded as a
// duplicate or an error.
func TestMerge_InstallsTheDuplicatesEdgesOnTheHeldInstance(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "held title", with(required(), "ISSUED_BY", target{"a", map[string]any{"role": "lead"}})))
	mustMerge(t, g, issue(t, s, "i1", "other title", with(required(), "ISSUED_BY", target{"b", map[string]any{"role": "co"}})))

	snap := g.Snapshot()
	want := []string{
		"HOME->[\"p1\"]{}",
		"ISSUED_BY->[\"a\"]{role: \"lead\"}",
		"ISSUED_BY->[\"b\"]{role: \"co\"}",
		"LISTED_ON->[\"p1\"]{}",
	}
	if got := edgesOf(snap); !slices.Equal(got, want) {
		t.Errorf("edges %q, want %q", got, want)
	}
	held, _ := snap.InstanceByKey(mustTypeID(t, s, "Issue"), `["i1"]`)
	if title, _ := held.Property("title"); title.Unwrap() != "held title" {
		t.Errorf("title %v; the held instance's properties must stand", title.Unwrap())
	}
	if n := len(slices.Collect(snap.Duplicates())); n != 0 {
		t.Errorf("%d duplicates; a merge records none", n)
	}
}

// A target the held instance already names, by an edge or an unresolved
// record, adds nothing; the held edge's properties stand.
func TestMerge_ATargetAlreadyNamedAddsNothing(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY",
		target{"a", map[string]any{"role": "lead"}}, target{"zz", nil})))
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY",
		target{"a", map[string]any{"role": "other"}}, target{"zz", nil}, target{"b", nil})))

	snap := g.Snapshot()
	want := []string{`HOME->["p1"]{}`, `ISSUED_BY->["a"]{role: "lead"}`, `ISSUED_BY->["b"]{}`, `LISTED_ON->["p1"]{}`}
	if got := edgesOf(snap); !slices.Equal(got, want) {
		t.Errorf("edges %q, want %q", got, want)
	}
	if got := unresolvedOf(snap); !slices.Equal(got, []string{`ISSUED_BY:target_missing:["zz"]`}) {
		t.Errorf("unresolved %q, want the one forward reference", got)
	}
}

// A merge that would give a (one) association a second target is refused
// whole, before anything installs; the same target is not a second one.
func TestMerge_RefusesASecondTargetOnAOneAssociation(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", with(required(), "LEAD", target{"a", nil})))
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "LEAD", target{"a", nil})))

	merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i1", "t", with(with(required(), "LEAD", target{"b", nil}), "ISSUED_BY", target{"a", nil})))
	if merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Fatalf("merged %v, %s; want E_GRAPH_CARDINALITY", merged, res)
	}
	snap := g.Snapshot()
	want := []string{`HOME->["p1"]{}`, `LEAD->["a"]{}`, `LISTED_ON->["p1"]{}`}
	if got := edgesOf(snap); !slices.Equal(got, want) {
		t.Errorf("edges %q, want %q: a refused merge installs nothing", got, want)
	}
}

// A forward reference merges as an unresolved record on the held instance,
// which a later Add of the target resolves.
func TestMerge_CarriesAForwardReference(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY", target{"late", nil})))
	if got := unresolvedOf(g.Snapshot()); !slices.Equal(got, []string{`ISSUED_BY:target_missing:["late"]`}) {
		t.Fatalf("unresolved %q, want the forward reference", got)
	}
	mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{"late"}, map[string]any{"name": "L"}))
	snap := g.Snapshot()
	if len(slices.Collect(snap.Unresolved())) != 0 || !slices.Contains(edgesOf(snap), `ISSUED_BY->["late"]{}`) {
		t.Errorf("edges %q, unresolved %q; the target's Add must resolve the merged reference", edgesOf(snap), unresolvedOf(snap))
	}
}

// A required association's "absent" record is retired when the merge installs
// a record under it, so the record stands alone as the Records fact states,
// the Associations claim holds, and the snapshot reader accepts the result.
func TestMerge_RetiresARequiredAssociationsAbsentRecord(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", map[string][]target{"HOME": {{key: "p1"}}}))
	if got := unresolvedOf(g.Snapshot()); !slices.Equal(got, []string{"LISTED_ON:absent:"}) {
		t.Fatalf("unresolved %q, want LISTED_ON absent", got)
	}
	// The incoming instance lacks HOME: its absent record adds nothing.
	mustMerge(t, g, issue(t, s, "i1", "t", map[string][]target{"LISTED_ON": {{key: "p2"}}}))

	snap := g.Snapshot()
	if got := unresolvedOf(snap); len(got) != 0 {
		t.Errorf("unresolved %q, want none", got)
	}
	if !snap.Attestation().Associations {
		t.Error("Associations claim false after the required record resolved")
	}
	if res := g.Check(t.Context()); !res.OK() {
		t.Errorf("Check: %s", res)
	}
	data, res := snapshot.Marshal(t.Context(), snap)
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	if _, res := snapshot.Load(t.Context(), data, s); res.HasErrors() {
		t.Errorf("the snapshot reader refuses the merged snapshot: %s", res)
	}
}

// An incoming instance carrying composed children is refused as Add refuses a
// duplicate.
func TestMerge_RefusesADuplicateCarryingComposedChildren(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	withNote := parentWith(t, s, "Issue", []any{"i1"}, map[string]any{"title": "t"}, "NOTES", partData(t, s, "Note", []any{"n1"}, nil))
	merged, res := g.AddOrMerge(t.Context(), withNote)
	if merged || !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Fatalf("merged %v, %s; want E_DUPLICATE_PK", merged, res)
	}
	if n := len(slices.Collect(g.Snapshot().Duplicates())); n != 1 {
		t.Errorf("%d duplicates, want 1", n)
	}
}

// A root seeded from a snapshot merges as one added in this graph: the source
// index reads the imported records.
func TestMerge_MergesIntoASeededInstance(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", with(with(required(), "ISSUED_BY", target{"a", nil}), "LEAD", target{"a", nil})))
	seeded := mustImport(t, s, g.Snapshot())
	mustMerge(t, seeded, issue(t, s, "i1", "t", with(required(), "ISSUED_BY", target{"a", nil}, target{"b", nil})))
	if merged, res := seeded.AddOrMerge(t.Context(), issue(t, s, "i1", "t", with(required(), "LEAD", target{"b", nil}))); merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; the imported LEAD edge must count", merged, res)
	}
	want := []string{`HOME->["p1"]{}`, `ISSUED_BY->["a"]{}`, `ISSUED_BY->["b"]{}`, `LEAD->["a"]{}`, `LISTED_ON->["p1"]{}`}
	if got := edgesOf(seeded.Snapshot()); !slices.Equal(got, want) {
		t.Errorf("edges %q, want %q", got, want)
	}
}

// Concurrent callers merging listings of one key into one graph keep every
// listing's edge, record no duplicate, and hold one instance at the key. Run it
// under -race: the graph's own lock is all that guards the merge.
func TestAddOrMerge_ConcurrentListingsKeepEveryEdge(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	const issuers = 40
	mustAdd(t, g, roots(t, s)...)
	for i := range issuers {
		mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{fmt.Sprint("x", i)}, map[string]any{"name": "X"}))
	}
	var wg sync.WaitGroup
	var merges atomic.Int64
	for i := range issuers {
		wg.Go(func() {
			merged, res := g.AddOrMerge(t.Context(), issue(t, s, "shared", "t", with(required(), "ISSUED_BY", target{fmt.Sprint("x", i), nil})))
			if res.HasErrors() {
				t.Error(res)
			}
			if merged {
				merges.Add(1)
			}
		})
	}
	wg.Wait()
	if n := merges.Load(); n != issuers-1 {
		t.Errorf("%d calls merged, want %d: the first listing adds and every other merges", n, issuers-1)
	}
	snap := g.Snapshot()
	if n := len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Issue")))); n != 1 {
		t.Errorf("%d issues, want 1: a merge adds no instance", n)
	}
	issuedBy := 0
	for e := range snap.Edges() {
		if e.Relation() == "ISSUED_BY" {
			issuedBy++
		}
	}
	if issuedBy != issuers || len(slices.Collect(snap.Duplicates())) != 0 {
		t.Errorf("%d ISSUED_BY edges and %d duplicates, want %d and 0", issuedBy, len(slices.Collect(snap.Duplicates())), issuers)
	}
}

// validated returns a validator for s's instances that fails the test on any
// error, so a graph built from it attests Values.
func validated(t *testing.T, s schema.Schema) func(typ string, props map[string]any) instance.ValidInstance {
	t.Helper()
	v := instance.NewValidator(s)
	return func(typ string, props map[string]any) instance.ValidInstance {
		t.Helper()
		out, res := v.ValidateOne(t.Context(), typ, instance.RawInstance{Properties: props})
		if res.HasErrors() {
			t.Fatalf("validate %s: %s", typ, res)
		}
		return out
	}
}

// Records installed after the first merge, by a merge, an Add or a resolution,
// are held as a later merge finds them: a target already named adds nothing,
// and a held (one) target refuses a second.
func TestMerge_FindsRecordsInstalledAfterTheFirstMerge(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i0", "t", required()))
	mustMerge(t, g, issue(t, s, "i0", "t", with(required(), "ISSUED_BY", target{"late", nil})))

	// An Add after the first merge: an edge, a forward reference, a (one) edge.
	mustAdd(t, g, issue(t, s, "i1", "t", with(with(required(), "ISSUED_BY", target{"a", nil}, target{"zz", nil}), "LEAD", target{"a", nil})))
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY", target{"a", nil}, target{"zz", nil})))
	if merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i1", "t", with(required(), "LEAD", target{"b", nil}))); merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; want E_GRAPH_CARDINALITY for a LEAD an Add installed after the first merge", merged, res)
	}
	// A (one) forward reference an Add installed after the first merge.
	mustAdd(t, g, issue(t, s, "i2", "t", with(required(), "LEAD", target{"nobody", nil})))
	if merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i2", "t", with(required(), "LEAD", target{"b", nil}))); merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; want E_GRAPH_CARDINALITY for a pending LEAD an Add installed after the first merge", merged, res)
	}
	// An "absent" record an Add installed after the first merge retires.
	mustAdd(t, g, issue(t, s, "i3", "t", map[string][]target{"HOME": {{key: "p1"}}}))
	mustMerge(t, g, issue(t, s, "i3", "t", map[string][]target{"LISTED_ON": {{key: "p1"}}}))
	// The merged forward reference resolves; merging its target again adds nothing.
	mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{"late"}, map[string]any{"name": "L"}))
	mustMerge(t, g, issue(t, s, "i0", "t", with(required(), "ISSUED_BY", target{"late", nil})))
	// Records a merge installed: repeating them adds nothing, and a merged
	// (one) target refuses a second.
	mustMerge(t, g, issue(t, s, "i0", "t", with(with(required(), "ISSUED_BY", target{"a", nil}), "LEAD", target{"a", nil})))
	mustMerge(t, g, issue(t, s, "i0", "t", with(required(), "ISSUED_BY", target{"a", nil})))
	if merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i0", "t", with(required(), "LEAD", target{"b", nil}))); merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; want E_GRAPH_CARDINALITY for a LEAD a merge installed", merged, res)
	}

	snap := g.Snapshot()
	var got []string
	for e := range snap.Edges() {
		got = append(got, e.Source().PrimaryKey().String()+":"+e.Relation()+"->"+e.Target().PrimaryKey().String())
	}
	slices.Sort(got)
	want := []string{
		`["i0"]:HOME->["p1"]`, `["i0"]:ISSUED_BY->["a"]`, `["i0"]:ISSUED_BY->["late"]`, `["i0"]:LEAD->["a"]`, `["i0"]:LISTED_ON->["p1"]`,
		`["i1"]:HOME->["p1"]`, `["i1"]:ISSUED_BY->["a"]`, `["i1"]:LEAD->["a"]`, `["i1"]:LISTED_ON->["p1"]`,
		`["i2"]:HOME->["p1"]`, `["i2"]:LISTED_ON->["p1"]`,
		`["i3"]:HOME->["p1"]`, `["i3"]:LISTED_ON->["p1"]`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("edges %q, want %q", got, want)
	}
	if got, want := unresolvedOf(snap), []string{`ISSUED_BY:target_missing:["zz"]`, `LEAD:target_missing:["nobody"]`}; !slices.Equal(got, want) {
		t.Errorf("unresolved %q, want %q", got, want)
	}
}

// A (one) association counts a held forward reference as its target, counts
// no "absent" record, and takes a first target into an empty slot.
func TestMerge_CountsAOneAssociationsHeldTarget(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)

	mustAdd(t, g, issue(t, s, "pending", "t", with(required(), "LEAD", target{"zz", nil})))
	if merged, res := g.AddOrMerge(t.Context(), issue(t, s, "pending", "t", with(required(), "LEAD", target{"b", nil}))); merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; a held forward reference is the (one) target", merged, res)
	}
	mustMerge(t, g, issue(t, s, "pending", "t", with(required(), "LEAD", target{"zz", nil})))

	mustAdd(t, g, issue(t, s, "absent", "t", map[string][]target{"LISTED_ON": {{key: "p1"}}}))
	mustMerge(t, g, issue(t, s, "absent", "t", map[string][]target{"HOME": {{key: "p2"}}}))

	mustAdd(t, g, issue(t, s, "empty", "t", required()))
	mustMerge(t, g, issue(t, s, "empty", "t", with(required(), "LEAD", target{"a", nil})))

	snap := g.Snapshot()
	if got, want := unresolvedOf(snap), []string{`LEAD:target_missing:["zz"]`}; !slices.Equal(got, want) {
		t.Errorf("unresolved %q, want %q: the absent HOME retires, and the repeated LEAD adds nothing", got, want)
	}
	var got []string
	for e := range snap.Edges() {
		if e.Relation() == "HOME" || e.Relation() == "LEAD" {
			got = append(got, e.Source().PrimaryKey().String()+":"+e.Relation()+"->"+e.Target().PrimaryKey().String())
		}
	}
	slices.Sort(got)
	want := []string{`["absent"]:HOME->["p2"]`, `["empty"]:HOME->["p1"]`, `["empty"]:LEAD->["a"]`, `["pending"]:HOME->["p1"]`}
	if !slices.Equal(got, want) {
		t.Errorf("HOME and LEAD edges %q, want %q", got, want)
	}
}

// Retiring one of several "absent" or "empty" records filed under one target
// type keeps the others, and the snapshot still reads back.
func TestMerge_RetiresOneRecordOfSeveralSharingATargetType(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", nil))
	mustAdd(t, g, issue(t, s, "i2", "t", nil))
	mustAdd(t, g, issue(t, s, "i3", "t", map[string][]target{"HOME": {{key: "p1"}}, "LISTED_ON": {}}))

	mustMerge(t, g, issue(t, s, "i1", "t", map[string][]target{"LISTED_ON": {{key: "p1"}}}))
	mustMerge(t, g, issue(t, s, "i2", "t", map[string][]target{"HOME": {{key: "p1"}}}))
	mustMerge(t, g, issue(t, s, "i3", "t", map[string][]target{"LISTED_ON": {{key: "p1"}, {key: "p2"}}}))

	snap := g.Snapshot()
	var got []string
	for u := range snap.Unresolved() {
		got = append(got, u.Source().PrimaryKey().String()+":"+u.Relation()+":"+u.Reason())
	}
	slices.Sort(got)
	if want := []string{`["i1"]:HOME:absent`, `["i2"]:LISTED_ON:absent`}; !slices.Equal(got, want) {
		t.Errorf("unresolved %q, want %q", got, want)
	}
	data, res := snapshot.Marshal(t.Context(), snap)
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	if _, res := snapshot.Load(t.Context(), data, s); res.HasErrors() {
		t.Errorf("Load refuses the merged snapshot: %s", res)
	}
}

// A merged forward reference keeps its edge properties, its JSON field and
// whether its association is required, as an Add's does.
func TestMerge_AForwardReferenceKeepsItsRecord(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	mustMerge(t, g, issue(t, s, "i1", "t", map[string][]target{
		"ISSUED_BY": {{key: "late", props: map[string]any{"role": "r"}}},
		"LISTED_ON": {{key: "px"}},
	}))

	res := g.Check(t.Context())
	var fields []string
	for iss := range res.Issues() {
		if iss.Code() != diag.E_UNRESOLVED_REQUIRED {
			continue
		}
		for _, d := range iss.Details() {
			if d.Key == diag.DetailKeyJSONField {
				fields = append(fields, d.Value)
			}
		}
	}
	if !slices.Equal(fields, []string{"listed_on"}) {
		t.Errorf("E_UNRESOLVED_REQUIRED json fields %q, want [listed_on]: %s", fields, res)
	}
	if g.Snapshot().Attestation().Associations {
		t.Error("Associations attested with a required forward reference unresolved")
	}

	mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{"late"}, map[string]any{"name": "L"}))
	if got := edgesOf(g.Snapshot()); !slices.Contains(got, `ISSUED_BY->["late"]{role: "r"}`) {
		t.Errorf("edges %q: the resolved forward reference lost its edge property", got)
	}
}

// A merge's payload comes from a validator, so a merge leaves the Values claim
// standing.
func TestMerge_KeepsTheValuesAttestation(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	vi := validated(t, s)
	g := graph.New(s)
	mustAdd(t, g, vi("Page", map[string]any{"id": "p1"}), vi("Issuer", map[string]any{"id": "a", "name": "A"}))
	mustAdd(t, g, vi("Issue", map[string]any{
		"id": "i1", "title": "t",
		"home":      map[string]any{"_target_id": "p1"},
		"listed_on": []any{map[string]any{"_target_id": "p1"}},
	}))
	mustMerge(t, g, vi("Issue", map[string]any{
		"id": "i1", "title": "t",
		"home":      map[string]any{"_target_id": "p1"},
		"listed_on": []any{map[string]any{"_target_id": "p1"}},
		"issued_by": []any{map[string]any{"_target_id": "a", "role": "r"}},
	}))
	if !g.Snapshot().Attestation().Values {
		t.Fatal("Values withdrawn by a validated merge")
	}
}

// Add never merges: a held key is a duplicate, with E_DUPLICATE_PK and a
// Duplicate record, where AddOrMerge would merge.
func TestAdd_RefusesAHeldKeyWhereAddOrMergeMerges(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	if res := g.Add(t.Context(), issue(t, s, "i1", "t", with(required(), "ISSUED_BY", target{"a", nil}))); !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Errorf("Add of a held key = %s, want E_DUPLICATE_PK", res)
	}
	snap := g.Snapshot()
	if n := len(slices.Collect(snap.Duplicates())); n != 1 {
		t.Errorf("%d duplicates, want 1", n)
	}
	if slices.Contains(edgesOf(snap), `ISSUED_BY->["a"]{}`) {
		t.Error("Add installed the duplicate's edge")
	}
}

// Every AddOrMerge that returned no error is in the graph's snapshot, however
// the calls interleave.
func TestAddOrMerge_EveryAcceptedCallIsInTheSnapshot(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	const workers, perWorker = 8, 200
	var mu sync.Mutex
	accepted := make(map[string]bool)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			<-start
			for j := range perWorker {
				id := fmt.Sprintf("x-%d-%d", w, j)
				if _, res := g.AddOrMerge(t.Context(), mustValidInstance(t, s, "Issuer", []any{id}, map[string]any{"name": "X"})); res.HasErrors() {
					t.Errorf("%s: %s", id, res)
					return
				}
				mu.Lock()
				accepted[fmt.Sprintf("[%q]", id)] = true
				mu.Unlock()
			}
		})
	}
	close(start)
	wg.Wait()
	held := make(map[string]bool)
	for inst := range g.Snapshot().InstancesOf(mustTypeID(t, s, "Issuer")) {
		held[inst.PrimaryKey().String()] = true
	}
	mu.Lock()
	defer mu.Unlock()
	if !maps.Equal(held, accepted) {
		t.Errorf("snapshot holds %d issuers, %d calls were accepted: every accepted call, and no other, must be in it", len(held), len(accepted))
	}
}

// A snapshot taken while another goroutine sends listings of one key through
// AddOrMerge holds the first listings whole, every one that returned before it
// was asked for among them. Written to the wire and loaded, a mid-run snapshot
// seeds a graph that holds the snapshot's edges, takes every listing again and
// ends with each edge once.
func TestAddOrMerge_AMidRunSnapshotSeedsAResume(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	ctx := t.Context()
	g := graph.New(s)
	const listings, every = 40, 10
	mustAdd(t, g, roots(t, s)...)
	// A listing names two issuers, so a merge that is not atomic can show half
	// of one.
	sides := []string{"a", "b"}
	batch := make([]instance.ValidInstance, listings)
	for i := range listings {
		var targets []target
		for _, side := range sides {
			id := fmt.Sprint(side, i)
			mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{id}, map[string]any{"name": "X"}))
			targets = append(targets, target{id, map[string]any{"role": fmt.Sprint("r", i)}})
		}
		batch[i] = issue(t, s, "shared", "t", with(required(), "ISSUED_BY", targets...))
	}
	// edgesAt renders the shared issue's edges once it holds the first k
	// listings, as edgesOf renders them: none before the first listing.
	edgesAt := func(k int) []string {
		if k == 0 {
			return nil
		}
		out := []string{`HOME->["p1"]{}`, `LISTED_ON->["p1"]{}`}
		for i := range k {
			for _, side := range sides {
				out = append(out, fmt.Sprintf("ISSUED_BY->[%q]{role: %q}", fmt.Sprint(side, i), fmt.Sprint("r", i)))
			}
		}
		slices.Sort(out)
		return out
	}

	// check holds one snapshot to the rule, where at least lo listings had
	// returned before it was asked for. midRun keeps the first snapshot seen at
	// each count of listings between none and all.
	midRun := make(map[int]graph.Snapshot)
	var fault string
	var taken, checked int
	check := func(snap graph.Snapshot, lo int) {
		checked++
		// The first listing brings HOME and LISTED_ON beside its two issuers.
		got := edgesOf(snap)
		k := max(0, (len(got)-2)/2)
		switch {
		case fault != "":
		case !slices.Equal(got, edgesAt(k)):
			fault = fmt.Sprintf("a snapshot holds edges %q: not the first listings, each whole", got)
		case k < lo:
			fault = fmt.Sprintf("a snapshot holds %d listings, and %d had returned before it was asked for", k, lo)
		case k > 0 && k < listings && midRun[k].IsZero():
			midRun[k] = snap
		}
	}

	var returned atomic.Int64
	tick, done := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		defer close(done)
		for i, listing := range batch {
			if _, res := g.AddOrMerge(ctx, listing); res.HasErrors() {
				t.Error(res)
				return
			}
			returned.Add(1)
			// Each tick follows one snapshot. The snapshot before the second
			// tick ran whole during this wait, so it holds exactly n listings.
			if n := i + 1; n%every == 0 && n < listings {
				<-tick
				<-tick
			}
		}
	})
	wg.Go(func() {
		// Snapshots are checked in batches: no check runs between the snapshots
		// of one batch, and at most one batch waits unchecked.
		type sample struct {
			snap graph.Snapshot
			lo   int
		}
		samples := make([]sample, 0, 64)
		flush := func() {
			for _, sm := range samples {
				check(sm.snap, sm.lo)
			}
			samples = samples[:0]
		}
		defer flush()
		for {
			if len(samples) == cap(samples) {
				flush()
			}
			lo := int(returned.Load())
			samples = append(samples, sample{g.Snapshot(), lo})
			taken++
			select {
			case <-done:
				return
			case tick <- struct{}{}:
			default:
			}
		}
	})
	wg.Wait()
	if fault != "" {
		t.Fatal(fault)
	}
	if checked != taken {
		t.Fatalf("%d of %d snapshots were checked", checked, taken)
	}
	if floor := listings/every - 1; len(midRun) < floor {
		t.Fatalf("%d snapshots fell mid-run, want at least %d", len(midRun), floor)
	}

	want := edgesAt(listings)
	if res := g.Check(ctx); res.HasErrors() {
		t.Fatal(res)
	}
	if got := edgesOf(g.Snapshot()); !slices.Equal(got, want) {
		t.Fatalf("the run ended with edges %q, want %q", got, want)
	}

	for k, snap := range midRun {
		data, res := snapshot.Marshal(ctx, snap)
		if res.HasErrors() {
			t.Fatalf("the snapshot of %d listings: Marshal: %s", k, res)
		}
		loaded, res := snapshot.Load(ctx, data, s)
		if res.HasErrors() {
			t.Fatalf("the snapshot of %d listings: Load: %s", k, res)
		}
		resumed := mustImport(t, s, loaded)
		if got := edgesOf(resumed.Snapshot()); !slices.Equal(got, edgesAt(k)) {
			t.Fatalf("the seed from %d listings holds edges %q, want %q", k, got, edgesAt(k))
		}
		for i, listing := range batch {
			if merged, res := resumed.AddOrMerge(ctx, listing); res.HasErrors() || !merged {
				t.Fatalf("resumed from %d listings, listing %d: merged %v, %s", k, i, merged, res)
			}
		}
		if res := resumed.Check(ctx); res.HasErrors() {
			t.Fatalf("resumed from %d listings: Check: %s", k, res)
		}
		fin := resumed.Snapshot()
		if got := edgesOf(fin); !slices.Equal(got, want) {
			t.Fatalf("resumed from %d listings: edges %q, want %q", k, got, want)
		}
		if n := len(slices.Collect(fin.Duplicates())); n != 0 {
			t.Fatalf("resumed from %d listings: %d duplicates; a resume records none", k, n)
		}
	}
}

// Add and AddOrMerge each name themselves in the panics a caller cannot
// recover from and in a cancellation, and a cancelled call installs nothing.
// TestNilContext_Panics holds the nil-context panics.
func TestAdd_PanicsAndCancellationsNameTheMethod(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	inst := roots(t, s)[0]
	foreign := mustValidInstance(t, testSchemaWithAssociation(t), "Company", []any{"c1"}, map[string]any{"name": "n"})
	for name, add := range map[string]func(*graph.Graph, context.Context, instance.ValidInstance) diag.Result{
		"graph.Add": (*graph.Graph).Add,
		"graph.AddOrMerge": func(g *graph.Graph, ctx context.Context, v instance.ValidInstance) diag.Result {
			_, res := g.AddOrMerge(ctx, v)
			return res
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			var none *graph.Graph
			for want, call := range map[string]func(){
				name + ": nil *Graph receiver": func() { add(none, ctx, inst) },
				name + ": zero ValidInstance":  func() { add(graph.New(s), ctx, instance.ValidInstance{}) },
				name + ": instance was validated under another schema load, or its type is outside this schema's import closure": func() {
					add(graph.New(s), ctx, foreign)
				},
			} {
				if got := yammmtest.AssertPanics(t, call); got != want {
					t.Errorf("panic %v, want %q", got, want)
				}
			}
			// A cancelled context does not hide a programmer error.
			for want, call := range map[string]func(){
				name + ": zero ValidInstance": func() { add(graph.New(s), cancelled, instance.ValidInstance{}) },
				name + ": instance was validated under another schema load, or its type is outside this schema's import closure": func() {
					add(graph.New(s), cancelled, foreign)
				},
			} {
				if got := yammmtest.AssertPanics(t, call); got != want {
					t.Errorf("panic %v, want %q", got, want)
				}
			}
			g := graph.New(s)
			res := add(g, cancelled, inst)
			if !res.HasCode(diag.E_CONTEXT_CANCELLED) || !strings.Contains(res.String(), name+" cancelled") {
				t.Errorf("a cancelled call = %s, want an E_CONTEXT_CANCELLED naming %s", res, name)
			}
			if n := len(slices.Collect(g.Snapshot().AllInstances())); n != 0 {
				t.Errorf("a cancelled call installed %d instances", n)
			}
		})
	}
}

// A merge that would give several (one) associations a second target is
// refused with one E_GRAPH_CARDINALITY for each, and each names the held root
// and its association.
func TestMerge_RefusesEveryOneAssociationItWouldOverfill(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	for run := range 50 {
		g := graph.New(s)
		mustAdd(t, g, roots(t, s)...)
		mustAdd(t, g, issue(t, s, "i1", "t", with(required(), "LEAD", target{"a", nil})))
		merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i1", "t", with(with(required(), "HOME", target{"p2", nil}), "LEAD", target{"b", nil})))
		if merged {
			t.Fatalf("run %d: merged; want a refusal", run)
		}
		var relations []string
		for iss := range res.Issues() {
			if iss.Code() != diag.E_GRAPH_CARDINALITY {
				t.Fatalf("run %d: %s; want E_GRAPH_CARDINALITY alone", run, res)
			}
			details := make(map[string]string)
			for _, d := range iss.Details() {
				details[d.Key] = d.Value
			}
			if details[diag.DetailKeyTypeName] != "Issue" || details[diag.DetailKeyPrimaryKey] != `["i1"]` {
				t.Errorf("run %d: details %v; want type_name Issue and primary_key [\"i1\"]", run, details)
			}
			relations = append(relations, details[diag.DetailKeyRelationName])
			if !strings.Contains(iss.Message(), strconv.Quote(details[diag.DetailKeyRelationName])) {
				t.Errorf("run %d: message %q does not name its association", run, iss.Message())
			}
		}
		if slices.Sort(relations); !slices.Equal(relations, []string{"HOME", "LEAD"}) {
			t.Fatalf("run %d: refused relations %q, want [HOME LEAD]", run, relations)
		}
		// A collector that keeps one issue keeps the refusal that arrives first.
		capped := diag.NewCollector(1)
		capped.Merge(res)
		for iss := range capped.Result().Issues() {
			for _, d := range iss.Details() {
				if d.Key == diag.DetailKeyRelationName && d.Value != "HOME" {
					t.Fatalf("run %d: a one-issue collector kept %s; the refusals arrive in relation-name order", run, d.Value)
				}
			}
		}
		want := []string{`HOME->["p1"]{}`, `LEAD->["a"]{}`, `LISTED_ON->["p1"]{}`}
		if got := edgesOf(g.Snapshot()); !slices.Equal(got, want) {
			t.Fatalf("run %d: edges %q, want %q: a refused merge installs nothing", run, got, want)
		}
	}
}

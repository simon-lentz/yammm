package graph_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// mergeSchema has one type listed under several parents: an Issue names its
// issuers through an optional (many) association with an edge property and an
// optional (one), and its pages through a required (many) and a required (one).
func mergeSchema(t *testing.T) *schema.Schema {
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
func issue(t *testing.T, s *schema.Schema, id, title string, edges map[string][]target) *instance.ValidInstance {
	t.Helper()
	data := make(map[string]*instance.ValidEdgeData, len(edges))
	for rel, targets := range edges {
		ts := make([]instance.ValidEdgeTarget, len(targets))
		for i, tg := range targets {
			ts[i] = instance.NewValidEdgeTarget(immutable.WrapKey([]any{tg.key}), immutable.WrapProperties(tg.props))
		}
		data[rel] = instance.NewValidEdgeData(ts)
	}
	return instancetest.VI("Issue",
		instancetest.TypeID(mustTypeID(t, s, "Issue")),
		instancetest.PK(id),
		instancetest.Props(map[string]any{"id": id, "title": title}),
		instancetest.Edges(data),
	)
}

// edgesOf renders every edge of snap as `relation->key{name: "value"}`,
// sorted.
func edgesOf(snap *graph.Snapshot) []string {
	var out []string
	for _, e := range snap.Edges() {
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
func unresolvedOf(snap *graph.Snapshot) []string {
	var out []string
	for _, u := range snap.Unresolved() {
		out = append(out, u.Relation()+":"+u.Reason()+":"+u.TargetKey())
	}
	slices.Sort(out)
	return out
}

// mustAdd adds each instance through Add and fails on any error.
func mustAdd(t *testing.T, g *graph.Graph, insts ...*instance.ValidInstance) {
	t.Helper()
	for _, inst := range insts {
		if res := g.Add(t.Context(), inst); res.HasErrors() {
			t.Fatalf("Add %s: %s", inst.PrimaryKey(), res)
		}
	}
}

// mustMerge merges inst and fails unless it merged cleanly.
func mustMerge(t *testing.T, g *graph.Graph, inst *instance.ValidInstance) {
	t.Helper()
	merged, res := g.AddOrMerge(t.Context(), inst)
	if res.HasErrors() || !merged {
		t.Fatalf("AddOrMerge %s: merged %v, %s", inst.PrimaryKey(), merged, res)
	}
}

// roots returns the Issuer and Page roots every issue fixture may name.
func roots(t *testing.T, s *schema.Schema) []*instance.ValidInstance {
	t.Helper()
	return []*instance.ValidInstance{
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
func TestAddOrMerge_AddsAnInstanceTheGraphDoesNotHold(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	merged, res := g.AddOrMerge(t.Context(), issue(t, s, "i1", "first", required()))
	if res.HasErrors() || merged {
		t.Fatalf("merged %v, %s; want an add", merged, res)
	}
	if n := len(g.Snapshot().InstancesOf(mustTypeID(t, s, "Issue"))); n != 1 {
		t.Errorf("%d issues, want 1", n)
	}
}

// A second listing installs its edges on the held issue, carrying their edge
// properties; the held properties stand, and nothing is recorded as a
// duplicate or an error.
func TestAddOrMerge_InstallsTheDuplicatesEdgesOnTheHeldInstance(t *testing.T) {
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
	if len(snap.Duplicates()) != 0 || !snap.Diagnostics().OK() {
		t.Errorf("duplicates %d, diagnostics %s; a merge records neither", len(snap.Duplicates()), snap.Diagnostics())
	}
}

// A target the held instance already names, by an edge or an unresolved
// record, adds nothing, and neither does a repeat inside the incoming data;
// the held edge's properties stand.
func TestAddOrMerge_ATargetAlreadyNamedAddsNothing(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY",
		target{"a", map[string]any{"role": "lead"}}, target{"zz", nil})))
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY",
		target{"a", map[string]any{"role": "other"}}, target{"zz", nil}, target{"b", nil}, target{"b", nil})))

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
func TestAddOrMerge_RefusesASecondTargetOnAOneAssociation(t *testing.T) {
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
	if !snap.Diagnostics().HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Error("the refusal is not in the snapshot's diagnostics")
	}
}

// A forward reference merges as an unresolved record on the held instance,
// which a later Add of the target resolves.
func TestAddOrMerge_CarriesAForwardReference(t *testing.T) {
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
	if len(snap.Unresolved()) != 0 || !slices.Contains(edgesOf(snap), `ISSUED_BY->["late"]{}`) {
		t.Errorf("edges %q, unresolved %q; the target's Add must resolve the merged reference", edgesOf(snap), unresolvedOf(snap))
	}
}

// A required association's "absent" record is retired when the merge installs
// a record under it, so the record stands alone as the Records fact states,
// the Associations claim holds, and the snapshot reader accepts the result.
func TestAddOrMerge_RetiresARequiredAssociationsAbsentRecord(t *testing.T) {
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
func TestAddOrMerge_RefusesADuplicateCarryingComposedChildren(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	g := graph.New(s)
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	note := mustValidPartInstance(t, s, "Note", []any{"n1"}, nil)
	withNote := instancetest.VI("Issue",
		instancetest.TypeID(mustTypeID(t, s, "Issue")),
		instancetest.PK("i1"),
		instancetest.Props(map[string]any{"id": "i1", "title": "t"}),
		instancetest.Composed(map[string]immutable.Value{"NOTES": immutable.Wrap([]any{note})}),
	)
	merged, res := g.AddOrMerge(t.Context(), withNote)
	if merged || !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Fatalf("merged %v, %s; want E_DUPLICATE_PK", merged, res)
	}
	if n := len(g.Snapshot().Duplicates()); n != 1 {
		t.Errorf("%d duplicates, want 1", n)
	}
}

// A root seeded from a snapshot merges as one added in this graph: the source
// index reads the imported records.
func TestAddOrMerge_MergesIntoASeededInstance(t *testing.T) {
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

// Concurrent callers merging listings of one key into one assembler keep
// every listing's edge, record no duplicate, and count one instance.
func TestAddValidOrMerge_ConcurrentListingsKeepEveryEdge(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	ctx := context.Background()
	ba := graph.NewBatchAssembler(ctx, s)
	const issuers = 40
	for _, r := range roots(t, s) {
		if err := ba.AddValid(r); err != nil {
			t.Fatal(err)
		}
	}
	for i := range issuers {
		if err := ba.AddValid(mustValidInstance(t, s, "Issuer", []any{fmt.Sprint("x", i)}, map[string]any{"name": "X"})); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	merges := 0
	for i := range issuers {
		wg.Go(func() {
			merged, err := ba.AddValidOrMerge(issue(t, s, "shared", "t", with(required(), "ISSUED_BY", target{fmt.Sprint("x", i), nil})))
			if err != nil {
				t.Error(err)
			}
			if merged {
				mu.Lock()
				merges++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if merges != issuers-1 {
		t.Errorf("%d calls merged, want %d: the first listing adds and every other merges", merges, issuers-1)
	}
	if got, want := ba.Count(), len(roots(t, s))+issuers+1; got != want {
		t.Errorf("Count %d, want %d: a merge adds no instance", got, want)
	}
	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	issuedBy := 0
	for _, e := range res.Snapshot.Edges() {
		if e.Relation() == "ISSUED_BY" {
			issuedBy++
		}
	}
	if issuedBy != issuers || len(res.Snapshot.Duplicates()) != 0 {
		t.Errorf("%d ISSUED_BY edges and %d duplicates, want %d and 0", issuedBy, len(res.Snapshot.Duplicates()), issuers)
	}
	if merged, err := ba.AddValidOrMerge(issue(t, s, "shared", "t", required())); merged || !errors.Is(err, graph.ErrAssemblerFinalized) {
		t.Errorf("after Finalize: merged %v, %v; want false and ErrAssemblerFinalized", merged, err)
	}
}

// validated returns a validator for s's instances that fails the test on any
// error, so a graph built from it attests Values.
func validated(t *testing.T, s *schema.Schema) func(typ string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	v := instance.NewValidator(s)
	return func(typ string, props map[string]any) *instance.ValidInstance {
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
func TestAddOrMerge_FindsRecordsInstalledAfterTheFirstMerge(t *testing.T) {
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
	for _, e := range snap.Edges() {
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
func TestAddOrMerge_CountsAOneAssociationsHeldTarget(t *testing.T) {
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
	for _, e := range snap.Edges() {
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
func TestAddOrMerge_RetiresOneRecordOfSeveralSharingATargetType(t *testing.T) {
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
	for _, u := range snap.Unresolved() {
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
func TestAddOrMerge_AForwardReferenceKeepsItsRecord(t *testing.T) {
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

// An unvalidated payload that installs a record withdraws the Values claim;
// one that installs nothing leaves it standing.
func TestAddOrMerge_JoinsValuesWhenItInstallsARecord(t *testing.T) {
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

	mustMerge(t, g, issue(t, s, "i1", "t", required()))
	if !g.Snapshot().Attestation().Values {
		t.Error("Values withdrawn by an unvalidated merge that installed nothing")
	}
	mustMerge(t, g, issue(t, s, "i1", "t", with(required(), "LEAD", target{"a", nil})))
	if g.Snapshot().Attestation().Values {
		t.Error("Values stands after an unvalidated merge installed a record")
	}
}

// AddOrMerge names itself in its trace, its cancellation and its panics.
func TestAddOrMerge_NamesItself(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	var buf bytes.Buffer
	g := graph.New(s, graph.WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	mustAdd(t, g, roots(t, s)...)
	mustAdd(t, g, issue(t, s, "i1", "t", required()))
	buf.Reset()
	mustMerge(t, g, issue(t, s, "i1", "t", required()))
	if !strings.Contains(buf.String(), "op=yammm.graph.add_or_merge") {
		t.Errorf("trace %q names no yammm.graph.add_or_merge operation", buf.String())
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if merged, res := g.AddOrMerge(ctx, issue(t, s, "i2", "t", required())); merged || !strings.Contains(res.String(), "graph.AddOrMerge cancelled") {
		t.Errorf("merged %v, %s; want a graph.AddOrMerge cancellation", merged, res)
	}

	defer func() {
		if msg, _ := recover().(string); msg != "graph.AddOrMerge: nil ValidInstance" {
			t.Errorf("panic %q, want graph.AddOrMerge's", msg)
		}
	}()
	g.AddOrMerge(t.Context(), nil)
}

// An Add traces a resolution only when it resolved a record.
func TestAdd_TracesAResolutionOnlyWhenItResolves(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	var buf bytes.Buffer
	g := graph.New(s, graph.WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	mustAdd(t, g, roots(t, s)...)
	if strings.Contains(buf.String(), "pending edges resolved") {
		t.Errorf("trace %q reports a resolution no Add made", buf.String())
	}
	mustAdd(t, g, issue(t, s, "i1", "t", with(required(), "ISSUED_BY", target{"late", nil})))
	mustAdd(t, g, mustValidInstance(t, s, "Issuer", []any{"late"}, map[string]any{"name": "L"}))
	if !strings.Contains(buf.String(), "pending edges resolved") {
		t.Errorf("trace %q omits the resolution", buf.String())
	}
}

// A nil instance and a refused merge through AddValidOrMerge each report no
// merge, are tagged, and leave Count alone.
func TestAddValidOrMerge_ARefusalReportsNoMerge(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)
	for _, r := range roots(t, s) {
		if err := ba.AddValid(r); err != nil {
			t.Fatal(err)
		}
	}

	merged, err := ba.AddValidOrMerge(nil)
	ce, ok := errors.AsType[*diag.ContextualError](err)
	if merged || !ok || ce.Tag != "nil-instance (attempt #5)" || !ce.Result.HasCode(diag.E_INTERNAL) {
		t.Errorf("nil: merged %v, %v", merged, err)
	}

	if merged, err := ba.AddValidOrMerge(issue(t, s, "i1", "t", with(required(), "LEAD", target{"a", nil}))); merged || err != nil {
		t.Fatalf("first listing: merged %v, %v", merged, err)
	}
	merged, err = ba.AddValidOrMerge(issue(t, s, "i1", "t", with(required(), "LEAD", target{"b", nil})))
	ce, ok = errors.AsType[*diag.ContextualError](err)
	if merged || !ok || ce.Tag != "Issue (attempt #7)" || !ce.Result.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("second LEAD: merged %v, %v; want a tagged E_GRAPH_CARDINALITY", merged, err)
	}
	if got, want := ba.Count(), len(roots(t, s))+1; got != want {
		t.Errorf("Count %d, want %d", got, want)
	}
}

// AddValid still refuses a duplicate key; only AddValidOrMerge merges.
func TestBatchAssembler_AddValid_StillRefusesADuplicate(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)
	for _, r := range roots(t, s) {
		if err := ba.AddValid(r); err != nil {
			t.Fatal(err)
		}
	}
	err := ba.AddValid(roots(t, s)[0])
	if ce, ok := errors.AsType[*diag.ContextualError](err); !ok || !ce.Result.HasCode(diag.E_DUPLICATE_PK) {
		t.Errorf("AddValid of a held key: %v; want E_DUPLICATE_PK", err)
	}
	if got, want := ba.Count(), len(roots(t, s)); got != want {
		t.Errorf("Count %d, want %d", got, want)
	}
	res, err := ba.Finalize(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Snapshot.Duplicates()); n != 1 {
		t.Errorf("%d duplicates, want 1", n)
	}
}

// The assembler's two paths into the graph panic under their own names.
func TestBatchAssembler_PanicsUnderTheGraphMethodsName(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	foreign := mustValidInstance(t, testSchemaWithAssociation(t), "Company", []any{"c1"}, map[string]any{"name": "n"})
	for name, call := range map[string]func(*graph.BatchAssembler){
		"graph.Add":        func(ba *graph.BatchAssembler) { _ = ba.AddValid(foreign) },
		"graph.AddOrMerge": func(ba *graph.BatchAssembler) { _, _ = ba.AddValidOrMerge(foreign) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if msg, _ := recover().(string); !strings.HasPrefix(msg, name+": ") {
					t.Errorf("panic %q, want the prefix %q", msg, name+": ")
				}
			}()
			call(graph.NewBatchAssembler(t.Context(), s))
		})
	}
}

// Every AddValidOrMerge that returned no error is in Finalize's snapshot,
// however the calls interleave with Finalize.
func TestAddValidOrMerge_EveryAcceptedCallIsInTheSnapshot(t *testing.T) {
	t.Parallel()
	s := mergeSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)
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
				_, err := ba.AddValidOrMerge(mustValidInstance(t, s, "Issuer", []any{id}, map[string]any{"name": "X"}))
				switch {
				case err == nil:
					mu.Lock()
					accepted[fmt.Sprintf("[%q]", id)] = true
					mu.Unlock()
				case !errors.Is(err, graph.ErrAssemblerFinalized):
					t.Errorf("%s: %v", id, err)
					return
				}
			}
		})
	}
	close(start)
	time.Sleep(2 * time.Millisecond)
	res, err := ba.Finalize(t.Context())
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	held := make(map[string]bool)
	for _, inst := range res.Snapshot.InstancesOf(mustTypeID(t, s, "Issuer")) {
		held[inst.PrimaryKey().String()] = true
	}
	mu.Lock()
	defer mu.Unlock()
	if !maps.Equal(held, accepted) {
		t.Errorf("snapshot holds %d issuers, %d calls were accepted: every accepted call, and no other, must be in it", len(held), len(accepted))
	}
}

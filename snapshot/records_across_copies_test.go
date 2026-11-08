package snapshot_test

import (
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/recordtest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const copiesSource = `schema "copies"

part type Line {
	sku String primary
	qty Integer required
}

part type Cfg {
	ver String primary
}

type Company {
	id String primary
}

type Order {
	id String primary
	note String
	--> PLACED_BY (_:one) Company {
		role String
	}
	--> ALSO (many) Company
	*-> LINES (many) Line
	*-> CFG (_:one) Cfg
}

type Zone {
	id String primary
}
`

// copiesGraph holds two or more of each collection, and a no-property edge.
func copiesGraph(t *testing.T) (schema.Schema, *graph.Graph) {
	t.Helper()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, copiesSource, "copies.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typ string, props map[string]any) {
		t.Helper()
		valid, res := v.ValidateOne(ctx, typ, instance.RawInstance{Properties: props})
		if !res.OK() {
			t.Fatal(res)
		}
		_ = g.Add(ctx, valid)
	}
	add("Zone", map[string]any{"id": "z1"})
	add("Company", map[string]any{"id": "c1"})
	add("Company", map[string]any{"id": "c2"})
	add("Order", map[string]any{
		"id":        "o1",
		"note":      "first",
		"placed_by": map[string]any{"_target_id": "c1", "role": "buyer"},
		"also":      []any{map[string]any{"_target_id": "c2"}, map[string]any{"_target_id": "c1"}},
		"lines":     []any{map[string]any{"sku": "a", "qty": int64(1)}, map[string]any{"sku": "b", "qty": int64(2)}},
		"cfg":       []any{map[string]any{"ver": "1"}},
	})
	add("Order", map[string]any{
		"id":        "o2",
		"placed_by": map[string]any{"_target_id": "missing", "role": "r"},
		"also":      []any{map[string]any{"_target_id": "gone1"}, map[string]any{"_target_id": "gone2"}},
	})
	add("Order", map[string]any{"id": "o1", "note": "the duplicate"})
	add("Zone", map[string]any{"id": "z1"})
	return s, g
}

func checkCopyIdentity(t *testing.T, label string, snap graph.Snapshot) {
	t.Helper()
	roots := map[graph.Instance]bool{}
	for inst := range snap.AllInstances() {
		roots[inst] = true
	}
	for e := range snap.Edges() {
		if !roots[e.Source()] || !roots[e.Target()] {
			t.Errorf("%s: edge %s's endpoints must be roots of the snapshot", label, e.Relation())
		}
		if !slices.Contains(slices.Collect(snap.EdgesFrom(e.Source())), e) {
			t.Errorf("%s: EdgesFrom and Edges must name one record", label)
		}
	}
	for u := range snap.Unresolved() {
		if !roots[u.Source()] {
			t.Errorf("%s: an unresolved record's source must be a root", label)
		}
	}
	var duplicates int
	for d := range snap.Duplicates() {
		duplicates++
		if !roots[d.Conflict()] {
			t.Errorf("%s: a duplicate must conflict with a root", label)
		}
	}
	if duplicates != 2 {
		t.Errorf("%s: Duplicates yields %d, want 2", label, duplicates)
	}
}

func TestCopies_EveryRecordNamesTheRecordsOfItsOwnSnapshot(t *testing.T) {
	ctx := t.Context()
	s, g := copiesGraph(t)
	built := g.Snapshot()
	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatal(res)
	}
	loaded, res := snapshot.Load(ctx, data, s)
	if res.HasErrors() {
		t.Fatal(res)
	}
	imported := func(from graph.Snapshot) graph.Snapshot {
		t.Helper()
		copied, res := graph.NewFromSnapshot(s, from)
		if res.HasErrors() {
			t.Fatal(res)
		}
		return copied.Snapshot()
	}
	for label, snap := range map[string]graph.Snapshot{
		"built": built, "second": g.Snapshot(), "loaded": loaded,
		"imported": imported(built), "imported loaded": imported(loaded),
	} {
		checkCopyIdentity(t, label, snap)
	}
}

func TestCopies_TwoSnapshotsOfAGraphShareNoRecord(t *testing.T) {
	_, g := copiesGraph(t)
	first, second := g.Snapshot(), g.Snapshot()
	copied, res := graph.NewFromSnapshot(first.Schema(), first)
	if res.HasErrors() {
		t.Fatal(res)
	}
	imported := copied.Snapshot()
	for label, other := range map[string]graph.Snapshot{"second": second, "imported": imported} {
		held := map[graph.Instance]bool{}
		for d := range other.Duplicates() {
			held[d.Instance()] = true
			held[d.Conflict()] = true
		}
		for d := range first.Duplicates() {
			if held[d.Instance()] || held[d.Conflict()] {
				t.Errorf("%s: a duplicate at %s shares its record with the first snapshot", label, d.Instance().PrimaryKey())
			}
		}
	}
}

func TestCopies_ADuplicateWalksTheSameBuiltAndLoaded(t *testing.T) {
	ctx := t.Context()
	s, g := copiesGraph(t)
	built := g.Snapshot()
	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatal(res)
	}
	loaded, res := snapshot.Load(ctx, data, s)
	if res.HasErrors() {
		t.Fatal(res)
	}
	walks := func(snap graph.Snapshot) [][]string {
		var out [][]string
		for d := range snap.Duplicates() {
			out = append(out, recordtest.Walk(d.Instance(), loadedRecordsOptions()))
		}
		return out
	}
	yammmtest.Diff(t, recordtest.Walk(built, loadedRecordsOptions()), recordtest.Walk(loaded, loadedRecordsOptions()))
	yammmtest.Diff(t, walks(built), walks(loaded))
}

func TestImport_ACopyUnderAnotherAliasRenamesItsRecordsAndLeavesTheSourceAlone(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("lib.yammm", "schema \"lib\"\n\npart type P {\n\tk String primary\n}\n\ntype T {\n\tid String primary\n\t*-> PS (many) P\n}\n")
	load := func(file, alias string) schema.Schema {
		t.Helper()
		s, res := schema.Load(ctx, write(file, "schema \"m"+alias+"\"\n\nimport \"./lib\" as "+alias+"\n\ntype Host {\n\tid String primary\n}\n"))
		if res.HasErrors() {
			t.Fatal(res)
		}
		return s
	}
	xs, ys := load("x.yammm", "x"), load("y.yammm", "y")
	g := graph.New(xs)
	v := instance.NewValidator(xs)
	for range 2 {
		valid, res := v.ValidateOne(ctx, "x.T", instance.RawInstance{Properties: map[string]any{
			"id": "t1", "ps": []any{map[string]any{"k": "p1"}},
		}})
		if !res.OK() {
			t.Fatal(res)
		}
		_ = g.Add(ctx, valid)
	}
	source := g.Snapshot()
	copied, res := graph.NewFromSnapshot(ys, source)
	if res.HasErrors() {
		t.Fatal(res)
	}
	names := func(snap graph.Snapshot) []string {
		var out []string
		for inst := range snap.AllInstances() {
			out = append(out, inst.TypeName())
			for child := range inst.Composed("PS") {
				out = append(out, child.TypeName())
			}
		}
		for d := range snap.Duplicates() {
			out = append(out, d.Instance().TypeName(), d.Conflict().TypeName())
		}
		return out
	}
	yammmtest.Diff(t, []string{"y.T", "y.P", "y.T", "y.T"}, names(copied.Snapshot()))
	yammmtest.Diff(t, []string{"x.T", "x.P", "x.T", "x.T"}, names(source))
}

func TestImport_ADuplicatesKeyMovesToTheNewSchemasForm(t *testing.T) {
	ctx := t.Context()
	load := func(keyType string) schema.Schema {
		t.Helper()
		s, res := schema.LoadString(ctx, "schema \"r\"\n\ntype Run {\n\tat "+keyType+" primary\n}\n", "r.yammm")
		if res.HasErrors() {
			t.Fatal(res)
		}
		return s
	}
	before, after := load("String"), load("Timestamp")
	const raw = "2020-01-02T03:04:05.000+01:00"
	inst := instancetest.Valid(t, before, "Run", map[string]any{"at": raw})
	g := graph.New(before)
	_ = g.Add(ctx, inst)
	_ = g.Add(ctx, inst)
	copied, res := graph.NewFromSnapshot(after, g.Snapshot())
	if res.HasErrors() {
		t.Fatal(res)
	}
	snap := copied.Snapshot()
	var root string
	for r := range snap.AllInstances() {
		root = r.PrimaryKey().String()
	}
	if strings.Contains(root, raw) {
		t.Fatalf("the root's key %s did not move", root)
	}
	for d := range snap.Duplicates() {
		if got := d.Instance().PrimaryKey().String(); got != root {
			t.Errorf("the duplicate's key = %s, want %s", got, root)
		}
	}
}

func TestSnapshot_EachIteratorRangesAgainAndStopsAtABreak(t *testing.T) {
	_, g := copiesGraph(t)
	snap := g.Snapshot()
	order, ok := snap.Schema().Type("Order")
	if !ok {
		t.Fatal("no type Order")
	}
	first := slices.Collect(snap.InstancesOf(order.ID()))[0]
	checkIterator(t, "AllInstances", snap.AllInstances())
	checkIterator(t, "InstancesOf", snap.InstancesOf(order.ID()))
	checkIterator(t, "Composed", first.Composed("LINES"))
	checkIterator(t, "Edges", snap.Edges())
	checkIterator(t, "EdgesFrom", snap.EdgesFrom(first))
	checkIterator(t, "Duplicates", snap.Duplicates())
	checkIterator(t, "Unresolved", snap.Unresolved())
}

func checkIterator[T comparable](t *testing.T, name string, seq iter.Seq[T]) {
	t.Helper()
	once := slices.Collect(seq)
	if len(once) < 2 {
		t.Fatalf("%s yields %d, and the fixture must hold two", name, len(once))
	}
	if !slices.Equal(once, slices.Collect(seq)) {
		t.Errorf("%s answers differently on its second range", name)
	}
	var steps int
	for range seq {
		steps++
		break
	}
	if steps != 1 {
		t.Errorf("%s took %d steps before its break, want 1", name, steps)
	}
}

func TestSnapshot_CollectionsHoldTheirDocumentedOrder(t *testing.T) {
	_, g := copiesGraph(t)
	snap := g.Snapshot()

	var fromTypes []graph.Instance
	for _, id := range snap.Types() {
		fromTypes = slices.AppendSeq(fromTypes, snap.InstancesOf(id))
	}
	all := slices.Collect(snap.AllInstances())
	if !slices.Equal(all, fromTypes) {
		t.Error("AllInstances must hold Types' order, each type's instances in InstancesOf's order")
	}
	var typeNames []string
	for _, inst := range all {
		if !slices.Contains(typeNames, inst.TypeName()) {
			typeNames = append(typeNames, inst.TypeName())
		}
	}
	if len(typeNames) < 2 {
		t.Fatalf("the fixture holds types %q and must hold two", typeNames)
	}
	if !slices.IsSortedFunc(snap.Types(), func(a, b schema.TypeID) int { return strings.Compare(a.String(), b.String()) }) {
		t.Errorf("Types = %v, want sorted", snap.Types())
	}

	var edges []string
	for e := range snap.Edges() {
		edges = append(edges, e.Source().PrimaryKey().String()+" "+e.Relation()+" "+e.Target().PrimaryKey().String())
	}
	yammmtest.Diff(t, []string{
		`["o1"] ALSO ["c1"]`,
		`["o1"] ALSO ["c2"]`,
		`["o1"] PLACED_BY ["c1"]`,
	}, edges)
}

func TestZeroRoots_EachPanicNamesTheCallAndWhatIsZero(t *testing.T) {
	ctx := t.Context()
	s, g := copiesGraph(t)
	built := g.Snapshot()
	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatal(res)
	}
	for want, call := range map[string]func(){
		"graph.New: zero schema":                          func() { graph.New(schema.Schema{}) },
		"graph.NewFromSnapshot: zero Snapshot":            func() { _, _ = graph.NewFromSnapshot(s, graph.Snapshot{}) },
		"graph.NewFromSnapshot: zero schema":              func() { _, _ = graph.NewFromSnapshot(schema.Schema{}, built) },
		"graph.Add: zero ValidInstance":                   func() { g.Add(ctx, instance.ValidInstance{}) },
		"graph.AddOrMerge: zero ValidInstance":            func() { _, _ = g.AddOrMerge(ctx, instance.ValidInstance{}) },
		"snapshot.Marshal: zero Snapshot":                 func() { _, _ = snapshot.Marshal(ctx, graph.Snapshot{}) },
		"snapshot.Load: zero Schema":                      func() { _, _ = snapshot.Load(ctx, data, schema.Schema{}) },
		"snapshot.Verify: zero Schema":                    func() { snapshot.Verify(ctx, data, schema.Schema{}) },
		"snapshot.UpdateMetadataOrReMarshal: zero Schema": func() { _, _ = snapshot.UpdateMetadataOrReMarshal(ctx, data, nil, schema.Schema{}) },
	} {
		if got := yammmtest.AssertPanics(t, call); got != want {
			t.Errorf("panic = %v, want %q", got, want)
		}
	}
	// A zero schema is named before a zero snapshot, as the parameters stand.
	if got := yammmtest.AssertPanics(t, func() { _, _ = graph.NewFromSnapshot(schema.Schema{}, graph.Snapshot{}) }); got != "graph.NewFromSnapshot: zero schema" {
		t.Errorf("NewFromSnapshot with both arguments zero: panic = %v", got)
	}
}

func TestInfo_AnEdgeListUnderAnUndeclaredRelationDoesNotPanic(t *testing.T) {
	ctx := t.Context()
	_, g := copiesGraph(t)
	data, res := snapshot.Marshal(ctx, g.Snapshot())
	if res.HasErrors() {
		t.Fatal(res)
	}
	const anchor = `"key":["o2"],`
	if !strings.Contains(string(data), anchor) {
		t.Fatalf("no %s in the document", anchor)
	}
	edited := strings.Replace(string(data), anchor, anchor+`"edges":{"NOPE":[]},`, 1)
	if info, res := snapshot.Info(ctx, []byte(edited)); info == nil && !res.HasErrors() {
		t.Error("Info returned no record and no error")
	}
}

func TestLoad_ADuplicateWithNoProvenanceSortsBeforeAnEmptySourceName(t *testing.T) {
	s, res := schema.LoadString(t.Context(), "schema \"t\"\n\ntype Co {\n\tid String primary\n\tnote String\n}\n", "t.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	co, ok := s.Type("Co")
	if !ok {
		t.Fatal("no type Co")
	}
	key := immutable.WrapKey([]any{"c1"})
	props := immutable.WrapProperties(map[string]any{"id": "c1", "note": "x"})
	part := func(p location.Provenance) snapparts.Instance {
		return snapparts.Instance{TypeID: co.ID(), PrimaryKey: key, Properties: props, Provenance: p}
	}
	none, empty := location.Provenance{}, location.NewProvenance("", path.Root(), location.Span{})
	for _, order := range [][]location.Provenance{{none, empty}, {empty, none}} {
		snap := snappartstest.Loaded(t, s, snapparts.Parts{
			Instances: []snapparts.Instance{part(none)},
			Duplicates: []snapparts.Instance{
				part(order[0]),
				part(order[1]),
			},
		})
		var zero []bool
		for d := range snap.Duplicates() {
			zero = append(zero, d.Instance().Provenance().IsZero())
		}
		yammmtest.Diff(t, []bool{true, false}, zero)
	}
}

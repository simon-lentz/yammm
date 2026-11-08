package snapshot_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A type the entry schema cannot name: deep's Beacon, reached only through
// base. The entry declares a Beacon of its own whose properties differ, so a
// reader that resolved deep's Beacon by its bare name would judge it as the
// entry's.

const unreachableRootEntry = `schema "entry"

import "base.yammm" as base

type Beacon {
	id String primary
	label String required
}
`

const unreachableRootBase = `schema "base"

import "deep.yammm" as deep

type Basin {
	id String primary
	area Float
}
`

const unreachableRootDeep = `schema "deep"

type Beacon {
	id String primary
	power Float
}
`

// unnameableFixture loads the three-schema closure and returns the entry
// schema, the deep schema and the identity of each Beacon.
func unnameableFixture(t *testing.T) (s, deep schema.Schema, entryBeacon, deepBeacon schema.TypeID) {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(unreachableRootEntry),
		"base.yammm":  []byte(unreachableRootBase),
		"deep.yammm":  []byte(unreachableRootDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load fixture: %s", res)
	}
	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema().IsZero() {
		t.Fatal("import alias base did not resolve")
	}
	imp, ok := base.Schema().ImportByAlias("deep")
	if !ok || imp.Schema().IsZero() {
		t.Fatal("import alias deep did not resolve")
	}
	deep = imp.Schema()
	deepBeacon = factTypeID(t, deep, "Beacon")
	if _, addressable := schema.AddressableTag(s, deepBeacon); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Beacon")
	}
	return s, deep, factTypeID(t, s, "Beacon"), deepBeacon
}

// TestLoad_KeepsARootTheEntrySchemaCannotNameApartFromItsNamesake pins that a
// document holding both Beacons at one key reads back as two roots, and that
// revalidation judges each by its own type, so clean data reports nothing.
func TestLoad_KeepsARootTheEntrySchemaCannotNameApartFromItsNamesake(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, deep, entryBeacon, deepBeacon := unnameableFixture(t)
	g := graph.New(s)
	for _, valid := range []instance.ValidInstance{
		instancetest.Valid(t, s, "Beacon", map[string]any{"id": "d1", "label": "lamp"}),
		instancetest.Valid(t, deep, "Beacon", map[string]any{"id": "d1", "power": float64(2)}),
	} {
		if res := g.Add(ctx, valid); !res.OK() {
			t.Fatalf("Add: %s", res)
		}
	}
	data, res := snapshot.Marshal(ctx, g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	for name, opts := range map[string][]snapshot.LoadOption{
		"plain":                   nil,
		"revalidation at Warning": {snapshot.WithRevalidation(diag.Warning)},
		"revalidation at Error":   {snapshot.WithRevalidation(diag.Error)},
	} {
		snap, res := snapshot.Load(ctx, data, s, opts...)
		if res.Len() != 0 {
			t.Errorf("%s: Load = %s, want no issue", name, res)
			continue
		}
		for _, id := range []schema.TypeID{entryBeacon, deepBeacon} {
			if n := len(slices.Collect(snap.InstancesOf(id))); n != 1 {
				t.Errorf("%s: %d instances of %s, want 1", name, n, id)
			}
		}
	}
	if res := snapshot.Verify(ctx, data, s, snapshot.WithRevalidation(diag.Error)); res.Len() != 0 {
		t.Errorf("Verify = %s, want no issue", res)
	}
}

// TestLoad_KeepsAnEmptyGroupOfATypeTheEntrySchemaCannotName pins that a group
// with no items denotes deep's Beacon as a populated one does.
func TestLoad_KeepsAnEmptyGroupOfATypeTheEntrySchemaCannotName(t *testing.T) {
	t.Parallel()
	s, _, _, deepBeacon := unnameableFixture(t)
	doc := snappartstest.Document(t, s, snapparts.Parts{Types: []schema.TypeID{deepBeacon}})
	snap, res := snapshot.Load(t.Context(), doc, s)
	if res.Len() != 0 {
		t.Fatalf("Load = %s, want no issue", res)
	}
	if got := snap.Types(); !slices.Equal(got, []schema.TypeID{deepBeacon}) {
		t.Errorf("Types() = %v, want [%s]", got, deepBeacon)
	}
}

// TestLoad_RevalidatesARootTheEntrySchemaCannotNameAsItsOwnType pins that a bad
// value under deep's Beacon draws deep's finding alone. The entry's Beacon would
// add a missing label and an unknown power.
func TestLoad_RevalidatesARootTheEntrySchemaCannotNameAsItsOwnType(t *testing.T) {
	t.Parallel()
	s, _, _, deepBeacon := unnameableFixture(t)
	doc := snappartstest.Document(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     deepBeacon,
		PrimaryKey: immutable.WrapKey([]any{"d1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "d1", "power": "high"}),
	}}})
	_, res := snapshot.Load(t.Context(), doc, s, snapshot.WithRevalidation(diag.Warning))
	if res.Len() != 1 || !res.HasCode(diag.E_TYPE_MISMATCH) || res.HasErrors() {
		t.Errorf("Load = %s, want one %s at Warning", res, diag.E_TYPE_MISMATCH)
	}
}

// TestLoad_RevalidatesADirectlyImportedRootUnderItsTag pins that a root the
// entry schema names "base.Basin" is judged by the entry's validator under that
// tag: clean data reports nothing, and a bad value's finding names the tag.
func TestLoad_RevalidatesADirectlyImportedRootUnderItsTag(t *testing.T) {
	t.Parallel()
	s, _, _, _ := unnameableFixture(t)
	basin := mustTypeIDIn(t, s, "base", "Basin")
	doc := func(area any) []byte {
		return snappartstest.Document(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID:     basin,
			PrimaryKey: immutable.WrapKey([]any{"b1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": area}),
		}}})
	}
	if _, res := snapshot.Load(t.Context(), doc(float64(2)), s, snapshot.WithRevalidation(diag.Error)); res.Len() != 0 {
		t.Errorf("clean Load = %s, want no issue", res)
	}
	_, res := snapshot.Load(t.Context(), doc("wide"), s, snapshot.WithRevalidation(diag.Warning))
	if res.Len() != 1 || !res.HasCode(diag.E_TYPE_MISMATCH) {
		t.Fatalf("Load = %s, want one %s", res, diag.E_TYPE_MISMATCH)
	}
	for issue := range res.Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeName && d.Value != "base.Basin" {
				t.Errorf("the finding names type %q, want %q", d.Value, "base.Basin")
			}
		}
	}
}

// TestLoad_RevalidationHoldsTheIssueLimitAtATypeTheEntrySchemaCannotName pins
// that the validator of the schema declaring a type the entry schema cannot name
// caps a row at the load's limit, as the entry's validator does.
func TestLoad_RevalidationHoldsTheIssueLimitAtATypeTheEntrySchemaCannotName(t *testing.T) {
	t.Parallel()
	const width = 130
	var props strings.Builder
	values := map[string]any{"id": "w1"}
	for i := range width {
		fmt.Fprintf(&props, "\tp%d Float\n", i)
		values[fmt.Sprintf("p%d", i)] = "bad"
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"base.yammm\" as base\n"),
		"base.yammm":  []byte("schema \"base\"\n\nimport \"deep.yammm\" as deep\n"),
		"deep.yammm":  []byte("schema \"deep\"\n\ntype Wide {\n\tid String primary\n" + props.String() + "}\n"),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load fixture: %s", res)
	}
	var wide schema.TypeID
	for member := range s.Closure() {
		if typ, ok := member.Type("Wide"); ok {
			wide = typ.ID()
		}
	}
	if _, addressable := schema.AddressableTag(s, wide); wide.IsZero() || addressable {
		t.Fatal("fixture is vacuous: the entry schema can name Wide, or it did not resolve")
	}
	doc := snappartstest.Document(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     wide,
		PrimaryKey: immutable.WrapKey([]any{"w1"}),
		Properties: immutable.WrapProperties(values),
	}}})
	for limit, want := range map[int][2]int{0: {width, 0}, 120: {120, width - 120}} {
		_, res := snapshot.Load(t.Context(), doc, s, snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(limit))
		if res.Len() != want[0] || res.DroppedCount() != want[1] {
			t.Errorf("limit %d: %d issues, %d dropped; want %d and %d", limit, res.Len(), res.DroppedCount(), want[0], want[1])
		}
	}
}

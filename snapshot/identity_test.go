package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// crossSchemaSources declares an entry schema importing a second one, so the
// graph carries an instance whose tag form is alias-qualified.
var crossSchemaSources = map[string][]byte{
	"entry.yammm": []byte(`schema "geo"

import "base.yammm" as base

type Anchor {
	id String primary
	depth Float
}
`),
	"base.yammm": []byte(`schema "base"

type Basin {
	id String primary
	area Float
}
`),
}

func loadCrossSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.LoadSourcesWithEntry(t.Context(), crossSchemaSources, "entry.yammm", ".", schema.WithSourcesOnly())
	if result.HasErrors() {
		t.Fatalf("load cross-schema fixture: %s", result)
	}
	return s
}

// A composed child's type identity survives a round trip even when the
// relation is named differently from the type it targets. The wire omits
// type_id for a child the parent relation identifies, so the decoder must
// recover the child's type through that relation and not through its name.
func TestRoundTrip_ComposedChildKeepsTypeIdentity(t *testing.T) {
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}

	sensor, ok := loaded.InstanceByKey(tidOf(t, s, "Sensor"), graph.FormatKey("s1"))
	if !ok {
		t.Fatal("sensor s1 missing after round trip")
	}
	children := sensor.Composed("PARTS")
	if len(children) != 1 {
		t.Fatalf("PARTS children = %d, want 1", len(children))
	}
	child := children[0]
	if got := child.TypeName(); got != "Part" {
		t.Errorf("child TypeName = %q, want %q — the relation name replaced the type", got, "Part")
	}
	if child.TypeID().IsZero() {
		t.Error("child TypeID is zero — the child lost its type identity across the round trip")
	}
	if got := child.TypeID().Name(); got != "Part" {
		t.Errorf("child TypeID name = %q, want %q", got, "Part")
	}
}

// DiffSnapshots must fail on the identity loss above, not only on properties.
// The projection carried PK and properties alone until it did.
func TestDiffSnapshots_SeesComposedChildType(t *testing.T) {
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})
	snapshottest.AssertRoundTrip(t, snap, s)

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	snapshottest.DiffSnapshots(t, snap, loaded)
}

// A document holding an instance of an imported type loads back. Marshal
// writes the alias-qualified tag form into the types array, so every
// decode-side type lookup has to resolve that form and not only local names.
func TestRoundTrip_CrossSchemaInstance(t *testing.T) {
	ctx := context.Background()
	s := loadCrossSchema(t)

	basin, ok := s.ResolveType(schema.NewTypeRef("base", "Basin", location.Span{}))
	if !ok {
		t.Fatal("base.Basin did not resolve through the entry schema")
	}

	anchor := mustValidInstance(t, s, "Anchor", []any{"a1"},
		map[string]any{"id": "a1", "depth": float64(3)})
	imported := instancetest.VI(
		"Basin",
		instancetest.TypeID(basin.ID()),
		instancetest.PK("b1"),
		instancetest.Props(map[string]any{"id": "b1", "area": float64(7)}),
	)
	snap := snapshottest.BuildSnapshot(t, s, anchor, imported)

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"base.Basin"`) {
		t.Fatalf("marshalled types array does not carry the qualified tag form:\n%s", data)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load rejected a document Marshal produced: %v", err)
	}
	if _, ok := loaded.InstanceByKey(tidOf(t, s, "base.Basin"), graph.FormatKey("b1")); !ok {
		t.Error("imported-type instance missing after round trip")
	}
	snapshottest.AssertRoundTrip(t, snap, s)
}

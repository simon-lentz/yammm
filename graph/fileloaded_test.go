package graph_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadFromDisk loads testdata/composed.yammm through [schema.Load]. It is the
// package's only file-backed schema: every other test builds one from a
// synthetic source ID, whose [schema.TypeID] renders no filesystem path and so
// cannot exercise anything that leaks one.
func loadFromDisk(t *testing.T) schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	path, err := filepath.Abs(filepath.Join("testdata", "composed.yammm"))
	if err != nil {
		t.Fatalf("abs testdata path: %v", err)
	}
	s, res := schema.Load(t.Context(), path)
	if res.HasErrors() {
		t.Fatalf("load %s: %s", path, res.String())
	}
	return s
}

// loadFromDiskAt copies testdata/composed.yammm to file and loads it through
// [schema.Load].
func loadFromDiskAt(t *testing.T, file string) schema.Schema {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", "composed.yammm"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if err := os.WriteFile(file, text, 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	s, res := schema.Load(t.Context(), file)
	if res.HasErrors() {
		t.Fatalf("load %s: %s", file, res.String())
	}
	return s
}

// orderWithNotes validates an Order over s holding one Section with a Note per
// id. Its required slots hold what they require.
func orderWithNotes(t *testing.T, s schema.Schema, ids ...string) instance.ValidInstance {
	t.Helper()
	notes := make([]any, len(ids))
	for i, id := range ids {
		notes[i] = map[string]any{"note_id": id, "body": "b"}
	}
	return instancetest.Valid(t, s, "Order", map[string]any{
		"order_id": "o1",
		"customer": "c",
		"sections": []any{map[string]any{"section_id": "s1", "heading": "h", "notes": notes}},
		"invoice":  []any{map[string]any{"invoice_id": "inv1", "amount": "10"}},
		"stamp":    []any{map[string]any{"mark": "paid"}},
	})
}

// TestFileLoadedSchema_RendersAnAbsolutePath is the control. It asserts the
// property that makes the tests below meaningful: loaded from disk, this
// schema's type identities carry the checkout's path. If this ever fails the
// corpus has stopped being a file-backed one and the guards below prove
// nothing.
func TestFileLoadedSchema_RendersAnAbsolutePath(t *testing.T) {
	t.Parallel()
	s := loadFromDisk(t)
	typ, ok := s.Type("Order")
	if !ok {
		t.Fatal("Order not found")
	}
	if !filepath.IsAbs(typ.ID().SchemaPath().String()) {
		t.Fatalf("schema path %q is not absolute; this corpus is no longer file-backed",
			typ.ID().SchemaPath().String())
	}
}

// TestFileLoadedSchema_OneCompositionShapes exercises the two shapes this
// corpus had never carried: a (one) slot to a keyed part, and a (one) slot to
// a keyless one. Every other composition here is (_:many) to a keyed part, so
// nothing loaded from disk reached the address a (one) slot writes.
func TestFileLoadedSchema_OneCompositionShapes(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	file := filepath.Join(t.TempDir(), "composed.yammm")
	s := loadFromDiskAt(t, file)
	ctx := t.Context()

	order := orderWithNotes(t, s, "n1")

	g := graph.New(s)
	if res := g.Add(ctx, order); !res.OK() {
		t.Fatalf("a (one) composition to a keyed and a keyless part was refused: %s", res.String())
	}

	roots := slices.Collect(g.Snapshot().InstancesOf(mustTypeID(t, s, "Order")))
	if len(roots) != 1 {
		t.Fatalf("got %d Order instances; want 1", len(roots))
	}

	// The SHAPE, not just the count: one child in a slot is what a (_:many)
	// slot holds too, so counting alone stays green if the schema's (one)
	// becomes (_:many). Assert the cardinality the fixture declares, and that
	// the slot refuses a second occupant.
	orderType, ok := s.Type("Order")
	if !ok {
		t.Fatal("Order is not in the schema")
	}
	for _, rel := range []string{"INVOICE", "STAMP"} {
		if n := roots[0].ComposedCount(rel); n != 1 {
			t.Errorf("%s holds %d composed children; want 1", rel, n)
		}
		r, ok := orderType.Relation(rel)
		if !ok {
			t.Fatalf("%s is not a relation on Order", rel)
		}
		if r.IsMany() {
			t.Errorf("%s is declared (many); the fixture's whole point is a (one) slot", rel)
		}
	}
}

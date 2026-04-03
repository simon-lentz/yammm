package neo4j

import (
	"slices"
	"testing"
)

func TestShapeForSchema_Basic(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns, ok := shape.Types["Entity"]
	if !ok {
		t.Fatal("missing Entity in shape")
	}
	if ns.Label != "basic_test__Entity" {
		t.Errorf("Label = %q; want %q", ns.Label, "basic_test__Entity")
	}
	if !slices.Equal(ns.PrimaryKeys, []string{"id"}) {
		t.Errorf("PrimaryKeys = %v; want [id]", ns.PrimaryKeys)
	}
	// RequiredFields: id, name, count, active, created_at.
	for _, want := range []string{"id", "name", "count", "active", "created_at"} {
		if !slices.Contains(ns.RequiredFields, want) {
			t.Errorf("RequiredFields missing %q; got %v", want, ns.RequiredFields)
		}
	}
}

func TestShapeForSchema_CompositePK(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns := shape.Types["Record"]
	if !slices.Equal(ns.PrimaryKeys, []string{"schema_id", "record_id"}) {
		t.Errorf("PrimaryKeys = %v; want [schema_id record_id]", ns.PrimaryKeys)
	}
	for _, want := range []string{"schema_id", "record_id", "name"} {
		if !slices.Contains(ns.RequiredFields, want) {
			t.Errorf("RequiredFields missing %q; got %v", want, ns.RequiredFields)
		}
	}
}

func TestShapeForSchema_SkipsAbstract(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "abstract_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	if _, ok := shape.Types["Base"]; ok {
		t.Error("abstract type Base should not be in Types")
	}
	if _, ok := shape.Types["Widget"]; !ok {
		t.Error("concrete type Widget should be in Types")
	}
}

func TestShapeForSchema_PartTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "part_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	if _, ok := shape.Types["LineItem"]; !ok {
		t.Error("part type LineItem should be in Types")
	}
	if _, ok := shape.Types["Order"]; !ok {
		t.Error("type Order should be in Types")
	}
}

func TestShapeForSchema_Inheritance(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "inheritance.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	// Tracked is abstract — should not appear.
	if _, ok := shape.Types["Tracked"]; ok {
		t.Error("abstract type Tracked should not be in Types")
	}

	ns, ok := shape.Types["Entity"]
	if !ok {
		t.Fatal("missing Entity in shape")
	}
	// Inherited required fields from Tracked.
	for _, want := range []string{"run_id", "source_fetched_at", "id", "name"} {
		if !slices.Contains(ns.RequiredFields, want) {
			t.Errorf("RequiredFields missing inherited %q; got %v", want, ns.RequiredFields)
		}
	}
}

func TestShapeForSchema_MultipleTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	widget, ok := shape.Types["Widget"]
	if !ok {
		t.Fatal("missing Widget")
	}
	if !slices.Equal(widget.PrimaryKeys, []string{"id"}) {
		t.Errorf("Widget PrimaryKeys = %v; want [id]", widget.PrimaryKeys)
	}

	gadget, ok := shape.Types["Gadget"]
	if !ok {
		t.Fatal("missing Gadget")
	}
	if !slices.Equal(gadget.PrimaryKeys, []string{"uid", "sku"}) {
		t.Errorf("Gadget PrimaryKeys = %v; want [uid sku]", gadget.PrimaryKeys)
	}
}

func TestShapeForSchema_CustomSeparator(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithLabelSeparator("_"))

	shape, result := a.ShapeForSchema(s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns := shape.Types["Entity"]
	if ns.Label != "basic_test_Entity" {
		t.Errorf("Label = %q; want %q", ns.Label, "basic_test_Entity")
	}
}

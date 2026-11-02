package neo4j

import (
	"context"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestShapeForSchema_Basic(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns, ok := shape.types[typeID(t, s, "Entity")]
	if !ok {
		t.Fatal("missing Entity in shape")
	}
	if ns.label != "basic_test__Entity" {
		t.Errorf("Label = %q; want %q", ns.label, "basic_test__Entity")
	}
	if !slices.Equal(ns.primaryKeys, []string{"id"}) {
		t.Errorf("PrimaryKeys = %v; want [id]", ns.primaryKeys)
	}
}

func TestShapeForSchema_CompositePK(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns := shape.types[typeID(t, s, "Record")]
	if !slices.Equal(ns.primaryKeys, []string{"schema_id", "record_id"}) {
		t.Errorf("PrimaryKeys = %v; want [schema_id record_id]", ns.primaryKeys)
	}
}

func TestShapeForSchema_SkipsAbstract(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "abstract_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	if _, ok := shape.types[typeID(t, s, "Base")]; ok {
		t.Error("abstract type Base should not be in Types")
	}
	if _, ok := shape.types[typeID(t, s, "Widget")]; !ok {
		t.Error("concrete type Widget should be in Types")
	}
}

// TestShapeForSchema_ImportedPartThroughClosure pins the closure walk: a part
// type declared by an imported schema gets a shape under the DECLARING
// schema's label, so the composition phases can render it.
func TestShapeForSchema_ImportedPartThroughClosure(t *testing.T) {
	t.Parallel()
	const entry = `schema "entry"

import "base.yammm" as base

type Wrapper {
	id String primary
	*-> HOLDS (_:many) base.Item
}
`
	const baseSrc = `schema "base"

part type Item {
	label String required
}
`
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(entry),
		"base.yammm":  []byte(baseSrc),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	item, ok := s.ResolveType(schema.NewTypeRef("base", "Item", location.Span{}))
	if !ok {
		t.Fatal("base.Item did not resolve through the entry schema")
	}

	shape, result := New().ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}
	ns, ok := shape.types[item.ID()]
	if !ok {
		t.Fatal("imported part type base.Item has no shape")
	}
	if ns.label != "base__Item" {
		t.Errorf("imported part label = %q; want %q (the declaring schema's name)", ns.label, "base__Item")
	}
}

func TestShapeForSchema_PartTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "part_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	if _, ok := shape.types[typeID(t, s, "LineItem")]; !ok {
		t.Error("part type LineItem should be in Types")
	}
	if _, ok := shape.types[typeID(t, s, "Order")]; !ok {
		t.Error("type Order should be in Types")
	}
}

func TestShapeForSchema_Inheritance(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "inheritance.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	// Tracked is abstract — should not appear.
	if _, ok := shape.types[typeID(t, s, "Tracked")]; ok {
		t.Error("abstract type Tracked should not be in Types")
	}

	if _, ok := shape.types[typeID(t, s, "Entity")]; !ok {
		t.Fatal("missing Entity in shape")
	}
}

func TestShapeForSchema_MultipleTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	a := New()

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	widget, ok := shape.types[typeID(t, s, "Widget")]
	if !ok {
		t.Fatal("missing Widget")
	}
	if !slices.Equal(widget.primaryKeys, []string{"id"}) {
		t.Errorf("Widget PrimaryKeys = %v; want [id]", widget.primaryKeys)
	}

	gadget, ok := shape.types[typeID(t, s, "Gadget")]
	if !ok {
		t.Fatal("missing Gadget")
	}
	if !slices.Equal(gadget.primaryKeys, []string{"uid", "sku"}) {
		t.Errorf("Gadget PrimaryKeys = %v; want [uid sku]", gadget.primaryKeys)
	}
}

func TestShapeForSchema_CustomSeparator(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithLabelSeparator("_"))

	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema failed: %v", err)
	}

	ns := shape.types[typeID(t, s, "Entity")]
	if ns.label != "basic_test_Entity" {
		t.Errorf("Label = %q; want %q", ns.label, "basic_test_Entity")
	}
}

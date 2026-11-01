package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestHandles_IsZeroTellsARecordFromTheZeroValue(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "h"

type T {
	id String primary
	state String required @index
	! "has_id" id != ""
}

type Word = String
`, "h.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	prop, ok := typ.Property("state")
	if !ok {
		t.Fatal("no property state")
	}
	ann, ok := prop.Annotation("index")
	if !ok {
		t.Fatal("no annotation index")
	}
	dt, ok := s.DataType("Word")
	if !ok {
		t.Fatal("no datatype Word")
	}
	var inv schema.Invariant
	for inv = range typ.Invariants() {
		break
	}

	for name, zero := range map[string]bool{
		"Schema":     s.IsZero(),
		"Type":       typ.IsZero(),
		"Property":   prop.IsZero(),
		"Annotation": ann.IsZero(),
		"DataType":   dt.IsZero(),
		"Invariant":  inv.IsZero(),
		"Sources":    s.Sources().IsZero(),
	} {
		if zero {
			t.Errorf("a loaded %s must not be zero", name)
		}
	}
	for name, zero := range map[string]bool{
		"Schema":     schema.Schema{}.IsZero(),
		"Type":       schema.Type{}.IsZero(),
		"Property":   schema.Property{}.IsZero(),
		"Relation":   schema.Relation{}.IsZero(),
		"DataType":   schema.DataType{}.IsZero(),
		"Invariant":  schema.Invariant{}.IsZero(),
		"Annotation": schema.Annotation{}.IsZero(),
		"Import":     schema.Import{}.IsZero(),
		"Sources":    schema.Sources{}.IsZero(),
	} {
		if !zero {
			t.Errorf("the zero %s must report zero", name)
		}
	}
}

func TestLookups_AMissReturnsTheZeroHandleAndFalse(t *testing.T) {
	s, res := schema.LoadString(t.Context(), "schema \"m\"\n\ntype T {\n\tid String primary\n}\n", "m.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	real, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	check := func(name string, zero, ok bool) {
		t.Helper()
		if !zero || ok {
			t.Errorf("%s: zero = %v, ok = %v, want true and false", name, zero, ok)
		}
	}
	typ, ok := s.Type("Absent")
	check("Schema.Type", typ.IsZero(), ok)
	dt, ok := s.DataType("Absent")
	check("Schema.DataType", dt.IsZero(), ok)
	imp, ok := s.ImportByAlias("absent")
	check("Schema.ImportByAlias", imp.IsZero(), ok)
	prop, ok := real.Property("absent")
	check("Type.Property", prop.IsZero(), ok)
	rel, ok := real.Relation("ABSENT")
	check("Type.Relation", rel.IsZero(), ok)
	ann, ok := prop0(t, real).Annotation("absent")
	check("Property.Annotation", ann.IsZero(), ok)
}

func prop0(t *testing.T, typ schema.Type) schema.Property {
	t.Helper()
	p, ok := typ.Property("id")
	if !ok {
		t.Fatal("no property id")
	}
	return p
}

func TestAddressableTag_AZeroSchemaAddressesNothing(t *testing.T) {
	s, res := schema.LoadString(t.Context(), "schema \"m\"\n\ntype T {\n\tid String primary\n}\n", "m.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	if tag, ok := schema.AddressableTag(schema.Schema{}, typ.ID()); tag != "" || ok {
		t.Errorf("AddressableTag = %q, %v, want none", tag, ok)
	}
}

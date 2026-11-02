package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

const recordLookupSource = `schema "main"

type Word = String

type Words = List<Word>

abstract type Named {
	name String required
}

part type Line {
	sku String primary
}

type Company extends Named {
	id String primary
	state String required @index
	tag Word
	tags List<Word>
	@@index(id, name)
}

type Person extends Named {
	id String primary
	note String
	--> WORKS_AT (one) Company
	*-> LINES (many) Line
}
`

func TestSchema_EveryLookupNamesOneRecord(t *testing.T) {
	s, res := schema.LoadString(t.Context(), recordLookupSource, "main.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}

	company := mustType(t, s, "Company")
	byID, ok := s.TypeByID(company.ID())
	if !ok || company != byID {
		t.Error("a lookup by name and a lookup by identity must name one record")
	}

	state, ok := company.Property("state")
	if !ok {
		t.Fatal("no property state")
	}
	if _, ok := state.Annotation("index"); !ok {
		t.Error("no annotation index on state")
	}
	for _, name := range []string{"tag", "tags"} {
		prop, ok := company.Property(name)
		if !ok {
			t.Fatalf("no property %s", name)
		}
		dt, ok := s.ResolveDataType(prop.DataTypeRef())
		if !ok || dt.Name() != "Word" {
			t.Errorf("%s: DataTypeRef resolves to %v, %v, want Word", name, dt, ok)
		}
	}

	rel, ok := mustType(t, s, "Person").Relation("WORKS_AT")
	if !ok {
		t.Fatal("no relation WORKS_AT")
	}
	byField, ok := mustType(t, s, "Person").RelationByField(rel.FieldName())
	if !ok || rel != byField {
		t.Error("a lookup by name and by field must name one record")
	}
}

func mustType(t *testing.T, s schema.Schema, name string) schema.Type {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("no type %s", name)
	}
	return typ
}

func TestSchema_TypesAndDataTypesYieldDeclarationOrder(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "order"

type Zebra {
	id String primary
}

type Apple {
	id String primary
}

type Mango {
	id String primary
}

type Word = String

type Alpha = Integer
`, "order.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}

	var types, dataTypes []string
	for typ := range s.Types() {
		types = append(types, typ.Name())
	}
	for dt := range s.DataTypes() {
		dataTypes = append(dataTypes, dt.Name())
	}
	yammmtest.Diff(t, []string{"Zebra", "Apple", "Mango"}, types)
	yammmtest.Diff(t, []string{"Word", "Alpha"}, dataTypes)
}

func TestSchema_EveryPropertyFormNamesItsDataType(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "b"

type Code = String[1, 3]

type Doc {
	id Code primary
	required Code required
	optional Code
}
`, "b.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	doc := mustType(t, s, "Doc")
	for _, name := range []string{"id", "required", "optional"} {
		prop, ok := doc.Property(name)
		if !ok {
			t.Fatalf("no property %s", name)
		}
		if dt, ok := s.ResolveDataType(prop.DataTypeRef()); !ok || dt.Name() != "Code" {
			t.Errorf("%s: DataTypeRef resolves to %v, %v, want Code", name, dt, ok)
		}
	}
}

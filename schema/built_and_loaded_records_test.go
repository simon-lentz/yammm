package schema_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/internal/recordtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const builtAndLoadedSource = `schema "main"

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

// A builder reads no source, so it holds no span and no source text, and neither is
// compared. Every other accessor answers the same.
func builtAndLoadedOptions() recordtest.Options {
	return recordtest.Options{
		OmitTypes: []reflect.Type{reflect.TypeFor[location.Span]()},
		SkipMethod: func(owner, method string) bool {
			return method == "Sources" || method == "HasSourceProvider"
		},
	}
}

func TestBuilder_EveryRecordAccessorAnswersAsTheLoader(t *testing.T) {
	loaded, res := schema.LoadString(t.Context(), builtAndLoadedSource, "main.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}

	built, res := schema.NewBuilder().
		WithName("main").
		WithSourceID(loaded.SourceID()).
		AddDataType("Word", schema.NewStringConstraint()).
		AddDataType("Words", schema.NewListConstraint(schema.NewAliasConstraint("Word", nil))).
		AddType("Named").AsAbstract().
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		AddType("Line").AsPart().
		WithPrimaryKey("sku", schema.NewStringConstraint()).
		Done().
		AddType("Company").Extends(schema.LocalTypeRef("Named", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithTypeAnnotation("index", "id", "name").
		WithProperty("state", schema.NewStringConstraint()).
		WithPropertyAnnotation("state", "index").
		WithOptionalProperty("tag", schema.NewAliasConstraint("Word", nil)).
		WithOptionalProperty("tags", schema.NewListConstraint(schema.NewAliasConstraint("Word", nil))).
		Done().
		AddType("Person").Extends(schema.LocalTypeRef("Named", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithOptionalProperty("note", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.LocalTypeRef("Company", location.Span{}), false, false).
		WithComposition("LINES", schema.LocalTypeRef("Line", location.Span{}), true, true).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatal(res)
	}

	yammmtest.Diff(t, recordtest.Walk(loaded, builtAndLoadedOptions()), recordtest.Walk(built, builtAndLoadedOptions()))

	for _, s := range []schema.Schema{loaded, built} {
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

func TestRegistry_AllIsTheSetAtTheCallAndItsLoopHoldsNoLock(t *testing.T) {
	load := func(name string) schema.Schema {
		s, res := schema.LoadString(t.Context(), "schema \""+name+"\"\n\ntype T {\n\tid String primary\n}\n", name+".yammm")
		if res.HasErrors() {
			t.Fatal(res)
		}
		return s
	}
	reg := schema.NewRegistry()
	if err := reg.Register(load("first")); err != nil {
		t.Fatal(err)
	}
	all := reg.All()
	if err := reg.Register(load("before_the_range")); err != nil {
		t.Fatal(err)
	}
	inLoop := load("in_the_loop")

	var names []string
	for s := range all {
		// A Register inside the loop would deadlock on a loop that held the lock.
		if err := reg.Register(inLoop); err != nil {
			t.Fatal(err)
		}
		names = append(names, s.Name())
	}
	yammmtest.Diff(t, []string{"first"}, names)
	if got := len(slices.Collect(reg.All())); got != 3 {
		t.Errorf("the registry holds %d schemas, want 3", got)
	}
}

func TestBuilder_EveryPropertyFormNamesItsDataType(t *testing.T) {
	code := func() schema.Constraint { return schema.NewAliasConstraint("Code", nil) }
	s, res := schema.NewBuilder().WithName("b").
		AddDataType("Code", schema.StringLenBetween(1, 3)).
		AddType("Doc").WithPrimaryKey("id", code()).
		WithProperty("required", code()).
		WithOptionalProperty("optional", code()).Done().Build()
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

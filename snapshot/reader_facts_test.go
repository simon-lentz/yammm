package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func loadFactSchema(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func factTypeID(t *testing.T, s schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("%s type missing", name)
	}
	return typ.ID()
}

func requireReadersRefuse(t *testing.T, doc []byte, s schema.Schema, code diag.Code) {
	t.Helper()
	if _, res := snapshot.Load(t.Context(), doc, s); !res.HasCode(code) {
		t.Errorf("Load = %s, want %s", res, code)
	}
	if res := snapshot.Verify(t.Context(), doc, s); !res.HasCode(code) {
		t.Errorf("Verify = %s, want %s", res, code)
	}
}

// The undeclared-name rule holds below the first level of a composition tree.
func TestRead_RefusesAnUndeclaredPropertyAtAGrandchild(t *testing.T) {
	t.Parallel()
	s := loadFactSchema(t, `schema "facts"

part type Card {
	last4 String primary
}

part type Account {
	number String primary
	*-> CARDS (_:many) Card
}

type Holder {
	id String primary
	*-> ACCOUNTS (_:many) Account
}
`)
	key := func(k string) immutable.Key { return immutable.WrapKey([]any{k}) }
	doc := snappartstest.Document(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     factTypeID(t, s, "Holder"),
		PrimaryKey: key("h"),
		Properties: immutable.WrapProperties(map[string]any{"id": "h"}),
		Composed: map[string][]snapparts.Instance{"ACCOUNTS": {{
			TypeID:     factTypeID(t, s, "Account"),
			PrimaryKey: key("a"),
			Properties: immutable.WrapProperties(map[string]any{"number": "a"}),
			Composed: map[string][]snapparts.Instance{"CARDS": {{
				TypeID:     factTypeID(t, s, "Card"),
				PrimaryKey: key("c"),
				Properties: immutable.WrapProperties(map[string]any{"last4": "c", "ghost": "x"}),
			}}},
		}}},
	}}})
	requireReadersRefuse(t, doc, s, diag.E_SNAPSHOT_MALFORMED)
}

// A bool key component agrees with its key property by value, as any other does.
func TestRead_RefusesABoolKeyThatDisagreesWithItsProperty(t *testing.T) {
	t.Parallel()
	s := loadFactSchema(t, `schema "facts"

type Item {
	id String primary
}
`)
	parts := func(k, prop bool) snapparts.Parts {
		return snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID:     factTypeID(t, s, "Item"),
			PrimaryKey: immutable.WrapKey([]any{k}),
			Properties: immutable.WrapProperties(map[string]any{"id": prop}),
		}}}
	}
	requireReadersRefuse(t, snappartstest.Document(t, s, parts(true, false)), s, diag.E_SNAPSHOT_MALFORMED)
	if _, res := snapshot.Load(t.Context(), snappartstest.Document(t, s, parts(true, true)), s); res.HasErrors() {
		t.Errorf("a key that agrees with its property: Load = %s, want no error", res)
	}
}

// A duplicate record of a type the entry schema cannot name reads back as a
// root of it does.
func TestRead_KeepsADuplicateOfATypeTheEntrySchemaCannotName(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(unreachableRootEntry),
		"base.yammm":  []byte(unreachableRootBase),
		"deep.yammm":  []byte(unreachableRootDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	base, ok := s.ImportByAlias("base")
	if !ok {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok {
		t.Fatal("import alias deep did not resolve")
	}
	beacon := factTypeID(t, deep.Schema(), "Beacon")
	record := snapparts.Instance{
		TypeID:     beacon,
		PrimaryKey: immutable.WrapKey([]any{"b"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "b"}),
	}
	doc := snappartstest.Document(t, s, snapparts.Parts{
		Instances:  []snapparts.Instance{record},
		Duplicates: []snapparts.Instance{record},
	})
	if _, res := snapshot.Load(t.Context(), doc, s); res.HasErrors() {
		t.Errorf("Load = %s, want no error", res)
	}
	if res := snapshot.Verify(t.Context(), doc, s); res.HasErrors() {
		t.Errorf("Verify = %s, want no error", res)
	}
}

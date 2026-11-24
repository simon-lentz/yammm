package graph_test

import (
	"context"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// A type the entry schema cannot name: one reached only through an
// intermediate import. A graph holds it by identity, as a root and as a
// composed child, apart from the entry's own type of the same name.

const unnameableEntry = `schema "entry"

import "base.yammm" as base

type Beacon {
	id String primary
	power Float
}
`

const unnameableBase = `schema "base"

import "deep.yammm" as deep

part type Fragment {
	name String primary
	mass Float
}

type Basin {
	id String primary
	*-> PIECES (many) deep.Shard
}
`

const unnameableDeep = `schema "deep"

part type Shard {
	name String primary
	density Float
}

type Beacon {
	id String primary
	power Float
}
`

// unnameableFixture loads the three-schema closure and returns the entry
// schema with the transitively imported Beacon's identity.
func unnameableFixture(t *testing.T) (schema.Schema, schema.TypeID) {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(unnameableEntry),
		"base.yammm":  []byte(unnameableBase),
		"deep.yammm":  []byte(unnameableDeep),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load fixture: %s", err)
	}
	base, ok := s.ImportByAlias("base")
	if !ok || base.Schema().IsZero() {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok || deep.Schema().IsZero() {
		t.Fatal("import alias deep did not resolve")
	}
	typ, ok := deep.Schema().Type("Beacon")
	if !ok {
		t.Fatal("deep.Beacon not found")
	}
	if _, addressable := schema.AddressableTag(s, typ.ID()); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Beacon")
	}
	return s, typ.ID()
}

// TestRebuild_KeepsAnUnnameableComposedChild pins that a rebuilt graph keeps a
// composed child of a transitively imported part type under its identity.
func TestRebuild_KeepsAnUnnameableComposedChild(t *testing.T) {
	t.Parallel()
	s, _ := unnameableFixture(t)

	basin, ok := mustImportedType(t, s, "base", "Basin")
	if !ok {
		return
	}
	base, _ := s.ImportByAlias("base")
	deep, _ := base.Schema().ImportByAlias("deep")
	shard, found := deep.Schema().Type("Shard")
	if !found {
		t.Fatal("deep.Shard not found")
	}
	if _, addressable := schema.AddressableTag(s, shard.ID()); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Shard")
	}

	built := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{basin},
		Instances: []snapparts.Instance{
			{
				TypeID:     basin,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1"}),
				Composed: map[string][]snapparts.Instance{
					"PIECES": {{
						TypeID:     shard.ID(),
						PrimaryKey: immutable.WrapKey([]any{"s1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "s1", "density": float64(3)}),
					}},
				},
			},
		},
	})
	roots := slices.Collect(built.InstancesOf(basin))
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	children := slices.Collect(roots[0].Composed("PIECES"))
	if len(children) != 1 {
		t.Fatalf("got %d composed children, want 1", len(children))
	}
	if got := children[0].TypeID(); got != shard.ID() {
		t.Errorf("composed child carries type %s, want %s", got, shard.ID())
	}
}

// TestGraphAdd_KeepsAnUnnameableRootApartFromItsNamesake pins that Add takes a
// root of a type the entry schema cannot name, under either schema's spelling,
// and holds it apart from the entry's Beacon of the same name and key.
func TestGraphAdd_KeepsAnUnnameableRootApartFromItsNamesake(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)
	entryBeacon, ok := s.Type("Beacon")
	if !ok {
		t.Fatal("entry Beacon not found")
	}
	base, _ := s.ImportByAlias("base")
	deep, _ := base.Schema().ImportByAlias("deep")

	for _, c := range []struct {
		over     schema.Schema
		spelling string
	}{{base.Schema(), "deep.Beacon"}, {deep.Schema(), "Beacon"}} {
		g := graph.New(s)
		props := map[string]any{"id": "d1", "power": float64(1)}
		for _, valid := range []instance.ValidInstance{
			instancetest.Valid(t, s, "Beacon", props),
			instancetest.Valid(t, c.over, c.spelling, props),
		} {
			if err := g.Add(context.Background(), valid); err != nil {
				t.Fatalf("spelled %q: Add refused %s: %s", c.spelling, valid.TypeName(), err)
			}
		}
		snap := g.Snapshot()
		if got := snap.Types(); len(got) != 2 || !slices.Contains(got, deepBeacon) || !slices.Contains(got, entryBeacon.ID()) {
			t.Errorf("spelled %q: Types() = %v, want both Beacons", c.spelling, got)
		}
		for _, id := range []schema.TypeID{deepBeacon, entryBeacon.ID()} {
			if n := len(slices.Collect(snap.InstancesOf(id))); n != 1 {
				t.Errorf("spelled %q: %d instances of %s, want 1", c.spelling, n, id)
			}
		}
	}
}

// mustImportedType resolves a directly imported type, reporting rather than
// failing so a caller can skip the rest of its assertions.
func mustImportedType(t *testing.T, s schema.Schema, alias, name string) (schema.TypeID, bool) {
	t.Helper()
	imp, ok := s.ImportByAlias(alias)
	if !ok || imp.Schema().IsZero() {
		t.Errorf("import alias %q did not resolve", alias)
		return schema.TypeID{}, false
	}
	typ, ok := imp.Schema().Type(name)
	if !ok {
		t.Errorf("type %q not found in the schema imported as %q", name, alias)
		return schema.TypeID{}, false
	}
	return typ.ID(), true
}

// TestGraphAdd_NamesAnUnnameableRootApartFromItsNamesake pins that a root of a
// type the entry schema cannot name carries its declaring schema's name, so a
// duplicate of it and a duplicate of the entry's Beacon at one key draw two
// E_DUPLICATE_PK issues that name two types.
func TestGraphAdd_NamesAnUnnameableRootApartFromItsNamesake(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)
	base, _ := s.ImportByAlias("base")
	deep, _ := base.Schema().ImportByAlias("deep")

	g := graph.New(s)
	props := map[string]any{"id": "d1", "power": float64(1)}
	var named []string
	for range 2 {
		for _, valid := range []instance.ValidInstance{
			instancetest.Valid(t, s, "Beacon", props),
			instancetest.Valid(t, deep.Schema(), "Beacon", props),
		} {
			for issue := range diagtest.Failures(t, g.Add(context.Background(), valid)).Issues() {
				if issue.Code() != diag.E_DUPLICATE_PK {
					t.Fatalf("Add raised %s, want only E_DUPLICATE_PK", issue.Code())
				}
				for _, d := range issue.Details() {
					if d.Key == diag.DetailKeyTypeName {
						named = append(named, d.Value)
					}
				}
			}
		}
	}
	slices.Sort(named)
	if want := []string{"Beacon", "deep#Beacon"}; !slices.Equal(named, want) {
		t.Errorf("the duplicates name %v, want %v", named, want)
	}
	for inst := range g.Snapshot().InstancesOf(deepBeacon) {
		if got := inst.TypeName(); got != "deep#Beacon" {
			t.Errorf("the deep root's TypeName = %q, want %q", got, "deep#Beacon")
		}
	}
}

// TestGraphAdd_NamesARefusedTypeAsTheGraphRendersIt pins that Add's refusals of
// a composed child read out as a root, and of a duplicate root, name the type by
// the graph's rendering, not by the validating schema's own name.
func TestGraphAdd_NamesARefusedTypeAsTheGraphRendersIt(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(`schema "entry"

import "base.yammm" as base
`),
		"base.yammm": []byte(`schema "base"

import "deep.yammm" as deep

type Basin {
	id String primary
	*-> MARKS (many) deep.Mark
	*-> PIECES (many) deep.Shard
}
`),
		"deep.yammm": []byte(`schema "deep"

part type Mark {
	note String
}

part type Shard {
	name String primary
}
`),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load fixture: %s", err)
	}
	base, _ := s.ImportByAlias("base")
	basin := instancetest.Valid(t, base.Schema(), "Basin", map[string]any{
		"id":     "b1",
		"marks":  []any{map[string]any{"note": "n"}},
		"pieces": []any{map[string]any{"name": "s1"}},
	})

	children := map[string]instance.ValidInstance{}
	for _, rel := range []string{"MARKS", "PIECES"} {
		for child := range basin.Composed(rel) {
			children[rel] = child
			break
		}
	}
	for rel, want := range map[string]struct {
		code diag.Code
		name string
	}{
		"MARKS":  {diag.E_GRAPH_MISSING_PK, "deep#Mark"},
		"PIECES": {diag.E_GRAPH_INVALID_COMPOSITION, "deep#Shard"},
	} {
		res := diagtest.Failures(t, graph.New(s).Add(context.Background(), children[rel]))
		if !res.HasCode(want.code) {
			t.Fatalf("%s: Add drew %s, want %s", rel, res, want.code)
		}
		if got := refusedTypeNames(res); !slices.Equal(got, []string{want.name}) {
			t.Errorf("%s: the refusal names %q, want [%q]", rel, got, want.name)
		}
	}

	g := graph.New(s)
	if err := g.Add(context.Background(), basin); err != nil {
		t.Fatalf("first Add: %s", err)
	}
	res := diagtest.Failures(t, g.Add(context.Background(), basin))
	if !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Fatalf("a duplicate Add drew %s, want %s", res, diag.E_DUPLICATE_PK)
	}
	if got := refusedTypeNames(res); !slices.Equal(got, []string{"base.Basin"}) {
		t.Errorf("the duplicate names %q, want [%q]", got, "base.Basin")
	}
}

// refusedTypeNames returns the type_name detail of each issue in res.
func refusedTypeNames(res diag.Result) []string {
	var names []string
	for issue := range res.Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyTypeName {
				names = append(names, d.Value)
			}
		}
	}
	return names
}

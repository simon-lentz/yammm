package graph_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// The root type the entry schema cannot name.
//
// A root is keyed by name in every output document, and a type reached only
// through an intermediate import has no name form. Add and the reader refuse one
// so no writer has to check for a rendered-name collision: two types the entry
// schema can name never render alike.

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
	s, result := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(unnameableEntry),
		"base.yammm":  []byte(unnameableBase),
		"deep.yammm":  []byte(unnameableDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load fixture: %s", result)
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

// TestRebuild_KeepsAnUnnameableComposedChild is the other half of the rule: a
// composed child is addressed through its parent, never by name, so any type in
// the closure stays legal there, a transitively imported part type included.
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

// TestGraphAdd_UnnameableRootRefused pins that Add applies the rule by the shared
// predicate rather than by its own copy, and that the refusal carries the
// transitive-import hint and the declaring schema under
// any spelling of the name: the identity decides, not a dot in the caller's
// string.
//
// The BEHAVIOUR here predates the shared predicate — TestIntegration_ComplexMultiSchema
// already drove Add's refusal — so what this adds is the diagnostic code at a
// named site, not new coverage.
func TestGraphAdd_UnnameableRootRefused(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)

	base, _ := s.ImportByAlias("base")
	deep, _ := base.Schema().ImportByAlias("deep")
	typ, _ := deep.Schema().Type("Beacon")

	for _, c := range []struct {
		over     schema.Schema
		spelling string
	}{{base.Schema(), "deep.Beacon"}, {deep.Schema(), "Beacon"}} {
		spelling := c.spelling
		valid := instancetest.Valid(t, c.over, spelling, map[string]any{"id": "d1", "power": float64(1)})

		addResult := graph.New(s).Add(context.Background(), valid)
		if !addResult.HasErrors() {
			t.Fatalf("Add accepted a root the entry schema cannot name, spelled %q", spelling)
		}
		var sawHint, sawSchema bool
		for issue := range addResult.Issues() {
			if issue.Code() != diag.E_GRAPH_TYPE_NOT_FOUND {
				continue
			}
			sawHint = strings.Contains(issue.Hint(), "transitively imported schema")
			for _, d := range issue.Details() {
				if d.Key == diag.DetailKeyTypeSchema && d.Value == typ.ID().SchemaPath().String() {
					sawSchema = true
				}
			}
		}
		if !resultHasCode(addResult, diag.E_GRAPH_TYPE_NOT_FOUND) || !sawHint || !sawSchema {
			t.Errorf("spelled %q: Add reported %s, want %s with the transitive-import hint and type_schema", spelling, addResult, diag.E_GRAPH_TYPE_NOT_FOUND)
		}
	}
	if got := deepBeacon; got != typ.ID() {
		t.Errorf("the fixture and the resolved type disagree: %s, %s", got, typ.ID())
	}
}

// resultHasCode reports whether the result carries an issue with the code.
func resultHasCode(result diag.Result, code diag.Code) bool {
	for issue := range result.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
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

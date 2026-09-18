package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The root type the entry schema cannot name.
//
// A root is keyed by name in every output document, and a type reached only
// through an intermediate import has no name form. Both constructors refuse one
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
func unnameableFixture(t *testing.T) (*schema.Schema, schema.TypeID) {
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
	if !ok || base.Schema() == nil {
		t.Fatal("import alias base did not resolve")
	}
	deep, ok := base.Schema().ImportByAlias("deep")
	if !ok || deep.Schema() == nil {
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

func beaconParts(s *schema.Schema, id schema.TypeID, key string) graph.InstanceParts {
	return graph.InstanceParts{
		TypeName:   schema.TagForm(s, id),
		TypeID:     id,
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{"id": key, "power": float64(1)}),
	}
}

// TestRebuildSnapshot_UnnameableRootRefused pins the constructor refusal the
// writers rely on. This is the path a persisted document takes, and it is the
// only way a snapshot could otherwise come to hold such a root.
//
// Mutation: dropping the schema.Addressable arm from validatePartsRootTypes
// turns this red.
func TestRebuildSnapshot_UnnameableRootRefused(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)

	_, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{deepBeacon},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			deepBeacon: {beaconParts(s, deepBeacon, "d1")},
		},
	})
	if !result.HasErrors() {
		t.Fatal("a root the entry schema cannot name was accepted")
	}
	if !resultHasCode(result, diag.E_INTERNAL) {
		t.Errorf("refusal reported %s, want %s", result, diag.E_INTERNAL)
	}
	if msg := result.String(); !strings.Contains(msg, "intermediate import") {
		t.Errorf("refusal %q does not say why the type has no name", msg)
	}
}

// TestRebuildSnapshot_UnnameableRootDuplicateRefused covers the other position
// a root identity occupies. A root duplicate is a rejected root instance, so
// its type must be able to hold one; a COMPOSED duplicate may name a part type
// and is not held to the rule.
//
// Mutation: restricting the Addressable arm to parts.Instances turns this red.
func TestRebuildSnapshot_UnnameableRootDuplicateRefused(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)

	// ConflictType and ConflictKey are filled because validatePartsIdentity
	// refuses a zero conflict identity on its own, which would make HasErrors
	// below true whether or not the rule's duplicates arm exists.
	_, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{deepBeacon},
		Duplicates: []graph.DuplicateParts{{
			Type:         deepBeacon,
			Key:          immutable.WrapKey([]any{"d1"}),
			Instance:     beaconParts(s, deepBeacon, "d1"),
			ConflictType: deepBeacon,
			ConflictKey:  immutable.WrapKey([]any{"d1"}),
		}},
	})
	if !result.HasErrors() {
		t.Fatal("a root duplicate the entry schema cannot name was accepted")
	}
	if !resultHasCode(result, diag.E_INTERNAL) {
		t.Errorf("refusal reported %s, want %s", result, diag.E_INTERNAL)
	}
	if msg := result.String(); !strings.Contains(msg, "duplicate record") {
		t.Errorf("refusal %q does not name the duplicate position", msg)
	}
	if msg := result.String(); strings.Contains(msg, "zero type identity") {
		t.Errorf("the fixture raised an identity error of its own, so the refusal under test is not what failed it: %s", msg)
	}
}

// TestRebuildSnapshot_UnnameableDenotedTypeRefused covers the type a snapshot
// DENOTES without holding an instance of it. Naming it in Types with no instances
// bypassed the root rule entirely, and every writer still keyed its output by the
// denoted type's name — so the writers refused a snapshot both constructors had
// accepted.
//
// Mutation: deleting the validatePartsDenotedTypes call turns this red.
func TestRebuildSnapshot_UnnameableDenotedTypeRefused(t *testing.T) {
	t.Parallel()
	s, deepBeacon := unnameableFixture(t)

	_, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{deepBeacon},
	})
	if !result.HasErrors() {
		t.Fatal("a snapshot denoting a type the entry schema cannot name was assembled")
	}
	if msg := result.String(); !strings.Contains(msg, "types entry") {
		t.Errorf("refusal %q does not name the types entry", msg)
	}
}

// TestRebuildSnapshot_KeepsAnUnnameableComposedChild is the other half of the
// rule: a composed child is addressed through its parent, never by name, so any
// type in the closure stays legal there — a transitively imported part type
// included. Narrowing the rule to every instance rather than every ROOT would
// drop this subtree.
func TestRebuildSnapshot_KeepsAnUnnameableComposedChild(t *testing.T) {
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

	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{basin},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basin: {{
				TypeName:   schema.TagForm(s, basin),
				TypeID:     basin,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1"}),
				Composed: map[string][]graph.InstanceParts{
					"PIECES": {{
						TypeName:   schema.TagForm(s, shard.ID()),
						TypeID:     shard.ID(),
						PrimaryKey: immutable.WrapKey([]any{"s1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "s1", "density": float64(3)}),
					}},
				},
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("a composed child of a transitively imported part type was refused: %s", result)
	}
	roots := built.InstancesOf(basin)
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	children := roots[0].Composed("PIECES")
	if len(children) != 1 {
		t.Fatalf("got %d composed children, want 1", len(children))
	}
	if got := children[0].TypeID(); got != shard.ID() {
		t.Errorf("composed child carries type %s, want %s", got, shard.ID())
	}
}

// TestGraphAdd_UnnameableRootRefused pins that Add applies the same rule the
// constructors do, by the same predicate rather than by its own copy.
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

	valid := instance.NewValidInstance(schema.TagForm(s, typ.ID()), typ.ID(),
		immutable.WrapKey([]any{"d1"}),
		immutable.WrapProperties(map[string]any{"id": "d1", "power": float64(1)}),
		nil, nil, nil)

	addResult := graph.New(s).Add(context.Background(), valid)
	if !addResult.HasErrors() {
		t.Fatal("Add accepted a root the entry schema cannot name")
	}
	if !resultHasCode(addResult, diag.E_GRAPH_TYPE_NOT_FOUND) {
		t.Errorf("Add reported %s, want %s", addResult, diag.E_GRAPH_TYPE_NOT_FOUND)
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
func mustImportedType(t *testing.T, s *schema.Schema, alias, name string) (schema.TypeID, bool) {
	t.Helper()
	imp, ok := s.ImportByAlias(alias)
	if !ok || imp.Schema() == nil {
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

package json_test

import (
	"context"
	"encoding/json"
	"testing"

	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// The entry schema reaches deep only through mid and holds no alias for it.
// Holder is declared in mid, which CAN name deep, so a Holder root legally
// carries a deep.Shard composed child — the one position a type the entry
// schema cannot name still occupies.
const (
	transEntry = `schema "entry"

import "mid.yammm" as mid
`
	transMid = `schema "mid"

import "deep.yammm" as deep

type Holder {
	id String primary
	*-> PIECES (many) deep.Shard
}
`
	transDeep = `schema "deep"

part type Crumb {
	name String primary
}

part type Shard {
	name String primary
	*-> BITS (many) Crumb
}
`
)

// TestMarshalObject_ResolvesATransitivelyImportedType pins the JSON writer's
// type lookup against the closure. The lookup walked direct imports only, so a
// transitively imported type resolved to nothing and the writer fell back to
// the raw relation name where a resolved type supplies the declared field name.
//
// Mutation: replacing lookupType's TypeByID with a walk of local types and
// direct imports turns this red.
func TestMarshalObject_ResolvesATransitivelyImportedType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(transEntry),
		"mid.yammm":   []byte(transMid),
		"deep.yammm":  []byte(transDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}

	mid, ok := s.ImportByAlias("mid")
	if !ok || mid.Schema() == nil {
		t.Fatal("import alias mid did not resolve")
	}
	holderType, ok := mid.Schema().Type("Holder")
	if !ok {
		t.Fatal("mid.Holder not found")
	}
	deepImp, ok := mid.Schema().ImportByAlias("deep")
	if !ok || deepImp.Schema() == nil {
		t.Fatal("import alias deep did not resolve")
	}
	shardType, ok := deepImp.Schema().Type("Shard")
	if !ok {
		t.Fatal("deep.Shard not found")
	}
	crumbType, ok := deepImp.Schema().Type("Crumb")
	if !ok {
		t.Fatal("deep.Crumb not found")
	}
	holder, shard, crumb := holderType.ID(), shardType.ID(), crumbType.ID()

	// The fixture is only meaningful while the child's type is one the entry
	// schema cannot name: that is what a direct-imports-only walk misses.
	if _, addressable := schema.AddressableTag(s, shard); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Shard")
	}
	if _, addressable := schema.AddressableTag(s, holder); !addressable {
		t.Fatal("fixture is vacuous: the root must be a type the entry schema CAN name")
	}

	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{holder},
		Instances: []graph.InstanceParts{
			{
				TypeID:     holder,
				PrimaryKey: immutable.WrapKey([]any{"h1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
				Composed: map[string][]graph.InstanceParts{
					"PIECES": {{
						TypeID:     shard,
						PrimaryKey: immutable.WrapKey([]any{"s1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "s1"}),
						Composed: map[string][]graph.InstanceParts{
							"BITS": {{
								TypeID:     crumb,
								PrimaryKey: immutable.WrapKey([]any{"c1"}),
								Properties: immutable.WrapProperties(map[string]any{"name": "c1"}),
							}},
						},
					}},
				},
			},
		},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}

	data, err := adapterjson.New().MarshalObject(ctx, built)
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}

	var doc map[string][]map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not an object of arrays: %v\n%s", err, data)
	}

	// The root's key is the addressable tag; the child's field names come from
	// the relations its own resolved type declares.
	roots, ok := doc["mid.Holder"]
	if !ok || len(roots) != 1 {
		t.Fatalf("no mid.Holder root in the output: %s", data)
	}
	pieceField, ok := holderType.Relation("PIECES")
	if !ok {
		t.Fatal("mid.Holder declares no PIECES relation")
	}
	pieces, ok := roots[0][pieceField.FieldName()].([]any)
	if !ok || len(pieces) != 1 {
		t.Fatalf("no %q array on the root; got keys %v\n%s", pieceField.FieldName(), roots[0], data)
	}
	child, ok := pieces[0].(map[string]any)
	if !ok {
		t.Fatalf("the composed child is %T, want an object\n%s", pieces[0], data)
	}

	bits, ok := shardType.Relation("BITS")
	if !ok {
		t.Fatal("deep.Shard declares no BITS relation")
	}
	if bits.FieldName() == "BITS" {
		t.Fatalf("fixture is vacuous: the declared field name %q equals the relation name, so an unresolved type renders the same",
			bits.FieldName())
	}
	if _, ok := child[bits.FieldName()]; !ok {
		t.Errorf("the composed child carries no %q field, so its transitively imported type did not resolve; got keys %v\n%s",
			bits.FieldName(), child, data)
	}
	if _, raw := child["BITS"]; raw {
		t.Errorf("the composed child carries the raw relation name, so its type did not resolve\n%s", data)
	}
}

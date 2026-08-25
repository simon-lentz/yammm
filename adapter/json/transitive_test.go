package json_test

import (
	"context"
	"encoding/json"
	"testing"

	adapterjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The entry schema reaches deep only through mid, so deep is transitively
// imported and the entry schema holds no alias for it.
const (
	transEntry = `schema "entry"

import "mid.yammm" as mid
`
	transMid = `schema "mid"

import "deep.yammm" as deep
`
	transDeep = `schema "deep"

type Hub {
	id String primary
	--> LINKS (many) Hub
}
`
)

// TestMarshalObject_ResolvesATransitivelyImportedType pins the JSON writer's
// type lookup against the closure. The lookup walked direct imports only, so a
// transitively imported type — which the v3 wire preserves through Load —
// resolved to nothing and the writer fell back to guessing cardinality from the
// edge count. A (many) relation holding one edge then emitted a scalar.
func TestMarshalObject_ResolvesATransitivelyImportedType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(transEntry),
		"mid.yammm":   []byte(transMid),
		"deep.yammm":  []byte(transDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly())
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}

	if _, ok := s.ResolveType(schema.NewTypeRef("", "Hub", location.Span{})); ok {
		t.Fatal("fixture is vacuous: Hub resolves locally, so it is not transitively imported")
	}
	var hubID schema.TypeID
	for _, sc := range s.Closure() {
		if t2, ok := sc.Type("Hub"); ok {
			hubID = t2.ID()
		}
	}
	if hubID.IsZero() {
		t.Fatal("Hub not found in the import closure")
	}

	node := func(k string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "Hub",
			TypeID:     hubID,
			PrimaryKey: immutable.WrapKey([]any{k}),
			Properties: immutable.WrapProperties(map[string]any{"id": k}),
		}
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{hubID},
		Instances: map[schema.TypeID][]graph.InstanceParts{hubID: {node("h1"), node("h2")}},
		Edges: []graph.EdgeParts{{
			Relation:   "LINKS",
			SourceType: hubID, SourceKey: immutable.WrapKey([]any{"h1"}),
			TargetType: hubID, TargetKey: immutable.WrapKey([]any{"h2"}),
		}},
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
	var found bool
	for _, insts := range doc {
		for _, inst := range insts {
			if inst["id"] != "h1" {
				continue
			}
			found = true
			links, ok := inst["links"]
			if !ok {
				t.Fatalf("no links field on h1; got keys %v\n%s", inst, data)
			}
			// One edge under a (many) relation must still be an array — of
			// _target_ objects, whose field names only a resolved type can
			// supply.
			outer, ok := links.([]any)
			if !ok {
				t.Fatalf("links = %T, want []any", links)
			}
			if len(outer) == 0 {
				t.Fatal("links is empty")
			}
			target, ok := outer[0].(map[string]any)
			if !ok {
				t.Fatalf("links[0] = %T, want a _target_ object — the type did not resolve", outer[0])
			}
			if target["_target_id"] != "h2" {
				t.Errorf("links[0] = %v, want _target_id h2", target)
			}
		}
	}
	if !found {
		t.Fatalf("h1 missing from the output:\n%s", data)
	}
}

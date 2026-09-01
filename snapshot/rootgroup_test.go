package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const vSchema = `schema "v"

part type Card {
	last4 String primary
}

abstract type Base {
	id String primary
}

type Holder {
	id String primary
	*-> CARDS (many) Card
}
`

func vLoad(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), vSchema, "v.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s
}

func vID(t *testing.T, s *schema.Schema, n string) schema.TypeID {
	t.Helper()
	ty, ok := s.Type(n)
	if !ok {
		t.Fatalf("%s missing", n)
	}
	return ty.ID()
}

// TestRootGroup_RebuildRefusesIneligibleTypes is the WRITER half of the rule:
// a part type and an abstract type cannot key a root instance group, so the
// library cannot produce a document stating one.
func TestRootGroup_RebuildRefusesIneligibleTypes(t *testing.T) {
	s := vLoad(t)
	for _, name := range []string{"Card", "Base"} {
		id := vID(t, s, name)
		_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
			Types: []schema.TypeID{id},
			Instances: map[schema.TypeID][]graph.InstanceParts{
				id: {{
					TypeName:   name,
					TypeID:     id,
					PrimaryKey: immutable.WrapKey([]any{"x"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "x", "last4": "x"}),
				}},
			},
		})
		if !res.HasErrors() {
			t.Errorf("%s: RebuildSnapshot accepted an ineligible root", name)
			continue
		}
		t.Logf("%-6s -> %s", name, strings.TrimSpace(res.String()))
	}
}

// TestRootGroup_EmptyPartGroupIsStillLegal pins the boundary the rule must not
// cross. An EMPTY group states that the snapshot HOLDS the type, not that it
// holds a root instance of it, and the writer emits one for every type the
// document denotes — part types included. A guard that read the group without
// its item count refused this document, which the library itself writes.
func TestRootGroup_EmptyPartGroupIsStillLegal(t *testing.T) {
	ctx := context.Background()
	s := vLoad(t)
	holder, card := vID(t, s, "Holder"), vID(t, s, "Card")
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{holder, card},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			holder: {{
				TypeName:   "Holder",
				TypeID:     holder,
				PrimaryKey: immutable.WrapKey([]any{"h1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
				Composed: map[string][]graph.InstanceParts{"CARDS": {{
					TypeName:   "Card",
					TypeID:     card,
					PrimaryKey: immutable.WrapKey([]any{"4242"}),
					Properties: immutable.WrapProperties(map[string]any{"last4": "4242"}),
				}}},
			}},
		},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res)
	}
	data, mres := snapshot.Marshal(ctx, built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	if _, lres := snapshot.Load(ctx, data, s); lres.HasErrors() {
		t.Fatalf("a composed part under a real root must load: %s", lres)
	}
	t.Logf("round trip clean; document holds an empty Card group: %v",
		strings.Contains(string(data), `{"schema":"v","name":"Card"}`))
}

// TestRootGroup_LoadRefusesIneligibleTypes is the READER half, and it is
// defence in depth: this library cannot write such a document, but a foreign
// writer can, and admitting one hands the caller a snapshot no adapter can
// consume — adapter/neo4j fails mid-write extracting a key the type does not
// declare.
func TestRootGroup_LoadRefusesIneligibleTypes(t *testing.T) {
	ctx := context.Background()
	s := vLoad(t)
	holder, card := vID(t, s, "Holder"), vID(t, s, "Card")
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{holder, card},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			holder: {{
				TypeName:   "Holder",
				TypeID:     holder,
				PrimaryKey: immutable.WrapKey([]any{"h1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
				Composed: map[string][]graph.InstanceParts{"CARDS": {{
					TypeName:   "Card",
					TypeID:     card,
					PrimaryKey: immutable.WrapKey([]any{"4242"}),
					Properties: immutable.WrapProperties(map[string]any{"last4": "4242"}),
				}}},
			}},
		},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res)
	}
	data, mres := snapshot.Marshal(ctx, built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	// Card sorts before Holder, so row 0 is the part type and the writer emits
	// an empty group for it. Drop that group and re-point the populated one at
	// row 0 — the shape a foreign writer can emit.
	const emptyCardGroup = `"instances":[{"type":0,"items":[]},{"type":1,`
	doc := strings.Replace(string(data), emptyCardGroup, `"instances":[{"type":0,`, 1)
	if doc == string(data) {
		t.Fatalf("fixture shape changed; %s not found in %s", emptyCardGroup, data)
	}
	_, lres := snapshot.Load(ctx, []byte(doc), s, snapshot.WithIntegrityCheck(false))
	if !lres.HasCode(diag.E_SNAPSHOT_INVALID_ROOT) {
		t.Errorf("Load accepted a part type as a root group: %s", lres)
	}
}

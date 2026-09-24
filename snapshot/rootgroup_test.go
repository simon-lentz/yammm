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

// holderWithCard is Holder h1 holding the part Card 4242: the one legal place
// a Card instance stands.
func holderWithCard(holder, card schema.TypeID) graph.InstanceParts {
	return graph.InstanceParts{
		TypeID:     holder,
		PrimaryKey: immutable.WrapKey([]any{"h1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
		Composed: map[string][]graph.InstanceParts{"CARDS": {{
			TypeID:     card,
			PrimaryKey: immutable.WrapKey([]any{"4242"}),
			Properties: immutable.WrapProperties(map[string]any{"last4": "4242"}),
		}}},
	}
}

// TestRootGroup_RebuildRefusesIneligibleTypes is the WRITER half of the rule:
// a part type and an abstract type cannot key a root instance group, nor be
// denoted with none, so the library cannot produce a document stating one.
func TestRootGroup_RebuildRefusesIneligibleTypes(t *testing.T) {
	s := vLoad(t)
	for _, name := range []string{"Card", "Base"} {
		id := vID(t, s, name)
		_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
			Types: []schema.TypeID{id},
			Instances: []graph.InstanceParts{
				{
					TypeID:     id,
					PrimaryKey: immutable.WrapKey([]any{"x"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "x", "last4": "x"}),
				},
			},
		})
		if !res.HasErrors() {
			t.Errorf("%s: RebuildSnapshot accepted an ineligible root", name)
		}
		_, res = graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{id}})
		if !res.HasErrors() || !strings.Contains(res.String(), "types entry 0 denotes") {
			t.Errorf("%s: RebuildSnapshot accepted the type denoted with no instance: %s", name, res)
		}
	}
}

// TestRootGroup_AnEmptyPartGroupIsRefused pins that denotation takes the
// whole root rule. A snapshot denotes the types it can file a root under, so a
// composed part is in the types table and in no group, and a group naming a
// part type is refused even when it is empty.
func TestRootGroup_AnEmptyPartGroupIsRefused(t *testing.T) {
	ctx := context.Background()
	s := vLoad(t)
	holder, card := vID(t, s, "Holder"), vID(t, s, "Card")
	if _, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{holder, card},
		Instances: []graph.InstanceParts{holderWithCard(holder, card)},
	}); !res.HasErrors() {
		t.Error("RebuildSnapshot accepted a part type as a types entry")
	}

	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{holder},
		Instances: []graph.InstanceParts{holderWithCard(holder, card)},
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
	// Card sorts before Holder, so row 0 is the part type, and the writer
	// emits no group for it.
	const holderGroup = `"instances":[{"type":1,`
	doc := strings.Replace(string(data), holderGroup, `"instances":[{"type":0,"items":[]},{"type":1,`, 1)
	if doc == string(data) {
		t.Fatalf("fixture shape changed; %s not found in %s", holderGroup, data)
	}
	_, lres := snapshot.Load(ctx, []byte(doc), s, snapshot.WithIntegrityCheck(false))
	if !lres.HasCode(diag.E_SNAPSHOT_INVALID_ROOT) || !strings.Contains(lres.String(), "instances entry 0 denotes") {
		t.Errorf("Load accepted an empty part-type group: %s", lres)
	}
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
		Types:     []schema.TypeID{holder},
		Instances: []graph.InstanceParts{holderWithCard(holder, card)},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res)
	}
	data, mres := snapshot.Marshal(ctx, built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	// Card sorts before Holder, so row 0 is the part type. Re-point the
	// populated group at row 0 — the shape a foreign writer can emit.
	const holderGroup = `"instances":[{"type":1,`
	doc := strings.Replace(string(data), holderGroup, `"instances":[{"type":0,`, 1)
	if doc == string(data) {
		t.Fatalf("fixture shape changed; %s not found in %s", holderGroup, data)
	}
	_, lres := snapshot.Load(ctx, []byte(doc), s, snapshot.WithIntegrityCheck(false))
	if !lres.HasCode(diag.E_SNAPSHOT_INVALID_ROOT) {
		t.Errorf("Load accepted a part type as a root group: %s", lres)
	}
}

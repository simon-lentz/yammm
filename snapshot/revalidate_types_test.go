package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// WithRevalidation is documented as the option that reports what Load admits:
// "a .ys can hold a graph that fails the graph layer's Add-time relation
// guards ... WithRevalidation is the option that reports all of it". These two
// tests pin the half that was silent — a wire row naming a type the relation
// does not declare, which revalidation derived from the SCHEMA and never
// compared against the document.

const typeMismatchSchema = `schema "tmis"

type Station {
	id String primary
}

part type Card {
	last4 String primary
}

part type Tag {
	label String primary
}

type Sensor {
	id String primary
	--> FEED (_) Station
	*-> CARDS (many) Card
}
`

func typeMismatchLoad(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), typeMismatchSchema, "tmis.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s
}

func typeMismatchTypeID(t *testing.T, s *schema.Schema, n string) schema.TypeID {
	t.Helper()
	ty, ok := s.Type(n)
	if !ok {
		t.Fatalf("%s missing", n)
	}
	return ty.ID()
}

// TestRevalidation_ReportsEdgeTargetTypeMismatch drives an edge whose wire row
// names a type the association does not declare, with the target instance
// present so the dangling-reference guard cannot fire instead.
func TestRevalidation_ReportsEdgeTargetTypeMismatch(t *testing.T) {
	ctx := context.Background()
	s := typeMismatchLoad(t)
	v := instance.NewValidator(s)

	station, res := v.ValidateOne(ctx, "Station", instance.RawInstance{Properties: map[string]any{"id": "a"}})
	if res.HasErrors() {
		t.Fatalf("station: %s", res)
	}
	sensor, res := v.ValidateOne(ctx, "Sensor", instance.RawInstance{
		Properties: map[string]any{"id": "s1", "FEED": map[string]any{"_target_id": "a"}},
	})
	if res.HasErrors() {
		t.Fatalf("sensor: %s", res)
	}
	// A Sensor sharing the target's key, so re-pointing the edge finds a real
	// instance and E_SNAPSHOT_DANGLING_REFERENCE cannot fire first.
	decoy, res := v.ValidateOne(ctx, "Sensor", instance.RawInstance{Properties: map[string]any{"id": "a"}})
	if res.HasErrors() {
		t.Fatalf("decoy: %s", res)
	}

	g := graph.New(s)
	g.Add(ctx, station)
	g.Add(ctx, sensor)
	g.Add(ctx, decoy)
	data, mres := snapshot.Marshal(ctx, g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}

	doc := string(data)
	if !strings.Contains(doc, `{"schema":"tmis","name":"Sensor"}`) ||
		!strings.Contains(doc, `{"schema":"tmis","name":"Station"}`) {
		t.Fatalf("rows not found in %s", doc)
	}
	// Sensor sorts before Station, so the edge's target row moves 1 -> 0.
	edited := strings.Replace(doc, `"target_type":1`, `"target_type":0`, 1)
	if edited == doc {
		t.Fatalf("no target_type edit possible in %s", doc)
	}

	_, lres := snapshot.Load(ctx, []byte(edited), s,
		snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
	if !lres.HasCode(diag.E_SNAPSHOT_TYPE_MISMATCH) {
		t.Errorf("revalidation did not report an edge target contradicting the association: %s", lres)
	}
}

// TestRevalidation_ReportsComposedChildTypeMismatch drives a composed child
// whose wire row names a part type the composition does not declare.
func TestRevalidation_ReportsComposedChildTypeMismatch(t *testing.T) {
	ctx := context.Background()
	s := typeMismatchLoad(t)
	sensor, card, tag := typeMismatchTypeID(t, s, "Sensor"), typeMismatchTypeID(t, s, "Card"), typeMismatchTypeID(t, s, "Tag")

	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensor, card, tag},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			sensor: {{
				TypeName:   "Sensor",
				TypeID:     sensor,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1"}),
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

	doc := string(data)
	// Rows sort Card(0), Sensor(1), Tag(2). Re-point the composed child at Tag.
	edited := strings.Replace(doc, `"composed":{"CARDS":[{"key":["4242"],"type":0`,
		`"composed":{"CARDS":[{"key":["4242"],"type":2`, 1)
	if edited == doc {
		t.Fatalf("no composed type edit possible in %s", doc)
	}

	_, lres := snapshot.Load(ctx, []byte(edited), s,
		snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
	if !lres.HasCode(diag.E_SNAPSHOT_TYPE_MISMATCH) {
		t.Errorf("revalidation did not report a composed child contradicting the composition: %s", lres)
	}
}

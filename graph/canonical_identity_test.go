package graph_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// identitySchema loads a one-type schema whose sole primary key carries the
// given declaration, so the test submits one value under one key.
func identitySchema(t *testing.T, keyDecl string) *schema.Schema {
	t.Helper()
	src := "schema \"identity\"\n\ntype Item {\n\tid " + keyDecl + " primary\n}\n"
	s, result := schema.LoadString(t.Context(), src, "identity.yammm")
	if result.HasErrors() {
		t.Fatalf("load %q schema: %s", keyDecl, result)
	}
	return s
}

// addSpellings validates each value as an Item and adds it to one graph,
// returning the distinct rendered keys and the resulting snapshot.
func addSpellings(t *testing.T, s *schema.Schema, vals []any) ([]string, *graph.Snapshot) {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)

	seen := map[string]bool{}
	var keys []string
	for _, val := range vals {
		valid, res := v.ValidateOne(t.Context(), "Item", instance.RawInstance{
			Properties: map[string]any{"id": val},
		})
		if !res.OK() {
			t.Fatalf("validating %#v: %s", val, res)
		}
		k := valid.PrimaryKey().String()
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		g.Add(t.Context(), valid)
	}
	return keys, g.Snapshot()
}

// assertOneIdentity holds the property the whole canonicalization family
// exists for: every accepted spelling of one value is one instance under one
// key. Before it, each spelling produced its own key and the graph reported no
// duplicate — two rows for one entity, silently.
func assertOneIdentity(t *testing.T, s *schema.Schema, vals []any) {
	t.Helper()
	keys, snap := addSpellings(t, s, vals)
	if len(keys) != 1 {
		t.Fatalf("%d spellings produced %d distinct keys: %v", len(vals), len(keys), keys)
	}
	if n := len(snap.InstancesOf(mustTypeID(t, s, "Item"))); n != 1 {
		t.Errorf("graph holds %d instances for one value, want 1", n)
	}
	if n := len(snap.Duplicates()); n != len(vals)-1 {
		t.Errorf("graph reported %d duplicates, want %d — every repeat of one value is a duplicate",
			n, len(vals)-1)
	}
}

// TestIdentity_UUIDKeyCollapsesEverySpelling is the sharper half of the key
// split: uuid.Parse accepts five spellings and checkUUID accepts all of them.
func TestIdentity_UUIDKeyCollapsesEverySpelling(t *testing.T) {
	t.Parallel()
	const id = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
	assertOneIdentity(t, identitySchema(t, "UUID"), []any{
		uuid.MustParse(id),
		id,
		"0A35EF0F-9D40-4B6B-A0A1-0D1A5A0E1F2B",
		"{0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b}",
		"urn:uuid:0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b",
		"0a35ef0f9d404b6ba0a10d1a5a0e1f2b",
	})
}

// TestIdentity_TimestampKeyCollapsesEverySpelling covers the three RFC 3339
// spellings whose rendered key differed from the canonical one.
func TestIdentity_TimestampKeyCollapsesEverySpelling(t *testing.T) {
	t.Parallel()
	assertOneIdentity(t, identitySchema(t, "Timestamp"), []any{
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		"2026-08-19T12:00:00Z",
		"2026-08-19T12:00:00+00:00",
		"2026-08-19T12:00:00.000Z",
	})
}

// TestIdentity_DateKeyAcceptsBothRepresentations is the Date half. It could
// not be written before this release: the validator rejected a time.Time.
func TestIdentity_DateKeyAcceptsBothRepresentations(t *testing.T) {
	t.Parallel()
	assertOneIdentity(t, identitySchema(t, "Date"), []any{
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		"2026-08-19",
	})
}

// TestIdentity_DistinctValuesStayDistinct is the control: the assertions above
// must not pass by collapsing every value onto one key.
func TestIdentity_DistinctValuesStayDistinct(t *testing.T) {
	t.Parallel()
	keys, snap := addSpellings(t, identitySchema(t, "UUID"), []any{
		"0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b",
		"fa35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b",
	})
	if len(keys) != 2 {
		t.Errorf("two UUIDs produced %d keys: %v", len(keys), keys)
	}
	if n := len(snap.Duplicates()); n != 0 {
		t.Errorf("two distinct UUIDs reported %d duplicates", n)
	}
}

// rebuildSchema declares a canonicalizing kind in every parts position
// RebuildSnapshot fills: instance property, composed-child property and edge
// property.
func rebuildSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "rebuild_canonical"

type Station {
	id String primary
}

part type Reading {
	name String primary
	taken_at Timestamp
}

type Sensor {
	id String primary
	created_at Timestamp
	run_id UUID
	--> FEED (_) Station {
		seen_at Timestamp
	}
	*-> READINGS (many) Reading
}
`
	s, result := schema.LoadString(t.Context(), src, "rebuild_canonical.yammm")
	if result.HasErrors() {
		t.Fatalf("load rebuild schema: %s", result)
	}
	return s
}

// TestRebuildSnapshot_CanonicalizesEveryPropertyPosition kills the rebuild
// arm. RebuildSnapshot accepts pre-resolved parts and runs no validation, so
// without it a caller-supplied time.Time reaches Properties() untouched and a
// graph rebuilt from parts is not equal to one built through validation.
func TestRebuildSnapshot_CanonicalizesEveryPropertyPosition(t *testing.T) {
	t.Parallel()
	s := rebuildSchema(t)
	when := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	const wantText = "2026-08-19T12:00:00Z"
	const id = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"

	sensorID := mustTypeID(t, s, "Sensor")
	stationID := mustTypeID(t, s, "Station")
	readingID := mustTypeID(t, s, "Reading")

	sensorKey := immutable.WrapKey([]any{"s1"})
	stationKey := immutable.WrapKey([]any{"st1"})

	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensorID, stationID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			stationID: {{
				TypeName:   "Station",
				TypeID:     stationID,
				PrimaryKey: stationKey,
				Properties: immutable.WrapProperties(map[string]any{"id": "st1"}),
			}},
			sensorID: {{
				TypeName:   "Sensor",
				TypeID:     sensorID,
				PrimaryKey: sensorKey,
				Properties: immutable.WrapProperties(map[string]any{
					"id":         "s1",
					"created_at": when,
					// A non-canonical spelling of the same instant.
					"run_id": strings.ToUpper(id),
				}),
				Composed: map[string][]graph.InstanceParts{
					"READINGS": {{
						TypeName:   "Reading",
						TypeID:     readingID,
						PrimaryKey: immutable.WrapKey([]any{"r1"}),
						Properties: immutable.WrapProperties(map[string]any{
							"name": "r1", "taken_at": when,
						}),
					}},
				},
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "FEED",
			SourceType: sensorID,
			SourceKey:  sensorKey,
			TargetType: stationID,
			TargetKey:  stationKey,
			Properties: immutable.WrapProperties(map[string]any{"seen_at": when}),
		}},
	})
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	sensor := snap.InstancesOf(sensorID)[0]
	assertProp(t, sensor.Properties(), "created_at", wantText)
	assertProp(t, sensor.Properties(), "run_id", id)

	reading := sensor.Composed("READINGS")[0]
	assertProp(t, reading.Properties(), "taken_at", wantText)

	edges := snap.EdgesFrom(sensor)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	assertProp(t, edges[0].Properties(), "seen_at", wantText)
}

func assertProp(t *testing.T, props immutable.Properties, name, want string) {
	t.Helper()
	v, ok := props.Get(name)
	if !ok {
		t.Fatalf("property %q missing", name)
	}
	if got := v.Unwrap(); got != want {
		t.Errorf("property %q = %#v, want %q", name, got, want)
	}
}

// TestRebuildSnapshot_KeepsWhatItCannotRender is the other half of the arm's
// contract. A value the constraint cannot render survives, because this entry
// point reconstructs a document rather than validating one — and a document
// written before the rule existed has to keep loading.
func TestRebuildSnapshot_KeepsWhatItCannotRender(t *testing.T) {
	t.Parallel()
	s := rebuildSchema(t)
	sensorID := mustTypeID(t, s, "Sensor")

	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			sensorID: {{
				TypeName:   "Sensor",
				TypeID:     sensorID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{
					"id":         "s1",
					"created_at": "not-a-timestamp",
					"run_id":     "not-a-uuid",
				}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot refused a non-conforming value: %s", result)
	}

	props := snap.InstancesOf(sensorID)[0].Properties()
	assertProp(t, props, "created_at", "not-a-timestamp")
	assertProp(t, props, "run_id", "not-a-uuid")
}

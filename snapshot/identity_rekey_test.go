package snapshot_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The identity-keyed snapshot.
//
// A tag form is a rendering of an identity: bare for a local type,
// alias-qualified for a directly imported one. It cannot name a transitively
// imported type and cannot tell two same-named types apart, so every position
// that must denote a type exactly is keyed by [schema.TypeID]. These tests
// drive the positions where a name key merged or dropped instances.

// hasCode reports whether the result carries an issue with the given code.
func hasCode(result diag.Result, code diag.Code) bool {
	for issue := range result.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// TestImportSnapshot_KeepsTransitivelyImportedInstances drives the graph-side
// import of a tag the entry schema cannot name. A transitively imported type
// has no alias to qualify with, so its tag is a bare name that resolved
// against nothing and dropped every instance of the type.
func TestImportSnapshot_KeepsTransitivelyImportedInstances(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	probeID := mustTransitiveTypeID(t, s, "base", "deep", "Probe")
	tag := tagForm(s, probeID)
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{probeID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			probeID: {{
				TypeName:   tag,
				TypeID:     probeID,
				PrimaryKey: immutable.WrapKey([]any{"pr1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "pr1", "reading": float64(2)}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}

	g := graph.NewFromSnapshot(s, built)
	var count int
	for range g.Snapshot().AllInstances() {
		count++
	}
	if count != 1 {
		t.Errorf("importing a transitively imported type dropped its instances: want 1, got %d", count)
	}
}

// The tag collision, one layer above the wire.
//
// A local type and a transitively imported one render the same tag. Keying a
// snapshot by that tag merges them: the second group overwrites the first
// before anything is marshalled. Graph.Add refuses a transitively imported
// type outright, so the collision is reachable only through the
// deserialization path — which is exactly the path a persisted document takes.

// collidingBeacons builds a snapshot holding both Beacons, whose tags collide.
func collidingBeacons(t *testing.T, s *schema.Schema) (*graph.Snapshot, schema.TypeID, schema.TypeID) {
	t.Helper()
	localBeacon := mustTypeIDIn(t, s, "", "Beacon")
	deepBeacon := mustTransitiveTypeID(t, s, "base", "deep", "Beacon")
	if tagForm(s, localBeacon) != tagForm(s, deepBeacon) {
		t.Fatalf("fixture is vacuous: the two Beacons render different tags (%q, %q)",
			tagForm(s, localBeacon), tagForm(s, deepBeacon))
	}
	beacon := func(id schema.TypeID, key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   tagForm(s, id),
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "power": float64(1)}),
		}
	}
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{localBeacon, deepBeacon},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			localBeacon: {beacon(localBeacon, "local1")},
			deepBeacon:  {beacon(deepBeacon, "deep1")},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	return built, localBeacon, deepBeacon
}

// TestRebuildSnapshot_KeepsBothSidesOfATagCollision pins the assembly path. A
// name-keyed Instances map cannot even express this input; an identity-keyed
// one must carry both groups through.
func TestRebuildSnapshot_KeepsBothSidesOfATagCollision(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	built, localBeacon, deepBeacon := collidingBeacons(t, s)
	if got := len(built.InstancesOf(localBeacon)); got != 1 {
		t.Errorf("the local Beacon was merged away: want 1 instance, got %d", got)
	}
	if got := len(built.InstancesOf(deepBeacon)); got != 1 {
		t.Errorf("the transitively imported Beacon was merged away: want 1 instance, got %d", got)
	}
}

// TestGraphSnapshot_KeepsBothSidesOfATagCollision drives Graph.Snapshot itself,
// reached through the import path because Add refuses the transitive type.
func TestGraphSnapshot_KeepsBothSidesOfATagCollision(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	built, _, _ := collidingBeacons(t, s)
	after := graph.NewFromSnapshot(s, built).Snapshot()

	var count int
	for range after.AllInstances() {
		count++
	}
	if count != 2 {
		t.Errorf("Graph.Snapshot merged two types rendering one tag: want 2 instances, got %d", count)
	}
}

// TestRoundTrip_KeepsBothSidesOfATagCollision is the end-to-end bar: both
// groups survive Marshal and Load, each denoted by its own types-table row
// rather than by a tag the two share.
func TestRoundTrip_KeepsBothSidesOfATagCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	built, localBeacon, deepBeacon := collidingBeacons(t, s)
	data, result := snapshot.Marshal(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load rejected a document Marshal produced: %v\n%s", err, data)
	}
	if got := len(loaded.InstancesOf(localBeacon)); got != 1 {
		t.Errorf("the local Beacon did not survive the round trip: got %d\n%s", got, data)
	}
	if got := len(loaded.InstancesOf(deepBeacon)); got != 1 {
		t.Errorf("the transitively imported Beacon did not survive the round trip: got %d\n%s", got, data)
	}
}

// TestImportSnapshot_ResolvesJSONFieldForImportedSource pins the source-side
// lookup behind an unresolved edge's reported input field. An imported source
// type's tag is alias-qualified, and a local-name-only lookup misses it, so
// the field a consumer needs to locate the fault was reported empty.
func TestImportSnapshot_ResolvesJSONFieldForImportedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	basinID := mustTypeIDIn(t, s, "base", "Basin")
	tag := tagForm(s, basinID)
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basinID: {{
				TypeName:   tag,
				TypeID:     basinID,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
			}},
		},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: basinID,
			SourceKey:  immutable.WrapKey([]any{"b1"}),
			Relation:   "NEAR",
			TargetType: basinID,
			TargetKey:  immutable.WrapKey([]any{"gone"}),
			Required:   true,
			Reason:     "target_missing",
		}},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}

	checked := graph.NewFromSnapshot(s, built).Check(ctx)

	var seen, populated bool
	for issue := range checked.Issues() {
		if issue.Code() != diag.E_UNRESOLVED_REQUIRED {
			continue
		}
		seen = true
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyJSONField && d.Value != "" {
				populated = true
			}
		}
	}
	if !seen {
		t.Fatalf("no %s issue was reported: %s", diag.E_UNRESOLVED_REQUIRED, checked)
	}
	if !populated {
		t.Errorf("the %s detail is empty for an imported source type, so a consumer cannot locate the input field",
			diag.DetailKeyJSONField)
	}
}

// TestInfo_InstanceCountsKeyedByIdentity pins the schema-less Info surface
// over a tag collision: two same-named types count under two distinct
// TypeRef keys, and the per-type counts sum to the total.
func TestInfo_InstanceCountsKeyedByIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	built, localBeacon, deepBeacon := collidingBeacons(t, s)

	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	info, infoRes := snapshot.Info(ctx, data)
	if err := infoRes.Err(); err != nil {
		t.Fatalf("info: %v", err)
	}

	localRef := snapshot.TypeRef{SchemaPath: localBeacon.SchemaPath().String(), Name: localBeacon.Name()}
	deepRef := snapshot.TypeRef{SchemaPath: deepBeacon.SchemaPath().String(), Name: deepBeacon.Name()}
	if localRef == deepRef {
		t.Fatal("fixture is vacuous: the two Beacons share one TypeRef")
	}
	if got := info.InstanceCounts[localRef]; got != 1 {
		t.Errorf("InstanceCounts[%s] = %d, want 1", localRef, got)
	}
	if got := info.InstanceCounts[deepRef]; got != 1 {
		t.Errorf("InstanceCounts[%s] = %d, want 1", deepRef, got)
	}
	if info.TotalInstances != 2 {
		t.Errorf("TotalInstances = %d, want 2", info.TotalInstances)
	}
	sum := 0
	for _, c := range info.InstanceCounts {
		sum += c
	}
	if sum != info.TotalInstances {
		t.Errorf("per-type counts sum to %d, total says %d", sum, info.TotalInstances)
	}
}

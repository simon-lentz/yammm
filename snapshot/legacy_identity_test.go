package snapshot_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The legacy read path.
//
// A .ys document carries type identity twice: a tag-form name, which cannot
// name a transitively imported type and cannot tell two same-named types
// apart, and an optional persisted type_id, which does both. Where the two
// disagree the persisted identity is authoritative — the tag is the decoder's
// recovery hint, not its ground truth. These tests drive the positions where
// resolution used to discard an identity the document was carrying.

// contradictoryDuplicateDoc names the entry schema's Part in tag form while
// carrying base.Part's identity. Which one resolves is observable: mass is a
// Float on base.Part and undeclared on the entry schema's Part.
func contradictoryDuplicateDoc(t *testing.T, s *schema.Schema) (schema.TypeID, graph.SnapshotParts) {
	t.Helper()
	localPart := mustTypeIDIn(t, s, "", "Part")
	basePart := mustTypeIDIn(t, s, "base", "Part")

	root := graph.InstanceParts{
		TypeName:   "Part",
		TypeID:     localPart,
		PrimaryKey: immutable.WrapKey([]any{"p1"}),
		Properties: immutable.WrapProperties(map[string]any{"name": "p1", "weight": float64(1)}),
	}
	duplicate := graph.InstanceParts{
		TypeName:   "Part",
		TypeID:     basePart,
		PrimaryKey: immutable.WrapKey([]any{"p1"}),
		Properties: immutable.WrapProperties(map[string]any{"name": "p1", "mass": float64(2)}),
	}
	return basePart, graph.SnapshotParts{
		Types:     []schema.TypeID{localPart},
		Instances: map[schema.TypeID][]graph.InstanceParts{localPart: {root}},
		Duplicates: []graph.DuplicateParts{{
			Type:     localPart,
			Key:      immutable.WrapKey([]any{"p1"}),
			Instance: duplicate,
		}},
	}
}

// marshalDoc assembles parts and writes them as a legacy v2 document, which is
// the format every test in this file drives the reader with.
func marshalDoc(t *testing.T, ctx context.Context, s *schema.Schema, parts graph.SnapshotParts) []byte {
	t.Helper()
	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	data, result := snapshot.MarshalLegacyV2(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// TestLoad_DuplicateHonoursPersistedTypeID drives the duplicates section, the
// one position that persisted an identity and read it back with nothing. The
// same contradiction at a root instance is reported; here it bound silently to
// whichever type the tag form named.
func TestLoad_DuplicateHonoursPersistedTypeID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	basePart, parts := contradictoryDuplicateDoc(t, s)
	data := marshalDoc(t, ctx, s, parts)

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v\n%s", err, data)
	}
	dups := loaded.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("want 1 duplicate, got %d", len(dups))
	}
	if got := dups[0].Instance.TypeID(); got != basePart {
		t.Errorf("the duplicate's persisted identity was discarded:\nwant %s\ngot  %s\n%s", basePart, got, data)
	}
}

// TestLoad_DuplicateAmbiguousIdentityIsReported holds the other half. A tag
// form that resolves to a different type than the document's own persisted
// identity is ambiguous rather than wrong, so the document loads and the
// reader says which type it bound.
func TestLoad_DuplicateAmbiguousIdentityIsReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	_, parts := contradictoryDuplicateDoc(t, s)
	data := marshalDoc(t, ctx, s, parts)

	_, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("an ambiguous legacy document must load, not be rejected: %v", err)
	}
	var found bool
	for issue := range result.Issues() {
		if issue.Code() == diag.E_SNAPSHOT_TYPEID_MISMATCH && issue.Severity() == diag.Warning {
			found = true
		}
	}
	if !found {
		t.Errorf("a duplicate whose tag form and identity name different types bound silently; want a %s warning\n%s",
			diag.E_SNAPSHOT_TYPEID_MISMATCH, data)
	}
}

// TestLoad_AmbiguousRootIdentityLoadsWithWarning pins the severity a v2
// document's ambiguity now draws at a root instance, where it used to draw an
// Error and return no snapshot at all. Rejecting is wrong here: the document
// carries the identity that settles the question.
func TestLoad_AmbiguousRootIdentityLoadsWithWarning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	localPart := mustTypeIDIn(t, s, "", "Part")
	basePart := mustTypeIDIn(t, s, "base", "Part")
	data := marshalDoc(t, ctx, s, graph.SnapshotParts{
		Types: []schema.TypeID{localPart},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			localPart: {{
				TypeName:   "Part",
				TypeID:     basePart,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"name": "p1", "mass": float64(3)}),
			}},
		},
	})

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("an ambiguous root instance must load, not be rejected: %v\n%s", err, data)
	}
	if loaded == nil {
		t.Fatal("load returned no snapshot for a document it accepted")
	}
	insts := loaded.InstancesOf(basePart)
	if len(insts) != 1 {
		t.Fatalf("want 1 Part, got %d", len(insts))
	}
	if got := insts[0].TypeID(); got != basePart {
		t.Errorf("the root instance's persisted identity was discarded:\nwant %s\ngot  %s", basePart, got)
	}
	if !hasCode(result, diag.E_SNAPSHOT_TYPEID_MISMATCH) {
		t.Errorf("an ambiguous root instance bound silently; want %s\n%s", diag.E_SNAPSHOT_TYPEID_MISMATCH, result)
	}
}

// TestMarshal_ResolvesPropertiesByIdentityNotTag pins the emitter half. The
// duplicate's properties belong to base.Part, whose mass is a Float; the
// entry schema's Part does not declare mass, so resolving the tag form emits
// the value int-shaped and it narrows on the next load.
func TestMarshal_ResolvesPropertiesByIdentityNotTag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	_, parts := contradictoryDuplicateDoc(t, s)
	data := marshalDoc(t, ctx, s, parts)

	if !bytes.Contains(data, []byte(`"mass":2.0`)) {
		t.Errorf("mass emitted without a float indicator, so it narrows on the next load\n%s", data)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	dups := loaded.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("want 1 duplicate, got %d", len(dups))
	}
	got := dups[0].Instance.Properties().Clone()["mass"]
	if _, ok := got.(float64); !ok {
		t.Errorf("mass came back as %T(%v), want float64 — the Float property narrowed", got, got)
	}
}

// TestLoad_ComposedChildStalePathFallsBackToName drives a composed child whose
// persisted schema path no longer resolves, the shape a document takes after
// the schema it was written against moved. The path went stale; the name did
// not, so discarding both throws away recoverable identity.
func TestLoad_ComposedChildStalePathFallsBackToName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	siteID := mustTypeIDIn(t, s, "", "Site")
	basePartID := mustTypeIDIn(t, s, "base", "Part")
	// PARTS targets the entry schema's Part, so a base.Part child under it
	// cannot be recovered from the relation and the wire carries a type_id.
	data := marshalDoc(t, ctx, s, graph.SnapshotParts{
		Types: []schema.TypeID{siteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			siteID: {{
				TypeName:   tagForm(s, siteID),
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1"}),
				Composed: map[string][]graph.InstanceParts{
					"PARTS": {{
						TypeName:   tagForm(s, basePartID),
						TypeID:     basePartID,
						PrimaryKey: immutable.WrapKey([]any{"bp1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "bp1", "mass": float64(5)}),
					}},
				},
			}},
		},
	})

	// Marshal cannot write a path that does not resolve, so the stale-path
	// document is reached by editing one.
	basePath := []byte(basePartID.SchemaPath().String())
	if !bytes.Contains(data, basePath) {
		t.Fatalf("fixture shape changed; no persisted base path to stale out in:\n%s", data)
	}
	stale := bytes.ReplaceAll(data, basePath, []byte("/nonexistent/moved.yammm"))

	loaded, result := snapshot.Load(ctx, stale, s, snapshot.WithSkipIntegrityCheck())
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v\n%s", err, stale)
	}
	sites := loaded.InstancesOf(siteID)
	if len(sites) != 1 {
		t.Fatalf("want 1 Site, got %d", len(sites))
	}
	children := sites[0].Composed("PARTS")
	if len(children) != 1 {
		t.Fatalf("want 1 composed child, got %d", len(children))
	}
	if children[0].TypeID().IsZero() {
		t.Errorf("a stale schema path discarded the child's whole identity; the name %q still resolves\n%s",
			children[0].TypeName(), stale)
	}
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

// unresolvedTargetSnapshot hangs one unresolved edge off a Basin, targeting
// Basin, so a caller can rewrite only the target tag on the wire.
func unresolvedTargetSnapshot(t *testing.T, s *schema.Schema) *graph.Snapshot {
	t.Helper()
	basinID := mustTypeIDIn(t, s, "base", "Basin")
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basinID: {{
				TypeName:   tagForm(s, basinID),
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
			Reason:     "target_missing",
		}},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	return built
}

// loadWithTargetTag rewrites the unresolved record's target tag on the wire and
// loads the result, which is the only way to reach a tag the writer would never
// produce — a v2 document written elsewhere, against a schema laid out
// differently.
func loadWithTargetTag(t *testing.T, s *schema.Schema, tag string) (*graph.Snapshot, diag.Result) {
	t.Helper()
	ctx := context.Background()
	data, result := snapshot.MarshalLegacyV2(ctx, unresolvedTargetSnapshot(t, s))
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const anchor = `"target_type":"base.Basin"`
	if !bytes.Contains(data, []byte(anchor)) {
		t.Fatalf("fixture shape changed; %s not found in:\n%s", anchor, data)
	}
	edited := bytes.Replace(data, []byte(anchor), []byte(`"target_type":"`+tag+`"`), 1)
	return snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
}

// hasCode reports whether the result carries an issue with the given code.
func hasCode(result diag.Result, code diag.Code) bool {
	for issue := range result.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// TestLoad_BindsUniqueTransitiveTarget drives an unresolved record whose target
// tag is a bare name only a transitively imported schema declares. One
// declaration means no ambiguity, so it binds and the record survives rather
// than being dropped on a lookup the entry schema cannot make.
func TestLoad_BindsUniqueTransitiveTarget(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	loaded, result := loadWithTargetTag(t, s, "Probe")
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(loaded.Unresolved()); got != 1 {
		t.Errorf("a unique transitively imported target was dropped: want 1 record, got %d", got)
	}
	if hasCode(result, diag.W_SNAPSHOT_AMBIGUOUS_TYPE) {
		t.Errorf("a target declared exactly once was reported ambiguous: %s", result)
	}
	want := mustTransitiveTypeID(t, s, "base", "deep", "Probe")
	if got := loaded.Unresolved()[0].TargetType; got != want {
		t.Errorf("bound the wrong target:\nwant %s\ngot  %s", want, got)
	}
}

// TestLoad_ReportsAmbiguousTarget holds the ambiguous half. Marker is declared
// in two closure schemas and in nothing the entry schema can name, so the bare
// tag cannot say which was meant; the binding is deterministic and reported.
func TestLoad_ReportsAmbiguousTarget(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	loaded, result := loadWithTargetTag(t, s, "Marker")
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(loaded.Unresolved()); got != 1 {
		t.Errorf("an ambiguous target was dropped instead of bound: want 1 record, got %d", got)
	}
	if !hasCode(result, diag.W_SNAPSHOT_AMBIGUOUS_TYPE) {
		t.Errorf("a target two closure schemas declare bound silently; want %s\n%s",
			diag.W_SNAPSHOT_AMBIGUOUS_TYPE, result)
	}
}

// TestLoad_ReportsDroppedTarget holds the last shape. A tag naming no type
// anywhere in the closure cannot be bound, so the record is dropped — but a
// silently dropped edge is how data loss hides, so it is reported.
func TestLoad_ReportsDroppedTarget(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	loaded, result := loadWithTargetTag(t, s, "Nowhere")
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(loaded.Unresolved()); got != 0 {
		t.Errorf("a target naming no declared type was bound anyway: got %d records", got)
	}
	if !hasCode(result, diag.E_SNAPSHOT_UNKNOWN_TYPE) {
		t.Errorf("an unresolvable target dropped its record silently; want %s\n%s",
			diag.E_SNAPSHOT_UNKNOWN_TYPE, result)
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

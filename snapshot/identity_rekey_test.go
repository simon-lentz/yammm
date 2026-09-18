package snapshot_test

import (
	"context"
	"encoding/json"
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
// alias-qualified for a directly imported one. A BARE name cannot tell two
// same-named types apart, so every position that must denote a type exactly is
// keyed by [schema.TypeID]. These tests drive the positions where a name key
// merged or dropped instances.

// hasCode reports whether the result carries an issue with the given code.
func hasCode(result diag.Result, code diag.Code) bool {
	for issue := range result.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// TestImportSnapshot_KeepsATransitivelyImportedComposedChild drives the
// graph-side import of a child whose type the entry schema cannot name. Such a
// type has no alias to qualify with, so a name-resolved import dropped it; a
// composed child is addressed through its parent and must survive.
//
// It is a COMPOSED child and not a root because the entry schema cannot name
// the type, and every root is keyed by name.
func TestImportSnapshot_KeepsATransitivelyImportedComposedChild(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	siteID := mustTypeIDIn(t, s, "", "Site")
	deepPart := mustTransitiveTypeID(t, s, "base", "deep", "Part")
	if _, addressable := schema.AddressableTag(s, deepPart); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name deep.Part")
	}

	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{siteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			siteID: {{
				TypeName:   tagForm(s, siteID),
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"site1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "site1"}),
				Composed: map[string][]graph.InstanceParts{
					"PARTS": {{
						TypeName:   tagForm(s, deepPart),
						TypeID:     deepPart,
						PrimaryKey: immutable.WrapKey([]any{"dp1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "dp1", "density": float64(2)}),
					}},
				},
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}

	after := graph.NewFromSnapshot(s, built).Snapshot()
	roots := after.InstancesOf(siteID)
	if len(roots) != 1 {
		t.Fatalf("importing dropped the root: want 1, got %d", len(roots))
	}
	children := roots[0].Composed("PARTS")
	if len(children) != 1 {
		t.Fatalf("importing a transitively imported child dropped it: want 1, got %d", len(children))
	}
	if got := children[0].TypeID(); got != deepPart {
		t.Errorf("the child carries type %s, want %s", got, deepPart)
	}
}

// Identity keying over one name two schemas declare.
//
// A local type and a DIRECTLY imported one of the same name render different
// tags and share one bare name. Every position that must denote a type exactly
// is keyed by [schema.TypeID], and one keyed by the bare name merges the pair.
// A type the entry schema cannot name is not part of this: it cannot hold a
// root at all, so no position keyed by name ever has to separate one.

// sameNameBeacons builds a snapshot holding the entry schema's Beacon beside
// the one it imports as base. Both are addressable, their tags differ, and
// their bare names do not.
func sameNameBeacons(t *testing.T, s *schema.Schema) (*graph.Snapshot, schema.TypeID, schema.TypeID) {
	t.Helper()
	localBeacon := mustTypeIDIn(t, s, "", "Beacon")
	importedBeacon := mustTypeIDIn(t, s, "base", "Beacon")
	if localBeacon.Name() != importedBeacon.Name() {
		t.Fatalf("fixture is vacuous: the two Beacons have different names (%q, %q)",
			localBeacon.Name(), importedBeacon.Name())
	}
	if tagForm(s, localBeacon) == tagForm(s, importedBeacon) {
		t.Fatalf("fixture is vacuous: two addressable types render one tag (%q)", tagForm(s, localBeacon))
	}
	for _, id := range []schema.TypeID{localBeacon, importedBeacon} {
		if _, addressable := schema.AddressableTag(s, id); !addressable {
			t.Fatalf("fixture is vacuous: %s is not addressable, so it cannot hold a root", id)
		}
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
		Types: []schema.TypeID{localBeacon, importedBeacon},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			localBeacon:    {beacon(localBeacon, "local1")},
			importedBeacon: {beacon(importedBeacon, "imported1")},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	return built, localBeacon, importedBeacon
}

// TestRebuildSnapshot_SeparatesOneNameTwoSchemasDeclare pins the assembly path.
// A name-keyed Instances map cannot even express this input; an identity-keyed
// one must carry both groups through.
func TestRebuildSnapshot_SeparatesOneNameTwoSchemasDeclare(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	built, localBeacon, importedBeacon := sameNameBeacons(t, s)
	if got := len(built.InstancesOf(localBeacon)); got != 1 {
		t.Errorf("the local Beacon was merged away: want 1 instance, got %d", got)
	}
	if got := len(built.InstancesOf(importedBeacon)); got != 1 {
		t.Errorf("the imported Beacon was merged away: want 1 instance, got %d", got)
	}
}

// TestGraphSnapshot_SeparatesOneNameTwoSchemasDeclare drives Graph.Snapshot
// itself, reached through the import path so both groups arrive together.
func TestGraphSnapshot_SeparatesOneNameTwoSchemasDeclare(t *testing.T) {
	t.Parallel()
	s := loadIdentitySchema(t)

	built, _, _ := sameNameBeacons(t, s)
	after := graph.NewFromSnapshot(s, built).Snapshot()

	var count int
	for range after.AllInstances() {
		count++
	}
	if count != 2 {
		t.Errorf("Graph.Snapshot merged two types sharing one name: want 2 instances, got %d", count)
	}
}

// TestRoundTrip_SeparatesOneNameTwoSchemasDeclare is the end-to-end bar: both
// groups survive Marshal and Load, each denoted by its own types-table row
// rather than by the bare name the two share.
func TestRoundTrip_SeparatesOneNameTwoSchemasDeclare(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	built, localBeacon, importedBeacon := sameNameBeacons(t, s)
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
	if got := len(loaded.InstancesOf(importedBeacon)); got != 1 {
		t.Errorf("the imported Beacon did not survive the round trip: got %d\n%s", got, data)
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
	built, localBeacon, importedBeacon := sameNameBeacons(t, s)

	data, res := snapshot.Marshal(ctx, built)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	info, infoRes := snapshot.Info(ctx, data)
	if err := infoRes.Err(); err != nil {
		t.Fatalf("info: %v", err)
	}

	localRef := snapshot.TypeRef{Schema: schemaNameOf(t, s, localBeacon), Name: localBeacon.Name()}
	deepRef := snapshot.TypeRef{Schema: schemaNameOf(t, s, importedBeacon), Name: importedBeacon.Name()}
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

// The TypeRef contract.
//
// TypeRef is a report projection, not a serialization format: it renders and
// does not parse. These three tests pin the rendering, the reason the method
// exists, and the decision that there is no inverse.

const typeRefRendered = "a#Person"

func sampleTypeRef() snapshot.TypeRef {
	return snapshot.TypeRef{Schema: "a", Name: "Person"}
}

// TestTypeRef_RendersPathHashName pins the display form. The
// separator is the contract: schema.TypeID renders "path:name" and TypeRef
// renders "schema#name", the one form this package states a type identity in.
func TestTypeRef_RendersPathHashName(t *testing.T) {
	t.Parallel()
	ref := sampleTypeRef()

	if got := ref.String(); got != typeRefRendered {
		t.Errorf("String() = %q, want %q", got, typeRefRendered)
	}
	text, err := ref.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(text) != typeRefRendered {
		t.Errorf("MarshalText() = %q, want %q", text, typeRefRendered)
	}
}

// TestTypeRef_SerializesAsAStringKey pins what MarshalText is for. A
// map[TypeRef]int has no JSON encoding without a TextMarshaler key, so
// deleting the method takes `yammm snapshot info --format json` with it.
func TestTypeRef_SerializesAsAStringKey(t *testing.T) {
	t.Parallel()

	enc, err := json.Marshal(map[snapshot.TypeRef]int{sampleTypeRef(): 2})
	if err != nil {
		t.Fatalf("marshal a TypeRef-keyed map: %v", err)
	}
	if want := `{"` + typeRefRendered + `":2}`; string(enc) != want {
		t.Errorf("marshalled map as %s, want %s", enc, want)
	}

	list, err := json.Marshal([]snapshot.TypeRef{sampleTypeRef()})
	if err != nil {
		t.Fatalf("marshal a TypeRef slice: %v", err)
	}
	if want := `["` + typeRefRendered + `"]`; string(list) != want {
		t.Errorf("marshalled slice as %s, want %s", list, want)
	}
}

// TestTypeRef_IsWriteOnly pins a decision, not a defect. The rendering is
// injective over the values yammm produces — a DSL type name is
// [A-Z][A-Za-z0-9_]* and holds no '#' — but not over an arbitrary TypeRef, and
// no consumer has asked for the inverse. Adding UnmarshalText is additive and
// allowed; it moves this test and the write-only statement in docs/API.md
// together.
func TestTypeRef_IsWriteOnly(t *testing.T) {
	t.Parallel()

	var ref snapshot.TypeRef
	if err := json.Unmarshal([]byte(`"`+typeRefRendered+`"`), &ref); err == nil {
		t.Error("TypeRef decoded from its rendered form: it gained UnmarshalText, so its godoc and the write-only statement in docs/API.md need updating with it")
	}

	var info snapshot.HeaderInfo
	if err := json.Unmarshal([]byte(`{"Types":["`+typeRefRendered+`"]}`), &info); err == nil {
		t.Error("HeaderInfo decoded from the form yammm snapshot info writes; the surfaces are documented as one-way")
	}
}

// schemaNameOf returns the declared name of the closure member that declares
// id — the form a .ys types-table row carries since the wire was keyed by
// schema name rather than source path.
func schemaNameOf(t *testing.T, s *schema.Schema, id schema.TypeID) string {
	t.Helper()
	for _, cs := range s.Closure() {
		if cs.SourceID() == id.SchemaPath() {
			return cs.Name()
		}
	}
	t.Fatalf("no closure member declares %s", id)
	return ""
}

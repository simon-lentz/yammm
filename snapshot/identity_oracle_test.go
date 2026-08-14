package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The type-identity oracle.
//
// Every instance a graph builds is named by one rule: TypeName is the tag form
// of TypeID against the entry schema — bare for a locally declared type,
// alias-qualified for an imported one. A marshal/load round trip must return
// that pair unchanged at every position an instance can occupy.
//
// The oracle generates documents from ingredients rather than listing expected
// outputs: type origin (local, imported, or a name declared in both schemas),
// position (root, composed child, nested child, duplicate), relation naming
// (matching or differing from the target type's name), and whether the wire
// carries an explicit type_id. Its two right-hand sides come from outside the
// code under test — the identity the caller supplied and the schema's own
// import table — so a marshal rule and a decode rule that are wrong in
// agreement still fail it.
//
// [TestIdentityOracle_RoundTripPreservesIdentity] is the whole criterion; the
// cases below only feed it.

const identityEntrySource = `schema "geo"

import "base.yammm" as base

part type Part {
	name String primary
	weight Float
}

type Anchor {
	id String primary
	depth Float
}

type Site {
	id String primary
	*-> PARTS (many) Part
	*-> IMPORTED (many) base.Part
}

// Beacon is declared here and in deep, and both render the bare tag "Beacon"
// because the entry schema holds no alias for deep. Unlike the two Part
// declarations it is not a part type, so it can be added to a graph directly —
// which is the only way to reach the collision through Graph.Snapshot.
type Beacon {
	id String primary
	power Float
}
`

// The two Part declarations share a name and differ in their properties, so a
// child encoded under the wrong one loses a float indicator and narrows.
const identityBaseSource = `schema "base"

import "deep.yammm" as deep

part type Part {
	name String primary
	mass Float
}

type Basin {
	id String primary
	area Float
	--> NEAR (_) Basin {
		strength Float
	}
}

// Marker is declared here and in deep, and nowhere in the entry schema. That
// is the only shape a bare tag can be ambiguous in: a name the entry schema
// cannot resolve locally and more than one closure schema declares.
type Marker {
	id String primary
}
`

// identityDeepSource is reachable from the entry schema only through base, so
// the entry schema holds no alias for it. That is the one input the tag-form
// rule cannot name, and every case whose origin is "transitive" needs it.
const identityDeepSource = `schema "deep"

part type Part {
	name String primary
	density Float
}

type Probe {
	id String primary
	reading Float
}

type Marker {
	id String primary
}

type Beacon {
	id String primary
	power Float
	--> LINK (_) Beacon
}
`

func loadIdentitySchema(t *testing.T) *schema.Schema {
	t.Helper()
	sources := map[string][]byte{
		"entry.yammm": []byte(identityEntrySource),
		"base.yammm":  []byte(identityBaseSource),
		"deep.yammm":  []byte(identityDeepSource),
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly())
	if result.HasErrors() {
		t.Fatalf("load identity fixture: %s", result)
	}
	return s
}

// tagForm re-derives the naming rule from the entry schema's import table.
// The oracle needs a right-hand side that neither the emitter nor the decoder
// produced, so this reads the schema directly rather than calling either.
func tagForm(s *schema.Schema, id schema.TypeID) string {
	if id.IsZero() || id.SchemaPath() == s.SourceID() {
		return id.Name()
	}
	if alias := s.FindImportAlias(id.SchemaPath()); alias != "" {
		return alias + "." + id.Name()
	}
	return id.Name()
}

// mustTypeIDIn resolves a type in the entry schema or in one of its imports.
// alias is empty for a locally declared type.
func mustTypeIDIn(t *testing.T, s *schema.Schema, alias, name string) schema.TypeID {
	t.Helper()
	if alias == "" {
		typ, ok := s.Type(name)
		if !ok {
			t.Fatalf("type %q not found in entry schema", name)
		}
		return typ.ID()
	}
	imp, ok := s.ImportByAlias(alias)
	if !ok {
		t.Fatalf("import alias %q not found", alias)
	}
	imported := imp.Schema()
	if imported == nil {
		t.Fatalf("import alias %q resolved to no schema", alias)
	}
	typ, ok := imported.Type(name)
	if !ok {
		t.Fatalf("type %q not found in schema imported as %q", name, alias)
	}
	return typ.ID()
}

// mustTransitiveTypeID resolves a type the entry schema reaches only through
// an intermediate import, which is the position tagForm renders as a bare
// name because the entry schema holds no alias for it.
func mustTransitiveTypeID(t *testing.T, s *schema.Schema, viaAlias, thenAlias, name string) schema.TypeID { //nolint:unparam // the intermediate hop is an ingredient, kept explicit

	t.Helper()
	via, ok := s.ImportByAlias(viaAlias)
	if !ok {
		t.Fatalf("import alias %q not found in the entry schema", viaAlias)
	}
	mid := via.Schema()
	if mid == nil {
		t.Fatalf("import alias %q resolved to no schema", viaAlias)
	}
	then, ok := mid.ImportByAlias(thenAlias)
	if !ok {
		t.Fatalf("import alias %q not found in the schema imported as %q", thenAlias, viaAlias)
	}
	deep := then.Schema()
	if deep == nil {
		t.Fatalf("import alias %q resolved to no schema", thenAlias)
	}
	typ, ok := deep.Type(name)
	if !ok {
		t.Fatalf("type %q not found in the transitively imported schema", name)
	}
	return typ.ID()
}

// tidOf resolves a tag form to the identity it renders, so a test written in
// names reads the same now that the accessors take identities.
func tidOf(t *testing.T, s *schema.Schema, tag string) schema.TypeID {
	t.Helper()
	if alias, name, ok := strings.Cut(tag, "."); ok {
		return mustTypeIDIn(t, s, alias, name)
	}
	return mustTypeIDIn(t, s, "", tag)
}

// identityRecord is one instance's identity at one position, rendered so a
// whole snapshot's identities compare as sorted text.
type identityRecord string

func recordOf(position string, inst *graph.Instance) identityRecord {
	return identityRecord(fmt.Sprintf("%s | name=%s | id=%s | pk=%s | props={%s}",
		position, inst.TypeName(), inst.TypeID().String(), inst.PrimaryKey().String(),
		renderProps(inst.Properties().Clone())))
}

// renderProps carries each value's dynamic type into the record: a whole float
// narrowed to int64 is a fidelity loss the value alone does not show.
func renderProps(props map[string]any) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	rendered := make([]string, len(keys))
	for i, k := range keys {
		rendered[i] = k + "=" + renderValue(props[k])
	}
	return strings.Join(rendered, ",")
}

// renderValue descends into containers so an element that changes dynamic type
// is visible; the container's own %T is identical either way.
func renderValue(v any) string {
	rv := reflect.ValueOf(v)
	if v != nil && rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Interface {
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = renderValue(rv.Index(i).Interface())
		}
		return fmt.Sprintf("%T[%s]", v, strings.Join(parts, ","))
	}
	return fmt.Sprintf("%T(%v)", v, v)
}

// collectIdentities walks every position an instance can occupy and returns
// their identities in sorted order. Sorting rather than indexing is deliberate:
// a defect that moves an instance to a different type key must show up as a
// changed record, not as a lookup miss the walk skips.
func collectIdentities(snap *graph.Snapshot) []identityRecord {
	var out []identityRecord
	var walk func(position string, inst *graph.Instance)
	walk = func(position string, inst *graph.Instance) {
		out = append(out, recordOf(position, inst))
		for _, rel := range inst.ComposedRelations() {
			for _, child := range inst.Composed(rel) {
				walk(position+"/composed["+rel+"]", child)
			}
		}
	}
	for _, typeName := range snap.Types() {
		for _, inst := range snap.InstancesOf(typeName) {
			walk("root", inst)
		}
	}
	for _, d := range snap.Duplicates() {
		walk("duplicate", d.Instance)
	}
	for _, u := range snap.Unresolved() {
		out = append(out, identityRecord(fmt.Sprintf(
			"unresolved | source=%s | sourcePk=%s | rel=%s | target=%s | targetKey=%s | required=%t | reason=%s | props={%s}",
			u.Source.TypeName(), u.Source.PrimaryKey().String(), u.Relation,
			u.TargetType, u.TargetKey, u.Required, u.Reason,
			renderProps(u.Properties().Clone()),
		)))
	}
	slices.Sort(out)
	return out
}

// assertTagFormConsistent checks the naming rule itself, independently of the
// round trip: a name that disagrees with its identity is wrong even if it
// survives unchanged.
func assertTagFormConsistent(t *testing.T, s *schema.Schema, snap *graph.Snapshot, when string) {
	t.Helper()
	var check func(position string, inst *graph.Instance)
	check = func(position string, inst *graph.Instance) {
		if want := tagForm(s, inst.TypeID()); inst.TypeName() != want {
			t.Errorf("%s: %s pk=%s carries TypeName %q but its TypeID renders as %q",
				when, position, inst.PrimaryKey().String(), inst.TypeName(), want)
		}
		for _, rel := range inst.ComposedRelations() {
			for _, child := range inst.Composed(rel) {
				check(position+"/composed["+rel+"]", child)
			}
		}
	}
	for _, typeName := range snap.Types() {
		for _, inst := range snap.InstancesOf(typeName) {
			check("root", inst)
		}
	}
	for _, d := range snap.Duplicates() {
		check("duplicate", d.Instance)
	}
}

// identityCase names ingredients, never an expected output. Build places any
// identity at any position, including shapes Add cannot produce — those reach
// a snapshot through graph.RebuildSnapshot, which Marshal must not corrupt.
type identityCase struct {
	name string
	// origin describes which schema declares the type under test.
	origin string
	// position describes where the instance sits in the document.
	position string
	build    func(t *testing.T, s *schema.Schema) graph.SnapshotParts
}

// partProps names the float property the given schema's Part actually
// declares, so a child encoded under the other one loses its float indicator.
func partProps(alias, name string) immutable.Properties {
	floatProp := "weight"
	if alias != "" {
		floatProp = "mass"
	}
	return immutable.WrapProperties(map[string]any{"name": name, floatProp: float64(1)})
}

func identityCases() []identityCase {
	return []identityCase{
		{
			name:     "root_local",
			origin:   "local",
			position: "root",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				id := mustTypeIDIn(t, s, "", "Anchor")
				return graph.SnapshotParts{
					Types: []schema.TypeID{id},
					Instances: map[schema.TypeID][]graph.InstanceParts{
						id: {{
							TypeName:   tagForm(s, id),
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"a1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
						}},
					},
				}
			},
		},
		{
			name:     "root_imported",
			origin:   "imported",
			position: "root",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				id := mustTypeIDIn(t, s, "base", "Basin")
				return graph.SnapshotParts{
					Types: []schema.TypeID{id},
					Instances: map[schema.TypeID][]graph.InstanceParts{
						id: {{
							TypeName:   tagForm(s, id),
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"b1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
						}},
					},
				}
			},
		},
		{
			name:     "composed_local_relation_name_differs",
			origin:   "local",
			position: "composed",
			build:    composedCase("PARTS", "", "Part", "p1"),
		},
		{
			name:     "composed_imported_relation_targets_it",
			origin:   "imported",
			position: "composed",
			build:    composedCase("IMPORTED", "base", "Part", "p2"),
		},
		{
			// Child type base.Part under a relation targeting geo.Part: a
			// rule comparing names alone rebinds it to the wrong schema.
			name:     "composed_collided_name_across_schemas",
			origin:   "collided",
			position: "composed",
			build:    composedCase("PARTS", "base", "Part", "p3"),
		},
		{
			// The mirror of the case above: a locally declared Part sitting
			// under the relation that targets the imported one.
			name:     "composed_collided_name_reversed",
			origin:   "collided",
			position: "composed",
			build:    composedCase("IMPORTED", "", "Part", "p4"),
		},
		{
			// No declared relation, so the decoder has no target to recover
			// from and the child's type_id is the only thing carrying it.
			name:     "composed_under_undeclared_relation",
			origin:   "local",
			position: "composed",
			build:    composedCase("GHOST", "", "Part", "p5"),
		},
		{
			// An imported source type: its edge properties resolve only
			// through a lookup that understands the qualified tag form.
			name:     "unresolved_edge_imported_source",
			origin:   "imported",
			position: "unresolved",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				id := mustTypeIDIn(t, s, "base", "Basin")
				tag := tagForm(s, id)
				return graph.SnapshotParts{
					Types: []schema.TypeID{id},
					Instances: map[schema.TypeID][]graph.InstanceParts{
						id: {{
							TypeName:   tag,
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"b1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
						}},
					},
					Unresolved: []graph.UnresolvedParts{{
						SourceType: id,
						SourceKey:  immutable.WrapKey([]any{"b1"}),
						Relation:   "NEAR",
						TargetType: id,
						TargetKey:  immutable.WrapKey([]any{"gone"}),
						Reason:     "target_missing",
						Properties: immutable.WrapProperties(map[string]any{"strength": float64(1)}),
					}},
				}
			},
		},
		{
			// Reachable only through base, so the entry schema holds no alias
			// and the tag form falls back to the bare name.
			name:     "root_transitively_imported",
			origin:   "transitive",
			position: "root",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				id := mustTransitiveTypeID(t, s, "base", "deep", "Probe")
				tag := tagForm(s, id)
				return graph.SnapshotParts{
					Types: []schema.TypeID{id},
					Instances: map[schema.TypeID][]graph.InstanceParts{
						id: {{
							TypeName:   tag,
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"pr1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "pr1", "reading": float64(2)}),
						}},
					},
				}
			},
		},
		{
			// A local Part and a transitively imported one render the same bare
			// tag, so a name-keyed form cannot tell the two groups apart.
			name:     "root_tag_collision_local_and_transitive",
			origin:   "transitive",
			position: "root",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				localID := mustTypeIDIn(t, s, "", "Part")
				deepID := mustTransitiveTypeID(t, s, "base", "deep", "Part")
				return graph.SnapshotParts{
					Types: []schema.TypeID{localID, deepID},
					Instances: map[schema.TypeID][]graph.InstanceParts{
						localID: {{
							TypeName:   tagForm(s, localID),
							TypeID:     localID,
							PrimaryKey: immutable.WrapKey([]any{"lp1"}),
							Properties: immutable.WrapProperties(map[string]any{"name": "lp1", "weight": float64(1)}),
						}},
						deepID: {{
							TypeName:   tagForm(s, deepID),
							TypeID:     deepID,
							PrimaryKey: immutable.WrapKey([]any{"dp1"}),
							Properties: immutable.WrapProperties(map[string]any{"name": "dp1", "density": float64(1)}),
						}},
					},
				}
			},
		},
		{
			name:     "duplicate_local",
			origin:   "local",
			position: "duplicate",
			build:    duplicateCase("", "Anchor", "a9", map[string]any{"id": "a9", "depth": float64(4)}),
		},
		{
			name:     "duplicate_imported",
			origin:   "imported",
			position: "duplicate",
			build:    duplicateCase("base", "Basin", "b9", map[string]any{"id": "b9", "area": float64(9)}),
		},
	}
}

// composedCase places a child of the named type under the named relation of a
// Site. When the relation's declared target is not the child's type, the wire
// must carry an explicit type_id and the decoder must honour it.
func composedCase(relation, childAlias, childType, childKey string) func(*testing.T, *schema.Schema) graph.SnapshotParts { //nolint:unparam // ingredient kept explicit; the collision is about this name
	return func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
		t.Helper()
		siteID := mustTypeIDIn(t, s, "", "Site")
		childID := mustTypeIDIn(t, s, childAlias, childType)
		return graph.SnapshotParts{
			Types: []schema.TypeID{siteID},
			Instances: map[schema.TypeID][]graph.InstanceParts{
				siteID: {{
					TypeName:   tagForm(s, siteID),
					TypeID:     siteID,
					PrimaryKey: immutable.WrapKey([]any{"site1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "site1"}),
					Composed: map[string][]graph.InstanceParts{
						relation: {{
							TypeName:   tagForm(s, childID),
							TypeID:     childID,
							PrimaryKey: immutable.WrapKey([]any{childKey}),
							Properties: partProps(childAlias, childKey),
						}},
					},
				}},
			},
		}
	}
}

func duplicateCase(alias, typeName, key string, props map[string]any) func(*testing.T, *schema.Schema) graph.SnapshotParts {
	return func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
		t.Helper()
		id := mustTypeIDIn(t, s, alias, typeName)
		tag := tagForm(s, id)
		inst := graph.InstanceParts{
			TypeName:   tag,
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(props),
		}
		return graph.SnapshotParts{
			Types:     []schema.TypeID{id},
			Instances: map[schema.TypeID][]graph.InstanceParts{id: {inst}},
			Duplicates: []graph.DuplicateParts{{
				Type:     id,
				Key:      immutable.WrapKey([]any{key}),
				Instance: inst,
			}},
		}
	}
}

// TestIdentityOracle_RoundTripPreservesIdentity is the criterion. For every
// generated document it asserts three things: the naming rule holds before the
// round trip, every instance returns with the identity it went in with, and the
// naming rule still holds after.
func TestIdentityOracle_RoundTripPreservesIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	cases := identityCases()
	if len(cases) < 10 {
		t.Fatalf("the oracle generated %d documents; it is meant to cover every position", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parts := tc.build(t, s)
			built, result := graph.RebuildSnapshot(s, parts)
			if result.HasErrors() {
				t.Fatalf("assembling the %s/%s document: %s", tc.origin, tc.position, result)
			}

			assertTagFormConsistent(t, s, built, "before the round trip")
			want := collectIdentities(built)

			data, result := snapshot.Marshal(ctx, built)
			if err := result.Err(); err != nil {
				t.Fatalf("marshal: %v", err)
			}
			loaded, result := snapshot.Load(ctx, data, s)
			if err := result.Err(); err != nil {
				t.Fatalf("load rejected a document Marshal produced: %v\n%s", err, data)
			}

			got := collectIdentities(loaded)
			if !slices.Equal(want, got) {
				t.Errorf("identity did not survive the round trip (%s type at %s position)\nbefore:\n  %s\nafter:\n  %s\ndocument:\n%s",
					tc.origin, tc.position,
					strings.Join(asStrings(want), "\n  "),
					strings.Join(asStrings(got), "\n  "),
					data)
			}
			assertTagFormConsistent(t, s, loaded, "after the round trip")
		})
	}
}

// TestIdentityOracle_ByteFixpoint holds the enumeration's own promise over the
// same generated documents: re-marshalling a loaded document reproduces its
// bytes. A document whose identity is lost can still be a fixpoint — the loss
// is stable — so this runs beside the identity check, never instead of it.
func TestIdentityOracle_ByteFixpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			built, result := graph.RebuildSnapshot(s, tc.build(t, s))
			if result.HasErrors() {
				t.Fatalf("assembling: %s", result)
			}
			first, result := snapshot.Marshal(ctx, built)
			if err := result.Err(); err != nil {
				t.Fatalf("first marshal: %v", err)
			}
			loaded, result := snapshot.Load(ctx, first, s)
			if err := result.Err(); err != nil {
				t.Fatalf("load: %v", err)
			}
			second, result := snapshot.Marshal(ctx, loaded)
			if err := result.Err(); err != nil {
				t.Fatalf("second marshal: %v", err)
			}
			if string(first) != string(second) {
				t.Errorf("Marshal(Load(Marshal(x))) differs from Marshal(x)\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

// TestIdentityOracle_ContradictoryNameAndIdentityIsReported supplies the
// ingredient assertTagFormConsistent cannot otherwise reach: every case builder
// sets TypeName from the same tagForm the assertion re-derives, so that
// assertion cannot fail on a generated document. Built directly, the
// disagreement must draw the cross-check rather than bind silently.
func TestIdentityOracle_ContradictoryNameAndIdentityIsReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	siteID := mustTypeIDIn(t, s, "", "Site")
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	// The name says Anchor; the identity says Site. Nothing else disagrees.
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   "Anchor",
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"x1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "x1"}),
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	if anchorID == siteID {
		t.Fatal("fixture is vacuous: Anchor and Site share an identity")
	}

	data, result := snapshot.MarshalLegacyV2(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, result = snapshot.Load(ctx, data, s)
	var found bool
	for issue := range result.Errors() {
		if issue.Code() == diag.E_SNAPSHOT_TYPEID_MISMATCH {
			found = true
		}
	}
	if !found {
		t.Errorf("a persisted identity contradicting its own tag form bound silently; want %s\n%s",
			diag.E_SNAPSHOT_TYPEID_MISMATCH, data)
	}
}

// TestIdentityOracle_BothPreV012DefectsNeedTwoRoundTrips falsifies the
// migration instruction AssertRoundTrip carries. A document holding an
// int-shaped float AND a composed child whose persisted schema path no longer
// resolves cannot heal in one pass: the first load loses the child's identity,
// so its Float constraint is not found and the float stays narrowed.
func TestIdentityOracle_BothPreV012DefectsNeedTwoRoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)

	siteID := mustTypeIDIn(t, s, "", "Site")
	basePartID := mustTypeIDIn(t, s, "base", "Part")
	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{siteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			siteID: {{
				TypeName:   tagForm(s, siteID),
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1"}),
				Composed: map[string][]graph.InstanceParts{
					"IMPORTED": {{
						TypeName:   tagForm(s, basePartID),
						TypeID:     basePartID,
						PrimaryKey: immutable.WrapKey([]any{"bp1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "bp1", "mass": json.Number("5")}),
					}},
				},
			}},
		},
	})
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	base, result := snapshot.MarshalLegacyV2(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// No writer emits an unresolvable schema path, so the both-defects document
	// is reached by editing a legacy one.
	const anchor = `{"key":["bp1"],"properties":`
	const injected = `{"key":["bp1"],"type_id":{"schema_path":"/nonexistent/ghost.yammm","name":"Part"},"properties":`
	if !bytes.Contains(base, []byte(anchor)) {
		t.Fatalf("fixture shape changed; anchor not found in:\n%s", base)
	}
	doc := bytes.Replace(base, []byte(anchor), []byte(injected), 1)

	generations := make([][]byte, 0, 3)
	prev := doc
	for range 3 {
		loaded, res := snapshot.Load(ctx, prev, s, snapshot.WithSkipIntegrityCheck())
		if err := res.Err(); err != nil {
			t.Fatalf("load: %v", err)
		}
		next, res := snapshot.Marshal(ctx, loaded)
		if err := res.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}
		generations = append(generations, next)
		prev = next
	}

	// The instruction AssertRoundTrip carries: one round trip repairs such a
	// document and every later one holds. Red until the identity repair lands.
	if !bytes.Equal(generations[0], generations[1]) {
		t.Errorf("one round trip did not repair the document, so the migration instruction is false:\ngen1:\n%s\ngen2:\n%s",
			generations[0], generations[1])
	}
	if !bytes.Equal(generations[1], generations[2]) {
		t.Errorf("the document had not stabilised after two round trips:\ngen2:\n%s\ngen3:\n%s",
			generations[1], generations[2])
	}
}

func asStrings(rs []identityRecord) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

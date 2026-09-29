package snapshot_test

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

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
	*-> SEGMENTS (many) Segment
}

part type Segment {
	id String primary
	span Float
}

type Anchor {
	id String primary
	depth Float
}

type Site {
	id String primary
	*-> PARTS (many) Part
	*-> MAIN (_:one) Part
	*-> IMPORTED (many) base.Part
}

// Beacon is declared here, in base and in deep. The entry schema names the
// first two, and their tags differ; it cannot name deep's, which base alone
// imports, so that one cannot hold a root here at all.
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
	*-> DEEP (many) deep.Part
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

// Beacon is declared here and in the entry schema. Both are addressable — the
// entry schema names this one "base.Beacon" and its own one "Beacon" — so the
// two tags differ while the bare NAME does not. That is identity keying without
// a collision: a position keyed by TypeID separates them and one keyed by the
// bare name merges them.
type Beacon {
	id String primary
	power Float
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
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly(true))
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
func mustTransitiveTypeID(t *testing.T, s *schema.Schema, viaAlias, thenAlias, name string) schema.TypeID {
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
	keys := slices.Sorted(maps.Keys(props))
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
		walk("duplicate", d.Instance())
		// A conflict re-resolving to a different instance is a loss the
		// rejected instance's own record cannot show.
		conflict, parent := "-", "-"
		if d.Conflict() != nil {
			conflict = fmt.Sprintf("%s[%s]", d.Conflict().TypeID(), d.Conflict().PrimaryKey())
		}
		if d.Parent() != nil {
			parent = fmt.Sprintf("%s[%s]", d.Parent().TypeID(), d.Parent().PrimaryKey())
		}
		out = append(out, identityRecord(fmt.Sprintf(
			"duplicate-coords | id=%s | pk=%s | conflict=%s | parent=%s | rel=%s",
			d.Instance().TypeID(), d.Instance().PrimaryKey(), conflict, parent, d.Relation(),
		)))
	}
	for _, u := range snap.Unresolved() {
		out = append(out, identityRecord(fmt.Sprintf(
			"unresolved | source=%s | sourceId=%s | sourcePk=%s | rel=%s | target=%s | targetKey=%s | required=%t | reason=%s | props={%s}",
			u.Source().TypeName(), u.Source().TypeID(), u.Source().PrimaryKey().String(), u.Relation(),
			u.TargetType(), u.TargetKey(), u.Required(), u.Reason(),
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
		check("duplicate", d.Instance())
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
					Instances: []graph.InstanceParts{
						{
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"a1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
						},
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
					Instances: []graph.InstanceParts{
						{
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"b1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
						},
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
			// geo.Part and base.Part share a name and sit side by side, each
			// under the relation that targets it: a rule comparing names alone
			// rebinds one to the other's schema.
			name:     "composed_collided_name_across_schemas",
			origin:   "collided",
			position: "composed",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				parts := composedCase("PARTS", "", "Part", "p3")(t, s)
				imported := composedCase("IMPORTED", "base", "Part", "p4")(t, s)
				parts.Instances[0].Composed["IMPORTED"] = imported.Instances[0].Composed["IMPORTED"]
				return parts
			},
		},
		{
			// A child of a child: the one position where the parent's own
			// type reference is itself non-root.
			name:     "nested_child_depth_two",
			origin:   "local",
			position: "nested",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				siteID := mustTypeIDIn(t, s, "", "Site")
				partID := mustTypeIDIn(t, s, "", "Part")
				segmentID := mustTypeIDIn(t, s, "", "Segment")
				return graph.SnapshotParts{
					Types: []schema.TypeID{siteID},
					Instances: []graph.InstanceParts{
						{
							TypeID:     siteID,
							PrimaryKey: immutable.WrapKey([]any{"site2"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "site2"}),
							Composed: map[string][]graph.InstanceParts{
								"PARTS": {{
									TypeID:     partID,
									PrimaryKey: immutable.WrapKey([]any{"p6"}),
									Properties: immutable.WrapProperties(map[string]any{"name": "p6", "weight": float64(2)}),
									Composed: map[string][]graph.InstanceParts{
										"SEGMENTS": {{
											TypeID:     segmentID,
											PrimaryKey: immutable.WrapKey([]any{"sg1"}),
											Properties: immutable.WrapProperties(map[string]any{"id": "sg1", "span": float64(0.5)}),
										}},
									},
								}},
							},
						},
					},
				}
			},
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
				return graph.SnapshotParts{
					Types: []schema.TypeID{id},
					Instances: []graph.InstanceParts{
						{
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"b1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
						},
					},
					Unresolved: []graph.UnresolvedParts{{
						SourceType: id,
						SourceKey:  immutable.WrapKey([]any{"b1"}),
						Relation:   "NEAR",
						TargetKey:  immutable.WrapKey([]any{"gone"}),
						Reason:     "target_missing",
						Properties: immutable.WrapProperties(map[string]any{"strength": float64(1)}),
					}},
				}
			},
		},
		{
			// Reachable only through base, so the entry schema holds no alias
			// and no name form. It is legal as a composed child under base.Part's
			// DEEP and nowhere else, and this is the oracle that the child
			// survives the trip.
			name:     "composed_transitively_imported",
			origin:   "transitive",
			position: "composed",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				siteID := mustTypeIDIn(t, s, "", "Site")
				basePartID := mustTypeIDIn(t, s, "base", "Part")
				childID := mustTransitiveTypeID(t, s, "base", "deep", "Part")
				return graph.SnapshotParts{
					Types: []schema.TypeID{siteID},
					Instances: []graph.InstanceParts{
						{
							TypeID:     siteID,
							PrimaryKey: immutable.WrapKey([]any{"site1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "site1"}),
							Composed: map[string][]graph.InstanceParts{
								"IMPORTED": {{
									TypeID:     basePartID,
									PrimaryKey: immutable.WrapKey([]any{"bp1"}),
									Properties: partProps("base", "bp1"),
									Composed: map[string][]graph.InstanceParts{
										"DEEP": {{
											TypeID:     childID,
											PrimaryKey: immutable.WrapKey([]any{"dp1"}),
											Properties: immutable.WrapProperties(map[string]any{"name": "dp1", "density": float64(1)}),
										}},
									},
								}},
							},
						},
					},
				}
			},
		},
		{
			// One bare name, two schemas, both addressable: the entry schema's
			// own Beacon and the one it imports as base. Their tags differ and
			// their names do not, so a position keyed by the bare name merges
			// the two groups and one keyed by identity separates them.
			//
			// Beacon rather than Part because a root group's type must be able
			// to hold a root, and a part type is addressed through its parent.
			name:     "root_one_name_two_schemas",
			origin:   "collided",
			position: "root",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				localID := mustTypeIDIn(t, s, "", "Beacon")
				importedID := mustTypeIDIn(t, s, "base", "Beacon")
				return graph.SnapshotParts{
					Types: []schema.TypeID{localID, importedID},
					Instances: []graph.InstanceParts{
						{
							TypeID:     localID,
							PrimaryKey: immutable.WrapKey([]any{"lb1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "lb1", "power": float64(1)}),
						},
						{
							TypeID:     importedID,
							PrimaryKey: immutable.WrapKey([]any{"ib1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "ib1", "power": float64(2)}),
						},
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
			// A (one) slot's conflict is its sole occupant whatever its key, so
			// the conflict's key differs from the rejected child's and only the
			// walker's conflict coordinate can see a re-derived conflict.
			name:     "duplicate_composed_slot",
			origin:   "local",
			position: "duplicate",
			build: func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
				t.Helper()
				siteID := mustTypeIDIn(t, s, "", "Site")
				partID := mustTypeIDIn(t, s, "", "Part")
				occupant := graph.InstanceParts{
					TypeID:     partID,
					PrimaryKey: immutable.WrapKey([]any{"lp1"}),
					Properties: partProps("", "lp1"),
				}
				return graph.SnapshotParts{
					Types: []schema.TypeID{siteID},
					Instances: []graph.InstanceParts{
						{
							TypeID:     siteID,
							PrimaryKey: immutable.WrapKey([]any{"site9"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "site9"}),
							Composed:   map[string][]graph.InstanceParts{"MAIN": {occupant}},
						},
					},
					Duplicates: []graph.DuplicateParts{{
						Instance: graph.InstanceParts{
							TypeID:     partID,
							PrimaryKey: immutable.WrapKey([]any{"lp9"}),
							Properties: partProps("", "lp9"),
						},
						ParentType: siteID,
						ParentKey:  immutable.WrapKey([]any{"site9"}),
						Relation:   "MAIN",
					}},
				}
			},
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
			Instances: []graph.InstanceParts{
				{
					TypeID:     siteID,
					PrimaryKey: immutable.WrapKey([]any{"site1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "site1"}),
					Composed: map[string][]graph.InstanceParts{
						relation: {{
							TypeID:     childID,
							PrimaryKey: immutable.WrapKey([]any{childKey}),
							Properties: partProps(childAlias, childKey),
						}},
					},
				},
			},
		}
	}
}

func duplicateCase(alias, typeName, key string, props map[string]any) func(*testing.T, *schema.Schema) graph.SnapshotParts {
	return func(t *testing.T, s *schema.Schema) graph.SnapshotParts {
		t.Helper()
		id := mustTypeIDIn(t, s, alias, typeName)
		inst := graph.InstanceParts{
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(props),
		}
		return graph.SnapshotParts{
			Types: []schema.TypeID{id},
			Instances: []graph.InstanceParts{
				inst,
			},
			Duplicates: []graph.DuplicateParts{{
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
	covered := make(map[string]bool, len(cases))
	for _, tc := range cases {
		covered[tc.position+"/"+tc.origin] = true
	}
	// The floor is the ingredient matrix: a deleted case names its hole.
	for _, want := range []string{
		"root/local", "root/imported", "root/collided",
		"composed/local", "composed/imported", "composed/collided",
		"composed/transitive",
		"nested/local",
		"duplicate/local", "duplicate/imported",
		"unresolved/imported",
	} {
		if !covered[want] {
			t.Fatalf("the oracle generates no %s document; the ingredient matrix has a hole", want)
		}
	}
	// Illegal rather than untested: a root is keyed by name and the entry
	// schema cannot name a transitively imported type.
	if covered["root/transitive"] {
		t.Fatal("the oracle generates a root/transitive document, which every constructor refuses")
	}
	// Illegal rather than untested: a composed child is an instance of its
	// slot's declared target, and a slot is a composition its parent declares.
	for name, build := range map[string]func(*testing.T, *schema.Schema) graph.SnapshotParts{
		"base.Part under PARTS":           composedCase("PARTS", "base", "Part", "p3"),
		"geo.Part under IMPORTED":         composedCase("IMPORTED", "", "Part", "p4"),
		"geo.Part under undeclared GHOST": composedCase("GHOST", "", "Part", "p5"),
	} {
		if _, res := graph.RebuildSnapshot(s, build(t, s)); !res.HasErrors() {
			t.Errorf("RebuildSnapshot admitted %s, which Graph.Add refuses", name)
		}
	}
	// Illegal rather than untested: a keyed (many) slot's duplicate collides with
	// the sibling at its own key, so a conflict at another key is no record
	// Graph.AddComposed makes.
	crossKey := findCase(t, "duplicate_composed_slot").build(t, s)
	crossKey.Instances[0].Composed = map[string][]graph.InstanceParts{"PARTS": crossKey.Instances[0].Composed["MAIN"]}
	crossKey.Duplicates[0].Relation = "PARTS"
	if _, res := graph.RebuildSnapshot(s, crossKey); !res.HasErrors() {
		t.Error("RebuildSnapshot admitted a (many) duplicate whose conflict is at another key, which Graph.AddComposed never records")
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

func asStrings(rs []identityRecord) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// findCase returns the identity case named name.
func findCase(t *testing.T, name string) identityCase {
	t.Helper()
	for _, c := range identityCases() {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no identity case %q", name)
	return identityCase{}
}

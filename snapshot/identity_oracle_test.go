package snapshot_test

import (
	"context"
	"fmt"
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
`

// The two Part declarations share a name and differ in their properties, so a
// child encoded under the wrong one loses a float indicator and narrows.
const identityBaseSource = `schema "base"

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
`

func loadIdentitySchema(t *testing.T) *schema.Schema {
	t.Helper()
	sources := map[string][]byte{
		"entry.yammm": []byte(identityEntrySource),
		"base.yammm":  []byte(identityBaseSource),
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
		rendered[i] = fmt.Sprintf("%s=%T(%v)", k, props[k], props[k])
	}
	return strings.Join(rendered, ",")
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
		out = append(out, identityRecord(fmt.Sprintf("unresolved | source=%s | rel=%s | target=%s | props={%s}",
			u.Source.TypeName(), u.Relation, u.TargetType, renderProps(u.Properties().Clone()))))
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
					Types: []string{"Anchor"},
					Instances: map[string][]graph.InstanceParts{
						"Anchor": {{
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
					Types: []string{tagForm(s, id)},
					Instances: map[string][]graph.InstanceParts{
						tagForm(s, id): {{
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
					Types: []string{tag},
					Instances: map[string][]graph.InstanceParts{
						tag: {{
							TypeName:   tag,
							TypeID:     id,
							PrimaryKey: immutable.WrapKey([]any{"b1"}),
							Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
						}},
					},
					Unresolved: []graph.UnresolvedParts{{
						SourceType: tag,
						SourceKey:  immutable.WrapKey([]any{"b1"}),
						Relation:   "NEAR",
						TargetType: tag,
						TargetKey:  immutable.WrapKey([]any{"gone"}),
						Reason:     "target_missing",
						Properties: immutable.WrapProperties(map[string]any{"strength": float64(1)}),
					}},
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
			Types: []string{"Site"},
			Instances: map[string][]graph.InstanceParts{
				"Site": {{
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
			Types:     []string{tag},
			Instances: map[string][]graph.InstanceParts{tag: {inst}},
			Duplicates: []graph.DuplicateParts{{
				Type:     tag,
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

func asStrings(rs []identityRecord) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

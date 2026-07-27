package neo4j

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The emitted set and its order are pinned verbatim by indexes_all.golden, so
// this asserts only what a Cypher string cannot express: the structured metadata
// each Index carries. Statement text belongs to the golden alone — asserted in
// both places, one shape change fails twice for one cause, and the literals here
// drift the moment anyone regenerates only the golden.
func TestIndexesStructured_Metadata(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("IndexesStructured: %v", err)
	}

	byName := make(map[string]Index, len(indexes))
	for _, idx := range indexes {
		byName[idx.Name] = idx
	}

	tests := []struct {
		name string
		want Index
	}{
		// Property-level @index on an own property.
		{"index_test__Document_state_idx", Index{
			Kind: IndexRange, Label: "index_test__Document", Properties: []string{"state"},
		}},
		// Type-level @@index composite; declared order preserved.
		{"index_test__Document_state_published_on_idx", Index{
			Kind: IndexRange, Label: "index_test__Document", Properties: []string{"state", "published_on"},
		}},
		// @vector(cosine) with its dimension read off Vector[768].
		{"index_test__Document_embedding_vector_idx", Index{
			Kind: IndexVector, Label: "index_test__Document", Properties: []string{"embedding"},
			VectorDimensions: 768, VectorSimilarity: "cosine",
		}},
		// An inherited @@index emits on each concrete subtype's own label.
		{"index_test__Note_first_seen_idx", Index{
			Kind: IndexRange, Label: "index_test__Note", Properties: []string{"first_seen"},
		}},
		// Part types are not skipped: they get a label and constraints today.
		{"index_test__Section_heading_idx", Index{
			Kind: IndexRange, Label: "index_test__Section", Properties: []string{"heading"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := byName[tt.name]
			if !ok {
				t.Fatalf("no index named %q; emitted: %v", tt.name, slices.Sorted(maps.Keys(byName)))
			}
			if got.Kind != tt.want.Kind {
				t.Errorf("Kind = %v; want %v", got.Kind, tt.want.Kind)
			}
			if got.Label != tt.want.Label {
				t.Errorf("Label = %q; want %q", got.Label, tt.want.Label)
			}
			if !slices.Equal(got.Properties, tt.want.Properties) {
				t.Errorf("Properties = %v; want %v", got.Properties, tt.want.Properties)
			}
			if got.VectorDimensions != tt.want.VectorDimensions {
				t.Errorf("VectorDimensions = %d; want %d", got.VectorDimensions, tt.want.VectorDimensions)
			}
			if got.VectorSimilarity != tt.want.VectorSimilarity {
				t.Errorf("VectorSimilarity = %q; want %q", got.VectorSimilarity, tt.want.VectorSimilarity)
			}
		})
	}

	// Abstract types emit nothing directly.
	for _, idx := range indexes {
		if strings.Contains(idx.Label, "index_test__Tracked") {
			t.Errorf("abstract type emitted an index: %s", idx.Statement)
		}
	}
}

// TestIndexesForSchema_Golden pins the complete emission list and order.
func TestIndexesForSchema_Golden(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	stmts, result := New().IndexesForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("IndexesForSchema: %v", err)
	}
	yammmtest.Golden(t, "indexes_all", []byte(strings.Join(stmts, "\n")+"\n"))
}

// IndexesForSchema is a pure projection of the Statement field, so one golden
// pins both entry points. A second, structured golden would have to be
// regenerated alongside the first for every Cypher change, and a partial
// regeneration leaves the two disagreeing about the same statement.
func TestIndexesForSchema_IsStatementProjection(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	a := New()

	stmts, r1 := a.IndexesForSchema(context.Background(), s)
	structured, r2 := a.IndexesStructured(context.Background(), s)
	if r1.HasErrors() || r2.HasErrors() {
		t.Fatalf("unexpected errors: %v / %v", r1.Err(), r2.Err())
	}
	want := make([]string, len(structured))
	for i, idx := range structured {
		want[i] = idx.Statement
	}
	if !slices.Equal(stmts, want) {
		t.Errorf("IndexesForSchema is not the Statement projection of IndexesStructured:\n got: %v\nwant: %v", stmts, want)
	}
}

// Emission is stable across independent LOADS of the same source, not merely
// across two calls on one loaded schema.
//
// Reloading is what gives the assertion power. Nothing in the emission path
// ranges a map in a way that reaches the output, so calling a pure function
// twice on one already-loaded schema cannot fail whatever the emitter does;
// schema construction is where map iteration genuinely decides slice order, and
// that order is what the emitter walks.
func TestIndexes_DeterministicAcrossLoads(t *testing.T) {
	t.Parallel()
	a := New()

	first, r1 := a.IndexesForSchema(context.Background(), loadSchema(t, "indexes.yammm"))
	if err := r1.Err(); err != nil {
		t.Fatalf("first load: %v", err)
	}
	for i := range 5 {
		next, r := a.IndexesForSchema(context.Background(), loadSchema(t, "indexes.yammm"))
		if err := r.Err(); err != nil {
			t.Fatalf("load %d: %v", i+2, err)
		}
		if !slices.Equal(first, next) {
			t.Fatalf("emission differs across loads:\nfirst: %v\n  got: %v", first, next)
		}
	}
}

// Two @@index composites whose readable names collide (underscore-join
// ambiguity: (a, b_c) and (a_b, c) both render {label}_a_b_c_idx) are emitted
// with distinct disambiguated names rather than reported as an error. Because
// CREATE ... IF NOT EXISTS silently skips a duplicate name, both must reach the
// database under names of their own; and because index emission is
// all-or-nothing, erroring here suppressed every unrelated index in the schema.
func TestIndexes_CollidingNamesAreDisambiguated(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_collision.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("a name collision should disambiguate, not fail: %v", result.Err())
	}
	if len(indexes) != 2 {
		t.Fatalf("expected both composites emitted, got %d: %+v", len(indexes), indexes)
	}
	if indexes[0].Name == indexes[1].Name {
		t.Errorf("colliding indexes still share the name %q", indexes[0].Name)
	}
	for _, idx := range indexes {
		if !strings.Contains(idx.Statement, idx.Name) {
			t.Errorf("statement does not carry the disambiguated name %q: %s", idx.Name, idx.Statement)
		}
		if err := ValidateIdentifier(idx.Name, "index name"); err != nil {
			t.Errorf("disambiguated name %q is not a valid Neo4j identifier: %v", idx.Name, err)
		}
	}
}

// Disambiguation must be deterministic ACROSS LOADS: the emitted DDL is a build
// artifact tooling diffs, and a name that changed run to run would read as drift
// forever. Re-running the emitter on one already-loaded schema re-runs a pure
// function on identical inputs and cannot fail; reloading re-runs schema
// construction, where map iteration decides the slice order the digest would
// pick up if it were derived from position rather than identity.
func TestIndexes_DisambiguationIsDeterministic(t *testing.T) {
	t.Parallel()
	a := New()

	first, r1 := a.IndexesStructured(context.Background(), loadSchema(t, "indexes_collision.yammm"))
	if r1.HasErrors() {
		t.Fatalf("first load: %v", r1.Err())
	}
	for range 5 {
		next, r := a.IndexesStructured(context.Background(), loadSchema(t, "indexes_collision.yammm"))
		if r.HasErrors() {
			t.Fatalf("reload: %v", r.Err())
		}
		if len(next) != len(first) {
			t.Fatalf("emitted %d indexes on reload, first load emitted %d", len(next), len(first))
		}
		for i := range first {
			if first[i].Name != next[i].Name {
				t.Fatalf("index %d name is not stable across loads: %q then %q",
					i, first[i].Name, next[i].Name)
			}
		}
	}
}

// A schema with no collisions keeps its readable names — disambiguation must
// touch only the names that would actually clash.
func TestIndexes_NonCollidingNamesUnchanged(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("IndexesStructured: %v", result.Err())
	}
	for _, idx := range indexes {
		if want := indexName(idx.Label, idx.Properties, idx.Kind); idx.Name != want {
			t.Errorf("uncollided index name = %q; want the readable form %q", idx.Name, want)
		}
	}
}

// TestIndexes_InvalidPropertyIdentifier pins that a property whose name is a
// Cypher reserved keyword is rejected at emission. Such a name is a valid DSL
// property name but not a valid unquoted Neo4j identifier.
func TestIndexes_InvalidPropertyIdentifier(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_bad_ident.yammm")
	_, result := New().IndexesStructured(context.Background(), s)
	if !result.HasCode(E_NEO4J_INVALID_IDENTIFIER) {
		t.Fatalf("expected E_NEO4J_INVALID_IDENTIFIER, got: %v", result.Err())
	}
}

// TestIndexes_InvalidPropertyIdentifierReportedOnce pins that a reserved-keyword
// property referenced by multiple index annotations (here `match` via both
// @index and @@index) yields exactly one E_NEO4J_INVALID_IDENTIFIER, not one per
// reference — the report-each-error-once contract.
func TestIndexes_InvalidPropertyIdentifierReportedOnce(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_bad_ident_multi.yammm")
	_, result := New().IndexesStructured(context.Background(), s)
	n := 0
	for issue := range result.Issues() {
		if issue.Code() == E_NEO4J_INVALID_IDENTIFIER {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 E_NEO4J_INVALID_IDENTIFIER for the reserved property, got %d: %v", n, result.Err())
	}
}

// TestIndexes_DuplicateDeclarationDedups pins that declaring the same index
// twice — @index on a property plus a single-property @@index over it — emits
// one index rather than tripping the name-collision check. Load-time validation
// accepts both placements (neither checks the other), so a hard emit failure
// would leave a schema the loader called valid unable to emit any index DDL.
func TestIndexes_DuplicateDeclarationDedups(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_duplicate_decl.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("identical duplicate declarations should emit cleanly, got: %v", result.Err())
	}
	if len(indexes) != 1 {
		t.Fatalf("expected 1 emitted index, got %d: %+v", len(indexes), indexes)
	}
	if got := indexes[0].Properties; !slices.Equal(got, []string{"name"}) {
		t.Errorf("emitted index properties = %v, want [name]", got)
	}
}

// A schema.Builder schema with no registry defers every qualified reference, so
// a type extending an unresolvable qualified supertype seals cleanly and
// validateIndexType suppresses E_UNKNOWN_ANNOTATION_TARGET for its @@index
// arguments — the property might live on the invisible ancestor. The adapter
// therefore cannot trust the sealed model: emitting DDL for a property that does
// not exist creates a permanently empty index that DiffIndexes then reports as
// matched on every subsequent run.
func TestIndexesStructured_UnknownCompositeProperty_Reported(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("probe").
		AddType("Entity").
		Extends(schema.NewTypeRef("common", "Base", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithTypeAnnotation("index", "missingProp").
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("precondition: the deferred-supertype path should seal cleanly, got: %v", res)
	}

	indexes, result := New().IndexesStructured(context.Background(), s)
	if !result.HasCode(E_NEO4J_UNKNOWN_PROPERTY) {
		t.Errorf("want E_NEO4J_UNKNOWN_PROPERTY for @@index on an undeclared property, got: %v", result)
	}
	for _, idx := range indexes {
		if slices.Contains(idx.Properties, "missingProp") {
			t.Errorf("emitted DDL for an undeclared property: %s", idx.Statement)
		}
	}
}

// Every emitted index name must be distinct. disambiguateIndexNames is the sole
// guarantor of that, and a fixed-width digest asserts uniqueness without
// checking it — so the property is asserted here directly, over the whole
// emitted set rather than within a collision group.
func TestIndexes_EmittedNamesAreUnique(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"indexes.yammm", "indexes_collision.yammm", "indexes_inherited.yammm"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, fixture)
			indexes, result := New().IndexesStructured(context.Background(), s)
			if result.HasErrors() {
				t.Fatalf("IndexesStructured: %v", result.Err())
			}
			seen := make(map[string]Index, len(indexes))
			for _, idx := range indexes {
				if prev, dup := seen[idx.Name]; dup {
					t.Errorf("name %q emitted twice: %v and %v", idx.Name, prev.Properties, idx.Properties)
				}
				seen[idx.Name] = idx
			}
		})
	}
}

// A disambiguated name must not land on a name some other object already holds
// unsuffixed, which is why the non-colliding names are reserved before any
// suffix is chosen.
func TestUniqueDigestName_AvoidsReservedNames(t *testing.T) {
	t.Parallel()
	// The seed has to be a name uniqueDigestName would OTHERWISE return, or the
	// reservation check is never reached and the assertion holds by construction
	// whether or not the guard exists. Take the unreserved answer first, then
	// reserve exactly it.
	free := uniqueDigestName("n", "identity-one", map[string]bool{})

	taken := map[string]bool{free: true}
	got := uniqueDigestName("n", "identity-one", taken)
	if got == free {
		t.Errorf("uniqueDigestName returned the reserved name %q", got)
	}
	if !strings.HasPrefix(got, free) {
		t.Errorf("uniqueDigestName = %q; want %q lengthened, so the name stays derived from the same identity", got, free)
	}

	// And again, so a group of three cannot collapse to two names.
	taken[got] = true
	third := uniqueDigestName("n", "identity-one", taken)
	if third == free || third == got {
		t.Errorf("uniqueDigestName returned %q, which is already taken", third)
	}
}

// Constraint names are disambiguated by the same helper, with the same
// reservation set. Emitted statements carry IF NOT EXISTS, so two constraints
// sharing a name make the server silently skip the second and the NOT NULL or
// TYPE guarantee it encodes is simply absent from the database.
func TestConstraints_DisambiguationIsUniqueAndReserves(t *testing.T) {
	t.Parallel()
	// `a_b` on Item and `b` on Item_a both render {schema}__Item_a_b_not_null.
	s, res := schema.LoadString(context.Background(), `schema "col"
type Item { id String primary
            a_b String required }
type Item_a { id String primary
              b String required }
`, "p.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}

	got, result := New().ConstraintsStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("ConstraintsStructured: %v", result.Err())
	}

	seen := make(map[string]Constraint, len(got))
	var collided int
	for _, c := range got {
		if prev, dup := seen[c.Name]; dup {
			t.Errorf("constraint name %q emitted twice: %v(%v) and %v(%v)",
				c.Name, prev.Label, prev.Properties, c.Label, c.Properties)
		}
		seen[c.Name] = c
		// The statement must carry the final name, or the server creates it under
		// the pre-disambiguation one and the diff never pairs it.
		if !strings.Contains(c.Statement, c.Name) {
			t.Errorf("statement %q does not carry its own name %q", c.Statement, c.Name)
		}
		if strings.HasSuffix(c.Name, "_not_null") {
			continue
		}
		if strings.Contains(c.Name, "_not_null_") {
			collided++
		}
	}
	if collided != 2 {
		t.Errorf("got %d suffixed NOT NULL names; want the two that collide (names: %v)",
			collided, slices.Sorted(maps.Keys(seen)))
	}
}

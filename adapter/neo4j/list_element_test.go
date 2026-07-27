package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A list whose ELEMENT is itself a collection is valid yammm — docs/SPEC.md
// teaches both `List<List<Integer>>` and `List<Vector[768]>` — but Neo4j has no
// nested collection property type, so the emitter cannot express one and says so.
//
// The failure is worth pinning because it is whole-schema: ConstraintsStructured
// documents returning (nil, result) on any validation error, so ONE such property
// suppresses every constraint the schema would otherwise emit, including those of
// unrelated types. That is the intended fail-fast contract — a partial DDL script
// would silently drop guarantees an operator believes they applied — but it makes
// the diagnostic the only thing standing between a user and an empty output, so
// the message has to name the property and the offending kind.

// loadInline builds a sealed schema from source, failing the test if it does not
// load. These cases need schemas that are VALID yammm and unemittable, which is a
// narrow enough combination that inline source reads better than a fixture file.
func loadInline(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(context.Background(), src, "list_element.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("fixture is not valid yammm, so it cannot exercise the emitter: %v", err)
	}
	return s
}

func TestConstraintsStructured_UnsupportedListElement(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		property string
		wantKind string // the element kind named in the message
	}{{
		name:     "nested list",
		property: "matrix List<List<Integer>> required",
		wantKind: "List",
	}, {
		name:     "list of vectors",
		property: "embeddings List<Vector[768]> required",
		wantKind: "Vector",
	}, {
		// An alias resolves before the element kind is judged, so a
		// list-typed alias reports the kind it resolves TO, not "Alias".
		// The KindAlias arm of neo4jListElementType is unreachable for that
		// reason and exists only to satisfy the exhaustiveness guard.
		name:     "list of a list-typed alias",
		property: "codes List<Codes> required",
		wantKind: "List",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := loadInline(t, `schema "le"
type Codes = List<String>
type A {
    id   String primary
    name String required
    `+tc.property+`
}
`)

			constraints, result := New().ConstraintsStructured(context.Background(), s)

			if result.Err() == nil {
				t.Fatalf("emitted %d constraints for a property Neo4j cannot express", len(constraints))
			}
			// All-or-nothing: the schema's own id and name constraints are
			// emittable and must still be withheld, or an operator applies a
			// script that looks complete.
			if constraints != nil {
				t.Errorf("returned %d constraints alongside the error; the contract is (nil, result)", len(constraints))
			}

			var codes, messages []string
			for iss := range result.Errors() {
				codes = append(codes, iss.Code().String())
				messages = append(messages, iss.Message())
			}
			if !slices.Contains(codes, E_NEO4J_UNSUPPORTED_TYPE.String()) {
				t.Errorf("codes = %v; want %s", codes, E_NEO4J_UNSUPPORTED_TYPE)
			}

			joined := strings.Join(messages, " | ")
			// The property name is what the user has to go and change; the kind
			// is what tells them why. Without both, the only signal is empty output.
			for _, want := range []string{propertyNameOf(tc.property), tc.wantKind} {
				if !strings.Contains(joined, want) {
					t.Errorf("diagnostic does not name %q: %s", want, joined)
				}
			}
		})
	}
}

// propertyNameOf returns the declared name from a property line.
func propertyNameOf(declaration string) string {
	name, _, _ := strings.Cut(declaration, " ")
	return name
}

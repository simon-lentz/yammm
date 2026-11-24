package schema_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// TestExtends_ASupertypeIsAbstractOrOfTheSubtypesKind pins the rule over the
// nine pairs of kinds: a supertype is abstract, or of the subtype's own kind,
// so an abstract type extends abstract types alone. A refused pair draws one
// E_INVALID_SUPERTYPE at the supertype's name in the extends clause.
func TestExtends_ASupertypeIsAbstractOrOfTheSubtypesKind(t *testing.T) {
	t.Parallel()
	modifier := map[string]string{"abstract": "abstract ", "concrete": "", "part": "part "}
	refused := map[[2]string]bool{
		{"abstract", "concrete"}: true,
		{"abstract", "part"}:     true,
		{"concrete", "part"}:     true,
		{"part", "concrete"}:     true,
	}
	for _, sub := range []string{"abstract", "concrete", "part"} {
		for _, super := range []string{"abstract", "concrete", "part"} {
			t.Run(sub+" extends "+super, func(t *testing.T) {
				t.Parallel()
				src := fmt.Sprintf(`schema "s"
%stype Base {
	id String primary
}
%stype Sub extends Base {
	n String
}
`, modifier[super], modifier[sub])
				s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
					return schema.LoadString(t.Context(), src, "s.yammm", schema.WithWarnings(warn))
				})
				if !refused[[2]string{sub, super}] {
					if s.IsZero() {
						t.Fatalf("want a load, got %v", res)
					}
					return
				}
				wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_SUPERTYPE: 1})
				for i := range res.Issues() {
					if got := src[i.Span().Start.Offset:i.Span().End.Offset]; got != "Base" {
						t.Errorf("the issue covers %q, want the supertype's name", got)
					}
					want := fmt.Sprintf("%s type %q extends %s type %q: a supertype is abstract or of the subtype's own kind", sub, "Sub", super, "Base")
					if i.Message() != want {
						t.Errorf("message %q, want %q", i.Message(), want)
					}
				}
			})
		}
	}
}

// TestExtends_AnAbstractTypeDoesNotCarryAKindAcross pins that no chain through
// an abstract type joins two kinds, and that an imported supertype is judged
// by its kind as a local one is.
func TestExtends_AnAbstractTypeDoesNotCarryAKindAcross(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "s"
type Emp {
	id String primary
}
abstract type Mid extends Emp {
	m String
}
part type Line extends Mid {
	n String
}
`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_SUPERTYPE: 1})

	const main = `schema "main"
import "./lib" as lib
type Car extends lib.Wheel {
	vin String primary
}
`
	res = loadSourcesErr(t, map[string][]byte{
		"lib.yammm":  []byte("schema \"lib\"\npart type Wheel {\n\tpos String\n}\n"),
		"main.yammm": []byte(main),
	})
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_SUPERTYPE: 1})
	for i := range res.Issues() {
		if got := main[i.Span().Start.Offset:i.Span().End.Offset]; got != "lib.Wheel" {
			t.Errorf("the issue covers %q, want the qualified name", got)
		}
		if want := `concrete type "Car" extends part type "lib.Wheel": a supertype is abstract or of the subtype's own kind`; i.Message() != want {
			t.Errorf("message %q, want %q", i.Message(), want)
		}
	}
}

// TestExtends_EachRefusedNameInAClauseIsReported pins that the rule judges
// every name an extends clause writes, one an earlier name already reached
// too.
func TestExtends_EachRefusedNameInAClauseIsReported(t *testing.T) {
	t.Parallel()
	const src = `schema "s"
type C {
	id String primary
}
type D extends C {
	d String
}
abstract type X extends D, C {
	x String
}
`
	res := loadStringErr(t, src)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_SUPERTYPE: 2})
	var names []string
	for i := range res.Issues() {
		names = append(names, src[i.Span().Start.Offset:i.Span().End.Offset])
	}
	if !slices.Equal(names, []string{"D", "C"}) {
		t.Errorf("the issues cover %q, want D then C", names)
	}
}

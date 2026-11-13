package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoad_ARelationNamedSelfLoads pins that self binds no relation: a relation
// is read by its UPPER_SNAKE name, so SELF is a member and self the instance.
func TestLoad_ARelationNamedSelfLoads(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, relation string }{
		{"association", `--> SELF (_) Other`},
		{"composition", `*-> SELF (_) Part`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := `schema "self_rel"

type Other {
	id String primary
}

part type Part {
	id String primary
}

type Row {
	id String primary
	` + tc.relation + `

	! "reads both" SELF == nil || $SELF != nil && self.id != ""
}
`
			if _, res := schema.LoadString(t.Context(), src, "self_rel.yammm"); res.Err() != nil {
				t.Errorf("a relation named SELF was refused: %v", res)
			}
		})
	}
}

// TestSelfVariable_LivesInOnePlace pins that the name the checker, the
// completer and the evaluator all bind is one constant. It stood as a private
// const in schema and a string literal at two evaluator sites, so the layers
// agreed by coincidence.
func TestSelfVariable_LivesInOnePlace(t *testing.T) {
	t.Parallel()
	if expr.SelfVariable != "self" {
		t.Errorf("expr.SelfVariable = %q, want %q", expr.SelfVariable, "self")
	}
}

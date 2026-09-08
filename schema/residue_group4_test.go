package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// TestLoad_RelationNamedSelfIsRefused pins that the self rule covers BOTH
// member kinds. A property named self was refused and a relation whose field
// name is self was not, so the member was unreachable by its own name with no
// diagnostic — the rule delivered at one of its two sites. Removing the
// relation walk turns this red.
func TestLoad_RelationNamedSelfIsRefused(t *testing.T) {
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
}
`
			_, res := schema.LoadString(t.Context(), src, "self_rel.yammm")
			if res.Err() == nil {
				t.Fatal("a relation whose field name is self loaded clean")
			}
			var found bool
			for is := range res.Issues() {
				if is.Code() == diag.E_INVALID_NAME {
					found = true
					if is.Span().IsZero() {
						t.Error("the refusal carries no span")
					}
				}
			}
			if !found {
				t.Errorf("want E_INVALID_NAME; got %v", res.Err())
			}
		})
	}
}

// TestLoad_RelationNotNamedSelfIsAccepted is the control: the rule refuses the
// one name and nothing near it.
func TestLoad_RelationNotNamedSelfIsAccepted(t *testing.T) {
	t.Parallel()
	const src = `schema "self_rel_ok"

type Other {
	id String primary
}

type Row {
	id String primary
	--> SELFISH (_) Other
	--> MYSELF (_) Other
}
`
	if _, res := schema.LoadString(t.Context(), src, "self_rel_ok.yammm"); res.Err() != nil {
		t.Errorf("a relation merely containing the name was refused: %v", res.Err())
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

package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// Grammar-level acceptance for schema annotations: these schemas must parse and
// carry their annotations onto the model. The grammar assigns them no meaning —
// eligibility is settled in a later completion phase ([completer.validateAnnotations],
// exercised in annotation_validate_test.go) and the emitted DDL is the adapters'
// concern (adapter/neo4j) — so a case here proves only that the shape reaches
// the model intact.

func TestAnnotation_Grammar_PropertyLevelParses(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index
	published_on Date @index
}`)
}

func TestAnnotation_Grammar_TypeLevelCompositeParses(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String
	published_on Date
	@@index(state, published_on)
}`)
}

func TestAnnotation_Grammar_VectorAndWriteOnceParse(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	embedding Vector[768] @vector(cosine)
	first_seen_at Timestamp @writeOnce
}`)
}

func TestAnnotation_Grammar_MultipleAnnotationsAndDocComments(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index @writeOnce
	/* the composite lookup index */
	@@index(state, content_hash)
}`)
}

func TestAnnotation_Grammar_TrailingCommaInArgs(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String
	@@index(state, content_hash,)
}`)
}

// Grammar-level rejection guards. annotation_args requires at least one arg
// (parens present ⇒ non-empty), and a single '@' only attaches trailing a
// property — a standalone '@name' as a type-body member is the '@@' role.

func TestAnnotation_Grammar_EmptyParensIsSyntaxError(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Document {
	content_hash String primary
	state String @index()
}`)
	if !res.HasCode(diag.E_SYNTAX) {
		t.Errorf("want E_SYNTAX for @index() empty parens, got: %v", res)
	}
}

func TestAnnotation_Grammar_BareAtAtTypeBodyStartIsSyntaxError(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Document {
	@index
	content_hash String primary
}`)
	if !res.HasCode(diag.E_SYNTAX) {
		t.Errorf("want E_SYNTAX for a bare @ at type-body start, got: %v", res)
	}
}

// An empty argument list ("@index()") is a syntax error, and the annotation
// survives it as a bare "@name": the parenthesised group does not parse, so it
// is left out and the stray "(" is what fails. The annotation therefore reaches
// its arity check like any other zero-argument one.
//
// Only the arity check may fire. A target check must not, because the argument
// list it would read was never written.
func TestAnnotation_Parse_EmptyArgList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantArity int
	}{
		// @index legitimately takes no arguments, so nothing else is owed.
		{name: "index", body: "\tx String @index()", wantArity: 0},
		{name: "vector", body: "\te Vector[8] @vector()", wantArity: 1},
		{name: "compositeIndex", body: "\tx String\n\t@@index()", wantArity: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, "schema \"main\"\ntype T {\n\tid String primary\n"+tt.body+"\n}")
			if !res.HasCode(diag.E_SYNTAX) {
				t.Fatalf("precondition: an empty argument list is a syntax error, got: %v", res)
			}
			counts := codeCounts(res)
			if n := counts[diag.E_INVALID_ANNOTATION]; n != tt.wantArity {
				t.Errorf("E_INVALID_ANNOTATION count: got %d, want %d: %v", n, tt.wantArity, res)
			}
			for _, code := range []diag.Code{
				diag.E_INVALID_ANNOTATION_TARGET,
				diag.E_UNKNOWN_ANNOTATION_TARGET,
			} {
				if n := counts[code]; n != 0 {
					t.Errorf("an unwritten argument list must not cascade into %d %s: %v", n, code, res)
				}
			}
		})
	}
}

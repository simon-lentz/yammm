package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// Grammar-level acceptance for schema annotations. Before the grammar carries
// the annotation productions, '@' lexes as the AT token but appears in no rule,
// so any '@'/'@@'-bearing schema is a syntax error. After, these load clean:
// the annotations are collected onto the model but carry no semantics yet
// (adapters in tracks 2/3 consume them; eligibility validation is a later
// completion phase, exercised separately).

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

// ANTLR's error recovery leaves an argument context with no matched alternative
// when an argument list is empty (`@index()`, itself a syntax error). Carrying
// that as an argument used to drive a contradictory semantic diagnostic on top
// of the syntax error: a zero-argument call told it has arguments, or a
// composite blamed for referencing an empty-named property. The syntax error
// owns the diagnosis; no annotation diagnostic may pile on.
func TestAnnotation_Parse_EmptyArgListNoSemanticCascade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"index", "\tx String @index()"},
		{"vector", "\te Vector[8] @vector()"},
		{"compositeIndex", "\tx String\n\t@@index()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, "schema \"main\"\ntype T {\n\tid String primary\n"+tt.body+"\n}")
			if !res.HasCode(diag.E_SYNTAX) {
				t.Fatalf("precondition: an empty argument list is a syntax error, got: %v", res)
			}
			counts := codeCounts(res)
			for _, code := range []diag.Code{
				diag.E_INVALID_ANNOTATION,
				diag.E_INVALID_ANNOTATION_TARGET,
				diag.E_UNKNOWN_ANNOTATION_TARGET,
			} {
				if n := counts[code]; n != 0 {
					t.Errorf("a malformed argument list must not cascade into %d %s: %v", n, code, res)
				}
			}
		})
	}
}

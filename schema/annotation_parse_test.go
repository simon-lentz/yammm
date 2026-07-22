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

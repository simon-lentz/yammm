package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestAnnotation_Validation_Errors is the table of per-rule rejection cases.
// Each schema is otherwise valid, so wantCounts pins the exact annotation
// diagnostic with no incidental extras.
func TestAnnotation_Validation_Errors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		want   map[diag.Code]int
	}{
		{
			name: "unknown property annotation",
			source: `schema "main"
type T {
	id String primary
	x String @nope
}`,
			want: map[diag.Code]int{diag.E_UNKNOWN_ANNOTATION: 1},
		},
		{
			name: "unknown type annotation",
			source: `schema "main"
type T {
	id String primary
	@@nope(id)
}`,
			want: map[diag.Code]int{diag.E_UNKNOWN_ANNOTATION: 1},
		},
		{
			name: "placement mismatch: writeOnce at type level",
			source: `schema "main"
type T {
	id String primary
	@@writeOnce
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "placement mismatch: vector at type level",
			source: `schema "main"
type T {
	id String primary
	@@vector(cosine)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "@index takes no arguments",
			source: `schema "main"
type T {
	id String primary
	x String @index(id)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "@index on non-scalar (Vector)",
			source: `schema "main"
type T {
	id String primary
	v Vector[4] @index
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "@index on sole primary key",
			source: `schema "main"
type T {
	id String primary @index
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "@@index unknown property",
			source: `schema "main"
type T {
	id String primary
	@@index(ghost)
}`,
			want: map[diag.Code]int{diag.E_UNKNOWN_ANNOTATION_TARGET: 1},
		},
		{
			name: "@@index duplicate reference",
			source: `schema "main"
type T {
	id String primary
	x String
	@@index(x, x)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "@@index non-scalar member",
			source: `schema "main"
type T {
	id String primary
	v Vector[4]
	@@index(id, v)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "@vector wrong keyword",
			source: `schema "main"
type T {
	id String primary
	v Vector[4] @vector(hnsw)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "@vector on non-Vector",
			source: `schema "main"
type T {
	id String primary
	x String @vector(cosine)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "@writeOnce on sole primary key",
			source: `schema "main"
type T {
	id String primary @writeOnce
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "@writeOnce on composite primary-key member",
			source: `schema "main"
type T {
	a String primary @writeOnce
	b String primary
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
		{
			name: "literal argument where reference required",
			source: `schema "main"
type T {
	id String primary
	x String
	@@index("x")
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "own-body exact-duplicate @@index",
			source: `schema "main"
type T {
	id String primary
	x String
	@@index(x)
	@@index(x)
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "duplicate property annotation name",
			source: `schema "main"
type T {
	id String primary
	x String @index @index
}`,
			want: map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1},
		},
		{
			name: "multiple independent annotation errors reported together",
			source: `schema "main"
type T {
	id String primary
	x String @nope
	y String @vector(cosine)
}`,
			want: map[diag.Code]int{diag.E_UNKNOWN_ANNOTATION: 1, diag.E_INVALID_ANNOTATION_TARGET: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, tc.source)
			wantCounts(t, res, tc.want)
		})
	}
}

func TestAnnotation_Validation_ValidKindsLoadClean(t *testing.T) {
	t.Parallel()
	loadOK(t, `schema "main"
type Document {
	content_hash String primary
	state String @index
	published_on Date @index
	embedding Vector[768] @vector(cosine)
	first_seen Timestamp @writeOnce
	@@index(state, published_on)
}`)
}

func TestAnnotation_Validation_CompositePKMemberIndexAllowed(t *testing.T) {
	t.Parallel()
	// A member of a composite primary key is legitimately indexable — the
	// composite backing index does not serve single-property lookups on it.
	loadOK(t, `schema "main"
type T {
	a String primary @index
	b String primary
}`)
}

func TestAnnotation_Validation_InheritedPropertyRefResolves(t *testing.T) {
	t.Parallel()
	// @@index may name an inherited property; validation runs post-linearization.
	loadOK(t, `schema "main"
abstract type Base {
	id String primary
	state String
}
type Derived extends Base {
	published_on Date
	@@index(state, published_on)
}`)
}

func TestAnnotation_Validation_PartTypeValidatedIdentically(t *testing.T) {
	t.Parallel()
	// A part type's annotations validate exactly like a concrete type's:
	// @index/@vector/@writeOnce are accepted, and @writeOnce on the part's
	// declared primary is still rejected.
	loadOK(t, `schema "main"
part type Wheel {
	serial String primary
	size Integer @index
	first_seen Timestamp @writeOnce
}`)

	res := loadStringErr(t, `schema "main"
part type Wheel {
	serial String primary @writeOnce
	size Integer
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1})
}

func TestAnnotation_Validation_StampsArgKinds(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
type T {
	id String primary
	state String
	embedding Vector[768] @vector(cosine)
	@@index(id, state)
}`)
	ty := schemaType(t, s, "T")
	// @vector keyword arg
	v, _ := typeProperty(t, ty, "embedding").Annotation("vector")
	if got := v.Args()[0].Kind(); got != schema.ArgKeyword {
		t.Errorf("@vector arg kind: got %v, want ArgKeyword", got)
	}
	// @@index property-ref args
	idx := ty.AllAnnotationsSlice()[0]
	for i, arg := range idx.Args() {
		if arg.Kind() != schema.ArgPropertyRef {
			t.Errorf("@@index arg %d kind: got %v, want ArgPropertyRef", i, arg.Kind())
		}
	}
}

func TestAnnotation_Validation_IndexArityHintsComposite(t *testing.T) {
	t.Parallel()
	// @index takes no arguments; the arity error names the @@index composite form
	// so a "single @ meant as type-level" mistake gets an actionable hint.
	res := loadStringErr(t, `schema "main"
type T {
	id String primary
	x String @index(id)
}`)
	for issue := range res.Issues() {
		if issue.Code() == diag.E_INVALID_ANNOTATION {
			if !strings.Contains(issue.Message(), "@@index") {
				t.Errorf("arity message should hint the @@index composite form, got: %q", issue.Message())
			}
			return
		}
	}
	t.Fatalf("expected an E_INVALID_ANNOTATION issue, got: %v", res)
}

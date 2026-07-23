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
	v, ok := typeProperty(t, ty, "embedding").Annotation("vector")
	if !ok {
		t.Fatal("embedding should carry @vector")
	}
	if args := v.Args(); len(args) != 1 {
		t.Fatalf("@vector should have exactly one arg, got %d", len(args))
	} else if args[0].Kind() != schema.ArgKeyword {
		t.Errorf("@vector arg kind: got %v, want ArgKeyword", args[0].Kind())
	}
	// @@index property-ref args
	all := ty.AllAnnotationsSlice()
	if len(all) == 0 {
		t.Fatal("T should carry the @@index annotation")
	}
	idx := all[0]
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

// A property whose constraint never built — parse-error recovery leaves it nil —
// must not draw a second, misleading annotation-target diagnostic on top of the
// constraint error that is the real cause. The old phrasing blamed the
// annotation and called a property that plainly declares a type "missing type".
func TestAnnotation_Validation_UnbuiltConstraintNoTargetCascade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"vector", "\te Vector[0] @vector(cosine)"},
		{"index", "\te Vector[0] @index"},
		{"compositeIndex", "\te Vector[0]\n\t@@index(e)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, "schema \"main\"\ntype T {\n\tid String primary\n"+tt.body+"\n}")
			if !res.HasCode(diag.E_INVALID_CONSTRAINT) {
				t.Fatalf("precondition: the constraint error should be reported, got: %v", res)
			}
			if n := codeCounts(res)[diag.E_INVALID_ANNOTATION_TARGET]; n != 0 {
				t.Errorf("an unbuilt constraint must not cascade into %d annotation-target error(s): %v",
					n, res)
			}
		})
	}
}

// The three @vector argument diagnostics are distinct: a quoted keyword suggests
// un-quoting, a quoted non-keyword says "not a literal" without misleading
// advice, and a bare unknown keyword names the valid set. This pins that they
// actually differ rather than collapsing to one generic arity message.
func TestAnnotation_Vector_ArgumentDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, arg, wantSubstr string
	}{
		{"quoted keyword", `"cosine"`, "not a quoted string"},
		{"quoted non-keyword", `"bogus"`, "not a literal"},
		{"bare unknown", "sigmoid", "unknown @vector similarity"},
		{"numeric literal", "3", "not a literal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, `schema "main"
type T {
	id String primary
	emb Vector[8] @vector(`+tc.arg+`)
}`)
			var msg string
			for i := range res.Issues() {
				if i.Code() == diag.E_INVALID_ANNOTATION {
					msg = i.Message()
					break
				}
			}
			if !strings.Contains(msg, tc.wantSubstr) {
				t.Errorf("@vector(%s): message %q should contain %q", tc.arg, msg, tc.wantSubstr)
			}
		})
	}
}

// annotationDisplay echoes a duplicated type-level annotation in its SOURCE
// spelling: a string literal is re-quoted, a bare literal is not, and a
// zero-argument annotation shows no parentheses (which the grammar would reject).
func TestAnnotation_DuplicateDisplay_FaithfulSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, wantSubstr string
	}{
		// @@index arguments are property refs (identifiers), echoed bare.
		{"identifier args", "id String primary\n\ta String\n\t@@index(a)\n\t@@index(a)", "@@index(a)"},
		// A string-literal arg (invalid, but duplicated) must be RE-QUOTED in the
		// echo — its stored text is unquoted.
		{"string literal arg", "id String primary\n\t@@index(\"a\")\n\t@@index(\"a\")", `@@index("a")`},
		// A bare literal (number) must be shown as its own spelling, NOT re-quoted.
		{"bare literal arg", "id String primary\n\t@@index(3)\n\t@@index(3)", "@@index(3)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, "schema \"main\"\ntype T {\n\t"+tc.body+"\n}")
			var msg string
			for i := range res.Issues() {
				if i.Code() == diag.E_INVALID_ANNOTATION && strings.Contains(i.Message(), "more than once") {
					msg = i.Message()
					break
				}
			}
			if !strings.Contains(msg, tc.wantSubstr) {
				t.Errorf("duplicate message %q should echo %q", msg, tc.wantSubstr)
			}
			if strings.Contains(msg, "()") {
				t.Errorf("duplicate message must not render an empty argument list: %q", msg)
			}
		})
	}
}

// A duplicated zero-argument annotation renders without parentheses.
func TestAnnotation_DuplicateDisplay_ZeroArgNoParens(t *testing.T) {
	t.Parallel()
	// @@writeOnce is misplaced (a property-level name at type level) AND
	// duplicated; the duplicate message must read "@@writeOnce", never
	// "@@writeOnce()".
	res := loadStringErr(t, `schema "main"
type T {
	id String primary
	@@writeOnce
	@@writeOnce
}`)
	for i := range res.Issues() {
		if strings.Contains(i.Message(), "more than once") && strings.Contains(i.Message(), "()") {
			t.Errorf("zero-arg duplicate must not show (): %q", i.Message())
		}
	}
}

// A yammm string literal can carry an escape the lexer accepts but Go's unquoter
// rejects (\u with no hex digits). Such an argument must not be labelled
// tokenString — its text is not actually unquoted — or two distinct source
// literals collide in identity() (a false duplicate that also swallows the
// second's own error) and displayText re-quotes the already-quoted raw.
func TestAnnotation_UnunquotableStringArgs_NotConflated(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type T {
	id String primary
	@@index("\u")
	@@index("\"\\u\"")
}`)
	// Two DISTINCT literal-arg errors, and no spurious "declares ... more than once".
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION]; n != 2 {
		t.Errorf("two distinct bad string args should each err; got %d: %v", n, res)
	}
	for i := range res.Issues() {
		if strings.Contains(i.Message(), "more than once") {
			t.Errorf("distinct source literals must not be reported as duplicates: %s", i.Message())
		}
	}
}

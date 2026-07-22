package schema

import (
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/location"
)

// AnnotationArgKind is the validated semantic role of an annotation argument.
// It is stamped during schema completion; at parse time every argument is
// ArgUnvalidated. The set is minimal for the blessed v1 annotations and grows
// only when a registered annotation needs a new argument shape.
type AnnotationArgKind uint8

const (
	// ArgUnvalidated is the zero value: the argument's role is not yet known
	// (before completion, or for an annotation that failed validation).
	ArgUnvalidated AnnotationArgKind = iota
	// ArgPropertyRef marks an argument that resolves to a property of the type,
	// e.g. a member of a @@index composite.
	ArgPropertyRef
	// ArgKeyword marks an argument drawn from a fixed keyword set,
	// e.g. the similarity function of @vector(cosine).
	ArgKeyword
)

// annotationTokenKind records whether a parsed argument was an identifier or a
// literal. It is fixed at parse time and lets completion emit a precise
// "expected a reference or keyword, got a literal" diagnostic without
// reparsing. It is deliberately coarse: finer literal classification waits for
// an annotation that accepts a literal argument.
type annotationTokenKind uint8

const (
	tokenIdentifier annotationTokenKind = iota
	tokenLiteral
)

// AnnotationArg is one argument of an annotation. It is a value type; the
// semantic Kind is stamped during completion (before the owning type is
// sealed), and the public surface is immutable thereafter because Args returns
// a defensive copy.
type AnnotationArg struct {
	text      string
	tokenKind annotationTokenKind
	kind      AnnotationArgKind
	span      location.Span
}

// Text returns the source text of the argument (a string literal is unquoted).
func (a AnnotationArg) Text() string { return a.text }

// Kind returns the validated semantic role of the argument.
func (a AnnotationArg) Kind() AnnotationArgKind { return a.kind }

// Span returns the source location of the argument.
func (a AnnotationArg) Span() location.Span { return a.span }

// isLiteral reports whether the argument was a literal token rather than an
// identifier. Completion uses this to reject a literal where a property
// reference or keyword is required.
func (a AnnotationArg) isLiteral() bool { return a.tokenKind == tokenLiteral }

// Annotation is a validated @name / @@name decorator carried on a property or
// type. Meaning (DDL emission, write-shape derivation) lives in adapters; the
// core carries structure and, after completion, validated argument kinds.
// Annotations are immutable after schema completion.
type Annotation struct {
	name string
	args []AnnotationArg
	doc  string
	span location.Span
}

// newAnnotation builds an annotation from parsed parts. Argument semantic kinds
// start ArgUnvalidated and are stamped by completion before sealing.
func newAnnotation(name string, args []AnnotationArg, doc string, span location.Span) *Annotation {
	return &Annotation{name: name, args: args, doc: doc, span: span}
}

// Name returns the annotation name, without the @ / @@ sigil.
func (a *Annotation) Name() string { return a.name }

// Args returns a defensive copy of the annotation's arguments in source order.
func (a *Annotation) Args() []AnnotationArg { return slices.Clone(a.args) }

// argCount returns the argument count without copying.
func (a *Annotation) argCount() int { return len(a.args) }

// Documentation returns the leading doc comment, if any. Only type-level
// (@@name) annotations may carry one.
func (a *Annotation) Documentation() string { return a.doc }

// Span returns the source location of the annotation.
func (a *Annotation) Span() location.Span { return a.span }

// setArgKind stamps the validated semantic kind of the i-th argument. Internal;
// called only during completion, before the owning type is sealed. It mutates
// the backing slice in place; Args returns a copy, so the sealed public surface
// stays immutable.
func (a *Annotation) setArgKind(i int, kind AnnotationArgKind) {
	a.args[i].kind = kind
}

// identity returns the annotation's deduplication key: name followed by the
// ordered argument texts, NUL-separated so a single arg "a,b" cannot collide
// with two args "a", "b". Two type-level annotations are exact duplicates iff
// their identities match (see mergeAnnotations).
func (a *Annotation) identity() string {
	var b strings.Builder
	b.WriteString(a.name)
	for _, arg := range a.args {
		b.WriteByte(0)
		b.WriteString(arg.text)
	}
	return b.String()
}

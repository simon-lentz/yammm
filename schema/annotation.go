package schema

import (
	"fmt"
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
	// ArgUnvalidated is the zero value: the argument's role is not known. It
	// stays on an argument completion did not judge: one of an annotation that
	// failed validation, of one whose check DEFERRED on an unresolved supertype,
	// or of one whose TARGET type could not be judged. Each case comes with an
	// error, so a schema a load returns holds none of them; a consumer still
	// reads ArgUnvalidated as "not checked", never as "checked and found roleless".
	ArgUnvalidated AnnotationArgKind = iota
	// ArgPropertyRef marks an argument that resolves to a property of the type,
	// e.g. a member of a @@index composite.
	ArgPropertyRef
	// ArgKeyword marks an argument drawn from a fixed keyword set,
	// e.g. the similarity function of @vector(cosine).
	ArgKeyword
)

// String returns the canonical lowercase label for the argument kind, so a
// consumer formatting one gets a name rather than a bare integer — matching
// AnnotationPlacement and the other exported enums in the package.
func (k AnnotationArgKind) String() string {
	//exhaustive:enforce
	switch k {
	case ArgUnvalidated:
		return "unvalidated"
	case ArgPropertyRef:
		return "property-ref"
	case ArgKeyword:
		return "keyword"
	default:
		return "unknown"
	}
}

// annotationTokenKind records what lexical form a parsed argument had. It is
// fixed at parse time and lets completion emit a precise "expected a reference
// or keyword, got a literal" diagnostic without reparsing, and lets a diagnostic
// echo the argument in its source spelling.
//
// A quoted string is distinguished from a bare literal (number, boolean, regex)
// because only the string's text was unquoted, so only it holds its source
// spelling apart, in raw; the others are already their own spelling. Both are
// "literals" for eligibility ([AnnotationArg.isLiteral]).
type annotationTokenKind uint8

const (
	tokenIdentifier annotationTokenKind = iota
	tokenLiteral                        // a bare literal: number, boolean, regex — text is the source spelling
	tokenString                         // a quoted string literal — text is UNQUOTED, raw is the source spelling
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

	// raw is the argument's source text for a quoted string, whose text field
	// holds the UNQUOTED value; empty for every other kind (their text is
	// already the source spelling). Read only by [AnnotationArg.displayText].
	raw string
}

// Text returns the source text of the argument (a string literal is unquoted).
func (a AnnotationArg) Text() string { return a.text }

// Kind returns the validated semantic role of the argument.
func (a AnnotationArg) Kind() AnnotationArgKind { return a.kind }

// Span returns the source location of the argument.
func (a AnnotationArg) Span() location.Span { return a.span }

// isLiteral reports whether the argument was a literal token — a bare literal or
// a quoted string — rather than an identifier. Completion uses this to reject a
// literal where a property reference or keyword is required.
func (a AnnotationArg) isLiteral() bool {
	return a.tokenKind == tokenLiteral || a.tokenKind == tokenString
}

// displayText returns the argument in its SOURCE spelling. A quoted string
// carries its raw source text (text alone is the unquoted value), so the echo
// keeps the delimiter the author actually wrote — the yammm lexer accepts both
// ' and ", and re-quoting through strconv would report a single-quoted
// argument back to the user in double quotes. Everything else is already its
// own spelling. Used by diagnostics that echo an argument back to the user.
func (a AnnotationArg) displayText() string {
	if a.tokenKind == tokenString {
		return a.raw
	}
	return a.text
}

// Annotation is a validated @name / @@name decorator carried on a property or
// type. Meaning (DDL emission, write-shape derivation) lives in adapters; the
// core carries structure and, after completion, validated argument kinds.
// Annotations are immutable after schema completion.
type Annotation struct{ *annotationState }

// IsZero reports whether the handle names no record.
func (a Annotation) IsZero() bool { return a.annotationState == nil }

// annotationState is the record an [Annotation] names.
type annotationState struct {
	name string
	args []AnnotationArg
	doc  string
	span location.Span

	// detachedFrom is the line of the property this annotation binds to, set
	// only when the annotation begins a line. Property annotations are postfix
	// and whitespace is not significant, so an annotation that begins a line
	// binds to the PRECEDING property rather than the following one a reader
	// would expect; completion reports that rather than letting the two
	// readings diverge silently.
	detachedFrom int
}

// newAnnotation builds an annotation from parsed parts. Argument semantic kinds
// start ArgUnvalidated and are stamped by completion before sealing.
func newAnnotation(name string, args []AnnotationArg, doc string, span location.Span) Annotation {
	return Annotation{&annotationState{name: name, args: args, doc: doc, span: span}}
}

// setDetachedFrom records the line of the property this annotation binds to,
// when the annotation begins a line. Zero means attached.
func (a Annotation) setDetachedFrom(line int) { a.detachedFrom = line }

// Name returns the annotation name, without the @ / @@ sigil.
func (a Annotation) Name() string { return a.name }

// Args returns a defensive copy of the annotation's arguments in source order.
func (a Annotation) Args() []AnnotationArg { return slices.Clone(a.args) }

// argCount returns the argument count without copying.
func (a Annotation) argCount() int { return len(a.args) }

// Documentation returns the leading doc comment, if any. Only type-level
// (@@name) annotations may carry one.
func (a Annotation) Documentation() string { return a.doc }

// Span returns the source location of the annotation.
func (a Annotation) Span() location.Span { return a.span }

// setArgKind stamps the validated semantic kind of the i-th argument. Internal;
// called only during completion, before the owning type is sealed. It mutates
// the backing slice in place; Args returns a copy, so the sealed public surface
// stays immutable.
func (a Annotation) setArgKind(i int, kind AnnotationArgKind) {
	a.args[i].kind = kind
}

// identity returns the annotation's deduplication key: the name, which as a
// grammar word holds no NUL, then each argument's token kind and length-prefixed
// text. The kind tells a bare `cosine` from the string `"cosine"`, and the length
// keeps apart two argument lists whose values hold a NUL through a \x00 escape.
func (a Annotation) identity() string {
	const sep = "\x00"
	var b strings.Builder
	b.WriteString(a.name)
	for _, arg := range a.args {
		fmt.Fprintf(&b, "%s%d%s%d%s", sep, arg.tokenKind, sep, len(arg.text), sep)
		b.WriteString(arg.text)
	}
	return b.String()
}

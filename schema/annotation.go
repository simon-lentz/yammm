package schema

import (
	"fmt"
	"slices"
	"strconv"
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
	// survives into a sealed schema in four cases — an annotation that failed
	// validation, one whose argument list failed to parse, one whose check had to
	// DEFER because the type has an unresolved supertype that may yet supply the
	// referent (see validateIndexType), and one whose TARGET type could not be
	// judged, where annotationTargetTypeUnknown returns before any kind is
	// stamped. A consumer reading a sealed schema must therefore treat
	// ArgUnvalidated as "not checked", never as "checked and found roleless".
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
// because only the string's text was unquoted, so only it must be re-quoted to
// reproduce the source; the others are already their own spelling. Both are
// "literals" for eligibility ([AnnotationArg.isLiteral]).
type annotationTokenKind uint8

const (
	tokenIdentifier annotationTokenKind = iota
	tokenLiteral                        // a bare literal: number, boolean, regex — text is the source spelling
	tokenString                         // a quoted string literal — text is UNQUOTED, source adds the quotes
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
	// already the source spelling) and for Builder-constructed arguments,
	// which have no source. Read only by [AnnotationArg.displayText].
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
		if a.raw != "" {
			return a.raw
		}
		// Builder-constructed arguments have no source text to preserve.
		return strconv.Quote(a.text)
	}
	return a.text
}

// Annotation is a validated @name / @@name decorator carried on a property or
// type. Meaning (DDL emission, write-shape derivation) lives in adapters; the
// core carries structure and, after completion, validated argument kinds.
// Annotations are immutable after schema completion.
type Annotation struct {
	name string
	args []AnnotationArg
	doc  string
	span location.Span

	// detachedFrom is the line of the property this annotation binds to, set
	// only when the annotation was written on a LATER line. Property
	// annotations are postfix and whitespace is not significant, so an
	// annotation on its own line binds to the PRECEDING property rather than
	// the following one a reader would expect; completion reports that rather
	// than letting the two readings diverge silently.
	detachedFrom int

	// argsMalformed marks an annotation whose argument list failed to parse, so
	// args does not faithfully record the source. Completion skips its
	// registry-driven argument and target checks: those read a list that never
	// parsed and can only contradict the syntax error already reported.
	argsMalformed bool
}

// newAnnotation builds an annotation from parsed parts. Argument semantic kinds
// start ArgUnvalidated and are stamped by completion before sealing.
func newAnnotation(name string, args []AnnotationArg, doc string, span location.Span, argsMalformed bool) *Annotation {
	return &Annotation{name: name, args: args, doc: doc, span: span, argsMalformed: argsMalformed}
}

// setDetachedFrom records that this annotation was written on a later line
// than the property it binds to. See [Annotation.detachedFromLine].
func (a *Annotation) setDetachedFrom(line int) { a.detachedFrom = line }

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
// ordered arguments, NUL-separated so a single arg "a,b" cannot collide with two
// args "a", "b". Two type-level annotations are exact duplicates iff their
// identities match (see mergeAnnotations).
//
// Each argument contributes its TOKEN KIND as well as its text, because text
// alone is the unquoted value: a bare identifier `cosine` and the string literal
// `"cosine"` are different source, mean different things to validation, and must
// not compare equal — one is a keyword the loader accepts, the other a literal
// it rejects.
//
// The encoding is length-prefixed rather than NUL-separated: an argument's text
// is the UNQUOTED value of a string literal, which can itself contain a NUL byte
// (written in source with a \x00 escape), so a NUL delimiter alone could be
// forged to make two different argument lists produce the same key. Writing each
// text's byte length before
// its bytes makes the encoding self-delimiting and therefore injective,
// whatever the text contains.
func (a *Annotation) identity() string {
	const sep = "\x00"
	var b strings.Builder
	// The NAME is length-prefixed for the same reason the arguments are: it is
	// the first field, so without a length its bytes run straight into the
	// first argument's encoding and a name chosen to look like that encoding
	// makes two different annotations produce one key. The DSL grammar cannot
	// spell such a name, but [TypeBuilder.WithTypeAnnotation] and
	// [TypeBuilder.WithPropertyAnnotation] take one as a plain string.
	fmt.Fprintf(&b, "%d%s", len(a.name), sep)
	b.WriteString(a.name)
	for _, arg := range a.args {
		fmt.Fprintf(&b, "%s%d%s%d%s", sep, arg.tokenKind, sep, len(arg.text), sep)
		b.WriteString(arg.text)
	}
	return b.String()
}

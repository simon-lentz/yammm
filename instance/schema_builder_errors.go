package instance

import (
	"runtime"
	"strconv"
	"strings"
)

// buildErrorKind classifies a single shape-level failure accumulated on a
// SchemaBuilder. Each kind carries a distinct message prefix so Build's
// returned error is self-describing without consulting the kind enum directly.
type buildErrorKind uint8

const (
	kindUnknownProperty buildErrorKind = iota
	kindUnknownRelation
	kindEdgeShape
	kindCardinality
	kindWrongComposedType
	kindMissingEdgeProp
	kindUnknownEdgeProp
	kindChildError
)

// buildError is one accumulated shape-level failure on a SchemaBuilder.
// Multiple errors may accrue over a chain of builder calls; Build returns
// the first such error (with a count suffix when more than one occurred).
type buildError struct {
	kind   buildErrorKind
	typ    string // bound type name (b.typeName); always populated
	target string // property name, relation name, or composition name
	detail string // free-form expected-vs-got detail; may be empty
	caller string // "file:line" from runtime.Callers; empty when not applicable
	child  error  // non-nil iff kind == kindChildError
}

// Error returns the formatted error message. Callers of SchemaBuilder.Build
// treat the returned error as opaque; the format is documented on Build's
// Godoc but not on this private type.
func (e *buildError) Error() string {
	var b strings.Builder
	if e.caller != "" {
		b.WriteString(e.caller)
		b.WriteString(": ")
	}
	b.WriteString(e.typ)
	b.WriteString(": ")
	switch e.kind {
	case kindUnknownProperty:
		b.WriteString("unknown property ")
		b.WriteString(strconv.Quote(e.target))
	case kindUnknownRelation:
		b.WriteString("unknown relation ")
		b.WriteString(strconv.Quote(e.target))
	case kindEdgeShape:
		b.WriteString("relation ")
		b.WriteString(strconv.Quote(e.target))
		if e.detail != "" {
			b.WriteString(": ")
			b.WriteString(e.detail)
		}
	case kindCardinality:
		b.WriteString("relation ")
		b.WriteString(strconv.Quote(e.target))
		b.WriteString(": cardinality mismatch")
		if e.detail != "" {
			b.WriteString(": ")
			b.WriteString(e.detail)
		}
	case kindWrongComposedType:
		b.WriteString("composition ")
		b.WriteString(strconv.Quote(e.target))
		if e.detail != "" {
			b.WriteString(": ")
			b.WriteString(e.detail)
		}
	case kindMissingEdgeProp:
		b.WriteString("relation ")
		b.WriteString(strconv.Quote(e.target))
		b.WriteString(": missing required edge property")
		if e.detail != "" {
			b.WriteString(" ")
			b.WriteString(strconv.Quote(e.detail))
		}
	case kindUnknownEdgeProp:
		b.WriteString("relation ")
		b.WriteString(strconv.Quote(e.target))
		b.WriteString(": unknown edge property")
		if e.detail != "" {
			b.WriteString(" ")
			b.WriteString(e.detail)
		}
	case kindChildError:
		b.WriteString("composition ")
		b.WriteString(strconv.Quote(e.target))
		if e.child != nil {
			b.WriteString(": ")
			b.WriteString(e.child.Error())
		}
	default:
		b.WriteString("build error")
		if e.target != "" {
			b.WriteString(" (")
			b.WriteString(e.target)
			b.WriteString(")")
		}
		if e.detail != "" {
			b.WriteString(": ")
			b.WriteString(e.detail)
		}
	}
	return b.String()
}

// Unwrap exposes the wrapped child error for composition-child failures so
// errors.Is and errors.As walk into the parent→child chain transparently.
// Returns nil for non-child kinds.
func (e *buildError) Unwrap() error {
	return e.child
}

// captureCaller returns the "file:line" of the call site one level above
// the SchemaBuilder method that invoked it — i.e. the user's source line.
// Frame math, following runtime.Callers's "0 identifies Callers itself"
// semantics: 0 = runtime.Callers, 1 = captureCaller, 2 = the SchemaBuilder
// method, 3 = user code. A zero-length string means the stack was too
// shallow to recover (vanishingly rare in practice; a benign degradation).
func captureCaller() string {
	var pcs [1]uintptr
	const userFrameSkip = 3
	n := runtime.Callers(userFrameSkip, pcs[:])
	if n == 0 {
		return ""
	}
	frame, _ := runtime.CallersFrames(pcs[:1]).Next()
	return frame.File + ":" + strconv.Itoa(frame.Line)
}

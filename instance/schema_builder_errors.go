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
	child  error  // non-nil iff kind == kindChildError

	// callerPC is the raw program counter of the user's call site, resolved to
	// "file:line" by [symbolizePC] only when this error is rendered. Zero when
	// not applicable. Storing the PC rather than the string is what keeps the
	// builder's success path allocation-free — see [capturePC].
	callerPC uintptr
}

// Error returns the formatted error message. Callers of SchemaBuilder.Build
// treat the returned error as opaque; the format is documented on Build's
// Godoc but not on this private type.
func (e *buildError) Error() string {
	var b strings.Builder
	if caller := symbolizePC(e.callerPC); caller != "" {
		b.WriteString(caller)
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

// capturePC returns the program counter of the call site one level above the
// SchemaBuilder method that invoked it — i.e. the user's source line.
//
// Frame math, following runtime.Callers's "0 identifies Callers itself"
// semantics: 0 = runtime.Callers, 1 = capturePC, 2 = the SchemaBuilder method,
// 3 = user code. Every caller must therefore invoke this DIRECTLY from the
// public builder method; routing it through a helper shifts the attributed
// line with no compile-time signal. A zero return means the stack was too
// shallow to recover (vanishingly rare in practice; a benign degradation).
//
// Only the stack walk happens here. Resolving the PC to a file and line costs
// three allocations and roughly half the total latency, and on the success path
// — the overwhelmingly common case — the result is discarded unread, so that
// half is deferred to [symbolizePC] on the error-render path. PCs stay
// resolvable for the life of the process, so deferring is safe.
func capturePC() uintptr {
	var pcs [1]uintptr
	const userFrameSkip = 3
	if runtime.Callers(userFrameSkip, pcs[:]) == 0 {
		return 0
	}
	return pcs[0]
}

// symbolizePC resolves a PC captured by [capturePC] to "file:line", returning
// "" for the zero PC.
//
// It must resolve through runtime.CallersFrames, never runtime.FuncForPC:
// runtime.Callers records RETURN addresses, and CallersFrames applies the
// pc-1 adjustment that maps one back to the calling instruction. FuncForPC
// does not, so it would silently attribute the line following the call —
// off-by-one in the common case, and the wrong function entirely when a call
// is the last instruction of one.
func symbolizePC(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" {
		return ""
	}
	return frame.File + ":" + strconv.Itoa(frame.Line)
}

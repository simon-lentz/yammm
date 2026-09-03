package expr

import (
	"slices"
	"strings"
)

// BuiltinResult states how a pipeline builtin types its result, relative to
// its receiver. The static checker in the schema layer uses it to follow a
// type through a pipeline; the evaluator does not consult it.
type BuiltinResult uint8

const (
	// ResultScalar is a value with no members: a number, string, boolean,
	// nil or pattern.
	ResultScalar BuiltinResult = iota
	// ResultReceiver is the receiver's own type (Sort, Filter, Default).
	ResultReceiver
	// ResultElement is one element of the receiver (First, Last).
	ResultElement
	// ResultBodyList is a list whose element is the body's type (Map).
	ResultBodyList
	// ResultBody is the body's type (With).
	ResultBody
	// ResultFlattened is the receiver with one level of nesting removed.
	ResultFlattened
	// ResultList is a list of scalars (Split, Match).
	ResultList
	// ResultUnknown makes no claim (Reduce, Then, Lest, Coalesce).
	ResultUnknown
)

// ParamBinding states what a builtin binds its lambda parameters to.
type ParamBinding uint8

const (
	// BindNone: the builtin takes no body.
	BindNone ParamBinding = iota
	// BindElement: the single parameter (or $0) is one element of the receiver.
	BindElement
	// BindReceiver: the single parameter (or $0) is the receiver itself.
	BindReceiver
	// BindAccumulatorElement: the first parameter (or $0) is the accumulator,
	// the second (or $1) is one element of the receiver.
	BindAccumulatorElement
)

// BuiltinSpec describes one pipeline builtin as the language defines it. The
// evaluator enforces the arity fields; the static checker uses all of them.
// Both read this one table, so neither can drift from the other.
type BuiltinSpec struct {
	Name       string
	MinArgs    int
	MaxArgs    int // -1 is unbounded
	MaxParams  int
	AcceptBody bool
	Result     BuiltinResult
	Params     ParamBinding
}

var builtinSpecs = map[string]BuiltinSpec{}

func spec(name string, minArgs, maxArgs, maxParams int, acceptBody bool, result BuiltinResult, params ParamBinding) {
	builtinSpecs[strings.ToLower(name)] = BuiltinSpec{
		Name: name, MinArgs: minArgs, MaxArgs: maxArgs, MaxParams: maxParams,
		AcceptBody: acceptBody, Result: result, Params: params,
	}
}

func init() {
	// Collection
	spec("Reduce", 0, 1, 2, true, ResultUnknown, BindAccumulatorElement)
	spec("Map", 0, 0, 1, true, ResultBodyList, BindElement)
	spec("Filter", 0, 0, 1, true, ResultReceiver, BindElement)
	spec("Count", 0, 0, 1, true, ResultScalar, BindElement)
	spec("All", 0, 0, 1, true, ResultScalar, BindElement)
	spec("Any", 0, 0, 1, true, ResultScalar, BindElement)
	spec("AllOrNone", 0, 0, 1, true, ResultScalar, BindElement)
	spec("Compact", 0, 0, 0, false, ResultReceiver, BindNone)
	spec("Unique", 0, 0, 0, false, ResultReceiver, BindNone)
	spec("Len", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Sum", 0, 0, 0, false, ResultScalar, BindNone)
	spec("First", 0, 0, 0, false, ResultElement, BindNone)
	spec("Last", 0, 0, 0, false, ResultElement, BindNone)
	spec("Sort", 0, 0, 0, false, ResultReceiver, BindNone)
	spec("Reverse", 0, 0, 0, false, ResultReceiver, BindNone)
	spec("Flatten", 0, 0, 0, false, ResultFlattened, BindNone)
	spec("Contains", 1, 1, 0, false, ResultScalar, BindNone)

	// Control flow
	spec("Then", 0, 0, 1, true, ResultUnknown, BindReceiver)
	spec("Lest", 0, 0, 1, true, ResultUnknown, BindReceiver)
	spec("With", 0, 0, 1, true, ResultBody, BindReceiver)

	// Numeric
	spec("Abs", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Floor", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Ceil", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Round", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Min", 0, 1, 0, false, ResultScalar, BindNone)
	spec("Max", 0, 1, 0, false, ResultScalar, BindNone)
	spec("Compare", 1, 1, 0, false, ResultScalar, BindNone)

	// String
	spec("Upper", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Lower", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Trim", 0, 0, 0, false, ResultScalar, BindNone)
	spec("TrimPrefix", 1, 1, 0, false, ResultScalar, BindNone)
	spec("TrimSuffix", 1, 1, 0, false, ResultScalar, BindNone)
	spec("Split", 1, 1, 0, false, ResultList, BindNone)
	spec("Join", 1, 1, 0, false, ResultScalar, BindNone)
	spec("StartsWith", 1, 1, 0, false, ResultScalar, BindNone)
	spec("EndsWith", 1, 1, 0, false, ResultScalar, BindNone)
	spec("Replace", 2, 2, 0, false, ResultScalar, BindNone)
	spec("Substring", 1, 2, 0, false, ResultScalar, BindNone)

	// Pattern matching
	spec("Match", 1, 1, 0, false, ResultList, BindNone)

	// Utility
	spec("TypeOf", 0, 0, 0, false, ResultScalar, BindNone)
	spec("IsNil", 0, 0, 0, false, ResultScalar, BindNone)
	spec("Default", 1, 1, 0, false, ResultReceiver, BindNone)
	spec("Coalesce", 1, -1, 0, false, ResultUnknown, BindNone)
}

// LookupBuiltin returns the spec for a builtin by name, matched
// case-insensitively as the pipeline resolves names.
func LookupBuiltin(name string) (BuiltinSpec, bool) {
	s, ok := builtinSpecs[strings.ToLower(name)]
	return s, ok
}

// Builtins returns every builtin spec, ordered by name.
func Builtins() []BuiltinSpec {
	out := make([]BuiltinSpec, 0, len(builtinSpecs))
	for _, s := range builtinSpecs {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b BuiltinSpec) int { return strings.Compare(a.Name, b.Name) })
	return out
}

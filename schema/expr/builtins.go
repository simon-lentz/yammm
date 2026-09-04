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
	// ResultFlattened is the receiver with one level of nesting removed; a
	// list whose elements are not lists is unchanged.
	ResultFlattened
	// ResultList is a list of scalars (Split, Match).
	ResultList
	// ResultElementOrArg is one element of a list receiver when the call has
	// no argument, and a scalar when it has one (Min, Max).
	ResultElementOrArg
	// ResultReceiverOrArg is the receiver's type when the receiver holds a
	// value and the argument's when it is nil (Default). The static checker
	// requires the two to be of one kind, so the stage after the call is typed
	// by a value it predicted, and types the result as their merge.
	ResultReceiverOrArg
	// ResultUnknown makes no claim (Reduce, Then, Lest, Coalesce).
	ResultUnknown
)

// ParamBinding states what a builtin binds its lambda parameters to.
type ParamBinding uint8

const (
	// BindNone: the builtin binds no parameter. Lest evaluates its body in
	// the caller's scope and so binds none although it takes a body.
	BindNone ParamBinding = iota
	// BindElement: the single parameter (or $0) is one element of the receiver.
	BindElement
	// BindReceiver: the single parameter (or $0) is the receiver itself.
	BindReceiver
	// BindAccumulatorElement: the first parameter (or $0) is the accumulator,
	// the second (or $1) is one element of the receiver.
	BindAccumulatorElement
)

// ReceiverKind states what a builtin accepts as its receiver. The evaluator
// refuses the rest on every input, so the static checker refuses it at load.
// A kind is as narrow as the implementation: a string builtin takes a string,
// not any scalar, because a number reaching it fails on every instance.
type ReceiverKind uint8

const (
	// RecvAny: any value, nil included.
	RecvAny ReceiverKind = iota
	// RecvList: a list. A scalar, an instance or an association key is refused.
	RecvList
	// RecvScalar: any scalar, an association key among them. A list or an
	// instance is refused. Compare ranks two values by strata, so it alone
	// takes any scalar.
	RecvScalar
	// RecvScalarList: a list of scalars. A list of instances is refused as a
	// scalar is, because the elements are ordered.
	RecvScalarList
	// RecvString: a string. A number, a boolean, a list or an instance is
	// refused.
	RecvString
	// RecvNumeric: a number. A string, a boolean, a list or an instance is
	// refused.
	RecvNumeric
	// RecvSized: a string, a list or a map (an instance among them); nil
	// yields zero. A number or a boolean is refused.
	RecvSized
	// RecvListOrArg: a list when the call has no argument, when the builtin
	// ranks the list's elements; a scalar when it has one, when the builtin
	// ranks receiver against argument and promises a scalar. An instance is
	// refused either way, a list with an argument. The receiver's mirror of
	// [ResultElementOrArg].
	RecvListOrArg
	// RecvStringList: a list of strings. A list of numbers or instances is
	// refused.
	RecvStringList
	// RecvNumericList: a list of numbers. A list of strings or instances is
	// refused.
	RecvNumericList
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
	Receiver   ReceiverKind
	Result     BuiltinResult
	Params     ParamBinding
}

var builtinSpecs = map[string]BuiltinSpec{}

func spec(name string, minArgs, maxArgs, maxParams int, acceptBody bool, recv ReceiverKind, result BuiltinResult, params ParamBinding) {
	builtinSpecs[strings.ToLower(name)] = BuiltinSpec{
		Name: name, MinArgs: minArgs, MaxArgs: maxArgs, MaxParams: maxParams,
		AcceptBody: acceptBody, Receiver: recv, Result: result, Params: params,
	}
}

func init() {
	// Collection
	spec("Reduce", 0, 1, 2, true, RecvList, ResultUnknown, BindAccumulatorElement)
	spec("Map", 0, 0, 1, true, RecvList, ResultBodyList, BindElement)
	spec("Filter", 0, 0, 1, true, RecvList, ResultReceiver, BindElement)
	spec("Count", 0, 0, 1, true, RecvList, ResultScalar, BindElement)
	spec("All", 0, 0, 1, true, RecvList, ResultScalar, BindElement)
	spec("Any", 0, 0, 1, true, RecvList, ResultScalar, BindElement)
	spec("AllOrNone", 0, 0, 1, true, RecvList, ResultScalar, BindElement)
	spec("Compact", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Unique", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Len", 0, 0, 0, false, RecvSized, ResultScalar, BindNone)
	spec("Sum", 0, 0, 0, false, RecvNumericList, ResultScalar, BindNone)
	spec("First", 0, 0, 0, false, RecvList, ResultElement, BindNone)
	spec("Last", 0, 0, 0, false, RecvList, ResultElement, BindNone)
	spec("Sort", 0, 0, 0, false, RecvScalarList, ResultReceiver, BindNone)
	spec("Reverse", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Flatten", 0, 0, 0, false, RecvList, ResultFlattened, BindNone)
	spec("Contains", 1, 1, 0, false, RecvList, ResultScalar, BindNone)

	// Control flow
	spec("Then", 0, 0, 1, true, RecvAny, ResultUnknown, BindReceiver)
	spec("Lest", 0, 0, 0, true, RecvAny, ResultUnknown, BindNone)
	spec("With", 0, 0, 1, true, RecvAny, ResultBody, BindReceiver)

	// Numeric
	spec("Abs", 0, 0, 0, false, RecvNumeric, ResultScalar, BindNone)
	spec("Floor", 0, 0, 0, false, RecvNumeric, ResultScalar, BindNone)
	spec("Ceil", 0, 0, 0, false, RecvNumeric, ResultScalar, BindNone)
	spec("Round", 0, 0, 0, false, RecvNumeric, ResultScalar, BindNone)
	spec("Min", 0, 1, 0, false, RecvListOrArg, ResultElementOrArg, BindNone)
	spec("Max", 0, 1, 0, false, RecvListOrArg, ResultElementOrArg, BindNone)
	spec("Compare", 1, 1, 0, false, RecvScalar, ResultScalar, BindNone)

	// String
	spec("Upper", 0, 0, 0, false, RecvString, ResultScalar, BindNone)
	spec("Lower", 0, 0, 0, false, RecvString, ResultScalar, BindNone)
	spec("Trim", 0, 0, 0, false, RecvString, ResultScalar, BindNone)
	spec("TrimPrefix", 1, 1, 0, false, RecvString, ResultScalar, BindNone)
	spec("TrimSuffix", 1, 1, 0, false, RecvString, ResultScalar, BindNone)
	spec("Split", 1, 1, 0, false, RecvString, ResultList, BindNone)
	spec("Join", 1, 1, 0, false, RecvStringList, ResultScalar, BindNone)
	spec("StartsWith", 1, 1, 0, false, RecvString, ResultScalar, BindNone)
	spec("EndsWith", 1, 1, 0, false, RecvString, ResultScalar, BindNone)
	spec("Replace", 2, 2, 0, false, RecvString, ResultScalar, BindNone)
	spec("Substring", 1, 2, 0, false, RecvString, ResultScalar, BindNone)

	// Pattern matching
	spec("Match", 1, 1, 0, false, RecvString, ResultList, BindNone)

	// Utility
	spec("TypeOf", 0, 0, 0, false, RecvAny, ResultScalar, BindNone)
	spec("IsNil", 0, 0, 0, false, RecvAny, ResultScalar, BindNone)
	spec("Default", 1, 1, 0, false, RecvAny, ResultReceiverOrArg, BindNone)
	spec("Coalesce", 1, -1, 0, false, RecvAny, ResultUnknown, BindNone)
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

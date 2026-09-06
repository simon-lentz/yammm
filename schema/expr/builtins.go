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
	// ResultNumber is a number (Len, Sum, Count, Abs, Floor, Ceil, Round,
	// Compare). The static checker types each result by its subkind, as it
	// types a receiver and an argument by theirs, so the stage after the call
	// is judged: a number into a string builtin is refused at load.
	ResultNumber BuiltinResult = iota
	// ResultReceiver is the receiver's own type (Sort, Filter, Default).
	ResultReceiver
	// ResultElement is one element of the receiver (First, Last).
	ResultElement
	// ResultBodyList is a list whose element is the body's type (Map).
	ResultBodyList
	// ResultBody is the body's type (With; Then, whose nil for an absent
	// receiver any type absorbs).
	ResultBody
	// ResultFlattened is the receiver with one level of nesting removed; a
	// list whose elements are not lists is unchanged.
	ResultFlattened
	// ResultList is a list of scalars (Split, Match).
	ResultList
	// ResultElementOrArg is one element of a list receiver when the call has
	// no argument, and the receiver or the argument when it has one (Min,
	// Max), typed as their join.
	ResultElementOrArg
	// ResultReceiverOrArg is the receiver's type when the receiver holds a
	// value and the first non-nil argument's when it is nil (Default,
	// Coalesce). The static checker types the result as the join of the
	// receiver and every argument and refuses alternatives of disjoint kinds,
	// so the stage after the call is typed by a value it predicted.
	ResultReceiverOrArg
	// ResultUnknown makes no claim (Reduce).
	ResultUnknown
	// ResultReceiverOrBody is the receiver's type when the receiver holds a
	// value and the body's when it is nil (Lest), typed as their join under
	// the rule [ResultReceiverOrArg] states.
	ResultReceiverOrBody
	// ResultString is a string (Upper, Lower, Trim, TrimPrefix, TrimSuffix,
	// Join, Replace, Substring, TypeOf).
	ResultString
	// ResultBoolean is a boolean (All, Any, AllOrNone, Contains, StartsWith,
	// EndsWith, IsNil).
	ResultBoolean
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
	// RecvOrdered: any value the total order ranks — a scalar, an association
	// key, a list. An instance is refused. Compare alone takes it.
	RecvOrdered
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

// ArgKind states what a builtin accepts at one argument position. As with
// [ReceiverKind], the evaluator refuses the rest on every input, so the static
// checker refuses it at load.
type ArgKind uint8

const (
	// ArgAny: any value, nil included (Contains, Default, Reduce's seed).
	ArgAny ArgKind = iota
	// ArgString: a string. A number, a boolean, a list or an instance is
	// refused (TrimPrefix, Split, Join's separator, Replace's two).
	ArgString
	// ArgNumber: a number (Substring's indices).
	ArgNumber
	// ArgPattern: a regular-expression literal (Match).
	ArgPattern
	// ArgOrdered: any value the total order ranks — a scalar, an association
	// key, a list. An instance is refused (Compare, and Min and Max with an
	// argument).
	ArgOrdered
)

// String names the kind as a diagnostic reads it.
func (k ArgKind) String() string {
	switch k {
	case ArgString:
		return "a string"
	case ArgNumber:
		return "a number"
	case ArgPattern:
		return "a pattern"
	case ArgOrdered:
		return "a value the total order ranks"
	case ArgAny:
	}
	return "any value"
}

// BuiltinSpec describes one pipeline builtin as the language defines it. The
// evaluator enforces the arity fields; the static checker uses all of them.
// Both read this one table, so neither can drift from the other.
//
// Args states the kind at each argument position, one entry per position up
// to MaxArgs; for an unbounded builtin the last entry repeats. It is empty
// exactly when the builtin takes no argument.
type BuiltinSpec struct {
	Name       string
	MinArgs    int
	MaxArgs    int // -1 is unbounded
	MaxParams  int
	AcceptBody bool
	Receiver   ReceiverKind
	Result     BuiltinResult
	Params     ParamBinding
	Args       []ArgKind
}

// ArgAt returns the kind the builtin accepts at 0-based argument position i:
// the entry at i, or the last entry when the builtin's arguments repeat. It
// reports false for a builtin that takes no argument.
func (s BuiltinSpec) ArgAt(i int) (ArgKind, bool) {
	if len(s.Args) == 0 || i < 0 {
		return ArgAny, false
	}
	if i >= len(s.Args) {
		return s.Args[len(s.Args)-1], true
	}
	return s.Args[i], true
}

var builtinSpecs = map[string]BuiltinSpec{}

func spec(name string, minArgs, maxArgs, maxParams int, acceptBody bool, recv ReceiverKind, result BuiltinResult, params ParamBinding, args ...ArgKind) {
	builtinSpecs[strings.ToLower(name)] = BuiltinSpec{
		Name: name, MinArgs: minArgs, MaxArgs: maxArgs, MaxParams: maxParams,
		AcceptBody: acceptBody, Receiver: recv, Result: result, Params: params, Args: args,
	}
}

func init() {
	// Collection
	spec("Reduce", 0, 1, 2, true, RecvList, ResultUnknown, BindAccumulatorElement, ArgAny)
	spec("Map", 0, 0, 1, true, RecvList, ResultBodyList, BindElement)
	spec("Filter", 0, 0, 1, true, RecvList, ResultReceiver, BindElement)
	spec("Count", 0, 0, 1, true, RecvList, ResultNumber, BindElement)
	spec("All", 0, 0, 1, true, RecvList, ResultBoolean, BindElement)
	spec("Any", 0, 0, 1, true, RecvList, ResultBoolean, BindElement)
	spec("AllOrNone", 0, 0, 1, true, RecvList, ResultBoolean, BindElement)
	spec("Compact", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Unique", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Len", 0, 0, 0, false, RecvSized, ResultNumber, BindNone)
	spec("Sum", 0, 0, 0, false, RecvNumericList, ResultNumber, BindNone)
	spec("First", 0, 0, 0, false, RecvList, ResultElement, BindNone)
	spec("Last", 0, 0, 0, false, RecvList, ResultElement, BindNone)
	spec("Sort", 0, 0, 0, false, RecvScalarList, ResultReceiver, BindNone)
	spec("Reverse", 0, 0, 0, false, RecvList, ResultReceiver, BindNone)
	spec("Flatten", 0, 0, 0, false, RecvList, ResultFlattened, BindNone)
	spec("Contains", 1, 1, 0, false, RecvList, ResultBoolean, BindNone, ArgAny)

	// Control flow
	spec("Then", 0, 0, 1, true, RecvAny, ResultBody, BindReceiver)
	spec("Lest", 0, 0, 0, true, RecvAny, ResultReceiverOrBody, BindNone)
	spec("With", 0, 0, 1, true, RecvAny, ResultBody, BindReceiver)

	// Numeric
	spec("Abs", 0, 0, 0, false, RecvNumeric, ResultNumber, BindNone)
	spec("Floor", 0, 0, 0, false, RecvNumeric, ResultNumber, BindNone)
	spec("Ceil", 0, 0, 0, false, RecvNumeric, ResultNumber, BindNone)
	spec("Round", 0, 0, 0, false, RecvNumeric, ResultNumber, BindNone)
	spec("Min", 0, 1, 0, false, RecvListOrArg, ResultElementOrArg, BindNone, ArgOrdered)
	spec("Max", 0, 1, 0, false, RecvListOrArg, ResultElementOrArg, BindNone, ArgOrdered)
	spec("Compare", 1, 1, 0, false, RecvOrdered, ResultNumber, BindNone, ArgOrdered)

	// String
	spec("Upper", 0, 0, 0, false, RecvString, ResultString, BindNone)
	spec("Lower", 0, 0, 0, false, RecvString, ResultString, BindNone)
	spec("Trim", 0, 0, 0, false, RecvString, ResultString, BindNone)
	spec("TrimPrefix", 1, 1, 0, false, RecvString, ResultString, BindNone, ArgString)
	spec("TrimSuffix", 1, 1, 0, false, RecvString, ResultString, BindNone, ArgString)
	spec("Split", 1, 1, 0, false, RecvString, ResultList, BindNone, ArgString)
	spec("Join", 1, 1, 0, false, RecvStringList, ResultString, BindNone, ArgString)
	spec("StartsWith", 1, 1, 0, false, RecvString, ResultBoolean, BindNone, ArgString)
	spec("EndsWith", 1, 1, 0, false, RecvString, ResultBoolean, BindNone, ArgString)
	spec("Replace", 2, 2, 0, false, RecvString, ResultString, BindNone, ArgString, ArgString)
	spec("Substring", 1, 2, 0, false, RecvString, ResultString, BindNone, ArgNumber, ArgNumber)

	// Pattern matching
	spec("Match", 1, 1, 0, false, RecvString, ResultList, BindNone, ArgPattern)

	// Utility
	spec("TypeOf", 0, 0, 0, false, RecvAny, ResultString, BindNone)
	spec("IsNil", 0, 0, 0, false, RecvAny, ResultBoolean, BindNone)
	spec("Default", 1, 1, 0, false, RecvAny, ResultReceiverOrArg, BindNone, ArgAny)
	spec("Coalesce", 1, -1, 0, false, RecvAny, ResultReceiverOrArg, BindNone, ArgAny)
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

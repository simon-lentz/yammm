package eval

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema/expr"
)

// builtinEvaluator is the interface that builtins use to evaluate sub-expressions.
// This is passed to builtin functions so they can evaluate body expressions.
type builtinEvaluator interface {
	// evaluate evaluates an expression in the given scope.
	evaluate(e expr.Expression, scope Scope) (any, error)
}

// builtinFunc is the signature for builtin function implementations.
// lhs is the left-hand side value (receiver for method-style calls).
// args are the evaluated positional arguments.
// params are the lambda parameter names (for functions with body).
// body is the unevaluated body expression for lambdas.
// scope is the evaluation scope.
// ev allows evaluating sub-expressions.
type builtinFunc func(ev builtinEvaluator, lhs any, args []any, params []string, body expr.Expression, scope Scope) (any, error)

// builtinDef pairs a builtin's implementation with the spec the language
// defines for it in [expr.BuiltinSpec]; the arity rules live only there.
type builtinDef struct {
	spec expr.BuiltinSpec
	fn   builtinFunc
}

// builtinRegistry holds builtin function definitions.
var builtinRegistry = map[string]builtinDef{}

func init() {
	registerBuiltins()
}

func registerBuiltins() {
	// Collection builtins
	register("Reduce", builtinReduce)
	register("Map", builtinMap)
	register("Filter", builtinFilter)
	register("Count", builtinCount)
	register("All", builtinAll)
	register("Any", builtinAny)
	register("AllOrNone", builtinAllOrNone)
	register("Compact", builtinCompact)
	register("Unique", builtinUnique)
	register("Len", builtinLen)
	register("Sum", builtinSum)
	register("First", builtinFirst)
	register("Last", builtinLast)
	register("Sort", builtinSort)
	register("Reverse", builtinReverse)
	register("Flatten", builtinFlatten)
	register("Contains", builtinContains)

	// Control flow builtins
	register("Then", builtinThen)
	register("Lest", builtinLest)
	register("With", builtinWith)

	// Numeric builtins
	register("Abs", builtinAbs)
	register("Floor", builtinFloor)
	register("Ceil", builtinCeil)
	register("Round", builtinRound)
	register("Min", builtinMin)
	register("Max", builtinMax)
	register("Compare", builtinCompare)

	// String builtins
	register("Upper", builtinUpper)
	register("Lower", builtinLower)
	register("Trim", builtinTrim)
	register("TrimPrefix", builtinTrimPrefix)
	register("TrimSuffix", builtinTrimSuffix)
	register("Split", builtinSplit)
	register("Join", builtinJoin)
	register("StartsWith", builtinStartsWith)
	register("EndsWith", builtinEndsWith)
	register("Replace", builtinReplace)
	register("Substring", builtinSubstring)

	// Pattern matching
	register("Match", builtinMatch)

	// Utility builtins
	register("TypeOf", builtinTypeOf)
	register("IsNil", builtinIsNil)
	register("Default", builtinDefault)
	register("Coalesce", builtinCoalesce)
}

// register binds an implementation to the catalogue entry of the same name.
// A name the catalogue does not define is a programming error caught at init.
func register(name string, fn builtinFunc) {
	spec, ok := expr.LookupBuiltin(name)
	if !ok {
		panic("eval: builtin " + name + " is not in the expr catalogue")
	}
	builtinRegistry[strings.ToLower(name)] = builtinDef{spec: spec, fn: fn}
}

// lookupBuiltin returns the builtin definition if it exists.
func lookupBuiltin(name string) (builtinDef, bool) {
	def, ok := builtinRegistry[name]
	return def, ok
}

// --- Collection Builtin implementations ---

func builtinReduce(ev builtinEvaluator, lhs any, args []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("Reduce", lhs)
	if err != nil {
		return nil, err
	}

	hasStart := len(args) > 0
	memoName := "0"
	nextName := "1"
	switch len(params) {
	case 1:
		memoName = params[0]
	case 2:
		memoName, nextName = params[0], params[1]
	}

	if len(slice) == 0 {
		if hasStart {
			return args[0], nil
		}
		return nil, errors.New("reduce of empty sequence with no initial value")
	}

	var memo any
	startIdx := 0
	if hasStart {
		memo = args[0]
	} else {
		memo = slice[0]
		startIdx = 1
	}

	for i := startIdx; i < len(slice); i++ {
		childScope := scope.WithVar(memoName, memo).WithVar(nextName, slice[i])
		result, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		memo = result
	}
	return memo, nil
}

func builtinMap(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("Map", lhs)
	if err != nil {
		return nil, err
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	result := make([]any, len(slice))
	for i, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

func builtinFilter(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("Filter", lhs)
	if err != nil {
		return nil, err
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, errors.New("filter expression did not return a boolean value")
		}
		if b {
			result = append(result, elem)
		}
	}
	return result, nil
}

func builtinCount(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("Count", lhs)
	if err != nil {
		return nil, err
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	var count int64
	for _, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, errors.New("count expression did not return a boolean value")
		}
		if b {
			count++
		}
	}
	return count, nil
}

func builtinAll(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("All", lhs)
	if err != nil {
		return nil, err
	}

	// Empty slice returns true (vacuous truth: all zero elements satisfy any predicate)
	if len(slice) == 0 {
		return true, nil
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	for _, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, errors.New("all expression did not return a boolean value")
		}
		if !b {
			return false, nil
		}
	}
	return true, nil
}

func builtinAny(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("Any", lhs)
	if err != nil {
		return nil, err
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	for _, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, errors.New("any expression did not return a boolean value")
		}
		if b {
			return true, nil
		}
	}
	return false, nil
}

func builtinAllOrNone(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	slice, err := asSlice("AllOrNone", lhs)
	if err != nil {
		return nil, err
	}

	// Empty slice returns true per spec
	if len(slice) == 0 {
		return true, nil
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	count := 0
	for _, elem := range slice {
		childScope := scope.WithVar(paramName, elem)
		val, err := ev.evaluate(body, childScope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, errors.New("AllOrNone expression did not return a boolean value")
		}
		if b {
			count++
		}
	}
	return count == 0 || count == len(slice), nil
}

func builtinCompact(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Compact", lhs)
	if err != nil {
		return nil, err
	}

	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		if elem != nil {
			result = append(result, elem)
		}
	}
	return result, nil
}

func builtinUnique(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Unique", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return []any{}, nil
	}

	// value.Equal is the DSL's equality: NaN equals NaN, 1 equals 1.0, and
	// an instance equals another structurally. A Go map keyed on the element
	// would follow IEEE 754 and could not hold an instance at all.
	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		found := false
		for _, r := range result {
			if value.Equal(elem, r) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, elem)
		}
	}
	return result, nil
}

func builtinLen(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	switch v := lhs.(type) {
	case nil:
		return int64(0), nil
	case string:
		// Use rune count per SPEC: string length is counted in runes, not bytes
		return int64(utf8.RuneCountInString(v)), nil
	case []any:
		return int64(len(v)), nil
	case immutable.Slice:
		return int64(v.Len()), nil
	case immutable.Map[string]:
		// A Map is a struct, so the reflect.Map arm below never sees it.
		return int64(v.Len()), nil
	}

	rv := reflect.ValueOf(lhs)
	if !rv.IsValid() {
		return nil, fmt.Errorf("Len() unsupported for type %T", lhs)
	}

	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return int64(0), nil
		}
		return int64(rv.Len()), nil
	case reflect.Array:
		return int64(rv.Len()), nil
	case reflect.Map:
		if rv.IsNil() {
			return int64(0), nil
		}
		return int64(rv.Len()), nil
	}

	return nil, fmt.Errorf("Len() unsupported for type %T", lhs)
}

func builtinSum(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Sum", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return int64(0), nil
	}

	// Classify first, then sum in the kind the result has: a list holding a
	// float is float arithmetic, and an integer subtotal it would discard
	// cannot overflow it. An integer carried by a json.Number is an integer.
	ints := make([]int64, 0, len(slice))
	floats := make([]float64, 0, len(slice))
	hasFloat := false
	for _, elem := range slice {
		if i, ok := value.GetInt64(elem); ok {
			ints = append(ints, i)
			floats = append(floats, float64(i))
		} else if f, ok := value.GetFloat64(elem); ok {
			hasFloat = true
			floats = append(floats, f)
		} else {
			return nil, fmt.Errorf("Sum() expects numeric elements, got %T", elem)
		}
	}
	if hasFloat {
		var sum float64
		for _, f := range floats {
			sum += f
		}
		return sum, nil
	}
	var sum int64
	for _, i := range ints {
		next, err := checkedAdd(sum, i)
		if err != nil {
			return nil, errors.New("integer overflow in Sum")
		}
		sum = next
	}
	return sum, nil
}

func builtinFirst(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("First", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return nil, nil //nolint:nilnil // First of empty returns nil per spec
	}
	return slice[0], nil
}

func builtinLast(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Last", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return nil, nil //nolint:nilnil // Last of empty returns nil per spec
	}
	return slice[len(slice)-1], nil
}

func builtinSort(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Sort", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return []any{}, nil
	}

	// Create a copy to avoid mutating the original
	result := make([]any, len(slice))
	copy(result, slice)

	// Capture first comparison error during sort
	var sortErr error

	// Sort using value.Order
	slices.SortFunc(result, func(a, b any) int {
		if sortErr != nil {
			return 0 // Already have an error, just return 0 to complete sort
		}
		cmp, err := value.Order(a, b)
		if err != nil {
			sortErr = fmt.Errorf("sort: %w", err)
			return 0
		}
		return cmp
	})

	if sortErr != nil {
		return nil, sortErr
	}
	return result, nil
}

func builtinReverse(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Reverse", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return []any{}, nil
	}

	// Create a reversed copy
	result := make([]any, len(slice))
	for i, v := range slice {
		result[len(slice)-1-i] = v
	}
	return result, nil
}

func builtinFlatten(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Flatten", lhs)
	if err != nil {
		return nil, err
	}

	if len(slice) == 0 {
		return []any{}, nil
	}

	// Flatten one level of nesting; an element that is not a list is kept.
	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		if inner, ok := value.ListElems(elem); ok {
			result = append(result, inner...)
		} else {
			result = append(result, elem)
		}
	}
	return result, nil
}

func builtinContains(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Contains", lhs)
	if err != nil {
		return nil, err
	}

	target := args[0]
	for _, elem := range slice {
		if value.Equal(elem, target) {
			return true, nil
		}
	}
	return false, nil
}

// --- Control Flow Builtin implementations ---

func builtinThen(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	if lhs == nil {
		return nil, nil //nolint:nilnil // short-circuit: nothing to evaluate
	}

	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	childScope := scope.WithVar(paramName, lhs)
	return ev.evaluate(body, childScope)
}

func builtinLest(ev builtinEvaluator, lhs any, _ []any, _ []string, body expr.Expression, scope Scope) (any, error) {
	if lhs != nil {
		return lhs, nil
	}
	return ev.evaluate(body, scope)
}

func builtinWith(ev builtinEvaluator, lhs any, _ []any, params []string, body expr.Expression, scope Scope) (any, error) {
	paramName := "0"
	if len(params) > 0 {
		paramName = params[0]
	}

	childScope := scope.WithVar(paramName, lhs)
	return ev.evaluate(body, childScope)
}

// --- Numeric Builtin implementations ---

func builtinAbs(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if i, ok := value.GetInt64(lhs); ok {
		if i == math.MinInt64 {
			return nil, errors.New("integer overflow in Abs")
		}
		if i < 0 {
			return -i, nil
		}
		return i, nil
	}
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Abs(f), nil
	}
	return nil, fmt.Errorf("Abs() expects numeric argument, got %T", lhs)
}

func builtinFloor(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Floor(f), nil
	}
	return nil, fmt.Errorf("Floor() expects numeric argument, got %T", lhs)
}

func builtinCeil(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Ceil(f), nil
	}
	return nil, fmt.Errorf("Ceil() expects numeric argument, got %T", lhs)
}

func builtinRound(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	if f, ok := value.GetFloat64(lhs); ok {
		return math.RoundToEven(f), nil
	}
	return nil, fmt.Errorf("Round() expects numeric argument, got %T", lhs)
}

func builtinMin(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	// Two-arg form: Min(a, b)
	if len(args) == 1 {
		if err := refuseListReceiver("Min", lhs); err != nil {
			return nil, err
		}
		cmp, err := value.Order(lhs, args[0])
		if err != nil {
			return nil, fmt.Errorf("min: %w", err)
		}
		if cmp <= 0 {
			return lhs, nil
		}
		return args[0], nil
	}

	// Slice form: [1,2,3].Min()
	slice, err := asSlice("Min", lhs)
	if err != nil {
		return nil, err
	}
	if len(slice) == 0 {
		return nil, errors.New("min of empty sequence")
	}

	result := slice[0]
	for i := 1; i < len(slice); i++ {
		cmp, err := value.Order(slice[i], result)
		if err != nil {
			return nil, fmt.Errorf("min: %w", err)
		}
		if cmp < 0 {
			result = slice[i]
		}
	}
	return result, nil
}

func builtinMax(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	// Two-arg form: Max(a, b)
	if len(args) == 1 {
		if err := refuseListReceiver("Max", lhs); err != nil {
			return nil, err
		}
		cmp, err := value.Order(lhs, args[0])
		if err != nil {
			return nil, fmt.Errorf("max: %w", err)
		}
		if cmp >= 0 {
			return lhs, nil
		}
		return args[0], nil
	}

	// Slice form: [1,2,3].Max()
	slice, err := asSlice("Max", lhs)
	if err != nil {
		return nil, err
	}
	if len(slice) == 0 {
		return nil, errors.New("max of empty sequence")
	}

	result := slice[0]
	for i := 1; i < len(slice); i++ {
		cmp, err := value.Order(slice[i], result)
		if err != nil {
			return nil, fmt.Errorf("max: %w", err)
		}
		if cmp > 0 {
			result = slice[i]
		}
	}
	return result, nil
}

// refuseListReceiver is the two-argument form's guard for Min and Max: they
// rank a scalar against the argument and promise a scalar, where value.Order
// would rank a list above every scalar and hand the list back. nil is a
// scalar here — it ranks below everything.
func refuseListReceiver(name string, lhs any) error {
	if lhs == nil {
		return nil
	}
	if _, err := asSlice(name, lhs); err == nil {
		return fmt.Errorf("%s ranks its receiver against its argument, and a list cannot be ranked against a scalar", name)
	}
	return nil
}

func builtinCompare(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	cmp, err := value.Order(lhs, args[0])
	if err != nil {
		return nil, fmt.Errorf("compare: %w", err)
	}
	return int64(cmp), nil
}

// --- String Builtin implementations ---

func builtinUpper(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Upper() expects string argument, got %T", lhs)
	}
	return strings.ToUpper(s), nil
}

func builtinLower(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Lower() expects string argument, got %T", lhs)
	}
	return strings.ToLower(s), nil
}

func builtinTrim(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Trim() expects string argument, got %T", lhs)
	}
	return strings.TrimSpace(s), nil
}

func builtinTrimPrefix(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("TrimPrefix() expects string receiver, got %T", lhs)
	}
	prefix, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("TrimPrefix() expects string argument, got %T", args[0])
	}
	return strings.TrimPrefix(s, prefix), nil
}

func builtinTrimSuffix(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("TrimSuffix() expects string receiver, got %T", lhs)
	}
	suffix, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("TrimSuffix() expects string argument, got %T", args[0])
	}
	return strings.TrimSuffix(s, suffix), nil
}

func builtinSplit(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Split() expects string receiver, got %T", lhs)
	}
	sep, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("Split() expects string separator, got %T", args[0])
	}
	parts := strings.Split(s, sep)
	// Convert to []any for consistency
	result := make([]any, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result, nil
}

func builtinJoin(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	slice, err := asSlice("Join", lhs)
	if err != nil {
		return nil, err
	}
	sep, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("Join() expects string separator, got %T", args[0])
	}
	// Convert elements to strings
	parts := make([]string, len(slice))
	for i, elem := range slice {
		s, ok := elem.(string)
		if !ok {
			return nil, fmt.Errorf("Join() expects all string elements, got %T at index %d", elem, i)
		}
		parts[i] = s
	}
	return strings.Join(parts, sep), nil
}

func builtinStartsWith(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("StartsWith() expects string receiver, got %T", lhs)
	}
	prefix, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("StartsWith() expects string argument, got %T", args[0])
	}
	return strings.HasPrefix(s, prefix), nil
}

func builtinEndsWith(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("EndsWith() expects string receiver, got %T", lhs)
	}
	suffix, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("EndsWith() expects string argument, got %T", args[0])
	}
	return strings.HasSuffix(s, suffix), nil
}

func builtinReplace(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string receiver, got %T", lhs)
	}
	old, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string for old value, got %T", args[0])
	}
	replacement, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string for new value, got %T", args[1])
	}
	return strings.ReplaceAll(s, old, replacement), nil
}

func builtinSubstring(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Substring() expects string receiver, got %T", lhs)
	}

	start, ok := value.GetInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("Substring() expects integer start index, got %T", args[0])
	}

	// Indexes are runes, and every clamp runs in int64: narrowing first would
	// wrap an index no int can hold into range on a 32-bit build.
	runes := []rune(s)
	length := int64(len(runes))

	if start < 0 {
		start += length
	}
	if start < 0 {
		start = 0
	}
	if start > length {
		return "", nil
	}

	end := length
	if len(args) == 2 {
		end, ok = value.GetInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("Substring() expects integer end index, got %T", args[1])
		}
		if end < 0 {
			end += length
		}
	}
	if end < start {
		return "", nil
	}
	if end > length {
		end = length
	}

	return string(runes[int(start):int(end)]), nil
}

// --- Pattern Matching Builtin ---

func builtinMatch(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	re, ok := args[0].(*regexp.Regexp)
	if !ok {
		return nil, fmt.Errorf("match expects regexp argument, got %T", args[0])
	}

	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("match expects string receiver, got %T", lhs)
	}

	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return nil, nil //nolint:nilnil // no match returns nil
	}

	// Convert to []any for consistency
	result := make([]any, len(matches))
	for i, m := range matches {
		result[i] = m
	}
	return result, nil
}

// --- Utility Builtin implementations ---

func builtinTypeOf(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	return dslTypeName(lhs), nil
}

// dslTypeName maps an evaluator value onto the DSL type vocabulary that TypeOf
// reports: "nil", "boolean", "integer", "float", "string", "list", "map",
// "pattern", or "unknown" for any shape outside it. A scalar is read the way
// [value.Classify] reads it, so a json.Number is the number it spells and a
// named carrier is its base kind; the vocabulary is finer than
// [value.TypeStrata]'s, which groups every numeric together and patterns with
// strings.
//
// A Timestamp, Date or UUID reports as "string": [CoerceValue] renders all
// three to text, and invariants evaluate on coerced values, so no time.Time or
// uuid.UUID reaches here.
func dslTypeName(v any) string {
	if v == nil {
		return "nil"
	}
	switch v.(type) {
	case *regexp.Regexp:
		return "pattern"
	case immutable.Slice:
		return "list"
	case immutable.Map[string]:
		return "map"
	}
	switch kind, _ := value.Classify(v); kind {
	case value.BoolKind:
		return "boolean"
	case value.IntKind:
		return "integer"
	case value.FloatKind:
		return "float"
	case value.StringKind:
		return "string"
	case value.UnspecifiedKind:
	}
	if t := reflect.TypeOf(v); t != nil {
		switch t.Kind() {
		case reflect.Slice, reflect.Array:
			return "list"
		case reflect.Map:
			return "map"
		}
	}
	return "unknown"
}

func builtinIsNil(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if lhs == nil {
		return true, nil
	}
	// A typed nil: reflect.ValueOf never yields Kind Interface, so that
	// kind is not listed.
	rv := reflect.ValueOf(lhs)
	if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Map ||
		rv.Kind() == reflect.Slice || rv.Kind() == reflect.Chan || rv.Kind() == reflect.Func {
		return rv.IsNil(), nil
	}
	return false, nil
}

func builtinDefault(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if lhs == nil {
		return args[0], nil
	}
	return lhs, nil
}

func builtinCoalesce(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	// Check lhs first
	if lhs != nil {
		return lhs, nil
	}
	// Check each argument
	for _, arg := range args {
		if arg != nil {
			return arg, nil
		}
	}
	return nil, nil //nolint:nilnil // all values were nil
}

// asSlice converts a value to []any for iteration.
func asSlice(funcName string, val any) ([]any, error) {
	if val == nil {
		return []any{}, nil
	}
	elems, ok := value.ListElems(val)
	if !ok {
		return nil, fmt.Errorf("%s expects slice or array input, got %T", funcName, val)
	}
	return elems, nil
}

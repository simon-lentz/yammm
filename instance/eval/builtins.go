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

// builtinDef describes a builtin function.
type builtinDef struct {
	name       string
	minArgs    int
	maxArgs    int
	maxParams  int
	acceptBody bool
	fn         builtinFunc
}

// builtinRegistry holds builtin function definitions.
var builtinRegistry = map[string]builtinDef{}

func init() {
	registerBuiltins()
}

func registerBuiltins() {
	// Collection builtins
	register("Reduce", 0, 1, 2, true, builtinReduce)
	register("Map", 0, 0, 1, true, builtinMap)
	register("Filter", 0, 0, 1, true, builtinFilter)
	register("Count", 0, 0, 1, true, builtinCount)
	register("All", 0, 0, 1, true, builtinAll)
	register("Any", 0, 0, 1, true, builtinAny)
	register("AllOrNone", 0, 0, 1, true, builtinAllOrNone)
	register("Compact", 0, 0, 0, false, builtinCompact)
	register("Unique", 0, 0, 0, false, builtinUnique)
	register("Len", 0, 0, 0, false, builtinLen)
	register("Sum", 0, 0, 0, false, builtinSum)
	register("First", 0, 0, 0, false, builtinFirst)
	register("Last", 0, 0, 0, false, builtinLast)
	register("Sort", 0, 0, 0, false, builtinSort)
	register("Reverse", 0, 0, 0, false, builtinReverse)
	register("Flatten", 0, 0, 0, false, builtinFlatten)
	register("Contains", 1, 1, 0, false, builtinContains)

	// Control flow builtins
	register("Then", 0, 0, 1, true, builtinThen)
	register("Lest", 0, 0, 1, true, builtinLest)
	register("With", 0, 0, 1, true, builtinWith)

	// Numeric builtins
	register("Abs", 0, 0, 0, false, builtinAbs)
	register("Floor", 0, 0, 0, false, builtinFloor)
	register("Ceil", 0, 0, 0, false, builtinCeil)
	register("Round", 0, 0, 0, false, builtinRound)
	register("Min", 0, 1, 0, false, builtinMin)
	register("Max", 0, 1, 0, false, builtinMax)
	register("Compare", 1, 1, 0, false, builtinCompare)

	// String builtins
	register("Upper", 0, 0, 0, false, builtinUpper)
	register("Lower", 0, 0, 0, false, builtinLower)
	register("Trim", 0, 0, 0, false, builtinTrim)
	register("TrimPrefix", 1, 1, 0, false, builtinTrimPrefix)
	register("TrimSuffix", 1, 1, 0, false, builtinTrimSuffix)
	register("Split", 1, 1, 0, false, builtinSplit)
	register("Join", 1, 1, 0, false, builtinJoin)
	register("StartsWith", 1, 1, 0, false, builtinStartsWith)
	register("EndsWith", 1, 1, 0, false, builtinEndsWith)
	register("Replace", 2, 2, 0, false, builtinReplace)
	register("Substring", 1, 2, 0, false, builtinSubstring)

	// Pattern matching
	register("Match", 1, 1, 0, false, builtinMatch)

	// Utility builtins
	register("TypeOf", 0, 0, 0, false, builtinTypeOf)
	register("IsNil", 0, 0, 0, false, builtinIsNil)
	register("Default", 1, 1, 0, false, builtinDefault)
	register("Coalesce", 1, -1, 0, false, builtinCoalesce) // -1 means unlimited args
}

func register(name string, minArgs, maxArgs, maxParams int, acceptBody bool, fn builtinFunc) {
	// Store with lowercase key since evaluator normalizes to lowercase for lookup.
	// Display name (name field) keeps original casing for error messages/docs.
	builtinRegistry[strings.ToLower(name)] = builtinDef{
		name:       name,
		minArgs:    minArgs,
		maxArgs:    maxArgs,
		maxParams:  maxParams,
		acceptBody: acceptBody,
		fn:         fn,
	}
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
	if body == nil {
		return nil, errors.New("count function requires a lambda expression")
	}

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

	// Use value.ValueOrder for all comparisons to ensure semantic equality.
	// This handles edge cases like NaN correctly (NaN == NaN per DSL total ordering,
	// not IEEE 754 where NaN != NaN). Using map keys for comparable types would
	// break this semantic because Go's map equality follows IEEE 754.
	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		found := false
		for _, r := range result {
			cmp, err := value.ValueOrder(elem, r)
			if err == nil && cmp == 0 {
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

	// Determine if we should return int64 or float64 based on input types
	hasFloat := false
	var intSum int64
	var floatSum float64

	for _, elem := range slice {
		if f, ok := value.GetFloat64(elem); ok {
			hasFloat = true
			floatSum += f
		} else if i, ok := value.GetInt64(elem); ok {
			intSum += i
			floatSum += float64(i)
		} else {
			return nil, fmt.Errorf("Sum() expects numeric elements, got %T", elem)
		}
	}

	if hasFloat {
		return floatSum, nil
	}
	return intSum, nil
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

	// Sort using value.ValueOrder
	slices.SortFunc(result, func(a, b any) int {
		if sortErr != nil {
			return 0 // Already have an error, just return 0 to complete sort
		}
		cmp, err := value.ValueOrder(a, b)
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

	// Flatten one level of nesting
	result := make([]any, 0, len(slice))
	for _, elem := range slice {
		if inner, ok := elem.([]any); ok {
			result = append(result, inner...)
		} else if rv := reflect.ValueOf(elem); rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			for i := range rv.Len() {
				result = append(result, rv.Index(i).Interface())
			}
		} else {
			// Non-slice elements are kept as-is
			result = append(result, elem)
		}
	}
	return result, nil
}

func builtinContains(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("contains requires exactly one argument")
	}

	slice, err := asSlice("Contains", lhs)
	if err != nil {
		return nil, err
	}

	target := args[0]
	for _, elem := range slice {
		cmp, err := value.ValueOrder(elem, target)
		if err == nil && cmp == 0 {
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
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Abs(f), nil
	}
	if i, ok := value.GetInt64(lhs); ok {
		if i < 0 {
			return -i, nil
		}
		return i, nil
	}
	return nil, fmt.Errorf("Abs() expects numeric argument, got %T", lhs)
}

func builtinFloor(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Floor(f), nil
	}
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	return nil, fmt.Errorf("Floor() expects numeric argument, got %T", lhs)
}

func builtinCeil(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if f, ok := value.GetFloat64(lhs); ok {
		return math.Ceil(f), nil
	}
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	return nil, fmt.Errorf("Ceil() expects numeric argument, got %T", lhs)
}

func builtinRound(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if f, ok := value.GetFloat64(lhs); ok {
		return math.RoundToEven(f), nil
	}
	if i, ok := value.GetInt64(lhs); ok {
		return i, nil
	}
	return nil, fmt.Errorf("Round() expects numeric argument, got %T", lhs)
}

func builtinMin(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	// Two-arg form: Min(a, b)
	if len(args) == 1 {
		cmp, err := value.ValueOrder(lhs, args[0])
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
		cmp, err := value.ValueOrder(slice[i], result)
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
		cmp, err := value.ValueOrder(lhs, args[0])
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
		cmp, err := value.ValueOrder(slice[i], result)
		if err != nil {
			return nil, fmt.Errorf("max: %w", err)
		}
		if cmp > 0 {
			result = slice[i]
		}
	}
	return result, nil
}

func builtinCompare(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("compare requires exactly one argument")
	}
	cmp, err := value.ValueOrder(lhs, args[0])
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
	if len(args) != 1 {
		return nil, errors.New("TrimPrefix requires exactly one argument")
	}
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
	if len(args) != 1 {
		return nil, errors.New("TrimSuffix requires exactly one argument")
	}
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
	if len(args) != 1 {
		return nil, errors.New("split requires exactly one argument")
	}
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
	if len(args) != 1 {
		return nil, errors.New("join requires exactly one argument")
	}
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
	if len(args) != 1 {
		return nil, errors.New("StartsWith requires exactly one argument")
	}
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
	if len(args) != 1 {
		return nil, errors.New("EndsWith requires exactly one argument")
	}
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
	if len(args) != 2 {
		return nil, errors.New("replace requires exactly two arguments")
	}
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string receiver, got %T", lhs)
	}
	old, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string for old value, got %T", args[0])
	}
	new, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("Replace() expects string for new value, got %T", args[1])
	}
	return strings.ReplaceAll(s, old, new), nil
}

func builtinSubstring(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, errors.New("substring requires one or two arguments")
	}
	s, ok := lhs.(string)
	if !ok {
		return nil, fmt.Errorf("Substring() expects string receiver, got %T", lhs)
	}

	// Get start index
	startVal, ok := value.GetInt64(args[0])
	if !ok {
		return nil, fmt.Errorf("Substring() expects integer start index, got %T", args[0])
	}
	start := int(startVal)

	// Convert string to runes for proper Unicode handling
	runes := []rune(s)
	length := len(runes)

	// Handle negative start index (from end)
	if start < 0 {
		start = length + start
	}

	// Clamp start to valid range
	if start < 0 {
		start = 0
	}
	if start > length {
		return "", nil
	}

	// Get end index (optional)
	end := length
	if len(args) == 2 {
		endVal, ok := value.GetInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("Substring() expects integer end index, got %T", args[1])
		}
		end = int(endVal)

		// Handle negative end index (from end)
		if end < 0 {
			end = length + end
		}
	}

	// Clamp end to valid range
	if end < start {
		return "", nil
	}
	if end > length {
		end = length
	}

	return string(runes[start:end]), nil
}

// --- Pattern Matching Builtin ---

func builtinMatch(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("match requires exactly one argument")
	}

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
	if lhs == nil {
		return "nil", nil
	}
	return fmt.Sprintf("%T", lhs), nil
}

func builtinIsNil(_ builtinEvaluator, lhs any, _ []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if lhs == nil {
		return true, nil
	}
	// Check for nil interface values
	rv := reflect.ValueOf(lhs)
	if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface ||
		rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice ||
		rv.Kind() == reflect.Chan || rv.Kind() == reflect.Func {
		return rv.IsNil(), nil
	}
	return false, nil
}

func builtinDefault(_ builtinEvaluator, lhs any, args []any, _ []string, _ expr.Expression, _ Scope) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("default requires exactly one argument")
	}
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

// --- Helper functions ---

// asSlice converts a value to []any for iteration.
func asSlice(funcName string, val any) ([]any, error) {
	if val == nil {
		return []any{}, nil
	}

	if slice, ok := val.([]any); ok {
		return slice, nil
	}

	// Handle immutable.Slice (returned by property Unwrap for List-typed properties).
	if is, ok := val.(immutable.Slice); ok {
		result := make([]any, is.Len())
		for i, v := range is.Iter2() {
			result[i] = v.Unwrap()
		}
		return result, nil
	}

	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("%s expects slice or array input, got %T", funcName, val)
	}

	result := make([]any, rv.Len())
	for i := range rv.Len() {
		result[i] = rv.Index(i).Interface()
	}
	return result, nil
}

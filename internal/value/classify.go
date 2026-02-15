package value

import (
	"encoding/json"
	"reflect"
)

// Kind identifies the semantic type of a runtime value.
type Kind int

const (
	// UnspecifiedKind indicates an unknown or unsupported type.
	UnspecifiedKind Kind = iota
	// StringKind indicates a string value.
	StringKind
	// IntKind indicates an integer value.
	IntKind
	// FloatKind indicates a floating-point value.
	FloatKind
	// BoolKind indicates a boolean value.
	BoolKind
	// VectorKind indicates a slice of float32 or float64 values.
	VectorKind
)

// String returns the string representation of a Kind.
func (k Kind) String() string {
	switch k {
	case UnspecifiedKind:
		return "UnspecifiedKind"
	case StringKind:
		return "StringKind"
	case IntKind:
		return "IntKind"
	case FloatKind:
		return "FloatKind"
	case BoolKind:
		return "BoolKind"
	case VectorKind:
		return "VectorKind"
	default:
		return "UnknownKind"
	}
}

// Registry allows custom type recognition via reflect.Type hooks.
//
// The Registry hook is designed for Phase 3 (v2/instance/eval) integration.
// When instance validation is implemented, the evaluator will populate
// BaseKindOfReflectType with schema-aware type recognition. Until then,
// a zero-value Registry provides complete functionality via built-in detection.
type Registry struct {
	// BaseKindOfReflectType returns the Kind for a custom reflect.Type.
	// Returns UnspecifiedKind if the type is not recognized.
	// If nil, ClassifyWithRegistry falls back to built-in detection.
	BaseKindOfReflectType func(reflect.Type) Kind
}

// Classify normalizes a runtime value into a Kind and possibly transformed value.
// It is used by runtime validation to stay aligned with type checker expectations.
//
// For json.Number: attempts Int64() first, then Float64() to determine kind.
// For slices: detects and coerces to []float64 or []float32 for VectorKind.
func Classify(val any) (Kind, any) {
	return ClassifyWithRegistry(Registry{}, val)
}

// ClassifyWithRegistry normalizes a value using the provided type registry so custom
// RecognizeReflectType hooks participate in base-kind detection. A zero Registry
// falls back to built-in type detection.
//
// Pointers are automatically dereferenced before classification, so *int and int
// return the same Kind. Nil pointers return UnspecifiedKind.
//
// Registry hooks are checked BEFORE built-in slice handling, allowing custom slice
// types (e.g., type Vec []float64) to be recognized by hooks.
func ClassifyWithRegistry(registry Registry, val any) (Kind, any) {
	// Handle nil
	if val == nil {
		return UnspecifiedKind, val
	}

	// Dereference pointers first
	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return UnspecifiedKind, nil
		}
		rv = rv.Elem()
		val = rv.Interface()
	}

	// Type switch for primitives
	switch v := val.(type) {
	case json.Number:
		// Try integer first (no decimal point)
		if n, err := v.Int64(); err == nil {
			return IntKind, n
		}
		// Then try float
		if n, err := v.Float64(); err == nil {
			return FloatKind, n
		}
		return UnspecifiedKind, val
	case bool:
		return BoolKind, val
	case string:
		return StringKind, val
	case int, int8, int16, int32, int64:
		return IntKind, val
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return IntKind, val
	case float32, float64:
		return FloatKind, val
	}

	valType := reflect.TypeOf(val)
	if valType == nil {
		return UnspecifiedKind, val
	}

	// Check registry FIRST - allows custom types to override built-in behavior.
	// This enables custom slice types (e.g., type Vec []float64) to be recognized.
	if registry.BaseKindOfReflectType != nil {
		kind := registry.BaseKindOfReflectType(valType)
		if kind != UnspecifiedKind {
			return kind, val
		}
	}

	// Then built-in slice handling
	if valType.Kind() == reflect.Slice {
		return classifySlice(val)
	}

	return UnspecifiedKind, val
}

// jsonNumberType is used to detect []json.Number slices
var jsonNumberType = reflect.TypeFor[json.Number]()

func classifySlice(val any) (Kind, any) {
	// Fast path for typed float slices
	switch v := val.(type) {
	case []float64:
		return VectorKind, v
	case []float32:
		return VectorKind, v
	}

	rv := reflect.ValueOf(val)
	elemType := rv.Type().Elem()
	elemKind := elemType.Kind()

	// Handle nil slices - only float slices become VectorKind
	if rv.IsNil() {
		switch elemKind {
		case reflect.Float64:
			return VectorKind, []float64(nil)
		case reflect.Float32:
			return VectorKind, []float32(nil)
		default:
			return UnspecifiedKind, val
		}
	}

	// Handle empty slices - only typed float slices become VectorKind.
	// Empty []any{} returns UnspecifiedKind because it's genuinely ambiguous:
	// we cannot distinguish an empty vector from an empty string list or empty
	// object list without schema context. The validator (Phase 3) has schema
	// information and can properly interpret empty arrays. This follows the
	// "determine what it IS, not what it should be" principle.
	if rv.Len() == 0 {
		switch elemKind {
		case reflect.Float64:
			return VectorKind, []float64{}
		case reflect.Float32:
			return VectorKind, []float32{}
		default:
			return UnspecifiedKind, val
		}
	}

	// Only coerce certain slice types to VectorKind:
	// - []any (interface{}) slices with numeric elements
	// - []json.Number slices
	// Typed slices like []int, []string should return UnspecifiedKind.
	// Per spec: "Vector[N] | []float64, []float32, []any with numeric elements"
	isInterfaceSlice := elemKind == reflect.Interface
	isJSONNumberSlice := elemType == jsonNumberType
	if !isInterfaceSlice && !isJSONNumberSlice {
		return UnspecifiedKind, val
	}

	// Build vector from elements - always produce []float64 for []any input.
	// This ensures JSON input (which comes as []any) always produces []float64,
	// matching the architecture spec's "each element is coerced to float64".
	// Typed []float32 inputs are preserved by the fast path above.
	result := make([]float64, 0, rv.Len())
	for i := range rv.Len() {
		elem := rv.Index(i).Interface()
		fval, ok := toFloat64(elem)
		if !ok {
			return UnspecifiedKind, val
		}
		result = append(result, fval)
	}
	return VectorKind, result
}

// toFloat64 converts any numeric type to float64 for vector element coercion.
// Returns (value, true) if the element is numeric, (0, false) otherwise.
//
// All numeric types (int*, uint*, float32, float64, json.Number) are accepted.
// This is used for []any and []json.Number vector coercion where we always
// produce []float64 output.
func toFloat64(elem any) (float64, bool) {
	switch n := elem.(type) {
	// Float types
	case float64:
		return n, true
	case float32:
		return float64(n), true

	// Integer types (v2: coerced to float64 for vectors)
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uintptr:
		return float64(n), true

	// json.Number in vectors: coerce to float64
	case json.Number:
		if fv, err := n.Float64(); err == nil {
			return fv, true
		}
		return 0, false
	}

	// Reflect fallback for custom numeric types
	rv := reflect.ValueOf(elem)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	default:
		return 0, false
	}
}

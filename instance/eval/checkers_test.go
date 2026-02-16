package eval_test

import (
	"math"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsString(t *testing.T) {
	checker := eval.IsString()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"valid_string", "hello", true},
		{"empty_string", "", true},
		{"int", 42, false},
		{"float", 3.14, false},
		{"bool", true, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsInteger(t *testing.T) {
	checker := eval.IsInteger()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"int", 42, true},
		{"int32", int32(42), true},
		{"int64", int64(42), true},
		{"uint", uint(42), true},
		{"float", 3.14, false},
		{"string", "42", false},
		{"bool", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsFloat(t *testing.T) {
	checker := eval.IsFloat()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"float64", 3.14, true},
		{"float32", float32(3.14), true},
		{"int", 42, true}, // integers are valid floats
		{"int64", int64(42), true},
		{"string", "3.14", false},
		{"bool", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsBoolean(t *testing.T) {
	checker := eval.IsBoolean()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"true", true, true},
		{"false", false, true},
		{"int", 1, false},
		{"string", "true", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsUUID(t *testing.T) {
	checker := eval.IsUUID()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"valid_uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"invalid_uuid", "not-a-uuid", false},
		{"short_uuid", "550e8400-e29b-41d4", false},
		{"int", 42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsTimestamp(t *testing.T) {
	checker := eval.IsTimestamp()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"rfc3339", "2024-01-15T10:30:00Z", true},
		{"rfc3339_offset", "2024-01-15T10:30:00+05:00", true},
		{"rfc3339_nano", "2024-01-15T10:30:00.123456789Z", true},
		{"invalid_format", "2024/01/15", false},
		{"not_string", 12345, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestIsDate(t *testing.T) {
	checker := eval.IsDate()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"valid_date", "2024-01-15", true},
		{"invalid_format", "01/15/2024", false},
		{"datetime", "2024-01-15T10:30:00Z", false},
		{"not_string", 20240115, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-z]+$`)
	checker := eval.MatchesPattern(pattern)

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"matches", "hello", true},
		{"no_match", "Hello", false},
		{"with_numbers", "hello123", false},
		{"not_string", 123, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestInEnum(t *testing.T) {
	allowed := []string{"red", "green", "blue"}
	checker := eval.InEnum(allowed)

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		{"valid_red", "red", true},
		{"valid_green", "green", true},
		{"valid_blue", "blue", true},
		{"invalid_yellow", "yellow", false},
		{"case_sensitive", "RED", false},
		{"not_string", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

// --- CheckValue Tests ---

func TestCheckValue_Nil(t *testing.T) {
	// nil is always valid (required check is done separately)
	err := eval.CheckValue(nil, schema.NewStringConstraint())
	assert.NoError(t, err)
}

func TestCheckValue_String(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		{"valid_string", "hello", schema.NewStringConstraint(), false},
		{"empty_string", "", schema.NewStringConstraint(), false},
		{"wrong_type", 42, schema.NewStringConstraint(), true},
		{"min_length_ok", "abc", schema.NewStringConstraintBounded(3, 10), false},
		{"min_length_fail", "ab", schema.NewStringConstraintBounded(3, 10), true},
		{"max_length_ok", "abcdefghij", schema.NewStringConstraintBounded(1, 10), false},
		{"max_length_fail", "abcdefghijk", schema.NewStringConstraintBounded(1, 10), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Integer(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		{"valid_int", int64(42), schema.NewIntegerConstraint(), false},
		{"valid_int32", int32(42), schema.NewIntegerConstraint(), false},
		{"valid_uint", uint(42), schema.NewIntegerConstraint(), false},
		{"wrong_type_string", "42", schema.NewIntegerConstraint(), true},
		{"wrong_type_float", 3.14, schema.NewIntegerConstraint(), true},
		{"min_ok", int64(10), schema.NewIntegerConstraintBounded(10, true, 100, true), false},
		{"min_fail", int64(9), schema.NewIntegerConstraintBounded(10, true, 100, true), true},
		{"max_ok", int64(100), schema.NewIntegerConstraintBounded(10, true, 100, true), false},
		{"max_fail", int64(101), schema.NewIntegerConstraintBounded(10, true, 100, true), true},
		{"no_min", int64(-1000), schema.NewIntegerConstraintBounded(0, false, 100, true), false},
		{"no_max", int64(1000), schema.NewIntegerConstraintBounded(0, true, 0, false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Float(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		{"valid_float", 3.14, schema.NewFloatConstraint(), false},
		{"valid_int_as_float", int64(42), schema.NewFloatConstraint(), false},
		{"wrong_type_string", "3.14", schema.NewFloatConstraint(), true},
		{"wrong_type_bool", true, schema.NewFloatConstraint(), true},
		{"min_ok", 0.0, schema.NewFloatConstraintBounded(0.0, true, 1.0, true), false},
		{"min_fail", -0.1, schema.NewFloatConstraintBounded(0.0, true, 1.0, true), true},
		{"max_ok", 1.0, schema.NewFloatConstraintBounded(0.0, true, 1.0, true), false},
		{"max_fail", 1.1, schema.NewFloatConstraintBounded(0.0, true, 1.0, true), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Boolean(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"true", true, false},
		{"false", false, false},
		{"wrong_type_int", 1, true},
		{"wrong_type_string", "true", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewBooleanConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Timestamp(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"rfc3339", "2024-01-15T10:30:00Z", false},
		{"rfc3339_offset", "2024-01-15T10:30:00+05:00", false},
		{"rfc3339_nano", "2024-01-15T10:30:00.123456789Z", false},
		{"invalid_format", "2024/01/15 10:30:00", true},
		{"wrong_type", 12345, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewTimestampConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_TimestampCustomFormat(t *testing.T) {
	constraint := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"custom_format", "2024-01-15 10:30:00", false},
		{"rfc3339_wrong", "2024-01-15T10:30:00Z", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Date(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_date", "2024-01-15", false},
		{"invalid_format", "01/15/2024", true},
		{"wrong_type", 20240115, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewDateConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_UUID(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"invalid_uuid", "not-a-uuid", true},
		{"wrong_type", 12345, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewUUIDConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Enum(t *testing.T) {
	constraint := schema.NewEnumConstraint([]string{"red", "green", "blue"})

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_red", "red", false},
		{"valid_green", "green", false},
		{"invalid_yellow", "yellow", true},
		{"case_sensitive", "RED", true},
		{"wrong_type", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Pattern(t *testing.T) {
	pattern := regexp.MustCompile(`^[A-Z]{2}-\d{4}$`)
	constraint := schema.NewPatternConstraint([]*regexp.Regexp{pattern})

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_pattern", "AB-1234", false},
		{"invalid_pattern", "ab-1234", true},
		{"wrong_format", "ABC-12345", true},
		{"wrong_type", 12345, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Vector(t *testing.T) {
	constraint := schema.NewVectorConstraint(3)

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_vector", []any{1.0, 2.0, 3.0}, false},
		{"valid_int_vector", []any{int64(1), int64(2), int64(3)}, false},
		{"wrong_dimensions", []any{1.0, 2.0}, true},
		{"wrong_element_type", []any{"a", "b", "c"}, true},
		{"wrong_type", "not a vector", true},
		{"typed_float_slice", []float64{1.0, 2.0, 3.0}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Alias(t *testing.T) {
	// Alias that resolves to integer
	constraint := schema.NewAliasConstraint("PositiveInt", schema.NewIntegerConstraintBounded(1, true, 0, false))

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid", int64(10), false},
		{"invalid_zero", int64(0), true},
		{"wrong_type", "10", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_UnresolvedAlias(t *testing.T) {
	// Alias without resolved constraint
	constraint := schema.NewAliasConstraint("UnresolvedType", nil)

	err := eval.CheckValue(int64(10), constraint)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unresolved")
}

func TestCheckerFor(t *testing.T) {
	constraint := schema.NewIntegerConstraintBounded(0, true, 100, true)
	checker := eval.CheckerFor(constraint)

	ok, msg := checker(int64(50))
	assert.True(t, ok)
	assert.Empty(t, msg)

	ok, msg = checker(int64(150))
	assert.False(t, ok)
	assert.NotEmpty(t, msg)
}

// --- NaN/Inf Rejection Tests ---

func TestCheckValue_Float_NaNInf(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
		errMsg     string
	}{
		{"reject_nan", math.NaN(), schema.NewFloatConstraint(), true, "not finite"},
		{"reject_pos_inf", math.Inf(1), schema.NewFloatConstraint(), true, "not finite"},
		{"reject_neg_inf", math.Inf(-1), schema.NewFloatConstraint(), true, "not finite"},
		{"accept_zero", 0.0, schema.NewFloatConstraint(), false, ""},
		{"accept_negative", -1.5, schema.NewFloatConstraint(), false, ""},
		{"accept_large", 1e308, schema.NewFloatConstraint(), false, ""},
		{"accept_small", 1e-308, schema.NewFloatConstraint(), false, ""},

		// NaN/Inf with bounds - should fail on NaN/Inf check before bounds
		{"nan_with_bounds", math.NaN(), schema.NewFloatConstraintBounded(0, true, 100, true), true, "not finite"},
		{"inf_with_bounds", math.Inf(1), schema.NewFloatConstraintBounded(0, true, 100, true), true, "not finite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_Vector_NaNInf(t *testing.T) {
	constraint := schema.NewVectorConstraint(3)

	tests := []struct {
		name    string
		val     any
		wantErr bool
		errMsg  string
	}{
		{"reject_nan_element", []any{1.0, math.NaN(), 3.0}, true, "element [1]"},
		{"reject_inf_element", []any{1.0, math.Inf(1), 3.0}, true, "element [1]"},
		{"reject_neg_inf_element", []any{math.Inf(-1), 2.0, 3.0}, true, "element [0]"},
		{"reject_all_nan", []any{math.NaN(), math.NaN(), math.NaN()}, true, "element [0]"},
		{"accept_valid_vector", []any{1.0, 2.0, 3.0}, false, ""},
		{"accept_mixed_int_float", []any{int64(1), 2.5, int64(3)}, false, ""},
		{"accept_typed_slice", []float64{1.0, 2.0, 3.0}, false, ""},

		// Edge cases with typed slices
		{"reject_nan_float64_slice", []float64{1.0, math.NaN(), 3.0}, true, "element [1]"},
		{"reject_inf_float64_slice", []float64{math.Inf(1), 2.0, 3.0}, true, "element [0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCoerceValue_Float_NaNInf(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"reject_nan", math.NaN(), true},
		{"reject_inf", math.Inf(1), true},
		{"reject_neg_inf", math.Inf(-1), true},
		{"accept_zero", 0.0, false},
		{"accept_int", int64(42), false},
		{"accept_negative", -3.14, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := eval.CoerceValue(tt.val, schema.NewFloatConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCoerceValue_Vector_NaNInf(t *testing.T) {
	constraint := schema.NewVectorConstraint(3)

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"reject_nan_element", []any{1.0, math.NaN(), 3.0}, true},
		{"reject_inf_element", []any{1.0, math.Inf(1), 3.0}, true},
		{"accept_valid", []any{1.0, 2.0, 3.0}, false},
		{"accept_int_elements", []any{int64(1), int64(2), int64(3)}, false},
		{"reject_typed_nan", []float64{1.0, math.NaN(), 3.0}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := eval.CoerceValue(tt.val, constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Checker with Registry Tests ---
//
// NOTE: The Registry enables custom type KIND DETECTION via ClassifyWithRegistry,
// but named scalar types (e.g., `type MyInt int64`) remain unsupported for value
// extraction (GetInt64/GetFloat64 don't convert them). This is by design.
//
// These tests verify the registry wiring is correct, not that named scalars work.

func TestChecker_DefaultChecker(t *testing.T) {
	// DefaultChecker should use built-in type detection
	checker := eval.DefaultChecker()

	// Built-in types should work
	err := checker.CheckValue(int64(42), schema.NewIntegerConstraint())
	require.NoError(t, err)

	err = checker.CheckValue(3.14, schema.NewFloatConstraint())
	require.NoError(t, err)

	err = checker.CheckValue("hello", schema.NewStringConstraint())
	require.NoError(t, err)
}

func TestChecker_NewChecker_WithRegistry(t *testing.T) {
	// Verify NewChecker accepts a Registry and the Checker is usable
	var hookCalled bool
	reg := value.Registry{
		BaseKindOfReflectType: func(rt reflect.Type) value.Kind {
			hookCalled = true
			return value.UnspecifiedKind // Hook doesn't recognize the type
		},
	}

	checker := eval.NewChecker(reg)

	// Built-in types should still work (hook returns UnspecifiedKind, falls back to built-in)
	err := checker.CheckValue(int64(42), schema.NewIntegerConstraint())
	require.NoError(t, err)

	// Hook is called for non-built-in types during Integer/Float/Vector classification.
	// Using a struct type with integer constraint to trigger the classify path.
	// Note: Only Integer, Float, and Vector constraints call ClassifyWithRegistry;
	// String just does a type assertion.
	type customType struct{}
	_ = checker.CheckValue(customType{}, schema.NewIntegerConstraint())
	// We don't care about the error; we just want to verify the hook was called
	assert.True(t, hookCalled, "registry hook should be called for unrecognized types in Integer check")
}

func TestChecker_CheckValue_Backward_Compatibility(t *testing.T) {
	// Package-level CheckValue should continue to work (uses DefaultChecker internally)
	err := eval.CheckValue(int64(42), schema.NewIntegerConstraint())
	require.NoError(t, err)

	err = eval.CheckValue(3.14, schema.NewFloatConstraint())
	require.NoError(t, err)

	err = eval.CheckValue("hello", schema.NewStringConstraint())
	require.NoError(t, err)

	// All integer variants should work
	err = eval.CheckValue(int8(42), schema.NewIntegerConstraint())
	require.NoError(t, err)

	err = eval.CheckValue(uint64(42), schema.NewIntegerConstraint())
	require.NoError(t, err)
}

func TestChecker_CoerceValue_Backward_Compatibility(t *testing.T) {
	// Package-level CoerceValue should continue to work
	result, err := eval.CoerceValue(42, schema.NewIntegerConstraint())
	require.NoError(t, err)
	assert.Equal(t, int64(42), result)

	result, err = eval.CoerceValue(float32(3.14), schema.NewFloatConstraint())
	require.NoError(t, err)
	assert.IsType(t, float64(0), result)
}

func TestChecker_Method_vs_PackageLevel(t *testing.T) {
	// Verify method and package-level function produce same results
	checker := eval.DefaultChecker()

	testCases := []struct {
		val        any
		constraint schema.Constraint
	}{
		{int64(42), schema.NewIntegerConstraint()},
		{3.14, schema.NewFloatConstraint()},
		{"test", schema.NewStringConstraint()},
		{true, schema.NewBooleanConstraint()},
	}

	for _, tc := range testCases {
		methodErr := checker.CheckValue(tc.val, tc.constraint)
		pkgErr := eval.CheckValue(tc.val, tc.constraint)

		if methodErr == nil {
			require.NoError(t, pkgErr, "package-level should match method result")
		} else {
			require.Error(t, pkgErr, "package-level should match method error")
		}
	}
}

// =============================================================================
// Additional Coverage Tests for Uncovered Paths
// =============================================================================

func TestCoerceValue_AliasConstraint(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})

	// Test alias that resolves to integer
	intAlias := schema.NewAliasConstraint("MyInt", schema.NewIntegerConstraint())

	t.Run("alias_to_integer_coerce_int32", func(t *testing.T) {
		result, err := checker.CoerceValue(int32(42), intAlias)
		require.NoError(t, err)
		assert.Equal(t, int64(42), result)
	})

	t.Run("alias_to_integer_coerce_uint", func(t *testing.T) {
		result, err := checker.CoerceValue(uint(100), intAlias)
		require.NoError(t, err)
		assert.Equal(t, int64(100), result)
	})

	// Test alias that resolves to float
	floatAlias := schema.NewAliasConstraint("MyFloat", schema.NewFloatConstraint())

	t.Run("alias_to_float_coerce_int", func(t *testing.T) {
		result, err := checker.CoerceValue(int64(42), floatAlias)
		require.NoError(t, err)
		assert.Equal(t, float64(42), result)
	})

	t.Run("alias_to_float_coerce_float32", func(t *testing.T) {
		result, err := checker.CoerceValue(float32(3.14), floatAlias)
		require.NoError(t, err)
		// float32 to float64 conversion
		assert.InDelta(t, 3.14, result.(float64), 0.001)
	})

	// Test alias that resolves to string (no coercion needed)
	stringAlias := schema.NewAliasConstraint("MyString", schema.NewStringConstraint())

	t.Run("alias_to_string_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue("hello", stringAlias)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	// Test nil value
	t.Run("nil_value_returns_nil", func(t *testing.T) {
		result, err := checker.CoerceValue(nil, intAlias)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestCoerceValue_VectorConstraint(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	vectorConstraint := schema.NewVectorConstraint(3)

	t.Run("typed_float64_slice", func(t *testing.T) {
		input := []float64{1.0, 2.0, 3.0}
		result, err := checker.CoerceValue(input, vectorConstraint)
		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("any_slice_of_floats", func(t *testing.T) {
		input := []any{1.0, 2.0, 3.0}
		result, err := checker.CoerceValue(input, vectorConstraint)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0, 2.0, 3.0}, result)
	})

	t.Run("any_slice_of_ints", func(t *testing.T) {
		input := []any{int64(1), int64(2), int64(3)}
		result, err := checker.CoerceValue(input, vectorConstraint)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0, 2.0, 3.0}, result)
	})

	t.Run("coerce_error_non_numeric", func(t *testing.T) {
		input := []any{"a", "b", "c"}
		_, err := checker.CoerceValue(input, vectorConstraint)
		assert.Error(t, err)
	})
}

func TestCoerceValue_FloatEdgeCases(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	floatConstraint := schema.NewFloatConstraint()

	t.Run("float32_to_float64", func(t *testing.T) {
		result, err := checker.CoerceValue(float32(1.5), floatConstraint)
		require.NoError(t, err)
		assert.InDelta(t, 1.5, result.(float64), 0.0001)
	})

	t.Run("uint64_to_float64", func(t *testing.T) {
		result, err := checker.CoerceValue(uint64(100), floatConstraint)
		require.NoError(t, err)
		assert.Equal(t, float64(100), result)
	})
}

func TestCoerceValue_IntegerEdgeCases(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	intConstraint := schema.NewIntegerConstraint()

	t.Run("uint8_to_int64", func(t *testing.T) {
		result, err := checker.CoerceValue(uint8(255), intConstraint)
		require.NoError(t, err)
		assert.Equal(t, int64(255), result)
	})

	t.Run("int16_to_int64", func(t *testing.T) {
		result, err := checker.CoerceValue(int16(-32768), intConstraint)
		require.NoError(t, err)
		assert.Equal(t, int64(-32768), result)
	})
}

func TestCheckValue_BooleanConstraint(t *testing.T) {
	boolConstraint := schema.NewBooleanConstraint()

	t.Run("true_value", func(t *testing.T) {
		err := eval.CheckValue(true, boolConstraint)
		require.NoError(t, err)
	})

	t.Run("false_value", func(t *testing.T) {
		err := eval.CheckValue(false, boolConstraint)
		require.NoError(t, err)
	})

	t.Run("string_true_fails", func(t *testing.T) {
		err := eval.CheckValue("true", boolConstraint)
		assert.Error(t, err)
	})

	t.Run("int_one_fails", func(t *testing.T) {
		err := eval.CheckValue(1, boolConstraint)
		assert.Error(t, err)
	})
}

// =============================================================================
// Float64 Whole Number Integer Coercion Tests (Issue 1 fix)
// =============================================================================

func TestIsInteger_Float64WholeNumber(t *testing.T) {
	checker := eval.IsInteger()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		// Float64 whole numbers should be accepted
		{"float64_zero", float64(0.0), true},
		{"float64_positive", float64(42.0), true},
		{"float64_negative", float64(-42.0), true},
		{"float64_large", float64(1000000.0), true},

		// Float64 with fractions should be rejected
		{"float64_fraction_half", float64(0.5), false},
		{"float64_fraction_pi", float64(3.14), false},
		{"float64_fraction_negative", float64(-2.5), false},

		// Non-finite floats should be rejected
		{"float64_nan", math.NaN(), false},
		{"float64_inf", math.Inf(1), false},
		{"float64_neg_inf", math.Inf(-1), false},

		// Standard integer types still work
		{"int", 42, true},
		{"int64", int64(42), true},
		{"uint64", uint64(42), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestCheckValue_Integer_Float64WholeNumber(t *testing.T) {
	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		// Float64 whole numbers should be accepted
		{"float64_whole_zero", float64(0.0), schema.NewIntegerConstraint(), false},
		{"float64_whole_positive", float64(42.0), schema.NewIntegerConstraint(), false},
		{"float64_whole_negative", float64(-42.0), schema.NewIntegerConstraint(), false},
		{"float64_whole_large", float64(1000000.0), schema.NewIntegerConstraint(), false},

		// Float64 with fractions should be rejected
		{"float64_fraction", float64(3.14), schema.NewIntegerConstraint(), true},
		{"float64_fraction_half", float64(0.5), schema.NewIntegerConstraint(), true},

		// Float64 whole numbers should respect bounds
		{"float64_min_ok", float64(10.0), schema.NewIntegerConstraintBounded(10, true, 100, true), false},
		{"float64_min_fail", float64(9.0), schema.NewIntegerConstraintBounded(10, true, 100, true), true},
		{"float64_max_ok", float64(100.0), schema.NewIntegerConstraintBounded(10, true, 100, true), false},
		{"float64_max_fail", float64(101.0), schema.NewIntegerConstraintBounded(10, true, 100, true), true},

		// Non-finite should be rejected
		{"float64_nan", math.NaN(), schema.NewIntegerConstraint(), true},
		{"float64_inf", math.Inf(1), schema.NewIntegerConstraint(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCoerceValue_Integer_Float64WholeNumber(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	intConstraint := schema.NewIntegerConstraint()

	tests := []struct {
		name    string
		val     any
		wantVal int64
		wantErr bool
	}{
		// Float64 whole numbers should coerce to int64
		{"float64_zero", float64(0.0), 0, false},
		{"float64_positive", float64(42.0), 42, false},
		{"float64_negative", float64(-42.0), -42, false},
		{"float64_large", float64(1000000.0), 1000000, false},

		// Float64 with fractions should fail
		{"float64_fraction", float64(3.14), 0, true},
		{"float64_half", float64(0.5), 0, true},

		// Non-finite should fail
		{"float64_nan", math.NaN(), 0, true},
		{"float64_inf", math.Inf(1), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.CoerceValue(tt.val, intConstraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVal, result)
			}
		})
	}
}

// =============================================================================
// Native time.Time and uuid.UUID Acceptance Tests (Issue 3 fix)
// =============================================================================

func TestIsTimestamp_TimeTime(t *testing.T) {
	checker := eval.IsTimestamp()

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		// time.Time should be accepted
		{"time_now", time.Now(), true},
		{"time_utc", time.Now().UTC(), true},
		{"time_zero", time.Time{}, true},
		{"time_fixed", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), true},

		// Valid timestamp strings still work
		{"string_rfc3339", "2024-01-15T10:30:00Z", true},
		{"string_rfc3339_offset", "2024-01-15T10:30:00+05:00", true},

		// Invalid types should be rejected
		{"int", 12345, false},
		{"float", 3.14, false},
		{"invalid_string", "not a timestamp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestCheckValue_Timestamp_TimeTime(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		// time.Time should be accepted
		{"time_now", time.Now(), false},
		{"time_utc", time.Now().UTC(), false},
		{"time_zero", time.Time{}, false},
		{"time_fixed", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"time_with_location", time.Date(2024, 1, 15, 10, 30, 0, 0, time.FixedZone("EST", -5*60*60)), false},

		// Valid timestamp strings still work
		{"string_rfc3339", "2024-01-15T10:30:00Z", false},
		{"string_rfc3339_nano", "2024-01-15T10:30:00.123456789Z", false},

		// Invalid types should be rejected
		{"int", 12345, true},
		{"float", 3.14, true},
		{"invalid_string", "not a timestamp", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewTimestampConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsUUID_UUIDType(t *testing.T) {
	checker := eval.IsUUID()

	// Generate some UUIDs for testing
	validUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	nilUUID := uuid.Nil

	tests := []struct {
		name     string
		val      any
		expected bool
	}{
		// uuid.UUID should be accepted
		{"uuid_valid", validUUID, true},
		{"uuid_nil", nilUUID, true},
		{"uuid_new", uuid.New(), true},

		// Valid UUID strings still work
		{"string_uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"string_uuid_uppercase", "550E8400-E29B-41D4-A716-446655440000", true},

		// Invalid types should be rejected
		{"int", 12345, false},
		{"float", 3.14, false},
		{"invalid_string", "not-a-uuid", false},
		{"short_string", "550e8400-e29b-41d4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, msg := checker(tt.val)
			assert.Equal(t, tt.expected, ok)
			if !tt.expected {
				assert.NotEmpty(t, msg)
			}
		})
	}
}

func TestCheckValue_UUID_UUIDType(t *testing.T) {
	// Generate some UUIDs for testing
	validUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	nilUUID := uuid.Nil

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		// uuid.UUID should be accepted
		{"uuid_valid", validUUID, false},
		{"uuid_nil", nilUUID, false},
		{"uuid_new", uuid.New(), false},

		// Valid UUID strings still work
		{"string_uuid", "550e8400-e29b-41d4-a716-446655440000", false},

		// Invalid types should be rejected
		{"int", 12345, true},
		{"float", 3.14, true},
		{"invalid_string", "not-a-uuid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, schema.NewUUIDConstraint())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCoerceValue_Timestamp_TimeTime(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	timestampConstraint := schema.NewTimestampConstraint()

	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	t.Run("time_time_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue(fixedTime, timestampConstraint)
		require.NoError(t, err)
		// time.Time should pass through unchanged
		assert.Equal(t, fixedTime, result)
	})

	t.Run("string_timestamp_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue("2024-01-15T10:30:00Z", timestampConstraint)
		require.NoError(t, err)
		assert.Equal(t, "2024-01-15T10:30:00Z", result)
	})

	t.Run("nil_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue(nil, timestampConstraint)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestCoerceValue_UUID_UUIDType(t *testing.T) {
	checker := eval.NewChecker(value.Registry{})
	uuidConstraint := schema.NewUUIDConstraint()

	validUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	t.Run("uuid_type_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue(validUUID, uuidConstraint)
		require.NoError(t, err)
		// uuid.UUID should pass through unchanged
		assert.Equal(t, validUUID, result)
	})

	t.Run("string_uuid_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue("550e8400-e29b-41d4-a716-446655440000", uuidConstraint)
		require.NoError(t, err)
		assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", result)
	})

	t.Run("nil_passthrough", func(t *testing.T) {
		result, err := checker.CoerceValue(nil, uuidConstraint)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

// Custom types for testing registry-aware coercion
type (
	myCustomInt   int64
	myCustomFloat float64
)

func TestChecker_CoerceValue_RegistryAwareInteger(t *testing.T) {
	// Registry that recognizes myCustomInt as IntKind
	reg := value.Registry{
		BaseKindOfReflectType: func(rt reflect.Type) value.Kind {
			if rt.Name() == "myCustomInt" {
				return value.IntKind
			}
			return value.UnspecifiedKind
		},
	}

	checker := eval.NewChecker(reg)
	intConstraint := schema.NewIntegerConstraint()

	// Note: CheckValue still fails because value.GetInt64 doesn't handle custom types.
	// The registry-aware coercion fix in Issue 9 only affects CoerceValue.
	t.Run("custom_int_check_fails_without_getint64_support", func(t *testing.T) {
		// This documents the current limitation - CheckValue doesn't use reflection
		// fallback for value extraction, only for classification
		err := checker.CheckValue(myCustomInt(42), intConstraint)
		assert.Error(t, err)
	})

	// CoerceValue now uses registry-aware reflection fallback
	t.Run("custom_int_coerce_succeeds", func(t *testing.T) {
		result, err := checker.CoerceValue(myCustomInt(42), intConstraint)
		require.NoError(t, err)
		assert.Equal(t, int64(42), result)
	})

	t.Run("custom_int_negative", func(t *testing.T) {
		result, err := checker.CoerceValue(myCustomInt(-100), intConstraint)
		require.NoError(t, err)
		assert.Equal(t, int64(-100), result)
	})
}

func TestChecker_CoerceValue_RegistryAwareFloat(t *testing.T) {
	// Registry that recognizes myCustomFloat as FloatKind
	reg := value.Registry{
		BaseKindOfReflectType: func(rt reflect.Type) value.Kind {
			if rt.Name() == "myCustomFloat" {
				return value.FloatKind
			}
			return value.UnspecifiedKind
		},
	}

	checker := eval.NewChecker(reg)
	floatConstraint := schema.NewFloatConstraint()

	// Note: CheckValue still fails because value.GetFloat64 doesn't handle custom types.
	t.Run("custom_float_check_fails_without_getfloat64_support", func(t *testing.T) {
		err := checker.CheckValue(myCustomFloat(3.14), floatConstraint)
		assert.Error(t, err)
	})

	// CoerceValue now uses registry-aware reflection fallback
	t.Run("custom_float_coerce_succeeds", func(t *testing.T) {
		result, err := checker.CoerceValue(myCustomFloat(3.14), floatConstraint)
		require.NoError(t, err)
		assert.InDelta(t, 3.14, result.(float64), 0.0001)
	})
}

func TestChecker_CoerceValue_VectorWithCustomTypes(t *testing.T) {
	// Registry that recognizes myCustomFloat as FloatKind
	reg := value.Registry{
		BaseKindOfReflectType: func(rt reflect.Type) value.Kind {
			if rt.Name() == "myCustomFloat" {
				return value.FloatKind
			}
			return value.UnspecifiedKind
		},
	}

	checker := eval.NewChecker(reg)
	vectorConstraint := schema.NewVectorConstraint(3)

	// CoerceValue now uses registry-aware reflection fallback for each element
	t.Run("vector_with_custom_float_elements", func(t *testing.T) {
		input := []any{myCustomFloat(1.0), myCustomFloat(2.0), myCustomFloat(3.0)}
		result, err := checker.CoerceValue(input, vectorConstraint)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0, 2.0, 3.0}, result)
	})
}

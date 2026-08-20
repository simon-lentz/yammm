package eval_test

import (
	"math"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/instance/internal/eval"
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
		{"uint64", uint64(42), true},
		{"string", "42", false},
		{"bool", true, false},

		// Float64 whole numbers are integers; fractions and non-finite are not.
		{"float64_zero", float64(0.0), true},
		{"float64_positive", float64(42.0), true},
		{"float64_negative", float64(-42.0), true},
		{"float64_large", float64(1000000.0), true},
		{"float64_fraction_half", float64(0.5), false},
		{"float64_fraction_pi", float64(3.14), false},
		{"float64_fraction_negative", float64(-2.5), false},
		{"float64_nan", math.NaN(), false},
		{"float64_inf", math.Inf(1), false},
		{"float64_neg_inf", math.Inf(-1), false},
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
		{"uuid_uppercase", "550E8400-E29B-41D4-A716-446655440000", true},
		{"invalid_uuid", "not-a-uuid", false},
		{"short_uuid", "550e8400-e29b-41d4", false},
		{"int", 42, false},

		// uuid.UUID values are accepted without string parsing.
		{"uuid_type_valid", uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), true},
		{"uuid_type_nil", uuid.Nil, true},
		{"uuid_type_new", uuid.New(), true},
		{"float", 3.14, false},
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

		// time.Time values are timestamps without string parsing.
		{"time_now", time.Now(), true},
		{"time_utc", time.Now().UTC(), true},
		{"time_zero", time.Time{}, true},
		{"time_fixed", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), true},
		{"float", 3.14, false},
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
		{"min_length_ok", "abc", schema.StringLenBetween(3, 10), false},
		{"min_length_fail", "ab", schema.StringLenBetween(3, 10), true},
		{"max_length_ok", "abcdefghij", schema.StringLenBetween(1, 10), false},
		{"max_length_fail", "abcdefghijk", schema.StringLenBetween(1, 10), true},
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

// TestCheckCoerce_Integer drives CheckValue and CoerceValue through one
// table. CheckValue enforces kind AND bounds; CoerceValue converts kind only
// (it documents itself as running after a successful CheckValue), so a
// bounds-violating whole number still coerces — wantCoerced pins that split.
// A nil wantCoerced with wantCoerceErr=false skips the coercion assertions.
func TestCheckCoerce_Integer(t *testing.T) {
	tests := []struct {
		name          string
		val           any
		constraint    schema.Constraint
		wantErr       bool
		wantCoerced   any
		wantCoerceErr bool
	}{
		{"valid_int", int64(42), schema.NewIntegerConstraint(), false, int64(42), false},
		{"valid_int32", int32(42), schema.NewIntegerConstraint(), false, int64(42), false},
		{"valid_uint", uint(42), schema.NewIntegerConstraint(), false, int64(42), false},
		{"uint8", uint8(255), schema.NewIntegerConstraint(), false, int64(255), false},
		{"int16", int16(-32768), schema.NewIntegerConstraint(), false, int64(-32768), false},
		{"wrong_type_string", "42", schema.NewIntegerConstraint(), true, nil, true},
		{"wrong_type_float", 3.14, schema.NewIntegerConstraint(), true, nil, true},
		{"min_ok", int64(10), schema.IntegerBetween(10, 100), false, int64(10), false},
		{"min_fail", int64(9), schema.IntegerBetween(10, 100), true, int64(9), false},
		{"max_ok", int64(100), schema.IntegerBetween(10, 100), false, int64(100), false},
		{"max_fail", int64(101), schema.IntegerBetween(10, 100), true, int64(101), false},
		{"no_min", int64(-1000), schema.IntegerMax(100), false, int64(-1000), false},
		{"no_max", int64(1000), schema.IntegerMin(0), false, int64(1000), false},

		// Float64 whole numbers are integers; fractions and non-finite fail
		// both checking and coercion.
		{"float64_whole_zero", float64(0.0), schema.NewIntegerConstraint(), false, int64(0), false},
		{"float64_whole_positive", float64(42.0), schema.NewIntegerConstraint(), false, int64(42), false},
		{"float64_whole_negative", float64(-42.0), schema.NewIntegerConstraint(), false, int64(-42), false},
		{"float64_whole_large", float64(1000000.0), schema.NewIntegerConstraint(), false, int64(1000000), false},
		{"float64_fraction", float64(3.14), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_fraction_half", float64(0.5), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_min_ok", float64(10.0), schema.IntegerBetween(10, 100), false, int64(10), false},
		{"float64_min_fail", float64(9.0), schema.IntegerBetween(10, 100), true, int64(9), false},
		{"float64_max_ok", float64(100.0), schema.IntegerBetween(10, 100), false, int64(100), false},
		{"float64_max_fail", float64(101.0), schema.IntegerBetween(10, 100), true, int64(101), false},
		{"float64_nan", math.NaN(), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_inf", math.Inf(1), schema.NewIntegerConstraint(), true, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			coerced, coerceErr := eval.CoerceValue(tt.val, tt.constraint)
			if tt.wantCoerceErr {
				assert.Error(t, coerceErr)
				return
			}
			require.NoError(t, coerceErr)
			if tt.wantCoerced != nil {
				assert.Equal(t, tt.wantCoerced, coerced)
			}
		})
	}
}

// TestCheckCoerce_Float drives CheckValue and CoerceValue through one table;
// non-finite values fail both, and coercion converts every accepted numeric
// representation to float64 exactly.
func TestCheckCoerce_Float(t *testing.T) {
	tests := []struct {
		name          string
		val           any
		constraint    schema.Constraint
		wantErr       bool
		errMsg        string
		wantCoerced   any
		wantCoerceErr bool
	}{
		{"valid_float", 3.14, schema.NewFloatConstraint(), false, "", float64(3.14), false},
		{"valid_int_as_float", int64(42), schema.NewFloatConstraint(), false, "", float64(42), false},
		{"float32_to_float64", float32(1.5), schema.NewFloatConstraint(), false, "", float64(1.5), false},
		{"float32_imprecise", float32(3.14), schema.NewFloatConstraint(), false, "", float64(float32(3.14)), false},
		{"uint64_to_float64", uint64(100), schema.NewFloatConstraint(), false, "", float64(100), false},
		{"wrong_type_string", "3.14", schema.NewFloatConstraint(), true, "", nil, true},
		{"wrong_type_bool", true, schema.NewFloatConstraint(), true, "", nil, true},
		{"min_ok", 0.0, schema.FloatBetween(0.0, 1.0), false, "", float64(0), false},
		{"min_fail", -0.1, schema.FloatBetween(0.0, 1.0), true, "", float64(-0.1), false},
		{"max_ok", 1.0, schema.FloatBetween(0.0, 1.0), false, "", float64(1), false},
		{"max_fail", 1.1, schema.FloatBetween(0.0, 1.0), true, "", float64(1.1), false},

		// Non-finite floats are rejected before bounds by both paths.
		{"reject_nan", math.NaN(), schema.NewFloatConstraint(), true, "not finite", nil, true},
		{"reject_pos_inf", math.Inf(1), schema.NewFloatConstraint(), true, "not finite", nil, true},
		{"reject_neg_inf", math.Inf(-1), schema.NewFloatConstraint(), true, "not finite", nil, true},
		{"nan_with_bounds", math.NaN(), schema.FloatBetween(0, 100), true, "not finite", nil, true},
		{"inf_with_bounds", math.Inf(1), schema.FloatBetween(0, 100), true, "not finite", nil, true},
		{"accept_large", 1e308, schema.NewFloatConstraint(), false, "", float64(1e308), false},
		{"accept_small", 1e-308, schema.NewFloatConstraint(), false, "", float64(1e-308), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}

			coerced, coerceErr := eval.CoerceValue(tt.val, tt.constraint)
			if tt.wantCoerceErr {
				assert.Error(t, coerceErr)
				return
			}
			require.NoError(t, coerceErr)
			if tt.wantCoerced != nil {
				assert.Equal(t, tt.wantCoerced, coerced)
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

// TestCheckCoerce_Timestamp covers string and time.Time forms plus the
// custom-format constraint. Timestamp is a canonical kind, so CoerceValue
// passes every value through unchanged — even ones CheckValue rejects.
func TestCheckCoerce_Timestamp(t *testing.T) {
	defaultC := schema.NewTimestampConstraint()
	customC := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")

	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		{"rfc3339", "2024-01-15T10:30:00Z", defaultC, false},
		{"rfc3339_offset", "2024-01-15T10:30:00+05:00", defaultC, false},
		{"rfc3339_nano", "2024-01-15T10:30:00.123456789Z", defaultC, false},
		{"invalid_format", "2024/01/15 10:30:00", defaultC, true},
		{"wrong_type", 12345, defaultC, true},
		{"float", 3.14, defaultC, true},

		// time.Time values are accepted directly.
		{"time_now", time.Now(), defaultC, false},
		{"time_utc", time.Now().UTC(), defaultC, false},
		{"time_zero", time.Time{}, defaultC, false},
		{"time_fixed", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), defaultC, false},
		{"time_with_location", time.Date(2024, 1, 15, 10, 30, 0, 0, time.FixedZone("EST", -5*60*60)), defaultC, false},

		// Custom format replaces RFC3339, not extends it.
		{"custom_format", "2024-01-15 10:30:00", customC, false},
		{"custom_format_rejects_rfc3339", "2024-01-15T10:30:00Z", customC, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Coercion renders the value through the constraint's own layout,
			// so the stored text satisfies the format the schema declared and
			// names the same instant the caller submitted.
			coerced, coerceErr := eval.CoerceValue(tt.val, tt.constraint)
			require.NoError(t, coerceErr)
			text, isString := coerced.(string)
			require.True(t, isString, "Timestamp coerces to a string, got %T", coerced)

			layout := time.RFC3339Nano
			if tc, isTS := tt.constraint.(schema.TimestampConstraint); isTS && tc.Format() != "" {
				layout = tc.Format()
			}
			back, parseErr := time.Parse(layout, text)
			require.NoError(t, parseErr, "coerced text must satisfy the constraint's layout")
			if orig, isTime := tt.val.(time.Time); isTime {
				assert.True(t, back.Equal(orig),
					"canonical text names a different instant: %s vs %s", back, orig)
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

// TestCheckCoerce_UUID covers string and uuid.UUID forms. UUID is a
// canonical kind, so CoerceValue passes every value through unchanged.
func TestCheckCoerce_UUID(t *testing.T) {
	constraint := schema.NewUUIDConstraint()

	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"valid_uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"uuid_uppercase", "550E8400-E29B-41D4-A716-446655440000", false},
		{"invalid_uuid", "not-a-uuid", true},
		{"wrong_type", 12345, true},
		{"float", 3.14, true},

		// uuid.UUID values are accepted directly.
		{"uuid_type_valid", uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), false},
		{"uuid_type_nil", uuid.Nil, false},
		{"uuid_type_new", uuid.New(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Every accepted spelling coerces to the one canonical lowercase
			// form, which is what stops two spellings of one UUID producing
			// two primary keys.
			coerced, coerceErr := eval.CoerceValue(tt.val, constraint)
			require.NoError(t, coerceErr)
			assert.Equal(t, canonicalUUIDText(t, tt.val), coerced)
		})
	}
}

// canonicalUUIDText renders val the way uuid.UUID itself does, independently of
// the coercer under test.
func canonicalUUIDText(t *testing.T, val any) string {
	t.Helper()
	var u uuid.UUID
	switch v := val.(type) {
	case uuid.UUID:
		u = v
	case string:
		var err error
		u, err = uuid.Parse(v)
		require.NoError(t, err)
	default:
		t.Fatalf("not a UUID input: %T", val)
	}
	text, err := u.MarshalText()
	require.NoError(t, err)
	return string(text)
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

// TestCheckCoerce_Vector drives CheckValue and CoerceValue through one
// table: dimension and element-kind violations (with their element-index
// error anchors), non-finite element rejection, and coercion of every
// accepted representation to []float64.
func TestCheckCoerce_Vector(t *testing.T) {
	constraint := schema.NewVectorConstraint(3)

	tests := []struct {
		name          string
		val           any
		wantErr       bool
		errMsg        string
		wantCoerced   any
		wantCoerceErr bool
	}{
		{"valid_vector", []any{1.0, 2.0, 3.0}, false, "", []float64{1, 2, 3}, false},
		{"valid_int_vector", []any{int64(1), int64(2), int64(3)}, false, "", []float64{1, 2, 3}, false},
		{"mixed_int_float", []any{int64(1), 2.5, int64(3)}, false, "", []float64{1, 2.5, 3}, false},
		{"typed_float_slice", []float64{1.0, 2.0, 3.0}, false, "", []float64{1, 2, 3}, false},
		{"wrong_dimensions", []any{1.0, 2.0}, true, "", nil, false},
		{"wrong_element_type", []any{"a", "b", "c"}, true, "", nil, true},
		{"wrong_type", "not a vector", true, "", nil, true},

		// Non-finite elements are rejected with their index in the message.
		{"reject_nan_element", []any{1.0, math.NaN(), 3.0}, true, "element [1]", nil, true},
		{"reject_inf_element", []any{1.0, math.Inf(1), 3.0}, true, "element [1]", nil, true},
		{"reject_neg_inf_element", []any{math.Inf(-1), 2.0, 3.0}, true, "element [0]", nil, true},
		{"reject_all_nan", []any{math.NaN(), math.NaN(), math.NaN()}, true, "element [0]", nil, true},
		{"reject_nan_float64_slice", []float64{1.0, math.NaN(), 3.0}, true, "element [1]", nil, true},
		{"reject_inf_float64_slice", []float64{math.Inf(1), 2.0, 3.0}, true, "element [0]", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eval.CheckValue(tt.val, constraint)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}

			coerced, coerceErr := eval.CoerceValue(tt.val, constraint)
			if tt.wantCoerceErr {
				assert.Error(t, coerceErr)
				return
			}
			require.NoError(t, coerceErr)
			if tt.wantCoerced != nil {
				assert.Equal(t, tt.wantCoerced, coerced)
			}
		})
	}
}

func TestCheckValue_Alias(t *testing.T) {
	// Alias that resolves to integer
	constraint := schema.NewAliasConstraint("PositiveInt", schema.IntegerMin(1))

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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unresolved")
}

func TestCheckerFor(t *testing.T) {
	constraint := schema.IntegerBetween(0, 100)
	checker := eval.CheckerFor(constraint)

	ok, msg := checker(int64(50))
	assert.True(t, ok)
	assert.Empty(t, msg)

	ok, msg = checker(int64(150))
	assert.False(t, ok)
	assert.NotEmpty(t, msg)
}

// NOTE: The Registry enables custom type KIND DETECTION via ClassifyWithRegistry,
// but named scalar types (e.g., `type MyInt int64`) remain unsupported for value
// extraction (GetInt64/GetFloat64 don't convert them). This is by design.
//
// These tests verify the registry wiring is correct, not that named scalars work.

func TestChecker_NewChecker_WithRegistry(t *testing.T) {
	// Verify NewChecker accepts a Registry and the Checker is usable
	var hookCalled bool
	reg := value.Registry{
		BaseKindOfReflectType: func(_ reflect.Type) value.Kind {
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

func TestCheckValue_List(t *testing.T) {
	t.Parallel()
	stringList := schema.NewListConstraint(schema.NewStringConstraint())
	boundedList := schema.ListLenBetween(schema.NewStringConstraint(), 1, 3)

	tests := []struct {
		name       string
		val        any
		constraint schema.Constraint
		wantErr    bool
	}{
		{"valid string list", []any{"a", "b"}, stringList, false},
		{"empty list unbounded", []any{}, stringList, false},
		{"nil value passthrough", nil, stringList, false},
		{"not a slice", "hello", stringList, true},
		{"wrong element type", []any{123}, stringList, true},
		{"mixed types", []any{"a", 123}, stringList, true},
		{"within bounds", []any{"a", "b"}, boundedList, false},
		{"at min bound", []any{"a"}, boundedList, false},
		{"at max bound", []any{"a", "b", "c"}, boundedList, false},
		{"below min bound", []any{}, boundedList, true},
		{"above max bound", []any{"a", "b", "c", "d"}, boundedList, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := eval.CheckValue(tt.val, tt.constraint)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckValue_List_ElementConstraintViolation(t *testing.T) {
	t.Parallel()
	// String with max length 3
	constraint := schema.NewListConstraint(schema.StringMaxLen(3))

	t.Run("element within constraint", func(t *testing.T) {
		t.Parallel()
		err := eval.CheckValue([]any{"ab", "cd"}, constraint)
		assert.NoError(t, err)
	})

	t.Run("element exceeds constraint", func(t *testing.T) {
		t.Parallel()
		err := eval.CheckValue([]any{"ab", "toolong"}, constraint)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "element [1]")
	})
}

func TestCheckValue_List_Nested(t *testing.T) {
	t.Parallel()
	nested := schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint()))

	t.Run("valid nested", func(t *testing.T) {
		t.Parallel()
		err := eval.CheckValue([]any{[]any{int64(1), int64(2)}, []any{int64(3)}}, nested)
		assert.NoError(t, err)
	})

	t.Run("inner not a slice", func(t *testing.T) {
		t.Parallel()
		err := eval.CheckValue([]any{"not_a_list"}, nested)
		assert.Error(t, err)
	})

	t.Run("inner element wrong type", func(t *testing.T) {
		t.Parallel()
		err := eval.CheckValue([]any{[]any{int64(1), "bad"}}, nested)
		assert.Error(t, err)
	})
}

func TestCoerceValue_List(t *testing.T) {
	t.Parallel()
	checker := eval.NewChecker(value.Registry{})

	t.Run("coerce integer elements", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewIntegerConstraint())
		// float64 values (as from JSON) should coerce to int64
		result, err := checker.CoerceValue([]any{float64(1), float64(2), float64(3)}, constraint)
		require.NoError(t, err)
		slice, ok := result.([]any)
		require.True(t, ok)
		assert.Len(t, slice, 3)
		assert.Equal(t, int64(1), slice[0])
		assert.Equal(t, int64(2), slice[1])
		assert.Equal(t, int64(3), slice[2])
	})

	t.Run("coerce float elements", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewFloatConstraint())
		result, err := checker.CoerceValue([]any{float32(1.5), int(2)}, constraint)
		require.NoError(t, err)
		slice, ok := result.([]any)
		require.True(t, ok)
		assert.Len(t, slice, 2)
		assert.Equal(t, float64(1.5), slice[0])
		assert.Equal(t, float64(2), slice[1])
	})

	t.Run("coerce string elements passthrough", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		result, err := checker.CoerceValue([]any{"hello", "world"}, constraint)
		require.NoError(t, err)
		slice, ok := result.([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"hello", "world"}, slice)
	})

	t.Run("coerce empty list", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		result, err := checker.CoerceValue([]any{}, constraint)
		require.NoError(t, err)
		slice, ok := result.([]any)
		require.True(t, ok)
		assert.Empty(t, slice)
	})

	t.Run("not a slice", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		_, err := checker.CoerceValue("not_a_slice", constraint)
		assert.Error(t, err)
	})

	t.Run("element coercion fails", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewIntegerConstraint())
		_, err := checker.CoerceValue([]any{"not_an_int"}, constraint)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "element [0]")
	})
}

// TestCoerceValue_CanonicalPassthrough pins the canonical-passthrough decision that
// CoerceValue's exhaustiveness guard does not check: the already-canonical kinds
// (String, Boolean, Timestamp, Date, UUID, Enum, Pattern) must return the value
// unchanged with no error. A future kind added to the explicit canonical group is
// thereby forced to be a genuine passthrough, not merely listed.
// TestCoerceValue_CanonicalFormsAreStable covers both halves of the coercer's
// string-valued arm: the kinds that pass a string through untouched, and the
// three that render one — whose already-canonical input must come back
// identical, or coercion would not be idempotent.
func TestCoerceValue_CanonicalFormsAreStable(t *testing.T) {
	t.Parallel()
	checker := eval.NewChecker(value.Registry{})

	cases := []struct {
		name string
		c    schema.Constraint
		val  any
	}{
		{"String", schema.NewStringConstraint(), "x"},
		{"Boolean", schema.NewBooleanConstraint(), true},
		{"Enum", schema.NewEnumConstraint([]string{"a"}), "a"},
		{"Pattern", schema.NewPatternConstraint(nil), "p"},
		{"Timestamp", schema.NewTimestampConstraint(), "2020-01-01T00:00:00Z"},
		{"Date", schema.NewDateConstraint(), "2020-01-01"},
		{"UUID", schema.NewUUIDConstraint(), "550e8400-e29b-41d4-a716-446655440000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := checker.CoerceValue(tc.val, tc.c)
			require.NoError(t, err)
			assert.Equal(t, tc.val, got, "a canonical value must coerce to itself")
		})
	}
}

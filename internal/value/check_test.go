package value_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

func TestCheck_Nil(t *testing.T) {
	// nil is always valid (required check is done separately)
	err := value.Check(nil, schema.NewStringConstraint())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheck_String(t *testing.T) {
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
			err := value.Check(tt.val, tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCheckCoerce_Integer drives Check and Coerce through one
// table. Check enforces kind AND bounds; Coerce converts kind only
// (it documents itself as running after a successful Check), so a
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

		// A float fails both checking and coercion, whole or not and in bounds
		// or not: a float is never an integer.
		{"float64_whole_zero", float64(0.0), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_whole_positive", float64(42.0), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_whole_negative", float64(-42.0), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_whole_large", float64(1000000.0), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_fraction", float64(3.14), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_fraction_half", float64(0.5), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_in_bounds", float64(10.0), schema.IntegerBetween(10, 100), true, nil, true},
		{"float32_whole", float32(42), schema.NewIntegerConstraint(), true, nil, true},
		{"json_number_whole_decimal", json.Number("42.0"), schema.NewIntegerConstraint(), true, nil, true},
		{"json_number_whole_exponent", json.Number("1e2"), schema.NewIntegerConstraint(), true, nil, true},
		{"json_number_integer", json.Number("42"), schema.NewIntegerConstraint(), false, int64(42), false},
		{"float64_nan", math.NaN(), schema.NewIntegerConstraint(), true, nil, true},
		{"float64_inf", math.Inf(1), schema.NewIntegerConstraint(), true, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := value.Check(tt.val, tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coerced, coerceErr := value.Coerce(tt.val, tt.constraint)
			if tt.wantCoerceErr {
				if coerceErr == nil {
					t.Errorf("want an error, got nil")
				}
				return
			}
			if coerceErr != nil {
				t.Fatalf("unexpected error: %v", coerceErr)
			}
			if tt.wantCoerced != nil {
				if !reflect.DeepEqual(coerced, tt.wantCoerced) {
					t.Errorf("got %#v, want %#v", coerced, tt.wantCoerced)
				}
			}
		})
	}
}

// TestCheckCoerce_Float drives Check and Coerce through one table;
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
			err := value.Check(tt.val, tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got nil")
				}
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("%q does not contain %q", err.Error(), tt.errMsg)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coerced, coerceErr := value.Coerce(tt.val, tt.constraint)
			if tt.wantCoerceErr {
				if coerceErr == nil {
					t.Errorf("want an error, got nil")
				}
				return
			}
			if coerceErr != nil {
				t.Fatalf("unexpected error: %v", coerceErr)
			}
			if tt.wantCoerced != nil {
				if !reflect.DeepEqual(coerced, tt.wantCoerced) {
					t.Errorf("got %#v, want %#v", coerced, tt.wantCoerced)
				}
			}
		})
	}
}

func TestCheck_Boolean(t *testing.T) {
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
			err := value.Check(tt.val, schema.NewBooleanConstraint())
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCheckCoerce_Timestamp covers string and time.Time forms plus the
// custom-format constraint. Coerce renders both forms to the one stored
// spelling through value.Canonical, and rejects what Check rejects.
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
			err := value.Check(tt.val, tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Coercion renders the value through the constraint's own layout,
			// so the stored text satisfies the format the schema declared and
			// names the same instant the caller submitted.
			coerced, coerceErr := value.Coerce(tt.val, tt.constraint)
			if coerceErr != nil {
				t.Fatalf("unexpected error: %v", coerceErr)
			}
			text, isString := coerced.(string)
			if !isString {
				t.Fatalf("Timestamp coerces to a string, got %T", coerced)
			}

			layout := time.RFC3339Nano
			if tc, isTS := tt.constraint.(schema.TimestampConstraint); isTS && tc.Format() != "" {
				layout = tc.Format()
			}
			back, parseErr := time.Parse(layout, text)
			if parseErr != nil {
				t.Fatalf("coerced text must satisfy the constraint's layout: unexpected error: %v", parseErr)
			}
			if orig, isTime := tt.val.(time.Time); isTime {
				if !back.Equal(orig) {
					t.Errorf("canonical text names a different instant: %s vs %s", back, orig)
				}
			}
		})
	}
}

func TestCheck_Date(t *testing.T) {
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
			err := value.Check(tt.val, schema.NewDateConstraint())
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCheckCoerce_UUID covers string and uuid.UUID forms. Coerce renders
// both to the one lowercase spelling through value.Canonical, and rejects
// what Check rejects.
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
			err := value.Check(tt.val, constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Every accepted spelling coerces to the one canonical lowercase
			// form, which is what stops two spellings of one UUID producing
			// two primary keys.
			coerced, coerceErr := value.Coerce(tt.val, constraint)
			if coerceErr != nil {
				t.Fatalf("unexpected error: %v", coerceErr)
			}
			if !reflect.DeepEqual(coerced, canonicalUUIDText(t, tt.val)) {
				t.Errorf("got %#v, want %#v", coerced, canonicalUUIDText(t, tt.val))
			}
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
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
		t.Fatalf("not a UUID input: %T", val)
	}
	text, err := u.MarshalText()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return string(text)
}

func TestCheck_Enum(t *testing.T) {
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
			err := value.Check(tt.val, constraint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCheck_Pattern(t *testing.T) {
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
			err := value.Check(tt.val, constraint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestCheckCoerce_Vector drives Check and Coerce through one
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
			err := value.Check(tt.val, constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got nil")
				}
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("%q does not contain %q", err.Error(), tt.errMsg)
					}
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			coerced, coerceErr := value.Coerce(tt.val, constraint)
			if tt.wantCoerceErr {
				if coerceErr == nil {
					t.Errorf("want an error, got nil")
				}
				return
			}
			if coerceErr != nil {
				t.Fatalf("unexpected error: %v", coerceErr)
			}
			if tt.wantCoerced != nil {
				if !reflect.DeepEqual(coerced, tt.wantCoerced) {
					t.Errorf("got %#v, want %#v", coerced, tt.wantCoerced)
				}
			}
		})
	}
}

func TestCheck_Alias(t *testing.T) {
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
			err := value.Check(tt.val, constraint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCheck_UnresolvedAlias(t *testing.T) {
	// Alias without resolved constraint
	constraint := schema.NewAliasConstraint("UnresolvedType", nil)

	err := value.Check(int64(10), constraint)
	if err == nil {
		t.Fatalf("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "unresolved") {
		t.Errorf("%q does not contain %q", err.Error(), "unresolved")
	}
}

func TestCoerce_AliasConstraint(t *testing.T) {
	// Test alias that resolves to integer
	intAlias := schema.NewAliasConstraint("MyInt", schema.NewIntegerConstraint())

	t.Run("alias_to_integer_coerce_int32", func(t *testing.T) {
		result, err := value.Coerce(int32(42), intAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(result, int64(42)) {
			t.Errorf("got %#v, want %#v", result, int64(42))
		}
	})

	t.Run("alias_to_integer_coerce_uint", func(t *testing.T) {
		result, err := value.Coerce(uint(100), intAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(result, int64(100)) {
			t.Errorf("got %#v, want %#v", result, int64(100))
		}
	})

	// Test alias that resolves to float
	floatAlias := schema.NewAliasConstraint("MyFloat", schema.NewFloatConstraint())

	t.Run("alias_to_float_coerce_int", func(t *testing.T) {
		result, err := value.Coerce(int64(42), floatAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(result, float64(42)) {
			t.Errorf("got %#v, want %#v", result, float64(42))
		}
	})

	t.Run("alias_to_float_coerce_float32", func(t *testing.T) {
		result, err := value.Coerce(float32(3.14), floatAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := float64(float32(3.14)); result != want {
			t.Errorf("got %#v, want %#v", result, want)
		}
	})

	// Test alias that resolves to string (no coercion needed)
	stringAlias := schema.NewAliasConstraint("MyString", schema.NewStringConstraint())

	t.Run("alias_to_string_passthrough", func(t *testing.T) {
		result, err := value.Coerce("hello", stringAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(result, "hello") {
			t.Errorf("got %#v, want %#v", result, "hello")
		}
	})

	// Test nil value
	t.Run("nil_value_returns_nil", func(t *testing.T) {
		result, err := value.Coerce(nil, intAlias)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Errorf("got %#v, want nil", result)
		}
	})
}

func TestCheck_List(t *testing.T) {
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
		// A nil ELEMENT is not the nil VALUE above. Check short-circuits
		// nil for the optional-property slot; no element position is optional,
		// so the element constraint applies and the null is rejected.
		//
		// Mutation: removing checkList's nil guard turns these three green.
		{"nil element among strings", []any{"a", nil}, stringList, true},
		{"nil as the only element", []any{nil}, stringList, true},
		{"nil element within bounds", []any{"a", nil}, boundedList, true},
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
			err := value.Check(tt.val, tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Errorf("want an error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCheck_List_ElementConstraintViolation(t *testing.T) {
	t.Parallel()
	// String with max length 3
	constraint := schema.NewListConstraint(schema.StringMaxLen(3))

	t.Run("element within constraint", func(t *testing.T) {
		t.Parallel()
		err := value.Check([]any{"ab", "cd"}, constraint)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("element exceeds constraint", func(t *testing.T) {
		t.Parallel()
		err := value.Check([]any{"ab", "toolong"}, constraint)
		if err == nil {
			t.Fatalf("want an error, got nil")
		}
		if !strings.Contains(err.Error(), "element [1]") {
			t.Errorf("%q does not contain %q", err.Error(), "element [1]")
		}
	})
}

func TestCheck_List_Nested(t *testing.T) {
	t.Parallel()
	nested := schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint()))

	t.Run("valid nested", func(t *testing.T) {
		t.Parallel()
		err := value.Check([]any{[]any{int64(1), int64(2)}, []any{int64(3)}}, nested)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("inner not a slice", func(t *testing.T) {
		t.Parallel()
		err := value.Check([]any{"not_a_list"}, nested)
		if err == nil {
			t.Errorf("want an error, got nil")
		}
	})

	t.Run("inner element wrong type", func(t *testing.T) {
		t.Parallel()
		err := value.Check([]any{[]any{int64(1), "bad"}}, nested)
		if err == nil {
			t.Errorf("want an error, got nil")
		}
	})
}

func TestCoerce_List(t *testing.T) {
	t.Parallel()
	t.Run("coerce integer elements", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewIntegerConstraint())
		// Integer literals from a JSON document coerce to int64; a float
		// element is refused, whole or not.
		result, err := value.Coerce([]any{json.Number("1"), int32(2), int64(3)}, constraint)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, ferr := value.Coerce([]any{int64(1), float64(2)}, constraint)
		if ferr == nil || !strings.Contains(ferr.Error(), "element [1]: cannot coerce float 2.0 to int64") {
			t.Fatalf("error %v, want one containing %q", ferr, "element [1]: cannot coerce float 2.0 to int64")
		}
		slice, ok := result.([]any)
		if !ok {
			t.Fatalf("want %s", "ok")
		}
		if len(slice) != 3 {
			t.Errorf("len %d, want %d", len(slice), 3)
		}
		if !reflect.DeepEqual(slice[0], int64(1)) {
			t.Errorf("got %#v, want %#v", slice[0], int64(1))
		}
		if !reflect.DeepEqual(slice[1], int64(2)) {
			t.Errorf("got %#v, want %#v", slice[1], int64(2))
		}
		if !reflect.DeepEqual(slice[2], int64(3)) {
			t.Errorf("got %#v, want %#v", slice[2], int64(3))
		}
	})

	// The check side rejects a null element, so the coercer only meets one when
	// a caller coerces without checking. It refuses rather than writing a nil
	// into the coerced list, which is the invariant a validated List carries.
	//
	// Mutation: removing coerceList's nil guard turns this green and the
	// returned slice holds a nil at index 1.
	t.Run("nil element is refused", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		result, err := value.Coerce([]any{"a", nil}, constraint)
		if err == nil {
			t.Fatalf("want an error, got nil")
		}
		if result != nil {
			t.Errorf("got %#v, want nil", result)
		}
		if !strings.Contains(err.Error(), "element [1]") {
			t.Errorf("%q does not contain %q", err.Error(), "element [1]")
		}
	})

	t.Run("coerce float elements", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewFloatConstraint())
		result, err := value.Coerce([]any{float32(1.5), int(2)}, constraint)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slice, ok := result.([]any)
		if !ok {
			t.Fatalf("want %s", "ok")
		}
		if len(slice) != 2 {
			t.Errorf("len %d, want %d", len(slice), 2)
		}
		if !reflect.DeepEqual(slice[0], float64(1.5)) {
			t.Errorf("got %#v, want %#v", slice[0], float64(1.5))
		}
		if !reflect.DeepEqual(slice[1], float64(2)) {
			t.Errorf("got %#v, want %#v", slice[1], float64(2))
		}
	})

	t.Run("coerce string elements passthrough", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		result, err := value.Coerce([]any{"hello", "world"}, constraint)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slice, ok := result.([]any)
		if !ok {
			t.Fatalf("want %s", "ok")
		}
		if !reflect.DeepEqual(slice, []any{"hello", "world"}) {
			t.Errorf("got %#v, want %#v", slice, []any{"hello", "world"})
		}
	})

	t.Run("coerce empty list", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		result, err := value.Coerce([]any{}, constraint)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slice, ok := result.([]any)
		if !ok {
			t.Fatalf("want %s", "ok")
		}
		if len(slice) != 0 {
			t.Errorf("got %#v, want it empty", slice)
		}
	})

	t.Run("not a slice", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewStringConstraint())
		_, err := value.Coerce("not_a_slice", constraint)
		if err == nil {
			t.Errorf("want an error, got nil")
		}
	})

	t.Run("element coercion fails", func(t *testing.T) {
		t.Parallel()
		constraint := schema.NewListConstraint(schema.NewIntegerConstraint())
		_, err := value.Coerce([]any{"not_an_int"}, constraint)
		if err == nil {
			t.Fatalf("want an error, got nil")
		}
		if !strings.Contains(err.Error(), "element [0]") {
			t.Errorf("%q does not contain %q", err.Error(), "element [0]")
		}
	})
}

// TestCoerce_CanonicalPassthrough pins the canonical-passthrough decision that
// Coerce's exhaustiveness guard does not check: the already-canonical kinds
// (String, Boolean, Timestamp, Date, UUID, Enum, Pattern) must return the value
// unchanged with no error. A future kind added to the explicit canonical group is
// thereby forced to be a genuine passthrough, not merely listed.
// TestCoerce_CanonicalFormsAreStable covers both halves of the coercer's
// string-valued arm: the kinds that pass a string through untouched, and the
// three that render one — whose already-canonical input must come back
// identical, or coercion would not be idempotent.
func TestCoerce_CanonicalFormsAreStable(t *testing.T) {
	t.Parallel()
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
			got, err := value.Coerce(tc.val, tc.c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.val) {
				t.Errorf("a canonical value must coerce to itself: got %#v, want %#v", got, tc.val)
			}
		})
	}
}

// Every pattern judges the value, and a miss names the pattern that missed.
func TestCheck_EveryPatternJudges(t *testing.T) {
	t.Parallel()
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a"), regexp.MustCompile("b$")})
	if err := value.Check("ab", c); err != nil {
		t.Errorf("ab: %v, want nil", err)
	}
	for text, missed := range map[string]string{"ac": "b$", "xb": "^a"} {
		err := value.Check(text, c)
		if err == nil || !strings.Contains(err.Error(), missed) {
			t.Errorf("%s: %v, want a miss naming %s", text, err, missed)
		}
	}

	// A text that misses both names the first, on each call: the check reads the
	// constraint's own slice, which every text shares.
	for range 2 {
		if err := value.Check("xy", c); err == nil || !strings.Contains(err.Error(), "^a") {
			t.Errorf("xy: %v, want a miss naming ^a", err)
		}
	}
}

// The validator judges a pattern once per value. The detector makes regexp's sync.Pool
// drop what it holds, so a match allocates under -race alone.
func TestCheck_APatternAllocatesNothing(t *testing.T) {
	raceskip.Skip(t)
	var c schema.Constraint = schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a"), regexp.MustCompile("b$")})
	var text any = "ab"
	var err error
	if allocs := testing.AllocsPerRun(100, func() { err = value.Check(text, c) }); allocs != 0 {
		t.Errorf("a pattern check allocated %v times, want 0", allocs)
	}
	if err != nil {
		t.Error(err)
	}
}

// A []float64 is the form a Vector is stored in. The check reads it in place
// and the coercion copies it once, whatever its length, and each answers as it
// answers the same elements in a []any.
func TestCheckAndCoerce_AStoredVectorCostsOneCopy(t *testing.T) {
	raceskip.Skip(t)
	var c schema.Constraint = schema.NewVectorConstraint(1536)
	stored := make([]float64, 1536)
	for i := range stored {
		stored[i] = float64(i) + 0.5
	}
	val := any(stored)
	form := testing.AllocsPerRun(100, func() { _ = value.Absent(val) })
	if allocs := testing.AllocsPerRun(100, func() {
		if err := value.Check(val, c); err != nil {
			t.Fatal(err)
		}
	}); allocs != form {
		t.Errorf("Check allocates %v per call at a []float64 of 1536; want %v, the cost of reading its form", allocs, form)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := value.Coerce(val, c); err != nil {
			t.Fatal(err)
		}
	}); allocs != form+2 {
		t.Errorf("Coerce allocates %v per call at a []float64 of 1536; want %v, its form, the copy and the copy's box", allocs, form+2)
	}
	got, err := value.Coerce(val, c)
	out, ok := got.([]float64)
	if err != nil || !ok || !reflect.DeepEqual(out, stored) || &out[0] == &stored[0] {
		t.Errorf("Coerce = %T, %v; want a copy of the []float64", got, err)
	}
}

// The check and the coercion answer a []float64 at a Vector as they answer the
// same elements in a []any: a wrong dimension and an element that is not finite
// are refused in the same words.
func TestCheckAndCoerce_AStoredVectorIsJudgedAsAList(t *testing.T) {
	t.Parallel()
	c := schema.NewVectorConstraint(3)
	for name, stored := range map[string][]float64{
		"a short vector":    {1, 2},
		"a long vector":     {1, 2, 3, 4},
		"a NaN":             {1, math.NaN(), 3},
		"an infinity":       {1, 2, math.Inf(-1)},
		"a vector":          {1, 2.5, -3},
		"an empty vector":   {},
		"a negative zero":   {math.Copysign(0, -1), 0, 0},
		"the largest float": {math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64},
	} {
		list := make([]any, len(stored))
		for i, f := range stored {
			list[i] = f
		}
		wantErr, gotErr := value.Check(list, c), value.Check(stored, c)
		if !reflect.DeepEqual(gotErr, wantErr) {
			t.Errorf("%s: Check = %#v; as a []any it is %#v", name, gotErr, wantErr)
		}
		want, wantErr := value.Coerce(list, c)
		got, gotErr := value.Coerce(stored, c)
		if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
			t.Errorf("%s: Coerce = %#v, %v; as a []any it is %#v, %v", name, got, gotErr, want, wantErr)
		}
	}
}

// A value of the kind that breaks a bound, a length, a member set, a pattern or
// finiteness is a constraint failure, at a property and at an element. A value
// of another kind is a type mismatch.
func TestCheck_ARefusalCarriesItsKind(t *testing.T) {
	t.Parallel()
	list := schema.NewListConstraint
	for name, c := range map[string]struct {
		v    any
		c    schema.Constraint
		kind value.CheckErrorKind
		want string
	}{
		"a string below its minimum":   {"a", schema.StringLenBetween(2, 3), value.KindConstraintFail, "string length 1 is less than minimum 2"},
		"a string above its maximum":   {"abcd", schema.StringLenBetween(2, 3), value.KindConstraintFail, "string length 4 exceeds maximum 3"},
		"an integer below its minimum": {int64(-1), schema.IntegerBetween(0, 10), value.KindConstraintFail, "integer -1 is less than minimum 0"},
		"an integer above its maximum": {int64(11), schema.IntegerBetween(0, 10), value.KindConstraintFail, "integer 11 exceeds maximum 10"},
		"a float below its minimum":    {-0.5, schema.FloatBetween(0, 1), value.KindConstraintFail, "float -0.5 is less than minimum 0"},
		"a float above its maximum":    {1.5, schema.FloatBetween(0, 1), value.KindConstraintFail, "float 1.5 exceeds maximum 1"},
		"an int64 above a Float's maximum": {
			int64(5), schema.FloatBetween(0, 1), value.KindConstraintFail, "float 5 exceeds maximum 1",
		},
		"a uint64 above a Float's maximum": {
			uint64(math.MaxUint64), schema.FloatBetween(0, 1), value.KindConstraintFail, "float 1.8446744073709552e+19 exceeds maximum 1",
		},
		"a NaN":                       {math.NaN(), schema.NewFloatConstraint(), value.KindConstraintFail, "float value is not finite (NaN or Inf)"},
		"an infinity":                 {math.Inf(1), schema.NewFloatConstraint(), value.KindConstraintFail, "float value is not finite (NaN or Inf)"},
		"no member of an Enum":        {"b", schema.NewEnumConstraint([]string{"a"}), value.KindConstraintFail, ""},
		"no match of a Pattern":       {"b", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a$")}), value.KindConstraintFail, ""},
		"a short vector":              {[]any{1.0}, schema.NewVectorConstraint(2), value.KindConstraintFail, "vector has 1 elements, expected 2"},
		"a long vector":               {[]any{1.0, 2.0}, schema.NewVectorConstraint(1), value.KindConstraintFail, "vector has 2 elements, expected 1"},
		"a long stored vector":        {[]float64{1, 2}, schema.NewVectorConstraint(1), value.KindConstraintFail, "vector has 2 elements, expected 1"},
		"a NaN in a vector":           {[]any{math.NaN()}, schema.NewVectorConstraint(1), value.KindConstraintFail, "vector element [0]: float value is not finite (NaN or Inf)"},
		"a string in a vector":        {[]any{"x"}, schema.NewVectorConstraint(1), value.KindTypeMismatch, "vector element [0]: expected float, got string"},
		"a short list":                {[]any{}, schema.ListLenBetween(schema.NewStringConstraint(), 1, 2), value.KindConstraintFail, "list length 0 is less than minimum 1"},
		"a long list":                 {[]any{"a", "b", "c"}, schema.ListLenBetween(schema.NewStringConstraint(), 1, 2), value.KindConstraintFail, "list length 3 exceeds maximum 2"},
		"an element above its bound":  {[]any{"abc"}, list(schema.StringLenBetween(0, 2)), value.KindConstraintFail, "element [0]: string length 3 exceeds maximum 2"},
		"an element of another kind":  {[]any{5}, list(schema.NewStringConstraint()), value.KindTypeMismatch, "element [0]: expected string, got int"},
		"a null element at an alias":  {[]any{"a", nil}, list(schema.NewAliasConstraint("Name", schema.NewStringConstraint())), value.KindTypeMismatch, "element [1]: expected string, got null"},
		"a number at a String":        {5, schema.NewStringConstraint(), value.KindTypeMismatch, "expected string, got int"},
		"a string at an Integer":      {"5", schema.NewIntegerConstraint(), value.KindTypeMismatch, "expected integer, got string"},
		"a string at a bounded Float": {"5", schema.FloatBetween(0, 1), value.KindTypeMismatch, "expected float, got string"},
	} {
		err := value.Check(c.v, c.c)
		ce, ok := errors.AsType[*value.CheckError](err)
		if !ok || ce.Kind != c.kind || (c.want != "" && ce.Msg != c.want) {
			t.Errorf("%s: Check = %#v; want kind %d and %q", name, err, c.kind, c.want)
		}
	}
}

// An alias that resolves to nothing is refused by the check and by the
// coercion, alone and as a list's element, and CheckKind refuses a kind that
// has no bare check.
func TestCheckAndCoerce_RefuseWhatHasNoRule(t *testing.T) {
	t.Parallel()
	gone := schema.NewAliasConstraint("Gone", nil)
	const want = "unresolved alias constraint: Gone"
	if err := value.Check("x", gone); err == nil || err.Error() != want {
		t.Errorf("Check at an unresolved alias = %v; want %q", err, want)
	}
	if got, err := value.Coerce("x", gone); got != nil || err == nil || err.Error() != want {
		t.Errorf("Coerce at an unresolved alias = %#v, %v; want nil and %q", got, err, want)
	}
	if err := value.Check([]any{"x"}, schema.NewListConstraint(gone)); err == nil || err.Error() != "element [0]: "+want {
		t.Errorf("Check of an element at an unresolved alias = %v; want the element named", err)
	}
	if got, err := value.Coerce([]any{"x"}, schema.NewListConstraint(gone)); got != nil || err == nil || err.Error() != "element [0]: "+want {
		t.Errorf("Coerce of an element at an unresolved alias = %#v, %v; want nil and the element named", got, err)
	}
	for _, k := range []schema.ConstraintKind{schema.KindEnum, schema.KindPattern, schema.KindVector, schema.KindList, schema.KindAlias} {
		if err := value.CheckKind("x", k); err == nil || err.Error() != "value: no bare check for kind "+k.String() {
			t.Errorf("CheckKind at %s = %v; want a refusal that names the kind", k, err)
		}
	}
}

// The vector coercion names the element it refuses in the Float rule's words,
// and an element that holds none as a null.
func TestCoerce_AVectorNamesTheElementItRefuses(t *testing.T) {
	t.Parallel()
	c := schema.NewVectorConstraint(2)
	for name, tc := range map[string]struct {
		v    any
		want string
	}{
		"a null element":          {[]any{1.0, nil}, "vector element [1]: expected float, got null"},
		"a string element":        {[]any{"x", 1.0}, "vector element [0]: cannot coerce string to float64"},
		"a NaN":                   {[]any{1.0, math.NaN()}, "vector element [1]: cannot coerce non-finite float (NaN or Inf)"},
		"a NaN in a stored form":  {[]float64{math.NaN(), 1}, "vector element [0]: cannot coerce non-finite float (NaN or Inf)"},
		"an infinity in a stored": {[]float64{1, math.Inf(1)}, "vector element [1]: cannot coerce non-finite float (NaN or Inf)"},
	} {
		if got, err := value.Coerce(tc.v, c); got != nil || err == nil || err.Error() != tc.want {
			t.Errorf("%s: Coerce = %#v, %v; want nil and %q", name, got, err, tc.want)
		}
	}
}

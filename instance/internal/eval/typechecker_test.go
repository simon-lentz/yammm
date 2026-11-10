package eval_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/instance/internal/eval"
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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

		// A float is never an integer, whole or not.
		{"float64_zero", float64(0.0), false},
		{"float64_positive", float64(42.0), false},
		{"float64_negative", float64(-42.0), false},
		{"float64_large", float64(1000000.0), false},
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
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
			if !reflect.DeepEqual(ok, tt.expected) {
				t.Errorf("got %#v, want %#v", ok, tt.expected)
			}
			if !tt.expected {
				if len(msg) == 0 {
					t.Errorf("got an empty value, want one that holds text")
				}
			}
		})
	}
}

package csv

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/schema"
)

func TestCoerceStringValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    []Option
		input   string
		c       schema.Constraint
		want    any
		wantErr string // non-empty = expect an error containing this substring ("*" = any)
	}{
		{name: "string", input: "hello", c: schema.NewStringConstraint(), want: "hello"},
		{name: "empty string", input: "", c: schema.NewStringConstraint(), want: ""},
		{name: "integer", input: "42", c: schema.NewIntegerConstraint(), want: int64(42)},
		{name: "integer invalid", input: "abc", c: schema.NewIntegerConstraint(), wantErr: "cannot parse"},
		{name: "float", input: "3.14", c: schema.NewFloatConstraint(), want: 3.14},
		{name: "bool true", input: "true", c: schema.NewBooleanConstraint(), want: true},
		{name: "bool false", input: "false", c: schema.NewBooleanConstraint(), want: false},
		{name: "bool 1", input: "1", c: schema.NewBooleanConstraint(), want: true},
		{name: "bool 0", input: "0", c: schema.NewBooleanConstraint(), want: false},
		{name: "bool t", input: "t", c: schema.NewBooleanConstraint(), want: true},
		{name: "bool f", input: "f", c: schema.NewBooleanConstraint(), want: false},
		// Temporal values stay as validated strings; the instance layer owns
		// the typed conversion.
		{name: "date", input: "2024-06-15", c: schema.NewDateConstraint(), want: "2024-06-15"},
		{name: "date invalid", input: "not-a-date", c: schema.NewDateConstraint(), wantErr: "*"},
		{name: "timestamp", input: "2024-01-01T00:00:00Z", c: schema.NewTimestampConstraint(), want: "2024-01-01T00:00:00Z"},
		{name: "timestamp nano", input: "2024-01-01T00:00:00.123456789Z", c: schema.NewTimestampConstraint(), want: "2024-01-01T00:00:00.123456789Z"},
		{name: "list of strings", input: "a|b|c", c: schema.NewListConstraint(schema.NewStringConstraint()), want: []any{"a", "b", "c"}},
		{name: "list of integers", input: "1|2|3", c: schema.NewListConstraint(schema.NewIntegerConstraint()), want: []any{int64(1), int64(2), int64(3)}},
		// Empty string is not the configured null value, so it is an empty list.
		{
			name: "empty list", opts: []Option{WithNullValue("NULL")}, input: "",
			c: schema.NewListConstraint(schema.NewStringConstraint()), want: []any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := New(tt.opts...)
			val, err := a.coerceStringValue(tt.input, tt.c)
			if tt.wantErr != "" {
				require.Error(t, err, "input: %q", tt.input)
				if tt.wantErr != "*" {
					assert.Contains(t, err.Error(), tt.wantErr)
				}
				return
			}
			require.NoError(t, err, "input: %q", tt.input)
			assert.Equal(t, tt.want, val)
		})
	}
}

func TestStripBOM(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"with BOM", append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...), "hello"},
		{"without BOM", []byte("hello"), "hello"},
		{"short input", []byte("hi"), "hi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := stripBOM(bytes.NewReader(tt.input))
			data, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

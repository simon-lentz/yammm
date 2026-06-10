package csv

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/schema"
)

func TestCoerceStringValue_String(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("hello", schema.NewStringConstraint())
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestCoerceStringValue_Integer(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("42", schema.NewIntegerConstraint())
	require.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestCoerceStringValue_IntegerInvalid(t *testing.T) {
	t.Parallel()
	a := New()
	_, err := a.coerceStringValue("abc", schema.NewIntegerConstraint())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse")
}

func TestCoerceStringValue_Float(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("3.14", schema.NewFloatConstraint())
	require.NoError(t, err)
	assert.Equal(t, 3.14, val)
}

func TestCoerceStringValue_Boolean(t *testing.T) {
	t.Parallel()
	a := New()
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"t", true},
		{"f", false},
	} {
		val, err := a.coerceStringValue(tc.input, schema.NewBooleanConstraint())
		require.NoError(t, err, "input: %q", tc.input)
		assert.Equal(t, tc.want, val, "input: %q", tc.input)
	}
}

func TestCoerceStringValue_Date(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("2024-06-15", schema.NewDateConstraint())
	require.NoError(t, err)
	assert.Equal(t, "2024-06-15", val) // stays as string
}

func TestCoerceStringValue_DateInvalid(t *testing.T) {
	t.Parallel()
	a := New()
	_, err := a.coerceStringValue("not-a-date", schema.NewDateConstraint())
	assert.Error(t, err)
}

func TestCoerceStringValue_Timestamp(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("2024-01-01T00:00:00Z", schema.NewTimestampConstraint())
	require.NoError(t, err)
	assert.Equal(t, "2024-01-01T00:00:00Z", val) // stays as string
}

func TestCoerceStringValue_TimestampNano(t *testing.T) {
	t.Parallel()
	a := New()
	val, err := a.coerceStringValue("2024-01-01T00:00:00.123456789Z", schema.NewTimestampConstraint())
	require.NoError(t, err)
	assert.Equal(t, "2024-01-01T00:00:00.123456789Z", val)
}

func TestCoerceStringValue_EmptyString(t *testing.T) {
	t.Parallel()
	a := New()
	// Empty string is a valid string value (null is handled by the caller).
	val, err := a.coerceStringValue("", schema.NewStringConstraint())
	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestCoerceStringValue_ListString(t *testing.T) {
	t.Parallel()
	a := New()
	c := schema.NewListConstraint(schema.NewStringConstraint())
	val, err := a.coerceStringValue("a|b|c", c)
	require.NoError(t, err)
	assert.Equal(t, []any{"a", "b", "c"}, val)
}

func TestCoerceStringValue_ListInteger(t *testing.T) {
	t.Parallel()
	a := New()
	c := schema.NewListConstraint(schema.NewIntegerConstraint())
	val, err := a.coerceStringValue("1|2|3", c)
	require.NoError(t, err)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, val)
}

func TestCoerceStringValue_ListEmpty(t *testing.T) {
	t.Parallel()
	a := New(WithNullValue("NULL"))
	c := schema.NewListConstraint(schema.NewStringConstraint())
	// Empty string is not the null value here, so it's an empty list.
	val, err := a.coerceStringValue("", c)
	require.NoError(t, err)
	assert.Equal(t, []any{}, val)
}

func TestStripBOM_WithBOM(t *testing.T) {
	t.Parallel()
	input := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)
	r := stripBOM(bytes.NewReader(input))
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestStripBOM_WithoutBOM(t *testing.T) {
	t.Parallel()
	r := stripBOM(strings.NewReader("hello"))
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestStripBOM_ShortInput(t *testing.T) {
	t.Parallel()
	r := stripBOM(strings.NewReader("hi"))
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "hi", string(data))
}

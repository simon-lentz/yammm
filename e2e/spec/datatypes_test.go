package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

// raw constructs a RawInstance from a property map for inline test data.
func raw(props map[string]any) instance.RawInstance {
	return instance.RawInstance{Properties: props}
}

// =============================================================================
// Integer
// =============================================================================

// TestDatatypes_IntegerUnbounded verifies that an unbounded Integer accepts
// arbitrary signed values.
// Source: SPEC.md, "Integer" — "age Integer // unbounded integer"
func TestDatatypes_IntegerUnbounded(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "IntUnbounded"
type R {
    id String primary
    val Integer
}`, "int_unbounded")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 999999}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": -999999}))
}

// TestDatatypes_IntegerBothBounds verifies that Integer[0, 150] accepts
// boundary values and rejects out-of-range values.
// Source: SPEC.md, "Integer" — "age Integer[0, 150] // 0 to 150 inclusive"
func TestDatatypes_IntegerBothBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "IntBounds"
type R {
    id String primary
    val Integer[0, 150] required
}`, "int_bounds")
	// boundary values pass
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 0}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 150}))
	// out of range fails
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "val": -1}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "4", "val": 151}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_IntegerOneSidedMin verifies that Integer[1, _] rejects 0 and
// accepts 1 (the underscore means unbounded upper).
// Source: SPEC.md, "Integer" — "count Integer[1, _] // minimum 1, no maximum"
func TestDatatypes_IntegerOneSidedMin(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "IntMin"
type R {
    id String primary
    val Integer[1, _] required
}`, "int_min")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 1}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 999999}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "val": 0}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_IntegerOneSidedMax verifies that Integer[_, 99] rejects 100 and
// accepts 99 (the underscore means unbounded lower).
// Source: SPEC.md, "Integer" — "index Integer[_, 99] // no minimum, maximum 99"
func TestDatatypes_IntegerOneSidedMax(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "IntMax"
type R {
    id String primary
    val Integer[_, 99] required
}`, "int_max")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 99}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": -999999}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "val": 100}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_IntegerNegativeBounds verifies that Integer[-40, 50] accepts
// negative boundary values and rejects values outside that range.
// Source: SPEC.md, "Integer" — "temperature Integer[-40, 50] // negative lower bound"
func TestDatatypes_IntegerNegativeBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "IntNeg"
type R {
    id String primary
    val Integer[-40, 50] required
}`, "int_neg")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": -40}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 50}))
	assertValid(t, v, "R", raw(map[string]any{"id": "3", "val": 0}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "4", "val": -41}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "5", "val": 51}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Float
// =============================================================================

// TestDatatypes_FloatUnbounded verifies that an unbounded Float accepts
// arbitrary floating-point values.
// Source: SPEC.md, "Float" — "temperature Float // unbounded float"
func TestDatatypes_FloatUnbounded(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "FltUnbounded"
type R {
    id String primary
    val Float
}`, "flt_unbounded")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 99999.99}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": -99999.99}))
}

// TestDatatypes_FloatBothBounds verifies that Float[0.0, 100.0] accepts
// boundary values and rejects out-of-range values.
// Source: SPEC.md, "Float" — "percentage Float[0.0, 100.0] // 0 to 100 inclusive"
func TestDatatypes_FloatBothBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "FltBounds"
type R {
    id String primary
    val Float[0.0, 100.0] required
}`, "flt_bounds")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 0.0}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 100.0}))
	assertValid(t, v, "R", raw(map[string]any{"id": "3", "val": 50.5}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "4", "val": -0.1}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "5", "val": 100.1}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_FloatIntegerStyleBounds verifies that Float bounds can use
// integer-style notation (e.g., Float[0, 1.0]).
// Source: SPEC.md, "Float" — "ratio Float[0, 1.0] // 0 to 1 inclusive"
func TestDatatypes_FloatIntegerStyleBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "FltIntBounds"
type R {
    id String primary
    val Float[0, 1.0] required
}`, "flt_int_bounds")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": 0.0}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 1.0}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "val": -0.1}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "4", "val": 1.1}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_FloatNegativeBounds verifies that Float[-90.0, 90.0] accepts
// negative boundary values and rejects values outside that range.
// Source: SPEC.md, "Float" — "latitude Float[-90.0, 90.0] // negative lower bound"
func TestDatatypes_FloatNegativeBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "FltNeg"
type R {
    id String primary
    val Float[-90.0, 90.0] required
}`, "flt_neg")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": -90.0}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": 90.0}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "val": -90.1}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "4", "val": 90.1}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Boolean
// =============================================================================

// TestDatatypes_Boolean verifies that Boolean accepts true and false.
// Source: SPEC.md, "Boolean" — "Represents true/false values"
func TestDatatypes_Boolean(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Bool"
type R {
    id String primary
    val Boolean required
}`, "bool")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "val": true}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "val": false}))
}

// =============================================================================
// String
// =============================================================================

// TestDatatypes_StringLengthInRunes verifies that String length bounds are
// counted in runes, not bytes. "caf\u00e9" is 4 runes but 5 UTF-8 bytes.
// Source: SPEC.md, "String" — "counted in runes, not bytes"
func TestDatatypes_StringLengthInRunes(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/datatypes/string_runes.yammm")
	data := "testdata/datatypes/data.json"
	// "caf\u00e9" = 4 runes, 5 UTF-8 bytes => passes String[1, 4]
	assertValid(t, v, "RuneTest", loadTestData(t, data, "RuneTest")[0])
	// "caf\u00e9!" = 5 runes => fails String[1, 4]
	assertInvalid(t, v, "RuneTest", loadTestData(t, data, "RuneTest__too_long")[0], diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_StringExactLength verifies that String[3, 3] requires exactly
// 3 runes.
// Source: SPEC.md, "String" — "code String[3, 3] // exactly 3 runes"
func TestDatatypes_StringExactLength(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrExact"
type R {
    id String primary
    code String[3, 3] required
}`, "str_exact")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "code": "ABC"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "code": "AB"}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "code": "ABCD"}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_StringBothBounds verifies that String[1, 100] accepts strings
// within range and rejects empty strings.
// Source: SPEC.md, "String" — "name String[1, 100] // 1 to 100 runes"
func TestDatatypes_StringBothBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrBounds"
type R {
    id String primary
    name String[1, 100] required
}`, "str_bounds")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "a"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "name": ""}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_StringMaxOnly verifies that String[_, 1000] has no minimum
// but enforces a maximum.
// Source: SPEC.md, "String" — "notes String[_, 1000] // maximum 1000 runes"
func TestDatatypes_StringMaxOnly(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrMax"
type R {
    id String primary
    notes String[_, 5] required
}`, "str_max")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "notes": ""}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "notes": "abcde"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "notes": "abcdef"}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Enum
// =============================================================================

// TestDatatypes_EnumMinimumTwoOptions verifies that an Enum with fewer than
// two options fails compilation.
// Source: SPEC.md, "Enum" — "At least two options must be provided."
func TestDatatypes_EnumMinimumTwoOptions(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "BadEnum"
type R {
    id String primary
    status Enum["only_one"]
}`, "enum_one")
	assertDiagHasCode(t, result, diag.E_INVALID_CONSTRAINT)
}

// TestDatatypes_EnumValidAndInvalid verifies that a valid enum value is accepted
// and an invalid value is rejected.
// Source: SPEC.md, "Enum" — enum value matching
func TestDatatypes_EnumValidAndInvalid(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "GoodEnum"
type R {
    id String primary
    status Enum["active", "inactive"] required
}`, "enum_valid")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "status": "active"}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "status": "inactive"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "status": "unknown"}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_EnumTrailingComma verifies that a trailing comma in the enum
// option list is allowed syntactically.
// Source: SPEC.md, "Enum" grammar — '{ "," STRING } [ "," ]'
func TestDatatypes_EnumTrailingComma(t *testing.T) {
	t.Parallel()
	// Trailing comma in enum list should compile cleanly
	loadSchemaString(t, `schema "TrailingEnum"
type R {
    id String primary
    status Enum["a", "b",]
}`, "enum_trailing")
}

// =============================================================================
// Pattern
// =============================================================================

// TestDatatypes_PatternSingle verifies that a single-pattern Pattern validates
// the value against that regex.
// Source: SPEC.md, "Pattern" — single pattern validation
func TestDatatypes_PatternSingle(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Pat1"
type R {
    id String primary
    email Pattern["^[^@]+@[^@]+$"] required
}`, "pat1")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "email": "a@b.com"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "email": "invalid"}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_PatternDual verifies that when two patterns are provided, the
// value must match both.
// Source: SPEC.md, "Pattern" — "When two patterns are provided, the value must match both."
func TestDatatypes_PatternDual(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Pat2"
type R {
    id String primary
    code Pattern["^[A-Z]+$", "^.{3}$"] required
}`, "pat2")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "code": "ABC"}))
	// too short: matches uppercase but not 3-char length
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "code": "AB"}), diag.E_CONSTRAINT_FAIL)
	// lowercase: matches 3-char length but not uppercase
	assertInvalid(t, v, "R", raw(map[string]any{"id": "3", "code": "abc"}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Timestamp
// =============================================================================

// TestDatatypes_TimestampRFC3339 verifies that Timestamp defaults to RFC3339
// format and rejects non-conforming strings.
// Source: SPEC.md, "Timestamp" — 'When omitted, RFC3339 ("2006-01-02T15:04:05Z07:00") is used.'
func TestDatatypes_TimestampRFC3339(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Ts"
type R {
    id String primary
    created Timestamp required
}`, "ts")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "created": "2026-01-01T00:00:00Z"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "created": "not-a-timestamp"}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Date
// =============================================================================

// TestDatatypes_Date verifies that Date accepts YYYY-MM-DD format and rejects
// non-conforming strings.
// Source: SPEC.md, "Date" — "Represents a date value (without time component)"
func TestDatatypes_Date(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Dt"
type R {
    id String primary
    birthday Date required
}`, "date")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "birthday": "2026-01-15"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "birthday": "not-a-date"}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// UUID
// =============================================================================

// TestDatatypes_UUID verifies that UUID accepts a valid UUID string and rejects
// an invalid one.
// Source: SPEC.md, "UUID" — "Represents a universally unique identifier"
func TestDatatypes_UUID(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Uuid"
type R {
    id UUID primary
}`, "uuid")
	assertValid(t, v, "R", raw(map[string]any{"id": "550e8400-e29b-41d4-a716-446655440000"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "not-a-uuid"}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Vector
// =============================================================================

// TestDatatypes_VectorDimension verifies that Vector[3] accepts a 3-element
// float slice and rejects a 2-element one (wrong dimension).
// Source: SPEC.md, "Vector" — dimension enforcement
func TestDatatypes_VectorDimension(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Vec"
type R {
    id String primary
    embedding Vector[3] required
}`, "vec")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "embedding": []any{1.0, 2.0, 3.0}}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "embedding": []any{1.0, 2.0}}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_VectorTooMany verifies that Vector[3] rejects a 4-element slice.
// Source: SPEC.md, "Vector" — fixed-dimension enforcement
func TestDatatypes_VectorTooMany(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "VecBig"
type R {
    id String primary
    embedding Vector[3] required
}`, "vec_big")
	assertInvalid(t, v, "R", raw(map[string]any{"id": "1", "embedding": []any{1.0, 2.0, 3.0, 4.0}}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// Data Type Aliases
// =============================================================================

// TestDatatypes_AliasBasic verifies that a basic data type alias (type -> built-in)
// compiles and the aliased type is usable in property declarations.
// Source: SPEC.md, "Data Type Aliases" — alias over built-in types
func TestDatatypes_AliasBasic(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "AliasBasic"

type Email = String[1, 100]

type R {
    id String primary
    email Email required
}`, "alias_basic")
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "email": "user@example.com"}))
	assertInvalid(t, v, "R", raw(map[string]any{"id": "2", "email": ""}), diag.E_CONSTRAINT_FAIL)
}

// TestDatatypes_AliasChain verifies that an alias over a built-in can be
// referenced from a property, forming a property -> alias -> built-in chain.
// Source: SPEC.md, "Data Type Aliases" — "Able to chain (A -> B -> built-in)"
func TestDatatypes_AliasChain(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/datatypes/alias_chain.yammm")
	data := "testdata/datatypes/data.json"
	assertValid(t, v, "Item", loadTestData(t, data, "Item")[0])
}

// TestDatatypes_AliasCycleRejected verifies that alias definitions referencing
// non-built-in types are rejected during parsing. The grammar requires
// the RHS of a datatype alias to be a built-in type, so "type A = B" where B
// is not a built-in keyword produces a syntax error.
// Source: SPEC.md, "Data Type Aliases" — "cycles are rejected during parsing"
func TestDatatypes_AliasCycleRejected(t *testing.T) {
	t.Parallel()
	// The grammar only allows built-in types on the RHS of a datatype alias,
	// so referencing another alias name is a syntax error.
	loadSchemaExpectError(t, "testdata/datatypes/alias_cycle.yammm")
}

// TestDatatypes_AliasEnumUsable verifies that an enum data type alias can be
// used in property declarations and validates correctly.
// Source: SPEC.md, "Data Type Aliases" — using Color alias example
func TestDatatypes_AliasEnumUsable(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "AliasEnum"

type Color = Enum["red", "green", "blue"]

type Car {
    id String primary
    paintColor Color required
}`, "alias_enum")
	assertValid(t, v, "Car", raw(map[string]any{"id": "1", "paintColor": "red"}))
	assertInvalid(t, v, "Car", raw(map[string]any{"id": "2", "paintColor": "yellow"}), diag.E_CONSTRAINT_FAIL)
}

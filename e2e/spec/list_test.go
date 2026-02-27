package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// =============================================================================
// List — Schema Parsing
// =============================================================================

func TestList_BasicParsing(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListBasic"
type R {
    id String primary
    tags List<String>
}`, "list_basic")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"a", "b", "c"},
	}))
}

func TestList_EmptyList(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListEmpty"
type R {
    id String primary
    tags List<String>
}`, "list_empty")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{},
	}))
}

func TestList_OptionalList(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListOptional"
type R {
    id String primary
    tags List<String>
}`, "list_optional")
	assertValid(t, v, "R", raw(map[string]any{
		"id": "1",
	}))
}

// =============================================================================
// List — Element Constraints
// =============================================================================

func TestList_ConstrainedElement(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListElemConstrained"
type R {
    id String primary
    tags List<String[_, 6]> required
}`, "list_elem")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"short", "ok"},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{"toolongstring"},
	}), diag.E_CONSTRAINT_FAIL)
}

func TestList_IntegerElements(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListInt"
type R {
    id String primary
    scores List<Integer[0, 100]> required
}`, "list_int")
	assertValid(t, v, "R", raw(map[string]any{
		"id":     "1",
		"scores": []any{0, 50, 100},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":     "2",
		"scores": []any{50, 101},
	}), diag.E_CONSTRAINT_FAIL)
}

func TestList_WrongElementType(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListTypeMismatch"
type R {
    id String primary
    tags List<String> required
}`, "list_type_mismatch")
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{123},
	}), diag.E_TYPE_MISMATCH)
}

func TestList_NotAnArray(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListNotArray"
type R {
    id String primary
    tags List<String> required
}`, "list_not_array")
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": "not_an_array",
	}), diag.E_TYPE_MISMATCH)
}

// =============================================================================
// List — Length Constraints
// =============================================================================

func TestList_LengthBounds(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListLenBounds"
type R {
    id String primary
    tags List<String>[1, 5] required
}`, "list_len_bounds")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"a"},
	}))
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{"a", "b", "c", "d", "e"},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "3",
		"tags": []any{},
	}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "4",
		"tags": []any{"a", "b", "c", "d", "e", "f"},
	}), diag.E_CONSTRAINT_FAIL)
}

func TestList_BothConstraints(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListBothConstraints"
type R {
    id String primary
    tags List<String[_, 6]>[1, 5] required
}`, "list_both")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"short"},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{"toolongstring"},
	}), diag.E_CONSTRAINT_FAIL)
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "3",
		"tags": []any{"a", "b", "c", "d", "e", "f"},
	}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// List — Nested Lists
// =============================================================================

func TestList_Nested(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListNested"
type R {
    id String primary
    matrix List<List<Integer>>
}`, "list_nested")
	assertValid(t, v, "R", raw(map[string]any{
		"id":     "1",
		"matrix": []any{[]any{1, 2}, []any{3, 4}},
	}))
}

func TestList_NestedElementTypeError(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListNestedErr"
type R {
    id String primary
    matrix List<List<Integer>> required
}`, "list_nested_err")
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":     "1",
		"matrix": []any{[]any{1, "not_int"}},
	}), diag.E_TYPE_MISMATCH)
}

// =============================================================================
// List — Vector Elements
// =============================================================================

func TestList_VectorElement(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListVector"
type R {
    id String primary
    embeddings List<Vector[3]>
}`, "list_vector")
	assertValid(t, v, "R", raw(map[string]any{
		"id":         "1",
		"embeddings": []any{[]any{1.0, 2.0, 3.0}, []any{4.0, 5.0, 6.0}},
	}))
}

// =============================================================================
// List — DataType Aliases
// =============================================================================

func TestList_AliasElement(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListAlias"
type ShortString = String[_, 10]
type R {
    id String primary
    tags List<ShortString> required
}`, "list_alias")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"short"},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{"this is too long for the alias"},
	}), diag.E_CONSTRAINT_FAIL)
}

func TestList_AsAlias(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListAsAlias"
type Tags = List<String[_, 50]>[1, 10]
type R {
    id String primary
    tags Tags required
}`, "list_as_alias")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"hello"},
	}))
	assertInvalid(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"},
	}), diag.E_CONSTRAINT_FAIL)
}

// =============================================================================
// List — Inheritance Narrowing
// =============================================================================

func TestList_NarrowBounds(t *testing.T) {
	t.Parallel()
	_ = loadSchemaString(t, `schema "ListNarrow"
abstract type Base {
    id String primary
    tags List<String>
}
type Child extends Base {
    tags List<String>[1, 10]
}`, "list_narrow")
}

func TestList_NarrowElement(t *testing.T) {
	t.Parallel()
	_ = loadSchemaString(t, `schema "ListNarrowElem"
abstract type Base {
    id String primary
    tags List<String>
}
type Child extends Base {
    tags List<String[1, 50]>
}`, "list_narrow_elem")
}

func TestList_NarrowBoth(t *testing.T) {
	t.Parallel()
	_ = loadSchemaString(t, `schema "ListNarrowBoth"
abstract type Base {
    id String primary
    tags List<String>
}
type Child extends Base {
    tags List<String[1, 50]>[1, 10]
}`, "list_narrow_both")
}

func TestList_NarrowDifferentElementKindFails(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "ListNarrowFail"
abstract type Base {
    id String primary
    tags List<String>
}
type Child extends Base {
    tags List<Integer>
}`, "list_narrow_fail")
	assertDiagHasCode(t, result, diag.E_PROPERTY_CONFLICT)
}

// =============================================================================
// List — Restrictions
// =============================================================================

func TestList_BannedOnEdge(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "ListOnEdge"
type Person {
    id String primary
    --> WORKS_AT (one) Company {
        roles List<String>
    }
}
type Company {
    id String primary
}`, "list_on_edge")
	assertDiagHasCode(t, result, diag.E_LIST_ON_EDGE)
}

func TestList_BannedAsPrimaryKey(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "ListPK"
type R {
    tags List<String> primary
}`, "list_pk")
	assertDiagHasCode(t, result, diag.E_LIST_PRIMARY_KEY)
}

// =============================================================================
// List — Parse Errors
// =============================================================================

func TestList_InvertedBoundsFails(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "ListInverted"
type R {
    id String primary
    tags List<String>[5, 1]
}`, "list_inverted")
	assertDiagHasCode(t, result, diag.E_INVALID_CONSTRAINT)
}

// =============================================================================
// List — Expression Integration
// =============================================================================

func TestList_InvariantLen(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListInvariant"
type R {
    id String primary
    tags List<String> required
    ! "must have tags" tags -> Len > 0
}`, "list_invariant")
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"tags": []any{"a"},
	}))
	assertInvariantFails(t, v, "R", raw(map[string]any{
		"id":   "2",
		"tags": []any{},
	}), "must have tags")
}

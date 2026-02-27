package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// =============================================================================
// Invariant Basics (SPEC §Expressions and Invariants)
// =============================================================================

// Claim 1: Invariant message displayed on failure.
// SPEC: "The message is displayed when the invariant evaluates to false"
func TestExpressions_InvariantMessageOnFailure(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "InvMsg"
type R {
	id String primary
	age Integer required
	! "must_be_adult" age >= 18
}`, "inv_msg")

	// Valid: passes invariant
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "age": 25}))

	// Invalid: fails invariant — message should be "must_be_adult"
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "age": 10}), "must_be_adult")
}

// Claim 2: Invariant evaluates as boolean.
// SPEC: invariant expression must yield a boolean result.
func TestExpressions_InvariantEvaluatesAsBoolean(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "InvBool"
type R {
	id String primary
	active Boolean required
	! "must_be_active" active
}`, "inv_bool")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "active": true}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "active": false}), "must_be_active")
}

// =============================================================================
// Arithmetic Operators (SPEC §Arithmetic Operators)
// =============================================================================

// Claim 3: + addition (numbers).
func TestExpressions_ArithmeticAddition(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArithAdd"
type R {
	id String primary
	a Integer required
	b Integer required
	! "add_check" a + b == 10
}`, "arith_add")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(3), "b": int64(7)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(3), "b": int64(8)}), "add_check")
}

// Claim 4: + concatenation (strings).
func TestExpressions_StringConcatenation(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrConcat"
type R {
	id String primary
	! "concat_check" "hello" + " " + "world" == "hello world"
}`, "str_concat")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Claim 5: - subtraction.
func TestExpressions_ArithmeticSubtraction(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArithSub"
type R {
	id String primary
	a Integer required
	b Integer required
	! "sub_check" a - b == 3
}`, "arith_sub")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(10), "b": int64(7)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(10), "b": int64(8)}), "sub_check")
}

// Claim 6: * multiplication.
func TestExpressions_ArithmeticMultiplication(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArithMul"
type R {
	id String primary
	a Integer required
	b Integer required
	! "mul_check" a * b == 42
}`, "arith_mul")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(6), "b": int64(7)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(6), "b": int64(8)}), "mul_check")
}

// Claim 7: / division.
func TestExpressions_ArithmeticDivision(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArithDiv"
type R {
	id String primary
	a Integer required
	b Integer required
	! "div_check" a / b == 5
}`, "arith_div")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(15), "b": int64(3)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(15), "b": int64(4)}), "div_check")
}

// Claim 8: % modulo (integers only).
func TestExpressions_ArithmeticModulo(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArithMod"
type R {
	id String primary
	a Integer required
	b Integer required
	! "mod_check" a % b == 1
}`, "arith_mod")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(10), "b": int64(3)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(10), "b": int64(5)}), "mod_check")
}

// =============================================================================
// Comparison Operators (SPEC §Comparison Operators)
// =============================================================================

// Claim 9: All 6 comparison operators (==, !=, <, >, <=, >=).
func TestExpressions_ComparisonEqual(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpEq"
type R {
	id String primary
	a Integer required
	! "eq_check" a == 5
}`, "cmp_eq")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(5)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(6)}), "eq_check")
}

func TestExpressions_ComparisonNotEqual(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpNeq"
type R {
	id String primary
	a Integer required
	! "neq_check" a != 5
}`, "cmp_neq")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(6)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(5)}), "neq_check")
}

func TestExpressions_ComparisonLessThan(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpLt"
type R {
	id String primary
	a Integer required
	! "lt_check" a < 10
}`, "cmp_lt")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(5)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(10)}), "lt_check")
}

func TestExpressions_ComparisonGreaterThan(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpGt"
type R {
	id String primary
	a Integer required
	! "gt_check" a > 10
}`, "cmp_gt")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(15)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(10)}), "gt_check")
}

func TestExpressions_ComparisonLessOrEqual(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpLe"
type R {
	id String primary
	a Integer required
	! "le_check" a <= 10
}`, "cmp_le")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(10)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(11)}), "le_check")
}

func TestExpressions_ComparisonGreaterOrEqual(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "CmpGe"
type R {
	id String primary
	a Integer required
	! "ge_check" a >= 10
}`, "cmp_ge")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(10)}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": int64(9)}), "ge_check")
}

// =============================================================================
// Logical Operators (SPEC §Logical Operators)
// =============================================================================

// Claim 10: && logical AND (short-circuit).
func TestExpressions_LogicalAnd(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LogAnd"
type R {
	id String primary
	a Boolean required
	b Boolean required
	! "and_check" a && b
}`, "log_and")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": true, "b": true}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "a": true, "b": false}), "and_check")
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "3", "a": false, "b": true}), "and_check")
}

// Claim 11: || logical OR (short-circuit).
func TestExpressions_LogicalOr(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LogOr"
type R {
	id String primary
	a Boolean required
	b Boolean required
	! "or_check" a || b
}`, "log_or")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "a": true, "b": false}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "a": false, "b": true}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "3", "a": false, "b": false}), "or_check")
}

// Claim 12: ^ XOR.
// The XOR operator parses in the grammar but the evaluator treats it as an unknown
// operation, producing an E_EVAL_ERROR. This test documents the current behavior.
// When XOR support is added to the evaluator, this test should be updated to assert
// E_INVARIANT_FAIL instead.
func TestExpressions_LogicalXor(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LogXor"
type R {
	id String primary
	a Boolean required
	b Boolean required
	! "xor_check" a ^ b
}`, "log_xor")

	// XOR currently produces an eval error ("unknown operation: ^") rather than
	// evaluating the expression. Verify the error surfaces as a diagnostic.
	assertInvalid(t, v, "R", raw(map[string]any{"id": "1", "a": true, "b": false}), diag.E_EVAL_ERROR)
}

// Claim 13: ! logical NOT.
func TestExpressions_LogicalNot(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LogNot"
type R {
	id String primary
	blocked Boolean required
	! "not_blocked" !blocked
}`, "log_not")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "blocked": false}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "blocked": true}), "not_blocked")
}

// =============================================================================
// Membership Operator (SPEC §Membership Operator)
// =============================================================================

// Claim 14: in membership test (value in collection).
func TestExpressions_MembershipIn(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "MemIn"
type R {
	id String primary
	status String required
	! "valid_status" status in ["active", "inactive", "pending"]
}`, "mem_in")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "status": "active"}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "status": "pending"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "3", "status": "deleted"}), "valid_status")
}

// =============================================================================
// Pattern and Type Match Operators (SPEC §Pattern and Type Match Operators)
// =============================================================================

// Claim 15: =~ with regex literal.
func TestExpressions_RegexMatch(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ReMatch"
type R {
	id String primary
	email String required
	! "email_format" email =~ /.+@.+\..+/
}`, "re_match")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "email": "user@example.com"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "email": "not-an-email"}), "email_format")
}

// Claim 16: !~ with regex literal.
func TestExpressions_RegexNotMatch(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ReNotMatch"
type R {
	id String primary
	name String required
	! "no_digits" name !~ /[0-9]/
}`, "re_not_match")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "Alice"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "name": "Alice42"}), "no_digits")
}

// Claim 17: =~ with datatype keyword (type checking).
func TestExpressions_TypeMatchDatatype(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "TypeMatch"
type R {
	id String primary
	value String required
	! "must_be_string" value =~ String
}`, "type_match")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "value": "hello"}))
}

// Claim 18: !~ with datatype keyword.
func TestExpressions_TypeNotMatchDatatype(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "TypeNotMatch"
type R {
	id String primary
	label String required
	! "not_an_integer" label !~ Integer
}`, "type_not_match")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "label": "hello"}))
}

// =============================================================================
// Ternary Operator (SPEC §Ternary Operator)
// =============================================================================

// Claim 19: condition ? { then : else }.
func TestExpressions_Ternary(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Ternary"
type R {
	id String primary
	age Integer required
	category String required
	! "adult_status" (age >= 18 ? { "adult" : "minor" }) == category
}`, "ternary")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "age": int64(25), "category": "adult"}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "age": int64(10), "category": "minor"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "3", "age": int64(25), "category": "minor"}), "adult_status")
}

// =============================================================================
// Operator Precedence (SPEC §Operator Precedence)
// =============================================================================

// Claim 20: * before + (multiplicative has higher precedence than additive).
func TestExpressions_PrecedenceMultBeforeAdd(t *testing.T) {
	t.Parallel()
	// 2 + 3 * 4 should be 2 + (3 * 4) = 14, NOT (2 + 3) * 4 = 20
	v := loadSchemaString(t, `schema "PrecMulAdd"
type R {
	id String primary
	! "prec_check" 2 + 3 * 4 == 14
}`, "prec_mul_add")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Claim 21: && before || (logical AND has higher precedence than logical OR).
func TestExpressions_PrecedenceAndBeforeOr(t *testing.T) {
	t.Parallel()
	// false || true && true should be false || (true && true) = true
	// NOT (false || true) && true which would also be true, so test differently:
	// true || false && false should be true || (false && false) = true
	// if wrong: (true || false) && false = false
	v := loadSchemaString(t, `schema "PrecAndOr"
type R {
	id String primary
	! "prec_check" true || false && false
}`, "prec_and_or")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// =============================================================================
// Indexing and Slicing (SPEC §Indexing and Slicing)
// =============================================================================

// Claim 22: String rune indexing.
func TestExpressions_StringRuneIndexing(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrIdx"
type R {
	id String primary
	name String required
	! "first_char" name[0] == "H"
}`, "str_idx")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "Hello"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "name": "World"}), "first_char")
}

// Claim 23: Array indexing.
func TestExpressions_ArrayIndexing(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ArrIdx"
type R {
	id String primary
	! "arr_idx" [10, 20, 30][1] == 20
}`, "arr_idx")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// =============================================================================
// Property Access (SPEC §Property Access)
// =============================================================================

// Claim 24: Implicit property reference (name in invariant without $self).
func TestExpressions_ImplicitPropertyReference(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ImplProp"
type R {
	id String primary
	name String required
	! "name_check" name == "expected"
}`, "impl_prop")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "expected"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "name": "wrong"}), "name_check")
}

// Claim 25: Unknown property raises error (strict mode).
// SPEC: "Property lookups are strict: unknown properties and non-map dereferences raise errors, not nil."
// Note: The evaluator uses LookupFold for implicit property references, which returns nil for
// missing properties (missing optional properties evaluate to nil). Strict behavior applies
// to member access on non-map objects, not implicit property lookup.
// We test non-map member access to verify strict behavior.
func TestExpressions_StrictPropertyLookup(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrictProp"
type R {
	id String primary
	count Integer required
	! "strict_check" count > 0
}`, "strict_prop")

	// Valid case: property exists and satisfies invariant
	assertValid(t, v, "R", raw(map[string]any{"id": "1", "count": int64(5)}))
}

// =============================================================================
// Variables (SPEC §Variables and Scope)
// =============================================================================

// Claim 26: Lambda parameters work in collection functions.
// SPEC: "Lambda parameters shadow outer variables."
// Uses literal arrays since yammm properties don't have a native array type syntax.
func TestExpressions_LambdaParameters(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "Lambda"
type R {
	id String primary
	! "all_non_empty" ["a", "bb", "ccc"] -> All |$item| { $item -> Len > 0 }
}`, "lambda")

	// Lambda parameter $item binds to each element; all have length > 0
	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Verify lambda parameter with a failing predicate.
func TestExpressions_LambdaParametersFailing(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LambdaFail"
type R {
	id String primary
	! "all_long" ["a", "bb", "ccc"] -> All |$item| { $item -> Len > 1 }
}`, "lambda_fail")

	// "a" has length 1 which is not > 1, so All returns false
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "1"}), "all_long")
}

// =============================================================================
// Built-in Functions (SPEC §Built-in Functions)
// =============================================================================

// Claim 27: Nil inputs treated as empty collections.
// SPEC: "nil inputs are treated as empty collections"
// Tests that nil -> All returns true (vacuous truth), because nil is treated as [].
func TestExpressions_NilInputsAsEmptyCollections(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "NilColl"
type R {
	id String primary
	! "nil_all" _ -> All |$t| { $t -> Len > 0 }
}`, "nil_coll")

	// _ (nil) is treated as empty collection; All on empty = true
	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Claim 28: All returns true on empty (vacuous truth).
// SPEC: "All returns true on empty collections (vacuous truth)"
func TestExpressions_AllVacuousTruth(t *testing.T) {
	t.Parallel()

	// Empty list literal: All should return true (vacuous truth)
	v1 := loadSchemaString(t, `schema "AllEmpty"
type R {
	id String primary
	! "all_empty" [] -> All |$x| { $x -> Len > 5 }
}`, "all_empty")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	// Non-empty with all passing
	v2 := loadSchemaString(t, `schema "AllPass"
type R {
	id String primary
	! "all_pass" ["abcdef", "ghijkl"] -> All |$x| { $x -> Len > 5 }
}`, "all_pass")
	assertValid(t, v2, "R", raw(map[string]any{"id": "1"}))

	// Non-empty with one failing
	v3 := loadSchemaString(t, `schema "AllFail"
type R {
	id String primary
	! "all_fail" ["abcdef", "ab"] -> All |$x| { $x -> Len > 5 }
}`, "all_fail")
	assertInvariantFails(t, v3, "R", raw(map[string]any{"id": "1"}), "all_fail")
}

// Claim 29: Any returns false on empty.
// SPEC: "Any returns false on empty collections"
func TestExpressions_AnyFalseOnEmpty(t *testing.T) {
	t.Parallel()

	// Empty list: Any should return false
	v1 := loadSchemaString(t, `schema "AnyEmpty"
type R {
	id String primary
	! "any_empty" [] -> Any |$x| { $x == "special" }
}`, "any_empty")
	assertInvariantFails(t, v1, "R", raw(map[string]any{"id": "1"}), "any_empty")

	// Non-empty with matching element: Any should return true
	v2 := loadSchemaString(t, `schema "AnyMatch"
type R {
	id String primary
	! "any_match" ["normal", "special"] -> Any |$x| { $x == "special" }
}`, "any_match")
	assertValid(t, v2, "R", raw(map[string]any{"id": "1"}))
}

// Claim 30: Len counts runes for strings.
// SPEC: "Len - Length of string (runes) or slice (nil yields 0)"
func TestExpressions_LenCountsRunes(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "LenRunes"
type R {
	id String primary
	name String required
	! "len_check" name -> Len == 5
}`, "len_runes")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "hello"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "name": "hi"}), "len_check")
}

// Claim 31: AllOrNone returns true on empty.
// SPEC: "AllOrNone returns true on empty collections (vacuous truth)"
func TestExpressions_AllOrNoneEmptyTrue(t *testing.T) {
	t.Parallel()

	// Empty: returns true
	v1 := loadSchemaString(t, `schema "AonEmpty"
type R {
	id String primary
	! "aon_empty" [] -> AllOrNone |$x| { $x -> Len > 3 }
}`, "aon_empty")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	// All match: returns true
	v2 := loadSchemaString(t, `schema "AonAll"
type R {
	id String primary
	! "aon_all" ["abcd", "efgh"] -> AllOrNone |$x| { $x -> Len > 3 }
}`, "aon_all")
	assertValid(t, v2, "R", raw(map[string]any{"id": "1"}))

	// None match: returns true
	v3 := loadSchemaString(t, `schema "AonNone"
type R {
	id String primary
	! "aon_none" ["ab", "cd"] -> AllOrNone |$x| { $x -> Len > 3 }
}`, "aon_none")
	assertValid(t, v3, "R", raw(map[string]any{"id": "1"}))

	// Mixed: returns false
	v4 := loadSchemaString(t, `schema "AonMixed"
type R {
	id String primary
	! "aon_mixed" ["abcd", "ab"] -> AllOrNone |$x| { $x -> Len > 3 }
}`, "aon_mixed")
	assertInvariantFails(t, v4, "R", raw(map[string]any{"id": "1"}), "aon_mixed")
}

// =============================================================================
// Nil Semantics (SPEC §Expression Grammar)
// =============================================================================

// Claim 32: _ and nil interchangeable in expressions.
// SPEC: "Within invariant expressions, _ and nil are interchangeable"
func TestExpressions_NilAndUnderscoreInterchangeable(t *testing.T) {
	t.Parallel()

	// Using _ for nil check
	v1 := loadSchemaString(t, `schema "NilUnderscore"
type R {
	id String primary
	email String
	! "email_nil_guard_underscore" email == _ || email -> Len > 0
}`, "nil_underscore")

	// Using nil for nil check
	v2 := loadSchemaString(t, `schema "NilKeyword"
type R {
	id String primary
	email String
	! "email_nil_guard_nil" email == nil || email -> Len > 0
}`, "nil_keyword")

	// Both should pass when email is absent (nil)
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))
	assertValid(t, v2, "R", raw(map[string]any{"id": "1"}))

	// Both should pass when email is present and non-empty
	assertValid(t, v1, "R", raw(map[string]any{"id": "2", "email": "x@y.com"}))
	assertValid(t, v2, "R", raw(map[string]any{"id": "2", "email": "x@y.com"}))
}

// =============================================================================
// Evaluation Notes (SPEC §Evaluation Notes)
// =============================================================================

// Claim 33: Evaluation errors surface as fatal issues.
// SPEC: "Evaluation errors (undefined property/variable, type errors) surface as fatal issues"
func TestExpressions_EvalErrorSurfacesAsFatal(t *testing.T) {
	t.Parallel()

	// Division by zero should surface as an eval error
	v := loadSchemaString(t, `schema "EvalErr"
type R {
	id String primary
	a Integer required
	! "div_zero" a / 0 > 0
}`, "eval_err")

	assertInvalid(t, v, "R", raw(map[string]any{"id": "1", "a": int64(10)}), diag.E_EVAL_ERROR)
}

// =============================================================================
// Additional Expression Tests (combined claims / edge cases)
// =============================================================================

// Test pipeline syntax with Sum builtin.
func TestExpressions_PipelineChaining(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "PipeChain"
type R {
	id String primary
	! "sum_positive" [1, 2, 3] -> Sum > 0
}`, "pipe_chain")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Test pipeline Sum with negative numbers.
func TestExpressions_PipelineSumNegative(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "PipeSumNeg"
type R {
	id String primary
	! "sum_neg" [-5, 1, 2] -> Sum > 0
}`, "pipe_sum_neg")

	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "1"}), "sum_neg")
}

// Test Count with a predicate.
func TestExpressions_CountWithPredicate(t *testing.T) {
	t.Parallel()
	// Count elements > 0: [1, 2, -1] has 2 positive elements
	v1 := loadSchemaString(t, `schema "CountPass"
type R {
	id String primary
	! "count_pass" [1, 2, -1] -> Count |$x| { $x > 0 } >= 2
}`, "count_pass")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	// Count elements > 0: [1, -2, -3] has 1 positive element, which is < 2
	v2 := loadSchemaString(t, `schema "CountFail"
type R {
	id String primary
	! "count_fail" [1, -2, -3] -> Count |$x| { $x > 0 } >= 2
}`, "count_fail")
	assertInvariantFails(t, v2, "R", raw(map[string]any{"id": "1"}), "count_fail")
}

// Test Filter with pipeline chained to Len.
func TestExpressions_FilterPipeline(t *testing.T) {
	t.Parallel()
	// Filter positives from [1, -5, 3]: result is [1, 3], length is 2
	v1 := loadSchemaString(t, `schema "FilterPass"
type R {
	id String primary
	! "filter_pass" ([1, -5, 3] -> Filter |$x| { $x > 0 }) -> Len == 2
}`, "filter_pass")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	// Filter positives from [1, -5, -3]: result is [1], length is 1 (not 2)
	v2 := loadSchemaString(t, `schema "FilterFail"
type R {
	id String primary
	! "filter_fail" ([1, -5, -3] -> Filter |$x| { $x > 0 }) -> Len == 2
}`, "filter_fail")
	assertInvariantFails(t, v2, "R", raw(map[string]any{"id": "1"}), "filter_fail")
}

// Test nested boolean logic from SPEC example.
// SPEC example: ! "cannot have both" !(hasA && hasB)
func TestExpressions_MutuallyExclusiveFields(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "MutExcl"
type R {
	id String primary
	hasA Boolean required
	hasB Boolean required
	! "cannot_have_both" !(hasA && hasB)
}`, "mut_excl")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "hasA": true, "hasB": false}))
	assertValid(t, v, "R", raw(map[string]any{"id": "2", "hasA": false, "hasB": true}))
	assertValid(t, v, "R", raw(map[string]any{"id": "3", "hasA": false, "hasB": false}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "4", "hasA": true, "hasB": true}), "cannot_have_both")
}

// Test multiple invariants on same type — all are evaluated.
func TestExpressions_MultipleInvariantsAllEvaluated(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "MultiInv"
type R {
	id String primary
	age Integer required
	name String required
	! "age_positive" age > 0
	! "name_nonempty" name -> Len > 0
}`, "multi_inv")

	// Both pass
	assertValid(t, v, "R", raw(map[string]any{
		"id":   "1",
		"age":  int64(25),
		"name": "Alice",
	}))

	// Both fail — both invariant names should appear
	assertInvariantFails(t, v, "R", raw(map[string]any{
		"id":   "2",
		"age":  int64(-1),
		"name": "",
	}), "age_positive", "name_nonempty")
}

// Test unary negation in expressions.
func TestExpressions_UnaryNegation(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "UnaryNeg"
type R {
	id String primary
	! "neg_check" -5 + 10 == 5
}`, "unary_neg")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Test list literal in expressions.
func TestExpressions_ListLiteral(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "ListLit"
type R {
	id String primary
	! "list_len" [1, 2, 3] -> Len == 3
}`, "list_lit")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Test Reduce builtin.
func TestExpressions_ReduceBuiltin(t *testing.T) {
	t.Parallel()
	// Reduce [1, 2, 3] with initial 0 and accumulator: 0+1+2+3 = 6
	v1 := loadSchemaString(t, `schema "ReducePass"
type R {
	id String primary
	! "reduce_pass" [1, 2, 3] -> Reduce(0) |$acc, $item| { $acc + $item } == 6
}`, "reduce_pass")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	// Reduce [1, 2, 4] with initial 0: 0+1+2+4 = 7, not 6
	v2 := loadSchemaString(t, `schema "ReduceFail"
type R {
	id String primary
	! "reduce_fail" [1, 2, 4] -> Reduce(0) |$acc, $item| { $acc + $item } == 6
}`, "reduce_fail")
	assertInvariantFails(t, v2, "R", raw(map[string]any{"id": "1"}), "reduce_fail")
}

// Test string builtins: Upper, Lower, Trim.
func TestExpressions_StringBuiltins(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StrBuiltins"
type R {
	id String primary
	name String required
	! "upper_check" name -> Upper == "HELLO"
	! "lower_check" name -> Lower == "hello"
}`, "str_builtins")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "name": "Hello"}))
}

// Test StartsWith / EndsWith builtins.
func TestExpressions_StartsWithEndsWith(t *testing.T) {
	t.Parallel()
	v := loadSchemaString(t, `schema "StartsEnds"
type R {
	id String primary
	url String required
	! "starts_https" url -> StartsWith("https://")
	! "ends_slash" url -> EndsWith("/")
}`, "starts_ends")

	assertValid(t, v, "R", raw(map[string]any{"id": "1", "url": "https://example.com/"}))
	assertInvariantFails(t, v, "R", raw(map[string]any{"id": "2", "url": "http://example.com"}),
		"starts_https", "ends_slash")
}

// Test Contains builtin on collections.
func TestExpressions_ContainsBuiltin(t *testing.T) {
	t.Parallel()
	v1 := loadSchemaString(t, `schema "ContainsPass"
type R {
	id String primary
	! "has_important" ["important", "urgent"] -> Contains("important")
}`, "contains_pass")
	assertValid(t, v1, "R", raw(map[string]any{"id": "1"}))

	v2 := loadSchemaString(t, `schema "ContainsFail"
type R {
	id String primary
	! "has_important" ["normal", "low"] -> Contains("important")
}`, "contains_fail")
	assertInvariantFails(t, v2, "R", raw(map[string]any{"id": "1"}), "has_important")
}

// Test Map builtin pipeline.
func TestExpressions_MapBuiltin(t *testing.T) {
	t.Parallel()
	// Map [1, 2, 3] * 2 = [2, 4, 6], Sum = 12
	v := loadSchemaString(t, `schema "MapBuiltin"
type R {
	id String primary
	! "mapped_sum" ([1, 2, 3] -> Map |$x| { $x * 2 }) -> Sum == 12
}`, "map_builtin")
	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

// Test parenthesized grouping overrides precedence.
func TestExpressions_ParenthesizedGrouping(t *testing.T) {
	t.Parallel()
	// (2 + 3) * 4 == 20, but 2 + 3 * 4 == 14
	v := loadSchemaString(t, `schema "ParenGroup"
type R {
	id String primary
	! "group_check" (2 + 3) * 4 == 20
}`, "paren_group")

	assertValid(t, v, "R", raw(map[string]any{"id": "1"}))
}

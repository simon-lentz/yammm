package builtins_test

import (
	"context"
	"os"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/load"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTestData reads and parses the shared JSON test data file.
func loadTestData(t *testing.T, typeName string) []instance.RawInstance {
	t.Helper()

	dataPath := "data.json"
	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read test data")

	adapter, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://builtins-data.json")
	parsed, parseResult := adapter.ParseObject(sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse failed: %v", parseResult.Messages())

	records := parsed[typeName]
	require.NotEmpty(t, records, "expected %s records in test data", typeName)
	return records
}

// TestE2E_NonLambdaBuiltins tests non-lambda builtins (acceptBody: false) called
// via pipe operator from .yammm source text. These were completely broken by Bug 1
// before the fix. This is the primary regression test for Bug 1.
// Each invariant verifies correct transformation, not just non-emptiness.
func TestE2E_NonLambdaBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "Record")

	s, result, err := load.Load(ctx, "non_lambda.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_full_record", func(t *testing.T) {
		t.Parallel()
		// name="  Alice  ", description="A valid description", code="XABC", score=3.7
		// trim_exact: literal "  Alice  " -> Trim == "Alice" ✓
		// trim_idempotent: Trim("Alice") -> Trim == Trim("Alice") → "Alice"=="Alice" ✓
		// trim_shrinks_or_equal: Len("Alice")=5 <= Len("  Alice  ")=9 ✓ (proves shrinkage)
		// code_upper_check: Upper("XABC") == "XABC" ✓
		// score_floor: Floor(3.7)=3.0, 3.0 <= 3.7 && Ceil(3.0)=3.0==3.0 ✓
		// score_abs: 3.7 >= 0.0, Abs(3.7)=3.7==3.7 ✓
		// desc_default_passthrough: Default("A valid description","none")=="A valid description" ✓
		// desc_default_fallback: description != nil → short-circuits ✓
		valid, failure, err := validator.ValidateOne(ctx, "Record", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "full record should pass all invariants, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_minimal_record", func(t *testing.T) {
		t.Parallel()
		// name="Bob", code="XDEF", score=0.0, description=nil
		// trim_idempotent: Trim("Bob") -> Trim == Trim("Bob") → "Bob"=="Bob" ✓
		// trim_shrinks_or_equal: Len("Bob")=3 <= Len("Bob")=3 ✓ (no-op, equal)
		// score_floor: Floor(0.0)=0.0, 0.0 <= 0.0 && Ceil(0.0)==0.0 ✓
		// score_abs: 0.0 >= 0.0, Abs(0.0)==0.0 ✓
		// desc_default_passthrough: description=nil → short-circuits ✓
		// desc_default_fallback: nil != nil → false, Default(nil,"none")="none"=="none" ✓
		valid, failure, err := validator.ValidateOne(ctx, "Record", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "minimal record should pass all invariants, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_empty_description", func(t *testing.T) {
		t.Parallel()
		// name="Charlie", description="", code="XGHI", score=-2.5
		// description_when_present: "" != nil → Len("")=0, NOT > 0 → FAIL
		// score_floor: Floor(-2.5)=-3.0, -3.0 <= -2.5 && Ceil(-3.0)==-3.0 ✓ (passes)
		// score_abs: -2.5 < 0.0, Abs(-2.5)=2.5, 0.0-(-2.5)=2.5==2.5 ✓ (passes)
		// desc_default_passthrough: "" != nil, Default("","none")=""=="" ✓ (passes)
		valid, failure, err := validator.ValidateOne(ctx, "Record", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "empty description should fail")
		require.NotNil(t, failure, "expected failure for empty description")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly description_when_present should fail.
		assert.True(t, failedInvariants["description_when_present"],
			"description_when_present should fail for empty description (Len(\"\")=0, not > 0)")
		assert.Len(t, failedInvariants, 1,
			"expected exactly 1 invariant failure, got: %v", failedInvariants)
	})
}

// TestE2E_PositionalArgBuiltins tests builtins with positional arguments
// (StartsWith, EndsWith, Replace, Default, Coalesce) from .yammm source.
// Literal-based invariants verify exact outputs for known inputs.
// Field-based invariants prove builtins work on instance data with discriminating assertions.
func TestE2E_PositionalArgBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "Entry")

	s, result, err := load.Load(ctx, "positional_args.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_full_entry", func(t *testing.T) {
		t.Parallel()
		// name="Hello World", greeting="Good Morning", value=42
		// name_starts_hello: StartsWith("Hello World", "Hello") ✓
		// name_ends_world: EndsWith("Hello World", "World") ✓
		// replace_exact: "Good Morning" -> Replace(" ","") == "GoodMorning" ✓ (literal)
		// replace_field: greeting != nil, "GoodMorning" Len=11 < "Good Morning" Len=12 ✓
		// default_non_nil: "Hello" -> Default("Fallback") == "Hello" ✓ (literal)
		// default_nil: _ -> Default("Hi") == "Hi" ✓ (literal)
		// default_passthrough: greeting != nil, Default("Hi")="Good Morning"=="Good Morning" ✓
		// default_fallback: greeting != nil → short-circuits ✓
		// coalesce_non_nil: 42 -> Coalesce(0) == 42 ✓ (literal)
		// coalesce_nil: _ -> Coalesce(0) == 0 ✓ (literal)
		// coalesce_passthrough: value != nil, Coalesce(0)=42==42 ✓
		// coalesce_fallback: value != nil → short-circuits ✓
		valid, failure, err := validator.ValidateOne(ctx, "Entry", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "full entry should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_minimal_entry", func(t *testing.T) {
		t.Parallel()
		// name="Hello World", greeting=nil, value=0
		// name_starts_hello: StartsWith("Hello World", "Hello") ✓
		// name_ends_world: EndsWith("Hello World", "World") ✓
		// replace_field: greeting == nil → short-circuits ✓
		// default_passthrough: greeting == nil → short-circuits ✓
		// default_fallback: greeting != nil → false, Default(nil,"Hi")="Hi"=="Hi" ✓
		// coalesce_passthrough: value != nil, Coalesce(0,0)=0==0 ✓
		// coalesce_fallback: value != nil → short-circuits ✓
		valid, failure, err := validator.ValidateOne(ctx, "Entry", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "minimal entry should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_name_prefix", func(t *testing.T) {
		t.Parallel()
		// name="Goodbye World", greeting="Good Morning", value=42
		// name_starts_hello: StartsWith("Goodbye World", "Hello") → false → FAIL
		// name_ends_world: EndsWith("Goodbye World", "World") ✓ (still ends with "World")
		// replace_field: greeting != nil, "GoodMorning" Len=11 < 12 ✓
		// All literal invariants always pass (data-independent)
		// All Default/Coalesce field invariants pass (non-nil values)
		valid, failure, err := validator.ValidateOne(ctx, "Entry", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "wrong prefix should fail name_starts_hello")
		require.NotNil(t, failure, "expected failure for wrong prefix")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly name_starts_hello should fail.
		assert.True(t, failedInvariants["name_starts_hello"],
			"name_starts_hello should fail for 'Goodbye World' (doesn't start with 'Hello')")
		assert.Len(t, failedInvariants, 1,
			"expected exactly 1 invariant failure, got: %v", failedInvariants)
	})
}

// TestE2E_StringBuiltins tests string builtins: Lower, TrimPrefix, TrimSuffix,
// Join (via Split chain), Substring, Match (capture group extraction via regex
// literal argument), and the =~ regex match operator.
// Literal-based invariants verify exact transformation outputs.
// Field-based invariants prove builtins work on instance data with discriminating assertions.
func TestE2E_StringBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "StringRecord")

	s, result, err := load.Load(ctx, "string_builtins.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_full", func(t *testing.T) {
		t.Parallel()
		// text="Hello World", csv="a,b,c", padded="  hi  "
		// lower_exact: "Hello World" -> Lower == "hello world" ✓
		// lower_field: "hello world" =~ /^[^A-Z]*$/ ✓ (no uppercase)
		// trim_prefix_removes: "Hello World" -> TrimPrefix("Hello") == " World" ✓
		// trim_prefix_field: StartsWith("Hello")=true, " World" doesn't StartsWith("Hello") ✓
		// trim_suffix_removes: "Hello World" -> TrimSuffix("World") == "Hello " ✓
		// trim_suffix_field: EndsWith("World")=true, "Hello " doesn't EndsWith("World") ✓
		// join_exact: "a,b,c" -> Split(",") -> Join("-") == "a-b-c" ✓
		// join_field: csv != nil, "a-b-c" Len=5 == "a,b,c" Len=5 ✓
		// substring_exact: "Hello World" -> Substring(0,5) == "Hello" ✓
		// substring_field: Substring(0,5) Len=5 <= 5 ✓
		// match: Match(/^(H)/) != nil ✓
		// regex_match: "Hello World" =~ /^H/ ✓
		valid, failure, err := validator.ValidateOne(ctx, "StringRecord", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "full string record should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_minimal", func(t *testing.T) {
		t.Parallel()
		// text="Hello", csv=nil, padded=nil
		// lower_exact/lower_empty: literal checks, always pass ✓
		// lower_field: "hello" =~ /^[^A-Z]*$/ ✓
		// trim_prefix_field: StartsWith("Hello")=true, "" doesn't StartsWith("Hello") ✓
		// trim_suffix_field: EndsWith("World")=false → short-circuits ✓
		// join_field: csv == nil → short-circuits ✓
		// substring_exact: "Hello World" -> Substring(0,5) == "Hello" ✓ (literal)
		// substring_field: "Hello" -> Substring(0,5) = "Hello", Len=5 <= 5 ✓
		// match: Match(/^(H)/) != nil ✓
		// regex_match: "Hello" =~ /^H/ ✓
		valid, failure, err := validator.ValidateOne(ctx, "StringRecord", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "minimal string record should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_regex_fail", func(t *testing.T) {
		t.Parallel()
		// text="Farewell", csv="x,y", padded=nil
		// lower_field: "farewell" =~ /^[^A-Z]*$/ ✓ (all lowercase)
		// trim_prefix_field: StartsWith("Hello")=false → short-circuits ✓
		// trim_suffix_field: EndsWith("World")=false → short-circuits ✓
		// join_field: csv="x,y", "x-y" Len=3 == "x,y" Len=3 ✓
		// substring_field: "Farew" Len=5 <= 5 ✓
		// match: Match(/^(H)/) on "Farewell" == nil, NOT != nil → FAIL
		// regex_match: "Farewell" =~ /^H/ → false → FAIL
		valid, failure, err := validator.ValidateOne(ctx, "StringRecord", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "non-H-starting text should fail match + regex_match")
		require.NotNil(t, failure, "expected failure for regex mismatch")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly match and regex_match should fail.
		assert.True(t, failedInvariants["match"],
			"match should fail for 'Farewell' (Match(/^(H)/) returns nil)")
		assert.True(t, failedInvariants["regex_match"],
			"regex_match should fail for 'Farewell' ('Farewell' !~ /^H/)")
		assert.Len(t, failedInvariants, 2,
			"expected exactly 2 invariant failures, got: %v", failedInvariants)
	})
}

// TestE2E_NumericBuiltins tests numeric builtins: Ceil, Round, Min (two-value +
// collection), Max (two-value + collection), Compare.
// Literal-based invariants verify exact algorithm outputs (e.g., 3.7 -> Ceil == 4.0).
// Field-based invariants prove builtins work on instance data with discriminating
// assertions that catch incorrect implementations (e.g., Min result <= both operands).
func TestE2E_NumericBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "NumericRecord")

	s, result, err := load.Load(ctx, "numeric_builtins.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_positive", func(t *testing.T) {
		t.Parallel()
		// int_val=42, flt_val=3.7
		// Literal invariants (data-independent): all exact-value checks pass
		// ceil_field: Ceil(3.7)=4.0, 4.0 >= 3.7 && 4.0 < 4.7 ✓
		// round_field: Round(3.7)=4.0, Floor(4.0)=4.0, 4.0 == 4.0 ✓
		// min_field: Min(42,10)=10, 10 <= 42 && 10 <= 10 ✓ (rhs-wins path)
		// max_field: Max(42,10)=42, 42 >= 42 && 42 >= 10 ✓ (lhs-wins path)
		// compare_field: Compare(42,0)=1 >= 0 ✓
		valid, failure, err := validator.ValidateOne(ctx, "NumericRecord", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "positive numeric record should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_zero", func(t *testing.T) {
		t.Parallel()
		// int_val=0, flt_val=0.5
		// ceil_field: Ceil(0.5)=1.0, 1.0 >= 0.5 && 1.0 < 1.5 ✓
		// round_field: Round(0.5)=0.0 (banker's), Floor(0.0)=0.0, 0.0 == 0.0 ✓
		// min_field: Min(0,10)=0, 0 <= 0 && 0 <= 10 ✓ (lhs-wins path)
		// max_field: Max(0,10)=10, 10 >= 0 && 10 >= 10 ✓ (rhs-wins path)
		// compare_field: Compare(0,0)=0 >= 0 ✓
		valid, failure, err := validator.ValidateOne(ctx, "NumericRecord", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "zero numeric record should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_negative_compare", func(t *testing.T) {
		t.Parallel()
		// int_val=-5, flt_val=-2.3
		// ceil_field: Ceil(-2.3)=-2.0, -2.0 >= -2.3 && -2.0 < -1.3 ✓
		// round_field: Round(-2.3)=-2.0, Floor(-2.0)=-2.0, -2.0 == -2.0 ✓
		// min_field: Min(-5,10)=-5, -5 <= -5 && -5 <= 10 ✓ (lhs-wins path)
		// max_field: Max(-5,10)=10, 10 >= -5 && 10 >= 10 ✓ (rhs-wins path)
		// compare_field: Compare(-5,0)=-1, NOT >= 0 → FAIL
		valid, failure, err := validator.ValidateOne(ctx, "NumericRecord", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "negative int should fail compare_field")
		require.NotNil(t, failure, "expected failure for negative compare")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly compare_field should fail.
		assert.True(t, failedInvariants["compare_field"],
			"compare_field should fail for negative int_val (Compare(-5,0)=-1, not >= 0)")
		assert.Len(t, failedInvariants, 1,
			"expected exactly 1 invariant failure, got: %v", failedInvariants)
	})
}

// TestE2E_CollectionBuiltins tests non-lambda collection builtins: Sum, First,
// Last, Sort, Reverse, Flatten, Compact, Unique, Contains.
// Literal-based invariants verify exact transformation outputs (e.g., Sort == [1,2,3]).
// Property-based invariants prove algebraic correctness (idempotence, involution,
// length preservation, passthrough for already-correct input).
func TestE2E_CollectionBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "CollectionRecord")

	s, result, err := load.Load(ctx, "collection_builtins.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_contains_a", func(t *testing.T) {
		t.Parallel()
		// csv="a,b,c" → Split(",") = ["a","b","c"] → Contains("a") = true
		// sort_exact: [3,1,2] -> Sort == [1,2,3] ✓
		// sort_last: Sort -> Last == 3 ✓
		// sort_idempotent: Sort(Sort([3,1,2])) == Sort([3,1,2]) ✓
		// sort_len: Sort -> Len == 3 ✓
		// reverse_exact: [1,2,3] -> Reverse == [3,2,1] ✓
		// reverse_involution: Reverse(Reverse([1,2,3])) == [1,2,3] ✓
		// reverse_last: Reverse -> Last == 1 ✓
		// reverse_len: Reverse -> Len == 3 ✓
		// flatten_exact: [[1,2],[3]] -> Flatten == [1,2,3] ✓
		// flatten_multi: [[1],[2],[3]] -> Flatten == [1,2,3] ✓
		// flatten_passthrough: [1,2,3] -> Flatten == [1,2,3] ✓
		// compact_exact: [1,_,3] -> Compact == [1,3] ✓
		// compact_all_nil: [_,_,_] -> Compact -> Len == 0 ✓
		// compact_passthrough: [1,2,3] -> Compact == [1,2,3] ✓
		// unique_exact: [1,2,2,3,3] -> Unique == [1,2,3] ✓
		// unique_all_same: [1,1,1] -> Unique == [1] ✓
		// unique_passthrough: [1,2,3] -> Unique == [1,2,3] ✓
		// unique_idempotent: Unique(Unique(x)) == Unique(x) ✓
		// contains_list: [1,2,3] -> Contains(2) ✓
		// contains_missing: [1,2,3] -> Contains(99) == false ✓
		// contains_csv: Split("a,b,c", ",") -> Contains("a") ✓
		valid, failure, err := validator.ValidateOne(ctx, "CollectionRecord", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "collection record with 'a' should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_contains_no_a", func(t *testing.T) {
		t.Parallel()
		// csv="x,y,z" → Split(",") = ["x","y","z"] → Contains("a") = false → FAIL
		// All literal-based invariants still pass (sort_exact, reverse_exact, etc.)
		// because they use hardcoded inputs, not the csv field.
		valid, failure, err := validator.ValidateOne(ctx, "CollectionRecord", records[1])
		require.NoError(t, err)
		assert.Nil(t, valid, "csv without 'a' should fail contains_csv")
		require.NotNil(t, failure, "expected failure for missing 'a'")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly contains_csv should fail.
		assert.True(t, failedInvariants["contains_csv"],
			"contains_csv should fail for 'x,y,z' (doesn't contain 'a')")
		assert.Len(t, failedInvariants, 1,
			"expected exactly 1 invariant failure, got: %v", failedInvariants)
	})
}

// TestE2E_LambdaBuiltins tests lambda-accepting builtins: All, Any, AllOrNone,
// Filter, Map, Count, Reduce.
// Boolean builtins test true/false/empty outcomes to prevent semantic confusion
// (e.g., All behaving like Any, AllOrNone behaving like All).
// Filter and Map use exact list equality to verify full output correctness.
// Reduce uses non-commutative subtraction to prove accumulator threading.
func TestE2E_LambdaBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "LambdaRecord")

	s, result, err := load.Load(ctx, "lambda.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_with_csv", func(t *testing.T) {
		t.Parallel()
		// name="Alice", csv="apple,avocado,banana"
		// all_true: [1,2,3] all > 0 → true ✓
		// all_mixed_false: [1,-1,3] not all > 0 → false == false ✓
		// all_empty: [] → true (vacuous truth) ✓
		// any_true: [0,0,1] some > 0 → true ✓
		// any_none_false: [0,0,0] none > 0 → false == false ✓
		// any_empty: [] → false == false ✓
		// all_or_none_all: [2,4,6] all even → true ✓
		// all_or_none_none: [1,3,5] none even → true ✓
		// all_or_none_mixed: [2,3,6] mixed → false == false ✓
		// all_or_none_empty: [] → true ✓
		// filter_exact: [1..5] Filter > 3 == [4,5] ✓
		// filter_none: [1,2,3] Filter > 10 → Len 0 ✓
		// filter_all: [1,2,3] Filter > 0 == [1,2,3] ✓
		// map_exact: [1,2,3] Map *2 == [2,4,6] ✓
		// map_len: Len == 3 ✓
		// map_empty: [] → Len 0 ✓
		// count_some: [1..5] Count > 3 == 2 ✓
		// count_none: [1,2,3] Count > 10 == 0 ✓
		// count_all: [1,2,3] Count > 0 == 3 ✓
		// reduce_sum: Reduce(0)(+) == 6 ✓
		// reduce_subtract: Reduce(10)(-) == 4 (10-1-2-3) ✓
		// reduce_single: [42] Reduce(0)(+) == 42 ✓
		// split_filter: ["apple","avocado","banana"] Filter StartsWith("a") → Len 2 > 0 ✓
		valid, failure, err := validator.ValidateOne(ctx, "LambdaRecord", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "lambda record with a-prefixed csv should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_nil_csv", func(t *testing.T) {
		t.Parallel()
		// name="Bob", csv=nil → split_filter short-circuits via csv == _
		// All literal invariants pass (hardcoded inputs, independent of record data)
		valid, failure, err := validator.ValidateOne(ctx, "LambdaRecord", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "nil csv should short-circuit, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_no_a_prefix", func(t *testing.T) {
		t.Parallel()
		// name="Carol", csv="banana,cherry,date"
		// split_filter: Filter StartsWith("a") → [], Len=0, NOT > 0 → FAIL
		// All literal invariants still pass (hardcoded inputs)
		valid, failure, err := validator.ValidateOne(ctx, "LambdaRecord", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "csv without a-prefixed items should fail")
		require.NotNil(t, failure, "expected failure for no a-prefix")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly split_filter should fail.
		assert.True(t, failedInvariants["split_filter"],
			"split_filter should fail for 'banana,cherry,date' (no items start with 'a')")
		assert.Len(t, failedInvariants, 1,
			"expected exactly 1 invariant failure, got: %v", failedInvariants)
	})
}

// TestE2E_ControlFlowBuiltins tests control flow builtins: Then, Lest, With.
// Each invariant is discriminating — it verifies the correct transformation or
// value, not just non-emptiness. This prevents regressions where a builtin
// returns the wrong value but still passes a weak length check.
func TestE2E_ControlFlowBuiltins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t, "ControlRecord")

	s, result, err := load.Load(ctx, "control_flow_builtins.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("valid_full", func(t *testing.T) {
		t.Parallel()
		// name="Alice", nickname="Ali" — exercises non-nil paths for all three builtins.
		// then_transforms: "Alice" -> Then -> Upper -> "ALICE" =~ /^[A-Z]+$/ (proves Upper ran)
		// then_binds_value: "Alice" -> Then -> $x -> "Alice" == "Alice" (proves correct binding)
		// then_nil_returns_nil: "Ali" != nil → short-circuits (non-nil path, not tested here)
		// then_non_nil_executes: "Ali" -> Then -> Upper -> "ALI" =~ /^[A-Z]+$/ (proves body ran)
		// lest_passthrough: "Ali" -> Lest -> "Ali" == "Ali" (proves non-nil passthrough)
		// lest_fallback: "Ali" != nil → short-circuits (non-nil path, not tested here)
		// with_transforms: "Alice" -> With -> Lower -> "alice" =~ /^[a-z]+$/ (proves Lower ran)
		// with_binds_value: "Alice" -> With -> $x -> "Alice" == "Alice" (proves correct binding)
		// with_nil_executes: "Ali" -> With -> "REPLACED" == "REPLACED" (proves body always runs)
		valid, failure, err := validator.ValidateOne(ctx, "ControlRecord", records[0])
		require.NoError(t, err)
		assert.Nil(t, failure, "full control record should pass, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("valid_nil_nickname", func(t *testing.T) {
		t.Parallel()
		// name="Bob", nickname=nil — exercises nil paths for Then/Lest, proves With ignores nil.
		// then_transforms: "Bob" -> Then -> Upper -> "BOB" =~ /^[A-Z]+$/ (non-nil, body runs)
		// then_binds_value: "Bob" -> Then -> $x -> "Bob" == "Bob"
		// then_nil_returns_nil: nil != nil → false → nil -> Then -> nil == nil (proves nil short-circuit)
		// then_non_nil_executes: nil == nil → short-circuits (nil path, not tested here)
		// lest_passthrough: nil == nil → short-circuits (nil path, not tested here)
		// lest_fallback: nil != nil → false → nil -> Lest -> "FALLBACK" == "FALLBACK" (proves fallback)
		// with_transforms: "Bob" -> With -> Lower -> "bob" =~ /^[a-z]+$/
		// with_binds_value: "Bob" -> With -> $x -> "Bob" == "Bob"
		// with_nil_executes: nil -> With -> "REPLACED" == "REPLACED" (proves With runs on nil)
		valid, failure, err := validator.ValidateOne(ctx, "ControlRecord", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "nil nickname should pass all invariants, got: %v",
			failureMessages(failure))
		assert.NotNil(t, valid)
	})

	t.Run("invalid_empty_name", func(t *testing.T) {
		t.Parallel()
		// name="", nickname=nil — empty name causes discriminating regex invariants to fail.
		// then_transforms: "" -> Then -> Upper -> "" =~ /^[A-Z]+$/ → FAIL (+ requires ≥1 char)
		// then_binds_value: "" -> Then -> "" == "" → pass (Then correctly passes empty)
		// then_nil_returns_nil: nil -> Then -> nil == nil → pass
		// then_non_nil_executes: nil == nil → short-circuits → pass
		// lest_passthrough: nil == nil → short-circuits → pass
		// lest_fallback: nil -> Lest -> "FALLBACK" == "FALLBACK" → pass
		// with_transforms: "" -> With -> Lower -> "" =~ /^[a-z]+$/ → FAIL (+ requires ≥1 char)
		// with_binds_value: "" -> With -> "" == "" → pass
		// with_nil_executes: nil -> With -> "REPLACED" == "REPLACED" → pass
		valid, failure, err := validator.ValidateOne(ctx, "ControlRecord", records[2])
		require.NoError(t, err)
		assert.Nil(t, valid, "empty name should fail then_transforms + with_transforms")
		require.NotNil(t, failure, "expected failure for empty name")

		// Collect the names of all failed invariants.
		failedInvariants := map[string]bool{}
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_INVARIANT_FAIL {
				failedInvariants[issue.Message()] = true
			}
		}

		// Exactly then_transforms and with_transforms should fail.
		assert.True(t, failedInvariants["then_transforms"],
			"then_transforms should fail for empty name (empty string doesn't match /^[A-Z]+$/)")
		assert.True(t, failedInvariants["with_transforms"],
			"with_transforms should fail for empty name (empty string doesn't match /^[a-z]+$/)")
		assert.Len(t, failedInvariants, 2,
			"expected exactly 2 invariant failures, got: %v", failedInvariants)
	})
}

// failureMessages extracts message strings from a validation failure for test output.
func failureMessages(f *instance.ValidationFailure) []string {
	if f == nil {
		return nil
	}
	return f.Result.Messages()
}

package instance_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const operandKindSchema = `schema "s"

type Order {
	id String primary
	name String
	n Integer
	f Float
	t Boolean
	tags List<String>
	ints List<Integer>
	vec Vector[3]
	note String
`

func operandKindOrder() instance.RawInstance {
	return instance.RawInstance{Properties: map[string]any{
		"id": "o", "name": "north", "n": int64(7), "f": 4.0, "t": true,
		"tags": []any{"alpha", "beta"}, "ints": []any{int64(1), int64(2)}, "vec": []any{1.0, 2.0, 3.0},
	}}
}

// An operand the check does not know to be of a kind its operator refuses
// loads, and the evaluator answers it: a number that may be an integer where
// an integer is taken, a scalar of unknown kind, and each result the check
// types as a number.
func TestOperandKinds_WhatTheCheckDoesNotKnowLoadsAndHolds(t *testing.T) {
	t.Parallel()
	for _, inv := range []string{
		// a join of an integer and a float may be an integer
		`(n -> Default(1.5)) % 2 == 1`,
		`(n -> Default(1.5)) > 0`,
		`(t ? { n : f }) % 2 == 1`,
		// a scalar of unknown kind
		`([1, "a"][0]) % 2 == 1`,
		`[true, "a"][0]`,
		// arithmetic and negation over a value of unknown kind
		`tags[(tags -> Reduce(0) |$a, $b| { 0 }) - 0] == "alpha"`,
		`tags[-(tags -> Reduce(0) |$a, $b| { 0 })] == "alpha"`,
		`tags[(tags -> Reduce(0) |$a, $b| { 0 }) -> Abs] == "alpha"`,
		// a remainder, and the results of Len, Compare and Sum, are numbers
		`tags[n % 2] == "beta"`,
		`(name -> Len) % 2 == 1`,
		`tags[(name -> Len) - 4] == "beta"`,
		`tags[f -> Compare(1.0)] == "beta"`,
		`(ints -> Sum) % 2 == 1`,
		`([] -> Sum) % 2 == 0`,
		`[] -> Sum == 0`,
		// a float is a number, and a string is ordered
		`f - 1.5 > 0`,
		`(-f) > -9.0`,
		`name >= "a"`,
		`name <= "z"`,
		// a list function over nil is the empty list, and no value that is always nil
		`(nil -> Compact)[0] == nil`,
		`(nil -> Sort) -> TypeOf == "list"`,
		`([nil -> Unique, true][0] + [[1], 2][0]) -> Len == 1`,
		`((t -> Max(nil -> Reverse)) + [[1], 2][0]) -> Len == 1`,
		`((nil -> Filter { true }) + ints) -> Len == 2`,
	} {
		t.Run(inv, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), operandKindSchema+"\t! \"m\" "+inv+"\n}\n", "s.yammm")
			if res.Err() != nil {
				t.Fatalf("the check refused it: %v", res.Err())
			}
			if _, res := instance.NewValidator(s).ValidateOne(t.Context(), "Order", operandKindOrder()); res.Err() != nil {
				t.Errorf("the invariant does not hold: %v", res.Err())
			}
		})
	}
}

// An operand, a receiver or a root of a kind the check knows is refused where
// that kind is not taken, with one issue for each mistake.
func TestOperandKinds_AKnownKindIsRefused(t *testing.T) {
	t.Parallel()
	const integerLeft = "% takes an integer as its left operand, and it is not one"
	for _, row := range []struct {
		inv  string
		want []string
	}{
		// a float where an integer is taken
		{`f % 2 == 0`, []string{integerLeft}},
		{`n % f == 0`, []string{"% takes an integer as its right operand, and it is not one"}},
		{`tags[f] == nil`, []string{"indexing takes an integer as its index, and it is not one"}},
		{`name -> Substring(f) == ""`, []string{"Substring takes an integer as its argument, and argument 1 is not one"}},
		{`vec[0] % 2 == 0`, []string{integerLeft}},
		// arithmetic with a float on either side is a float
		{`(f * 2) % 2 == 0`, []string{integerLeft}},
		{`(2 * f) % 2 == 0`, []string{integerLeft}},
		{`(-f) % 2 == 0`, []string{integerLeft}},
		// the numeric functions keep a float
		{`(f -> Abs) % 2 == 0`, []string{integerLeft}},
		{`(f -> Floor) % 2 == 0`, []string{integerLeft}},
		{`(f -> Ceil) % 2 == 0`, []string{integerLeft}},
		{`(f -> Round) % 2 == 0`, []string{integerLeft}},
		// a number has no index, a float among them
		{`f[0] == nil`, []string{"a number cannot be indexed"}},
		{`vec[0][0] == nil`, []string{"a number cannot be indexed"}},
		{`(n -> Default(1.5))[0] == nil`, []string{"a number cannot be indexed"}},
		// an operator's result is of a known kind
		{`(name =~ /n/) -> Upper != ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(n > 0) -> Upper != ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(!t) -> Upper != ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(name + name) -> Abs == 1`, []string{"Abs takes a number, and its receiver is not one"}},
		{`[1, 1.5] -> First -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		// a boolean has no + arm, whatever stands beside it
		{`(t + ([] -> First)) == 1`, []string{"+ takes two numbers, two strings or two lists"}},
		{`([1, "a"][0] + t) == 1`, []string{"+ takes two numbers, two strings or two lists"}},
		// a list function keeps its receiver's element kind
		{`(ints -> Filter |$x| { true })[0] -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(ints -> Compact)[0] -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(ints -> Unique)[0] -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(ints -> Sort)[0] -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		{`(ints -> Reverse)[0] -> Upper == ""`, []string{"Upper takes a string, and its receiver is not one"}},
		// a list function over nil is a list, and a refused receiver gives its result no kind
		{`(nil -> Compact) -> Default(true)`, []string{"Default takes a fallback of its receiver's kind"}},
		{`(nil -> Reverse).x == 1`, []string{`"x" is read from a list`}},
		{`((t -> Sort) + 1) == 1`, []string{"Sort takes a list, and its receiver is not one"}},
		{`(name -> Compact) -> Abs == 1`, []string{"Compact takes a list, and its receiver is not one"}},
		// Min of a float and a boolean is the boolean
		{`(f -> Min(true)) -> Abs == 1`, []string{"Abs takes a number, and its receiver is not one"}},
		// a scalar is no list to reduce, with a seed or without
		{`name -> Reduce(0) |$a, $b| { $a } == 0`, []string{"Reduce takes a list, and its receiver is not one"}},
		// a position that takes a pattern literal names it
		{`name -> Match("a") != nil`, []string{"Match takes a pattern literal as its argument, and argument 1 is not one"}},
		{`name =~ nonexistent`, []string{"=~ takes a pattern literal as its right operand", `unknown property "nonexistent"`}},
		// a literal with no value keeps the kind it was written as
		{`tags[99999999999999999999] == nil`, []string{"invalid integer literal"}},
		{`tags[1.5e999] == nil`, []string{"indexing takes an integer as its index, and it is not one", "invalid float literal"}},
		// a pattern is no value, and nothing is read from it
		{`/a/.x == 1`, []string{"a pattern literal stands only on the right of =~ or !~ and as the argument of Match"}},
	} {
		t.Run(row.inv, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(), operandKindSchema+"\t! \"m\" "+row.inv+"\n}\n", "s.yammm")
			var got []string
			for is := range res.Issues() {
				got = append(got, is.Message())
			}
			if len(got) != len(row.want) {
				t.Fatalf("got %d issues %q, want %d", len(got), got, len(row.want))
			}
			for i, want := range row.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("issue %d is %q, want it to hold %q", i, got[i], want)
				}
			}
		})
	}
}

// A root that is the nil literal loads, and the invariant fails on the instance.
func TestOperandKinds_ANilRootLoadsAndFails(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), operandKindSchema+"\t! \"m\" nil\n}\n", "s.yammm")
	if res.Err() != nil {
		t.Fatalf("the check refused it: %v", res.Err())
	}
	_, res = instance.NewValidator(s).ValidateOne(t.Context(), "Order", operandKindOrder())
	if got := codes(res); len(got) != 1 || got[0] != "E_INVARIANT_FAIL" {
		t.Errorf("got %v, want one E_INVARIANT_FAIL", got)
	}
}

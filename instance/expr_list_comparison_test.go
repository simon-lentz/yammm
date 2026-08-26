package instance_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// invariantCodes loads a one-type schema carrying inv, validates props against
// it, and returns the diagnostic codes the result holds. It exists beside
// invariantHolds because the distinction that matters here is between an
// invariant that answered false and one that could not be evaluated at all.
func invariantCodes(t *testing.T, decls, inv string, props map[string]any) []diag.Code {
	t.Helper()
	src := "schema \"x\"\n\ntype T {\n\tid String primary\n" + decls +
		"\t! \"probe\" " + inv + "\n}\n"
	s, result := schema.LoadString(t.Context(), src, "x.yammm")
	if result.HasErrors() {
		t.Fatalf("schema carrying %q did not load: %s", inv, result)
	}
	all := map[string]any{"id": "a"}
	maps.Copy(all, props)
	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: all})
	var codes []diag.Code
	for issue := range res.Issues() {
		codes = append(codes, issue.Code())
	}
	return codes
}

// Every comparison operator accepts a List-typed property. Its value arrives as
// immutable.Slice — a struct, which no reflect.Slice test names — so before
// internal/value carried an explicit case, each of these was E_EVAL_ERROR
// rather than an answer. The bracket operator had been repaired for the same
// root cause and the comparison operators had not, which is why
// TestIndexing_ListPropertyIsIndexable passed throughout.
//
// The truth values follow the documented strata order, nil < bool < numeric <
// string < slice: a list is never nil, never equal to a string, and ranks above
// one.
//
// Mutation: making TypeStrata's immutable.Slice case return InvalidStrata turns
// every subtest red with the E_EVAL_ERROR this fix removed.
func TestListComparison_EveryOperatorEvaluates(t *testing.T) {
	const decls = "\ttags List<String>\n"
	props := map[string]any{"tags": []any{"alpha", "beta"}}

	for _, tc := range []struct {
		name string
		inv  string
		want bool
	}{
		{"equal to nil is false", `tags == nil`, false},
		{"not equal to nil is true", `tags != nil`, true},
		{"not equal to a string is true", `tags != "x"`, true},
		{"equal to a matching literal", `tags == ["alpha", "beta"]`, true},
		{"equal to a shorter literal is false", `tags == ["alpha"]`, false},
		{"ordered above a string", `tags > "z"`, true},
		{"not ordered below a string", `tags < "z"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codes := invariantCodes(t, decls, tc.inv, props)
			for _, c := range codes {
				if strings.Contains(c.String(), "EVAL_ERROR") {
					t.Fatalf("%s: %s — the comparison did not evaluate", tc.inv, c)
				}
			}
			passed := len(codes) == 0
			if passed != tc.want {
				t.Errorf("%s: passed = %v, want %v (codes %v)", tc.inv, passed, tc.want, codes)
			}
		})
	}
}

// A Vector property compares on the same path, and its elements are floats.
func TestListComparison_VectorProperty(t *testing.T) {
	const decls = "\tembedding Vector[3]\n"
	props := map[string]any{"embedding": []any{1.0, 2.0, 3.0}}

	codes := invariantCodes(t, decls, `embedding == [1.0, 2.0, 3.0]`, props)
	if len(codes) != 0 {
		t.Errorf("a Vector did not compare equal to its own literal: %v", codes)
	}
}

// A nil List property is nil, which the null-guard idiom SPEC documents relies
// on. immutable wraps a nil slice as an empty Slice struct rather than leaving
// it nil, so this asks a different question from the cases above.
func TestListComparison_AbsentListIsNil(t *testing.T) {
	const decls = "\ttags List<String>\n"

	codes := invariantCodes(t, decls, `tags == nil`, map[string]any{})
	if len(codes) != 0 {
		t.Errorf("an absent List property was not nil: %v", codes)
	}
}

// in, Contains and Unique compare their operands through the same ordering the
// comparison operators use, and all three SWALLOW an ordering error — answering
// not-found, false, or distinct rather than reporting. While a list could not be
// ordered, every one of these returned a silently wrong answer instead of a
// diagnostic, which is why no test caught them.
//
// Mutation: making TypeStrata's immutable.Slice case return InvalidStrata turns
// each of these red WITHOUT producing a diagnostic — the failure is a wrong
// boolean, which is the property that made the original defect invisible.
func TestListComparison_CollectionBuiltinsCompareLists(t *testing.T) {
	for _, tc := range []struct {
		name  string
		decls string
		inv   string
		props map[string]any
		want  bool
	}{
		{
			"Unique dedupes two equal lists",
			"\ttags List<String>\n", `[tags, tags] -> Unique -> Len == 1`,
			map[string]any{"tags": []any{"a"}},
			true,
		},
		{
			"in finds a list among list literals",
			"\ttags List<String>\n", `tags in [["a"], ["b"]]`,
			map[string]any{"tags": []any{"a"}},
			true,
		},
		{
			"in reports absence honestly",
			"\ttags List<String>\n", `tags in [["b"], ["c"]]`,
			map[string]any{"tags": []any{"a"}},
			false,
		},
		{
			"Unique dedupes nested lists",
			"\tnested List<List<String>>\n", `nested -> Unique -> Len == 1`,
			map[string]any{"nested": []any{[]any{"a"}, []any{"a"}}},
			true,
		},
		{
			"Contains distinguishes an element from a list",
			"\ttags List<String>\n", `tags -> Contains(["a"])`,
			map[string]any{"tags": []any{"a"}},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codes := invariantCodes(t, tc.decls, tc.inv, tc.props)
			for _, c := range codes {
				if strings.Contains(c.String(), "EVAL_ERROR") {
					t.Fatalf("%s: %s", tc.inv, c)
				}
			}
			if passed := len(codes) == 0; passed != tc.want {
				t.Errorf("%s: passed = %v, want %v", tc.inv, passed, tc.want)
			}
		})
	}
}

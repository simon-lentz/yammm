package instance_test

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// invariantHolds loads a one-type schema carrying inv and validates props
// against it, reporting whether every invariant passed.
func invariantHolds(t *testing.T, decls, inv string, props map[string]any) (bool, string) {
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
	return res.OK(), res.String()
}

// A List-typed property is indexable. Its value arrives as immutable.Slice —
// what Value.Unwrap returns for a collection — which the bracket operator did
// not handle, so every List index was an evaluation error against the SPEC.
func TestIndexing_ListPropertyIsIndexable(t *testing.T) {
	const decls = "\ttags List<String>\n"
	props := map[string]any{"tags": []any{"alpha", "beta", "gamma"}}

	for _, tc := range []struct {
		name string
		inv  string
		want bool
	}{
		{"first element", `tags[0] == "alpha"`, true},
		{"middle element", `tags[1] == "beta"`, true},
		{"last element", `tags[2] == "gamma"`, true},
		{"wrong element fails", `tags[0] == "beta"`, false},
		{"out of range is nil", `tags[9] -> IsNil`, true},
		{"negative index is nil", `tags[-1] -> IsNil`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := invariantHolds(t, decls, tc.inv, props)
			if ok != tc.want {
				t.Errorf("%s: passed = %v, want %v — %s", tc.inv, ok, tc.want, detail)
			}
		})
	}
}

// A string property indexes by rune, and the two receiver kinds must agree.
func TestIndexing_StringPropertyStillIndexes(t *testing.T) {
	ok, detail := invariantHolds(t, "\tname String\n", `name[0] == "é"`,
		map[string]any{"name": "école"})
	if !ok {
		t.Errorf("string rune indexing broke: %s", detail)
	}
}

// The empty index list parses — the bracket grammar is more permissive than
// the language — and must draw an evaluation error, as the SPEC states.
func TestIndexing_EmptyIndexListIsAnEvaluationError(t *testing.T) {
	ok, detail := invariantHolds(t, "\ttags List<String>\n", `tags[] -> IsNil`,
		map[string]any{"tags": []any{"alpha"}})
	if ok {
		t.Error("tags[] evaluated without error, want an evaluation error")
	}
	if !strings.Contains(detail, "slice access requires an index") {
		t.Errorf("empty index list reported %q, want the user-facing message", detail)
	}
}

// Two indices draw an error naming the same vocabulary as the empty case; the
// two halves of one contract must not report in different languages.
func TestIndexing_TwoIndicesReportInTheSameVocabulary(t *testing.T) {
	ok, detail := invariantHolds(t, "\ttags List<String>\n", `tags[0, 1] -> IsNil`,
		map[string]any{"tags": []any{"alpha", "beta"}})
	if ok {
		t.Error("tags[0, 1] evaluated without error, want an evaluation error")
	}
	if !strings.Contains(detail, "slice access accepts exactly one index") {
		t.Errorf("two indices reported %q, want the user-facing message", detail)
	}
}

// Len counts a map's entries. The DSL declares no map datatype, so the only
// map an expression sees is $self — carried as immutable.Map[string], a
// struct the reflect.Map arm never matches, which is why the SPEC's "or map"
// was false. TypeOf already named it "map", so the two disagreed.
func TestLen_CountsSelfMapEntries(t *testing.T) {
	const decls = "\tname String\n"
	props := map[string]any{"name": "n"}

	if ok, detail := invariantHolds(t, decls, `$self -> TypeOf == "map"`, props); !ok {
		t.Errorf("$self is not named a map: %s", detail)
	}
	if ok, detail := invariantHolds(t, decls, `$self -> Len == 2`, props); !ok {
		t.Errorf("Len over $self's two properties: %s", detail)
	}
	if ok, _ := invariantHolds(t, decls, `$self -> Len == 3`, props); ok {
		t.Error("Len over $self accepted the wrong count — it cannot fail")
	}
}

// TypeOf names a timestamp rather than reporting "unknown". The name comes
// from the value, so only the time.Time form is distinguishable — a Timestamp
// carrying a string is indistinguishable from a String property.
func TestTypeOf_NamesATimestamp(t *testing.T) {
	ok, detail := invariantHolds(t, "\tts Timestamp\n", `ts -> TypeOf == "timestamp"`,
		map[string]any{"ts": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)})
	if !ok {
		t.Errorf("TypeOf on a time.Time-valued Timestamp: %s", detail)
	}

	ok, detail = invariantHolds(t, "\tts Timestamp\n", `ts -> TypeOf == "string"`,
		map[string]any{"ts": "2020-01-02T03:04:05Z"})
	if !ok {
		t.Errorf("TypeOf on a string-valued Timestamp: %s", detail)
	}
}

// TypeOf names a UUID rather than reporting it as a list. uuid.UUID is
// [16]byte, so the array arm of the classifier claimed it until this case
// preceded it.
func TestTypeOf_NamesAUUID(t *testing.T) {
	ok, detail := invariantHolds(t, "\tu UUID\n", `u -> TypeOf == "uuid"`,
		map[string]any{"u": uuid.MustParse("0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b")})
	if !ok {
		t.Errorf("TypeOf on a uuid.UUID-valued UUID property: %s", detail)
	}

	// The D-8 limit: the name comes from the value, so a string-valued UUID
	// property is indistinguishable from a String.
	ok, detail = invariantHolds(t, "\tu UUID\n", `u -> TypeOf == "string"`,
		map[string]any{"u": "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"})
	if !ok {
		t.Errorf("TypeOf on a string-valued UUID property: %s", detail)
	}
}

// A list index beyond int32 range yields nil. On a 64-bit build this holds
// with or without the arm's int64 bounds check, so it states the contract
// rather than pinning the check; the check exists for a consumer building
// 32-bit, where narrowing first wraps a large index into range.
func TestIndexing_LargeIndexDoesNotWrap(t *testing.T) {
	const decls = "\ttags List<String>\n"
	props := map[string]any{"tags": []any{"alpha", "beta", "gamma"}}

	for _, inv := range []string{
		`tags[4294967296] -> IsNil`,
		`tags[4294967297] -> IsNil`,
		`tags[-4294967295] -> IsNil`,
	} {
		if ok, detail := invariantHolds(t, decls, inv, props); !ok {
			t.Errorf("%s: %s", inv, detail)
		}
	}

	// The control: an in-range index still resolves, so the assertions above
	// cannot pass by making every index nil.
	if ok, detail := invariantHolds(t, decls, `tags[1] == "beta"`, props); !ok {
		t.Errorf("in-range index stopped resolving: %s", detail)
	}
}

// Comparing a Timestamp property compares whatever Go value the caller
// submitted, because CoerceValue treats the kind as already canonical. A
// string orders as a string; a time.Time reaches no strata and every operator
// over it draws E_EVAL_ERROR.
func TestComparison_TimestampOrdersOnlyItsStringForm(t *testing.T) {
	const decl = "\tts Timestamp\n"

	ok, detail := invariantHolds(t, decl, `ts > "2020-01-01T00:00:00Z"`,
		map[string]any{"ts": "2020-06-01T00:00:00Z"})
	if !ok {
		t.Errorf("comparing a string-valued Timestamp: %s", detail)
	}

	ok, detail = invariantHolds(t, decl, `ts > "2020-01-01T00:00:00Z"`,
		map[string]any{"ts": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)})
	if ok {
		t.Error("comparing a time.Time-valued Timestamp succeeded")
	}
	if !strings.Contains(detail, "unsupported type comparison") {
		t.Errorf("comparison over a time.Time reported %q, want the unsupported-comparison error", detail)
	}
}

// `in` is the one operator that swallows the comparison error rather than
// reporting it: an incomparable element is treated as not equal, so a
// time.Time-valued Timestamp is silently absent from a list holding it.
func TestIn_SwallowsTheTimestampComparisonError(t *testing.T) {
	const decl = "\tts Timestamp\n"

	ok, detail := invariantHolds(t, decl, `ts in ["2020-06-01T00:00:00Z"]`,
		map[string]any{"ts": "2020-06-01T00:00:00Z"})
	if !ok {
		t.Errorf("a string-valued Timestamp in a list holding it: %s", detail)
	}

	ok, detail = invariantHolds(t, decl, `ts in ["2020-06-01T00:00:00Z"]`,
		map[string]any{"ts": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)})
	if ok {
		t.Error("a time.Time-valued Timestamp matched a list entry")
	}
	if strings.Contains(detail, "unsupported type comparison") {
		t.Errorf("`in` surfaced the comparison error rather than swallowing it: %s", detail)
	}
}

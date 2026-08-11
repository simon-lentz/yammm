package instance_test

import (
	"maps"
	"strings"
	"testing"
	"time"

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

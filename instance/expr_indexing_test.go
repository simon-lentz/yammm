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

// TypeOf reports "string" for a Timestamp whichever way the caller wrote it.
// Coercion renders the kind to text before an invariant ever sees it, so
// "timestamp" is a name nothing can produce and it left the vocabulary.
func TestTypeOf_NamesATimestampAsAString(t *testing.T) {
	for name, val := range map[string]any{
		"go native": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		"string":    "2020-01-02T03:04:05Z",
	} {
		if ok, detail := invariantHolds(t, "\tts Timestamp\n", `ts -> TypeOf == "string"`,
			map[string]any{"ts": val}); !ok {
			t.Errorf("TypeOf on a %s Timestamp: %s", name, detail)
		}
	}

	// The vocabulary entry is gone, not merely unreachable for this input.
	if ok, _ := invariantHolds(t, "\tts Timestamp\n", `ts -> TypeOf == "timestamp"`,
		map[string]any{"ts": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}); ok {
		t.Error(`TypeOf still yields "timestamp"`)
	}
}

// TypeOf reports "string" for a UUID whichever way the caller wrote it. Before
// canonicalization a uuid.UUID reached the evaluator as [16]byte and needed its
// own vocabulary entry to avoid reporting as a list.
func TestTypeOf_NamesAUUIDAsAString(t *testing.T) {
	for name, val := range map[string]any{
		"go native": uuid.MustParse("0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"),
		"string":    "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b",
	} {
		if ok, detail := invariantHolds(t, "\tu UUID\n", `u -> TypeOf == "string"`,
			map[string]any{"u": val}); !ok {
			t.Errorf("TypeOf on a %s UUID property: %s", name, detail)
		}
	}

	if ok, _ := invariantHolds(t, "\tu UUID\n", `u -> TypeOf == "uuid"`,
		map[string]any{"u": uuid.MustParse("0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b")}); ok {
		t.Error(`TypeOf still yields "uuid"`)
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

// Comparison over a Timestamp works for both representations, because
// coercion renders the kind to text before an invariant sees it. A time.Time
// reached no strata and drew E_EVAL_ERROR on every operator until it did.
func TestComparison_TimestampOrdersBothRepresentations(t *testing.T) {
	const decl = "\tts Timestamp\n"

	for name, val := range map[string]any{
		"string":    "2020-06-01T00:00:00Z",
		"go native": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	} {
		ok, detail := invariantHolds(t, decl, `ts > "2020-01-01T00:00:00Z"`,
			map[string]any{"ts": val})
		if !ok {
			t.Errorf("comparing a %s Timestamp: %s", name, detail)
		}
		if strings.Contains(detail, "unsupported type comparison") {
			t.Errorf("comparing a %s Timestamp still draws the strata error: %s", name, detail)
		}
	}

	// The control: comparison still decides rather than always holding.
	if ok, _ := invariantHolds(t, decl, `ts > "2020-12-01T00:00:00Z"`,
		map[string]any{"ts": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)}); ok {
		t.Error("a Timestamp compared greater than a later instant")
	}
}

// Comparison over a UUID is the twin of the Timestamp case, and the sharper
// one: uuid.UUID is [16]byte, so it reached the strata classifier as an array
// and every ordered comparison over it was an evaluation error.
func TestComparison_UUIDOrdersBothRepresentations(t *testing.T) {
	const decl = "\tu UUID\n"
	const low = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"
	const high = "fa35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"

	for name, val := range map[string]any{
		"string":    high,
		"go native": uuid.MustParse(high),
	} {
		ok, detail := invariantHolds(t, decl, `u > "`+low+`"`, map[string]any{"u": val})
		if !ok {
			t.Errorf("comparing a %s UUID: %s", name, detail)
		}
		if strings.Contains(detail, "unsupported type comparison") {
			t.Errorf("comparing a %s UUID still draws the strata error: %s", name, detail)
		}
	}
}

// `in` matches a Timestamp in either representation. It is the one operator
// that swallows a comparison error rather than reporting it, so before
// canonicalization a time.Time was silently absent from a list holding it —
// a wrong answer with no diagnostic.
func TestIn_MatchesATimestampInEitherRepresentation(t *testing.T) {
	const decl = "\tts Timestamp\n"

	for name, val := range map[string]any{
		"string":    "2020-06-01T00:00:00Z",
		"go native": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC),
	} {
		if ok, detail := invariantHolds(t, decl, `ts in ["2020-06-01T00:00:00Z"]`,
			map[string]any{"ts": val}); !ok {
			t.Errorf("a %s Timestamp in a list holding it: %s", name, detail)
		}
	}

	// The control: `in` still reports absence.
	if ok, _ := invariantHolds(t, decl, `ts in ["2020-07-01T00:00:00Z"]`,
		map[string]any{"ts": time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)}); ok {
		t.Error("a Timestamp matched a list that does not hold it")
	}
}

// The UUID twin of the `in` case.
func TestIn_MatchesAUUIDInEitherRepresentation(t *testing.T) {
	const decl = "\tu UUID\n"
	const id = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"

	for name, val := range map[string]any{
		"string":    id,
		"go native": uuid.MustParse(id),
	} {
		if ok, detail := invariantHolds(t, decl, `u in ["`+id+`"]`,
			map[string]any{"u": val}); !ok {
			t.Errorf("a %s UUID in a list holding it: %s", name, detail)
		}
	}
}

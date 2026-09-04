package value_test

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// canon runs Canonical and fails the test on error, for the cases where the
// error is not what is under test.
func canon(t *testing.T, in any, c schema.Constraint) any {
	t.Helper()
	got, err := value.Canonical(in, c)
	if err != nil {
		t.Fatalf("Canonical(%#v): %v", in, err)
	}
	return got
}

// TestCanonical_TimestampReachesOneSpelling covers both representations and
// every RFC 3339 spelling that renders differently from its input, which is
// the set §2.4's key-identity defect lives in.
func TestCanonical_TimestampReachesOneSpelling(t *testing.T) {
	t.Parallel()
	bare := schema.NewTimestampConstraint()

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"go native, whole second", time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), "2026-08-19T12:00:00Z"},
		{"go native, sub-second", time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC), "2026-08-19T12:00:00.5Z"},
		{"go native, offset", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 2*60*60)), "2026-08-19T12:00:00+02:00"},
		{"string, already canonical", "2026-08-19T12:00:00Z", "2026-08-19T12:00:00Z"},
		{"string, offset survives", "2026-08-19T12:00:00+02:00", "2026-08-19T12:00:00+02:00"},
		{"string, zero offset renders Z", "2026-08-19T12:00:00+00:00", "2026-08-19T12:00:00Z"},
		{"string, trailing fraction zeros trimmed", "2026-08-19T12:00:00.500Z", "2026-08-19T12:00:00.5Z"},
		{"string, all-zero fraction dropped", "2026-08-19T12:00:00.000Z", "2026-08-19T12:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := canon(t, tc.in, bare); got != tc.want {
				t.Errorf("Canonical = %#v, want %q", got, tc.want)
			}
		})
	}
}

// TestCanonical_TimestampMovesNoKey is the assertion that keeps the release
// from re-keying every stored instance. computeKeyString renders a key through
// encoding/json, so the canonical text has to be the text json.Marshal already
// produces for the same time.Time.
func TestCanonical_TimestampMovesNoKey(t *testing.T) {
	t.Parallel()
	bare := schema.NewTimestampConstraint()

	for _, ts := range []time.Time{
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC),
		time.Date(2026, 8, 19, 12, 0, 0, 123456789, time.UTC),
		time.Date(2026, 8, 19, 12, 0, 0, 1, time.UTC),
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", -5*60*60)),
	} {
		marshalled, err := json.Marshal(ts)
		if err != nil {
			t.Fatalf("json.Marshal(%v): %v", ts, err)
		}
		var want string
		if err := json.Unmarshal(marshalled, &want); err != nil {
			t.Fatalf("unmarshal %s: %v", marshalled, err)
		}
		if got := canon(t, ts, bare); got != want {
			t.Errorf("Canonical(%v) = %#v, want %q — the key would move", ts, got, want)
		}
	}
}

// TestCanonical_TimestampUsesTheDeclaredFormat closes the defect where a
// custom-format timestamp built from a time.Time was written in RFC 3339 text
// that does not satisfy its own declared layout.
func TestCanonical_TimestampUsesTheDeclaredFormat(t *testing.T) {
	t.Parallel()
	const layout = "2006-01-02 15:04:05"
	c := schema.NewTimestampConstraintFormatted(layout)

	got := canon(t, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), c)
	if got != "2026-08-19 12:00:00" {
		t.Errorf("Canonical of a time.Time under %q = %#v", layout, got)
	}
	if _, err := time.Parse(layout, got.(string)); err != nil {
		t.Errorf("canonical text does not satisfy its own declared layout: %v", err)
	}

	// The string arm parses under the declared layout exclusively, matching
	// what the checker accepts.
	if got := canon(t, "2026-08-19 12:00:00", c); got != "2026-08-19 12:00:00" {
		t.Errorf("Canonical of a conforming string = %#v", got)
	}
	if _, err := value.Canonical("2026-08-19T12:00:00Z", c); err == nil {
		t.Error("RFC 3339 text under a declared layout canonicalized; want an error")
	}
}

// TestCanonical_UUIDCollapsesEverySpelling pins the fix for the silent
// primary-key split: uuid.Parse accepts five spellings and every one of them
// produced its own key.
func TestCanonical_UUIDCollapsesEverySpelling(t *testing.T) {
	t.Parallel()
	c := schema.NewUUIDConstraint()
	const want = "0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"

	for _, in := range []any{
		uuid.MustParse(want),
		want,
		"0A35EF0F-9D40-4B6B-A0A1-0D1A5A0E1F2B",
		"{0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b}",
		"urn:uuid:0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b",
		"0a35ef0f9d404b6ba0a10d1a5a0e1f2b",
	} {
		if got := canon(t, in, c); got != want {
			t.Errorf("Canonical(%#v) = %#v, want %q", in, got, want)
		}
	}

	if _, err := value.Canonical("not-a-uuid", c); err == nil {
		t.Error("a non-UUID string canonicalized; want an error")
	}
}

// TestCanonical_UUIDMovesNoKey holds the Go-native form to the text
// MarshalText already produces, so canonicalizing moves no stored key.
func TestCanonical_UUIDMovesNoKey(t *testing.T) {
	t.Parallel()
	u := uuid.MustParse("0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b")
	text, err := u.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if got := canon(t, u, schema.NewUUIDConstraint()); got != string(text) {
		t.Errorf("Canonical = %#v, want %q", got, text)
	}
}

// TestCanonical_DateAcceptsATimeTime is the widening that makes gogen's
// generated time.Time field usable for a Date, and it truncates in the value's
// own location rather than in UTC.
func TestCanonical_DateAcceptsATimeTime(t *testing.T) {
	t.Parallel()
	c := schema.NewDateConstraint()

	if got := canon(t, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), c); got != "2026-08-19" {
		t.Errorf("Canonical of a UTC time.Time = %#v", got)
	}
	// 00:30 +02:00 is the 19th where it was observed; the same instant is the
	// 18th at 22:30Z. The location the caller built the value in decides.
	east := time.Date(2026, 8, 19, 0, 30, 0, 0, time.FixedZone("", 2*60*60))
	if got := canon(t, east, c); got != "2026-08-19" {
		t.Errorf("Canonical of a +02:00 time.Time = %#v, want the local day", got)
	}
	if got := canon(t, east.UTC(), c); got != "2026-08-18" {
		t.Errorf("Canonical of the same instant in UTC = %#v, want the UTC day", got)
	}

	if got := canon(t, "2026-08-19", c); got != "2026-08-19" {
		t.Errorf("Canonical of a date string = %#v", got)
	}
	for _, bad := range []string{"2026-8-19", "2026-08-9", "26-08-19", "2026-08-19T12:00:00Z"} {
		if _, err := value.Canonical(bad, c); err == nil {
			t.Errorf("Canonical(%q) succeeded; time.DateOnly admits one spelling", bad)
		}
	}
}

// TestCanonical_IsIdempotent is the property the fixpoint round trip rests on:
// a second pass over canonical text has to leave it alone.
func TestCanonical_IsIdempotent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		c    schema.Constraint
	}{
		{"timestamp go native", time.Date(2026, 8, 19, 12, 0, 0, 500000000, time.UTC), schema.NewTimestampConstraint()},
		{"timestamp non-canonical string", "2026-08-19T12:00:00.000+00:00", schema.NewTimestampConstraint()},
		{"timestamp declared layout", time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")},
		{"uuid go native", uuid.MustParse("0a35ef0f-9d40-4b6b-a0a1-0d1a5a0e1f2b"), schema.NewUUIDConstraint()},
		{"uuid uppercase string", "0A35EF0F-9D40-4B6B-A0A1-0D1A5A0E1F2B", schema.NewUUIDConstraint()},
		{"date go native", time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), schema.NewDateConstraint()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			once := canon(t, tc.in, tc.c)
			twice := canon(t, once, tc.c)
			if once != twice {
				t.Errorf("not idempotent: first pass %#v, second %#v", once, twice)
			}
		})
	}
}

// TestCanonical_ListsCanonicalizeElementwise pins A-38's arm. A suite with
// only scalar cases cannot tell the recursion from dead code.
func TestCanonical_ListsCanonicalizeElementwise(t *testing.T) {
	t.Parallel()
	ts := schema.NewListConstraint(schema.NewTimestampConstraint())

	got := canon(t, []any{
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
		"2026-08-19T13:00:00+00:00",
	}, ts)
	want := []any{"2026-08-19T12:00:00Z", "2026-08-19T13:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List<Timestamp> = %#v, want %#v", got, want)
	}

	nested := schema.NewListConstraint(ts)
	gotNested := canon(t, []any{[]any{time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}}, nested)
	wantNested := []any{[]any{"2026-08-19T12:00:00Z"}}
	if !reflect.DeepEqual(gotNested, wantNested) {
		t.Errorf("List<List<Timestamp>> = %#v, want %#v", gotNested, wantNested)
	}

	// A list of a pass-through kind keeps its concrete slice type.
	strs := []string{"a", "b"}
	if got := canon(t, strs, schema.NewListConstraint(schema.NewStringConstraint())); !reflect.DeepEqual(got, strs) {
		t.Errorf("List<String> = %#v, want the input slice untouched", got)
	}
}

// TestCanonical_ResolvesThroughAnAlias holds the DataType path: a
// Timestamp["layout"] reached through an alias keeps its layout.
func TestCanonical_ResolvesThroughAnAlias(t *testing.T) {
	t.Parallel()
	alias := schema.NewAliasConstraint("EventTime", schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05"))
	if got := canon(t, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), alias); got != "2026-08-19 12:00:00" {
		t.Errorf("Canonical through an alias = %#v", got)
	}
}

// TestCanonical_PassesEveryOtherKindThrough is the negative half: the rule
// moves three kinds and must leave the rest identical.
func TestCanonical_PassesEveryOtherKindThrough(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		c    schema.Constraint
	}{
		{"string", "x", schema.NewStringConstraint()},
		{"integer", int64(3), schema.NewIntegerConstraint()},
		{"float", 2.5, schema.NewFloatConstraint()},
		{"boolean", true, schema.NewBooleanConstraint()},
		{"enum", "a", schema.NewEnumConstraint([]string{"a", "b"})},
		{"pattern", "ab", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a")})},
		{"vector", []float64{1, 2}, schema.NewVectorConstraint(2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := canon(t, tc.in, tc.c); !reflect.DeepEqual(got, tc.in) {
				t.Errorf("Canonical = %#v, want the input untouched", got)
			}
		})
	}
}

// TestCanonical_ReturnsTheInputOnFailure is what lets a write arm ignore the
// error and use the value: it heals what it can and passes through what it
// cannot, never failing a write.
func TestCanonical_ReturnsTheInputOnFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		c    schema.Constraint
	}{
		{"timestamp, wrong type", 42, schema.NewTimestampConstraint()},
		{"timestamp, unparseable text", "not-a-timestamp", schema.NewTimestampConstraint()},
		{"uuid, wrong type", 42, schema.NewUUIDConstraint()},
		{"date, wrong type", 42, schema.NewDateConstraint()},
		{"list element fails", []any{"not-a-timestamp"}, schema.NewListConstraint(schema.NewTimestampConstraint())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.Canonical(tc.in, tc.c)
			if err == nil {
				t.Fatal("want an error")
			}
			if !reflect.DeepEqual(got, tc.in) {
				t.Errorf("on failure Canonical returned %#v, want the input unchanged", got)
			}
		})
	}
}

// TestCanonical_NilInputsPassThrough covers the two guards every call site
// relies on: an absent optional value and an unconstrained property.
func TestCanonical_NilInputsPassThrough(t *testing.T) {
	t.Parallel()
	if got := canon(t, nil, schema.NewTimestampConstraint()); got != nil {
		t.Errorf("Canonical(nil) = %#v, want nil", got)
	}
	if got := canon(t, "x", nil); got != "x" {
		t.Errorf("Canonical with a nil constraint = %#v, want the input", got)
	}
}

// canonicalUUID reads the same carriers checkUUID accepts: a named string
// type (adapter/gogen's shape for a DataType over UUID) and a *string. A
// value the check passes must not fail the coercion that follows it.
func TestCanonical_UUIDReadsAStringCarrier(t *testing.T) {
	t.Parallel()
	type userID string
	const spelled = "{123E4567-E89B-12D3-A456-426614174000}"
	const want = "123e4567-e89b-12d3-a456-426614174000"
	str := spelled
	for name, in := range map[string]any{"named string": userID(spelled), "pointer": &str} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := canon(t, in, schema.NewUUIDConstraint()); got != want {
				t.Errorf("Canonical(%T) = %#v, want %q", in, got, want)
			}
		})
	}
}

// A non-slice under a List constraint is an error, not a value already in
// canonical form: the check side's coerceList refuses it, and both halves of
// the stored-form rule must agree.
func TestCanonical_ListRefusesANonSlice(t *testing.T) {
	t.Parallel()
	lc := schema.NewListConstraint(schema.NewTimestampConstraint())
	for name, in := range map[string]any{"string": "2026-08-19T12:00:00Z", "array": [1]string{"2026-08-19T12:00:00Z"}, "int": 7} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := value.Canonical(in, lc)
			if err == nil {
				t.Fatalf("Canonical(%T) under List<Timestamp> returned nil error", in)
			}
			if !reflect.DeepEqual(got, in) {
				t.Errorf("on failure Canonical returned %#v, want the input unchanged", got)
			}
		})
	}
}

// An immutable.Slice is a slice for every purpose in this package, the list
// arm included: it is what a stored List property unwraps to.
func TestCanonical_ListReadsAnImmutableSlice(t *testing.T) {
	t.Parallel()
	lc := schema.NewListConstraint(schema.NewTimestampConstraint())
	in := immutable.Wrap([]any{time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), "2026-08-19T13:00:00+00:00"}).Unwrap()
	if _, ok := in.(immutable.Slice); !ok {
		t.Fatalf("fixture: Wrap([]any).Unwrap() is %T, want immutable.Slice", in)
	}
	got := canon(t, in, lc)
	want := []any{"2026-08-19T12:00:00Z", "2026-08-19T13:00:00Z"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Canonical(immutable.Slice) = %#v, want %#v", got, want)
	}
}

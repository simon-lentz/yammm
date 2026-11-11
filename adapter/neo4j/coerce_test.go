package neo4j

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

func TestCoerceScalar_TimestampParse(t *testing.T) {
	got, err := coerceScalar(schema.NewTimestampConstraint(), "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("got %T, want time.Time", got)
	}
}

func TestCoerceScalar_DateParse(t *testing.T) {
	got, err := coerceScalar(schema.NewDateConstraint(), "2024-06-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(dbtype.Date); !ok {
		t.Fatalf("got %T, want dbtype.Date", got)
	}
}

func TestCoerceScalar_TimestampParseFailureErrors(t *testing.T) {
	if _, err := coerceScalar(schema.NewTimestampConstraint(), "not-a-timestamp"); err == nil {
		t.Fatal("want error on unparseable timestamp string, got nil")
	}
}

func TestCoerceScalar_DateParseFailureErrors(t *testing.T) {
	if _, err := coerceScalar(schema.NewDateConstraint(), "06/15/2024"); err == nil {
		t.Fatal("want error on unparseable date string, got nil")
	}
}

func TestCoerceScalar_PassThroughString(t *testing.T) {
	got, err := coerceScalar(schema.NewStringConstraint(), "hello")
	if err != nil || got != "hello" {
		t.Fatalf("string pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerceScalar_NilPassThrough(t *testing.T) {
	got, err := coerceScalar(schema.NewTimestampConstraint(), nil)
	if err != nil || got != nil {
		t.Fatalf("nil pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerceScalar_NilConstraintPassThrough(t *testing.T) {
	t.Parallel()
	// A nil constraint means "no type to coerce against": pass the value through,
	// matching coerceValue. A bad/unhandled kind is unreachable — schema.Constraint
	// is a sealed interface, so every value carries a valid kind, and the
	// exhaustiveness lint guards the switch.
	got, err := coerceScalar(nil, "x")
	if err != nil || got != "x" {
		t.Fatalf("nil constraint should pass through: got %#v, err %v", got, err)
	}
}

func TestCoerceParams_FloatRepairAndUnknownKey(t *testing.T) {
	params := map[string]any{
		"principal": int64(1860000),
		"untouched": int64(7),
	}
	types := ParamTypes{
		"principal": schema.NewFloatConstraint(),
		"missing":   schema.NewFloatConstraint(), // names a key params doesn't carry: no-op
	}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f, ok := out["principal"].(float64); !ok || f != 1860000.0 {
		t.Fatalf("principal: got %#v (%T), want float64(1860000)", out["principal"], out["principal"])
	}
	if out["untouched"] != int64(7) {
		t.Fatalf("untyped key should pass through unchanged, got %#v", out["untouched"])
	}
	if _, present := out["missing"]; present {
		t.Fatal("a types entry for an absent param must not appear in the output")
	}
}

func TestCoerceParams_TemporalGood(t *testing.T) {
	native := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	params := map[string]any{
		"ts":      "2026-04-22T00:00:00Z",
		"ts_nano": "2026-04-22T00:00:00.123456789Z",
		"native":  native,
		"date":    "2026-04-22",
		"label":   "unrelated",
	}
	types := ParamTypes{
		"ts":      schema.NewTimestampConstraint(),
		"ts_nano": schema.NewTimestampConstraint(),
		"native":  schema.NewTimestampConstraint(),
		"date":    schema.NewDateConstraint(),
		"label":   schema.NewStringConstraint(),
	}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out["ts"].(time.Time); !ok {
		t.Fatalf("ts: want time.Time, got %T", out["ts"])
	}
	if _, ok := out["ts_nano"].(time.Time); !ok {
		t.Fatalf("ts_nano: want time.Time, got %T", out["ts_nano"])
	}
	if got, ok := out["native"].(time.Time); !ok || !got.Equal(native) {
		t.Fatalf("native time.Time: want the instant %v, got %#v", native, out["native"])
	}
	if _, ok := out["date"].(dbtype.Date); !ok {
		t.Fatalf("date: want dbtype.Date, got %T", out["date"])
	}
	if out["label"] != "unrelated" {
		t.Fatalf("string under KindString should pass through, got %#v", out["label"])
	}
}

func TestCoerceParams_BadTemporalErrors(t *testing.T) {
	// yammm's coerceScalar returns an error on an unparseable Timestamp/Date; the
	// param boundary surfaces it (a naive passthrough would let these through).
	if _, err := CoerceParams(
		map[string]any{"ts": "not-a-timestamp"},
		ParamTypes{"ts": schema.NewTimestampConstraint()},
	); err == nil {
		t.Fatal("want error for unparseable Timestamp param, got nil")
	}
	if _, err := CoerceParams(
		map[string]any{"d": "2026/04/22"},
		ParamTypes{"d": schema.NewDateConstraint()},
	); err == nil {
		t.Fatal("want error for unparseable Date param, got nil")
	}
}

func TestCoerceParams_NilOrEmptyTypesPassThrough(t *testing.T) {
	t.Parallel()
	// With no constraints to apply, every value passes through unchanged — but
	// the result is still a fresh, independent map (see
	// TestCoerceParams_ReturnsIndependentMap), not the input aliased.
	params := map[string]any{"x": int64(1), "y": "hi"}
	out, err := CoerceParams(params, nil)
	if err != nil || len(out) != 2 || out["x"] != int64(1) || out["y"] != "hi" {
		t.Fatalf("nil types should pass values through unchanged: out=%v err=%v", out, err)
	}
	out2, err := CoerceParams(params, ParamTypes{})
	if err != nil || len(out2) != 2 || out2["x"] != int64(1) || out2["y"] != "hi" {
		t.Fatalf("empty types should pass values through unchanged: out=%v err=%v", out2, err)
	}
}

// CoerceParams reads one flat map: a key of types names a key of params, and no
// value is walked. A nested map, rows and a list of maps under an undeclared
// key pass as given, whatever a dotted key of types names.
func TestCoerceParams_WalksNoValue(t *testing.T) {
	t.Parallel()
	nested := map[string]any{"release_date": "2026-04-22", "principal": int64(5), "none": []any(nil)}
	rows := []map[string]any{{"release_date": "2026-04-22"}, nil}
	list := []any{map[string]any{"release_date": "2026-04-22"}, nil}
	types := ParamTypes{
		"updates.release_date": schema.NewDateConstraint(),
		"updates.principal":    schema.NewFloatConstraint(),
		"rows.release_date":    schema.NewDateConstraint(),
		"list.release_date":    schema.NewDateConstraint(),
	}
	out, err := CoerceParams(map[string]any{"updates": nested, "rows": rows, "list": list}, types)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	want := map[string]any{
		"updates": map[string]any{"release_date": "2026-04-22", "principal": int64(5), "none": []any(nil)},
		"rows":    []map[string]any{{"release_date": "2026-04-22"}, nil},
		"list":    []any{map[string]any{"release_date": "2026-04-22"}, nil},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("CoerceParams = %#v; want each value as given, %#v", out, want)
	}
}

// A map, rows and a list of maps under a declared key are each judged as the
// value they are: the validator stores none of them, so each is refused in the
// class of every refused value. A nil constraint judges nothing.
func TestCoerceParams_JudgesAMapOrRowsAtADeclaredKey(t *testing.T) {
	t.Parallel()
	strings := schema.NewListConstraint(schema.NewStringConstraint())
	for name, tc := range map[string]struct {
		c   schema.Constraint
		val any
	}{
		"a map at a String":                 {schema.NewStringConstraint(), map[string]any{"a": "b"}},
		"a map at a List of String":         {strings, map[string]any{"a": "b"}},
		"rows at a List of String":          {strings, []map[string]any{{"a": "b"}}},
		"a list of maps at a List":          {strings, []any{map[string]any{"a": "b"}}},
		"a list of a map and a nil at List": {strings, []any{map[string]any{"a": "b"}, nil}},
	} {
		got, err := CoerceParams(map[string]any{"v": tc.val}, ParamTypes{"v": tc.c})
		if !errors.Is(err, ErrUnrepresentable) || !contains(err, `param "v"`) {
			t.Errorf("%s: CoerceParams = %v, %v; want a refusal naming the key that matches ErrUnrepresentable", name, got, err)
		}
	}
	given := map[string]any{"a": "b"}
	got, err := CoerceParams(map[string]any{"v": given}, ParamTypes{"v": nil})
	if err != nil || !reflect.DeepEqual(got["v"], given) {
		t.Errorf("a map at a nil constraint: CoerceParams = %v, %v; want it as given", got, err)
	}
}

func contains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}

func TestCoerceParams_NilValuePassThrough(t *testing.T) {
	params := map[string]any{"deactivated_at": nil, "run_id": "runA"}
	types := ParamTypes{"deactivated_at": schema.NewTimestampConstraint()}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["deactivated_at"] != nil {
		t.Fatalf("nil value must pass through, got %#v", out["deactivated_at"])
	}
	if out["run_id"] != "runA" {
		t.Fatalf("run_id passthrough broken: %#v", out["run_id"])
	}
}

func TestCoerceParams_TopLevelListCoerced(t *testing.T) {
	t.Parallel()
	// A top-level []any list param is element-coerced against its List element
	// type, exactly like a nested one — no boundary silently passes lists through.
	params := map[string]any{
		"scores": []any{int64(1), int64(2)}, // List<Float>
		"labels": []any{"x", "y"},           // List<String>
	}
	types := ParamTypes{
		"scores": schema.NewListConstraint(schema.NewFloatConstraint()),
		"labels": schema.NewListConstraint(schema.NewStringConstraint()),
	}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := out["scores"].([]float64); !ok {
		t.Fatalf("top-level List<Float> not element-coerced: scores is %T, want []float64", out["scores"])
	}
	if _, ok := out["labels"].([]string); !ok {
		t.Fatalf("top-level List<String> not normalized: labels is %T, want []string", out["labels"])
	}
}

func TestParamTypesForType(t *testing.T) {
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity type not found in basic.yammm")
	}

	// Each key is a property name, and the stored value is the full constraint.
	flat := ParamTypesForType(st)
	if schema.ResolveAlias(flat["score"]).Kind() != schema.KindFloat {
		t.Errorf("score: got %v, want KindFloat", flat["score"])
	}
	if schema.ResolveAlias(flat["created_at"]).Kind() != schema.KindTimestamp {
		t.Errorf("created_at: got %v, want KindTimestamp", flat["created_at"])
	}
	if schema.ResolveAlias(flat["birth_date"]).Kind() != schema.KindDate {
		t.Errorf("birth_date: got %v, want KindDate", flat["birth_date"])
	}

	// End-to-end: the derived map feeds CoerceParams and a row of properties coerces.
	out, err := CoerceParams(map[string]any{"score": int64(3)}, flat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isFloat := out["score"].(float64); !isFloat {
		t.Fatalf("end-to-end: score not coerced to float64: %T", out["score"])
	}
}

func TestCoerceParams_ListElementCoercion(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity type not found in list_properties.yammm")
	}

	// One row of a direct-Cypher UNWIND $rows write of node properties. List
	// values arrive as []any with un-coerced elements (int64 for Float, strings
	// for Date/Timestamp) — exactly what a JSON decode or a hand-built param map
	// produces. CoerceParams must element-coerce them, not pass them through.
	params := map[string]any{
		"id":     "e1",
		"ratios": []any{int64(1), int64(2)},         // List<Float>
		"dates":  []any{"2024-01-01", "2024-06-15"}, // List<Date>
		"times":  []any{"2024-01-01T00:00:00Z"},     // List<Timestamp>
		"tags":   []any{"x", "y"},                   // List<String>
	}

	row, err := CoerceParams(params, ParamTypesForType(st))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := row["ratios"].([]float64); !ok {
		t.Errorf("List<Float> param not element-coerced: ratios is %T, want []float64", row["ratios"])
	}
	if _, ok := row["dates"].([]dbtype.Date); !ok {
		t.Errorf("List<Date> param not element-coerced: dates is %T, want []dbtype.Date", row["dates"])
	}
	if _, ok := row["times"].([]time.Time); !ok {
		t.Errorf("List<Timestamp> param not element-coerced: times is %T, want []time.Time", row["times"])
	}
	if _, ok := row["tags"].([]string); !ok {
		t.Errorf("List<String> param not normalized: tags is %T, want []string", row["tags"])
	}
}

func TestCoerceParams_ListElementError(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity type not found in list_properties.yammm")
	}
	// An unparseable element in a List<Date> param must surface as an error,
	// not reach the driver as a raw string.
	params := map[string]any{"id": "e1", "dates": []any{"not-a-date"}}
	if _, err := CoerceParams(params, ParamTypesForType(st)); err == nil {
		t.Fatal("want error for an unparseable List<Date> element, got nil")
	}
}

func TestCoerceParams_ListElementTypeMismatchErrors(t *testing.T) {
	t.Parallel()
	// A direct-Cypher List<Float> param carrying a non-numeric element cannot be
	// built into a []float64; CoerceParams must error rather than hand the driver
	// a heterogeneous []any it will reject.
	params := map[string]any{"scores": []any{float64(1), "oops"}}
	types := ParamTypes{"scores": schema.NewListConstraint(schema.NewFloatConstraint())}
	if _, err := CoerceParams(params, types); err == nil {
		t.Fatal("want error for a wrong-type List<Float> element, got nil")
	}
}

func TestCoerceParams_ReturnsIndependentMap(t *testing.T) {
	t.Parallel()
	// The returned map's top level must be independent of the input on every
	// path, so a caller mutating the result cannot corrupt its own input.
	t.Run("NoCoercionPath", func(t *testing.T) {
		t.Parallel()
		in := map[string]any{"a": int64(1)}
		out, err := CoerceParams(in, nil)
		if err != nil {
			t.Fatal(err)
		}
		out["a"] = int64(999)
		if in["a"] != int64(1) {
			t.Errorf("input mutated through returned map: in[a] = %v, want 1", in["a"])
		}
	})
	t.Run("ActiveCoercionPath", func(t *testing.T) {
		t.Parallel()
		in := map[string]any{"a": int64(1), "b": "x"}
		out, err := CoerceParams(in, ParamTypes{"a": schema.NewFloatConstraint()})
		if err != nil {
			t.Fatal(err)
		}
		out["b"] = "mutated"
		if in["b"] != "x" {
			t.Errorf("input mutated through returned map: in[b] = %v, want x", in["b"])
		}
	})
}

func TestCoerceParams_DeterministicError(t *testing.T) {
	t.Parallel()
	// With more than one failing key, the reported error must be stable across
	// runs (keys processed in sorted order), not a function of map iteration
	// order — so a coercion failure is reproducible and testable.
	types := ParamTypes{
		"a_date": schema.NewDateConstraint(),
		"z_date": schema.NewDateConstraint(),
	}
	params := map[string]any{"a_date": "not-a-date", "z_date": "also-bad"}

	var first string
	for i := range 64 {
		_, err := CoerceParams(params, types)
		if err == nil {
			t.Fatal("want a coercion error, got nil")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("non-deterministic error: run %d = %q, first = %q", i, err.Error(), first)
		}
	}
	if !strings.Contains(first, "a_date") {
		t.Errorf("error should name the lexicographically-first bad key (a_date): %q", first)
	}
}

func TestCoerceScalar_StrictRejectsUnrepairableInput(t *testing.T) {
	t.Parallel()
	// A Float, a Timestamp and a Date each refuse a value in another kind's
	// form, as a list element of that kind does.
	cases := []struct {
		name       string
		constraint schema.Constraint
		in         any
	}{
		{"Float/string", schema.NewFloatConstraint(), "1.5"},
		{"Float/bool", schema.NewFloatConstraint(), true},
		{"Timestamp/int", schema.NewTimestampConstraint(), int64(123)},
		{"Timestamp/bool", schema.NewTimestampConstraint(), true},
		{"Date/int", schema.NewDateConstraint(), int64(123)},
		{"Date/bool", schema.NewDateConstraint(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := coerceScalar(tc.constraint, tc.in); err == nil {
				t.Errorf("coerceScalar(%v, %#v): want error, got nil", tc.constraint, tc.in)
			}
		})
	}
}

func TestCoerceParams_StrictRejectsWrongScalarType(t *testing.T) {
	t.Parallel()
	// CoerceParams refuses a string at a Float, as the validator does.
	if _, err := CoerceParams(
		map[string]any{"principal": "not-a-number"},
		ParamTypes{"principal": schema.NewFloatConstraint()},
	); err == nil {
		t.Fatal("want error for a string under Float, got nil")
	}
	if _, err := CoerceParams(
		map[string]any{"observed_at": int64(123)},
		ParamTypes{"observed_at": schema.NewTimestampConstraint()},
	); err == nil {
		t.Fatal("want error for an int under Timestamp, got nil")
	}
}

func TestCoerceParams_TimestampCustomFormat(t *testing.T) {
	t.Parallel()
	// A Timestamp declared with a custom Go layout (Timestamp["…"]) is honored
	// end-to-end on the direct-Cypher path: a value valid under that layout but
	// not RFC3339 coerces to time.Time rather than erroring.
	out, err := CoerceParams(
		map[string]any{"logged_at": "2024-01-01 12:30:00"},
		ParamTypes{"logged_at": schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")},
	)
	if err != nil {
		t.Fatalf("custom-format Timestamp param should coerce: %v", err)
	}
	if _, ok := out["logged_at"].(time.Time); !ok {
		t.Fatalf("logged_at: got %T, want time.Time", out["logged_at"])
	}
}

func TestCoerceParams_TimestampListCustomFormat(t *testing.T) {
	t.Parallel()
	// The custom layout reaches list elements too (List<Timestamp["…"]>).
	out, err := CoerceParams(
		map[string]any{"times": []any{"2024-01-01 12:30:00", "2024-06-15 08:00:00"}},
		ParamTypes{"times": schema.NewListConstraint(schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05"))},
	)
	if err != nil {
		t.Fatalf("custom-format Timestamp list param should coerce: %v", err)
	}
	if _, ok := out["times"].([]time.Time); !ok {
		t.Fatalf("times: got %T, want []time.Time", out["times"])
	}
}

func TestCoerceScalar_TimestampCustomFormatDirect(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")
	got, err := coerceScalar(c, "2024-01-01 12:30:00")
	if err != nil {
		t.Fatalf("custom-format timestamp should parse: %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("got %T, want time.Time", got)
	}
	// The declared layout is exclusive (matching validation): a non-matching
	// string — even a valid RFC3339 one — errors.
	if _, err := coerceScalar(c, "2024-01-01T12:30:00Z"); err == nil {
		t.Error("RFC3339 string under a custom layout should error")
	}
}

func TestCoerceParams_VectorElementCoercion(t *testing.T) {
	t.Parallel()
	// A Vector value carried as []any{int64...} — a hand-built direct-Cypher
	// param — must element-coerce to []float64, the same
	// rule the List paths apply. A Vector is float-valued, so reaching the driver
	// as []any (or a list of int64) would be rejected against its FLOAT-list type.
	params := map[string]any{"embedding": []any{int64(1), int64(2), int64(3)}}
	types := ParamTypes{"embedding": schema.NewVectorConstraint(3)}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vec, ok := out["embedding"].([]float64)
	if !ok {
		t.Fatalf("Vector param not element-coerced: embedding is %T, want []float64", out["embedding"])
	}
	if len(vec) != 3 || vec[0] != 1 || vec[1] != 2 || vec[2] != 3 {
		t.Fatalf("Vector elements wrong: %#v", vec)
	}
}

func TestCoerceParams_ListValueUnderScalarConstraintErrors(t *testing.T) {
	t.Parallel()
	// A []any value declared under a scalar constraint is a shape mismatch: a
	// scalar param cannot hold a list. Coercion errors rather than silently
	// handing the driver a []any it will reject — the same fail-fast contract the
	// element-type checks already enforce, applied to the container shape.
	params := map[string]any{"score": []any{int64(1), int64(2)}}
	types := ParamTypes{"score": schema.NewFloatConstraint()}
	if _, err := CoerceParams(params, types); err == nil {
		t.Fatal("want error for a list value under a scalar Float constraint, got nil")
	}
}

// A type declaring a property literally named key_<pk> keeps it: the derived
// map describes the type's properties alone, so key_id is the declared String
// property and never the merge key a batch row spells the same way.
func TestParamTypes_AKeyNamedPropertyIsTheProperty(t *testing.T) {
	s := loadSchema(t, "temporal_pk.yammm")
	st, ok := s.Type("Order")
	if !ok {
		t.Fatal("Order type not found in temporal_pk.yammm")
	}
	if got := schema.ResolveAlias(ParamTypesForType(st)["key_id"]).Kind(); got != schema.KindString {
		t.Errorf("ParamTypesForType(Order)[%q] = %v; want KindString", "key_id", got)
	}
}

// coerceScalar and CoerceParams judge a hand-built constraint before they coerce
// anything, refusing what the DSL refuses and a DataType reference
// with no resolved constraint, so neither reaches the driver as a value in the
// wrong type.
func TestCoerceParams_RefusesAConstraintNoSchemaHolds(t *testing.T) {
	unresolved := schema.NewAliasConstraint("Day", nil)
	list := schema.NewListConstraint(schema.NewStringConstraint())
	for name, tc := range map[string]struct {
		c    schema.Constraint
		raw  any
		want string
	}{
		"an unresolved reference":             {unresolved, "2026-01-01", `datatype "Day" resolves to no constraint`},
		"a List of an unresolved reference":   {schema.NewListConstraint(unresolved), []any{"2026-01-01"}, `datatype "Day" resolves to no constraint`},
		"a pointer to a List":                 {&list, []any{"a"}, "none this package constructs"},
		"inverted bounds, whatever the value": {schema.IntegerBetween(5, 1), nil, "integer bounds inverted"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := CoerceParams(map[string]any{"v": tc.raw}, ParamTypes{"v": tc.c})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), `param type "v"`) {
				t.Errorf("CoerceParams = %v, %v; want an error naming the key and %q", got, err, tc.want)
			}
		})
	}
}

// A constraint judged sound passes, resolved or not wrapped in a reference.
func TestCoerceParams_CoercesThroughAResolvedReference(t *testing.T) {
	day := schema.NewAliasConstraint("Day", schema.NewDateConstraint())
	got, err := CoerceParams(map[string]any{"days": []any{"2026-01-01"}}, ParamTypes{"days": schema.NewListConstraint(day)})
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	if _, ok := got["days"].([]dbtype.Date); !ok {
		t.Errorf("days = %T; want []dbtype.Date", got["days"])
	}
}

// CoerceParams judges every constraint types holds before it reads params: a
// refused key params does not hold is an error, params empty or not, and of
// several refused keys the first in sorted order is named. A nil constraint is
// "no type to coerce against" and passes its value through.
func TestCoerceParams_JudgesEveryTypeBeforeAnyValue(t *testing.T) {
	bad := schema.IntegerBetween(5, 1)
	for name, params := range map[string]map[string]any{
		"a key params does not hold": {"a": int64(1)},
		"empty params":               {},
		"nil params":                 nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := CoerceParams(params, ParamTypes{"zz": bad})
			if err == nil || !strings.Contains(err.Error(), `param type "zz"`) {
				t.Errorf("CoerceParams = %v; want an error naming \"zz\"", err)
			}
		})
	}
	t.Run("the first refused key in sorted order", func(t *testing.T) {
		types := ParamTypes{"d4": bad, "b2": bad, "c3": bad, "e5": bad}
		for range 20 {
			_, err := CoerceParams(map[string]any{}, types)
			if err == nil || !strings.Contains(err.Error(), `param type "b2"`) {
				t.Fatalf("CoerceParams = %v; want an error naming \"b2\"", err)
			}
		}
	})
	t.Run("a nil constraint passes its value through", func(t *testing.T) {
		got, err := CoerceParams(map[string]any{"x": "s"}, ParamTypes{"x": nil})
		if err != nil || got["x"] != "s" {
			t.Errorf("CoerceParams = %v, %v; want x passed through", got, err)
		}
	})
}

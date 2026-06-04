package neo4j

import (
	"strings"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

func TestCoerce_FloatIntRepair(t *testing.T) {
	got, err := Coerce(schema.NewFloatConstraint(), int64(1860000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f, ok := got.(float64); !ok || f != 1860000.0 {
		t.Fatalf("got %#v (%T), want float64(1860000)", got, got)
	}
}

func TestCoerce_TimestampParse(t *testing.T) {
	got, err := Coerce(schema.NewTimestampConstraint(), "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("got %T, want time.Time", got)
	}
}

func TestCoerce_DateParse(t *testing.T) {
	got, err := Coerce(schema.NewDateConstraint(), "2024-06-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(dbtype.Date); !ok {
		t.Fatalf("got %T, want dbtype.Date", got)
	}
}

func TestCoerce_DateFromTime(t *testing.T) {
	got, err := Coerce(schema.NewDateConstraint(), time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(dbtype.Date); !ok {
		t.Fatalf("got %T, want dbtype.Date from a time.Time (a Date value must not reach Neo4j as ZONED DATETIME)", got)
	}
}

func TestCoerce_TimestampParseFailureErrors(t *testing.T) {
	if _, err := Coerce(schema.NewTimestampConstraint(), "not-a-timestamp"); err == nil {
		t.Fatal("want error on unparseable timestamp string, got nil")
	}
}

func TestCoerce_DateParseFailureErrors(t *testing.T) {
	if _, err := Coerce(schema.NewDateConstraint(), "06/15/2024"); err == nil {
		t.Fatal("want error on unparseable date string, got nil")
	}
}

func TestCoerce_PassThroughString(t *testing.T) {
	got, err := Coerce(schema.NewStringConstraint(), "hello")
	if err != nil || got != "hello" {
		t.Fatalf("string pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerce_NilPassThrough(t *testing.T) {
	got, err := Coerce(schema.NewTimestampConstraint(), nil)
	if err != nil || got != nil {
		t.Fatalf("nil pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerce_NilConstraintPassThrough(t *testing.T) {
	t.Parallel()
	// A nil constraint means "no type to coerce against": pass the value through,
	// matching coerceValue. A bad/unhandled kind is unreachable — schema.Constraint
	// is a sealed interface, so every value carries a valid kind, and the
	// exhaustiveness lint guards the switch.
	got, err := Coerce(nil, "x")
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
	if _, ok := out["native"].(time.Time); !ok {
		t.Fatalf("native time.Time: want time.Time passthrough, got %T", out["native"])
	}
	if _, ok := out["date"].(dbtype.Date); !ok {
		t.Fatalf("date: want dbtype.Date, got %T", out["date"])
	}
	if out["label"] != "unrelated" {
		t.Fatalf("string under KindString should pass through, got %#v", out["label"])
	}
}

func TestCoerceParams_BadTemporalErrors(t *testing.T) {
	// yammm's Coerce returns an error on an unparseable Timestamp/Date; the
	// param boundary surfaces it (rdata's mirror passed these through).
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

func TestCoerceParams_NestedRowsAndError(t *testing.T) {
	types := ParamTypes{
		"rows.principal_amount": schema.NewFloatConstraint(),
		"rows.closing_date":     schema.NewDateConstraint(),
	}
	params := map[string]any{"rows": []map[string]any{
		{"id": "a", "principal_amount": int64(5), "closing_date": "2026-04-22"},
	}}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	row := out["rows"].([]map[string]any)[0]
	if _, ok := row["principal_amount"].(float64); !ok {
		t.Fatalf("principal_amount not coerced to float64: %T", row["principal_amount"])
	}
	if _, ok := row["closing_date"].(dbtype.Date); !ok {
		t.Fatalf("closing_date not coerced to dbtype.Date: %T", row["closing_date"])
	}
	if row["id"] != "a" {
		t.Fatalf("untyped row field should pass through, got %#v", row["id"])
	}

	bad := map[string]any{"rows": []map[string]any{{"closing_date": "nope"}}}
	if _, err := CoerceParams(bad, types); err == nil {
		t.Fatal("want error when a row value fails coercion, got nil")
	}
}

func TestCoerceParams_NestedMap(t *testing.T) {
	params := map[string]any{
		"updates": map[string]any{
			"closing_date": "2026-04-22",
			"principal":    int64(1860000),
			"passthrough":  "unchanged",
		},
	}
	types := ParamTypes{
		"updates.closing_date": schema.NewDateConstraint(),
		"updates.principal":    schema.NewFloatConstraint(),
	}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updates := out["updates"].(map[string]any)
	if _, ok := updates["closing_date"].(dbtype.Date); !ok {
		t.Fatalf("updates.closing_date: want dbtype.Date, got %T", updates["closing_date"])
	}
	if _, ok := updates["principal"].(float64); !ok {
		t.Fatalf("updates.principal: want float64, got %T", updates["principal"])
	}
	if updates["passthrough"] != "unchanged" {
		t.Fatalf("updates.passthrough should pass through, got %#v", updates["passthrough"])
	}
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

	// Prefix "" → bare property-name keys. The stored value is the full
	// constraint; assert against its resolved kind.
	flat := ParamTypesForType(st, "")
	if schema.ResolveAlias(flat["score"]).Kind() != schema.KindFloat {
		t.Errorf("score: got %v, want KindFloat", flat["score"])
	}
	if schema.ResolveAlias(flat["created_at"]).Kind() != schema.KindTimestamp {
		t.Errorf("created_at: got %v, want KindTimestamp", flat["created_at"])
	}
	if schema.ResolveAlias(flat["birth_date"]).Kind() != schema.KindDate {
		t.Errorf("birth_date: got %v, want KindDate", flat["birth_date"])
	}

	// Non-empty prefix → dot-joined keys matching CoerceParams/coerceNested.
	nested := ParamTypesForType(st, "rows")
	if schema.ResolveAlias(nested["rows.score"]).Kind() != schema.KindFloat {
		t.Errorf("rows.score: got %v, want KindFloat", nested["rows.score"])
	}
	if _, present := nested["score"]; present {
		t.Error("non-empty prefix must dot-join; a bare 'score' key must not be present")
	}

	// End-to-end: the derived map feeds CoerceParams and a nested row coerces.
	// This fails if ParamTypesForType used bare concatenation instead of dotting.
	out, err := CoerceParams(
		map[string]any{"rows": []map[string]any{{"score": int64(3)}}},
		ParamTypesForType(st, "rows"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, isFloat := out["rows"].([]map[string]any)[0]["score"].(float64); !isFloat {
		t.Fatalf("end-to-end: rows[0].score not coerced to float64: %T",
			out["rows"].([]map[string]any)[0]["score"])
	}
}

func TestCoerceParams_ListElementCoercion(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity type not found in list_properties.yammm")
	}

	// A direct-Cypher UNWIND $rows write of node properties. List values arrive
	// as []any with un-coerced elements (int64 for Float, strings for
	// Date/Timestamp) — exactly what a JSON decode or a hand-built param map
	// produces. CoerceParams must element-coerce them, not pass them through.
	params := map[string]any{"rows": []map[string]any{
		{
			"id":     "e1",
			"ratios": []any{int64(1), int64(2)},         // List<Float>
			"dates":  []any{"2024-01-01", "2024-06-15"}, // List<Date>
			"times":  []any{"2024-01-01T00:00:00Z"},     // List<Timestamp>
			"tags":   []any{"x", "y"},                   // List<String>
		},
	}}

	out, err := CoerceParams(params, ParamTypesForType(st, "rows"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	row := out["rows"].([]map[string]any)[0]

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
	params := map[string]any{"rows": []map[string]any{
		{"id": "e1", "dates": []any{"not-a-date"}},
	}}
	if _, err := CoerceParams(params, ParamTypesForType(st, "rows")); err == nil {
		t.Fatal("want error for an unparseable List<Date> element, got nil")
	}
}

func TestCoerceParams_IntegerListWidthRepair(t *testing.T) {
	t.Parallel()
	// The direct-Cypher boundary: a List<Integer> param built with mixed or narrow
	// int widths element-coerces to []int64 rather than erroring — symmetric with
	// the List<Float> width repair, closing the Integer-vs-Float list asymmetry.
	params := map[string]any{"counts": []any{int(1), int32(2), uint16(3)}}
	types := ParamTypes{"counts": schema.NewListConstraint(schema.NewIntegerConstraint())}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := out["counts"].([]int64)
	if !ok {
		t.Fatalf("counts: got %T, want []int64", out["counts"])
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("counts elements wrong: %#v", got)
	}
}

func TestCoerce_FloatFromAllNumericTypes(t *testing.T) {
	t.Parallel()
	// Every Go integer width and float32 must repair to float64, so a Float
	// property never reaches the driver as a non-float numeric (Neo4j rejects an
	// integer value against an IS :: FLOAT constraint).
	cases := []struct {
		name string
		in   any
	}{
		{"int", int(5)},
		{"int8", int8(5)},
		{"int16", int16(5)},
		{"int32", int32(5)},
		{"int64", int64(5)},
		{"uint", uint(5)},
		{"uint8", uint8(5)},
		{"uint16", uint16(5)},
		{"uint32", uint32(5)},
		{"uint64", uint64(5)},
		{"float32", float32(5)},
		{"float64", float64(5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Coerce(schema.NewFloatConstraint(), tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			f, ok := got.(float64)
			if !ok {
				t.Fatalf("%s: got %T, want float64", tc.name, got)
			}
			if f != 5.0 {
				t.Errorf("%s: got %v, want 5.0", tc.name, f)
			}
		})
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

func TestCoerce_StrictRejectsUnrepairableInput(t *testing.T) {
	t.Parallel()
	// The transforming kinds (Float/Date/Timestamp) error on an input they can
	// neither pass through as already-driver-native nor repair, rather than
	// silently handing a wrong-typed value to the driver — matching the list path.
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
			if _, err := Coerce(tc.constraint, tc.in); err == nil {
				t.Errorf("Coerce(%v, %#v): want error, got nil", tc.constraint, tc.in)
			}
		})
	}
}

func TestCoerce_IdempotentOnCanonicalTypes(t *testing.T) {
	t.Parallel()
	// An already-driver-native value for a transforming kind passes through
	// unchanged: coercion is idempotent and never rejects a correct value.
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	d := dbtype.Date(time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))

	if got, err := Coerce(schema.NewFloatConstraint(), float64(3.14)); err != nil || got != float64(3.14) {
		t.Errorf("Float float64: got %#v err %v, want 3.14", got, err)
	}
	if got, err := Coerce(schema.NewTimestampConstraint(), ts); err != nil {
		t.Errorf("Timestamp time.Time: unexpected err %v", err)
	} else if gt, ok := got.(time.Time); !ok || !gt.Equal(ts) {
		t.Errorf("Timestamp time.Time: got %#v, want passthrough of %v", got, ts)
	}
	if got, err := Coerce(schema.NewDateConstraint(), d); err != nil {
		t.Errorf("Date dbtype.Date: unexpected err %v", err)
	} else if gd, ok := got.(dbtype.Date); !ok || !time.Time(gd).Equal(time.Time(d)) {
		t.Errorf("Date dbtype.Date: got %#v, want passthrough of %v", got, d)
	}
}

func TestCoerceParams_StrictRejectsWrongScalarType(t *testing.T) {
	t.Parallel()
	// The direct-Cypher boundary: a scalar param whose Go type the kind can
	// neither pass through nor repair now errors rather than reaching the driver
	// wrong-typed.
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

func TestCoerce_TimestampCustomFormatDirect(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")
	got, err := Coerce(c, "2024-01-01 12:30:00")
	if err != nil {
		t.Fatalf("custom-format timestamp should parse: %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("got %T, want time.Time", got)
	}
	// The declared layout is exclusive (matching validation): a non-matching
	// string — even a valid RFC3339 one — errors.
	if _, err := Coerce(c, "2024-01-01T12:30:00Z"); err == nil {
		t.Error("RFC3339 string under a custom layout should error")
	}
}

func TestCoerceParams_VectorElementCoercion(t *testing.T) {
	t.Parallel()
	// A Vector value carried as []any{int64...} — the shape a .ys snapshot load
	// produces (NormalizeValue narrows whole-number JSON to int64), or a
	// hand-built direct-Cypher param — must element-coerce to []float64, the same
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

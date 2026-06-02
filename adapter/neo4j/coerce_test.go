package neo4j

import (
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

func TestCoerce_FloatIntRepair(t *testing.T) {
	got, err := Coerce(schema.KindFloat, int64(1860000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f, ok := got.(float64); !ok || f != 1860000.0 {
		t.Fatalf("got %#v (%T), want float64(1860000)", got, got)
	}
}

func TestCoerce_TimestampParse(t *testing.T) {
	got, err := Coerce(schema.KindTimestamp, "2024-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("got %T, want time.Time", got)
	}
}

func TestCoerce_DateParse(t *testing.T) {
	got, err := Coerce(schema.KindDate, "2024-06-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(dbtype.Date); !ok {
		t.Fatalf("got %T, want dbtype.Date", got)
	}
}

func TestCoerce_DateFromTime(t *testing.T) {
	got, err := Coerce(schema.KindDate, time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(dbtype.Date); !ok {
		t.Fatalf("got %T, want dbtype.Date from a time.Time (a Date value must not reach Neo4j as ZONED DATETIME)", got)
	}
}

func TestCoerce_TimestampParseFailureErrors(t *testing.T) {
	if _, err := Coerce(schema.KindTimestamp, "not-a-timestamp"); err == nil {
		t.Fatal("want error on unparseable timestamp string, got nil")
	}
}

func TestCoerce_DateParseFailureErrors(t *testing.T) {
	if _, err := Coerce(schema.KindDate, "06/15/2024"); err == nil {
		t.Fatal("want error on unparseable date string, got nil")
	}
}

func TestCoerce_PassThroughString(t *testing.T) {
	got, err := Coerce(schema.KindString, "hello")
	if err != nil || got != "hello" {
		t.Fatalf("string pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerce_NilPassThrough(t *testing.T) {
	got, err := Coerce(schema.KindTimestamp, nil)
	if err != nil || got != nil {
		t.Fatalf("nil pass-through: got %#v, err %v", got, err)
	}
}

func TestCoerce_UnhandledKindErrors(t *testing.T) {
	// A ConstraintKind past the enumerated set must error rather than
	// silently passing a value through to the driver.
	if _, err := Coerce(schema.ConstraintKind(255), "x"); err == nil {
		t.Fatal("want error on an unhandled ConstraintKind, got nil")
	}
}

func TestCoerceParams_FloatRepairAndUnknownKey(t *testing.T) {
	params := map[string]any{
		"principal": int64(1860000),
		"as_str":    "passthrough",
		"untouched": int64(7),
	}
	types := ParamTypes{
		"principal": schema.KindFloat,
		"as_str":    schema.KindFloat, // string under Float falls through unchanged
		"missing":   schema.KindFloat, // names a key params doesn't carry: no-op
	}
	out, err := CoerceParams(params, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f, ok := out["principal"].(float64); !ok || f != 1860000.0 {
		t.Fatalf("principal: got %#v (%T), want float64(1860000)", out["principal"], out["principal"])
	}
	if out["as_str"] != "passthrough" {
		t.Fatalf("string under KindFloat should pass through, got %#v", out["as_str"])
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
		"ts":      schema.KindTimestamp,
		"ts_nano": schema.KindTimestamp,
		"native":  schema.KindTimestamp,
		"date":    schema.KindDate,
		"label":   schema.KindString,
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
		ParamTypes{"ts": schema.KindTimestamp},
	); err == nil {
		t.Fatal("want error for unparseable Timestamp param, got nil")
	}
	if _, err := CoerceParams(
		map[string]any{"d": "2026/04/22"},
		ParamTypes{"d": schema.KindDate},
	); err == nil {
		t.Fatal("want error for unparseable Date param, got nil")
	}
}

func TestCoerceParams_NilTypesIsZeroCost(t *testing.T) {
	params := map[string]any{"x": int64(1), "y": "hi"}
	out, err := CoerceParams(params, nil)
	if err != nil || len(out) != 2 {
		t.Fatalf("nil types should pass through unchanged: out=%v err=%v", out, err)
	}
	out2, err := CoerceParams(params, ParamTypes{})
	if err != nil || len(out2) != 2 {
		t.Fatalf("empty types should pass through unchanged: out=%v err=%v", out2, err)
	}
}

func TestCoerceParams_NestedRowsAndError(t *testing.T) {
	types := ParamTypes{
		"rows.principal_amount": schema.KindFloat,
		"rows.closing_date":     schema.KindDate,
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
		"updates.closing_date": schema.KindDate,
		"updates.principal":    schema.KindFloat,
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
	types := ParamTypes{"deactivated_at": schema.KindTimestamp}
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

func TestCoerceParams_TopLevelListPassThrough(t *testing.T) {
	// A top-level []any param passes through untouched; element coercion for
	// List properties happens in the snapshot write path, not at this boundary.
	params := map[string]any{"tags": []any{"x", "y"}}
	out, err := CoerceParams(params, ParamTypes{"tags": schema.KindList})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := out["tags"].([]any)
	if !ok || len(got) != 2 || got[0] != "x" {
		t.Fatalf("tags []any should pass through unchanged, got %#v", out["tags"])
	}
}

func TestParamTypesForType(t *testing.T) {
	s := loadSchema(t, "basic.yammm")
	st, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity type not found in basic.yammm")
	}

	// Prefix "" → bare property-name keys.
	flat := ParamTypesForType(st, "")
	if flat["score"] != schema.KindFloat {
		t.Errorf("score: got %v, want KindFloat", flat["score"])
	}
	if flat["created_at"] != schema.KindTimestamp {
		t.Errorf("created_at: got %v, want KindTimestamp", flat["created_at"])
	}
	if flat["birth_date"] != schema.KindDate {
		t.Errorf("birth_date: got %v, want KindDate", flat["birth_date"])
	}

	// Non-empty prefix → dot-joined keys matching CoerceParams/coerceNested.
	nested := ParamTypesForType(st, "rows")
	if nested["rows.score"] != schema.KindFloat {
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

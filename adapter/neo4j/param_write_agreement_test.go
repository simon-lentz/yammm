package neo4j

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
)

const agreementSchema = `schema "agree"

type T {
	id String primary
	s String[1, 5]
	i Integer[0, 10]
	f Float
	b Boolean
	ts Timestamp
	d Date
	u UUID
	e Enum["a", "b"]
	p Pattern["^a.b$"]
	v Vector[2]
	ls List<String>
	li List<Integer>
	lf List<Float>
	lt List<Timestamp>
	ld List<Date>
	lu List<UUID>
}
`

// sameSent reports whether a and b are one driver value: an instant by its
// moment, zone name and offset, a date by its day, and any other value by
// reflect.DeepEqual.
func sameSent(a, b any) bool {
	sameTime := func(x, y time.Time) bool {
		xn, xo := x.Zone()
		yn, yo := y.Zone()
		return x.Equal(y) && xn == yn && xo == yo && x.Location().String() == y.Location().String()
	}
	switch x := a.(type) {
	case time.Time:
		y, ok := b.(time.Time)
		return ok && sameTime(x, y)
	case []time.Time:
		y, ok := b.([]time.Time)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameTime(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		return ok && x == y && math.Signbit(x) == math.Signbit(y)
	}
	return reflect.DeepEqual(a, b)
}

// CoerceParams sends for a value what the write path sends for the instance the
// validator builds from it, and refuses the value when the validator refuses
// it, at every kind and in every Go form, a map and rows included.
func TestCoerceParams_SendsWhatTheWritePathSendsForTheValidatedValue(t *testing.T) {
	t.Parallel()
	s := loadInline(t, agreementSchema)
	typ, _ := s.Type("T")
	validator := instance.NewValidator(s)
	a := New()
	shape, res := a.ShapeForSchema(t.Context(), s)
	if res.Err() != nil {
		t.Fatalf("ShapeForSchema: %v", res.Err())
	}
	types := ParamTypesForType(typ)

	type (
		namedText  string
		namedInt   int32
		namedFloat float32
		namedFlag  bool
	)
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	utc := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)
	zoned := time.Date(2024, 7, 2, 23, 4, 5, 0, newYork)
	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("X", 3600))
	seconds := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 3600+59))
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	wide := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 24*3600))
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	const upperID = "12345678-1234-1234-1234-123456789ABC"
	text, n := "abc", int64(5)
	var nilText *string

	rows := map[string][]any{
		"s": {"abc", namedText("abc"), "", "toolong", 5, json.Number("5"), &text, nilText, []any{"a"}, map[string]string{"a": "b"}, map[string]any{"a": "b"}, map[string]any{}, []map[string]any{{"a": "b"}}},
		"i": {int64(5), 5, int32(5), uint8(5), uint64(5), namedInt(5), json.Number("5"), 11, -1, 5.0, json.Number("5.0"), uint64(1 << 63), "5", &n},
		"f": {1.5, float32(1.5), namedFloat(1.5), int64(3), 3, uint64(1 << 63), json.Number("-0"), json.Number("0.1"), json.Number("99999999999999999999"), json.Number("1e400"), math.NaN(), math.Inf(1), "1.5"},
		"b": {true, false, namedFlag(true), "true", 1},
		"ts": {
			"2024-01-02T03:04:05Z", "2024-01-02T03:04:05.123456789+02:00", "2024-01-02T03:04:05+00:00", "2024-01-02T03:04:05-07:00",
			"2024-01-02T03:04:05+18:00", "2024-01-02T03:04:05-18:00", "2024-01-02T03:04:05+24:60", "2024-01-02T03:04:05+24:00",
			utc, zoned, fixed, seconds, utc.In(time.Local), far, wide, "nope", "2024-01-02", "2024-01-02 03:04", "2024-01-02T03:04:05", &utc, 5,
		},
		"d":  {"2024-01-02", utc, zoned, far, "01/02/2024", "2024-01-02T03:04:05Z", 5},
		"u":  {id.String(), upperID, "{" + upperID + "}", id, "zz", &id, 5},
		"e":  {"a", namedText("b"), "c", 5},
		"p":  {"axb", "zzz", 5},
		"v":  {[]any{1.0, 2.0}, []any{int64(1), 2.5}, []float64{1, 2}, []float32{1, 2}, []any{json.Number("-0"), 1}, []any{1.0}, []any{1.0, nil}, []any{1.0, "x"}, []any{1.0, math.NaN()}, 1.0},
		"ls": {[]any{"a"}, []string{"a", "b"}, []any{}, []namedText{"a"}, []any{"a", 5}, []any{"a", nil}, []any{"a", &text}, "a", map[string]any{"a": "b"}, []map[string]any{{"a": "b"}}, []map[string]any{}, []any{map[string]any{"a": "b"}, nil}},
		"li": {[]any{int64(1), int32(2), json.Number("3")}, []int{1, 2}, []any{1.5}, []any{json.Number("1.0")}},
		"lf": {[]any{int64(1), 2.5, json.Number("-0")}, []float64{1}, []any{"1"}},
		"lt": {[]any{"2024-01-02T03:04:05Z", utc, zoned}, []time.Time{utc, fixed}, []any{"nope"}},
		"ld": {[]any{"2024-01-02", zoned}, []time.Time{utc}, []any{"01/02/2024"}},
		"lu": {[]any{upperID, id}, []uuid.UUID{id}, []any{"zz"}},
	}
	for prop, values := range rows {
		for _, v := range values {
			params, coerceErr := CoerceParams(map[string]any{prop: v}, types)
			valid, vres := validator.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", prop: v}})
			if vres.Err() != nil {
				if coerceErr == nil {
					t.Errorf("%s = %#v: the validator refuses it (%s) and CoerceParams returned %#v", prop, v, vres, params[prop])
				} else if !errors.Is(coerceErr, ErrUnrepresentable) {
					t.Errorf("%s = %#v: the refusal %v does not match ErrUnrepresentable", prop, v, coerceErr)
				}
				continue
			}
			if coerceErr != nil {
				t.Errorf("%s = %#v: the validator accepts it and CoerceParams refuses it: %v", prop, v, coerceErr)
				continue
			}
			g := graph.New(s)
			if res := g.Add(t.Context(), valid); res.Err() != nil {
				t.Fatalf("%s = %#v: graph.Add: %v", prop, v, res.Err())
			}
			nodes, err := a.BatchNodeQueries(t.Context(), g.Snapshot(), shape)
			if err != nil {
				t.Errorf("%s = %#v: the write path refuses the validated instance: %v", prop, v, err)
				continue
			}
			if sent := sentProp(nodes, prop); !sameSent(params[prop], sent) {
				t.Errorf("%s = %#v: CoerceParams sends %#v (%T), the write path %#v (%T)", prop, v, params[prop], params[prop], sent, sent)
			}
		}
	}
}

// A dbtype.Date is the driver's own date: CoerceParams passes one at a Date,
// with no time of day, where the validator refuses it. It is refused at any
// other kind and inside a list, as the validator refuses it there.
func TestCoerceParams_ADriverDatePassesAtADateAlone(t *testing.T) {
	t.Parallel()
	s := loadInline(t, agreementSchema)
	typ, _ := s.Type("T")
	types := ParamTypesForType(typ)
	day := dbtype.Date(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
	got, err := CoerceParams(map[string]any{"d": dbtype.Date(time.Date(2024, 1, 2, 23, 30, 0, 0, time.FixedZone("X", -3600)))}, types)
	if err != nil || !sameSent(got["d"], day) {
		t.Errorf("a dbtype.Date at a Date = %#v, %v; want %#v", got["d"], err, day)
	}
	if _, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "d": day}}); res.Err() == nil {
		t.Error("control: the validator accepts a dbtype.Date at a Date")
	}
	for prop, v := range map[string]any{"s": day, "ts": day, "ld": []any{day}, "lt": []dbtype.Date{day}} {
		if got, err := CoerceParams(map[string]any{prop: v}, types); !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s = %#v: CoerceParams = %#v, %v; want a refusal that matches ErrUnrepresentable", prop, v, got[prop], err)
		}
	}
	if got, err := CoerceParams(map[string]any{"ld": []dbtype.Date{day}}, types); !errors.Is(err, ErrUnrepresentable) {
		t.Errorf("a []dbtype.Date at a List<Date>: CoerceParams = %#v, %v; want a refusal", got["ld"], err)
	}
	// At another kind the refusal is the validator's own, as for any other value.
	for _, prop := range []string{"s", "ts"} {
		want := value.Check(day, types[prop])
		_, err := CoerceParams(map[string]any{prop: day}, types)
		if want == nil || err == nil || !strings.Contains(err.Error(), want.Error()) {
			t.Errorf("%s: CoerceParams = %v; want the validator's refusal %v", prop, err, want)
		}
	}
}

package neo4j

import (
	"errors"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
)

const offsetBoundSchema = `schema "bound"

type T {
	id String primary
	at Timestamp
	ats List<Timestamp>
}
`

// offsetRefusal is the coercion's refusal of text at offset, less the place.
func offsetRefusal(text, offset string) string {
	return "coerce Timestamp: cannot store " + diag.QuoteValue(text) + ": its UTC offset " + offset + " is past the 18 hours the server holds"
}

// The server holds an instant at a UTC offset of 18 hours at most, and the
// validator takes a wider one. An instant past the bound is refused by name on
// the write path and by CoerceParams, and one at the bound is sent at its
// offset. A time.Time is judged by the offset its stored text holds, which
// drops the offset's seconds.
func TestCoerce_AnOffsetPastEighteenHoursIsRefusedOnBothPaths(t *testing.T) {
	t.Parallel()
	s := loadInline(t, offsetBoundSchema)
	typ, _ := s.Type("T")
	types := ParamTypesForType(typ)
	validator := instance.NewValidator(s)
	a := New()
	shape, res := a.ShapeForSchema(t.Context(), s)
	if res.Err() != nil {
		t.Fatalf("ShapeForSchema: %v", res.Err())
	}
	const eighteen = 18 * 60 * 60
	atOffset := func(seconds int) time.Time {
		return time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", seconds))
	}
	for _, row := range []struct {
		prop    string
		val     any
		sent    int    // the offset sent, for a scalar that is sent
		refused string // the refusal less its place, for a value that is refused
	}{
		{"at", "2026-08-19T12:00:00+18:00", eighteen, ""},
		{"at", "2026-08-19T12:00:00-18:00", -eighteen, ""},
		{"at", "2026-08-19T12:00:00+18:01", 0, offsetRefusal("2026-08-19T12:00:00+18:01", "+18:01")},
		{"at", "2026-08-19T12:00:00-18:01", 0, offsetRefusal("2026-08-19T12:00:00-18:01", "-18:01")},
		{"at", "2026-08-19T12:00:00+23:59", 0, offsetRefusal("2026-08-19T12:00:00+23:59", "+23:59")},
		{"at", "2026-08-19T12:00:00.000000001-23:59", 0, offsetRefusal("2026-08-19T12:00:00.000000001-23:59", "-23:59")},
		{"at", atOffset(eighteen), eighteen, ""},
		{"at", atOffset(-eighteen), -eighteen, ""},
		{"at", atOffset(eighteen + 59), eighteen, ""},
		{"at", atOffset(-eighteen - 59), -eighteen, ""},
		{"at", atOffset(eighteen + 60), 0, offsetRefusal("2026-08-19T12:00:00+18:01", "+18:01")},
		{"at", atOffset(-eighteen - 60), 0, offsetRefusal("2026-08-19T12:00:00-18:01", "-18:01")},
		{"ats", []any{"2026-08-19T12:00:00+18:00", "2026-08-19T12:00:00-18:00"}, 0, ""},
		{"ats", []any{"2026-08-19T12:00:00Z", "2026-08-19T12:00:00-18:01"}, 0, "list element 1: " + offsetRefusal("2026-08-19T12:00:00-18:01", "-18:01")},
		{"ats", []any{atOffset(eighteen + 60)}, 0, "list element 0: " + offsetRefusal("2026-08-19T12:00:00+18:01", "+18:01")},
	} {
		valid, vres := validator.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", row.prop: row.val}})
		if vres.Err() != nil {
			t.Fatalf("control: %s = %#v: the validator refuses it: %s", row.prop, row.val, vres)
		}
		g := graph.New(s)
		if res := g.Add(t.Context(), valid); res.Err() != nil {
			t.Fatalf("%s = %#v: graph.Add: %v", row.prop, row.val, res.Err())
		}
		nodes, writeErr := a.BatchNodeQueries(t.Context(), g.Snapshot(), shape)
		params, paramErr := CoerceParams(map[string]any{row.prop: row.val}, types)
		if row.refused == "" {
			sent := sentProp(nodes, row.prop)
			if writeErr != nil || paramErr != nil {
				t.Errorf("%s = %#v: the write path = %v, CoerceParams = %v; want it sent on both", row.prop, row.val, writeErr, paramErr)
			} else if !sameSent(params[row.prop], sent) {
				t.Errorf("%s = %#v: CoerceParams sends %#v, the write path %#v", row.prop, row.val, params[row.prop], sent)
			}
			if at, ok := sent.(time.Time); ok {
				if _, offset := at.Zone(); offset != row.sent {
					t.Errorf("%s = %#v: sent at offset %d; want %d", row.prop, row.val, offset, row.sent)
				}
			}
			continue
		}
		for path, got := range map[string]struct {
			err  error
			want string
		}{
			"the write path": {writeErr, `type "T": property "` + row.prop + `": ` + row.refused},
			"CoerceParams":   {paramErr, `param "` + row.prop + `": ` + row.refused},
		} {
			if !errors.Is(got.err, ErrUnrepresentable) || got.err.Error() != got.want {
				t.Errorf("%s = %#v: %s = %v; want a refusal that matches ErrUnrepresentable and reads %s", row.prop, row.val, path, got.err, got.want)
			}
		}
	}
}

const offsetBoundPlacesSchema = `schema "places"

type Target {
	at Timestamp primary
}

part type Note {
	seen Timestamp
}

type Holder {
	id String primary
	--> LINK (_:many) Target {
		when Timestamp
	}
	*-> NOTES (_:many) Note
}
`

// The bound holds wherever the write path coerces an instant: a primary key,
// an edge's endpoint key, an edge property and a part's property.
func TestCoerce_AnOffsetPastEighteenHoursIsRefusedAtEveryPlace(t *testing.T) {
	t.Parallel()
	s := loadInline(t, offsetBoundPlacesSchema)
	validator := instance.NewValidator(s)
	a := New()
	shape, res := a.ShapeForSchema(t.Context(), s)
	if res.Err() != nil {
		t.Fatalf("ShapeForSchema: %v", res.Err())
	}
	const (
		atBound = "2026-08-19T12:00:00+18:00"
		past    = "2026-08-19T12:00:00+18:01"
	)
	refusal := offsetRefusal(past, "+18:01")
	for _, row := range []struct {
		name            string
		key, when, seen string
		nodes, edges    string
	}{
		{"every instant at the bound", atBound, atBound, atBound, "", ""},
		{"a primary key and an endpoint key", past, atBound, atBound, `type "Target": primary key "at": ` + refusal, `target string://list_element.yammm:Target: primary key "at": ` + refusal},
		{"an edge property", atBound, past, atBound, "", `relation "LINK": property "when": ` + refusal},
		{"a part's property", atBound, atBound, past, `part places__Note: property "seen": ` + refusal, ""},
	} {
		target, tres := validator.ValidateOne(t.Context(), "Target", instance.RawInstance{Properties: map[string]any{"at": row.key}})
		holder, hres := validator.ValidateOne(t.Context(), "Holder", instance.RawInstance{Properties: map[string]any{
			"id":    "h",
			"link":  []any{map[string]any{"_target_at": row.key, "when": row.when}},
			"notes": []any{map[string]any{"seen": row.seen}},
		}})
		if tres.Err() != nil || hres.Err() != nil {
			t.Fatalf("control: %s: the validator refuses it: %s %s", row.name, tres, hres)
		}
		g := graph.New(s)
		for _, valid := range []instance.ValidInstance{target, holder} {
			if res := g.Add(t.Context(), valid); res.Err() != nil {
				t.Fatalf("%s: graph.Add: %v", row.name, res.Err())
			}
		}
		_, nodeErr := a.BatchNodeQueries(t.Context(), g.Snapshot(), shape)
		_, edgeErr := a.BatchEdgeQueries(t.Context(), g.Snapshot(), shape)
		for path, got := range map[string]struct {
			err  error
			want string
		}{
			"BatchNodeQueries": {nodeErr, row.nodes},
			"BatchEdgeQueries": {edgeErr, row.edges},
		} {
			switch {
			case got.want == "" && got.err != nil:
				t.Errorf("%s: %s = %v; want it sent", row.name, path, got.err)
			case got.want != "" && (!errors.Is(got.err, ErrUnrepresentable) || got.err.Error() != got.want):
				t.Errorf("%s: %s = %v; want a refusal that matches ErrUnrepresentable and reads %s", row.name, path, got.err, got.want)
			}
		}
	}
}

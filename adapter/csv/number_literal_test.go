package csv

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const numberCellSchema = `schema "n"
type Firm { code String primary }
type P {
	id String primary
	i Integer
	f Float
	is List<Integer>
	fs List<Float>
	v Vector[2]
	--> AT (_:one) Firm { w Integer }
}
`

// numberCellPositions is every position a cell can hold a number at, as a CSV
// header and cell and as the JSON member that states the same literal.
var numberCellPositions = []numberCellPosition{
	{"i", "i", "%s", `"i":%s`, `column "i"`},
	{"f", "f", "%s", `"f":%s`, `column "f"`},
	{"is", "is", "3|%s", `"is":[3,%s]`, `column "is"`},
	{"fs", "fs", "0.5|%s", `"fs":[0.5,%s]`, `column "fs"`},
	{"v", "v", "0.5|%s", `"v":[0.5,%s]`, `column "v"`},
	{"AT", "at._target_code,at.w", "f1,%s", `"at":{"_target_code":"f1","w":%s}`, `column "at".w`},
}

type numberCellPosition struct {
	prop, header, cell, json, column string
}

// A cell holding a JSON number literal is accepted exactly when the JSON
// document holding that literal at the same position is, and stores the same
// value, the sign of a zero included, at every position a cell reaches: an
// Integer, a Float, a List's element, a Vector's element and an edge property.
// The JSON adapter and the validator are the other implementation.
func TestCoerce_ANumberCellReadsAsItsJSONLiteral(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCellSchema, "n.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	p, _ := s.Type("P")
	v := instance.NewValidator(s)
	literals := []string{
		"0", "-0", "5", "-5", "0.0", "-0.0", "1.0", "1.5", "-1.5e-3", "1e2", "1E2", "5e0", "5e-1",
		"9223372036854775807", "-9223372036854775808", "9223372036854775808", "-9223372036854775809",
		"99999999999999999999", "9007199254740993", "1e400", "-1e400", "1e-400", "4.9e-324",
	}
	for _, pos := range numberCellPositions {
		for _, lit := range literals {
			header := "id," + pos.header
			cell := fmt.Sprintf(pos.cell, lit)
			byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"),
				[]byte(`{"P":[{"id":"p1",`+fmt.Sprintf(pos.json, lit)+`}]}`))
			if jres.HasErrors() {
				t.Fatalf("json %s = %s: %s", pos.prop, lit, jres)
			}
			wantVal, wantOK := validatedNumber(t, v, byType["P"][0], pos.prop)

			raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "P",
				strings.NewReader(header+"\np1,"+cell+"\n"), p)
			for issue := range pres.Issues() {
				if issue.Code() != E_CSV_COERCE {
					t.Fatalf("%s = %s: parse drew %s, want at most E_CSV_COERCE", pos.prop, lit, pres)
				}
			}
			gotVal, gotOK := validatedNumber(t, v, raws[0], pos.prop)
			if pres.HasErrors() {
				gotVal, gotOK = nil, false
			}
			if gotOK != wantOK || gotOK && !sameNumbers(gotVal, wantVal) {
				t.Errorf("%s = %s: CSV accepted=%v stored %#v, JSON accepted=%v stored %#v", pos.prop, lit, gotOK, gotVal, wantOK, wantVal)
			}
		}
	}
}

// A spelling JSON has no number literal for is refused at every position with
// one E_CSV_COERCE naming the column, the cell keeping its text: those strconv
// read ("+5", "007", "1_000", "0x1p4", "Inf", ".5") and those it refused
// alike. A literal no float64 holds is refused the same way at a Float, and a
// float literal at an Integer says it is no integer literal.
func TestCoerce_ASpellingThatIsNoJSONLiteralIsRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCellSchema, "n.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	p, _ := s.Type("P")
	refused := func(pos numberCellPosition, cell, text, mention string) {
		t.Helper()
		raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "P",
			strings.NewReader("id,"+pos.header+"\np1,"+cell+"\n"), p)
		issue, ok := issueContaining(pres, mention)
		if pres.Len() != 1 || !ok || issue.Code() != E_CSV_COERCE || !strings.Contains(issue.Message(), pos.column) {
			t.Errorf("%s = %q drew %s, want one E_CSV_COERCE naming %s and %q", pos.prop, cell, pres, pos.column, mention)
			return
		}
		if !strings.Contains(fmt.Sprint(raws[0].Properties), text) {
			t.Errorf("%s = %q stored %v, want the cell's text kept", pos.prop, cell, raws[0].Properties)
		}
	}
	for _, pos := range numberCellPositions {
		for _, sp := range []string{"+5", "007", "-01", "1_000", "0x10", "0x1p4", "Inf", "-Inf", "NaN", ".5", "5.", "1e", "1e+", "--5", "5 ", "\t5"} {
			cell := fmt.Sprintf(pos.cell, sp)
			if strings.ContainsAny(sp, " \t") {
				head, last, _ := cutLast(cell, ",")
				cell = head + `"` + last + `"`
			}
			refused(pos, cell, sp, "not a JSON")
		}
	}
	for _, pos := range numberCellPositions[1:5:5] {
		if pos.prop == "is" {
			continue
		}
		for _, lit := range []string{"1e400", "-1e400"} {
			refused(pos, fmt.Sprintf(pos.cell, lit), lit, "value out of range")
		}
	}
	for _, lit := range []string{"1e2", "1.0"} {
		refused(numberCellPositions[0], lit, lit, "not a JSON integer literal")
	}
}

// cutLast splits s after its last sep, so a quote can wrap the last field.
func cutLast(s, sep string) (head, last string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", s, false
	}
	return s[:i+len(sep)], s[i+len(sep):], true
}

// validatedNumber validates raw and returns the value it stores at prop, the
// edge property w for "AT", as plain Go values: a list is a []any.
func validatedNumber(t *testing.T, v *instance.Validator, raw instance.RawInstance, prop string) (any, bool) {
	t.Helper()
	vi, res := v.ValidateOne(t.Context(), "P", raw)
	if !res.OK() {
		return nil, false
	}
	if prop == "AT" {
		edge, ok := vi.Edge("AT")
		if !ok || len(edge.Targets()) != 1 {
			t.Fatalf("validated no single AT target")
		}
		target := edge.Targets()[0]
		w, ok := target.Properties().Get("w")
		if !ok {
			return nil, true
		}
		return w.Clone(), true
	}
	val, ok := vi.Property(prop)
	if !ok {
		return nil, true
	}
	return val.Clone(), true
}

// sameNumbers compares two stored values, each float bit for bit.
func sameNumbers(a, b any) bool {
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		return ok && math.Float64bits(av) == math.Float64bits(bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !sameNumbers(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

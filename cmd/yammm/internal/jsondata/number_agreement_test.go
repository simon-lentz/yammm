package jsondata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/jsondata"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const agreementSchema = `schema "agree"

type T {
	id String primary
	n Integer
	f Float
}
`

// agreementLiterals are JSON number literals that decide the number rule: whole
// values spelled as floats, digit runs past uint64 before an indicator, the
// int64 and float64 edges on both sides, and a negative zero.
var agreementLiterals = []string{
	"0", "-0", "5", "-5", "5.0", "5.5", "-5.5", "1e2", "1E2", "5e0", "5e-1", "1.5e3",
	"100000000000000000000e-19", "18446744073709551616e-18", "123456789012345678901234.5e-20",
	"-9223372036854775809.0", "-9.223372036854775809e18",
	"9007199254740993", "9007199254740993.0",
	"9223372036854775807", "-9223372036854775808", "9223372036854775808", "-9223372036854775809",
	"18446744073709551615", "99999999999999999999",
	"1e400", "-1e400", "1e-400", "4.9e-324", "1.7976931348623157e308",
	"2" + strings.Repeat("0", 308), "1" + strings.Repeat("0", 400),
}

// The second implementation is encoding/json decoding into the Go type each
// kind stores: an Integer accepts exactly what it decodes into an int64, with
// the same value, and a Float exactly what it decodes into a float64, bit for
// bit, the sign of a zero included. [jsondata.Parse] and the validator agree with
// it on every literal, on what they accept and what they store; the .ys reader
// under revalidation agrees on what it accepts.
func TestNumberRule_EveryReaderAgreesWithEncodingJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, agreementSchema, "agree.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	v := instance.NewValidator(s)
	ys := ysTemplate(t, s, v)

	for _, prop := range []string{"n", "f"} {
		for _, lit := range agreementLiterals {
			name := lit
			if len(name) > 24 {
				name = fmt.Sprintf("%s…(%d)", name[:12], len(lit))
			}
			t.Run(prop+"="+name, func(t *testing.T) {
				t.Parallel()
				want, wantOK := decodeAs(t, prop, lit)

				got, ok := jsonReader(ctx, t, v, prop, lit)
				check(t, "JSON", ok, wantOK, got, want)

				if revalidated := ysReader(ctx, t, s, ys, prop, lit); revalidated != wantOK {
					t.Errorf(".ys revalidation accepted=%v, encoding/json accepted=%v", revalidated, wantOK)
				}
			})
		}
	}
}

// decodeAs decodes lit with encoding/json into the Go type prop stores.
func decodeAs(t *testing.T, prop, lit string) (any, bool) {
	t.Helper()
	if prop == "n" {
		var n int64
		if err := json.Unmarshal([]byte(lit), &n); err != nil {
			return nil, false
		}
		return n, true
	}
	var f float64
	if err := json.Unmarshal([]byte(lit), &f); err != nil {
		return nil, false
	}
	return f, true
}

func check(t *testing.T, reader string, ok, wantOK bool, got, want any) {
	t.Helper()
	if ok != wantOK {
		t.Errorf("%s accepted=%v (%#v), encoding/json accepted=%v (%#v)", reader, ok, got, wantOK, want)
		return
	}
	if !ok {
		return
	}
	if gf, isFloat := got.(float64); isFloat {
		if wf, _ := want.(float64); math.Float64bits(gf) != math.Float64bits(wf) {
			t.Errorf("%s stored %v (bits %x), encoding/json decoded %v (bits %x)", reader, gf, math.Float64bits(gf), wf, math.Float64bits(wf))
		}
		return
	}
	if got != want {
		t.Errorf("%s stored %#v, encoding/json decoded %#v", reader, got, want)
	}
}

// jsonReader parses a document holding lit at prop and validates it.
func jsonReader(ctx context.Context, t *testing.T, v *instance.Validator, prop, lit string) (any, bool) {
	t.Helper()
	other := `"f":0.5`
	if prop == "f" {
		other = `"n":0`
	}
	doc := fmt.Sprintf(`{"T":[{"id":"a",%s,"%s":%s}]}`, other, prop, lit)
	parsed, pres := jsondata.Parse(ctx, location.NewSourceID("agree.json"), []byte(doc))
	if err := pres.Err(); err != nil {
		t.Fatalf("JSON parse %s: %v", doc, err)
	}
	return validated(ctx, v, parsed["T"][0], prop)
}

func validated(ctx context.Context, v *instance.Validator, raw instance.RawInstance, prop string) (any, bool) {
	vi, vres := v.ValidateOne(ctx, "T", raw)
	if !vres.OK() {
		return nil, false
	}
	got, _ := vi.Property(prop)
	return got.Unwrap(), true
}

// ysTemplate writes a .ys document holding n 0 and f 0.5, whose numbers
// ysReader replaces.
func ysTemplate(t *testing.T, s schema.Schema, v *instance.Validator) []byte {
	t.Helper()
	ctx := context.Background()
	vi, vres := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": "a", "n": int64(0), "f": 0.5}})
	if vres.HasErrors() {
		t.Fatalf("validate: %s", vres)
	}
	g := graph.New(s)
	if r := g.Add(ctx, vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, mres := snapshot.Marshal(ctx, g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	for _, anchor := range []string{`"n":0`, `"f":0.5`} {
		if bytes.Count(data, []byte(anchor)) != 1 {
			t.Fatalf("the document holds %s other than once:\n%s", anchor, data)
		}
	}
	return data
}

// ysReader loads the template with lit at prop under revalidation and reports
// whether the validator accepted it.
func ysReader(ctx context.Context, t *testing.T, s schema.Schema, ys []byte, prop, lit string) bool {
	t.Helper()
	anchor := map[string]string{"n": `"n":0`, "f": `"f":0.5`}[prop]
	data := bytes.Replace(ys, []byte(anchor), []byte(`"`+prop+`":`+lit), 1)
	_, lres := snapshot.Load(ctx, data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
	for issue := range lres.Issues() {
		if issue.Code() != diag.E_TYPE_MISMATCH {
			t.Fatalf(".ys load drew %s, want at most E_TYPE_MISMATCH", lres)
		}
	}
	return !lres.HasErrors()
}

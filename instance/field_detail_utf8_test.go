package instance_test

import (
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// A name the input wrote and no member claims is reported with the name in the
// message and in the field detail. A Go caller can hand the validator a name
// that is not valid UTF-8: the detail then holds the quoted spelling the
// message holds, and a valid name is held as it was written.
func TestValidateOne_AnUnknownFieldsDetailIsValidUTF8(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), utf8Schema, "utf8.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	v := instance.NewValidator(s)

	for _, c := range []struct {
		name, field, prefix string
		code                diag.Code
		raw                 func(field string) instance.RawInstance
	}{
		{
			"a property name", "te\xffxt", "unknown field ", diag.E_UNKNOWN_FIELD,
			func(f string) instance.RawInstance { return utf8Raw(map[string]any{f: "x"}) },
		},
		{
			"a property name that is valid", "nmae", "unknown field ", diag.E_UNKNOWN_FIELD,
			func(f string) instance.RawInstance { return utf8Raw(map[string]any{f: "x"}) },
		},
		{
			"a valid name holding a quote and a backslash", `a"b\c`, "unknown field ", diag.E_UNKNOWN_FIELD,
			func(f string) instance.RawInstance { return utf8Raw(map[string]any{f: "x"}) },
		},
		{
			"an edge property name", "la\xffbel", "unknown field in edge object: ", diag.E_UNKNOWN_EDGE_FIELD,
			func(f string) instance.RawInstance { return utf8Raw(utf8Link(map[string]any{f: "x"})) },
		},
		{
			"an edge property name that is valid", "lable", "unknown field in edge object: ", diag.E_UNKNOWN_EDGE_FIELD,
			func(f string) instance.RawInstance { return utf8Raw(utf8Link(map[string]any{f: "x"})) },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, res := v.ValidateOne(t.Context(), "Holder", c.raw(c.field))
			n := 0
			for issue := range res.Issues() {
				n++
				if issue.Code() != c.code {
					t.Errorf("issue %s, want %s: %s", issue.Code(), c.code, issue.Message())
				}
				if want := c.prefix + strconv.Quote(c.field); issue.Message() != want {
					t.Errorf("message %q, want %q", issue.Message(), want)
				}
				want := c.field
				if !utf8.ValidString(c.field) {
					want = strconv.Quote(c.field)
				}
				found := false
				for _, d := range issue.Details() {
					if !utf8.ValidString(d.Value) {
						t.Errorf("detail %s is not valid UTF-8: %q", d.Key, d.Value)
					}
					if d.Key == diag.DetailKeyField {
						found = true
						if d.Value != want {
							t.Errorf("detail field = %q, want %q", d.Value, want)
						}
					}
				}
				if !found {
					t.Errorf("the issue holds no field detail: %v", issue.Details())
				}
			}
			if n != 1 {
				t.Errorf("ValidateOne reported %d issues, want one: %s", n, res)
			}
		})
	}
}

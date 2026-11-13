package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// escaped writes every byte of v as a \xHH escape, so the DSL can write any
// byte string as a literal, whether or not it then accepts it.
func escaped(v string) string {
	var b strings.Builder
	for i := range len(v) {
		fmt.Fprintf(&b, `\x%02x`, v[i])
	}
	return b.String()
}

// jsonKeeps reports whether v survives a JSON round trip byte for byte, which
// is what every generator and wire a string value reaches requires of it.
func jsonKeeps(t *testing.T, v string) bool {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back string
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	return back == v
}

// TestStringValues_TheDSLAcceptsWhatJSONKeeps holds three places a schema holds a
// string value to one rule, against a second reading: a value is accepted
// exactly when encoding/json writes it back unchanged, as it does every valid
// UTF-8 string and no other.
func TestStringValues_TheDSLAcceptsWhatJSONKeeps(t *testing.T) {
	values := []string{"café", "a\xffb", "a\xe2\x82", "\xc3", "a\x00b", "a\uFEFFb", "\U0001F600"}
	places := []struct {
		name string
		dsl  func(lit string) string
	}{
		{"enum value", func(lit string) string {
			return "schema \"s\"\n\ntype Car {\n\tid String primary\n\tk Enum[\"" + lit + "\", \"zz\"]\n}\n"
		}},
		{"invariant message", func(lit string) string {
			return "schema \"s\"\n\ntype Car {\n\tid String primary\n\t! \"" + lit + "\" id != \"x\"\n}\n"
		}},
		{"expression literal", func(lit string) string {
			return "schema \"s\"\n\ntype Car {\n\tid String primary\n\t! \"m\" id != \"" + lit + "\"\n}\n"
		}},
	}
	for _, place := range places {
		for _, v := range values {
			t.Run(fmt.Sprintf("%s %q", place.name, v), func(t *testing.T) {
				want := jsonKeeps(t, v)
				_, lres := schema.LoadString(context.Background(), place.dsl(escaped(v)), "s.yammm")
				if loads := !lres.HasErrors(); loads != want {
					t.Errorf("the DSL loads %q: %v, want %v (%s)", v, loads, want, lres.String())
				}
			})
		}
	}
}

func TestCheckConstraint_RefusesAStringValueNotValidUTF8(t *testing.T) {
	for name, tc := range map[string]struct {
		c    schema.Constraint
		want string
	}{
		"enum value":          {schema.NewEnumConstraint([]string{"a\xff", "b"}), `enum value "a\xff" is not valid UTF-8`},
		"a list's enum value": {schema.NewListConstraint(schema.NewEnumConstraint([]string{"a", "b\xff"})), `enum value "b\xff" is not valid UTF-8`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := schema.CheckConstraint(tc.c); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckConstraint = %v, want %s", err, tc.want)
			}
		})
	}
}

// TestLoad_RefusesADocHoldingWhatTheSourceRulesRefuse pins E_SYNTAX on a doc
// comment holding what the source rules refuse.
func TestLoad_RefusesADocHoldingWhatTheSourceRulesRefuse(t *testing.T) {
	for _, doc := range []string{"a\uFEFFb", "a\x00b", "a\xffb", "a\xe2\x82"} {
		src := "schema \"s\"\n\n/* " + doc + " */\ntype Car {\n\tid String primary\n}\n"
		if _, res := schema.LoadString(context.Background(), src, "s.yammm"); !hasCode(res, diag.E_SYNTAX) {
			t.Errorf("the DSL loads a doc comment holding %q: %s", doc, res.String())
		}
	}
}

// TestPattern_ASurrogateEscapeLoads pins that a pattern is what regexp.Compile
// accepts: an escape that names a surrogate code point compiles, and it
// matches no string.
func TestPattern_ASurrogateEscapeLoads(t *testing.T) {
	c := schema.NewPatternConstraint(regexp.MustCompile(`a\x{D800}`))
	if err := schema.CheckConstraint(c); err != nil {
		t.Errorf("CheckConstraint = %v, want nil", err)
	}
	src := "schema \"s\"\n\ntype Car {\n\tid String primary\n\tp Pattern[\"a\\\\x{D800}\"]\n}\n"
	if _, lres := schema.LoadString(context.Background(), src, "s.yammm"); lres.HasErrors() {
		t.Errorf("the DSL: %s; want a load", lres.String())
	}
}

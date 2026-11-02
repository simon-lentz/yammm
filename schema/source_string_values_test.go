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

// TestStringValues_TheDSLAcceptsWhatJSONKeeps holds five places a schema holds a
// string value to one rule, against a second reading: a value is accepted
// exactly when encoding/json writes it back unchanged, as it does every valid
// UTF-8 string and no other.
func TestStringValues_TheDSLAcceptsWhatJSONKeeps(t *testing.T) {
	values := []string{"café", "a\xffb", "a\xe2\x82", "\xc3", "a\x00b", "a\uFEFFb", "\U0001F600"}
	places := []struct {
		name string
		dsl  func(lit string) string
	}{
		{"schema name", func(lit string) string { return `schema "` + lit + "\"\n\ntype Car {\n\tid String primary\n}\n" }},
		{"enum value", func(lit string) string {
			return "schema \"s\"\n\ntype Car {\n\tid String primary\n\tk Enum[\"" + lit + "\", \"zz\"]\n}\n"
		}},
		{"timestamp format", func(lit string) string {
			return "schema \"s\"\n\ntype Car {\n\tid String primary\n\tt Timestamp[\"" + lit + "\"]\n}\n"
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
		"enum value":                {schema.NewEnumConstraint([]string{"a\xff", "b"}), `enum value "a\xff" is not valid UTF-8`},
		"a list's timestamp format": {schema.NewListConstraint(schema.NewTimestampConstraintFormatted("\xff")), `timestamp format "\xff" is not valid UTF-8`},
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

// TestPattern_ASurrogateIsRefusedInOneSentence pins the surrogate rule at
// CheckConstraint and in the DSL, in the same words.
func TestPattern_ASurrogateIsRefusedInOneSentence(t *testing.T) {
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`a\x{D800}`)})
	want := `regex pattern "a\\x{D800}" names the surrogate code point U+D800, which no string holds`
	if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("CheckConstraint = %v, want %s", err, want)
	}
	src := "schema \"s\"\n\ntype Car {\n\tid String primary\n\tp Pattern[\"a\\\\x{D800}\"]\n}\n"
	if _, lres := schema.LoadString(context.Background(), src, "s.yammm"); !strings.Contains(lres.String(), want) {
		t.Errorf("the DSL: %s; want %s", lres.String(), want)
	}
}

// TestNewPatternConstraint_ReportsTheFirstFault pins that a later pattern's
// surrogate does not replace an earlier pattern's compile fault.
func TestNewPatternConstraint_ReportsTheFirstFault(t *testing.T) {
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**"), regexp.MustCompile(`\x{D800}`)})
	if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), `"a**"`) {
		t.Errorf("CheckConstraint = %v, want the first pattern's fault", err)
	}
}

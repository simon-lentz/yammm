package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// One language, one escape vocabulary. Every spelling below is a string
// literal the lexer admits, and an invariant comparing against it must resolve
// the same value a property, enum or pattern in the same spelling resolves.
// Four of them drew E_INVALID_INVARIANT while expression literals went through
// strconv.Unquote: a multi-character single-quoted literal, the \' escape, an
// unescaped '"' inside single quotes, and bare \0.
func TestInvariant_StringLiteralsResolveTheLexerVocabulary(t *testing.T) {
	cases := []struct {
		name    string
		literal string // as written in the invariant
		value   string // what it must resolve to
	}{
		{"single-quoted multi-character", `'abc'`, "abc"},
		{"double-quoted", `"abc"`, "abc"},
		{"escaped apostrophe in double quotes", `"don\'t"`, "don't"},
		{"escaped apostrophe in single quotes", `'don\'t'`, "don't"},
		{"unescaped double quote in single quotes", `'say "hi"'`, `say "hi"`},
		{"named escapes", `"a\tb"`, "a\tb"},
		{"hex escape", `"\x41"`, "A"},
		{"unicode escape", `"café"`, "café"},
		{"zero escape is not octal", `"\012"`, "\x0012"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"lit\"\n\ntype T {\n\tname String primary\n\t! \"m\" name == " +
				tc.literal + "\n}\n"
			s, result := schema.LoadString(t.Context(), src, "lit.yammm")
			if result.HasErrors() {
				t.Fatalf("schema carrying %s did not load: %s", tc.literal, result)
			}

			v := instance.NewValidator(s)
			if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{
				Properties: map[string]any{"name": tc.value},
			}); !res.OK() {
				t.Errorf("%s did not resolve to %q: %s", tc.literal, tc.value, res)
			}

			// The invariant must also be able to fail, or the case above
			// proves nothing about the resolved value.
			if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{
				Properties: map[string]any{"name": tc.value + "x"},
			}); res.OK() {
				t.Errorf("%s accepted a value it should reject — the invariant cannot fail", tc.literal)
			}
		})
	}
}

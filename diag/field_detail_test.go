package diag

import (
	"testing"
	"unicode/utf8"
)

// The field detail is the one detail whose text the data writes. A name that
// is valid UTF-8 is held as written, and any other is held quoted, so an
// issue's details are valid UTF-8 whatever the data held.
func TestFieldDetail_IsValidUTF8(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, want string }{
		{"amount", "amount"},
		{"", ""},
		{"naïve ✓", "naïve ✓"},
		{"a" + string(rune(0xFFFD)) + "b", "a" + string(rune(0xFFFD)) + "b"},
		{`a"b\c`, `a"b\c`},
		{"te\xffxt", `"te\xffxt"`},
		{"\xc3", `"\xc3"`},
		{"é\xffé", `"é\xffé"`},
	} {
		for _, details := range [][]Detail{TypeField("Holder", c.name), RelationField("LINK", c.name)} {
			var got string
			for _, d := range details {
				if !utf8.ValidString(d.Value) {
					t.Errorf("%q: detail %s is not valid UTF-8: %q", c.name, d.Key, d.Value)
				}
				if d.Key == DetailKeyField {
					got = d.Value
				}
			}
			if got != c.want {
				t.Errorf("%q: field detail = %q, want %q", c.name, got, c.want)
			}
		}
	}
}

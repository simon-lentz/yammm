package instance_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The UTF-8 rule reads every byte of every string a row holds, once at the
// check and once at the coercion. These three time what it adds where it
// costs most: one long string, the same length in multibyte text, and a row of
// many short strings. Not a CI gate.

func benchCheckString(b *testing.B, s string) {
	b.Helper()
	c := schema.NewStringConstraint()
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		if err := instance.CheckValue(s, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckValue_String1KiBASCII(b *testing.B) {
	benchCheckString(b, strings.Repeat("abcdefgh", 128))
}

func BenchmarkCheckValue_String1KiBMultibyte(b *testing.B) {
	benchCheckString(b, strings.Repeat("éé", 256))
}

func BenchmarkValidateOne_TwentyStringProperties(b *testing.B) {
	var decls strings.Builder
	props := map[string]any{"id": "row-0001"}
	for i := range 20 {
		fmt.Fprintf(&decls, "\tp%02d String\n", i)
		props[fmt.Sprintf("p%02d", i)] = fmt.Sprintf("value %02d of a short text property", i)
	}
	s := benchValidatorSchema(b, decls.String(), "")
	benchValidate(b, s, props)
}

func BenchmarkValidateOne_One1KiBStringProperty(b *testing.B) {
	s := benchValidatorSchema(b, "\tbody String\n", "")
	benchValidate(b, s, map[string]any{"id": "row-0001", "body": strings.Repeat("abcdefgh", 128)})
}

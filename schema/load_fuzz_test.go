package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// FuzzLoadString fuzzes the full parse→compile→load path. yammm is a DSL
// parser, so arbitrary input must never panic: malformed sources surface
// as diagnostics, and a nil schema without error diagnostics is a contract
// violation.
func FuzzLoadString(f *testing.F) {
	seeds := []string{
		`schema "test"`,
		"schema \"test\"\n\ntype Person {\n\tid String primary\n\tname String required\n}\n",
		"schema \"test\"\ntype Thing {\n\tvalue Float[0.0, 100.0]\n}",
		"schema \"test\"\ntype Thing {\n\tstatus Enum[\"A\", \"B\"]\n}",
		"schema \"test\"\ntype Thing {\n\tcode Pattern[/^[A-Z]+$/]\n}",
		"schema \"test\"\ntype Thing {\n\ttags List[String]\n}",
		"schema \"test\"\ntype Thing {\n\tembedding Vector[128]\n}",
		"schema \"main\"\nimport \"./parts\" as parts\n",
		"schema \"test\"\ntype Sub extends Base {\n\tid String primary\n}\ntype Base {\n\tname String\n}",
		"schema \"test\"\ntype Person {\n\tid String primary\n\t--> EMPLOYER (one) Company\n}\ntype Company {\n\tid String primary\n}",
		"schema \"test\"\ntype Car {\n\tid String primary\n\t*-> WHEELS (many) Wheel\n}\npart type Wheel {\n\tid String primary\n}",
		"schema \"test\"\ntype ShortName = String[1, 50]",
		"schema \"test\"\ntype Person {\n\tid String primary\n\tage Integer\n\tinvariant \"adult\" age >= 18\n}",
		"schema \"\x00\"",
		"schema",
		"",
		"type Orphan {}",
		"schema \"test\"\ntype Broken {\n\tid Unknown primary\n}",
		"schema \"test\"\ntype Trade {\n\tid String primary\n\tstate String @index\n\tembedding Vector[8] @vector(cosine)\n\tseen Timestamp @writeOnce\n\t@@index(id, state)\n}",
		"schema \"test\"\ntype Trade {\n\tid String primary\n\tx String @index()\n}",
		"schema \"test\"\ntype Trade {\n\tid String primary\n\t@@index(ghost)\n}",
		"schema \"test\"\ntype Trade {\n\tid String primary\n\tx String @index @index\n}",

		// Differential-fuzz finds from the parser spike: an alias target that
		// names its own type, and a modifier ambiguous with the next property.
		`schema""type A=A`,
		"schema \"0000\"  type A000000{ a00 A00000 primary A} ",

		// A pipeline arrow at end of input, all three spellings: the released
		// loader dereferenced nil here, and nothing else in the tree pins it.
		"type A{!00->",
		"type A{!0->",
		`schema "s" type A { ! "m" x->`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		s, result := schema.LoadString(t.Context(), src, "fuzz.yammm")
		if s == nil && !result.HasErrors() {
			t.Errorf("LoadString returned nil schema without error diagnostics for %q", src)
		}
	})
}

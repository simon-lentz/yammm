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

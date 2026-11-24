package format

import "testing"

// TestSource_DropsALeadingByteOrderMark pins that formatted output never
// carries U+FEFF: a marked file formats to the unmarked text, so a canonical
// file that starts with a mark reports as unformatted and a write removes it.
func TestSource_DropsALeadingByteOrderMark(t *testing.T) {
	t.Parallel()
	canonical := "schema \"test\"\n\ntype Person {\n\tname String required\n}\n"
	cases := map[string]string{
		"a marked canonical file":   "\uFEFF" + canonical,
		"a marked unformatted file": "\uFEFFschema \"test\"\ntype   Person   {\nname    String    required\n}\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := formatText(input)
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if got != canonical {
				t.Errorf("Source = %q, want %q", got, canonical)
			}
		})
	}
}

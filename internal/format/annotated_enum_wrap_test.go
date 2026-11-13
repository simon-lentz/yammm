package format_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// An Enum property too wide for one line wraps with its annotation after the
// closing "]", and the wrapped form loads and formats to itself.
func TestSource_AnAnnotatedEnumOverTheWidthWrapsAndLoads(t *testing.T) {
	t.Parallel()
	const src = "schema \"s\"\n\ntype T {\n\tid String primary\n" +
		"\tstate Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\"] required @index\n}\n"
	once, err := formatText(src)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if !strings.Contains(once, "\tstate Enum[\n") || !strings.Contains(once, "\t] required @index\n") {
		t.Fatalf("the Enum did not wrap with its annotation on the closing line:\n%s", once)
	}
	if _, res := schema.LoadString(t.Context(), once, "s.yammm"); res.HasErrors() {
		t.Errorf("the wrapped form does not load: %s\n%s", res.String(), once)
	}
	twice, err := formatText(once)
	if err != nil {
		t.Fatalf("second format: %v", err)
	}
	if twice != once {
		t.Errorf("the second run changed the output:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

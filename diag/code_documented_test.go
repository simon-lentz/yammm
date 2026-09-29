package diag_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/diag"

	// Blank imports register adapter-defined diagnostic codes so AllCodes()
	// includes them. This is a test file, so the coupling is acceptable.
	_ "github.com/simon-lentz/yammm/adapter/csv"
	_ "github.com/simon-lentz/yammm/adapter/neo4j"
)

// diagnosticsRefPath is the path to the plugin's diagnostics reference file,
// relative to the diag/ package directory.
const diagnosticsRefPath = "../claude-plugin/skills/yammm/references/diagnostics.md"

// specRefPath is the path to the specification's diagnostic-code appendix,
// relative to the diag/ package directory. It is the second place every
// registered code must appear; nothing but [TestRegisteredCodesDocumented]
// reads it against the registry.
const specRefPath = "../docs/SPEC.md"

// TestRegisteredCodesDocumented verifies that every registered diagnostic code
// appears in the plugin's references/diagnostics.md and in docs/SPEC.md.
//
// It catches a code added to the registry and not yet documented. The other
// direction, a documented name that is no registered code, is
// [github.com/simon-lentz/yammm/docs.TestModuleCitesRegisteredCodes]'s, over
// the whole module.
func TestRegisteredCodesDocumented(t *testing.T) {
	// Both documents enumerate the whole code set, so both are oracles: a
	// code documented in one and absent from the other is the drift this
	// guard exists to catch.
	for _, oracle := range []struct{ name, path string }{
		{"diagnostics.md", diagnosticsRefPath},
		{"SPEC.md", specRefPath},
	} {
		content, err := os.ReadFile(oracle.path)
		if os.IsNotExist(err) {
			t.Errorf("%s not found at %s; the gate has no oracle", oracle.name, oracle.path)
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", oracle.name, err)
		}

		text := string(content)
		for _, c := range diag.AllCodes() {
			if !documents(text, c.String()) {
				t.Errorf("registered code %s is not documented in %s", c, oracle.name)
			}
		}
	}

	t.Logf("verified %d registered codes against both oracles", len(diag.AllCodes()))
}

// documents reports whether text names code as a whole word: a code such as
// E_UNRESOLVED_REQUIRED is a substring of a longer one, and a substring match
// would document it by accident.
func documents(text, code string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(code) + `\b`).MatchString(text)
}

func TestDocuments(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		text string
		want bool
	}{
		{"| `E_UNRESOLVED_REQUIRED` | a required association |", true},
		{"E_UNRESOLVED_REQUIRED.", true},
		{"| `E_UNRESOLVED_REQUIRED_COMPOSITION` | a required part |", false},
		{"XE_UNRESOLVED_REQUIRED", false},
	} {
		if got := documents(c.text, "E_UNRESOLVED_REQUIRED"); got != c.want {
			t.Errorf("documents(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

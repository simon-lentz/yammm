package diag_test

import (
	"os"
	"regexp"
	"strings"
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

// codePattern matches E_* and W_* diagnostic code identifiers in markdown
// content. The E_ prefix covers error-severity and sentinel codes (the
// historical default); the W_ prefix covers warning-severity codes added
// from v0.3.0 onward. The final [A-Z0-9] ensures wildcard references like
// "E_IMPORT_*" or "W_FOO_*" are not matched.
var codePattern = regexp.MustCompile(`[EW]_[A-Z][A-Z0-9_]*[A-Z0-9]`)

// TestDocumentedCodesExist verifies that every E_* / W_* diagnostic code
// mentioned in the plugin's references/diagnostics.md exists in the diag
// code registry.
//
// This is a documentation drift guard: if a code is renamed or removed from
// the diag package but the reference file still mentions it, this test fails.
// It runs as part of normal `go test ./diag/...` and CI.
func TestDocumentedCodesExist(t *testing.T) {
	content, err := os.ReadFile(diagnosticsRefPath)
	if os.IsNotExist(err) {
		t.Skip("diagnostics reference file not yet written")
	}
	if err != nil {
		t.Fatalf("read diagnostics reference: %v", err)
	}

	// Build a set of all registered code strings.
	registered := make(map[string]bool)
	for _, c := range diag.AllCodes() {
		registered[c.String()] = true
	}

	// Extract all E_* identifiers from the reference file.
	matches := codePattern.FindAllString(string(content), -1)
	if len(matches) == 0 {
		t.Fatal("no E_* codes found in diagnostics reference file")
	}

	// Deduplicate.
	documented := make(map[string]bool)
	for _, m := range matches {
		documented[m] = true
	}

	// Every documented code must exist in the registry.
	for code := range documented {
		if !registered[code] {
			t.Errorf("diagnostics.md references %s but it is not a registered diagnostic code", code)
		}
	}

	t.Logf("verified %d unique documented codes against %d registered codes", len(documented), len(registered))
}

// TestRegisteredCodesDocumented verifies that every registered diagnostic code
// appears in the plugin's references/diagnostics.md.
//
// This is the reverse of TestDocumentedCodesExist: it catches new codes added
// to the registry that are not yet documented. Together, the two tests enforce
// bidirectional parity between the code registry and the reference file.
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
			if !strings.Contains(text, c.String()) {
				t.Errorf("registered code %s is not documented in %s", c, oracle.name)
			}
		}
	}

	t.Logf("verified %d registered codes against both oracles", len(diag.AllCodes()))
}

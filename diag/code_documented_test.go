package diag

import (
	"os"
	"regexp"
	"testing"
)

// diagnosticsRefPath is the path to the plugin's diagnostics reference file,
// relative to the diag/ package directory. This file is authored in Phase 2
// of the plugin refactor; the test skips gracefully if it doesn't exist yet.
const diagnosticsRefPath = "../claude-plugin/skills/yammm/references/diagnostics.md"

// codePattern matches E_* diagnostic code identifiers in markdown content.
var codePattern = regexp.MustCompile(`E_[A-Z][A-Z0-9_]+`)

// TestDocumentedCodesExist verifies that every E_* diagnostic code mentioned
// in the plugin's references/diagnostics.md exists in the diag code registry.
//
// This is a documentation drift guard: if a code is renamed or removed from
// the diag package but the reference file still mentions it, this test fails.
// It runs as part of normal `go test ./diag/...` and CI.
func TestDocumentedCodesExist(t *testing.T) {
	content, err := os.ReadFile(diagnosticsRefPath)
	if os.IsNotExist(err) {
		t.Skip("diagnostics reference file not yet written (Phase 2)")
	}
	if err != nil {
		t.Fatalf("read diagnostics reference: %v", err)
	}

	// Build a set of all registered code strings.
	registered := make(map[string]bool)
	for _, c := range AllCodes() {
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

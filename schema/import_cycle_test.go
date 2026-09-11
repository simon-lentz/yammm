package schema_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestImportCycle_ReportedOnTheClosingImport holds an import cycle to one
// diagnostic on the declaration that closes it. a.yammm imports b.yammm, so
// b.yammm's import of a.yammm is the one that closes the cycle.
func TestImportCycle_ReportedOnTheClosingImport(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	a := filepath.Join(root, "a.yammm")
	b := filepath.Join(root, "b.yammm")
	writeHostPathFile(t, a, "schema \"a\"\n\nimport \"./b\" as b\n\ntype A {\n\tid String primary\n\t--> USES (one) b.B\n}\n")
	writeHostPathFile(t, b, "schema \"b\"\n\nimport \"./a\" as a\n\ntype B {\n\tid String primary\n\t--> USES (one) a.A\n}\n")

	_, res := schema.Load(t.Context(), a, schema.WithModuleRoot(root))

	fileID := func(p string) location.SourceID {
		id, err := location.SourceIDFromAbsolutePath(canonicalPath(t, p))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	aID, bID := fileID(a), fileID(b)

	var cycles, upstream []diag.Issue
	for issue := range res.Issues() {
		switch issue.Code() {
		case diag.E_IMPORT_CYCLE:
			cycles = append(cycles, issue)
		case diag.E_UPSTREAM_FAIL:
			upstream = append(upstream, issue)
		}
	}
	on := func(issues []diag.Issue, id location.SourceID) bool {
		for _, issue := range issues {
			if issue.HasSpan() && issue.Span().Source == id {
				return true
			}
		}
		return false
	}
	namesNoFile := len(cycles) > 0
	var messages []string
	for _, issue := range cycles {
		messages = append(messages, issue.Message())
		if strings.Contains(issue.Message(), "a.yammm") || strings.Contains(issue.Message(), "b.yammm") {
			namesNoFile = false
		}
	}
	var spans []string
	for _, issue := range cycles {
		spans = append(spans, issue.Span().String())
	}

	knownBroken := map[string]string{
		"it is reported on the closing import":        "the cycle is detected on entering a.yammm, with no declaration in hand, so it has no span (B14)",
		"its message names no source file":            "the message names the absolute path of a.yammm (B14)",
		"the closing import draws no E_UPSTREAM_FAIL": "b.yammm's import of a.yammm also reports that a.yammm failed to compile (B14)",
	}

	rows := []struct {
		name   string
		ok     bool
		detail string
	}{
		{"exactly one E_IMPORT_CYCLE", len(cycles) == 1, fmt.Sprintf("%d E_IMPORT_CYCLE issues", len(cycles))},
		{
			"it is reported on the closing import",
			len(cycles) == 1 && cycles[0].HasSpan() && cycles[0].Span().Source == bID && cycles[0].Span().Start.Line == 3,
			fmt.Sprintf("spans %q, want %s line 3", spans, bID),
		},
		{"its message names no source file", namesNoFile, fmt.Sprintf("messages %q", messages)},
		{"the closing import draws no E_UPSTREAM_FAIL", !on(upstream, bID), fmt.Sprintf("E_UPSTREAM_FAIL on %s", bID)},
		{"the import of the schema the cycle failed still draws E_UPSTREAM_FAIL", on(upstream, aID), fmt.Sprintf("no E_UPSTREAM_FAIL on %s", aID)},
	}

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			reason, broken := knownBroken[row.name]
			switch {
			case broken && row.ok:
				t.Errorf("listed as broken (%s) and now passes: remove its knownBroken entry", reason)
			case broken:
				t.Logf("known broken: %s: %s", reason, row.detail)
			case !row.ok:
				t.Error(row.detail)
			}
		})
	}
	for name := range knownBroken {
		if !names[name] {
			t.Errorf("knownBroken names no row: %q", name)
		}
	}
}

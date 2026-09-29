package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/doclint"

	// Each adapter registers its own codes with diag when it is imported.
	_ "github.com/simon-lentz/yammm/adapter/csv"
	_ "github.com/simon-lentz/yammm/adapter/neo4j"
)

// TestModuleCitesRegisteredCodes points the code-citation checker at the
// module. A code renamed or removed in diag leaves its name behind in godoc and
// in the Markdown corpus, and nothing else reads those names against the
// registry.
func TestModuleCitesRegisteredCodes(t *testing.T) {
	t.Parallel()
	var codes []string
	for _, c := range diag.AllCodes() {
		codes = append(codes, c.String())
	}
	n := doclint.AssertCitedCodesExist(t, "..", doclint.CodeRules{
		Codes: codes,
		Exclude: []string{
			// Trigger queries are what a user might type, not claims about yammm.
			"claude-plugin/skills/*/evals/*.md",
		},
		// The change record names the codes releases removed or renamed. Every
		// other name it cites, in its policy sections and in Unreleased among
		// them, must be registered.
		Removed: map[string][]string{
			"docs/VERSIONING.md": {
				"E_INVALID_RELATION",
				"E_LIMIT_REACHED",
				"E_MISSING_PRIMARY_KEY",
				"E_MISSING_TYPE_TAG",
				"E_RELATION_NORMALIZATION_COLLISION",
				"E_SCHEMA_TYPE_NOT_FOUND",
				"E_SNAPSHOT_PATH_FALLBACK",
				"E_SNAPSHOT_TYPEID_MISMATCH",
				"E_TYPE_COLLISION",
				"E_UNKNOWN_BUILTIN",
			},
		},
		// NewCode's example registers a code, so it names one diag does not hold.
		Placeholders: []string{"E_MY_ERROR"},
	})
	// Each floor sits just under the count at the tree that set it, so a walk
	// that stops reaching the source fails instead of passing over nothing.
	if n.Comments < commentCitationFloor {
		t.Errorf("read only %d code names in Go comments; the walk is not reaching the source", n.Comments)
	}
	if n.Markdown < markdownCitationFloor {
		t.Errorf("read only %d code names in Markdown; the walk is not reaching the corpus", n.Markdown)
	}
	t.Logf("read %d code names in Go comments and %d in Markdown", n.Comments, n.Markdown)
}

const (
	commentCitationFloor  = 655
	markdownCitationFloor = 925
)

package main

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestLoadWarningsRenderedByEveryCommand pins that a load warning reaches the
// user from every command that loads a schema, not just validate.
//
// W_ANNOTATION_SHADOWED is the sole guard against a subtype silently dropping an
// inherited @writeOnce — Property.Equal deliberately ignores annotations so the
// re-declaration is not a structural conflict. Rendering it only on a failed
// load, as the neo4j commands did, left the schema loading clean, the warning
// unprinted, and the property mutable on every re-ingestion.
//
// The exit code stays ExitOK: a warning is not a failure. Every command that
// loads a schema routes through renderLoadDiagnostics; these are the ones a
// schema path alone can drive.
func TestLoadWarningsRenderedByEveryCommand(t *testing.T) {
	t.Parallel()

	const fixture = "testdata/annotation_shadowed.yammm"
	tests := []struct {
		name string
		args []string
	}{
		{"validate", []string{"validate", fixture}},
		{"neo4j indexes", []string{"neo4j", "indexes", fixture}},
		{"neo4j constraints", []string{"neo4j", "constraints", fixture}},
		{"gen md", []string{"gen", "--to", "md", fixture}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stderr := executeCmdStderr(t, tt.args...)
			if code != cli.ExitOK {
				t.Errorf("exit code = %d, want %d: a warnings-only load must not fail the command", code, cli.ExitOK)
			}
			if !strings.Contains(stderr, "W_ANNOTATION_SHADOWED") {
				t.Errorf("the shadowed-annotation warning must reach stderr; got:\n%s", stderr)
			}
		})
	}
}

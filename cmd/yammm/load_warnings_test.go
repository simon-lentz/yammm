package main

import (
	"encoding/json"
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
// loads a schema routes through reportSchemaLoad; these are the ones a schema
// path alone can drive.
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

// TestJSONDiagnosticsAreOneDocumentPerInvocation pins the JSON wire contract: a
// command writes AT MOST ONE JSON document to stderr, whatever phases it runs.
//
// Two-phase commands rendered the schema load's diagnostics and then their own,
// which in --format json wrote two complete JSON objects back to back. Once load
// warnings became reachable, any schema carrying one plus a data phase that
// reported anything produced output that json.Unmarshal, json.loads, and
// JSON.parse all reject with a trailing-data error.
func TestJSONDiagnosticsAreOneDocumentPerInvocation(t *testing.T) {
	t.Parallel()

	const (
		schemaPath = "testdata/annotation_shadowed.yammm"
		dataPath   = "testdata/annotation_shadowed_bad.json"
	)
	tests := []struct {
		name string
		args []string
	}{
		{"check", []string{"check", "--format", "json", schemaPath, dataPath}},
		{"load", []string{"load", "--format", "json", schemaPath, dataPath}},
		{"export", []string{"export", "--format", "json", "--to", "json", schemaPath, dataPath}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr := executeCmdStderr(t, tt.args...)

			dec := json.NewDecoder(strings.NewReader(stderr))
			var doc json.RawMessage
			if err := dec.Decode(&doc); err != nil {
				t.Fatalf("stderr is not a JSON document: %v\ngot:\n%s", err, stderr)
			}
			if dec.More() {
				t.Errorf("stderr carries more than one JSON document; got:\n%s", stderr)
			}

			// The single document must carry BOTH phases' diagnostics — folding
			// must not silently discard the load's warning.
			if !strings.Contains(string(doc), "W_ANNOTATION_SHADOWED") {
				t.Errorf("the one document should carry the load warning; got:\n%s", doc)
			}
			if !strings.Contains(string(doc), "E_CONSTRAINT_FAIL") {
				t.Errorf("the one document should carry the data-phase error; got:\n%s", doc)
			}
		})
	}
}

package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_StderrIsOneJSONDocument reads the byte stream a shell redirect
// captures: under --format json a failing invocation's stderr is exactly one
// JSON document or nothing, with the failure inside the document rather than
// printed beside it. It drives run(), so what run() itself writes is in scope.
func TestRun_StderrIsOneJSONDocument(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent")
	corrupt := filepath.Join(dir, "corrupt.ys")
	if err := os.WriteFile(corrupt, []byte(`{"yammm_snapshot":{`), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	broken := filepath.Join(dir, "broken.yammm")
	if err := os.WriteFile(broken, []byte("schema \"b\"\ntype A {\n\tid String primary\n\tname\n}\n"), 0o600); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	tests := []struct{ name, cmd string }{
		{"validate, unreadable schema", "validate " + missing},
		{"validate, invalid schema", "validate testdata/invalid.yammm"},
		{"fmt, unreadable path", "fmt " + missing},
		{"fmt, syntax error", "fmt --check " + broken},
		{"check, unreadable schema", "check " + missing + " testdata/data.json"},
		{"check, unreadable data", "check " + shadowedSchema + " " + missing + ".json"},
		{"check, undetectable data format", "check --from xml " + shadowedSchema + " testdata/data.json"},
		{"check, csv without --type", "check " + shadowedSchema + " testdata/data.csv"},
		{"check, validation errors", "check " + shadowedSchema + " testdata/annotation_shadowed_bad.json"},
		{"check, wrong arity", "check " + shadowedSchema},
		{"load, unreadable schema", "load " + missing + " testdata/data.json"},
		{"load, unreadable data", "load " + shadowedSchema + " " + missing + ".json"},
		{"export, unreadable schema", "export --to json " + missing + " testdata/data.json"},
		{"export, unreadable data", "export --to json " + shadowedSchema + " " + missing + ".json"},
		{"export, unsupported target", "export --to xml " + shadowedSchema + " testdata/data.json"},
		{"export, contradictory destinations", "export --to csv --output " + missing + " --output-dir " + dir + " " + shadowedSchema + " testdata/data.json"},
		{"export, unwritable output", "export --to json --output " + missing + "/x.json " + shadowedSchema + " testdata/annotation_shadowed_good.json"},
		{"export, invalid label prefix", "export --to cypher --prefix 1bad- testdata/valid.yammm testdata/data.json"},
		{"gen, unreadable schema", "gen --to go " + missing},
		{"gen, unsupported target", "gen --to rust " + shadowedSchema},
		{"snapshot save, unreadable schema", "snapshot save -o " + missing + ".ys " + missing + " testdata/data.json"},
		{"snapshot save, undetectable data format", "snapshot save -o " + missing + ".ys " + shadowedSchema + " " + shadowedSchema},
		{"snapshot save, no destination", "snapshot save " + shadowedSchema + " testdata/data.json"},
		{"snapshot info, unreadable file", "snapshot info " + missing + ".ys"},
		{"snapshot info, corrupt file", "snapshot info " + corrupt},
		{"snapshot info, header-only warning", "snapshot info --header-only " + hashAlgoFixture},
		{"snapshot info, body warning", "snapshot info " + provenanceFixture},
		{"snapshot info, unreadable directory", "snapshot info --dir " + missing},
		{"snapshot info, no argument", "snapshot info"},
		{"snapshot verify, unreadable schema", "snapshot verify " + missing + " " + corrupt},
		{"snapshot verify, corrupt snapshot", "snapshot verify " + shadowedSchema + " " + corrupt},
		{"snapshot update-metadata, unreadable file", "snapshot update-metadata -s a=b " + missing + ".ys"},
		{"snapshot update-metadata, no operation", "snapshot update-metadata " + corrupt},
		{"snapshot update-metadata, header warning", "snapshot update-metadata -s a=b " + hashAlgoFixture},
		{"neo4j constraints, unreadable schema", "neo4j constraints " + missing},
		{"neo4j constraints, unrecognized edition", "neo4j constraints --edition bogus " + shadowedSchema},
		{"neo4j indexes, unreadable schema", "neo4j indexes " + missing},
		{"neo4j diff, no --uri", "neo4j diff " + shadowedSchema},
		{"neo4j introspect, no --uri", "neo4j introspect"},
		{"neo4j introspect, unreachable server", "neo4j introspect --uri bolt://127.0.0.1:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--format", "json"}, strings.Fields(tt.cmd)...)
			_, _, stderr := runCLI(t, args...)
			checkRepairState(t, "one JSON document: "+tt.name, oneJSONDocumentOutcome(stderr))
		})
	}
}

// oneJSONDocumentOutcome reports stderr that is neither empty nor exactly one
// JSON document.
func oneJSONDocumentOutcome(stderr string) string {
	if strings.TrimSpace(stderr) == "" {
		return ""
	}
	dec := json.NewDecoder(strings.NewReader(stderr))
	var doc json.RawMessage
	if err := dec.Decode(&doc); err != nil {
		return "stderr is neither empty nor a JSON document: " + err.Error() + "\n" + stderr
	}
	if err := dec.Decode(&doc); !errors.Is(err, io.EOF) {
		return "stderr carries more than one JSON document, or prose beside one:\n" + stderr
	}
	return ""
}

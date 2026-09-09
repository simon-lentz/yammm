package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// The snapshot fixtures below are edited saved snapshots whose integrity_hash
// was recomputed the way the writer computes it, so the only diagnostic either
// draws is the one it is named for. valid_snapshot.ys is the same document
// unedited, and its cleanliness is what proves the recomputation.
const (
	// hashAlgoFixture states an unrecognized hash algorithm, which a
	// header-only read reports as a Warning and a body read as an Error.
	hashAlgoFixture = "testdata/hash_algo_unsupported.ys"
	// provenanceFixture is the only input that makes snapshot.Info return
	// warnings and no errors: an instance provenance path that will not parse.
	provenanceFixture = "testdata/provenance_unparseable.ys"
	// shadowedSchema loads clean with W_ANNOTATION_SHADOWED, so it is the
	// load-warning probe for every command that loads a schema.
	shadowedSchema = "testdata/annotation_shadowed.yammm"
)

// TestDiagnosticsSurviveEveryEarlyReturn pins B13, B14, B15 and B16: a command
// that returns before its own phase completes discarded the schema load's
// diagnostics entirely, because the render happened at the end of the happy
// path and nowhere else.
//
// Every row loads a schema that produces W_ANNOTATION_SHADOWED and then fails
// on something later, so the warning is the only thing under test. The control
// is the clean path, where the same warning has always rendered.
func TestDiagnosticsSurviveEveryEarlyReturn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missingData := filepath.Join(dir, "nonexistent.json")

	tests := []struct {
		name string
		args []string
	}{
		{"check, unreadable data file", []string{"check", shadowedSchema, missingData}},
		{"check, undetectable data format", []string{"check", "--from", "xml", shadowedSchema, missingData}},
		{"export, unreadable data file", []string{"export", "--to", "json", shadowedSchema, missingData}},
		{"load, unreadable data file", []string{"load", shadowedSchema, missingData}},
		{"snapshot save, undetectable data format", []string{
			"snapshot", "save", "-o", filepath.Join(dir, "out.ys"), shadowedSchema, shadowedSchema,
		}},
		{"neo4j constraints, unrecognized edition", []string{
			"neo4j", "constraints", "--edition", "bogus", shadowedSchema,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr := executeCmdStderr(t, tt.args...)
			if !strings.Contains(stderr, "W_ANNOTATION_SHADOWED") {
				t.Errorf("the schema load's warning was discarded by an early return; stderr:\n%s", stderr)
			}
		})
	}
}

// TestSnapshotInfoSurfacesWarnings pins B10 at both sites where a warning is
// reachable. Each gated its render on HasErrors, so a result carrying only
// warnings printed the summary, exited 0, and wrote nothing at all to stderr —
// the operator read a report of a document whose identity could not be checked
// and was told nothing.
func TestSnapshotInfoSurfacesWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		code string
	}{
		{
			name: "header-only, unrecognized hash algorithm",
			args: []string{"snapshot", "info", "--header-only", hashAlgoFixture},
			code: "E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM",
		},
		{
			name: "full read, unparseable provenance path",
			args: []string{"snapshot", "info", provenanceFixture},
			code: "E_SNAPSHOT_PATH_FALLBACK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, stdout, stderr := executeCmdOutput(t, tt.args...)
			if code != cli.ExitOK {
				t.Errorf("exit code = %d, want %d: a warning is not a failure", code, cli.ExitOK)
			}
			if !strings.Contains(stderr, tt.code) {
				t.Errorf("stderr does not carry %s; got:\n%s", tt.code, stderr)
			}
			if !strings.Contains(stdout, "Snapshot: test") {
				t.Errorf("the summary must still print; stdout:\n%s", stdout)
			}
		})
	}
}

// TestSnapshotInfoDirMarksWarnings pins B11. printDirEntries branched on
// HasErrors alone, so an entry carrying only warnings printed "ok" and was
// counted among the healthy files — the one place a dispatch-style scan reads.
func TestSnapshotInfoDirMarksWarnings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	copyFile(t, hashAlgoFixture, filepath.Join(dir, "unverifiable.ys"))
	copyFile(t, "testdata/valid_snapshot.ys", filepath.Join(dir, "clean.ys"))

	t.Run("text", func(t *testing.T) {
		t.Parallel()
		code, stdout, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir)
		if code != cli.ExitOK {
			t.Errorf("exit code = %d, want %d: a warning is not a failure", code, cli.ExitOK)
		}
		line := lineContaining(t, stdout, "unverifiable.ys")
		if !strings.Contains(line, "warn") {
			t.Errorf("the warned entry is not marked; line: %q", line)
		}
		if !strings.Contains(stdout, "1 ok, 1 warn") {
			t.Errorf("the summary must count warned entries apart from healthy ones; stdout:\n%s", stdout)
		}
		if clean := lineContaining(t, stdout, "clean.ys"); !strings.Contains(clean, "ok") {
			t.Errorf("the clean entry must still read ok; line: %q", clean)
		}
	})

	t.Run("an unreadable directory says why", func(t *testing.T) {
		t.Parallel()
		code, _, stderr := executeCmdOutput(t, "snapshot", "info", "--dir", filepath.Join(dir, "absent"))
		if code != cli.ExitRuntime {
			t.Errorf("exit code = %d, want %d", code, cli.ExitRuntime)
		}
		if !strings.Contains(stderr, "E_SNAPSHOT_IO") {
			t.Errorf("the scan's own failure must render; stderr:\n%s", stderr)
		}
	})

	t.Run("json still carries the issue", func(t *testing.T) {
		t.Parallel()
		_, stdout, _ := executeCmdOutput(t, "snapshot", "info", "--dir", dir, "--format", "json")
		if !strings.Contains(stdout, "E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM") {
			t.Errorf("the JSON entry must carry the warning; stdout:\n%s", stdout)
		}
	})
}

// TestOwnPhaseDiagnosticsReachTheOperator pins the results a command produces
// after the schema has loaded, on the two paths nothing else exercises: an
// export whose source snapshot could not be fully verified, and an emitter that
// refuses a property name Cypher reserves.
func TestOwnPhaseDiagnosticsReachTheOperator(t *testing.T) {
	t.Parallel()

	t.Run("export from a snapshot carrying a warning", func(t *testing.T) {
		t.Parallel()
		code, _, stderr := executeCmdOutput(t, "export", "--to", "json",
			"--output", filepath.Join(t.TempDir(), "out.json"),
			"testdata/valid.yammm", provenanceFixture)
		if code != cli.ExitOK {
			t.Errorf("exit code = %d, want %d: a warning is not a failure", code, cli.ExitOK)
		}
		if !strings.Contains(stderr, "E_SNAPSHOT_PATH_FALLBACK") {
			t.Errorf("the snapshot's warning was discarded; stderr:\n%s", stderr)
		}
	})

	t.Run("neo4j indexes refusing a reserved identifier", func(t *testing.T) {
		t.Parallel()
		code, _, stderr := executeCmdOutput(t, "neo4j", "indexes", "testdata/reserved_index.yammm")
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "E_NEO4J_INVALID_IDENTIFIER") {
			t.Errorf("the emitter's own error was discarded; stderr:\n%s", stderr)
		}
	})
}

// TestSnapshotSaveSurfacesImportedWarning pins the imported snapshot's own
// diagnostics on the path that succeeds: a merge whose source could not be
// fully verified still says so, and a warning does not fail the save.
func TestSnapshotSaveSurfacesImportedWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	into := filepath.Join(dir, "into.ys")
	copyFile(t, provenanceFixture, into)

	code, _, stderr := executeCmdOutput(t, "snapshot", "save",
		"--into", into, "-o", filepath.Join(dir, "merged.ys"),
		"testdata/valid.yammm", "testdata/data_carol.json")
	if code != cli.ExitOK {
		t.Errorf("exit code = %d, want %d: a warning is not a failure", code, cli.ExitOK)
	}
	if !strings.Contains(stderr, "E_SNAPSHOT_PATH_FALLBACK") {
		t.Errorf("the imported snapshot's warning was discarded; stderr:\n%s", stderr)
	}
}

// TestUpdateMetadataSurfacesHeaderWarning pins B12. The header read's result
// was rendered only when it carried errors, so the one warning HeaderOnly can
// produce was dropped — and the operator saw the later error alone, with no
// statement that the header had already been read and found unverifiable.
func TestUpdateMetadataSurfacesHeaderWarning(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "subject.ys")
	copyFile(t, hashAlgoFixture, path)

	code, _, stderr := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "a=b", path)
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	if !strings.Contains(stderr, "schema hash verification skipped") {
		t.Errorf("the header read's warning was discarded; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "the document cannot be checked against a schema") {
		t.Errorf("the update's own error must still render; stderr:\n%s", stderr)
	}
}

// TestDiagnosticsPrecedeThePayload pins the half of the render-once rule that a
// deferred render cannot supply: a command emitting a payload renders first, so
// an operator whose terminal carries both streams reads the warning above the
// report rather than below it.
//
// It needs one buffer for both streams. Captured separately, the two orderings
// are indistinguishable and the explicit Render calls are unasserted.
func TestDiagnosticsPrecedeThePayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		args           []string
		first, payload string
	}{
		{
			name:    "snapshot info --header-only",
			args:    []string{"snapshot", "info", "--header-only", hashAlgoFixture},
			first:   "E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM",
			payload: "Snapshot: test",
		},
		{
			name:    "snapshot info",
			args:    []string{"snapshot", "info", provenanceFixture},
			first:   "E_SNAPSHOT_PATH_FALLBACK",
			payload: "Snapshot: test",
		},
		{
			name:    "neo4j constraints",
			args:    []string{"neo4j", "constraints", shadowedSchema},
			first:   "W_ANNOTATION_SHADOWED",
			payload: "CREATE CONSTRAINT",
		},
		{
			name:    "neo4j indexes",
			args:    []string{"neo4j", "indexes", "testdata/annotation_shadowed_indexed.yammm"},
			first:   "W_ANNOTATION_SHADOWED",
			payload: "CREATE INDEX",
		},
		{
			name:    "export to stdout",
			args:    []string{"export", "--to", "json", shadowedSchema, "testdata/annotation_shadowed_good.json"},
			first:   "W_ANNOTATION_SHADOWED",
			payload: `"Doc"`,
		},
		{
			name:    "export from a snapshot, to stdout",
			args:    []string{"export", "--to", "json", "testdata/valid.yammm", provenanceFixture},
			first:   "E_SNAPSHOT_PATH_FALLBACK",
			payload: `"Person"`,
		},
		{
			name: "snapshot save, whose payload is its status line",
			args: []string{
				"snapshot", "save", "-o", filepath.Join(t.TempDir(), "made.snap"),
				shadowedSchema, "testdata/annotation_shadowed_good.json",
			},
			first:   "W_SNAPSHOT_PATH_EXTENSION",
			payload: "saved snapshot:",
		},
		{
			name:    "gen",
			args:    []string{"gen", "--to", "md", shadowedSchema},
			first:   "W_ANNOTATION_SHADOWED",
			payload: "# Schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			combined := executeCmdCombined(t, tt.args...)
			at, payloadAt := strings.Index(combined, tt.first), strings.Index(combined, tt.payload)
			if at < 0 {
				t.Fatalf("output does not carry %q; got:\n%s", tt.first, combined)
			}
			if payloadAt < 0 {
				t.Fatalf("output does not carry the payload %q; got:\n%s", tt.payload, combined)
			}
			if at > payloadAt {
				t.Errorf("the diagnostic follows the payload; got:\n%s", combined)
			}
		})
	}
}

// executeCmdCombined runs the CLI with both writers on one buffer, so a test
// can assert the order two streams reach a terminal in.
func executeCmdCombined(t *testing.T, args ...string) string {
	t.Helper()

	var buf bytes.Buffer
	cmd := newRootCmd("test")
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	_ = cmd.Execute()

	if t.Failed() {
		t.Logf("combined output:\n%s", buf.String())
	}
	return buf.String()
}

// TestStderrIsOneJSONDocument is the invariant reportSchemaLoad's godoc states
// and nothing enforced: one invocation renders one result, so under
// --format json stderr carries exactly one complete JSON document or nothing at
// all. Two documents concatenated are not parseable by any JSON reader.
//
// It covers every command in newRootCmd's tree and every failure mode reachable
// without a database, which is what makes it an invariant rather than a
// regression test for the paths that happened to be found broken.
func TestStderrIsOneJSONDocument(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent")
	corrupt := filepath.Join(dir, "corrupt.ys")
	if err := os.WriteFile(corrupt, []byte(`{"yammm_snapshot":{`), 0o600); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	tests := []struct{ name, cmd string }{
		{"validate, unreadable schema", "validate " + missing},
		{"validate, invalid schema", "validate testdata/invalid.yammm"},
		{"fmt, unreadable path", "fmt " + missing},
		{"check, unreadable schema", "check " + missing + " testdata/data.json"},
		{"check, unreadable data", "check " + shadowedSchema + " " + missing + ".json"},
		{"check, undetectable data format", "check --from xml " + shadowedSchema + " testdata/data.json"},
		{"check, csv without --type", "check " + shadowedSchema + " testdata/data.csv"},
		{"check, validation errors", "check " + shadowedSchema + " testdata/annotation_shadowed_bad.json"},
		{"load, unreadable schema", "load " + missing + " testdata/data.json"},
		{"load, unreadable data", "load " + shadowedSchema + " " + missing + ".json"},
		{"export, unreadable schema", "export --to json " + missing + " testdata/data.json"},
		{"export, unreadable data", "export --to json " + shadowedSchema + " " + missing + ".json"},
		{"export, unsupported target", "export --to xml " + shadowedSchema + " testdata/data.json"},
		{"export, contradictory destinations", "export --to csv --output " + missing + " --output-dir " + dir + " " + shadowedSchema + " testdata/data.json"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"--format", "json"}, strings.Fields(tt.cmd)...)
			_, _, stderr := executeCmdOutput(t, args...)
			assertAtMostOneJSONDocument(t, stderr)
		})
	}
}

// TestStderrIsOneJSONDocumentOnSuccess is the invariant's other half, and the
// one the command-writer harness cannot see: a status summary written straight
// to the process stream sits beside the document and stops it parsing.
//
// It drives run(), so what it reads is the byte stream a shell redirect would
// capture. Success paths only — a failure adds run()'s own "error: " line,
// which is prose beside a document for a different reason and a different row.
func TestStderrIsOneJSONDocumentOnSuccess(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "made.ys")
	notYS := filepath.Join(dir, "made.snap")
	meta := filepath.Join(dir, "meta.ys")
	copyFile(t, "testdata/valid_snapshot.ys", meta)

	tests := []struct {
		name string
		args []string
	}{
		{"load, whose pipeline reports a count", []string{
			"load", "--format", "json", shadowedSchema, "testdata/annotation_shadowed_good.json",
		}},
		{"snapshot save, whose summary follows the document", []string{
			"snapshot", "save", "--format", "json", "-o", snap,
			shadowedSchema, "testdata/annotation_shadowed_good.json",
		}},
		{"snapshot save onto a path that is not .ys", []string{
			"snapshot", "save", "--format", "json", "-o", notYS,
			shadowedSchema, "testdata/annotation_shadowed_good.json",
		}},
		{"export --output-dir, which reports a file count", []string{
			"export", "--format", "json", "--to", "csv", "--output-dir", dir,
			shadowedSchema, "testdata/annotation_shadowed_good.json",
		}},
		{"snapshot update-metadata, whose summary is a status line", []string{
			"snapshot", "update-metadata", "--format", "json", "-s", "a=b", meta,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, tt.args...)
			if code != cli.ExitOK {
				t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
			}
			assertAtMostOneJSONDocument(t, stderr)
		})
	}
}

// TestStatusSummariesShareOneStream pins B51: update-metadata reported its
// result on stdout where every other command reports progress on stderr, so a
// caller redirecting the two apart got one command's summary in the payload
// channel.
func TestStatusSummariesShareOneStream(t *testing.T) {
	dir := t.TempDir()
	meta := filepath.Join(dir, "meta.ys")
	copyFile(t, "testdata/valid_snapshot.ys", meta)

	code, stdout, stderr := runCLI(t, "snapshot", "update-metadata", "-s", "env=prod", meta)
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
	}
	if !strings.Contains(stderr, "updated metadata") {
		t.Errorf("the summary must reach stderr, where every other command reports progress; stderr:\n%s", stderr)
	}
	if strings.Contains(stdout, "updated metadata") {
		t.Errorf("stdout is the payload channel and carries no summary; stdout:\n%s", stdout)
	}
}

// TestNonYSExtensionIsADiagnostic pins B23's warning half: a path that is not
// .ys is a fact about the artefact, so it reaches a machine consumer as a coded
// diagnostic rather than as prose no JSON reader can see.
func TestNonYSExtensionIsADiagnostic(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "made.snap")
	code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
		shadowedSchema, "testdata/annotation_shadowed_good.json")
	if code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitOK, stderr)
	}
	if !strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION") {
		t.Errorf("the extension warning must carry a code; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "warning[W_SNAPSHOT_PATH_EXTENSION]") {
		t.Errorf("the path was named deliberately and the write succeeded, so this is a warning; stderr:\n%s", stderr)
	}
}

// assertAtMostOneJSONDocument reports stderr that is neither empty nor exactly
// one JSON document. Prose beside a document fails the same way a second
// document does: a machine consumer reads the stream, not the intent.
func assertAtMostOneJSONDocument(t *testing.T, stderr string) {
	t.Helper()
	if strings.TrimSpace(stderr) == "" {
		return
	}
	dec := json.NewDecoder(strings.NewReader(stderr))
	var doc json.RawMessage
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stderr is neither empty nor a JSON document: %v\ngot:\n%s", err, stderr)
	}
	if err := dec.Decode(&doc); !errors.Is(err, io.EOF) {
		t.Errorf("stderr carries more than one JSON document (second decode: %v); got:\n%s", err, stderr)
	}
}

// lineContaining returns the single output line holding want.
func lineContaining(t *testing.T, out, want string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line contains %q; got:\n%s", want, out)
	return ""
}

// copyFile copies src to dst, so a fixture a command rewrites in place is never
// the tracked one.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

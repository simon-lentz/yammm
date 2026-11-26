package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every command that reads a .ys file places what the read drew in that file:
// a snapshot read names no source, as a JSON decoder names none, so the command
// names the operand, relative to where it ran.
func TestSnapshotRead_NamesTheFileItRead(t *testing.T) {
	schemaPath, err := filepath.Abs(filepath.Join("testdata", "valid.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	dataPath, err := filepath.Abs(filepath.Join("testdata", "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	const bad = "bad.ys"
	if err := os.WriteFile(bad, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A command that loads a schema names a file relative to the schema's
	// module root, as check does, and bad.ys lies outside it.
	for _, c := range []struct {
		args     []string
		relative bool
	}{
		{[]string{"snapshot", "info", bad}, true},
		{[]string{"snapshot", "info", "--header-only", bad}, true},
		{[]string{"snapshot", "verify", schemaPath, bad}, false},
		{[]string{"snapshot", "update-metadata", "--set", "k=v", bad}, true},
		{[]string{"snapshot", "save", schemaPath, dataPath, "--into", bad, "-o", "out.ys"}, false},
	} {
		args := c.args
		name := strings.Join(args[:2], " ")
		code, _, stderr := runCLI(t, args...)
		if code == 0 || !strings.Contains(stderr, "E_SNAPSHOT_MALFORMED") {
			t.Fatalf("%s: exit %d, want a refusal of %s: %s", name, code, bad, stderr)
		}
		if c.relative && !lineStartsWith(stderr, bad+": ") {
			t.Errorf("%s: no line names %s relative to where it ran: %s", name, bad, stderr)
		}
		if !c.relative && !strings.Contains(stderr, "/"+bad+": ") {
			t.Errorf("%s: the refusal does not name %s: %s", name, bad, stderr)
		}

		code, _, stderr = runCLI(t, append([]string{"--json"}, args...)...)
		var doc struct {
			Issues []struct {
				Code string `json:"code"`
				Span *struct {
					Source string `json:"source"`
				} `json:"span"`
			} `json:"issues"`
		}
		if err := json.Unmarshal([]byte(stderr), &doc); err != nil {
			t.Fatalf("%s --json: exit %d, stderr is no JSON document: %v\n%s", name, code, err, stderr)
		}
		for _, iss := range doc.Issues {
			if iss.Code == "E_SNAPSHOT_MALFORMED" && (iss.Span == nil || !strings.HasSuffix(iss.Span.Source, bad)) {
				t.Errorf("%s --json: %s carries no span naming %s: %s", name, iss.Code, bad, stderr)
			}
		}
	}
}

// A refusal past the header read, a tampered integrity hash, names the file
// too: update-metadata's own check and info's read each place it.
func TestSnapshotRead_NamesTheFileWhoseHashItRefuses(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(string(fixture), `"sha256:`)
	if at < 0 {
		t.Fatal("the fixture holds no sha256 hash")
	}
	digit := at + len(`"sha256:`)
	tampered := []byte(string(fixture))
	tampered[digit] = map[bool]byte{true: '1', false: '0'}[tampered[digit] == '0']
	t.Chdir(t.TempDir())
	const name = "tampered.ys"
	if err := os.WriteFile(name, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"snapshot", "info", name},
		{"snapshot", "update-metadata", "--set", "k=v", name},
	} {
		code, _, stderr := runCLI(t, args...)
		if code == 0 || !lineStartsWith(stderr, name+": error[E_SNAPSHOT_INTEGRITY_MISMATCH]") {
			t.Errorf("%s: exit %d, want the hash refusal placed in %s: %s", strings.Join(args[:2], " "), code, name, stderr)
		}
	}
}

// lineStartsWith reports whether a line of text starts with prefix.
func lineStartsWith(text, prefix string) bool {
	for line := range strings.Lines(text) {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

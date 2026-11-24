package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestSnapshotInfo_WritesNoControlCharacterFromTheDocument holds the text
// report to one rule: no byte of the document's own text reaches the terminal
// as a control. A header-only read and a directory scan print a header the
// integrity check never reads, and a full read prints metadata that
// update-metadata sealed, so each mode is shown a forged text it prints.
func TestSnapshotInfo_WritesNoControlCharacterFromTheDocument(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/valid_snapshot.ys")
	if err != nil {
		t.Fatal(err)
	}
	const name = `"schema_name":"test"`
	if !strings.Contains(string(data), name) {
		t.Fatalf("fixture shape changed; no %s", name)
	}
	headerDir := t.TempDir()
	header := filepath.Join(headerDir, "header.ys")
	forgedName := strings.Replace(string(data), name, `"schema_name":"te\u001b[31mst\nForged: yes"`, 1)
	if err := os.WriteFile(header, []byte(forgedName), 0o600); err != nil {
		t.Fatal(err)
	}

	sealed := filepath.Join(t.TempDir(), "sealed.ys")
	copyFile(t, "testdata/valid_snapshot.ys", sealed)
	if code, _, stderr := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v\x1b[31m\nForged: yes", sealed); code != 0 {
		t.Fatalf("update-metadata exited %d:\n%s", code, stderr)
	}

	for _, args := range [][]string{
		{"snapshot", "info", "--header-only", header},
		{"snapshot", "info", "--dir", headerDir},
		{"snapshot", "info", sealed},
	} {
		code, stdout, stderr := executeCmdOutput(t, args...)
		if code != 0 {
			t.Fatalf("%v exited %d:\n%s", args, code, stderr)
		}
		if strings.Contains(stdout, "\x1b") {
			t.Errorf("%v wrote an ESC:\n%q", args, stdout)
		}
		if strings.Contains(stdout, "\nForged: yes") {
			t.Errorf("%v forged a line:\n%s", args, stdout)
		}
		if !strings.Contains(stdout, "\u241b[31m") {
			t.Errorf("%v does not show the forged text with its control pictures:\n%s", args, stdout)
		}
	}
}

// forgedJSON is a JSON string body that decodes to an ESC and a newline that
// would start a forged line; forgedRaw is the same text as a file name holds it.
const (
	forgedJSON = `X\u001b[31mY\nForged`
	forgedRaw  = "X\x1b[31mY\nForged"
)

// forgeEveryField returns the fixture with each header text the report prints,
// and the type's name, set to forgedJSON. The report prints created_at as the
// time it reads, which holds no text of the document.
func forgeEveryField(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/valid_snapshot.ys")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, r := range [][2]string{
		{`"schema_name":"test"`, `"schema_name":"` + forgedJSON + `"`},
		{`"schema_hash":"sha256:`, `"schema_hash":"` + forgedJSON + `sha256:`},
		{`"features":[]`, `"metadata":{"k` + forgedJSON + `":"` + forgedJSON + `"},"features":[]`},
		{`"name":"Person"`, `"name":"Per` + forgedJSON + `"`},
	} {
		if !strings.Contains(doc, r[0]) {
			t.Fatalf("fixture shape changed; no %s", r[0])
		}
		doc = strings.Replace(doc, r[0], r[1], 1)
	}
	return doc
}

var integrityMember = regexp.MustCompile(`"integrity_hash":"[^"]*"`)

// seal sets doc's integrity_hash to the hash of doc with that member empty,
// the form the snapshot reader hashes.
func seal(doc string) string {
	blank := integrityMember.ReplaceAllLiteralString(doc, `"integrity_hash":""`)
	return strings.Replace(blank, `"integrity_hash":""`, fmt.Sprintf(`"integrity_hash":"sha256:%x"`, sha256.Sum256([]byte(blank))), 1)
}

// checkNoControl fails when out holds a raw ESC or a forged line, or does not
// show the forged text with its control pictures.
func checkNoControl(t *testing.T, what, out string) {
	t.Helper()
	if strings.Contains(out, "\x1b") {
		t.Errorf("%s wrote an ESC:\n%q", what, out)
	}
	if strings.Contains(out, "\nForged") {
		t.Errorf("%s forged a line:\n%s", what, out)
	}
	if !strings.Contains(out, "X\u241b[31mY\u240aForged") {
		t.Errorf("%s does not show the forged text with its control pictures:\n%q", what, out)
	}
}

// TestSnapshotInfo_EscapesEveryFieldItPrints forges every header text and the
// type's name, so each text the report prints is shown a control: a full read
// of the sealed document, and a header-only read whose integrity hash is
// forged too, since that read never verifies it.
func TestSnapshotInfo_EscapesEveryFieldItPrints(t *testing.T) {
	t.Parallel()

	doc := forgeEveryField(t)
	dir := t.TempDir()
	sealed := filepath.Join(dir, "sealed.ys")
	if err := os.WriteFile(sealed, []byte(seal(doc)), 0o600); err != nil {
		t.Fatal(err)
	}
	header := filepath.Join(dir, "header.ys")
	unsealed := integrityMember.ReplaceAllLiteralString(doc, `"integrity_hash":"`+forgedJSON+`"`)
	if err := os.WriteFile(header, []byte(unsealed), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		args  []string
		lines []string
	}{
		{[]string{"snapshot", "info", sealed}, []string{"Snapshot:", "Schema:", "Hash:", "Metadata:", "Per"}},
		{[]string{"snapshot", "info", "--header-only", header}, []string{"Snapshot:", "Schema:", "Hash:", "Integrity:", "Metadata:", "Per"}},
	} {
		code, stdout, stderr := executeCmdOutput(t, c.args...)
		if code != 0 {
			t.Fatalf("%v exited %d:\n%s", c.args, code, stderr)
		}
		checkNoControl(t, fmt.Sprint(c.args), stdout)
		for line := range strings.Lines(stdout) {
			for i, label := range c.lines {
				if strings.Contains(line, label) && strings.Contains(line, "X\u241b[31mY\u240aForged") {
					c.lines[i] = ""
				}
			}
		}
		for _, label := range c.lines {
			if label != "" {
				t.Errorf("%v: no %q line shows the forged text:\n%s", c.args, label, stdout)
			}
		}
	}
}

// TestSnapshotInfo_DirEscapesEveryRow holds a directory scan to the rule for
// every text it prints: the directory's path, each entry's name in an ok, a
// malformed, an unreadable and a refused row, and an error message, which
// carries the host path.
func TestSnapshotInfo_DirEscapesEveryRow(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses a control character in a file name")
	}

	data, err := os.ReadFile("testdata/valid_snapshot.ys")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	dir := filepath.Join(t.TempDir(), "d"+forgedRaw)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("ok"+forgedRaw+".ys", src)
	write("malformed"+forgedRaw+".ys", "{not json")
	write("refused"+forgedRaw+".ys", strings.Replace(src, `"features":[]`, `"features":["zz"]`, 1))
	unreadable := write("unreadable"+forgedRaw+".ys", src)
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(unreadable); err == nil {
		t.Skip("the process reads a file whose mode grants nothing")
	}

	_, stdout, stderr := executeCmdOutput(t, "snapshot", "info", "--dir", dir)
	checkNoControl(t, "snapshot info --dir", stdout)
	for _, want := range []string{
		"Directory: ",
		"okX\u241b[31mY\u240aForged.ys",
		"malformedX\u241b[31mY\u240aForged.ys",
		"refusedX\u241b[31mY\u240aForged.ys",
		"unreadableX\u241b[31mY\u240aForged.ys",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no row shows %q:\n%s%s", want, stdout, stderr)
		}
	}
	if n := strings.Count(stdout, "X\u241b[31mY\u240aForged"); n < 7 {
		t.Errorf("%d texts show the forged text, want the directory, four names and an error message that names both:\n%s", n, stdout)
	}
}

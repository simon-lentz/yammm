package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// snapshot info prints the created_at time the header states as RFC 3339 in
// the time's own offset, with no trailing zero in its fraction and a zero
// offset as Z, in every mode, and "not set" or "" where the header states none.
func TestSnapshotInfo_PrintsCreatedAtAsTheHeaderStatesIt(t *testing.T) {
	t.Parallel()
	const stated = "2026-01-01T12:00:00.123456+02:00"

	stamped := filepath.Join(t.TempDir(), "stamped.ys")
	saveFixture(t, stamped)
	writeForeignSnapshot(t, stamped, stated)
	bare := filepath.Join(t.TempDir(), "bare.ys")
	saveFixture(t, bare)

	for _, run := range []struct {
		args       []string
		set, unset string
	}{
		{[]string{"snapshot", "info"}, "Created:    " + stated + "\n", "Created:    not set\n"},
		{[]string{"snapshot", "info", "--header-only"}, "Created:    " + stated + "\n", "Created:    not set\n"},
		{[]string{"snapshot", "info", "--json"}, `"created_at": "` + stated + `"`, `"created_at": ""`},
		{[]string{"snapshot", "info", "--header-only", "--json"}, `"created_at": "` + stated + `"`, `"created_at": ""`},
		{[]string{"snapshot", "info", "--dir"}, "created=" + stated, "created=not set"},
	} {
		for file, want := range map[string]string{stamped: run.set, bare: run.unset} {
			operand := file
			if run.args[len(run.args)-1] == "--dir" {
				operand = filepath.Dir(file)
			}
			code, out, errOut := executeCmdOutput(t, append(run.args, operand)...)
			if code != cli.ExitOK {
				t.Fatalf("%v %s: exit %d\n%s", run.args, operand, code, errOut)
			}
			if !strings.Contains(out, want) {
				t.Errorf("%v %s does not print %q:\n%s", run.args, filepath.Base(file), want, out)
			}
		}
	}
}

// --timestamp stamps the current time in UTC at second precision.
func TestSnapshotSave_TimestampWritesWholeSecondsInUTC(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.ys")
	before := time.Now().UTC().Truncate(time.Second)
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--timestamp",
		"testdata/valid.yammm", "testdata/data.json", "-o", path)
	if code != cli.ExitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	after := time.Now().UTC()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`"created_at":"([^"]*)"`).FindSubmatch(data)
	if m == nil || !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`).Match(m[1]) {
		t.Fatalf("created_at is not whole seconds in UTC: %q", m)
	}
	stamp, err := time.Parse(time.RFC3339, string(m[1]))
	if err != nil || stamp.Before(before) || stamp.After(after) {
		t.Errorf("created_at %s is not the time of the save, between %s and %s", m[1], before, after)
	}
}

// A read error names the path as the operator spelled it, in the full mode and
// with --header-only, as an open error does.
func TestSnapshotInfo_AReadErrorNamesThePathAsSpelled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, sub := range []string{"dd", "sub"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	sep := string(filepath.Separator)
	spelled := dir + sep + "sub" + sep + ".." + sep + "dd"
	for _, args := range [][]string{{"snapshot", "info", spelled}, {"snapshot", "info", "--header-only", spelled}} {
		var errBuf bytes.Buffer
		cmd := newRootCmd("test")
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)
		cmd.SetArgs(args)
		err := execute(cmd)
		reported := errBuf.String()
		if err != nil {
			reported += err.Error()
		}
		if cli.ExitForError(err) == cli.ExitOK || !strings.Contains(reported, "read "+spelled+": ") {
			t.Errorf("%v: %v, want a read error naming %s:\n%s", args, err, spelled, reported)
		}
	}
}

// update-metadata checks the integrity hash and the document's shape, not the
// body's instances: a body its writer sealed is updated though snapshot info
// refuses it.
func TestSnapshotUpdateMetadata_UpdatesASealedBodyItDoesNotCheck(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "dup.ys")
	saveFixture(t, path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dup := bytes.ReplaceAll(data, []byte(`"bob"`), []byte(`"alice"`))
	if bytes.Equal(dup, data) {
		t.Fatal("fixture shape changed; no bob")
	}
	if err := os.WriteFile(path, resealed(t, dup), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := executeCmdOutput(t, "snapshot", "info", path); code != cli.ExitValidation {
		t.Fatalf("snapshot info exit %d, want %d: the fixture must hold a body info refuses", code, cli.ExitValidation)
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "update-metadata", "-s", "k=v", path)
	if code != cli.ExitOK {
		t.Fatalf("update-metadata exit %d\n%s", code, errOut)
	}
	if got := readHeader(t, path).Metadata["k"]; got != "v" {
		t.Errorf("metadata k = %q, want v", got)
	}
}

// resealed returns data under the integrity hash its content has.
func resealed(t *testing.T, data []byte) []byte {
	t.Helper()
	hash := regexp.MustCompile(`"integrity_hash":"[^"]*"`)
	if n := len(hash.FindAllIndex(data, -1)); n != 1 {
		t.Fatalf("%d integrity_hash members, want 1", n)
	}
	blank := hash.ReplaceAll(data, []byte(`"integrity_hash":""`))
	return hash.ReplaceAll(data, fmt.Appendf(nil, `"integrity_hash":"sha256:%x"`, sha256.Sum256(blank)))
}

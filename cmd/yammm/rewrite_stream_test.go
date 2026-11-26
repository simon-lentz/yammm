package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/location"
)

// A command that writes back what it read refuses a descriptor path to that
// file at exit 3 and leaves the file as it was: a write through the descriptor
// appends after the bytes already there, so the file would hold two documents.
// A descriptor open for writing alone, as a shell's ">>" opens it, is refused
// too.
func TestRewrite_RefusesADescriptorPathToTheFileRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor path")
	}
	schemaPath, err := filepath.Abs(filepath.Join("testdata", "valid.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	dataPath, err := filepath.Abs(filepath.Join("testdata", "data_carol.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshotFixture, err := os.ReadFile(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		content  []byte
		args     func(fd, file string) []string
		openFlag int
	}{
		{"fmt -w", []byte(fmtUnformatted), func(fd, _ string) []string { return []string{"fmt", "-w", fd} }, os.O_RDWR},
		{"snapshot update-metadata", snapshotFixture, func(fd, _ string) []string {
			return []string{"snapshot", "update-metadata", "--set", "k=v", fd}
		}, os.O_RDWR},
		{"snapshot save --into the file -o its descriptor", snapshotFixture, func(fd, file string) []string {
			return []string{"snapshot", "save", schemaPath, dataPath, "--into", file, "-o", fd}
		}, os.O_RDWR},
		{"snapshot save --into its descriptor", snapshotFixture, func(fd, _ string) []string {
			return []string{"snapshot", "save", schemaPath, dataPath, "--into", fd}
		}, os.O_RDWR},
		{"snapshot save --into the file -o a write-only descriptor", snapshotFixture, func(fd, file string) []string {
			return []string{"snapshot", "save", schemaPath, dataPath, "--into", file, "-o", fd}
		}, os.O_WRONLY | os.O_APPEND},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "x")
			if err := os.WriteFile(file, c.content, 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(file, c.openFlag, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			code, _, stderr := executeCmdOutput(t, c.args(fmt.Sprintf("/dev/fd/%d", f.Fd()), file)...)
			if code != cli.ExitRuntime {
				t.Errorf("exit %d, want %d: %s", code, cli.ExitRuntime, stderr)
			}
			if got, _ := os.ReadFile(file); string(got) != string(c.content) {
				t.Errorf("the file changed: %d bytes, was %d", len(got), len(c.content))
			}
		})
	}
}

// fmt -w judges every operand before it formats any, so a stream it would
// write back leaves the other operands as they were.
func TestFmt_RefusesAStreamOperandBeforeFormattingAny(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor path")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.yammm")
	streamed := filepath.Join(dir, "b.yammm")
	for _, p := range []string{plain, streamed} {
		if err := os.WriteFile(p, []byte(fmtUnformatted), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(streamed, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	code, _, stderr := executeCmdOutput(t, "fmt", "-w", fmt.Sprintf("/dev/fd/%d", f.Fd()), plain)
	if code != cli.ExitRuntime {
		t.Errorf("exit %d, want %d: %s", code, cli.ExitRuntime, stderr)
	}
	for _, p := range []string{plain, streamed} {
		if got, _ := os.ReadFile(p); string(got) != fmtUnformatted {
			t.Errorf("%s was formatted: %q", filepath.Base(p), got)
		}
	}
}

// TestSnapshotSave_WarnsOnTheExtensionOfTheFileWritten holds the warning to what
// a scan for .ys files finds. "out.ys/x/.." and "out/../out.ys" write a file
// whose name ends in .ys, so neither warns. A link is found by its own name or
// by any name the write passes through, so only a chain in which no name ends
// in .ys warns, and a .ys name that reaches no regular file, which a scan
// skips, warns too.
func TestSnapshotSave_WarnsOnTheExtensionOfTheFileWritten(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	type link struct{ name, dest string }
	for _, c := range []struct {
		spelled, written string
		links            []link
		warns            bool
	}{
		{spelled: "out.ys" + sep + "x" + sep + "..", written: "out.ys"},
		{spelled: "out" + sep + ".." + sep + "out.ys", written: "out.ys"},
		{spelled: "out.ys" + sep + ".." + sep + "out", written: "out", warns: true},
		{spelled: "lnk", written: "real.ys", links: []link{{"lnk", "real.ys"}}},
		{spelled: "lnk.ys", written: "real", links: []link{{"lnk.ys", "real"}}},
		{spelled: "lnk", written: "real", links: []link{{"lnk", "real"}}, warns: true},
		{spelled: "a", written: "c", links: []link{{"a", "b.ys"}, {"b.ys", "c"}}},
		{spelled: "a", written: "c", links: []link{{"a", "sub" + sep + "b.ys"}, {"sub" + sep + "b.ys", ".." + sep + "c"}}},
		{spelled: "a", written: "c", links: []link{{"a", "b"}, {"b", "c"}}, warns: true},
		{spelled: "null.ys", links: []link{{"null.ys", os.DevNull}}, warns: true},
	} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		linked := true
		for _, l := range c.links {
			if err := os.Symlink(l.dest, filepath.Join(dir, l.name)); err != nil {
				if runtime.GOOS == "windows" {
					linked = false
					break
				}
				t.Fatal(err)
			}
		}
		if !linked || (c.written == "" && runtime.GOOS == "windows") {
			continue
		}
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", filepath.Join("testdata", "valid.yammm"),
			filepath.Join("testdata", "data.json"), "-o", dir+sep+c.spelled)
		if code != cli.ExitOK {
			t.Fatalf("%s: exit %d: %s", c.spelled, code, stderr)
		}
		if c.written != "" {
			if _, err := os.Stat(filepath.Join(dir, c.written)); err != nil {
				t.Errorf("%s: %s was not written: %v", c.spelled, c.written, err)
			}
		}
		if got := strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION"); got != c.warns {
			t.Errorf("%s via %v: warns %v, want %v: %s", c.spelled, c.links, got, c.warns, stderr)
		}
	}
}

// The extension warning names the file the output path reaches, relative to the
// schema's module root, however the path is spelled: through a linked
// directory, with a ".." element, in another case on a volume that ignores
// case, as a link to the file, or as the host spells it, which on Windows is
// with backslashes. Its message quotes the path as typed, and --json places it
// as a snapshot read's issues are placed: the file's identity as the span's
// source.
func TestSnapshotSave_NamesTheOutputUnderTheRootRelatively(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	for _, c := range []struct {
		name, label string
		out         func(t *testing.T, dir string) string
	}{
		{"as given", "out.dat", func(_ *testing.T, dir string) string { return filepath.Join(dir, "out.dat") }},
		{"with a .. element", "out.dat", func(t *testing.T, dir string) string {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "sub") + sep + ".." + sep + "out.dat"
		}},
		{"through a linked directory", "out.dat", func(t *testing.T, dir string) string {
			t.Helper()
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(dir, alias); err != nil {
				t.Skipf("no link: %v", err)
			}
			return filepath.Join(alias, "out.dat")
		}},
		{"as a link to the file", "out.dat", func(t *testing.T, dir string) string {
			t.Helper()
			link := filepath.Join(dir, "lnk.dat")
			if err := os.Symlink("out.dat", link); err != nil {
				t.Skipf("no link: %v", err)
			}
			return link
		}},
		{"in another case", "Sub/out.dat", func(t *testing.T, dir string) string {
			t.Helper()
			if err := os.Mkdir(filepath.Join(dir, "Sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "SUB")); err != nil {
				t.Skip("the volume tells case apart")
			}
			return filepath.Join(dir, "SUB", "out.dat")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range []string{"valid.yammm", "data.json"} {
				content, err := os.ReadFile(filepath.Join("testdata", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			out := c.out(t, dir)
			code, _, stderr := executeCmdOutput(t, "snapshot", "save", filepath.Join(dir, "valid.yammm"),
				filepath.Join(dir, "data.json"), "-o", out)
			if code != cli.ExitOK {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if !strings.HasPrefix(stderr, c.label+": warning[W_SNAPSHOT_PATH_EXTENSION]") {
				t.Errorf("the warning does not name %s relative to the root: %s", c.label, stderr)
			}
			if !strings.Contains(stderr, "output path "+strconv.Quote(out)+" reaches no file") {
				t.Errorf("the message does not quote the path as typed, %q: %s", out, stderr)
			}

			want, err := location.SourceIDFromPath(out)
			if err != nil {
				t.Fatal(err)
			}
			_, _, stderr = executeCmdOutput(t, "snapshot", "save", "--json", filepath.Join(dir, "valid.yammm"),
				filepath.Join(dir, "data.json"), "-o", out)
			var doc struct {
				Issues []struct {
					Span       struct{ Source string }
					SourceName string `json:"sourceName"`
				}
			}
			if err := json.Unmarshal([]byte(stderr), &doc); err != nil || len(doc.Issues) != 1 {
				t.Fatalf("want one JSON issue (%v): %s", err, stderr)
			}
			if got := doc.Issues[0]; got.Span.Source != want.String() || got.SourceName != "" {
				t.Errorf("--json places the warning at span %q, sourceName %q; want span %q, as a read's issues are placed",
					got.Span.Source, got.SourceName, want)
			}
		})
	}
}

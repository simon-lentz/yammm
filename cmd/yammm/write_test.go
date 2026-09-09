package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Every way the CLI writes a file the operator named.
//
// One flag, one answer: the durability and the mode policy must not depend on
// which command produced the bytes or which target it was asked for. Before the
// durable-write primitive these paths disagreed — one `--output` flag left 0600
// through `gen`, 0600 through `--to json` and 0644 through `--to csv` and
// `--to cypher` — and nothing in the tree asserted any of it.
type writePath struct {
	name string
	// build prepares fixtures and returns the file the command writes plus the
	// argv that writes it. It is called once per case per assertion, so each
	// gets a directory of its own.
	build func(t *testing.T, dir string) (out string, args []string)
	// creates is false for a command that rewrites a file in place and cannot
	// bring one into existence.
	creates bool
}

// snapshotFixture writes a .ys file with the CLI itself and returns its path.
func snapshotFixture(t *testing.T, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if code, _, errOut := runCLI(t, "snapshot", "save", "-o", out,
		"testdata/valid.yammm", "testdata/data.json"); code != 0 {
		t.Fatalf("snapshot fixture failed with %d: %s", code, errOut)
	}
	return out
}

// mergeDataFixture writes instances distinct from testdata/data.json, so a
// --into merge adds rows rather than colliding on the primary key.
func mergeDataFixture(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(dir, "more.json")
	body := `{"Person": [{"id": "carol", "name": "Carol", "age": 41}]}`
	if err := os.WriteFile(out, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return out
}

// unformattedFixture writes a .yammm file the formatter will rewrite.
func unformattedFixture(t *testing.T, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if err := os.WriteFile(out, []byte("schema  \"A\"\ntype T {\n      id UUID primary\n}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return out
}

func writePaths() []writePath {
	return []writePath{
		{
			name:    "gen --output",
			creates: true,
			build: func(_ *testing.T, dir string) (string, []string) {
				out := filepath.Join(dir, "gen.go")
				return out, []string{"gen", "--to", "go", "--output", out, "testdata/valid.yammm"}
			},
		},
		{
			name:    "export --to json --output",
			creates: true,
			build: func(_ *testing.T, dir string) (string, []string) {
				out := filepath.Join(dir, "out.json")
				return out, []string{
					"export", "--to", "json", "--output", out,
					"testdata/valid.yammm", "testdata/data.json",
				}
			},
		},
		{
			name:    "export --to csv --output",
			creates: true,
			build: func(_ *testing.T, dir string) (string, []string) {
				out := filepath.Join(dir, "out.csv")
				return out, []string{
					"export", "--to", "csv", "--output", out,
					"testdata/valid.yammm", "testdata/data.json",
				}
			},
		},
		{
			name:    "export --to cypher --output",
			creates: true,
			build: func(_ *testing.T, dir string) (string, []string) {
				out := filepath.Join(dir, "out.cypher")
				return out, []string{
					"export", "--to", "cypher", "--output", out,
					"testdata/valid.yammm", "testdata/data.json",
				}
			},
		},
		{
			name:    "snapshot save -o",
			creates: true,
			build: func(_ *testing.T, dir string) (string, []string) {
				out := filepath.Join(dir, "saved.ys")
				return out, []string{
					"snapshot", "save", "-o", out,
					"testdata/valid.yammm", "testdata/data.json",
				}
			},
		},
		{
			name:    "snapshot save --into, same file",
			creates: false,
			build: func(t *testing.T, dir string) (string, []string) {
				t.Helper()
				out := snapshotFixture(t, dir, "merge.ys")
				return out, []string{
					"snapshot", "save", "--into", out, "-o", out,
					"testdata/valid.yammm", mergeDataFixture(t, dir),
				}
			},
		},
		{
			// B1 and B38: writeOutput guarded by string equality, so the same
			// file spelled differently took a different write path entirely.
			name:    "snapshot save --into, same file spelled differently",
			creates: false,
			build: func(t *testing.T, dir string) (string, []string) {
				t.Helper()
				out := snapshotFixture(t, dir, "merge.ys")
				spelled := filepath.Join(dir, "sub", "..", "merge.ys")
				return out, []string{
					"snapshot", "save", "--into", out, "-o", spelled,
					"testdata/valid.yammm", mergeDataFixture(t, dir),
				}
			},
		},
		{
			name:    "snapshot update-metadata",
			creates: false,
			build: func(t *testing.T, dir string) (string, []string) {
				t.Helper()
				out := snapshotFixture(t, dir, "meta.ys")
				return out, []string{"snapshot", "update-metadata", "-s", "phase=link", out}
			},
		},
		{
			name:    "fmt --write",
			creates: false,
			build: func(t *testing.T, dir string) (string, []string) {
				t.Helper()
				out := unformattedFixture(t, dir, "dirty.yammm")
				return out, []string{"fmt", "-w", out}
			},
		},
	}
}

// A file the CLI brings into existence is readable by its owner and nobody
// else. An export can carry every value in the graph; 0644 publishes it to
// every account on the host.
func TestWrite_NewFileIsOwnerOnly(t *testing.T) {
	for _, tc := range writePaths() {
		if !tc.creates {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, args := tc.build(t, dir)

			if code, _, errOut := runCLI(t, args...); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			assertMode(t, out, 0o600)
		})
	}
}

// A file the operator already made is theirs. Whatever mode it carries is the
// mode it keeps: the CLI is rewriting content, not taking ownership of policy.
func TestWrite_ExistingFileKeepsItsMode(t *testing.T) {
	for _, tc := range writePaths() {
		for _, mode := range []fs.FileMode{0o644, 0o600, 0o640} {
			t.Run(tc.name+"/"+mode.String(), func(t *testing.T) {
				dir := t.TempDir()
				out, args := tc.build(t, dir)

				// The target exists before the command runs, whether or not the
				// case's own fixture made it.
				if _, err := os.Stat(out); err != nil {
					if err := os.WriteFile(out, []byte("placeholder\n"), 0o600); err != nil {
						t.Fatalf("seed target: %v", err)
					}
				}
				if err := os.Chmod(out, mode); err != nil {
					t.Fatalf("chmod target: %v", err)
				}

				if code, _, errOut := runCLI(t, args...); code != 0 {
					t.Fatalf("exit %d: %s", code, errOut)
				}
				assertMode(t, out, mode)
			})
		}
	}
}

// The file is REPLACED, not truncated and refilled. That is what makes a crash
// mid-write leave the previous content rather than a half-written file, and the
// identity of the inode is the only way to tell the two apart from outside.
func TestWrite_ReplacesRatherThanTruncates(t *testing.T) {
	for _, tc := range writePaths() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, args := tc.build(t, dir)

			if _, err := os.Stat(out); err != nil {
				if err := os.WriteFile(out, []byte("placeholder\n"), 0o600); err != nil {
					t.Fatalf("seed target: %v", err)
				}
			}
			before, err := os.Stat(out)
			if err != nil {
				t.Fatalf("stat before: %v", err)
			}

			if code, _, errOut := runCLI(t, args...); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}

			after, err := os.Stat(out)
			if err != nil {
				t.Fatalf("stat after: %v", err)
			}
			if os.SameFile(before, after) {
				t.Error("the file was written in place; a crash mid-write would leave it truncated")
			}
		})
	}
}

// B9: the staging file drifted off the shared convention onto
// .yammm-save-*.ys, which ScanDir reports as a malformed snapshot while a
// sibling .tmp is correctly skipped. Whatever a crash leaves behind must be
// invisible to a directory scan.
func TestWrite_LeavesNoDebrisADirectoryScanReports(t *testing.T) {
	for _, tc := range writePaths() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, args := tc.build(t, dir)
			before := dirEntryNames(t, dir)

			if code, _, errOut := runCLI(t, args...); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}

			// Only the file the operator asked for may be new. A staging file
			// left behind is what B9 reported: ScanDir counted it as a
			// malformed snapshot because it ended in .ys rather than .tmp.
			for name := range dirEntryNames(t, dir) {
				if _, existed := before[name]; existed {
					continue
				}
				if filepath.Join(dir, name) == out {
					continue
				}
				t.Errorf("the write left %q beside %q", name, filepath.Base(out))
			}
		})
	}
}

func dirEntryNames(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		names[e.Name()] = struct{}{}
	}
	return names
}

// B6: a blocked second type left the first type's file complete in the
// operator's directory, with nothing saying the set was partial. Either every
// file arrives or none does.
func TestWrite_PartialDirectoryExportLeavesTheDirectoryAsItWas(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "csvs")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A directory where one type's file belongs: the create fails, and it
	// cannot be removed by the cleanup either.
	blocker := filepath.Join(outDir, "Pet.csv")
	if err := os.Mkdir(blocker, 0o750); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	schemaPath, dataPath := multiTypeFixture(t)

	code, _, _ := runCLI(t, "export", "--to", "csv", "--output-dir", outDir, schemaPath, dataPath)
	if code == 0 {
		t.Fatal("a blocked write reported success")
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "Pet.csv" {
			continue // the blocker the test put there
		}
		t.Errorf("a failed export left %q behind; the set is partial and nothing says so", e.Name())
	}
}

// The files of a directory export are the operator's too.
func TestWrite_DirectoryExportFilesAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "csvs")
	schemaPath, dataPath := multiTypeFixture(t)

	if code, _, errOut := runCLI(t, "export", "--to", "csv", "--output-dir", outDir,
		schemaPath, dataPath); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("the export wrote nothing")
	}
	for _, e := range entries {
		assertMode(t, filepath.Join(outDir, e.Name()), 0o600)
	}
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %v, want %v", filepath.Base(path), got, want)
	}
}

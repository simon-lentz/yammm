package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// linkedDirFixture makes dir/real/deep/x and a relative link dir/link to it,
// so dir/link/.. is dir on the text and dir/real/deep to the kernel.
func linkedDirFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "real", "deep", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "deep", "x"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// readOrAbsent returns path's content, or nil when it does not exist.
func readOrAbsent(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return b
}

// replaceArg returns args with every element equal to from replaced by to.
func replaceArg(args []string, from, to string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == from {
			a = to
		}
		out[i] = a
	}
	return out
}

// Every write path reads and writes the file the loader reads for its
// spelling. Through dir/link/.., a ".." after a symlinked directory, that is
// the file in dir, where the kernel would reach dir/real/deep: the file there
// holds bytes no command can read, and must not change. A command that rewrites
// a file in place reads it by the same rule, so it fails if it reads the other.
func TestWrite_DotDotAfterASymlinkLandsWhereTheLoaderReads(t *testing.T) {
	sep := string(filepath.Separator)
	for _, tc := range writePaths() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, args := tc.build(t, dir)
			linkedDirFixture(t, dir)
			decoy := filepath.Join(dir, "real", "deep", filepath.Base(out))
			seed := []byte("the kernel's file, which no command may read\n")
			if err := os.WriteFile(decoy, seed, 0o600); err != nil {
				t.Fatal(err)
			}
			before := readOrAbsent(t, out)
			spelled := dir + sep + "link" + sep + ".." + sep + filepath.Base(out)

			if code, _, errOut := runCLI(t, replaceArg(args, out, spelled)...); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if got := readOrAbsent(t, decoy); !bytes.Equal(got, seed) {
				t.Errorf("the file the kernel reaches through the link changed; the loader reads %s", out)
			}
			if after := readOrAbsent(t, out); after == nil || bytes.Equal(after, before) {
				t.Errorf("%s, the file the loader reads, was not written", out)
			}
			if want, err := location.ResolveHostPath(spelled); err != nil || !sameFile(t, want, out) {
				t.Errorf("the loader reads %s (%v) for %s, not the file written", want, err, spelled)
			}
		})
	}
}

// A link whose target climbs out of a linked directory, u.json ->
// lr/../u.real with lr a link, names the file the kernel reaches: the parent
// of the directory lr reached. Every write path writes that file, which the
// loader's resolver names too, and leaves the file beside the link alone.
func TestWrite_ALinkTargetClimbsFromTheDirectoryItsLinkReached(t *testing.T) {
	sep := string(filepath.Separator)
	for _, tc := range writePaths() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, args := tc.build(t, dir)
			linkedDirFixture(t, dir)
			name := filepath.Base(out) + ".real"
			kernelFile := filepath.Join(dir, "real", "deep", name)
			besideFile := filepath.Join(dir, name)
			seed := readOrAbsent(t, out)
			if seed == nil {
				seed = []byte("placeholder\n")
			}
			for _, p := range []string{kernelFile, besideFile} {
				if err := os.WriteFile(p, seed, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink("link"+sep+".."+sep+name, out); err != nil {
				t.Fatal(err)
			}
			if want, err := location.ResolveHostPath(out); err != nil || !sameFile(t, want, kernelFile) {
				t.Fatalf("the loader reads %s (%v), want %s", want, err, kernelFile)
			}

			if code, _, errOut := runCLI(t, args...); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if info, err := os.Lstat(out); err != nil || info.Mode()&fs.ModeSymlink == 0 {
				t.Error("the link was replaced by a regular file")
			}
			if got := readOrAbsent(t, kernelFile); bytes.Equal(got, seed) {
				t.Errorf("%s, the file the link names, was not written", kernelFile)
			}
			if got := readOrAbsent(t, besideFile); !bytes.Equal(got, seed) {
				t.Errorf("%s, which the link does not name, changed", besideFile)
			}
		})
	}
}

// sameFile reports whether a and b name one existing file.
func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// What export writes through a ".." after a symlinked directory, check reads
// back through the same spelling: for --output, and for --output-dir, where
// the directory export makes and the one it writes into are one directory.
func TestExport_WhatItWritesThroughALinkedDotDotCheckReadsBack(t *testing.T) {
	schemaPath, err := filepath.Abs(filepath.Join("testdata", "valid.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	dataPath, err := filepath.Abs(filepath.Join("testdata", "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)

	t.Run("--output", func(t *testing.T) {
		dir := t.TempDir()
		linkedDirFixture(t, dir)
		out := dir + sep + "link" + sep + ".." + sep + "o.json"
		if code, _, errOut := runCLI(t, "export", "--to", "json", "--output", out, schemaPath, dataPath); code != 0 {
			t.Fatalf("export: exit %d: %s", code, errOut)
		}
		if code, _, errOut := runCLI(t, "check", schemaPath, out); code != 0 {
			t.Errorf("check of what export wrote: exit %d: %s", code, errOut)
		}
	})

	// The relative link at the kernel's spelling of the file would, followed
	// from the text's directory, name ./g.json there.
	t.Run("--output past a relative link at the kernel's spelling", func(t *testing.T) {
		dir := t.TempDir()
		linkedDirFixture(t, dir)
		if err := os.Symlink("g.json", filepath.Join(dir, "real", "deep", "f.json")); err != nil {
			t.Fatal(err)
		}
		kernelTarget := filepath.Join(dir, "real", "deep", "g.json")
		if err := os.WriteFile(kernelTarget, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		out := dir + sep + "link" + sep + ".." + sep + "f.json"
		if code, _, errOut := runCLI(t, "export", "--to", "json", "--output", out, schemaPath, dataPath); code != 0 {
			t.Fatalf("export: exit %d: %s", code, errOut)
		}
		if got := readOrAbsent(t, filepath.Join(dir, "g.json")); got != nil {
			t.Errorf("export wrote %s, a file no rule names", filepath.Join(dir, "g.json"))
		}
		if got := readOrAbsent(t, kernelTarget); string(got) != "old" {
			t.Errorf("the kernel's file changed to %q; the loader reads %s", got, filepath.Join(dir, "f.json"))
		}
		if code, _, errOut := runCLI(t, "check", schemaPath, out); code != 0 {
			t.Errorf("check of what export wrote: exit %d: %s", code, errOut)
		}
	})

	t.Run("--output-dir", func(t *testing.T) {
		dir := t.TempDir()
		linkedDirFixture(t, dir)
		outDir := dir + sep + "link" + sep + ".." + sep + "out"
		if code, _, errOut := runCLI(t, "export", "--to", "csv", "--output-dir", outDir, schemaPath, dataPath); code != 0 {
			t.Fatalf("export: exit %d: %s", code, errOut)
		}
		if _, err := os.Lstat(filepath.Join(dir, "real", "deep", "out")); err == nil {
			t.Errorf("export made the kernel's directory %s", filepath.Join(dir, "real", "deep", "out"))
		}
		if code, _, errOut := runCLI(t, "check", "--type", "Person", schemaPath, outDir+sep+"Person.csv"); code != 0 {
			t.Errorf("check of what export wrote: exit %d: %s", code, errOut)
		}
	})

	// From a working directory entered through a link, a relative ".." climbs
	// from the working directory's logical spelling, as the loader climbs.
	t.Run("--output relative, from a linked working directory", func(t *testing.T) {
		dir := t.TempDir()
		linkedDirFixture(t, dir)
		t.Chdir(filepath.Join(dir, "link"))
		out := ".." + sep + "o.json"
		if code, _, errOut := runCLI(t, "export", "--to", "json", "--output", out, schemaPath, dataPath); code != 0 {
			t.Fatalf("export: exit %d: %s", code, errOut)
		}
		if _, err := os.Stat(filepath.Join(dir, "o.json")); err != nil {
			t.Errorf("export did not write %s, the file the loader reads: %v", filepath.Join(dir, "o.json"), err)
		}
		if code, _, errOut := runCLI(t, "check", schemaPath, out); code != 0 {
			t.Errorf("check of what export wrote: exit %d: %s", code, errOut)
		}
	})
}

// Every command that only reads a path the operator named reads the file the
// loader reads for its spelling. Through dir/link/.. that is the file in dir;
// the file the kernel would reach, in dir/real/deep, holds bytes no command can
// read, so a command that reads it fails. From a working directory entered
// through a link, a ".." climbs from the directory's logical spelling.
func TestRead_EveryPathReadsWhatTheLoaderReads(t *testing.T) {
	sep := string(filepath.Separator)
	junk := []byte("the kernel's file, which no command may read\n")
	cases := []struct {
		name string
		// run builds the loader's file in dir and the kernel's in kernel, and
		// returns the command's arguments, which reach the loader's file, or its
		// directory, through spelledDir, and what stdout must hold.
		run func(t *testing.T, dir, kernel, spelledDir string) (args []string, want string)
	}{
		{"fmt", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			unformattedFixture(t, dir, "f.yammm")
			writeBytes(t, filepath.Join(kernel, "f.yammm"), junk)
			return []string{"fmt", spelledDir + sep + "f.yammm"}, "type T"
		}},
		{"snapshot info", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			snapshotFixture(t, dir, "s.ys")
			writeBytes(t, filepath.Join(kernel, "s.ys"), junk)
			return []string{"snapshot", "info", spelledDir + sep + "s.ys"}, "Total instances: 2"
		}},
		{"snapshot info --header-only", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			snapshotFixture(t, dir, "s.ys")
			writeBytes(t, filepath.Join(kernel, "s.ys"), junk)
			return []string{"snapshot", "info", "--header-only", spelledDir + sep + "s.ys"}, "Integrity"
		}},
		{"snapshot info --dir", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			snapshotFixture(t, dir, "loader.ys")
			writeBytes(t, filepath.Join(kernel, "kernel.ys"), junk)
			return []string{"snapshot", "info", "--dir", spelledDir}, "loader.ys"
		}},
		{"snapshot verify", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			snapshotFixture(t, dir, "s.ys")
			writeBytes(t, filepath.Join(kernel, "s.ys"), junk)
			return []string{"snapshot", "verify", "testdata/valid.yammm", spelledDir + sep + "s.ys"}, ""
		}},
		{"export from a snapshot", func(t *testing.T, dir, kernel, spelledDir string) ([]string, string) {
			t.Helper()
			snapshotFixture(t, dir, "s.ys")
			writeBytes(t, filepath.Join(kernel, "s.ys"), junk)
			return []string{"export", "--to", "json", "testdata/valid.yammm", spelledDir + sep + "s.ys"}, `"alice"`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+", through a linked directory", func(t *testing.T) {
			dir := t.TempDir()
			linkedDirFixture(t, dir)
			args, want := tc.run(t, dir, filepath.Join(dir, "real", "deep"), dir+sep+"link"+sep+"..")
			code, out, errOut := runCLI(t, args...)
			if code != 0 || !strings.Contains(out, want) {
				t.Errorf("exit %d, stdout %q, want %q: %s", code, out, want, errOut)
			}
		})
		t.Run(tc.name+", climbing from a linked working directory", func(t *testing.T) {
			dir := t.TempDir()
			linkedDirFixture(t, dir)
			args, want := tc.run(t, dir, filepath.Join(dir, "real", "deep"), "..")
			for i, a := range args {
				if strings.HasPrefix(a, "testdata"+sep) || strings.HasPrefix(a, "testdata/") {
					abs, err := filepath.Abs(a)
					if err != nil {
						t.Fatal(err)
					}
					args[i] = abs
				}
			}
			t.Chdir(filepath.Join(dir, "link"))
			code, out, errOut := runCLI(t, args...)
			if code != 0 || !strings.Contains(out, want) {
				t.Errorf("exit %d, stdout %q, want %q: %s", code, out, want, errOut)
			}
		})
	}
}

// writeBytes writes b to path.
func writeBytes(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

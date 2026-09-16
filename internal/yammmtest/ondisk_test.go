package yammmtest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func mustDiskSpelling(t *testing.T, p string) string {
	t.Helper()
	spelled, err := DiskSpelling(p)
	if err != nil {
		t.Fatalf("DiskSpelling(%q): %v", p, err)
	}
	return spelled
}

func mustWriteFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// TestDiskSpelling_EveryComponentIsAListedNameThatIsNotALink holds the result
// to its own definition, checked without a second resolver: every prefix of it
// is an entry its parent lists under exactly that name, and none is a link.
func TestDiskSpelling_EveryComponentIsAListedNameThatIsNotALink(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "Sub")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	got := mustDiskSpelling(t, dir)
	if !filepath.IsAbs(got) {
		t.Fatalf("DiskSpelling(%q) = %q, which is not absolute", dir, got)
	}
	for p := got; filepath.Dir(p) != p; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("lstat %q: %v", p, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			t.Errorf("%q, a prefix of %q, is a symbolic link", p, got)
		}
		entries, err := os.ReadDir(filepath.Dir(p))
		if err != nil {
			t.Fatalf("list %q: %v", filepath.Dir(p), err)
		}
		if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == filepath.Base(p) }) {
			t.Errorf("%q lists no entry named %q, a component of %q", filepath.Dir(p), filepath.Base(p), got)
		}
	}
}

// TestDiskSpelling_WritesANameAsItsDirectoryListsIt holds each component to the
// listing's spelling where the filesystem finds the entry by another one.
func TestDiskSpelling_WritesANameAsItsDirectoryListsIt(t *testing.T) {
	t.Parallel()

	t.Run("a name typed in another case", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		if !CaseFoldingFilesystem(t, base) {
			t.Skip("the filesystem is case-sensitive, so two spellings name two files")
		}
		mustWriteFile(t, filepath.Join(base, "Proj", "File.yammm"))

		typed := filepath.Join(base, "proj", "file.yammm")
		want := filepath.Join(mustDiskSpelling(t, base), "Proj", "File.yammm")
		if got := mustDiskSpelling(t, typed); got != want {
			t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
		}
	})

	t.Run("a composed name for a decomposed entry", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		mustWriteFile(t, filepath.Join(base, "cafe\u0301.yammm"))
		typed := filepath.Join(base, "caf\u00e9.yammm")
		if _, err := os.Stat(typed); err != nil {
			t.Skip("the filesystem tells the two normalization forms apart")
		}

		want := filepath.Join(mustDiskSpelling(t, base), "cafe\u0301.yammm")
		if got := mustDiskSpelling(t, typed); got != want {
			t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
		}
	})
}

// TestDiskSpelling_PrefersTheExactNameWhereTwoNamesDifferOnlyInCase holds a
// case-sensitive directory's exact entry above a sibling equal to it under case
// folding.
func TestDiskSpelling_PrefersTheExactNameWhereTwoNamesDifferOnlyInCase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if CaseFoldingFilesystem(t, base) {
		t.Skip("the filesystem folds case, so it cannot list File and file side by side")
	}
	mustWriteFile(t, filepath.Join(base, "File"))
	mustWriteFile(t, filepath.Join(base, "file"))

	typed := filepath.Join(base, "file")
	want := filepath.Join(mustDiskSpelling(t, base), "file")
	if got := mustDiskSpelling(t, typed); got != want {
		t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
	}
}

// TestDiskSpelling_PrefersANameMatchOverAnotherLinkToTheSameFile holds a name
// equal to the typed one under case folding or NFC above a hard link to the
// same file. The link sorts before the file in the listing, so a walk that
// reached the same-file match first would return the link's name.
func TestDiskSpelling_PrefersANameMatchOverAnotherLinkToTheSameFile(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name         string
		listed       string
		typed        string
		foldedReason string
	}{
		{
			name:   "a name typed in another case",
			listed: "File.yammm", typed: "file.yammm",
			foldedReason: "the filesystem is case-sensitive, so two spellings name two files",
		},
		{
			name:   "a decomposed name for a composed entry",
			listed: "café.yammm", typed: "café.yammm",
			foldedReason: "the filesystem tells the two normalization forms apart",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			mustWriteFile(t, filepath.Join(base, row.listed))
			typed := filepath.Join(base, row.typed)
			if _, err := os.Stat(typed); err != nil {
				t.Skip(row.foldedReason)
			}
			if err := os.Link(filepath.Join(base, row.listed), filepath.Join(base, "A.yammm")); err != nil {
				t.Skipf("hard links unavailable: %v", err)
			}

			want := filepath.Join(mustDiskSpelling(t, base), row.listed)
			if got := mustDiskSpelling(t, typed); got != want {
				t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
			}
		})
	}
}

// TestDiskSpelling_FindsAnEntryListedUnderANameNoFoldingMatches holds the walk
// to the entry that is the same file where the filesystem finds a name that
// neither strings.EqualFold nor NFC equates with its listed name.
func TestDiskSpelling_FindsAnEntryListedUnderANameNoFoldingMatches(t *testing.T) {
	t.Parallel()

	t.Run("a name the filesystem folds with full case folding", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		mustWriteFile(t, filepath.Join(base, "straße.yammm"))
		// Full case folding maps ß to ss; the simple folding strings.EqualFold
		// applies does not.
		typed := filepath.Join(base, "STRASSE.yammm")
		if _, err := os.Stat(typed); err != nil {
			t.Skip("the filesystem does not find straße.yammm as STRASSE.yammm")
		}

		want := filepath.Join(mustDiskSpelling(t, base), "straße.yammm")
		if got := mustDiskSpelling(t, typed); got != want {
			t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
		}
	})

	t.Run("a Windows 8.3 short name", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS != "windows" {
			t.Skip("only Windows lists an entry under an 8.3 short name")
		}
		base := t.TempDir()
		long := filepath.Join(base, "Long Directory Name")
		mustWriteFile(t, filepath.Join(long, "f.yammm"))
		short := shortPathName(t, long)
		if filepath.Base(short) == filepath.Base(long) {
			t.Skipf("the volume holding %q does not generate 8.3 short names", base)
		}

		typed := filepath.Join(short, "f.yammm")
		want := filepath.Join(mustDiskSpelling(t, base), "Long Directory Name", "f.yammm")
		if got := mustDiskSpelling(t, typed); got != want {
			t.Errorf("DiskSpelling(%q) = %q, want %q", typed, got, want)
		}
	})
}

// TestDiskSpelling_ResolvesSymbolicLinks holds the walk to a link's target,
// relative to the link's directory or absolute, with a ".." in the target
// taken as the host takes it: on Unix from the directory the target reaches,
// on Windows from the target's text.
func TestDiskSpelling_ResolvesSymbolicLinks(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	spelledBase := mustDiskSpelling(t, base)
	mustWriteFile(t, filepath.Join(base, "real", "f.yammm"))
	if err := os.MkdirAll(filepath.Join(base, "a", "b"), 0o750); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	// b's parent on disk is a; the textual parent of "b-link/.." is base.
	upWant := "a"
	if runtime.GOOS == "windows" {
		upWant = "."
	}

	rows := []struct {
		name         string
		link, target string
		in, want     string
	}{
		{
			name: "a relative target",
			link: "rel", target: "real",
			in: filepath.Join("rel", "f.yammm"), want: filepath.Join("real", "f.yammm"),
		},
		{
			name: "an absolute target",
			link: "abs", target: filepath.Join(base, "real"),
			in: filepath.Join("abs", "f.yammm"), want: filepath.Join("real", "f.yammm"),
		},
		{
			name: "a target whose .. follows a link",
			link: "up", target: "b-link" + sep + "..",
			in: "up", want: upWant,
		},
	}

	symlinkOrSkip(t, filepath.Join("a", "b"), filepath.Join(base, "b-link"))
	for _, row := range rows {
		symlinkOrSkip(t, row.target, filepath.Join(base, row.link))
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			in := filepath.Join(base, row.in)
			if got, want := mustDiskSpelling(t, in), filepath.Join(spelledBase, row.want); got != want {
				t.Errorf("DiskSpelling(%q) = %q, want %q", in, got, want)
			}
		})
	}

	t.Run("a cycle of links is an error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		symlinkOrSkip(t, "loop-b", filepath.Join(dir, "loop-a"))
		symlinkOrSkip(t, "loop-a", filepath.Join(dir, "loop-b"))
		if _, err := DiskSpelling(filepath.Join(dir, "loop-a")); !errors.Is(err, errTooManyLinks) {
			t.Errorf("DiskSpelling of a link cycle: err = %v, want errTooManyLinks", err)
		}
	})
}

func TestDiskSpelling_RefusesAPathThatDoesNotExist(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing", "f.yammm")
	if _, err := DiskSpelling(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("DiskSpelling(%q): err = %v, want fs.ErrNotExist", missing, err)
	}
}

// TestCaseFoldingFilesystem_AgreesWithTheFilesystemAndRemovesItsProbe checks
// the answer against a second probe of its own and the directory against a
// leftover file once the test that asked has ended.
func TestCaseFoldingFilesystem_AgreesWithTheFilesystemAndRemovesItsProbe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var folds bool
	// Registered after t.TempDir, so it runs once the subtest's cleanup has
	// removed the probe and before the directory itself is removed.
	t.Cleanup(func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("the probe left %d entries behind in %q", len(entries), dir)
		}

		mustWriteFile(t, filepath.Join(dir, "FoldCheck"))
		_, statErr := os.Stat(filepath.Join(dir, "foldcheck"))
		if found := statErr == nil; found != folds {
			t.Errorf("CaseFoldingFilesystem = %v, but a file created as FoldCheck is found as foldcheck: %v", folds, found)
		}
	})
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		folds = CaseFoldingFilesystem(t, dir)
	})
}

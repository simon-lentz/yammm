package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// No set removes a directory. A set that fails once its directory is made
// leaves the directory, and every level above it the set made, holding no
// staging file; a caller that must create nothing judges its data first.
func TestWriteFileSet_KeepsTheDirectoriesItMadeWhenItFails(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 300)
	for _, c := range []struct {
		name  string
		dir   []string // below the root
		files []NamedFile
		kept  []string // below the root
	}{
		{
			name:  "a file that cannot be staged",
			dir:   []string{"new", "deeper"},
			files: []NamedFile{{Name: "a.csv", Data: []byte("a")}, {Name: long + ".csv", Data: []byte("b")}},
			kept:  []string{"new", "deeper"},
		},
		{
			name:  "a level that cannot be made",
			dir:   []string{"new", long},
			files: []NamedFile{{Name: "a.csv", Data: []byte("a")}},
			kept:  []string{"new"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(append([]string{root}, c.dir...)...)
			if err := WriteFileSet(dir, c.files); err == nil {
				t.Fatal("the set reported success")
			}
			kept := filepath.Join(append([]string{root}, c.kept...)...)
			info, err := os.Stat(kept)
			if err != nil || !info.IsDir() {
				t.Fatalf("the directory the set made is gone (stat: %v)", err)
			}
			assertNoDebris(t, kept)
		})
	}
}

// A set whose names cannot share one directory is refused before anything is
// created: two names that differ only in case, and a name that is not a single
// element inside the directory.
func TestWriteFileSet_RefusesItsNamesBeforeCreatingAnything(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	for _, row := range []struct {
		names []string
		why   string
	}{
		{[]string{"Item.csv", "ITEM.csv"}, "differ only in case"},
		{[]string{"a.csv", "a.csv"}, "named twice"},
		{[]string{"a.csv", ""}, "not a file name"},
		{[]string{"."}, "not a file name"},
		{[]string{".."}, "not a file name"},
		{[]string{"a" + sep + "b.csv"}, "not a file name"},
		{[]string{".." + sep + "b.csv"}, "not a file name"},
		{[]string{"b.csv" + sep}, "not a file name"},
	} {
		names := row.names
		t.Run(fmt.Sprintf("%q", names), func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "new")
			files := make([]NamedFile, 0, len(names))
			for _, n := range names {
				files = append(files, NamedFile{Name: n, Data: []byte(n)})
			}
			err := WriteFileSet(dir, files)
			if err == nil {
				t.Fatal("the set reported success")
			}
			if _, serr := os.Stat(dir); !errors.Is(serr, fs.ErrNotExist) {
				t.Errorf("a refused set created %s (stat: %v)", dir, serr)
			}
			want := names[len(names)-1]
			if want == "" {
				want = `""`
			}
			if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), row.why) {
				t.Errorf("error %q does not name %q and say it is %s", err, want, row.why)
			}
		})
	}
}

// Two names whose links reach one file would leave the set one file short, so
// the set is refused and nothing is renamed, however the links spell the file:
// alike, one through "./", in two cases, or one link to the other name's own
// file; and two hard links to one file, which no text compares.
func TestWriteFileSet_RefusesTwoNamesThatReachOneFile(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	for _, row := range []struct {
		name  string
		links map[string]string
		file  string // a regular file the set's names reach, "" when none exists
	}{
		{"both links spell x.csv", map[string]string{"a.csv": "x.csv", "b.csv": "x.csv"}, ""},
		{"one link spells ./x.csv", map[string]string{"a.csv": "x.csv", "b.csv": "." + sep + "x.csv"}, ""},
		{"one link reaches the other name", map[string]string{"b.csv": "." + sep + "a.csv"}, "a.csv"},
		{"both links reach an existing file", map[string]string{"a.csv": "x.csv", "b.csv": "." + sep + "x.csv"}, "x.csv"},
		{"the links spell one name in two cases", map[string]string{"a.csv": "X.csv", "b.csv": "x.csv"}, ""},
		{"two hard links", nil, "a.csv"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if row.file != "" {
				if err := os.WriteFile(filepath.Join(dir, row.file), []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var kept []string
			if row.links == nil {
				if err := os.Link(filepath.Join(dir, "a.csv"), filepath.Join(dir, "b.csv")); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
				kept = append(kept, "b.csv")
			}
			for name, target := range row.links {
				if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				kept = append(kept, name)
			}
			if row.file != "" {
				kept = append(kept, row.file)
			}
			err := WriteFileSet(dir, []NamedFile{{Name: "a.csv", Data: []byte("a")}, {Name: "b.csv", Data: []byte("b")}})
			if err == nil || !strings.Contains(err.Error(), "a.csv") || !strings.Contains(err.Error(), "b.csv") {
				t.Fatalf("error %v, want a refusal naming a.csv and b.csv", err)
			}
			if row.file != "" {
				if got, _ := os.ReadFile(filepath.Join(dir, row.file)); string(got) != "old" {
					t.Errorf("%s changed to %q under a refused set", row.file, got)
				}
			} else if _, err := os.Lstat(filepath.Join(dir, "x.csv")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("x.csv was written by a refused set (lstat: %v)", err)
			}
			assertNoDebris(t, dir, kept...)
		})
	}
}

// A directory a set makes gives accounts outside its owner and group no access.
func TestWriteFileSet_MakesItsDirectoryClosedToOthers(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip(noDirPermissionsOnWindows)
	}
	root := t.TempDir()
	if err := WriteFileSet(filepath.Join(root, "new", "out"), []NamedFile{{Name: "a.csv", Data: []byte("a")}}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(root, "new"), filepath.Join(root, "new", "out")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o007 != 0 || perm&0o700 != 0o700 {
			t.Errorf("%s has mode %v, want the owner's alone and nothing for others", d, perm)
		}
	}
}

// Sets written at once into one new directory each land, whichever of them
// fail: no set removes the directory another set is writing into.
func TestWriteFileSet_ConcurrentSetsIntoOneNewDirectoryAllLand(t *testing.T) {
	t.Parallel()
	const rounds, writers = 40, 8
	long := strings.Repeat("x", 300)
	for round := range rounds {
		dir := filepath.Join(t.TempDir(), "new", "out")
		errs := make([]error, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Go(func() {
				name := fmt.Sprintf("w%d.csv", i)
				if i%2 == 1 {
					// Fails once the directory is made.
					name = long + name
				}
				errs[i] = WriteFileSet(dir, []NamedFile{{Name: name, Data: []byte("x")}})
			})
		}
		wg.Wait()
		for i := 0; i < writers; i += 2 {
			if errs[i] != nil {
				t.Fatalf("round %d: writer %d: %v", round, i, errs[i])
			}
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("w%d.csv", i))); err != nil {
				t.Fatalf("round %d: writer %d's file is missing: %v", round, i, err)
			}
		}
	}
}

// stageInto writes one file, Order.csv, as a set into dir.
func stageInto(dir string) error {
	return WriteFileSet(dir, []NamedFile{{Name: "Order.csv", Data: []byte(targetPayload)}})
}

// loaderDirChanges makes, on a fresh fixture from place, the directory the
// loader's resolver names for prefix+rel, as os.MkdirAll makes it, then writes
// Order.csv there when file is set. It returns what changed and the first
// error.
func loaderDirChanges(t *testing.T, place hostPathPlace, rel string, file bool) ([]string, error) {
	t.Helper()
	root, prefix := place(t)
	before := hostPathTree(t, root)
	want, err := location.ResolveHostPath(prefix + rel)
	if err == nil {
		err = os.MkdirAll(want, 0o750)
	}
	if err == nil && file {
		err = os.WriteFile(want+string(filepath.Separator)+"Order.csv", []byte(targetPayload), 0o600)
	}
	return hostPathChanges(before, hostPathTree(t, root)), err
}

// checkSetLandsWhereTheLoaderReads writes a one-file set into prefix+rel on one
// fixture and stages and discards one on another, and holds both to the
// loader's resolver on twin fixtures: the written set makes exactly what
// os.MkdirAll and a write at the resolved directory make, and the discarded
// set exactly what os.MkdirAll alone makes, since no set removes a directory.
func checkSetLandsWhereTheLoaderReads(t *testing.T, f *hostPathFailures, place hostPathPlace, rel, shown string) {
	t.Helper()
	f.cases++

	wantChanged, twinErr := loaderDirChanges(t, place, rel, true)
	root, prefix := place(t)
	before := hostPathTree(t, root)
	err := stageInto(prefix + rel)
	changed := hostPathChanges(before, hostPathTree(t, root))
	switch {
	case (err == nil) != (twinErr == nil):
		f.add(shown, "set error %v, the loader's directory %v", err, twinErr)
	case !slices.Equal(changed, wantChanged):
		f.add(shown, "changed %q, where the loader's directory takes %q", changed, wantChanged)
	}

	wantDirs, dirErr := loaderDirChanges(t, place, rel, false)
	root, prefix = place(t)
	before = hostPathTree(t, root)
	set, err := stageFileSet(prefix+rel, []NamedFile{{Name: "Order.csv", Data: []byte(targetPayload)}})
	if err == nil {
		set.discard()
	}
	changed = hostPathChanges(before, hostPathTree(t, root))
	switch {
	case (err == nil) != (dirErr == nil):
		f.add(shown, "staging error %v, the loader's directory %v", err, dirErr)
	case !slices.Equal(changed, wantDirs):
		f.add(shown, "a discarded set changed %q, where the loader's directory takes %q", changed, wantDirs)
	}
}

// A set writes into the directory the loader reads for the same spelling,
// making exactly the levels the loader's directory lacks, and a discarded set
// leaves those levels and nothing else. Every spelling of one or two
// components is tried, 56 in all.
func TestWriteFileSet_LandsWhereTheLoaderReads(t *testing.T) {
	t.Parallel()
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	sep := string(filepath.Separator)
	for _, first := range hostPathComponents {
		t.Run(first, func(t *testing.T) {
			t.Parallel()
			var f hostPathFailures
			for _, rel := range hostPathSpellings(sep+first, 2) {
				checkSetLandsWhereTheLoaderReads(t, &f, absoluteStart, rel, strings.TrimPrefix(rel, sep))
			}
			f.report(t)
		})
	}
}

// Relative spellings of a set's directory, from a working directory entered
// through a symbolic link, land where the loader reads them, as a file's do.
// Spellings of one or two components are tried, 56 in all.
func TestWriteFileSet_RelativeSpellingsLandWhereTheLoaderReads(t *testing.T) {
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	var f hostPathFailures
	for _, rel := range hostPathSpellings("", 3)[1:] {
		t.Run("set", func(t *testing.T) {
			checkSetLandsWhereTheLoaderReads(t, &f, logicalStart, rel, "."+rel)
		})
	}
	f.report(t)
}

func TestWriteFileSet_StagesBesideTheFileALinkReaches(t *testing.T) {
	t.Parallel()
	checkStagesBesideTheFileALinkReaches(t, func(path string) error {
		return WriteFileSet(filepath.Dir(path), []NamedFile{{Name: filepath.Base(path), Data: []byte(targetPayload)}})
	})
}

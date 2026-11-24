package main

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// copied returns a worker's tree of a checkout that holds a tracked script, a
// tracked link (not on Windows), a tracked file in a nested directory, a
// tracked file whose name git reads as a pattern, a tracked file its .gitignore matches, untracked files and an
// ignored one.
func copied(t *testing.T) *tree {
	t.Helper()
	f := newFixture(t)
	gittree.WriteFile(t, f.checkout, "scripts/run.sh", []byte("#!/bin/sh\n"), 0o755)
	if runtime.GOOS != "windows" {
		if err := os.Symlink("m.go", filepath.Join(f.checkout, "m", "link.go")); err != nil {
			t.Fatal(err)
		}
	}
	f.write("m/sub/s.txt", "nested\n")
	// A path git would read as a pattern beside a name the pattern matches.
	f.write("a[1].txt", "tracked\n")
	f.index()
	f.write("a1.txt", "untracked, and matched by a[1].txt as a pattern\n")
	f.write("forced.txt", "tracked though ignored\n")
	f.write(".gitignore", "ignored/\nforced.txt\n")
	gittree.Index(t, f.checkout, "-f", "forced.txt", ".gitignore")
	f.write("untracked.txt", "untracked\n")
	f.write("ignored/big.bin", "ignored\n")
	env, err := cleanEnv(t.Context(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	r := &run{checkout: f.checkout, out: t.TempDir(), env: env}
	if err := r.listFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	tree, err := newTree(t.Context(), r, filepath.Join(r.out, "w1"))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func gitLines(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = gittree.WithoutRepositoryVars(t, os.Environ())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.Fields(string(out))
}

func TestNewTree_CopiesWhatGitListsWithTheTrackedFilesIndexed(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	tracked := gitLines(t, tree.dir, "ls-files")
	want := gitLines(t, tree.run.checkout, "ls-files")
	if !slices.Equal(tracked, want) {
		t.Errorf("the copy tracks %q, want the checkout's %q", tracked, want)
	}
	if others := gitLines(t, tree.dir, "ls-files", "--others", "--exclude-standard"); !slices.Equal(others, []string{"a1.txt", "untracked.txt"}) {
		t.Errorf("the copy's untracked files are %q, want a1.txt and untracked.txt", others)
	}
	if !slices.Contains(tracked, "forced.txt") {
		t.Errorf("the copy does not track forced.txt, which its .gitignore matches: %q", tracked)
	}
	if changed := gitLines(t, tree.dir, "diff", "--name-only"); len(changed) != 0 {
		t.Errorf("the copy's index differs from its files: %q", changed)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "ignored")); err == nil {
		t.Error("the copy holds a file git ignores")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if info, err := os.Stat(filepath.Join(tree.dir, "scripts", "run.sh")); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the copied script: %v, %v; want mode 0755", info, err)
	}
	if target, err := os.Readlink(filepath.Join(tree.dir, "m", "link.go")); err != nil || target != "m.go" {
		t.Errorf("the copied link points to %q, %v; want m.go", target, err)
	}
}

func TestTree_RestorePutsBackWhatWasCopied(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	if differed, err := tree.restore(t.Context()); err != nil || len(differed) != 0 {
		t.Fatalf("a tree nothing changed differs: %q, %v", differed, err)
	}
	before, beforeDirs, err := tree.read()
	if err != nil {
		t.Fatal(err)
	}
	in := func(rel string) string { return filepath.Join(tree.dir, filepath.FromSlash(rel)) }
	gittree.WriteFile(t, tree.dir, "m/m.go", []byte("package m\n"), 0o600)        // a tracked file's bytes
	gittree.WriteFile(t, tree.dir, "untracked.txt", []byte("changed\n"), 0o600)   // an untracked file's
	gittree.WriteFile(t, tree.dir, "new/deep/left.txt", []byte("left\n"), 0o600)  // a new directory
	gittree.WriteFile(t, tree.dir, "m/left.go", []byte("package m\n"), 0o600)     // a new file beside copied ones
	gittree.WriteFile(t, tree.dir, "ignored/cache.bin", []byte("cache\n"), 0o600) // a new file git ignores
	want := []string{".git/index", "go.mod", "ignored", "m/left.go", "m/m.go", "m/m_test.go", "new", "untracked.txt"}
	if err := os.Remove(in("m/m_test.go")); err != nil {
		t.Fatal(err)
	}
	// A mode with no write bit changes the mode on every host: Windows keeps
	// the write bit alone, as the read-only attribute.
	if err := os.Chmod(in("go.mod"), 0o400); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Remove(in("m/link.go")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("m_test.go", in("m/link.go")); err != nil {
			t.Fatal(err)
		}
		// A file a test replaced with a directory that holds a file.
		if err := os.Remove(in("scripts/run.sh")); err != nil {
			t.Fatal(err)
		}
		gittree.WriteFile(t, tree.dir, "scripts/run.sh/inner/x", []byte("x"), 0o600)
		// A directory mode no file carries; m is left unwritable last.
		if err := os.Chmod(in("m"), 0o555); err != nil { //nolint:gosec // a directory the test must not be able to write
			t.Fatal(err)
		}
		want = slices.Sorted(slices.Values(append(want, "m/link.go", "scripts/run.sh", "m")))
	}
	gitLines(t, tree.dir, "add", "-f", "ignored/cache.bin")

	differed, err := tree.restore(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(differed)
	if !slices.Equal(differed, want) {
		t.Errorf("restore reports %q, want %q", differed, want)
	}
	after, afterDirs, err := tree.read()
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(afterDirs, beforeDirs) {
		t.Errorf("the directories are %v after the restore, want %v", afterDirs, beforeDirs)
	}
	if len(after) != len(before) {
		t.Errorf("the tree holds %d entries after the restore, want %d", len(after), len(before))
	}
	for name, e := range before {
		if after[name] != e {
			t.Errorf("%s is not what was copied", name)
		}
	}
	if again, err := tree.restore(t.Context()); err != nil || len(again) != 0 {
		t.Errorf("the restored tree differs: %q, %v", again, err)
	}
}

// A file that changed in the checkout cannot be put back as it was copied.
func TestTree_RestoreFailsWhenTheCheckoutChanged(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	gittree.WriteFile(t, tree.run.checkout, "m/m.go", []byte("package m\n\n// changed in the checkout\n"), 0o600)
	gittree.WriteFile(t, tree.dir, "m/m.go", []byte("package m\n"), 0o600)
	if differed, err := tree.restore(t.Context()); err == nil || !strings.Contains(err.Error(), "the checkout changed during the run") {
		t.Errorf("restore returned %q, %v; want an error that says the checkout changed", differed, err)
	}
}

// A path written while the copy is restored, as by a process a test left
// running outside its group, leaves the copy unlike what was copied, and
// restore fails.
func TestTree_RestoreFailsWhileAWriterAddsFiles(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	gittree.WriteFile(t, tree.dir, "m/m.go", []byte("package m\n"), 0o600)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_ = os.WriteFile(filepath.Join(tree.dir, fmt.Sprintf("w%d.txt", i)), nil, 0o600)
			}
		}
	}()
	differed, err := tree.restore(t.Context())
	close(stop)
	<-done
	if err == nil || !strings.Contains(err.Error(), "cannot be restored") {
		t.Errorf("restore returned %q, %v; want an error that says the copy cannot be restored", differed, err)
	}
}

// A directory a test left without read or search permission is given its mode
// back before the copy is walked, so what it holds is restored too.
func TestTree_RestoreGivesBackADirectoryItCannotWalk(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no read or search bit on a directory")
	}
	tree := copied(t)
	in := func(rel string) string { return filepath.Join(tree.dir, filepath.FromSlash(rel)) }
	gittree.WriteFile(t, tree.dir, "m/m.go", []byte("package m\n"), 0o600)
	gittree.WriteFile(t, tree.dir, "scripts/left.sh", []byte("left\n"), 0o600)
	for name, mode := range map[string]os.FileMode{"m": 0o300, "scripts": 0o600} {
		if err := os.Chmod(in(name), mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(in(name), 0o750) }) //nolint:gosec // restoring the test's own directories
	}
	differed, err := tree.restore(t.Context())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if want := []string{"m", "m/m.go", "scripts", "scripts/left.sh"}; !slices.Equal(differed, want) {
		t.Errorf("restore reports %q, want %q", differed, want)
	}
	if again, err := tree.restore(t.Context()); err != nil || len(again) != 0 {
		t.Errorf("the restored tree differs: %q, %v", again, err)
	}
}

// A copied directory a test made a link or a file goes whole before the copy,
// and no mode is changed through the link, outside the copy.
func TestTree_RestoreChangesNothingThroughADirectoryMadeALink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("needs a symbolic link and a directory mode")
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, replace := range map[string]func(at string) error{
		"link": func(at string) error { return os.Symlink(outside, at) },
		"file": func(at string) error { return os.WriteFile(at, []byte("a file\n"), 0o600) },
	} {
		tree := copied(t)
		at := filepath.Join(tree.dir, "m")
		if err := os.RemoveAll(at); err != nil {
			t.Fatal(err)
		}
		if err := replace(at); err != nil {
			t.Fatal(err)
		}
		if _, err := tree.restore(t.Context()); err != nil {
			t.Errorf("%s: restore: %v", name, err)
		}
		if again, err := tree.restore(t.Context()); err != nil || len(again) != 0 {
			t.Errorf("%s: the restored tree differs: %q, %v", name, again, err)
		}
	}
	if info, err := os.Stat(filepath.Join(outside, "sub")); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the directory the link led to: %v, %v; want mode 0700", info, err)
	}
}

// A copied file a test left unreadable is given its mode back and compared,
// and a new one it left unreadable is removed unread.
func TestTree_RestoreGivesBackAFileItCannotRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no read bit on a file")
	}
	tree := copied(t)
	at := filepath.Join(tree.dir, "m", "m.go")
	if err := os.Chmod(at, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(at, 0o600) })
	if err := os.WriteFile(filepath.Join(tree.dir, "m", "new.txt"), nil, 0); err != nil {
		t.Fatal(err)
	}
	if differed, err := tree.restore(t.Context()); err != nil || !slices.Equal(differed, []string{"m/m.go", "m/new.txt"}) {
		t.Errorf("restore reports %q, %v; want m/m.go and m/new.txt", differed, err)
	}
	if again, err := tree.restore(t.Context()); err != nil || len(again) != 0 {
		t.Errorf("the restored tree differs: %q, %v", again, err)
	}
}

func TestTree_MutateReplacesEveryMatchAndPutsTheBytesBack(t *testing.T) {
	t.Parallel()
	tree := copied(t)
	path := filepath.Join(tree.dir, "m", "m.go")
	for _, row := range []struct {
		name, search, replace string
		matches               int
		want                  string // the file's content with the mutant; empty when it changes nothing
	}{
		{name: "one match", search: "a + b", replace: "a - b", matches: 1, want: strings.Replace(addSource, "a + b", "a - b", 1)},
		{name: "every match", search: "a", replace: "aa", matches: 4, want: strings.ReplaceAll(addSource, "a", "aa")},
		{name: "a search that ends in a newline", search: "}\n", replace: "} // end\n", matches: 1, want: strings.Replace(addSource, "}\n", "} // end\n", 1)},
		{name: "bytes that are not text", search: "package m", replace: "package m // \xff\x00", matches: 1, want: strings.Replace(addSource, "package m", "package m // \xff\x00", 1)},
		{name: "no match", search: "a * b", replace: "x"},
		{name: "a replacement that changes nothing", search: "a + b", replace: "a + b", matches: 1},
	} {
		undo, matches, err := tree.mutate(mutant{file: "m/m.go", search: []byte(row.search), replace: []byte(row.replace)})
		if matches != row.matches {
			t.Errorf("%s: %d matches, want %d", row.name, matches, row.matches)
		}
		if row.want == "" {
			if err != errNoMatch { //nolint:errorlint // mutate returns it unwrapped
				t.Errorf("%s: mutate returned %v, want errNoMatch", row.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if got, _ := os.ReadFile(path); string(got) != row.want {
			t.Errorf("%s: the file holds %q, want %q", row.name, got, row.want)
		}
		if err := undo(); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != addSource {
			t.Errorf("%s: the file holds %q after the undo", row.name, got)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	// A mutant of a script a test runs keeps the script's mode.
	undo, _, err := tree.mutate(mutant{file: "scripts/run.sh", search: []byte("#!/bin/sh"), replace: []byte("#!/bin/sh -e")})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(tree.dir, "scripts", "run.sh")); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the mutated script: %v, %v; want mode 0755", info, err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
}

func TestGoIgnores(t *testing.T) {
	t.Parallel()
	for file, want := range map[string]bool{
		"m/m.go": false, "m/testdata/f.go": true, "m/testdatax/f.go": false, "testdata/f.go": true, "m/_skip.go": true, "_hid/h.go": true,
		".dot/d.go": true, "m/.d.go": true, "m/testdatum/f.go": false, "m/my_testdata/f.go": false, "m/a_b.go": false,
	} {
		if got := goIgnores(filepath.FromSlash(file)); got != want {
			t.Errorf("goIgnores(%s) = %t, want %t", file, got, want)
		}
	}
}

func TestWithin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inside := filepath.Join(root, "Runs", "Out")
	if err := os.MkdirAll(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	rows := map[string]bool{root: true, inside: true, filepath.Join(root, "Runs", ".", "Out"): true, t.TempDir(): false, filepath.Dir(root): false}
	// On a volume that folds case another spelling names the same directory.
	if folded := filepath.Join(root, "runs", "out"); sameDirectory(folded, inside) {
		rows[folded] = true
	}
	for dir, want := range rows {
		if got, err := within(root, dir); err != nil || got != want {
			t.Errorf("within(root, %s) = %t, %v; want %t", dir, got, err, want)
		}
	}
	if _, err := within(root, filepath.Join(root, "missing")); err == nil {
		t.Error("within of a directory that does not exist reports no error")
	}
}

// removeAll removes a tree a test left with a directory it cannot write.
func TestRemoveAll_RemovesADirectoryWithoutWritePermission(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory its owner cannot write")
	}
	dir := filepath.Join(t.TempDir(), "tree")
	gittree.WriteFile(t, dir, "locked/x", []byte("x"), 0o600)
	if err := os.Chmod(filepath.Join(dir, "locked"), 0o500); err != nil { //nolint:gosec // a directory the test must not be able to write
		t.Fatal(err)
	}
	if err := removeAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the tree is still there")
	}
}

func sameDirectory(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ai, bi)
}

func TestTmpSpellings(t *testing.T) {
	t.Parallel()
	if asis, _, err := tmpSpellings(""); err != nil || !sameDirectory(asis, os.TempDir()) {
		t.Errorf("tmpSpellings with no TMPDIR = %q, %v; want the host's temporary directory", asis, err)
	}
	plain := t.TempDir()
	if asis, folded, err := tmpSpellings(plain); err != nil || folded != "" || !sameDirectory(asis, plain) || !strings.HasSuffix(asis, string(filepath.Separator)) {
		t.Errorf("tmpSpellings(%s) = %q, %q, %v", plain, asis, folded, err)
	}
	upper := filepath.Join(t.TempDir(), "T")
	if err := os.Mkdir(upper, 0o750); err != nil {
		t.Fatal(err)
	}
	asis, folded, err := tmpSpellings(upper)
	if err != nil || filepath.Base(asis) != "T" {
		t.Fatalf("tmpSpellings(%s) = %q, %q, %v", upper, asis, folded, err)
	}
	lower := filepath.Join(filepath.Dir(upper), "t")
	if folds := sameDirectory(lower, upper); folds != (folded != "") {
		t.Errorf("the folded spelling is %q on a volume where %s is the same directory: %t", folded, lower, folds)
	}
	if folded != "" && filepath.Base(folded) != "t" {
		t.Errorf("the folded spelling is %q, want it to end in t", folded)
	}
	// A sibling t that is another directory is no spelling of T.
	other := filepath.Join(t.TempDir(), "T")
	if err := os.Mkdir(other, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(other), "t"), 0o750); err == nil {
		if _, folded, err := tmpSpellings(other); err != nil || folded != "" {
			t.Errorf("a sibling directory t reads as the folded spelling %q, %v", folded, err)
		}
	}
}

// unlike names the first held path, in order, that the copy does not hold as
// copied, with its cause read from the checkout: a path the checkout still
// holds as copied was removed or changed by a process the run did not stop.
func TestTree_UnlikeNamesTheCauseFromTheCheckout(t *testing.T) {
	t.Parallel()
	other := sha256.Sum256([]byte("other\n"))
	rewrite := func(content string) func(t *testing.T, checkout string) {
		return func(t *testing.T, checkout string) {
			t.Helper()
			gittree.WriteFile(t, checkout, "m/m.go", []byte(content), 0o600)
		}
	}
	type row struct {
		name   string
		change func(t *testing.T, checkout string) // what the checkout gets, or nil
		edit   func(now map[string]entry)
		want   string // a part of the error, or "" for none
	}
	rows := []row{
		{"the copy as copied", nil, func(map[string]entry) {}, ""},
		{
			"a held file gone", nil, func(now map[string]entry) { delete(now, "m/m.go") },
			"m/m.go is gone after it was copied back, so a process the run did not stop removes it",
		},
		{
			"a held file with other bytes", nil, func(now map[string]entry) { now["m/m.go"] = entry{mode: now["m/m.go"].mode, sum: other} },
			"m/m.go is not what was copied back from the checkout, so a process the run did not stop changes it",
		},
		{
			"a held file another kind", nil, func(now map[string]entry) { now["m/m.go"] = entry{mode: os.ModeDir} },
			"m/m.go is not what was copied back from the checkout, so a process the run did not stop changes it",
		},
		{
			"a held file gone, the checkout changed", rewrite("package m\n"), func(now map[string]entry) { delete(now, "m/m.go") },
			"m/m.go is not what was copied, so the checkout changed during the run",
		},
		{
			"a held file with other bytes, the checkout changed", rewrite("package m\n"), func(now map[string]entry) { now["m/m.go"] = entry{mode: now["m/m.go"].mode, sum: other} },
			"m/m.go is not what was copied, so the checkout changed during the run",
		},
		{
			"a held file gone, the checkout's file removed", func(t *testing.T, checkout string) {
				t.Helper()
				if err := os.Remove(filepath.Join(checkout, "m", "m.go")); err != nil {
					t.Fatal(err)
				}
			}, func(now map[string]entry) { delete(now, "m/m.go") },
			"m/m.go is not what was copied, so the checkout changed during the run",
		},
		{
			"a held file that cannot be read", nil, func(now map[string]entry) { now["m/m.go"] = entry{mode: now["m/m.go"].mode} },
			"m/m.go cannot be read",
		},
	}
	if runtime.GOOS != "windows" {
		rows = append(rows,
			row{
				"a held file with other bytes, the checkout's mode changed", func(t *testing.T, checkout string) {
					t.Helper()
					if err := os.Chmod(filepath.Join(checkout, "m", "m.go"), 0o700); err != nil { //nolint:gosec // a mode the copy did not take
						t.Fatal(err)
					}
				}, func(now map[string]entry) { now["m/m.go"] = entry{mode: now["m/m.go"].mode, sum: other} },
				"m/m.go is not what was copied, so the checkout changed during the run",
			},
			row{
				"a held link with another target", nil, func(now map[string]entry) { now["m/link.go"] = entry{mode: now["m/link.go"].mode, sum: other} },
				"m/link.go is not what was copied back from the checkout, so a process the run did not stop changes it",
			},
			row{
				// The file takes the link's permission bits as far as the umask lets
				// it; checkoutHolds compares the kind of entry first.
				"a held link, the checkout's link made a file holding its target", func(t *testing.T, checkout string) {
					t.Helper()
					link := filepath.Join(checkout, "m", "link.go")
					info, err := os.Lstat(link)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(link); err != nil {
						t.Fatal(err)
					}
					gittree.WriteFile(t, checkout, "m/link.go", []byte("m.go"), info.Mode().Perm())
				}, func(now map[string]entry) { now["m/link.go"] = entry{mode: now["m/link.go"].mode, sum: other} },
				"m/link.go is not what was copied, so the checkout changed during the run",
			},
		)
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := copied(t)
			if tt.change != nil {
				tt.change(t, tree.run.checkout)
			}
			now := maps.Clone(tree.files)
			tt.edit(now)
			err := tree.unlike(now)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("unlike = %v, want nil", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("unlike = %v, want an error holding %q", err, tt.want)
			}
		})
	}

	// With every held path gone, the first in path order is named, run after run.
	t.Run("every held path gone", func(t *testing.T) {
		t.Parallel()
		tree := copied(t)
		first := slices.Sorted(maps.Keys(tree.files))[0]
		for range 20 {
			err := tree.unlike(map[string]entry{})
			if err == nil || !strings.Contains(err.Error(), ": "+first+" is gone after it was copied back") {
				t.Fatalf("unlike = %v, want the first path in order, %s, named", err, first)
			}
		}
	})
}

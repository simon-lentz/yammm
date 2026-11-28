package hostpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// TestStage_ReplacesAFileReachedThroughALongLink holds a staged write to the
// name the target was reached by: darwin counts a link's body and the rest of
// the path against MAXPATHLEN, so a link of 1,014 bytes leaves room for
// "/x.dat" and none for a longer staging name spelled through the link, which
// a lookup through the directory's handle never spells.
func TestStage_ReplacesAFileReachedThroughALongLink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the link needs a Unix kernel's MAXPATHLEN arithmetic")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "L")
	symlink(t, strings.Repeat("./", 507), link)
	target := filepath.Join(link, "x.dat")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatalf("the target itself must be reachable: %v", err)
	}

	stage, err := Stage(target, 0o600, func(token string) string { return ".yammm-" + token + ".tmp" })
	if err != nil {
		t.Fatalf("Stage(%s through a %d-byte link) = %v", filepath.Base(target), 1014, err)
	}
	defer stage.Discard()
	if _, err := stage.File.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err := stage.File.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatalf("Commit = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "x.dat")); err != nil || string(got) != "new" {
		t.Errorf("the file the link reaches holds %q, %v; want the staged bytes", got, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("the directory holds %d entries after the commit, want the link and the file: %v", len(entries), entries)
	}
}

// TestStage_ReplacesAFileInADirectoryThatCannotBeListed holds staging to the
// permission a rename needs: write and search on the directory, never read, as
// os.CreateTemp and os.Rename need.
func TestStage_ReplacesAFileInADirectoryThatCannotBeListed(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a Unix directory mode the process obeys")
	}
	dir := filepath.Join(t.TempDir(), "d")
	mkdirAll(t, dir)
	target := filepath.Join(dir, "x.dat")
	writeEmpty(t, target)
	if err := os.Chmod(dir, 0o300); err != nil { //nolint:gosec // a directory the process may write and enter but not list
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) }) //nolint:gosec // restoring the test's own directory
	stage, err := Stage(target, 0o600, func(token string) string { return ".s-" + token + ".tmp" })
	if err != nil {
		t.Fatalf("Stage in a directory of mode 0300 = %v", err)
	}
	defer stage.Discard()
	if _, err := stage.File.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err := stage.File.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatalf("Commit = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Errorf("the target holds %q, %v; want the staged bytes", got, err)
	}
}

// TestStage_CreatesTheFileAtItsMode holds the staging file to the mode it is
// given, under the umask, from its creation: a caller writes every byte before
// any chmod, so a wider mode would publish them.
func TestStage_CreatesTheFileAtItsMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no Unix permission bits")
	}
	for _, perm := range []fs.FileMode{0o600, 0o640} {
		target := filepath.Join(t.TempDir(), "x.dat")
		stage, err := Stage(target, perm, func(token string) string { return ".s-" + token + ".tmp" })
		if err != nil {
			t.Fatal(err)
		}
		info, err := stage.File.Stat()
		stage.Discard()
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got&^perm != 0 {
			t.Errorf("Stage(%v) created mode %v; want no bit beyond %v", perm, got, perm)
		}
	}
}

// TestStageDirPath_KeepsTheTextTheKernelWalks holds a staging name to its
// directory's text as given: a cleaned join cancels "l/.." on the text, where
// the kernel takes the parent of the directory l reaches.
func TestStageDirPath_KeepsTheTextTheKernelWalks(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	for _, tc := range []struct{ dir, want string }{
		{"d" + sep + "l" + sep + "..", "d" + sep + "l" + sep + ".." + sep + "x"},
		{"d" + sep, "d" + sep + "x"},
		{sep, sep + "x"},
	} {
		if got := (stageDir{name: tc.dir}).path("x"); got != tc.want {
			t.Errorf("stageDir{%q}.path(x) = %q; want %q", tc.dir, got, tc.want)
		}
	}
}

// TestStage_NeverOpensAFileAnotherWriterHolds holds a staging name to one no
// file holds: a taken name is passed over for the next, and a name function
// that gives only taken names is refused. The file that holds it is never
// written or removed.
func TestStage_NeverOpensAFileAnotherWriterHolds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	taken := filepath.Join(dir, ".taken.tmp")
	if err := os.WriteFile(taken, []byte("another writer's"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := true
	stage, err := Stage(filepath.Join(dir, "x.dat"), 0o600, func(token string) string {
		if first {
			first = false
			return ".taken.tmp"
		}
		return ".s-" + token + ".tmp"
	})
	if err != nil {
		t.Fatalf("Stage with a taken first name = %v; want the next name", err)
	}
	stage.Discard()
	if _, err := Stage(filepath.Join(dir, "x.dat"), 0o600, func(string) string { return ".taken.tmp" }); !errors.Is(err, fs.ErrExist) {
		t.Errorf("Stage with only a taken name = %v; want fs.ErrExist", err)
	}
	if got, err := os.ReadFile(taken); err != nil || string(got) != "another writer's" {
		t.Errorf("the other writer's file reads %q, %v; want it untouched", got, err)
	}
}

// TestStage_DiscardAfterACommitReleasesNothingAgain holds Discard after a
// Commit that landed to doing nothing: a descriptor opened after the Commit,
// which may reuse the directory's number, stays open.
func TestStage_DiscardAfterACommitReleasesNothingAgain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stage, err := Stage(filepath.Join(dir, "x.dat"), 0o600, func(token string) string { return ".s-" + token + ".tmp" })
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.File.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stage.Commit(); err != nil {
		t.Fatal(err)
	}
	later, err := os.Open(filepath.Join(dir, "x.dat"))
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	stage.Discard()
	if _, err := later.Stat(); err != nil {
		t.Errorf("a file opened after the Commit reads %v after Discard; want it open", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.dat")); err != nil {
		t.Errorf("Discard after a Commit removed the target: %v", err)
	}
}

// TestStage_ARefusedStageHoldsNoDescriptor holds a Stage whose create is refused
// to releasing the directory it opened. It runs alone, while every parallel
// test waits, so no other test's descriptor enters the count.
func TestStage_ARefusedStageHoldsNoDescriptor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows lists no open descriptors")
	}
	dir := t.TempDir()
	refused := func() {
		if _, err := Stage(filepath.Join(dir, "x.dat"), 0o600, func(string) string { return "no" + string(filepath.Separator) + "such" }); err == nil {
			t.Fatal("Stage under a missing directory reported success")
		}
	}
	refused() // the first call's one-time descriptors are not the count
	before := openDescriptors(t)
	for range 5 {
		refused()
	}
	if after := openDescriptors(t); after > before {
		t.Errorf("five refused Stages left %d more descriptors open", after-before)
	}
}

// openDescriptors counts this process's open descriptors. It reads names
// alone: os.ReadDir stats each entry, and a descriptor another goroutine closes
// between the listing and the stat fails it with EBADF.
func openDescriptors(t *testing.T) int {
	t.Helper()
	d, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

// TestStage_ATargetInDirectoryFormIsRefused holds Stage to rename(2)'s refusal
// of a file over a directory: a target that ends in a separator or a "."
// element is refused with EISDIR before any file is made, never staged under
// the element's own name.
func TestStage_ATargetInDirectoryFormIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{sub + string(filepath.Separator), sub + string(filepath.Separator) + "."} {
		stage, err := Stage(target, 0o600, func(token string) string { return "s" + token })
		if err == nil {
			stage.Discard()
		}
		if !errors.Is(err, syscall.EISDIR) {
			t.Errorf("Stage(%q) = %v, want EISDIR", target, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want sub alone", len(entries))
	}
}

package snapshot_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// linkedFixture builds dir/link -> real/deep/x and returns dir. A ".." after
// link names dir on the text and dir/real/deep on disk.
func linkedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "deep", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "deep", "x"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return dir
}

// readOrMissing returns the content of path, or "<missing>".
func readOrMissing(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// A symbolic link at the path survives, and the file it names is written,
// through a chain of links and through a dangling one.
func TestWriteFile_FollowsALinkAtThePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	links := map[string]string{
		"one.ys":      "real.ys",
		"two.ys":      "one.ys",
		"dangling.ys": "absent.ys",
	}
	if err := os.WriteFile(filepath.Join(dir, "real.ys"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	for _, c := range []struct{ link, reached string }{
		{"one.ys", "real.ys"},
		{"two.ys", "real.ys"},
		{"dangling.ys", "absent.ys"},
	} {
		payload := "through " + c.link
		if err := snapshot.WriteFile(filepath.Join(dir, c.link), []byte(payload)); err != nil {
			t.Fatalf("WriteFile(%s): %v", c.link, err)
		}
		if got := readOrMissing(filepath.Join(dir, c.reached)); got != payload {
			t.Errorf("WriteFile(%s): %s holds %q, want %q", c.link, c.reached, got, payload)
		}
	}
	for name, target := range links {
		if got, err := os.Readlink(filepath.Join(dir, name)); err != nil || got != target {
			t.Errorf("%s is no longer a link to %s (%q, %v)", name, target, got, err)
		}
	}
	assertNoStagingFiles(t, dir)
}

// A ".." in the path cancels the element before it, so a ".." after a linked
// directory names the file beside the link, and the staging file is made in
// that file's directory.
func TestWriteFile_EvaluatesADotDotOnThePathsText(t *testing.T) {
	t.Parallel()
	dir := linkedFixture(t)
	sep := string(filepath.Separator)
	if err := snapshot.WriteFile(dir+sep+"link"+sep+".."+sep+"w.ys", []byte("text")); err != nil {
		t.Fatal(err)
	}
	if got := readOrMissing(filepath.Join(dir, "w.ys")); got != "text" {
		t.Errorf("the file beside the link holds %q, want %q", got, "text")
	}
	if got := readOrMissing(filepath.Join(dir, "real", "deep", "w.ys")); got != "<missing>" {
		t.Errorf("the file the kernel reaches was written: %q", got)
	}
	assertNoStagingFiles(t, dir)
}

// A link whose target climbs out of a linked directory is written where the
// kernel reaches it, and staged beside that file: the directory the target's
// text names is sealed here, so a staging file made there fails.
func TestWriteFile_StagesBesideTheFileALinkReaches(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not honour a directory's permission bits, and evaluates a target's .. on its text")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	dir := linkedFixture(t)
	sep := string(filepath.Separator)
	if err := os.Symlink("link"+sep+".."+sep+"e.ys", filepath.Join(dir, "u.ys")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o550); err != nil { //nolint:gosec // a directory the test must not be able to write
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec // restoring the test's own directory
	if err := snapshot.WriteFile(filepath.Join(dir, "u.ys"), []byte("reached")); err != nil {
		t.Fatalf("WriteFile through u.ys: %v", err)
	}
	if got := readOrMissing(filepath.Join(dir, "real", "deep", "e.ys")); got != "reached" {
		t.Errorf("the file the link reaches holds %q, want %q", got, "reached")
	}
	assertNoStagingFiles(t, filepath.Join(dir, "real"))
}

// A cycle of links is refused rather than followed, and nothing is written.
func TestWriteFile_RefusesALinkCycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink("b.ys", filepath.Join(dir, "a.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("a.ys", filepath.Join(dir, "b.ys")); err != nil {
		t.Fatal(err)
	}
	err := snapshot.WriteFile(filepath.Join(dir, "a.ys"), []byte("x"))
	if !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("WriteFile over a cycle: error %v, want ELOOP", err)
	}
	if !strings.Contains(err.Error(), "resolve target") {
		t.Errorf("error %q does not name the failing step", err)
	}
	for _, name := range []string{"a.ys", "b.ys"} {
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is no longer a link (%v)", name, err)
		}
	}
	assertNoStagingFiles(t, dir)
}

// A chain of as many links as Linux follows is written through, and one link
// more is refused.
func TestWriteFile_FollowsAsManyLinksAsTheKernel(t *testing.T) {
	t.Parallel()
	const kernelBound = 40
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "end.ys"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := "end.ys"
	for i := 1; i <= kernelBound+1; i++ {
		name := fmt.Sprintf("l%d.ys", i)
		if err := os.Symlink(prev, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		prev = name
	}
	if err := snapshot.WriteFile(filepath.Join(dir, fmt.Sprintf("l%d.ys", kernelBound)), []byte("new")); err != nil {
		t.Fatalf("a chain of %d links: %v", kernelBound, err)
	}
	if got := readOrMissing(filepath.Join(dir, "end.ys")); got != "new" {
		t.Errorf("the end of a chain of %d links holds %q, want %q", kernelBound, got, "new")
	}
	err := snapshot.WriteFile(filepath.Join(dir, fmt.Sprintf("l%d.ys", kernelBound+1)), []byte("x"))
	if !errors.Is(err, syscall.ELOOP) {
		t.Errorf("a chain of %d links: error %v, want ELOOP", kernelBound+1, err)
	}
	assertNoStagingFiles(t, dir)
}

// ScanDir reads the directory its yielded paths name: a ".." after a linked
// directory cancels the element before it, so the scan lists the directory
// beside the link and opens each file it lists.
func TestScanDir_ReadsTheDirectoryItsPathsName(t *testing.T) {
	t.Parallel()
	dir := linkedFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "only_text.ys"), []byte("not a snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "deep", "only_kernel.ys"), []byte("not a snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	scanned := dir + sep + "link" + sep + ".."
	var names []string
	for entry, err := range snapshot.ScanDir(t.Context(), scanned) {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, entry.Name)
		if want := filepath.Join(scanned, entry.Name); entry.Path != want {
			t.Errorf("%s: Path %q, want %q", entry.Name, entry.Path, want)
		}
		for issue := range entry.Result.Issues() {
			if issue.Code() == diag.E_SNAPSHOT_IO {
				t.Errorf("%s could not be opened: %s", entry.Name, issue.Message())
			}
		}
	}
	if !slices.Equal(names, []string{"only_text.ys"}) {
		t.Errorf("ScanDir(%s) listed %q, want the directory beside the link", scanned, names)
	}
}

// A relative path that climbs out of a working directory entered through a
// symbolic link is written where the loader reads it, beside the link's own
// directory, and not beside the directory the link reached.
func TestWriteFile_ClimbsFromTheWorkingDirectoryAsSpelled(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "phys", "inner"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logical"), 0o750); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "logical", "cwd")
	if err := os.Symlink(filepath.Join(root, "phys", "inner"), cwd); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(cwd)
	if err := snapshot.WriteFile(".."+string(filepath.Separator)+"x.ys", []byte("logical")); err != nil {
		t.Fatal(err)
	}
	if got := readOrMissing(filepath.Join(root, "logical", "x.ys")); got != "logical" {
		t.Errorf("the file the loader reads holds %q, want %q", got, "logical")
	}
	if got := readOrMissing(filepath.Join(root, "phys", "x.ys")); got != "<missing>" {
		t.Errorf("the file beside the directory the link reached was written: %q", got)
	}
}

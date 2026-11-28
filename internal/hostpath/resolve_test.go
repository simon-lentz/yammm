package hostpath

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// diskSpelling is [yammmtest.DiskSpelling], which reads directory listings and
// shares no code with walkHostPath, failing the test on error.
func diskSpelling(t *testing.T, p string) string {
	t.Helper()
	spelled, err := yammmtest.DiskSpelling(p)
	if err != nil {
		t.Fatal(err)
	}
	return spelled
}

// hostPathTree writes root/Proj/Sub/File.yammm and returns the created path
// and the same file typed in lower case.
func hostPathTree(t *testing.T) (created, typed string) {
	t.Helper()
	root := diskSpelling(t, t.TempDir())
	if !yammmtest.CaseFoldingFilesystem(t, root) {
		t.Skip("the filesystem is case-sensitive, so two spellings name two files")
	}
	created = filepath.Join(root, "Proj", "Sub", "File.yammm")
	if err := os.MkdirAll(filepath.Dir(created), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return created, filepath.Join(root, "proj", "sub", "file.yammm")
}

// TestResolve_OneAnswerForTwoSpellingsOfOneFile holds the resolver to the file
// a path names: where the filesystem finds one file by two spellings, both
// spellings resolve to one host path, and it is the one the filesystem itself
// uses.
func TestResolve_OneAnswerForTwoSpellingsOfOneFile(t *testing.T) {
	t.Parallel()

	created, typed := hostPathTree(t)

	fromCreated, err := Resolve(created)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", created, err)
	}
	fromTyped, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", typed, err)
	}
	if fromTyped != fromCreated {
		t.Errorf("two spellings of one file resolve apart:\n  created %q\n  typed   %q", fromCreated, fromTyped)
	}
	if fromTyped != created {
		t.Errorf("the resolution is %q; want the filesystem's own spelling %q", fromTyped, created)
	}
}

// TestResolve_AgreesWithTheDirectoryListings holds the resolver to the
// spelling [yammmtest.DiskSpelling] reads from the directory listings for the
// typed path. That reading shares no code with walkHostPath, so a resolver that
// spells a component wrongly fails here.
func TestResolve_AgreesWithTheDirectoryListings(t *testing.T) {
	t.Parallel()

	_, typed := hostPathTree(t)
	onDisk := diskSpelling(t, typed)

	resolved, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", typed, err)
	}
	if resolved != onDisk {
		t.Errorf("Resolve(%q) = %q; want %q", typed, resolved, onDisk)
	}
}

// TestResolve_AnswerDoesNotChangeWhenTheFileIsCreated holds the resolution
// of a path that does not exist yet to the one the file gets once it is
// written. A load that names a file before it exists must not
// re-key it afterwards.
func TestResolve_AnswerDoesNotChangeWhenTheFileIsCreated(t *testing.T) {
	t.Parallel()

	// t.TempDir is reached through a symlink on darwin (/var -> /private/var),
	// which is what makes the two mints differ when only the second resolves.
	p := filepath.Join(t.TempDir(), "later.yammm")

	before, err := Resolve(p)
	if err != nil {
		t.Fatalf("Resolve(%q) before creation: %v", p, err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Resolve(p)
	if err != nil {
		t.Fatalf("Resolve(%q) after creation: %v", p, err)
	}
	if before != after {
		t.Errorf("the resolution changed when the file was created:\n  before %q\n  after  %q", before, after)
	}
}

// holdLinkIdentity holds the path typed, which reaches a file through a link,
// to the file the host opens through it: content written at the resolved path
// is what a read of typed returns, and the resolved path is the on-disk
// spelling, before and after the write.
func holdLinkIdentity(t *testing.T, typed string, content []byte) {
	t.Helper()
	before, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q) before the write: %v", typed, err)
	}
	if err := os.WriteFile(before, content, 0o600); err != nil {
		t.Fatalf("write the resolved path %q: %v", before, err)
	}
	if got, err := os.ReadFile(typed); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("reading %q returns %q (err %v), not the file written at its resolved path %q: the host reaches another file through the link",
			typed, got, err, before)
	}
	after, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q) after the write: %v", typed, err)
	}
	if onDisk := diskSpelling(t, typed); after != before || onDisk != before {
		t.Errorf("Resolve(%q):\n  before the write   %q\n  after              %q\n  on-disk spelling   %q",
			typed, before, after, onDisk)
	}
}

// TestResolve_DanglingLinkKeepsItsAnswerWhenItsTargetIsCreated holds a
// dangling link's resolution to the file the host reaches through it: a file
// written at the path resolved before the target exists is the file a read
// through the link returns, and resolving again gives that path. A ".." in a
// target tells a parent taken on disk, as Unix takes it, from one taken from
// the text, as Windows takes it; the read through the link is the judge.
func TestResolve_DanglingLinkKeepsItsAnswerWhenItsTargetIsCreated(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		// setup builds the tree under base and returns the dangling path to resolve.
		setup func(t *testing.T, base string) string
	}{
		{
			name: "a relative target with .. after a symlinked directory",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				mkdirAll(t, filepath.Join(base, "real", "deep"))
				symlink(t, filepath.Join("real", "deep"), filepath.Join(base, "sub"))
				symlink(t, filepath.FromSlash("sub/../x"), filepath.Join(base, "l"))
				return filepath.Join(base, "l")
			},
		},
		{
			name: "an absolute target with .. after a symlinked directory",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				mkdirAll(t, filepath.Join(base, "real", "deep"))
				symlink(t, filepath.Join("real", "deep"), filepath.Join(base, "sub"))
				symlink(t, base+filepath.FromSlash("/sub/../x"), filepath.Join(base, "l"))
				return filepath.Join(base, "l")
			},
		},
		{
			name: "a relative target inside a symlinked directory",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				mkdirAll(t, filepath.Join(base, "real", "dir"))
				symlink(t, filepath.Join("real", "dir"), filepath.Join(base, "alias"))
				symlink(t, "x", filepath.Join(base, "real", "dir", "l"))
				return filepath.Join(base, "alias", "l")
			},
		},
		{
			name: "a relative target with .. inside a symlinked directory",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				mkdirAll(t, filepath.Join(base, "real", "dir"))
				symlink(t, filepath.Join("real", "dir"), filepath.Join(base, "alias"))
				symlink(t, filepath.FromSlash("../x"), filepath.Join(base, "real", "dir", "l"))
				return filepath.Join(base, "alias", "l")
			},
		},
		{
			name: "a relative target whose second .. follows another symlinked directory",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				mkdirAll(t, filepath.Join(base, "real", "deep"))
				mkdirAll(t, filepath.Join(base, "real", "other", "deep"))
				symlink(t, filepath.Join("real", "deep"), filepath.Join(base, "sub"))
				symlink(t, filepath.Join("other", "deep"), filepath.Join(base, "real", "alias"))
				symlink(t, filepath.FromSlash("sub/../alias/../x"), filepath.Join(base, "l"))
				return filepath.Join(base, "l")
			},
		},
		{
			name: "a relative target inside a directory typed in another case",
			setup: func(t *testing.T, base string) string {
				t.Helper()
				if !yammmtest.CaseFoldingFilesystem(t, base) {
					t.Skip("the filesystem is case-sensitive, so two spellings name two directories")
				}
				mkdirAll(t, filepath.Join(base, "Dir"))
				symlink(t, "x", filepath.Join(base, "Dir", "l"))
				return filepath.Join(base, "dir", "l")
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := diskSpelling(t, t.TempDir())
			holdLinkIdentity(t, row.setup(t, base), []byte(row.name))
		})
	}
}

// On Unix a dangling link whose target takes a ".." past a component the
// kernel cannot enter names no file: the kernel refuses the path, so the
// resolver refuses it rather than cancel the component on the text.
func TestResolve_RefusesALinkTargetThatClimbsPastWhatTheKernelCannotEnter(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, target string
		want         error
	}{
		{"a missing component", "m/../e.json", fs.ErrNotExist},
		{"a missing component, then more", "m/x/../../e.json", fs.ErrNotExist},
		{"a regular file", "f/../e.json", syscall.ENOTDIR},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := diskSpelling(t, t.TempDir())
			for _, f := range []string{"e.json", "f"} {
				if err := os.WriteFile(filepath.Join(base, f), []byte(f), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(base, "u.json")
			symlink(t, filepath.FromSlash(row.target), link)
			if runtime.GOOS == "windows" {
				// Windows evaluates the target's ".." on its text, so the kernel
				// reads e.json, and the resolver names it.
				want, err := Resolve(filepath.Join(base, "e.json"))
				if err != nil {
					t.Fatal(err)
				}
				if got, err := os.ReadFile(link); err != nil || string(got) != "e.json" {
					t.Fatalf("read through %s -> %s = %q, %v; want e.json's bytes", link, row.target, got, err)
				}
				if host, err := Resolve(link); err != nil || host != want {
					t.Errorf("Resolve(%s -> %s) = %q, %v; want %q, the file the kernel read", link, row.target, host, err, want)
				}
				return
			}
			if _, err := os.ReadFile(link); err == nil {
				t.Fatalf("the kernel reads through %s -> %s", link, row.target)
			}
			if host, err := Resolve(link); !errors.Is(err, row.want) {
				t.Errorf("Resolve(%s -> %s) = %q, %v; want an error wrapping %v", link, row.target, host, err, row.want)
			}
		})
	}
}

// TestResolve_LinkNamesTheFileTheHostReaches holds a link whose target
// names a symlinked directory before a ".." to the file the host opens through
// it. Unix follows that directory and takes its parent on disk; Windows
// evaluates the ".." on the target's text and never follows it. A file at each
// answer means a resolver with the other host's rule names a file the host
// does not open.
func TestResolve_LinkNamesTheFileTheHostReaches(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		// files are the paths under base written before the link is resolved,
		// each holding its own name.
		files []string
	}{
		{"a file at both answers", []string{"x", filepath.FromSlash("real/x")}},
		{"a file at the text's answer alone", []string{"x"}},
		{"a file at the disk's answer alone", []string{filepath.FromSlash("real/x")}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			base := diskSpelling(t, t.TempDir())
			mkdirAll(t, filepath.Join(base, "real", "deep"))
			symlink(t, filepath.Join("real", "deep"), filepath.Join(base, "sub"))
			symlink(t, filepath.FromSlash("sub/../x"), filepath.Join(base, "l"))
			for _, f := range row.files {
				if err := os.WriteFile(filepath.Join(base, f), []byte(f), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			holdLinkIdentity(t, filepath.Join(base, "l"), []byte(row.name))
		})
	}
}

// TestLexicalTarget_EvaluatesADotDotOnTheText holds the Windows rule for a
// link target as a function of text alone: nothing is looked up, so a ".."
// after a name takes that name's textual parent whatever the name is on disk.
func TestLexicalTarget_EvaluatesADotDotOnTheText(t *testing.T) {
	t.Parallel()

	base := yammmtest.HostAbs("/base")
	root := filepath.VolumeName(base) + string(filepath.Separator)
	rows := []struct{ name, dir, target, want string }{
		{"a relative target with .. after a name", base, filepath.FromSlash("sub/../x"), filepath.Join(base, "x")},
		{"a relative target with .. after two names", base, filepath.FromSlash("sub/../alias/../x"), filepath.Join(base, "x")},
		{"a relative target that climbs out of the directory", filepath.Join(base, "real", "dir"), filepath.FromSlash("../x"), filepath.Join(base, "real", "x")},
		{"a relative target with no ..", filepath.Join(base, "dir"), "x", filepath.Join(base, "dir", "x")},
		{"a target of .", filepath.Join(base, "dir"), ".", filepath.Join(base, "dir")},
		{"an absolute target with ..", base, base + filepath.FromSlash("/sub/../x"), filepath.Join(base, "x")},
		{"a rooted target with ..", base, filepath.FromSlash("/r/../x"), filepath.Join(root, "x")},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := lexicalTarget(row.dir, row.target); got != row.want {
				t.Errorf("lexicalTarget(%q, %q) = %q; want %q", row.dir, row.target, got, row.want)
			}
		})
	}
}

// A descriptor path reaches an open file that the host may give no name for, and
// reading the path the resolver answers must read that file.
func TestResolve_AnswersAPathThatReachesADescriptorsFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	const content = heldByADescriptor
	descriptorPath := func(f *os.File) string { return fmt.Sprintf("/dev/fd/%d", f.Fd()) }

	t.Run("a pipe", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if _, err := w.WriteString(content); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		checkReadsThrough(t, descriptorPath(r))
	})
	t.Run("a regular file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "held.txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		checkReadsThrough(t, descriptorPath(f))
	})
	// darwin's realpath(3) answers /dev/fd/<the file's name>, here another
	// descriptor's path, which exists and reaches another file.
	t.Run("a regular file named for another descriptor", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "0")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		checkReadsThrough(t, descriptorPath(f))
	})
	t.Run("a path under a descriptor's regular file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "held.txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if host, err := Resolve(descriptorPath(f) + "/x"); err == nil {
			t.Errorf("Resolve answered %q for a path under a regular file", host)
		}
	})
	t.Run("an unlinked file", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "gone.txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		checkReadsThrough(t, descriptorPath(f))
	})
}

// A link that ends at a descriptor path reaches the file the descriptor holds,
// which the host may name by a path that reaches no file, another descriptor's
// file, or, on Linux, a name in a process's own procfs table. The typed path is
// the one name sure to reach it, so the resolver answers it, however the chain
// runs and whether or not the descriptor is open.
func TestResolve_KeepsTheTypedPathOfALinkToADescriptor(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	dir := diskSpelling(t, t.TempDir())
	// Named "0", darwin's realpath answers /dev/fd/0 for its descriptor.
	held := filepath.Join(dir, "0")
	if err := os.WriteFile(held, []byte(heldByADescriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := fmt.Sprintf("/dev/fd/%d", f.Fd())

	links := map[string]string{
		"to-fd":     fd,
		"to-stdin":  "/dev/stdin",
		"to-closed": "/dev/fd/9999",
		"chain":     "to-fd",
		"to-dev":    "/dev",
	}
	for name, target := range links {
		symlink(t, target, filepath.Join(dir, name))
	}
	for _, typed := range []string{
		filepath.Join(dir, "to-fd"),
		filepath.Join(dir, "to-stdin"),
		filepath.Join(dir, "to-closed"),
		filepath.Join(dir, "chain"),
	} {
		if got, err := Resolve(typed); err != nil || got != typed {
			t.Errorf("Resolve(%q) = %q, %v; want the typed path", typed, got, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "chain"))
	if err != nil || string(got) != heldByADescriptor {
		t.Errorf("read through the chain = %q, %v; want the descriptor's file", got, err)
	}
	// A descriptor path a link to /dev reaches is named from /dev, which judged it.
	if got, err := Resolve(filepath.Join(dir, "to-dev", "stdout")); err != nil || got != "/dev/stdout" {
		t.Errorf("Resolve through a link to /dev = %q, %v; want /dev/stdout", got, err)
	}
	// Only the link's own element has no sure name: typed through a link to
	// its directory, it keeps that element in the directory's spelling.
	alias := filepath.Join(diskSpelling(t, t.TempDir()), "alias")
	symlink(t, dir, alias)
	if got, err := Resolve(filepath.Join(alias, "to-fd")); err != nil || got != filepath.Join(dir, "to-fd") {
		t.Errorf("Resolve through a link to the directory = %q, %v; want %q", got, err, filepath.Join(dir, "to-fd"))
	}
}

// TestResolve_RefusesALinkChainToADescriptorTheKernelRefuses holds a chain of
// links longer than the kernel follows to the kernel's answer, ELOOP, when it
// ends at a descriptor path as when it ends at a file.
func TestResolve_RefusesALinkChainToADescriptorTheKernelRefuses(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	dir := diskSpelling(t, t.TempDir())
	file := filepath.Join(dir, "f")
	writeEmpty(t, file)
	// darwin follows 32 links and Linux 40.
	const n = 64
	for _, end := range []string{"/dev/stdin", file} {
		prefix := "d"
		if end == file {
			prefix = "f"
		}
		symlink(t, end, filepath.Join(dir, prefix+"0"))
		for i := 1; i < n; i++ {
			symlink(t, fmt.Sprintf("%s%d", prefix, i-1), filepath.Join(dir, fmt.Sprintf("%s%d", prefix, i)))
		}
		typed := filepath.Join(dir, fmt.Sprintf("%s%d", prefix, n-1))
		if _, err := os.Stat(typed); !errors.Is(err, syscall.ELOOP) {
			t.Fatalf("the kernel answered %v for %d links; want ELOOP", err, n)
		}
		if got, err := Resolve(typed); !errors.Is(err, syscall.ELOOP) {
			t.Errorf("Resolve of %d links to %s = %q, %v; want ELOOP", n, end, got, err)
		}
	}
}

// A descriptor of a directory reaches the directory, which has a name on disk:
// Linux spells it through procfs, and darwin's speller answers inside the
// descriptor table, where the typed path stands. Either way the answer, and a
// path under it, reach the directory the descriptor holds.
func TestResolve_ReachesTheDirectoryADescriptorHolds(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	dir := diskSpelling(t, t.TempDir())
	// Named "1", darwin's realpath answers /dev/fd/1 for its descriptor.
	held := filepath.Join(dir, "1")
	mkdirAll(t, held)
	writeEmpty(t, filepath.Join(held, "x.yammm"))
	d, err := os.Open(held)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fd := fmt.Sprintf("/dev/fd/%d", d.Fd())
	symlink(t, fd, filepath.Join(dir, "ld"))
	want, err := os.Stat(held)
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{fd, filepath.Join(dir, "ld")} {
		got, err := Resolve(typed)
		if err != nil {
			t.Errorf("Resolve(%q) = %v", typed, err)
			continue
		}
		// darwin's stat of /dev/fd/N answers the descriptor table's own node;
		// an open duplicates the descriptor, so its fstat reaches the file.
		if !opensAs(t, got, want) {
			t.Errorf("Resolve(%q) = %q, which opens no held directory", typed, got)
		}
		if got == "/dev/fd/1" {
			t.Errorf("Resolve(%q) = %q, another descriptor's path", typed, got)
		}
	}
	under := fd + "/x.yammm"
	if got, err := Resolve(under); err != nil || strings.HasPrefix(got, "/dev/fd/1/") {
		t.Errorf("Resolve(%q) = %q, %v; want no other descriptor's path", under, got, err)
	}
}

// opensAs reports whether an open of p reaches the file want describes.
func opensAs(t *testing.T, p string, want fs.FileInfo) bool {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && os.SameFile(info, want)
}

// A file replaced during a resolve is spelled on disk at every instant, since the
// kernel reaches it through its directory at every instant.
func TestResolve_AFileReplacedDuringAResolveKeepsItsSpelling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "real"))
	symlink(t, "real", filepath.Join(dir, "lnk"))
	target := filepath.Join(dir, "real", "a.json")
	writeEmpty(t, target)
	want, err := Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var replacer sync.WaitGroup
	var replaced atomic.Int64
	replacer.Go(func() {
		next := filepath.Join(dir, "next")
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.WriteFile(next, nil, 0o600); err != nil {
				t.Error(err)
				return
			}
			err := os.Rename(next, target)
			// Windows refuses to replace a file while a resolve holds a handle
			// on it: that replace has not happened yet.
			if runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
				continue
			}
			if err != nil {
				t.Error(err)
				return
			}
			replaced.Add(1)
		}
	})
	for range 5000 {
		got, err := Resolve(filepath.Join(dir, "lnk", "a.json"))
		if err != nil || got != want {
			t.Errorf("Resolve = %q, %v; want %q", got, err, want)
			break
		}
	}
	close(stop)
	replacer.Wait()
	if replaced.Load() == 0 {
		t.Error("no replace landed while the resolves ran, so none was judged")
	}
}

// TestResolve_AFileRemovedDuringAResolveResolvesAsMissing holds a file removed
// between the stat that finds it and the spelling to the answer a missing file
// has, its typed name in its spelled directory, never a refusal.
func TestResolve_AFileRemovedDuringAResolveResolvesAsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "real"))
	symlink(t, "real", filepath.Join(dir, "lnk"))
	target := filepath.Join(dir, "real", "a.json")
	want, err := Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var churner sync.WaitGroup
	var removed atomic.Int64
	churner.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.WriteFile(target, nil, 0o600); err != nil {
				// Windows refuses to create a file a resolve's handle still holds
				// for deletion: that cycle has not happened yet.
				if runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
					continue
				}
				t.Error(err)
				return
			}
			if err := os.Remove(target); err == nil {
				removed.Add(1)
			}
		}
	})
	for range 5000 {
		got, err := Resolve(filepath.Join(dir, "lnk", "a.json"))
		if err != nil || got != want {
			t.Errorf("Resolve = %q, %v; want %q", got, err, want)
			break
		}
	}
	close(stop)
	churner.Wait()
	if removed.Load() == 0 {
		t.Error("no remove landed while the resolves ran, so none was judged")
	}
}

// TestResolve_ARefusalIsAPathErrorOfTheTypedPath holds a refusal to one
// *fs.PathError, Op "resolve", that names the path as typed.
func TestResolve_ARefusalIsAPathErrorOfTheTypedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f.json")
	writeEmpty(t, file)
	typed := file + string(filepath.Separator)
	_, err := Resolve(typed)
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || pe.Op != "resolve" || pe.Path != typed || !errors.Is(err, ErrNotDirectory) {
		t.Errorf("Resolve(%q) error = %#v; want a PathError{Op: resolve, Path: the typed path} of ErrNotDirectory", typed, err)
	}
}

// TestResolve_ALinkWithAnEmptyTargetResolvesAsMissing holds a link whose target
// is empty to a missing file of its own name: the kernel reaches nothing
// through it, so neither the link nor a path under it takes its directory's
// name.
func TestResolve_ALinkWithAnEmptyTargetResolvesAsMissing(t *testing.T) {
	t.Parallel()
	dir := diskSpelling(t, t.TempDir())
	link := filepath.Join(dir, "empty")
	if err := os.Symlink("", link); err != nil {
		t.Skipf("this host makes no link with an empty target: %v", err)
	}
	for _, typed := range []string{link, filepath.Join(link, "x")} {
		if got, err := Resolve(typed); err != nil || got != typed {
			t.Errorf("Resolve(%q) = %q, %v; want the typed path", typed, got, err)
		}
	}
}

// TestResolve_ARelativePathUnderARemovedDirectoryNamesThePath holds the refusal
// of a relative path, when the working directory is gone and no absolute form
// exists, to naming the path.
func TestResolve_ARelativePathUnderARemovedDirectoryNamesThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	mkdirAll(t, dir)
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Skipf("this host keeps a working directory in use: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("this host still names a removed working directory")
	}
	_, err := Resolve("x.yammm")
	pe, ok := errors.AsType[*fs.PathError](err)
	if !ok || pe.Op != "resolve" || pe.Path != "x.yammm" {
		t.Errorf("Resolve(x.yammm) under a removed directory = %v; want a resolve refusal that names x.yammm", err)
	}
}

// heldByADescriptor is what each file [checkReadsThrough] reads holds.
const heldByADescriptor = "held by a descriptor"

// checkReadsThrough requires the descriptor path typed to answer itself, and
// reading that answer to give [heldByADescriptor].
func checkReadsThrough(t *testing.T, typed string) {
	t.Helper()
	host, err := Resolve(typed)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", typed, err)
	}
	if host != typed {
		t.Errorf("Resolve(%q) = %q, want itself", typed, host)
	}
	got, err := os.ReadFile(host)
	if err != nil {
		t.Fatalf("read %q, the answer for %q: %v", host, typed, err)
	}
	if string(got) != heldByADescriptor {
		t.Errorf("read %q through %q, want %q", got, host, heldByADescriptor)
	}
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func writeEmpty(t *testing.T, file string) {
	t.Helper()
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// longTail creates a file under base whose path is exactly n bytes long and
// returns that path relative to base.
func longTail(t *testing.T, base string, n int) string {
	t.Helper()
	const nameMax = 255
	var dirs []string
	room := n - len(base) - len(string(filepath.Separator))
	for room > nameMax {
		dirs = append(dirs, strings.Repeat("d", 200))
		room -= 200 + len(string(filepath.Separator))
	}
	if room <= len(".yammm") {
		t.Fatalf("%q leaves no room for a path of %d bytes", base, n)
	}
	leaf := strings.Repeat("f", room-len(".yammm")) + ".yammm"
	dir := filepath.Join(append([]string{base}, dirs...)...)
	mkdirAll(t, dir)
	writeEmpty(t, filepath.Join(dir, leaf))
	return filepath.Join(append(dirs, leaf)...)
}

// childResolveEnv carries the path a child run of a test resolves, and
// childWantEnv the host path the child must get back.
const (
	childResolveEnv = "LOCATION_TEST_CHILD_RESOLVE"
	childWantEnv    = "LOCATION_TEST_CHILD_WANT"
)

// resolveInChild runs the top-level test name again in a new process of this
// test binary, handing it typed and want, and fails t with the child's output
// when the child fails. realpath(3) writes up to PATH_MAX bytes into the buffer
// it is given, so a resolver with a shorter buffer can crash the process; the
// crash then fails only t.
func resolveInChild(t *testing.T, name, typed, want string) {
	t.Helper()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+name+"$", "-test.count=1") //nolint:gosec // runs this test binary again
	child.Env = append(os.Environ(), childResolveEnv+"="+typed, childWantEnv+"="+want)
	if output, err := child.CombinedOutput(); err != nil {
		t.Errorf("the child run of %s for %q failed: %v\n%s", name, typed, err, output[:min(len(output), 4096)])
	}
}

// TestResolve_SpellsALongPathInFull holds the resolver to the whole
// answer for a path longer than a short buffer holds, up to the longest path
// that PATH_MAX, 1024 bytes with its NUL on darwin, leaves room for. Each path
// resolves in a child run of this test.
func TestResolve_SpellsALongPathInFull(t *testing.T) {
	t.Parallel()
	if typed, ok := os.LookupEnv(childResolveEnv); ok {
		want := os.Getenv(childWantEnv)
		if got, err := Resolve(typed); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q (%d bytes), %v; want %q (%d bytes)", typed, got, len(got), err, want, len(want))
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("a Windows path longer than MAX_PATH depends on the host's long-path setting")
	}

	for _, n := range []int{257, 600, 1023} {
		t.Run(strconv.Itoa(n)+" bytes", func(t *testing.T) {
			t.Parallel()
			// The path is typed from t.TempDir as given, so its resolved form,
			// not its typed form, is the one that is n bytes long.
			typedBase := t.TempDir()
			typed := filepath.Join(typedBase, longTail(t, diskSpelling(t, typedBase), n))
			want := diskSpelling(t, typed)
			if len(want) != n {
				t.Fatalf("%q resolves to %d bytes on disk; the row needs %d", typed, len(want), n)
			}
			resolveInChild(t, "TestResolve_SpellsALongPathInFull", typed, want)
		})
	}
}

// TestResolve_RefusesAPathWhoseResolvedFormExceedsPathMax pins a
// refusal on darwin. stat follows links with no bound on the length of the path
// they reach, but realpath(3) cannot write an answer of PATH_MAX bytes or more.
// Such a path exists and has no spelling, so every door refuses it rather than
// return part of one.
func TestResolve_RefusesAPathWhoseResolvedFormExceedsPathMax(t *testing.T) {
	t.Parallel()
	if typed, ok := os.LookupEnv(childResolveEnv); ok {
		if got, err := Resolve(typed); !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Errorf("Resolve(%q) = %q, %v; want ENAMETOOLONG", typed, got, err)
		}
		return
	}
	if runtime.GOOS != "darwin" {
		t.Skip("the PATH_MAX bound belongs to darwin's realpath(3)")
	}

	// base/hop and chain/hop each name the relative chain below them, so base/hop/hop
	// resolves to base/chain/chain. The kernel's lookup holds one link's target at
	// a time, under PATH_MAX, while the resolved path is longer than PATH_MAX.
	// darwin refuses mkdir by a path that resolves that far, so the tree is built
	// through a directory descriptor.
	base := diskSpelling(t, t.TempDir())
	chain := filepath.Join(slices.Repeat([]string{strings.Repeat("d", 200)}, 4)...)
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Join(chain, chain), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"hop", filepath.Join(chain, "hop")} {
		if err := root.Symlink(chain, link); err != nil {
			t.Fatal(err)
		}
	}
	typed := filepath.Join(base, "hop", "hop")
	if resolved := filepath.Join(base, chain, chain); len(resolved) < 1024 {
		t.Fatalf("the resolved path %q is %d bytes; want at least PATH_MAX", resolved, len(resolved))
	}
	if info, err := os.Stat(typed); err != nil || !info.IsDir() {
		t.Fatalf("stat(%q) through the links = %v, %v; want a directory", typed, info, err)
	}
	resolveInChild(t, "TestResolve_RefusesAPathWhoseResolvedFormExceedsPathMax", typed, "")
}

// TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling holds a path whose
// resolution passes through a directory past PATH_MAX to the speller's
// refusal, named once and with no other cause: a dangling link that climbs out
// of that directory with "..", whether or not the kernel then reaches a file,
// and a dangling link inside it, whose directory the speller cannot write.
func TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling(t *testing.T) {
	t.Parallel()
	if typed, ok := os.LookupEnv(childResolveEnv); ok {
		got, err := Resolve(typed)
		if !errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, ErrNotDirectory) {
			t.Errorf("Resolve(%q) = %q, %v; want ENAMETOOLONG and no ErrNotDirectory", typed, got, err)
		}
		if err != nil && strings.Count(err.Error(), typed) != 1 {
			t.Errorf("Resolve(%q) error names the path %d times: %v", typed, strings.Count(err.Error(), typed), err)
		}
		// A refusal at a later hop names the link it was reading.
		if link := os.Getenv(childWantEnv); link != "" && (err == nil || !strings.Contains(err.Error(), strconv.Quote(link))) {
			t.Errorf("Resolve(%q) = %v; want the cause to name the link %q", typed, err, link)
		}
		return
	}
	if runtime.GOOS != "darwin" {
		t.Skip("the PATH_MAX bound belongs to darwin's realpath(3)")
	}

	// lnk is a relative link, under PATH_MAX, to a directory whose real path
	// is past it, and dl's target climbs out of that directory by one "..".
	// darwin refuses mkdir by a path that resolves that far, so the tree is
	// built through a directory descriptor.
	base := diskSpelling(t, t.TempDir())
	chain := filepath.Join(slices.Repeat([]string{strings.Repeat("d", 200)}, 4)...)
	last := 1046 - len(base) - len(string(filepath.Separator)+chain+string(filepath.Separator))
	if last < 1 || last > 255 {
		t.Skipf("the temporary directory %q leaves no element length for a real path of 1,046 bytes", base)
	}
	target := filepath.Join(chain, strings.Repeat("e", last))
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(target, "lnk"); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("lnk/../m.yammm", "dl"); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink("nothing", filepath.Join(target, "inner")); err != nil {
		t.Fatal(err)
	}
	typed := filepath.Join(base, "dl")
	if _, err := os.Stat(typed); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat(%q) = %v; want the kernel's ENOENT for the file the link climbs to", typed, err)
	}
	resolveInChild(t, "TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling", typed, "")
	resolveInChild(t, "TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling", filepath.Join(base, "lnk", "inner"), "")
	if err := root.Symlink("dl", "via"); err != nil {
		t.Fatal(err)
	}
	resolveInChild(t, "TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling", filepath.Join(base, "via"), typed)
	if err := root.WriteFile(filepath.Join(filepath.Dir(target), "m.yammm"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(typed); err != nil {
		t.Fatalf("stat(%q) = %v; want the file the link climbs to", typed, err)
	}
	resolveInChild(t, "TestResolve_RefusesALinkTargetWhoseDotDotHasNoSpelling", typed, "")
}

// TestResolve_RefusesAnEmptyPath holds the resolver to refusing an unset file
// name, where filepath.Abs would make it the working directory.
func TestResolve_RefusesAnEmptyPath(t *testing.T) {
	t.Parallel()
	if _, err := Resolve(""); !errors.Is(err, ErrEmptyPath) {
		t.Errorf("Resolve(\"\") error = %v; want ErrEmptyPath", err)
	}
}

// A typed path that is valid UTF-8 can resolve to one that is not: through a
// dangling link's target, or through a directory named on disk in other bytes.
// An identity derived from it would reach both JSON wires, so the resolver
// refuses it.
// Each fixture is made twice: with the byte 0xFF, and with a lone surrogate in
// its WTF-8 form. On Windows Go writes U+FFFD for 0xFF, so that row runs only
// where the fixture reads back in the bytes it was given. Go keeps the
// surrogate on every host, so its row runs wherever the host makes the fixture.
func TestResolve_RefusesAResolutionThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]string{}
	for i, bad := range []struct {
		name, bytes string
		kept        bool
	}{
		{"the byte 0xFF", "\xff", false},
		{"a lone surrogate", "\xed\xa0\x80", true},
	} {
		link := filepath.Join(dir, "dangling"+strconv.Itoa(i)+".yammm")
		target := "missing-" + bad.bytes + "-name.yammm"
		if err := os.Symlink(target, link); err == nil {
			switch got, err := os.Readlink(link); {
			case err == nil && got == target:
				cases["a dangling link's target, "+bad.name] = link
			case bad.kept:
				t.Errorf("os.Readlink(%q) = %q, %v; want the target %q", link, got, err, target)
			}
		}
		dirName := "caf" + bad.bytes
		named := filepath.Join(dir, dirName)
		if err := os.Mkdir(named, 0o750); err == nil {
			switch {
			case holdsName(t, dir, dirName):
				through := filepath.Join(dir, "through"+strconv.Itoa(i))
				if err := os.Symlink(named, through); err == nil {
					cases["a directory named on disk, "+bad.name] = filepath.Join(through, "s.yammm")
				}
			case bad.kept:
				t.Errorf("%s does not list %q after os.Mkdir made it", dir, dirName)
			}
		}
	}
	if len(cases) == 0 {
		t.Skip("this host makes neither a symbolic link nor a name in bytes that are not valid UTF-8")
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if host, err := Resolve(p); !errors.Is(err, ErrInvalidUTF8Path) {
				t.Errorf("Resolve(%q) = %q, %v; want ErrInvalidUTF8Path", p, host, err)
			}
		})
	}
}

// holdsName reports whether dir lists an entry named name, byte for byte.
func holdsName(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == name })
}

// A ".." the path holds is taken on its text first, so an element it cancels is
// no part of the path: a writer that cleans the path writes the file the
// identity then names, whatever bytes the cancelled element held.
func TestResolve_JudgesUTF8AfterTheDotDotTextRule(t *testing.T) {
	t.Parallel()
	dir := diskSpelling(t, t.TempDir())
	sep := string(filepath.Separator)
	want, err := Resolve(dir + sep + "v.yammm")
	if err != nil {
		t.Fatal(err)
	}
	typed := dir + sep + "\xff" + sep + ".." + sep + "v.yammm"
	if got, err := Resolve(typed); err != nil || got != want {
		t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, want)
	}
	if _, err := Resolve(dir + sep + "\xff" + sep + "v.yammm"); !errors.Is(err, ErrInvalidUTF8Path) {
		t.Errorf("an element no \"..\" cancels is refused: got %v", err)
	}
}

// TestResolve_RefusesAPathThatIsNotValidUTF8 holds the resolver to text both
// JSON wires can carry. It runs on every host: the input is a string, and no
// file is opened.
func TestResolve_RefusesAPathThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	bad := filepath.Join(t.TempDir(), "caf\xff.yammm")
	if _, err := Resolve(bad); !errors.Is(err, ErrInvalidUTF8Path) {
		t.Errorf("Resolve(%q) error = %v; want ErrInvalidUTF8Path", bad, err)
	}
}

// TestResolve_RefusesAPathUnderARegularFile pins a refusal: a path can
// never exist under a regular file, and the resolver says so rather than
// keeping the path as typed.
func TestResolve_RefusesAPathUnderARegularFile(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "regular.yammm")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(file, "child.yammm")

	// Every host reports it as the Unix kernel does, so a caller tells it from
	// a missing path by errors.Is.
	if _, err := Resolve(under); !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("Resolve(%q) = %v; want an error wrapping ENOTDIR", under, err)
	}
}

// TestResolve_SpellsAPathTheProcessCannotRead holds the resolver to
// looking a path up without reading it. A file with no permission bits and a
// directory the process can traverse but not list are spelled on disk.
func TestResolve_SpellsAPathTheProcessCannotRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a Windows permission bit does not stop a read")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a path whatever its permission bits")
	}

	root := diskSpelling(t, t.TempDir())
	// Where the filesystem folds case, each path is typed in lower case, so a
	// resolver that keeps the typed spelling is seen.
	retype := func(p string) string { return p }
	if yammmtest.CaseFoldingFilesystem(t, root) {
		retype = func(p string) string { return root + strings.ToLower(strings.TrimPrefix(p, root)) }
	}

	sealed := filepath.Join(root, "Sealed.yammm")
	unlisted := filepath.Join(root, "Unlisted")
	inUnlisted := filepath.Join(unlisted, "File.yammm")
	if err := os.Mkdir(unlisted, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{sealed, inUnlisted} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(sealed, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unlisted, 0o300); err != nil { //nolint:gosec // a directory the process can traverse but not list
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unlisted, 0o700) }) //nolint:gosec // the temp directory's removal lists it

	rows := []struct{ name, created string }{
		{"a file with no permission bits", sealed},
		{"a directory the process can traverse but not list", unlisted},
		{"a file in a directory the process can traverse but not list", inUnlisted},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			typed := retype(row.created)
			if got, err := Resolve(typed); err != nil || got != row.created {
				t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, row.created)
			}
		})
	}
}

// TestResolve_KeepsWhatTheLSPDependsOn pins the three rules the editor
// depends on: a relative path is made absolute from the working directory as
// the filesystem spells it, cleaning runs before resolution so a ".." across a
// symlink lands where the loader lands it, and a name no file answers to is
// kept as typed.
func TestResolve_KeepsWhatTheLSPDependsOn(t *testing.T) {
	t.Parallel()

	base := diskSpelling(t, t.TempDir())
	sep := string(filepath.Separator)

	t.Run("a relative path is made absolute", func(t *testing.T) {
		t.Parallel()
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		rel := filepath.Join("does-not-exist", "schema.yammm")
		got, err := Resolve(rel)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", rel, err)
		}
		// os.Getwd keeps a symlinked or differently cased $PWD; the resolver
		// spells the working directory, the deepest part that exists, on disk.
		if want := filepath.Join(diskSpelling(t, cwd), rel); got != want {
			t.Errorf("Resolve(%q) = %q; want %q", rel, got, want)
		}
	})

	t.Run("cleaning runs before resolution", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(base, "clean")
		real := filepath.Join(dir, "a", "real")
		if err := os.MkdirAll(real, 0o750); err != nil {
			t.Fatal(err)
		}
		// The link's own directory is dir and its target's parent is dir/a, so
		// the two orders reach two different files.
		cleanedFirst := filepath.Join(dir, "target.yammm")
		resolvedFirst := filepath.Join(dir, "a", "target.yammm")
		for _, f := range []string{cleanedFirst, resolvedFirst} {
			if err := os.WriteFile(f, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		// Built by concatenation, not filepath.Join: Join cleans, which would
		// collapse the ".." before the resolver ever saw it.
		in := link + sep + ".." + sep + "target.yammm"
		got, err := Resolve(in)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", in, err)
		}
		if got != cleanedFirst {
			t.Errorf("Resolve(%q) = %q; want %q, where cleaning removes the \"..\" before the link is followed to %q",
				in, got, cleanedFirst, resolvedFirst)
		}
	})

	t.Run("a virtual name is kept", func(t *testing.T) {
		t.Parallel()
		virtual := filepath.Join(base, "notes.md") + "#block-0"
		got, err := Resolve(virtual)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", virtual, err)
		}
		if got != virtual {
			t.Errorf("Resolve(%q) = %q; want it unchanged", virtual, got)
		}
	})
}

// TestResolve_InvalidUTF8UnderAFileIsRefusedAsText holds that the path's
// text is judged before the walk, which would answer ENOTDIR under a regular file.
func TestResolve_InvalidUTF8UnderAFileIsRefusedAsText(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(file + "/\xff"); !errors.Is(err, ErrInvalidUTF8Path) {
		t.Errorf("Resolve = %v, want ErrInvalidUTF8Path", err)
	}
}

// TestResolve_ATrailingSeparatorNamesADirectory holds the resolver to the
// kernel's reading of a trailing separator or "." element: the last element
// must be a directory. A file so named is refused with ENOTDIR; a directory,
// and a path that does not exist yet, resolve without the separator.
func TestResolve_ATrailingSeparatorNamesADirectory(t *testing.T) {
	t.Parallel()
	base := diskSpelling(t, t.TempDir())
	file := filepath.Join(base, "b.json")
	writeEmpty(t, file)
	dir := filepath.Join(base, "sub")
	mkdirAll(t, dir)
	sep := string(filepath.Separator)

	for _, typed := range []string{file + sep, file + sep + sep, file + "/", file + sep + ".", file + sep + "." + sep} {
		if got, err := Resolve(typed); !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("Resolve(%q) = %q, %v; want ENOTDIR", typed, got, err)
		}
	}
	for typed, want := range map[string]string{
		dir + sep:                        dir,
		dir + sep + ".":                  dir,
		filepath.Join(base, "new") + sep: filepath.Join(base, "new"),
		file:                             file,
	} {
		if got, err := Resolve(typed); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, want)
		}
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(base, "to-file")
		symlink(t, file, link)
		if got, err := Resolve(link + sep); !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("Resolve(%q) through a link to a file = %q, %v; want ENOTDIR", link+sep, got, err)
		}

		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		typed := fmt.Sprintf("/dev/fd/%d/", f.Fd())
		if got, err := Resolve(typed); !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("Resolve(%q) for a file's descriptor = %q, %v; want ENOTDIR", typed, got, err)
		}
		d, err := os.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		// darwin's fdesc answers ENOTDIR for a directory's descriptor in
		// directory form, and Linux follows its procfs link to the directory:
		// Resolve answers as the kernel does, so its answer opens the directory.
		typed = fmt.Sprintf("/dev/fd/%d/", d.Fd())
		got, err := Resolve(typed)
		held, serr := os.Stat(dir)
		if serr != nil {
			t.Fatal(serr)
		}
		if _, serr := os.Stat(typed); errors.Is(serr, syscall.ENOTDIR) {
			if !errors.Is(err, syscall.ENOTDIR) {
				t.Errorf("Resolve(%q) for a directory's descriptor the kernel refuses = %q, %v; want ENOTDIR", typed, got, err)
			}
		} else if err != nil || !opensAs(t, got, held) {
			t.Errorf("Resolve(%q) for a directory's descriptor = %q, %v; want a path that opens the directory", typed, got, err)
		}
	}
}

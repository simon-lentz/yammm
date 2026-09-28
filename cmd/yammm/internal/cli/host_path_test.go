package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/snapshot"
)

// The directory components an operator path is generated from: a plain
// directory, a relative and an absolute symbolic link to a directory in
// another branch, "..", ".", a name nothing answers to, and a regular file.
var hostPathComponents = []string{"d", "lr", "la", "..", ".", "m", "f"}

// The file names a written path ends in: absent, a regular file, a relative
// link to a sibling, a relative link whose target climbs out of a linked
// directory ("lr/../e.json"), and an absolute link into the other branch.
var hostPathFiles = []string{"n.json", "e.json", "r.json", "u.json", "a.json"}

// hostPathFixture builds, in a fresh directory, a chain of "d" six deep with
// the start three down, so three ".." stay inside, and a second branch b1/b2/far
// with a "d" chain of its own that the links reach. Each directory of both
// chains, and b1 and b2, holds "f", "e.json" and every link; "m" and "n.json"
// exist nowhere. It returns the root and the start.
func hostPathFixture(t *testing.T) (root, start string) {
	t.Helper()
	root = t.TempDir()
	sep := string(filepath.Separator)
	far := root + sep + "b1" + sep + "b2" + sep + "far"
	var nodes []string
	for _, base := range []string{root, far} {
		p := base
		for range 7 {
			nodes = append(nodes, p)
			p += sep + "d"
		}
	}
	nodes = append(nodes, root+sep+"b1", root+sep+"b1"+sep+"b2")
	for _, n := range nodes {
		if err := os.MkdirAll(n, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range nodes {
		if err := os.WriteFile(n+sep+"f", []byte("file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(n+sep+"e.json", []byte("old "+n+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(n, far)
		if err != nil {
			t.Fatal(err)
		}
		for name, target := range map[string]string{
			"lr":     rel,
			"la":     far,
			"r.json": "e.json",
			"u.json": "lr" + sep + ".." + sep + "e.json",
			"a.json": far + sep + "e.json",
		} {
			if err := os.Symlink(target, n+sep+name); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
	}
	return root, root + sep + "d" + sep + "d" + sep + "d"
}

// hostPathEntry is one entry of a fixture: its kind, and a regular file's
// content or a link's target.
type hostPathEntry struct {
	kind fs.FileMode
	body string
}

// hostPathTree lists every entry under root, relative to it, without
// following links.
func hostPathTree(t *testing.T, root string) map[string]hostPathEntry {
	t.Helper()
	tree := make(map[string]hostPathEntry)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		tree[rel], err = readHostPathEntry(p, d.Type())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// readHostPathEntry reads the entry at p, whose type is kind.
func readHostPathEntry(p string, kind fs.FileMode) (hostPathEntry, error) {
	e := hostPathEntry{kind: kind}
	var err error
	switch {
	case kind&fs.ModeSymlink != 0:
		e.body, err = os.Readlink(p)
	case kind.IsRegular():
		var b []byte
		b, err = os.ReadFile(p)
		e.body = string(b)
	}
	return e, err
}

// hostPathSite is one fixture a test or subtest builds once: its root, the
// prefix a generated spelling is appended to, the root as the resolver spells
// it, and the tree as built. Each case is measured against the pristine tree
// and then restored to it, so a fixture serves every case of its test.
type hostPathSite struct {
	root, prefix, disk string
	pristine           map[string]hostPathEntry
	measured           bool // changes ran and restore has not
}

func newHostPathSite(t *testing.T, place hostPathPlace) *hostPathSite {
	t.Helper()
	root, prefix := place(t)
	return &hostPathSite{root: root, prefix: prefix, disk: diskRoot(t, root), pristine: hostPathTree(t, root)}
}

// changes lists the entries that differ from the pristine tree. It refuses a
// site measured and not restored since: one step's leftover can be exactly
// what the next step should write, and would hide a writer that wrote nothing.
func (s *hostPathSite) changes(t *testing.T) []string {
	t.Helper()
	if err := s.claim(); err != nil {
		t.Fatal(err)
	}
	return hostPathChanges(s.pristine, hostPathTree(t, s.root))
}

// claim marks the site measured, and refuses one measured since its last
// restore.
func (s *hostPathSite) claim() error {
	if s.measured {
		return errors.New("the site was measured and not restored")
	}
	s.measured = true
	return nil
}

// restore undoes changed, as changes listed it: every listed path is removed,
// deepest first, and each the pristine tree holds is made again, shallowest
// first, with the mode the fixture gave it.
func (s *hostPathSite) restore(t *testing.T, changed []string) {
	t.Helper()
	s.measured = false
	paths := make([]string, 0, len(changed))
	for _, c := range changed {
		paths = append(paths, strings.TrimPrefix(c, "-"))
	}
	depth := func(p string) int { return strings.Count(p, string(filepath.Separator)) }
	slices.SortFunc(paths, func(a, b string) int { return depth(b) - depth(a) })
	for _, p := range paths {
		if err := os.RemoveAll(filepath.Join(s.root, p)); err != nil {
			t.Fatalf("restore %s: %v", p, err)
		}
	}
	for _, p := range slices.Backward(paths) {
		e, ok := s.pristine[p]
		if !ok {
			continue
		}
		full := filepath.Join(s.root, p)
		var err error
		switch {
		case e.kind&fs.ModeSymlink != 0:
			err = os.Symlink(e.body, full)
		case e.kind.IsDir():
			err = os.Mkdir(full, 0o750)
		default:
			err = os.WriteFile(full, []byte(e.body), 0o600)
		}
		if err != nil {
			t.Fatalf("restore %s: %v", p, err)
		}
	}
}

// restore returns a site to its pristine tree after every kind of change: a
// file replaced by a rename (with another mode wherever the umask leaves a
// group or other read bit), a file added, directories added with a file in
// them, a file removed, a link replaced by a file, a link pointed elsewhere,
// and a fixture directory removed with everything in it. The restored file's
// and directory's modes are held to ones the fixture made that no step
// touched.
func TestHostPathSite_RestoresEveryKindOfChange(t *testing.T) {
	t.Parallel()
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	site := newHostPathSite(t, absoluteStart)
	d := filepath.Join(site.root, "d")
	for _, step := range []func() error{
		func() error {
			//nolint:gosec // the mode snapshot.WriteFile's staging file takes under a 022 umask, renamed over the file
			return os.WriteFile(filepath.Join(d, "e.json.tmp"), []byte("new\n"), 0o644)
		},
		func() error { return os.Rename(filepath.Join(d, "e.json.tmp"), filepath.Join(d, "e.json")) },
		func() error { return os.WriteFile(filepath.Join(d, "n.json"), []byte("new\n"), 0o600) },
		func() error { return os.MkdirAll(filepath.Join(d, "m", "x"), 0o750) },
		func() error { return os.WriteFile(filepath.Join(d, "m", "x", "Order.csv"), []byte("new\n"), 0o600) },
		func() error { return os.Remove(filepath.Join(d, "f")) },
		func() error { return os.Remove(filepath.Join(d, "r.json")) },
		func() error { return os.WriteFile(filepath.Join(d, "r.json"), []byte("new\n"), 0o600) },
		func() error { return os.Remove(filepath.Join(d, "lr")) },
		func() error { return os.Symlink(".", filepath.Join(d, "lr")) },
		func() error { return os.RemoveAll(filepath.Join(d, "d", "d", "d", "d", "d")) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	changed := site.changes(t)
	if len(changed) != 16 {
		t.Fatalf("changes = %q, want the 16 entries the steps touched: seven, d/lr, and the removed directory with its 7 entries", changed)
	}
	site.restore(t, changed)
	if left := site.changes(t); len(left) != 0 {
		t.Errorf("restore left %q", left)
	}
	fixture, err := os.Stat(filepath.Join(d, "d", "e.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(d, "e.json")); err != nil || info.Mode().Perm() != fixture.Mode().Perm() {
		t.Errorf("restore remade d/e.json with %v (%v), where the fixture made d/d/e.json with %v", info.Mode().Perm(), err, fixture.Mode().Perm())
	}
	fixtureDir, err := os.Stat(filepath.Join(d, "d"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(d, "d", "d", "d", "d", "d")); err != nil || info.Mode().Perm() != fixtureDir.Mode().Perm() {
		t.Errorf("restore remade d/d/d/d/d/d with %v (%v), where the fixture made d/d with %v", info.Mode().Perm(), err, fixtureDir.Mode().Perm())
	}
}

// A site measured and not restored is refused, so no step's leftover reaches
// the next step's comparison; each check helper leaves its site as it found it.
func TestHostPathSite_EveryStepRestoresItsSite(t *testing.T) {
	t.Parallel()
	var bare hostPathSite
	if err := bare.claim(); err != nil {
		t.Fatalf("a fresh site refused its first measure: %v", err)
	}
	if err := bare.claim(); err == nil {
		t.Error("a site measured twice without a restore between was not refused")
	}
	bare.restore(t, nil)
	if err := bare.claim(); err != nil {
		t.Errorf("a restored site refused its next measure: %v", err)
	}
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	site := newHostPathSite(t, absoluteStart)
	sep := string(filepath.Separator)
	var f hostPathFailures
	checkWriteLandsWhereTheLoaderReads(t, &f, WriteFile, site, site.prefix+sep+"m"+sep+"n.json", "/m/n.json")
	checkSetLandsWhereTheLoaderReads(t, &f, site, sep+"m"+sep+"x", "m/x")
	if len(f.list) != 0 {
		t.Fatalf("the checks failed: %q", f.list)
	}
	if left := site.changes(t); len(left) != 0 {
		t.Errorf("the checks left %q", left)
	}
	site.restore(t, nil)
}

// hostPathChanges lists the entries that differ between before and after.
func hostPathChanges(before, after map[string]hostPathEntry) []string {
	var changed []string
	for p, e := range after {
		if b, ok := before[p]; !ok || b != e {
			changed = append(changed, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			changed = append(changed, "-"+p)
		}
	}
	slices.Sort(changed)
	return changed
}

// hostPathSpellings returns every spelling of one to depth components from
// components, with prefix first, each joined by concatenation: filepath.Join
// would clean the ".." away before the code under test saw it.
func hostPathSpellings(prefix string, depth int) []string {
	sep := string(filepath.Separator)
	out := []string{prefix}
	frontier := []string{prefix}
	for range depth - 1 {
		var next []string
		for _, s := range frontier {
			for _, c := range hostPathComponents {
				next = append(next, s+sep+c)
			}
		}
		out = append(out, next...)
		frontier = next
	}
	return out
}

// diskRoot returns root as the resolver spells it, so a resolved path can be
// made relative to it.
func diskRoot(t *testing.T, root string) string {
	t.Helper()
	r, err := location.ResolveHostPath(root)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// hostPathFailures collects a generated test's failures and reports the
// first ten.
type hostPathFailures struct {
	cases int
	list  []string
}

func (f *hostPathFailures) add(spelling, format string, args ...any) {
	f.list = append(f.list, spelling+": "+fmt.Sprintf(format, args...))
}

func (f *hostPathFailures) report(t *testing.T) {
	t.Helper()
	t.Logf("%d spellings, %d failures", f.cases, len(f.list))
	for i, msg := range f.list {
		if i == 10 {
			t.Errorf("... and %d more", len(f.list)-10)
			return
		}
		t.Error(msg)
	}
}

// checkWriteLandsWhereTheLoaderReads writes to typed on site with write and
// holds the outcome to location.ResolveHostPath, the loader's own resolver: the
// write succeeds exactly when the file the resolver names sits in a directory
// that exists, and then that file, and nothing else in the tree, changes. The
// site is restored before it returns.
func checkWriteLandsWhereTheLoaderReads(t *testing.T, f *hostPathFailures, write func(string, []byte) error, site *hostPathSite, typed, shown string) {
	t.Helper()
	f.cases++
	want, rerr := location.ResolveHostPath(typed)
	parentIsDir := false
	if rerr == nil {
		info, err := os.Stat(filepath.Dir(want))
		parentIsDir = err == nil && info.IsDir()
	}
	err := write(typed, []byte(targetPayload))
	changed := site.changes(t)
	defer site.restore(t, changed)
	if !parentIsDir {
		if err == nil {
			f.add(shown, "written, where the loader's resolver names no writable place (%v)", rerr)
		}
		if len(changed) != 0 {
			f.add(shown, "a refused write changed %q", changed)
		}
		return
	}
	if err != nil {
		f.add(shown, "refused (%v), where the loader reads %s", err, want)
		return
	}
	rel, rerr := filepath.Rel(site.disk, want)
	if rerr != nil {
		f.add(shown, "the loader reads %s, outside the fixture", want)
		return
	}
	if !slices.Equal(changed, []string{rel}) {
		f.add(shown, "changed %q, where the loader reads %s", changed, rel)
		return
	}
	if got, _ := os.ReadFile(want); !bytes.Equal(got, []byte(targetPayload)) {
		f.add(shown, "the file the loader reads holds %q", got)
	}
}

// Every file WriteFile writes is the one the loader reads for the same
// spelling, a ".." evaluated on the text first, whatever the directories on
// the way are: plain, a link, missing or a file. Every spelling of up to two
// components is tried before each file name, 285 in all.
func TestWriteFile_LandsWhereTheLoaderReads(t *testing.T) {
	t.Parallel()
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	for _, name := range hostPathFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var f hostPathFailures
			site := newHostPathSite(t, absoluteStart)
			for _, rel := range hostPathSpellings("", 3) {
				typed := site.prefix + rel + string(filepath.Separator) + name
				checkWriteLandsWhereTheLoaderReads(t, &f, WriteFile, site, typed, rel+"/"+name)
			}
			f.report(t)
		})
	}
}

// snapshot.WriteFile, the library's atomic write, lands on the file the loader
// reads for an absolute spelling, as WriteFile does: one rule for a ".." and a
// link in the module. Every spelling of up to two components is tried before
// each file name, 285 in all.
func TestSnapshotWriteFile_LandsWhereTheLoaderReads(t *testing.T) {
	t.Parallel()
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	for _, name := range hostPathFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var f hostPathFailures
			site := newHostPathSite(t, absoluteStart)
			for _, rel := range hostPathSpellings("", 3) {
				typed := site.prefix + rel + string(filepath.Separator) + name
				checkWriteLandsWhereTheLoaderReads(t, &f, snapshot.WriteFile, site, typed, rel+"/"+name)
			}
			f.report(t)
		})
	}
}

// hostPathPlace builds a fixture and returns its root and the prefix a
// generated spelling is appended to: the start, or a relative spelling of it.
type hostPathPlace func(t *testing.T) (root, prefix string)

// absoluteStart spells every path from the fixture's start, absolutely.
func absoluteStart(t *testing.T) (string, string) {
	t.Helper()
	return hostPathFixture(t)
}

// logicalStart enters the fixture's start through a symbolic link two levels
// below the root in the other branch, and spells every path relative to it.
// Two ".." climb to the root from the link and to root/d from the start.
func logicalStart(t *testing.T) (string, string) {
	t.Helper()
	root, start := hostPathFixture(t)
	sep := string(filepath.Separator)
	lw := root + sep + "b1" + sep + "lw"
	if err := os.Symlink(start, lw); err != nil {
		t.Fatal(err)
	}
	t.Chdir(lw)
	return root, "."
}

// Relative spellings, from a working directory entered through a symbolic
// link, land where the loader reads them too: a ".." that climbs out of the
// working directory climbs from its logical spelling, as filepath.Abs and the
// loader do, and not from the directory the link reached. Spellings of up to
// two components are tried before each file name, 285 in all.
func TestWriteFile_RelativeSpellingsLandWhereTheLoaderReads(t *testing.T) {
	if runtimeCannotLink(t) {
		t.Skip("symlinks unavailable")
	}
	sep := string(filepath.Separator)
	var f hostPathFailures
	site := newHostPathSite(t, logicalStart)
	for _, rel := range hostPathSpellings("", 3) {
		for _, name := range hostPathFiles {
			checkWriteLandsWhereTheLoaderReads(t, &f, WriteFile, site, site.prefix+rel+sep+name, "."+rel+"/"+name)
		}
	}
	f.report(t)
}

// runtimeCannotLink reports whether this host refuses to create a symlink, as
// Windows does without developer mode.
func runtimeCannotLink(t *testing.T) bool {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	return os.Symlink(".", probe) != nil
}

// checkStagesBesideTheFileALinkReaches writes through a link whose target
// climbs out of a linked directory and requires the file the kernel reaches to
// be written and staged beside, not in the directory the target's text names,
// which is sealed here so a write staged there fails.
func checkStagesBesideTheFileALinkReaches(t *testing.T, write func(path string) error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(noDirPermissionsOnWindows)
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	sep := string(filepath.Separator)
	dir := unsealedTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "real", "deep", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"link":   filepath.Join("real", "deep", "x"),
		"u.json": "link" + sep + ".." + sep + "t.json",
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	reached := filepath.Join(dir, "real", "deep", "t.json")
	if err := sealDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := write(filepath.Join(dir, "u.json")); err != nil {
		t.Fatalf("write through u.json: %v", err)
	}
	if got := expectPayload(reached); got != "" {
		t.Error(got)
	}
	assertNoDebris(t, filepath.Join(dir, "real", "deep"), reached, "x")
}

func TestWriteFile_StagesBesideTheFileALinkReaches(t *testing.T) {
	t.Parallel()
	checkStagesBesideTheFileALinkReaches(t, func(path string) error { return WriteFile(path, []byte(targetPayload)) })
}

// A link whose target climbs out of a linked directory into /dev/ reaches
// /dev/fd/N, which is written through by its name: /dev/fd/N stats as the regular
// file its descriptor holds, and a rename over it would leave the descriptor
// writing to a file no path reaches. The target's ".." is taken as the kernel
// takes it, from /usr to /, where its text would name dir/dev/fd/N.
func TestWriteFile_ALinkIntoDevThroughALinkedDotDotIsWrittenThrough(t *testing.T) {
	t.Parallel()
	if resolved, err := filepath.EvalSymlinks("/usr"); err != nil || resolved != "/usr" {
		t.Skip("/usr is not a directory directly under /")
	}
	f, spellings := heldDescriptor(t)
	fdPath := spellings[0]
	dir := t.TempDir()
	sep := string(filepath.Separator)
	if err := os.Symlink("/usr", filepath.Join(dir, "u")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	out := filepath.Join(dir, "out")
	if err := os.Symlink("u"+sep+".."+fdPath, out); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(out, []byte(targetPayload)); err != nil {
		t.Fatalf("WriteFile(%s -> u/..%s): %v", out, fdPath, err)
	}
	if got, _ := os.ReadFile(f.Name()); string(got) != earlierOutput+targetPayload {
		t.Errorf("the descriptor's file holds %q, want %q", got, earlierOutput+targetPayload)
	}
}

// descriptorPath takes a descriptor path however the kernel reaches its
// directory: through two "..", the second after a link, each taken on disk;
// through a ".." climbing out of a working directory entered through a link;
// through a link to /dev, which the text never spells; and on Linux through
// every descriptor table procfs holds. Any other path under /dev, and a
// directory named fd anywhere else, is decided by its file.
func TestDescriptorPath_SeesAPathTheKernelTakesIntoDev(t *testing.T) {
	if resolved, err := filepath.EvalSymlinks("/usr"); err != nil || resolved != "/usr" {
		t.Skip("/usr is not a directory directly under /")
	}
	if info, err := os.Stat("/dev"); err != nil || !info.IsDir() {
		t.Skip("this host has no /dev")
	}
	root := t.TempDir()
	sep := string(filepath.Separator)
	if err := os.Mkdir(filepath.Join(root, "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "fd"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"u": "/usr", "devl": "/dev", "lnk": "/usr"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	descriptors := []string{
		root + sep + "a" + sep + ".." + sep + "u" + sep + ".." + sep + "dev" + sep + "stdout",
		root + sep + "a" + sep + ".." + sep + "u" + sep + ".." + sep + "dev" + sep + "fd" + sep + "0",
		root + sep + "devl" + sep + "stderr",
		// On Linux /dev/fd links out of /dev, so the link to /dev reaches /proc/self/fd.
		root + sep + "devl" + sep + "fd" + sep + "0",
		"/dev/fd/12",
	}
	if paths := procDescriptorPaths(t, 0); paths != nil {
		descriptors = append(descriptors, paths...)
		// A link named other than fd, to a descriptor table /dev/fd is not.
		if err := os.Symlink("/proc/thread-self/fd", filepath.Join(root, "tfd")); err != nil {
			t.Fatal(err)
		}
		descriptors = append(descriptors, root+sep+"tfd"+sep+"0")
	}
	for _, p := range descriptors {
		if !descriptorPath(p) {
			t.Errorf("descriptorPath(%q) = false, where the kernel reaches a descriptor path", p)
		}
	}
	for _, p := range []string{
		root + sep + "a" + sep + "stdout",
		"/dev/null",
		"/dev/stdin",
		"/dev/shm/x",
		"/dev/stdout.json",
		"/dev/fd",
		"/dev/fd/1a",
		"/dev/fd/-1",
		root + sep + "devl" + sep + "null",
		root + sep + "devl" + sep + "shm" + sep + "stdout",
		root + sep + "fd" + sep + "3",
		"/proc/self/fdinfo/0",
	} {
		if descriptorPath(p) {
			t.Errorf("descriptorPath(%q) = true for a path its file decides", p)
		}
	}
	t.Chdir(filepath.Join(root, "lnk"))
	if rel := "." + sep + ".." + sep + "dev" + sep + "stdout"; !descriptorPath(rel) {
		t.Errorf("descriptorPath(%q) from %s = false, where the kernel climbs from /usr into /dev", rel, filepath.Join(root, "lnk"))
	}
}

// ReadFile and Open name the path as the operator spelled it, not the path the
// rule evaluated.
func TestReadFile_NamesThePathAsSpelled(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	typed := t.TempDir() + sep + "d" + sep + ".." + sep + "missing.yammm"
	if _, err := ReadFile(typed); err == nil || !strings.Contains(err.Error(), typed) {
		t.Errorf("ReadFile(%q) error %v does not name the path as typed", typed, err)
	}
	if _, err := Open(typed); err == nil || !strings.Contains(err.Error(), typed) {
		t.Errorf("Open(%q) error %v does not name the path as typed", typed, err)
	}
}

package location

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// diskSpelling is [yammmtest.DiskSpelling], which reads directory listings and
// shares no code with resolveHostPath, failing the test on error.
func diskSpelling(t *testing.T, p string) string {
	t.Helper()
	spelled, err := yammmtest.DiskSpelling(p)
	if err != nil {
		t.Fatal(err)
	}
	return spelled
}

// identityForm writes a host path in the form an identity takes.
func identityForm(host string) string {
	return norm.NFC.String(filepath.ToSlash(host))
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

// TestNewCanonicalPath_OneIdentityForTwoSpellingsOfOneFile holds a file-backed
// identity to the file it names: where the filesystem finds one file by two
// spellings, both spellings give one identity, and it is the one the
// filesystem itself uses.
func TestNewCanonicalPath_OneIdentityForTwoSpellingsOfOneFile(t *testing.T) {
	t.Parallel()

	created, typed := hostPathTree(t)

	fromCreated, err := NewCanonicalPath(created)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q): %v", created, err)
	}
	fromTyped, err := NewCanonicalPath(typed)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q): %v", typed, err)
	}
	if fromTyped != fromCreated {
		t.Errorf("two spellings of one file give two identities:\n  created %q\n  typed   %q",
			fromCreated.String(), fromTyped.String())
	}
	if want := identityForm(created); fromTyped.String() != want {
		t.Errorf("the identity is %q; want the filesystem's own spelling %q", fromTyped.String(), want)
	}
}

// TestConstructors_AgreeOnTwoSpellingsOfOneFile holds every constructor that
// touches the filesystem to the spelling [yammmtest.DiskSpelling] reads from
// the directory listings for the typed path. That reading shares no code with
// resolveHostPath, so a resolver that spells a component wrongly fails here.
func TestConstructors_AgreeOnTwoSpellingsOfOneFile(t *testing.T) {
	t.Parallel()

	_, typed := hostPathTree(t)
	onDisk := diskSpelling(t, typed)
	want := identityForm(onDisk)

	resolved, err := ResolveHostPath(typed)
	if err != nil {
		t.Fatalf("ResolveHostPath(%q): %v", typed, err)
	}
	if resolved != onDisk {
		t.Errorf("ResolveHostPath(%q) = %q; want %q", typed, resolved, onDisk)
	}

	id, err := SourceIDFromPath(typed)
	if err != nil {
		t.Fatalf("SourceIDFromPath(%q): %v", typed, err)
	}
	if id.String() != want {
		t.Errorf("SourceIDFromPath(%q) = %q; want %q", typed, id.String(), want)
	}

	key, err := CanonicalizePathForSourceID(typed)
	if err != nil {
		t.Fatalf("CanonicalizePathForSourceID(%q): %v", typed, err)
	}
	if key != want {
		t.Errorf("CanonicalizePathForSourceID(%q) = %q; want %q", typed, key, want)
	}
}

// TestNewCanonicalPath_IdentityDoesNotChangeWhenTheFileIsCreated holds an
// identity minted for a path that does not exist yet to the one the file gets
// once it is written. A load that names a file before it exists must not
// re-key it afterwards.
func TestNewCanonicalPath_IdentityDoesNotChangeWhenTheFileIsCreated(t *testing.T) {
	t.Parallel()

	// t.TempDir is reached through a symlink on darwin (/var -> /private/var),
	// which is what makes the two mints differ when only the second resolves.
	p := filepath.Join(t.TempDir(), "later.yammm")

	before, err := NewCanonicalPath(p)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q) before creation: %v", p, err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := NewCanonicalPath(p)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q) after creation: %v", p, err)
	}
	if before != after {
		t.Errorf("the identity changed when the file was created:\n  before %q\n  after  %q",
			before.String(), after.String())
	}
}

// holdLinkIdentity holds the path typed, which reaches a file through a link,
// to the file the host opens through it: content written at the resolved path
// is what a read of typed returns, and the resolved path is the on-disk
// spelling, before and after the write.
func holdLinkIdentity(t *testing.T, typed string, content []byte) {
	t.Helper()
	before, err := ResolveHostPath(typed)
	if err != nil {
		t.Fatalf("ResolveHostPath(%q) before the write: %v", typed, err)
	}
	if err := os.WriteFile(before, content, 0o600); err != nil {
		t.Fatalf("write the resolved path %q: %v", before, err)
	}
	if got, err := os.ReadFile(typed); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("reading %q returns %q (err %v), not the file written at its resolved path %q: the host reaches another file through the link",
			typed, got, err, before)
	}
	after, err := ResolveHostPath(typed)
	if err != nil {
		t.Fatalf("ResolveHostPath(%q) after the write: %v", typed, err)
	}
	if onDisk := diskSpelling(t, typed); after != before || onDisk != before {
		t.Errorf("ResolveHostPath(%q):\n  before the write   %q\n  after              %q\n  on-disk spelling   %q",
			typed, before, after, onDisk)
	}
}

// TestResolveHostPath_DanglingLinkKeepsItsIdentityWhenItsTargetIsCreated holds a
// dangling link's identity to the file the host reaches through it: a file
// written at the path resolved before the target exists is the file a read
// through the link returns, and resolving again gives that path. A ".." in a
// target tells a parent taken on disk, as Unix takes it, from one taken from
// the text, as Windows takes it; the read through the link is the judge.
func TestResolveHostPath_DanglingLinkKeepsItsIdentityWhenItsTargetIsCreated(t *testing.T) {
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

// TestResolveHostPath_LinkNamesTheFileTheHostReaches holds a link whose target
// names a symlinked directory before a ".." to the file the host opens through
// it. Unix follows that directory and takes its parent on disk; Windows
// evaluates the ".." on the target's text and never follows it. A file at each
// answer means a resolver with the other host's rule names a file the host
// does not open.
func TestResolveHostPath_LinkNamesTheFileTheHostReaches(t *testing.T) {
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

// TestResolveHostPath_SpellsALongPathInFull holds the resolver to the whole
// answer for a path longer than a short buffer holds, up to the longest path
// that PATH_MAX, 1024 bytes with its NUL on darwin, leaves room for. Each path
// resolves in a child run of this test.
func TestResolveHostPath_SpellsALongPathInFull(t *testing.T) {
	t.Parallel()
	if typed, ok := os.LookupEnv(childResolveEnv); ok {
		want := os.Getenv(childWantEnv)
		if got, err := ResolveHostPath(typed); err != nil || got != want {
			t.Errorf("ResolveHostPath(%q) = %q (%d bytes), %v; want %q (%d bytes)", typed, got, len(got), err, want, len(want))
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
			resolveInChild(t, "TestResolveHostPath_SpellsALongPathInFull", typed, want)
		})
	}
}

// TestResolveHostPath_RefusesAPathWhoseResolvedFormExceedsPathMax pins a
// refusal on darwin. stat follows links with no bound on the length of the path
// they reach, but realpath(3) cannot write an answer of PATH_MAX bytes or more.
// Such a path exists and has no spelling, so every door refuses it rather than
// return part of one.
func TestResolveHostPath_RefusesAPathWhoseResolvedFormExceedsPathMax(t *testing.T) {
	t.Parallel()
	if typed, ok := os.LookupEnv(childResolveEnv); ok {
		if got, err := ResolveHostPath(typed); !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Errorf("ResolveHostPath(%q) = %q, %v; want ENAMETOOLONG", typed, got, err)
		}
		if got, err := NewCanonicalPath(typed); !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Errorf("NewCanonicalPath(%q) = %q, %v; want ENAMETOOLONG", typed, got.String(), err)
		}
		if got, err := CanonicalizePathForSourceID(typed); !errors.Is(err, syscall.ENAMETOOLONG) {
			t.Errorf("CanonicalizePathForSourceID(%q) = %q, %v; want ENAMETOOLONG", typed, got, err)
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
	resolveInChild(t, "TestResolveHostPath_RefusesAPathWhoseResolvedFormExceedsPathMax", typed, "")
}

// TestCanonicalize_RefusesAnEmptyPath holds every file-backed door to refusing
// an unset file name, where filepath.Abs would make it the working directory.
func TestCanonicalize_RefusesAnEmptyPath(t *testing.T) {
	t.Parallel()

	t.Run("ResolveHostPath", func(t *testing.T) {
		t.Parallel()
		if _, err := ResolveHostPath(""); !errors.Is(err, ErrEmptyPath) {
			t.Errorf("ResolveHostPath(\"\") error = %v; want ErrEmptyPath", err)
		}
	})
	t.Run("NewCanonicalPath", func(t *testing.T) {
		t.Parallel()
		if _, err := NewCanonicalPath(""); !errors.Is(err, ErrEmptyPath) {
			t.Errorf("NewCanonicalPath(\"\") error = %v; want ErrEmptyPath", err)
		}
	})
	t.Run("SourceIDFromPath", func(t *testing.T) {
		t.Parallel()
		if _, err := SourceIDFromPath(""); !errors.Is(err, ErrEmptyPath) {
			t.Errorf("SourceIDFromPath(\"\") error = %v; want ErrEmptyPath", err)
		}
	})
	t.Run("CanonicalizePathForSourceID", func(t *testing.T) {
		t.Parallel()
		if _, err := CanonicalizePathForSourceID(""); !errors.Is(err, ErrEmptyPath) {
			t.Errorf("CanonicalizePathForSourceID(\"\") error = %v; want ErrEmptyPath", err)
		}
	})
}

// TestCanonicalize_RefusesAPathThatIsNotValidUTF8 holds every identity to text
// both JSON wires can carry. The rows run on every host: the input is a string,
// and no file is opened.
func TestCanonicalize_RefusesAPathThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()

	bad := filepath.Join(t.TempDir(), "caf\xff.yammm")

	t.Run("ResolveHostPath", func(t *testing.T) {
		t.Parallel()
		if _, err := ResolveHostPath(bad); !errors.Is(err, ErrInvalidUTF8Path) {
			t.Errorf("ResolveHostPath(%q) error = %v; want ErrInvalidUTF8Path", bad, err)
		}
	})
	t.Run("SourceIDFromPath", func(t *testing.T) {
		t.Parallel()
		if _, err := SourceIDFromPath(bad); !errors.Is(err, ErrInvalidUTF8Path) {
			t.Errorf("SourceIDFromPath(%q) error = %v; want ErrInvalidUTF8Path", bad, err)
		}
	})
	t.Run("NewCanonicalPath", func(t *testing.T) {
		t.Parallel()
		if _, err := NewCanonicalPath(bad); !errors.Is(err, ErrInvalidUTF8Path) {
			t.Errorf("NewCanonicalPath(%q) error = %v; want ErrInvalidUTF8Path", bad, err)
		}
	})
	t.Run("ValidateSyntheticSourceID", func(t *testing.T) {
		t.Parallel()
		if err := ValidateSyntheticSourceID("inline:caf\xff"); !errors.Is(err, ErrInvalidUTF8Path) {
			t.Errorf("ValidateSyntheticSourceID error = %v; want ErrInvalidUTF8Path", err)
		}
	})
}

// TestResolveHostPath_RefusesAPathUnderARegularFile pins a refusal: a path can
// never exist under a regular file, and every door says so rather than keeping
// the path as typed.
func TestResolveHostPath_RefusesAPathUnderARegularFile(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "regular.yammm")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(file, "child.yammm")

	if _, err := ResolveHostPath(under); err == nil {
		t.Errorf("ResolveHostPath(%q) = nil error; want a refusal", under)
	}
	if _, err := NewCanonicalPath(under); err == nil {
		t.Errorf("NewCanonicalPath(%q) = nil error; want a refusal", under)
	}
}

// TestResolveHostPath_SpellsAPathTheProcessCannotRead holds the resolver to
// looking a path up without reading it. A file with no permission bits and a
// directory the process can traverse but not list are spelled on disk, and
// CanonicalizePathForSourceID, which requires the path to exist, accepts them.
func TestResolveHostPath_SpellsAPathTheProcessCannotRead(t *testing.T) {
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
			if got, err := ResolveHostPath(typed); err != nil || got != row.created {
				t.Errorf("ResolveHostPath(%q) = %q, %v; want %q", typed, got, err, row.created)
			}
			if got, err := CanonicalizePathForSourceID(typed); err != nil || got != identityForm(row.created) {
				t.Errorf("CanonicalizePathForSourceID(%q) = %q, %v; want %q", typed, got, err, identityForm(row.created))
			}
		})
	}
}

// TestResolveHostPath_KeepsWhatTheLSPDependsOn pins the three rules the editor
// depends on: a relative path is made absolute from the working directory as
// the filesystem spells it, cleaning runs before resolution so a ".." across a
// symlink lands where the loader lands it, and a name no file answers to is
// kept as typed.
func TestResolveHostPath_KeepsWhatTheLSPDependsOn(t *testing.T) {
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
		got, err := ResolveHostPath(rel)
		if err != nil {
			t.Fatalf("ResolveHostPath(%q): %v", rel, err)
		}
		// os.Getwd keeps a symlinked or differently cased $PWD; the resolver
		// spells the working directory, the deepest part that exists, on disk.
		if want := filepath.Join(diskSpelling(t, cwd), rel); got != want {
			t.Errorf("ResolveHostPath(%q) = %q; want %q", rel, got, want)
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
		got, err := ResolveHostPath(in)
		if err != nil {
			t.Fatalf("ResolveHostPath(%q): %v", in, err)
		}
		if got != cleanedFirst {
			t.Errorf("ResolveHostPath(%q) = %q; want %q, where cleaning removes the \"..\" before the link is followed to %q",
				in, got, cleanedFirst, resolvedFirst)
		}
	})

	t.Run("a virtual name is kept", func(t *testing.T) {
		t.Parallel()
		virtual := filepath.Join(base, "notes.md") + "#block-0"
		got, err := ResolveHostPath(virtual)
		if err != nil {
			t.Fatalf("ResolveHostPath(%q): %v", virtual, err)
		}
		if got != virtual {
			t.Errorf("ResolveHostPath(%q) = %q; want it unchanged", virtual, got)
		}
	})
}

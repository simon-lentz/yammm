package location

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// caseFoldingFilesystem reports whether dir's filesystem finds a file by
// another spelling of its name. The rows below it ask what an identity does
// when two spellings name one file, which a case-sensitive filesystem cannot
// produce.
func caseFoldingFilesystem(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(probe) })
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// hostPathTree writes root/Proj/Sub/File.yammm and returns the created path
// and the same file typed in lower case.
func hostPathTree(t *testing.T) (created, typed string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !caseFoldingFilesystem(t, root) {
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
	if want := filepath.ToSlash(created); fromTyped.String() != want {
		t.Errorf("the identity is %q; want the filesystem's own spelling %q", fromTyped.String(), want)
	}
}

// TestConstructors_AgreeOnTwoSpellingsOfOneFile is the second implementation of
// that rule (A-229 rule 1): the constructors that touch the filesystem answer
// it alike, so the property is not one function's behaviour.
func TestConstructors_AgreeOnTwoSpellingsOfOneFile(t *testing.T) {
	t.Parallel()

	created, typed := hostPathTree(t)

	want := filepath.ToSlash(created)
	resolved, err := ResolveHostPath(typed)
	if err != nil {
		t.Fatalf("ResolveHostPath(%q): %v", typed, err)
	}
	if got := filepath.ToSlash(resolved); got != want {
		t.Errorf("ResolveHostPath(%q) = %q; want %q", typed, got, want)
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
	t.Run("SourceIDFromAbsolutePath", func(t *testing.T) {
		t.Parallel()
		if _, err := SourceIDFromAbsolutePath(bad); !errors.Is(err, ErrInvalidUTF8Path) {
			t.Errorf("SourceIDFromAbsolutePath(%q) error = %v; want ErrInvalidUTF8Path", bad, err)
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

// TestResolveHostPath_RefusesAPathUnderARegularFile pins the one refusal that
// held before this group: a path can never exist under a regular file, and
// every door says so rather than keeping the path as typed.
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

// TestResolveHostPath_KeepsWhatTheLSPDependsOn carries the three rows the
// editor's own canonicalizer held before this group folded it into the
// resolver: a path is made absolute before anything else, cleaning runs before
// resolution so a ".." across a symlink lands where the loader lands it, and a
// name that no file answers to is kept as typed.
func TestResolveHostPath_KeepsWhatTheLSPDependsOn(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
		if want := filepath.Join(cwd, rel); got != want {
			t.Errorf("ResolveHostPath(%q) = %q; want %q", rel, got, want)
		}
	})

	t.Run("cleaning runs before resolution", func(t *testing.T) {
		t.Parallel()
		outerDir := filepath.Join(base, "outer")
		innerDir := filepath.Join(outerDir, "inner")
		if err := os.MkdirAll(innerDir, 0o750); err != nil {
			t.Fatal(err)
		}
		outer := filepath.Join(outerDir, "target.yammm")
		if err := os.WriteFile(outer, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(outerDir, "link")
		if err := os.Symlink(innerDir, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		// Built by concatenation, not filepath.Join: Join cleans, which would
		// collapse the ".." before the resolver ever saw it.
		in := link + sep + ".." + sep + "target.yammm"
		got, err := ResolveHostPath(in)
		if err != nil {
			t.Fatalf("ResolveHostPath(%q): %v", in, err)
		}
		if got != outer {
			t.Errorf("ResolveHostPath(%q) = %q; want %q — the link's real parent absorbed the \"..\"", in, got, outer)
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

package location

import (
	"path/filepath"
	"testing"
)

func TestSourceID_Rel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, id, dir, want string
		ok                  bool
	}{
		{"under", "/r/a/b.yammm", "/r", "a/b.yammm", true},
		{"sibling", "/r/x/e.yammm", "/r/y", "../x/e.yammm", true},
		{"above", "/e.yammm", "/r/s", "../../e.yammm", true},
		{"root of all", "/a.yammm", "/", "a.yammm", true},
		{"equal", "/r/a", "/r/a", "", false},
		{"an ancestor of dir", "/r", "/r/a", "", false},
		{"segment prefix is not a parent", "/rr/a.yammm", "/r", "../rr/a.yammm", true},
		{"drive", "C:/r/a.yammm", "C:/s", "../r/a.yammm", true},
		{"drive root", "C:/a.yammm", "C:/", "a.yammm", true},
		{"other drive", "D:/r/a.yammm", "C:/r", "", false},
		{"share", "//srv/sh/a/b.yammm", "//srv/sh/c", "../a/b.yammm", true},
		{"share root", "//srv/sh/a.yammm", "//srv/sh/", "a.yammm", true},
		{"other share", "//srv/other/a.yammm", "//srv/sh", "", false},
		{"other share below its root", "//srv/other/a.yammm", "//srv/sh/c", "", false},
		{"other server", "//other/sh/a.yammm", "//srv/sh", "", false},
		{"share against local", "//srv/sh/a.yammm", "/srv/sh", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := SourceID{cp: CanonicalPath{path: tc.id}}
			got, ok := id.Rel(CanonicalPath{path: tc.dir})
			if got != tc.want || ok != tc.ok {
				t.Errorf("Rel = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
	for name, id := range map[string]SourceID{
		"a synthetic source": MustNewSourceID("embedded://app/a.yammm"),
		"a zero source":      {},
	} {
		if got, ok := id.Rel(CanonicalPath{path: "/r"}); ok {
			t.Errorf("%s has a relative path %q", name, got)
		}
	}
	if got, ok := (SourceID{cp: CanonicalPath{path: "/r/a.yammm"}}).Rel(CanonicalPath{}); ok {
		t.Errorf("a zero dir gives a relative path %q", got)
	}
}

// TestSourceID_Rel_AgreesWithRelativeTo holds Rel to RelativeTo, the other
// implementation of "s past dir", on identities minted from real paths: where
// RelativeTo answers, Rel gives the same answer, and it climbs where RelativeTo
// refuses.
func TestSourceID_Rel_AgreesWithRelativeTo(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := func(parts ...string) CanonicalPath {
		t.Helper()
		cp, err := NewCanonicalPath(filepath.Join(append([]string{base}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return cp
	}
	file := func(parts ...string) SourceID {
		t.Helper()
		id, err := SourceIDFromPath(filepath.Join(append([]string{base}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	fsRoot, err := NewCanonicalPath(filepath.VolumeName(base) + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		src  SourceID
		dir  CanonicalPath
		want string
	}{
		{"under", file("x", "a.yammm"), dir(), "x/a.yammm"},
		{"under a decomposed name", file("cafe\u0301", "a.yammm"), dir("cafe\u0301"), "a.yammm"},
		{"a decomposed sibling", file("cafe\u0301", "a.yammm"), dir("lib"), "../caf\u00e9/a.yammm"},
		{"above", file("a.yammm"), dir("s", "t"), "../../a.yammm"},
		{"the filesystem root", file("a.yammm"), fsRoot, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.src.Rel(tc.dir)
			if !ok {
				t.Fatalf("%q.Rel(%q) reported no relative path", tc.src, tc.dir)
			}
			if under, ok := tc.src.RelativeTo(tc.dir); ok {
				if got != under {
					t.Errorf("Rel = %q, RelativeTo = %q", got, under)
				}
				return
			}
			if got != tc.want {
				t.Errorf("Rel = %q, want %q", got, tc.want)
			}
		})
	}
}

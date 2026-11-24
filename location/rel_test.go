package location

import (
	"path/filepath"
	"strings"
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
			id := SourceID{cp: canonicalPath{path: tc.id}}
			got, ok := id.Rel(SourceID{cp: canonicalPath{path: tc.dir}})
			if got != tc.want || ok != tc.ok {
				t.Errorf("Rel = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
	for name, id := range map[string]SourceID{
		"a synthetic source": {synthetic: "embedded://app/a.yammm"},
		"a zero source":      {},
	} {
		if got, ok := id.Rel(SourceID{cp: canonicalPath{path: "/r"}}); ok {
			t.Errorf("%s has a relative path %q", name, got)
		}
	}
	file := SourceID{cp: canonicalPath{path: "/r/a.yammm"}}
	for name, base := range map[string]SourceID{
		"a synthetic base": {synthetic: "embedded://app"},
		"a zero base":      {},
	} {
		if got, ok := file.Rel(base); ok {
			t.Errorf("%s gives a relative path %q", name, got)
		}
	}
}

// TestSourceID_Rel_OverResolvedPaths holds Rel to identities minted from real
// paths: under the base it writes the path past the base, a decomposed name in
// NFC, and elsewhere it climbs.
// TestSourceID_Rel_AllocatesOnlyToClimb holds Rel's cost on the renderer's
// path, one call per located issue: a source under the base is a slice of the
// source's own text, and a source beside it allocates its one result.
func TestSourceID_Rel_AllocatesOnlyToClimb(t *testing.T) {
	under := SourceID{cp: canonicalPath{path: "/r/a/b/c.yammm"}}
	beside := SourceID{cp: canonicalPath{path: "/s/a/b/c.yammm"}}
	base := SourceID{cp: canonicalPath{path: "/r"}}
	for _, c := range []struct {
		name string
		id   SourceID
		want float64
	}{
		{"under the base", under, 0},
		{"beside the base", beside, 1},
	} {
		if got := testing.AllocsPerRun(100, func() { _, _ = c.id.Rel(base) }); got != c.want {
			t.Errorf("%s: %v allocations, want %v", c.name, got, c.want)
		}
	}
}

func TestSourceID_Rel_OverResolvedPaths(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := func(parts ...string) SourceID {
		t.Helper()
		id, err := SourceIDFromPath(filepath.Join(append([]string{base}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	fsRoot, err := SourceIDFromPath(filepath.VolumeName(base) + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		src  SourceID
		base SourceID
		want string
		ok   bool
	}{
		{"under", id("x", "a.yammm"), id(), "x/a.yammm", true},
		{"two directories down", id("x", "y", "a.yammm"), id(), "x/y/a.yammm", true},
		{"under a decomposed name", id("cafe\u0301", "a.yammm"), id("cafe\u0301"), "a.yammm", true},
		{"a decomposed sibling", id("cafe\u0301", "a.yammm"), id("lib"), "../caf\u00e9/a.yammm", true},
		{"a sibling directory that shares the base's prefix", id("pq", "a.yammm"), id("p"), "../pq/a.yammm", true},
		{"above", id("a.yammm"), id("s", "t"), "../../a.yammm", true},
		{"the filesystem root holds every absolute path", id("a.yammm"), fsRoot, strings.TrimPrefix(id("a.yammm").String(), fsRoot.String()), true},
		{"the base itself", id(), id(), "", false},
		{"the filesystem root itself", fsRoot, fsRoot, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.src.Rel(tc.base)
			if got != tc.want || ok != tc.ok {
				t.Errorf("%q.Rel(%q) = %q, %v; want %q, %v", tc.src, tc.base, got, ok, tc.want, tc.ok)
			}
		})
	}
}

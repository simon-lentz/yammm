package location

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceID_RelativeTo(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustRoot := func(p string) CanonicalPath {
		t.Helper()
		cp, err := NewCanonicalPath(p)
		if err != nil {
			t.Fatal(err)
		}
		return cp
	}
	file := func(parts ...string) SourceID {
		t.Helper()
		id, err := SourceIDFromAbsolutePath(filepath.Join(append([]string{base}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	root := mustRoot(base)
	fsRoot := mustRoot(filepath.VolumeName(base) + string(filepath.Separator))

	tests := []struct {
		name   string
		src    SourceID
		root   CanonicalPath
		want   string
		wantOK bool
	}{
		{"a file under the root", file("a.yammm"), root, "a.yammm", true},
		{"a file two directories down", file("x", "y", "a.yammm"), root, "x/y/a.yammm", true},
		{"the filesystem root holds every absolute path", file("a.yammm"), fsRoot, strings.TrimPrefix(file("a.yammm").String(), fsRoot.String()), true},
		{"a root with a decomposed name", file("cafe\u0301", "a.yammm"), mustRoot(filepath.Join(base, "cafe\u0301")), "a.yammm", true},
		{"the root itself", SourceID{cp: root}, root, "", false},
		{"the filesystem root itself", SourceID{cp: fsRoot}, fsRoot, "", false},
		{"a sibling directory that shares the root's prefix", file("pq", "a.yammm"), mustRoot(filepath.Join(base, "p")), "", false},
		{"a source above the root", file("a.yammm"), mustRoot(filepath.Join(base, "sub")), "", false},
		{"a synthetic source", MustNewSourceID("test://a.yammm"), root, "", false},
		{"a zero root", file("a.yammm"), CanonicalPath{}, "", false},
		{"a zero source", SourceID{}, root, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tt.src.RelativeTo(tt.root)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("%q.RelativeTo(%q) = %q, %v; want %q, %v", tt.src, tt.root, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

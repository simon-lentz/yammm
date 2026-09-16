package yammmtest

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHostAbs_IsAbsoluteAndKeepsThePath(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"/project", "/foo/bar baz.yammm", "/a/b/c/d.yammm"} {
		got := HostAbs(p)
		if !filepath.IsAbs(got) {
			t.Errorf("HostAbs(%q) = %q, which this host does not read as absolute", p, got)
		}
		if rest := filepath.ToSlash(strings.TrimPrefix(got, filepath.VolumeName(got))); rest != p {
			t.Errorf("HostAbs(%q) = %q, whose path after the volume is %q", p, got, rest)
		}
	}
}

// The URI is compared as a string because callers compare it with
// lsputil.PathToURI's output, which spells the empty authority "file:///".
func TestFileURI_NamesHostAbs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		uriPath     string
		want        string
		wantWindows string
	}{
		{"/foo/bar.yammm", "file:///foo/bar.yammm", "file:///C:/foo/bar.yammm"},
		{"/foo/bar%20baz.yammm", "file:///foo/bar%20baz.yammm", "file:///C:/foo/bar%20baz.yammm"},
	}
	for _, tt := range tests {
		want := tt.want
		if runtime.GOOS == "windows" {
			want = tt.wantWindows
		}
		if got := FileURI(tt.uriPath); got != want {
			t.Errorf("FileURI(%q) = %q, want %q", tt.uriPath, got, want)
		}
	}
}

func TestHostAbsAndFileURI_PanicOnARelativePath(t *testing.T) {
	t.Parallel()
	helpers := map[string]func(string) string{"HostAbs": HostAbs, "FileURI": FileURI}
	for name, helper := range helpers {
		for _, p := range []string{"", "project/x.yammm", `C:\project\x.yammm`} {
			if !panics(func() { helper(p) }) {
				t.Errorf("%s(%q) returned without a panic", name, p)
			}
		}
	}
}

func panics(f func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	f()
	return false
}

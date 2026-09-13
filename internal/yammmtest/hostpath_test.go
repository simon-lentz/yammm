package yammmtest

import (
	"net/url"
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

func TestFileURI_NamesHostAbs(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"/foo/bar.yammm", "/foo/bar%20baz.yammm"} {
		u, err := url.Parse(FileURI(p))
		if err != nil {
			t.Fatalf("FileURI(%q) = %q does not parse: %v", p, FileURI(p), err)
		}
		decoded, err := url.PathUnescape(p)
		if err != nil {
			t.Fatalf("unescape %q: %v", p, err)
		}
		want := filepath.ToSlash(HostAbs(decoded))
		if runtime.GOOS == "windows" {
			want = "/" + want
		}
		if u.Scheme != "file" || u.Host != "" || u.Path != want {
			t.Errorf("FileURI(%q) = %q, want scheme file, no host, path %q", p, FileURI(p), want)
		}
	}
}

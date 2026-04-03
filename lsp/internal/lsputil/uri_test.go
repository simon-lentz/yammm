package lsputil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsMarkdownURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		want bool
	}{
		{"md extension", "file:///path/to/file.md", true},
		{"markdown extension", "file:///path/to/file.markdown", true},
		{"uppercase MD", "file:///path/to/file.MD", true},
		{"yammm file", "file:///path/to/file.yammm", false},
		{"txt file", "file:///path/to/file.txt", false},
		{"invalid URI", "not-a-uri", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsMarkdownURI(tt.uri))
		})
	}
}

func TestIsYammmURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		want bool
	}{
		{"yammm extension", "file:///path/to/file.yammm", true},
		{"uppercase YAMMM", "file:///path/to/file.YAMMM", true},
		{"md file", "file:///path/to/file.md", false},
		{"invalid URI", "not-a-uri", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsYammmURI(tt.uri))
		})
	}
}

func TestURIToPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{"simple", "file:///foo/bar.yammm", "/foo/bar.yammm", false},
		{"percent encoded space", "file:///foo/bar%20baz.yammm", "/foo/bar baz.yammm", false},
		{"nested path", "file:///a/b/c/d.yammm", "/a/b/c/d.yammm", false},
		{"non-file scheme", "http://example.com/foo", "", true},
		{"no scheme", "/foo/bar.yammm", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := URIToPath(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestPathToURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple", "/foo/bar.yammm", "file:///foo/bar.yammm"},
		{"with space", "/foo/bar baz.yammm", "file:///foo/bar%20baz.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := PathToURI(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasURIScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"file scheme", "file:///foo", true},
		{"http scheme", "http://example.com", true},
		{"no scheme", "/foo/bar", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HasURIScheme(tt.s))
		})
	}
}

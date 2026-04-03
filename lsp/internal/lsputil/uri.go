// Package lsputil provides URI/path utilities and position conversion for LSP.
package lsputil

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// PositionEncoding represents the position encoding used for LSP communication.
// LSP 3.17 introduced position encoding negotiation; prior versions assumed UTF-16.
type PositionEncoding string

const (
	// PositionEncodingUTF16 counts positions in UTF-16 code units.
	// This is the default for LSP compatibility: VS Code and most editors
	// use UTF-16 internally (JavaScript strings), and LSP < 3.17 mandates it.
	// Multi-byte Unicode characters (e.g., emoji, CJK) require conversion
	// from YAMMM's internal rune-based Column to UTF-16 code units.
	PositionEncodingUTF16 PositionEncoding = "utf-16"

	// PositionEncodingUTF8 counts positions in UTF-8 bytes.
	// Some newer editors (e.g., Neovim with LSP 3.17) prefer this encoding
	// as it avoids UTF-16 surrogate pair complexity. When negotiated,
	// positions map directly to byte offsets within lines.
	PositionEncodingUTF8 PositionEncoding = "utf-8"
)

// URIToPath converts a file:// URI to a filesystem path.
//
// On POSIX systems: file:///path/to/file -> /path/to/file
// On Windows: file:///C:/path/to/file -> C:\path\to\file
//
// UNC paths are not currently supported on Windows.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse URI %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %s", uri)
	}

	path := u.Path

	// Windows: file:///C:/path -> C:\path
	if runtime.GOOS == "windows" {
		// Remove leading slash before drive letter: /C:/foo -> C:/foo
		if len(path) >= 3 && path[0] == '/' && isWindowsDriveLetter(path[1]) && path[2] == ':' {
			path = path[1:]
		}
		// Convert forward slashes to backslashes
		path = filepath.FromSlash(path)
	}

	return path, nil
}

// PathToURI converts a filesystem path to a file:// URI.
//
// On POSIX systems: /path/to/file -> file:///path/to/file
// On Windows: C:\path\to\file -> file:///C:/path/to/file
//
// UNC paths are not currently supported on Windows.
func PathToURI(path string) string {
	// Ensure absolute path. filepath.Abs can only fail if os.Getwd() fails
	// (e.g., deleted CWD). In that case, producing a relative-path URI is
	// the best we can do — callers cannot handle an error from this utility.
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err == nil {
			path = absPath
		}
	}

	// Normalize to forward slashes for URI
	path = filepath.ToSlash(path)

	// Windows: C:/path -> /C:/path (add leading slash for URI format)
	if runtime.GOOS == "windows" && len(path) >= 2 && isWindowsDriveLetter(path[0]) && path[1] == ':' {
		path = "/" + path
	}

	// Use url.URL to properly escape the path
	u := url.URL{
		Scheme: "file",
		Path:   path,
	}
	return u.String()
}

// isWindowsDriveLetter reports whether c is a valid Windows drive letter (A-Z, a-z).
func isWindowsDriveLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// HasURIScheme reports whether s appears to have a URI scheme prefix.
// It checks for the common "scheme://" pattern used by hierarchical URIs
// like file:// and http://. This is used to avoid double-encoding URIs
// that already have a scheme.
//
// The scheme is validated per RFC3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
// This correctly identifies:
//   - "file:///path" -> true (has scheme)
//   - "http://example.com" -> true (has scheme)
//   - "custom-scheme://host/path" -> true (long scheme is valid)
//   - "/path/to/file" -> false (Unix filesystem path, no "://")
//   - "C:\path\file" -> false (Windows path, no "://")
func HasURIScheme(s string) bool {
	idx := strings.Index(s, "://")
	if idx <= 0 {
		return false
	}
	scheme := s[:idx]
	// RFC3986: scheme must start with ALPHA
	if !isSchemeAlpha(scheme[0]) {
		return false
	}
	// RFC3986: subsequent chars can be ALPHA / DIGIT / "+" / "-" / "."
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if !isSchemeAlpha(c) && !isSchemeDigit(c) && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// isSchemeAlpha reports whether c is an ASCII letter (RFC3986 ALPHA).
func isSchemeAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isSchemeDigit reports whether c is an ASCII digit (RFC3986 DIGIT).
func isSchemeDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// IsMarkdownURI returns true if the URI refers to a markdown file (.md or .markdown).
// Detection uses filepath.Ext on the filesystem path (not raw URI suffix) to avoid
// false positives from query strings or fragments.
func IsMarkdownURI(uri string) bool {
	path, err := URIToPath(uri)
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}

// IsYammmURI returns true if the URI refers to a yammm file (.yammm).
func IsYammmURI(uri string) bool {
	path, err := URIToPath(uri)
	if err != nil {
		return false
	}
	return strings.ToLower(filepath.Ext(path)) == ".yammm"
}

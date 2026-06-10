package location

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewCanonicalPath_Absolute(t *testing.T) {
	// Get current working directory to construct absolute path
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Relative path should become absolute
	cp, err := NewCanonicalPath("testfile.go")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	// Result should start with / (Unix) or contain :/ (Windows)
	s := cp.String()
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, ":/") {
		t.Errorf("expected absolute path, got %q", s)
	}

	// Should contain the expected file name
	if !strings.HasSuffix(s, "testfile.go") {
		t.Errorf("expected path to end with testfile.go, got %q", s)
	}

	// Should be relative to cwd
	expectedPrefix := filepath.ToSlash(cwd)
	if !strings.HasPrefix(s, expectedPrefix) {
		t.Errorf("expected path to start with %q, got %q", expectedPrefix, s)
	}
}

func TestNewCanonicalPath_Clean(t *testing.T) {
	// Paths with . and .. should be cleaned
	tests := []struct {
		input    string
		contains string // The cleaned suffix we expect
	}{
		{"/a/../b", "/b"},
		{"/a/./b", "/a/b"},
		{"/a//b", "/a/b"},
		{"/a/b/../c/./d", "/a/c/d"},
	}

	for _, tt := range tests {
		if runtime.GOOS == "windows" {
			// Skip Unix-style absolute paths on Windows
			continue
		}

		t.Run(tt.input, func(t *testing.T) {
			cp, err := NewCanonicalPath(tt.input)
			if err != nil {
				t.Fatalf("NewCanonicalPath failed: %v", err)
			}

			s := cp.String()
			if !strings.HasSuffix(s, tt.contains) && !strings.Contains(s, tt.contains) {
				t.Errorf("expected path to contain %q, got %q", tt.contains, s)
			}

			// Should not contain . or .. (except as part of file names)
			if strings.Contains(s, "/./") || strings.Contains(s, "/../") {
				t.Errorf("path should be cleaned, got %q", s)
			}
		})
	}
}

func TestNewCanonicalPath_ForwardSlashes(t *testing.T) {
	// Result should use forward slashes on all platforms
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	cp, err := NewCanonicalPath(cwd)
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	s := cp.String()
	if strings.Contains(s, "\\") {
		t.Errorf("expected forward slashes only, got %q", s)
	}
}

func TestNewCanonicalPath_NonExistentPath(t *testing.T) {
	// Non-existent paths should not error (supports new file creation)
	cp, err := NewCanonicalPath("/nonexistent/path/to/file.yammm")
	if runtime.GOOS == "windows" {
		cp, err = NewCanonicalPath("C:/nonexistent/path/to/file.yammm")
	}

	if err != nil {
		t.Fatalf("NewCanonicalPath should accept non-existent paths, got: %v", err)
	}

	if cp.IsZero() {
		t.Error("result should not be zero")
	}
}

func TestNewCanonicalPath_Symlink(t *testing.T) {
	skipOnWindows(t, "symlink test not reliable on Windows")

	// Create a temp directory with a symlink
	tmpDir := t.TempDir()

	realDir := filepath.Join(tmpDir, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	realFile := filepath.Join(realDir, "file.txt")
	if err := os.WriteFile(realFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	linkDir := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	linkedFile := filepath.Join(linkDir, "file.txt")

	// Canonicalize the symlinked path
	cp, err := NewCanonicalPath(linkedFile)
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	// The canonical form of an existing absolute path is its fully
	// symlink-resolved form (the link component AND any symlinked temp-dir
	// ancestors, e.g. /var -> /private/var on macOS).
	want, err := filepath.EvalSymlinks(linkedFile)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", linkedFile, err)
	}
	if s := cp.String(); s != want {
		t.Errorf("NewCanonicalPath(%q) = %q, want fully resolved %q", linkedFile, s, want)
	}
}

func TestNewCanonicalPath_ErrorHandling(t *testing.T) {
	skipOnWindows(t, "symlink/permission tests not reliable on Windows")

	tmpDir := t.TempDir()

	t.Run("permission denied returns error", func(t *testing.T) {
		// Create a directory with a file, then remove read permission
		unreadableDir := filepath.Join(tmpDir, "unreadable")
		if err := os.Mkdir(unreadableDir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		fileInDir := filepath.Join(unreadableDir, "file.txt")
		if err := os.WriteFile(fileInDir, []byte("test"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		// Remove all permissions from directory
		if err := os.Chmod(unreadableDir, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer os.Chmod(unreadableDir, 0o700) //nolint:gosec // Restore for cleanup

		_, err := NewCanonicalPath(fileInDir)
		if err == nil {
			t.Error("expected error for permission denied, got nil")
		}
		// Use errors.Is with fs.ErrPermission for robust error classification.
		// This properly follows the error chain through fmt.Errorf wrapping,
		// unlike os.IsPermission which only unwraps specific error types.
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("expected fs.ErrPermission in error chain, got: %v", err)
		}
	})

	t.Run("symlink loop returns error", func(t *testing.T) {
		linkA := filepath.Join(tmpDir, "loop_a")
		linkB := filepath.Join(tmpDir, "loop_b")

		if err := os.Symlink(linkB, linkA); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		if err := os.Symlink(linkA, linkB); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		_, err := NewCanonicalPath(linkA)
		if err == nil {
			t.Error("expected error for symlink loop, got nil")
		}
		// Use semantic error classification instead of brittle string matching.
		// The error message text varies by OS/locale ("too many links", "too many levels of symbolic links", etc.)
		// Verify: (1) path is mentioned, (2) not fs.ErrNotExist (would trigger fallback), (3) not permission error
		if !strings.Contains(err.Error(), linkA) {
			t.Errorf("error should reference input path %q, got: %v", linkA, err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("symlink loop should not be classified as fs.ErrNotExist: %v", err)
		}
		if errors.Is(err, fs.ErrPermission) {
			t.Errorf("symlink loop should not be classified as fs.ErrPermission: %v", err)
		}
	})

	t.Run("broken symlink falls back to absolute path", func(t *testing.T) {
		brokenLink := filepath.Join(tmpDir, "broken_link")
		if err := os.Symlink("/nonexistent/target/12345", brokenLink); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		cp, err := NewCanonicalPath(brokenLink)
		if err != nil {
			t.Errorf("broken symlink should fall back (IsNotExist), got error: %v", err)
		}
		if cp.IsZero() {
			t.Error("result should not be zero")
		}
		// Should contain the symlink path (not resolved)
		if !strings.Contains(cp.String(), "broken_link") {
			t.Errorf("expected fallback to contain 'broken_link', got: %q", cp.String())
		}
	})
}

func TestMustCanonicalPath(t *testing.T) {
	// Should not panic for valid path
	cp := MustCanonicalPath(".")
	if cp.IsZero() {
		t.Error("result should not be zero")
	}
}

func TestCanonicalPath_IsZero(t *testing.T) {
	var zeroCP CanonicalPath
	if !zeroCP.IsZero() {
		t.Error("zero value should report IsZero() == true")
	}

	cp, _ := NewCanonicalPath(".")
	if cp.IsZero() {
		t.Error("valid path should not be zero")
	}
}

func TestCanonicalPath_Base(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/a/b/c.txt", "c.txt"},
		{"/a/b/c", "c"},
		{"/a", "a"},
	}

	for _, tt := range tests {
		if runtime.GOOS == "windows" {
			continue
		}
		t.Run(tt.path, func(t *testing.T) {
			cp, err := NewCanonicalPath(tt.path)
			if err != nil {
				t.Fatalf("NewCanonicalPath failed: %v", err)
			}
			if got := cp.Base(); got != tt.want {
				t.Errorf("Base() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalPath_Dir(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp, err := NewCanonicalPath("/a/b/c.txt")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	dir := cp.Dir()
	if !strings.HasSuffix(dir.String(), "/a/b") {
		t.Errorf("Dir() = %q; want suffix /a/b", dir.String())
	}

	if dir.IsZero() {
		t.Error("Dir() should not return zero value")
	}
}

func TestCanonicalPath_Join(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp, err := NewCanonicalPath("/a/b")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	joined, err := cp.Join("c", "d.txt")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	if !strings.HasSuffix(joined.String(), "/a/b/c/d.txt") {
		t.Errorf("Join() = %q; want suffix /a/b/c/d.txt", joined.String())
	}
}

func TestCanonicalPath_Join_WithDotDot(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp, err := NewCanonicalPath("/a/b/c")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	joined, err := cp.Join("..", "d.txt")
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	// Should clean the .. segment
	s := joined.String()
	if strings.Contains(s, "..") {
		t.Errorf("Join should clean .. segments, got %q", s)
	}
	if !strings.HasSuffix(s, "/a/b/d.txt") {
		t.Errorf("Join() = %q; want suffix /a/b/d.txt", s)
	}
}

func TestCanonicalPath_Join_ZeroValue(t *testing.T) {
	var zeroCP CanonicalPath
	joined, err := zeroCP.Join("a", "b")
	if err != nil {
		t.Fatalf("Join on zero value should not error: %v", err)
	}
	if !joined.IsZero() {
		t.Error("Join on zero value should return zero value")
	}
}

func TestCanonicalPath_Join_BackslashNormalization(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp, err := NewCanonicalPath("/base/path")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	tests := []struct {
		name     string
		elements []string
		wantEnd  string
	}{
		{
			name:     "single backslash element",
			elements: []string{"sub\\dir"},
			wantEnd:  "/base/path/sub/dir",
		},
		{
			name:     "multiple backslash segments",
			elements: []string{"a\\b\\c"},
			wantEnd:  "/base/path/a/b/c",
		},
		{
			name:     "backslash path traversal cleaned",
			elements: []string{"..\\sibling"},
			wantEnd:  "/base/sibling",
		},
		{
			name:     "mixed forward and backslash",
			elements: []string{"sub/a\\b"},
			wantEnd:  "/base/path/sub/a/b",
		},
		{
			name:     "backslash in multiple elements",
			elements: []string{"a\\b", "c\\d"},
			wantEnd:  "/base/path/a/b/c/d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joined, err := cp.Join(tt.elements...)
			if err != nil {
				t.Fatalf("Join failed: %v", err)
			}

			got := joined.String()

			// Verify no backslashes remain (invariant check)
			if strings.Contains(got, "\\") {
				t.Errorf("Join() = %q; contains backslashes, violates forward-slash invariant", got)
			}

			// Verify expected suffix
			if !strings.HasSuffix(got, tt.wantEnd) {
				t.Errorf("Join() = %q; want suffix %q", got, tt.wantEnd)
			}
		})
	}
}

// TestCanonicalPath_Join_RejectsAbsoluteElements verifies that Join returns
// an error when any element looks like an absolute path.
func TestCanonicalPath_Join_RejectsAbsoluteElements(t *testing.T) {
	base := CanonicalPath{path: "/base/path"}
	if runtime.GOOS == "windows" {
		base = CanonicalPath{path: "C:/base/path"}
	}

	tests := []struct {
		name    string
		element string
	}{
		{"unix absolute", "/etc/passwd"},
		{"unix root", "/"},
		{"windows volume forward", "C:/Windows"},
		{"windows volume back", "C:\\Windows"},
		{"windows other volume", "D:/other"},
		{"unc forward", "//server/share"},
		{"unc back", "\\\\server\\share"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := base.Join(tt.element)
			if err == nil {
				t.Errorf("Join(%q) should return error for absolute element", tt.element)
				return
			}
			// Verify error type
			if !errors.Is(err, ErrAbsoluteJoinElement) {
				t.Errorf("expected ErrAbsoluteJoinElement, got: %v", err)
			}
			// Verify error mentions the problematic element (for context)
			if !strings.Contains(err.Error(), tt.element) {
				t.Errorf("error should mention element %q, got: %v", tt.element, err)
			}
		})
	}
}

// TestCanonicalPath_Join_AcceptsRelativeElements verifies that Join still
// works correctly with relative paths after adding absolute element rejection.
func TestCanonicalPath_Join_AcceptsRelativeElements(t *testing.T) {
	base := CanonicalPath{path: "/base/path"}
	if runtime.GOOS == "windows" {
		base = CanonicalPath{path: "C:/base/path"}
	}

	tests := []struct {
		name     string
		elements []string
	}{
		{"simple file", []string{"file.txt"}},
		{"subdirectory", []string{"sub", "dir", "file.txt"}},
		{"dotdot", []string{"..", "sibling"}},
		{"dot", []string{".", "same"}},
		{"backslash relative", []string{"sub\\dir"}},     // Backslash but not absolute
		{"volume-like name", []string{"C:", "notapath"}}, // C: without slash is just a name
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := base.Join(tt.elements...)
			if err != nil {
				t.Errorf("Join(%v) returned unexpected error: %v", tt.elements, err)
				return
			}
			if result.IsZero() {
				t.Error("Join should return non-zero result")
			}
		})
	}
}

func TestCanonicalPath_String_Empty(t *testing.T) {
	var cp CanonicalPath
	if cp.String() != "" {
		t.Errorf("zero value String() = %q; want empty", cp.String())
	}
}

func TestCanonicalPath_Equality(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp1, _ := NewCanonicalPath("/a/b/c")
	cp2, _ := NewCanonicalPath("/a/b/c")
	cp3, _ := NewCanonicalPath("/a/b/d")

	if cp1 != cp2 {
		t.Error("equal paths should be equal")
	}
	if cp1 == cp3 {
		t.Error("different paths should not be equal")
	}
}

func TestCanonicalPath_MapKey(t *testing.T) {
	// CanonicalPath should work as map key
	skipOnWindows(t, "Unix path test")

	cp1, _ := NewCanonicalPath("/a/b/c")
	cp2, _ := NewCanonicalPath("/a/b/c")

	m := make(map[CanonicalPath]int)
	m[cp1] = 42

	if m[cp2] != 42 {
		t.Error("equal CanonicalPaths should work as map keys")
	}
}

func TestCanonicalizeAbsolutePath(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantErrIs error
		check     func(string) bool
	}{
		{
			name:    "unix absolute",
			input:   "/a/../b",
			wantErr: false,
			check: func(s string) bool {
				return s == "/b"
			},
		},
		{
			name:    "unix with double slash",
			input:   "/a//b",
			wantErr: false,
			check: func(s string) bool {
				return s == "/a/b"
			},
		},
		{
			name:    "unix with dot",
			input:   "/a/./b",
			wantErr: false,
			check: func(s string) bool {
				return s == "/a/b"
			},
		},
		{
			name:    "relative path",
			input:   "a/b/c",
			wantErr: true,
		},
		{
			name:    "windows absolute",
			input:   "C:/a/b",
			wantErr: false,
			check: func(s string) bool {
				return s == "C:/a/b"
			},
		},
		{
			name:    "windows with backslash",
			input:   "C:\\a\\b",
			wantErr: false,
			check: func(s string) bool {
				// Should convert to forward slashes
				return s == "C:/a/b"
			},
		},
		// Drive-root handling: cleaning may not escape or strip the root.
		{
			name:    "windows clean to drive root",
			input:   "C:/a/..",
			wantErr: false,
			check: func(s string) bool {
				return s == "C:/"
			},
		},
		{
			name:    "windows dotdot at drive root",
			input:   "C:/..",
			wantErr: false,
			check: func(s string) bool {
				return s == "C:/"
			},
		},
		// UNC paths are rejected to prevent SourceID collisions
		// (path.Clean collapses // to /).
		{
			name:      "UNC path rejected",
			input:     "//server/share/file.txt",
			wantErr:   true,
			wantErrIs: ErrUNCPath,
		},
		{
			name:      "UNC root rejected",
			input:     "//server/share",
			wantErr:   true,
			wantErrIs: ErrUNCPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := canonicalizeAbsolutePath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", result)
				} else if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("expected %v in error chain, got: %v", tt.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil && !tt.check(result) {
				t.Errorf("check failed for result %q", result)
			}
		})
	}
}

// TestLooksLikeAbsolute drives looksLikeAbsoluteElement (Join's guard) and
// looksLikeAbsolutePath (the SourceID-side check) through one shared table:
// the two are intentionally behaviorally identical, and every row asserts
// both so any future divergence fails loudly.
func TestLooksLikeAbsolute(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Unix absolute paths
		{"/path/to/file", true},
		{"/etc/passwd", true},
		{"/", true},

		// Windows absolute paths
		{"C:/path", true},
		{"C:\\path", true},
		{"C:/Windows", true},
		{"D:/file.txt", true},

		// Windows UNC paths
		{"\\\\server\\share", true},
		{"//server/share", true},
		{"//", true},
		{"\\\\", true},

		// Synthetic identifiers (should NOT look like absolute paths)
		{"test://unit/test.yammm", false},
		{"inline:schema", false},
		{"<stdin>", false},
		{"embedded://app/builtin.yammm", false},

		// Relative paths
		{"relative/path", false},
		{"file.txt", false},
		{"./relative", false},
		{"../parent", false},
		{"..", false},
		{"sub\\dir", false}, // Backslash but not UNC or volume

		// Edge cases
		{"", false},
		{"C", false},        // Just a letter
		{"C:", false},       // Volume without slash
		{"1:/path", false},  // Digit, not letter
		{"\\single", false}, // Single backslash (not UNC)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := looksLikeAbsoluteElement(tt.input); got != tt.want {
				t.Errorf("looksLikeAbsoluteElement(%q) = %v; want %v", tt.input, got, tt.want)
			}
			if got := looksLikeAbsolutePath(tt.input); got != tt.want {
				t.Errorf("looksLikeAbsolutePath(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestFixWindowsPath tests the Windows drive-root fixup logic.
func TestFixWindowsPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
		want   string
	}{
		// Bare drive letter fixup: "C:" -> "C:/"
		{"bare drive from Dir", "C:/a", "C:", "C:/"},
		{"bare drive from Clean", "C:/a/..", "C:", "C:/"},
		{"bare drive from root Dir", "C:/", "C:", "C:/"},

		// Root escape fixup: "." -> "C:/"
		{"root escape single dotdot", "C:/..", ".", "C:/"},
		{"root escape multiple dotdot", "C:/a/b/../../..", ".", "C:/"},

		// Valid paths should pass through unchanged
		{"valid deep path", "C:/a/b", "C:/a", "C:/a"},
		{"valid root with file", "C:/file.txt", "C:/", "C:/"},

		// Unix paths should pass through unchanged
		{"unix path", "/a/b", "/a", "/a"},
		{"unix root", "/a", "/", "/"},
		{"unix root escape", "/..", "/", "/"},

		// Non-Windows paths should pass through unchanged
		{"relative path", "a/b", "a", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixWindowsPath(tt.input, tt.output)
			if got != tt.want {
				t.Errorf("fixWindowsPath(%q, %q) = %q; want %q", tt.input, tt.output, got, tt.want)
			}
		})
	}
}

// TestFixWindowsClean tests path.Clean with Windows drive-root fixup.
func TestFixWindowsClean(t *testing.T) {
	// One representative case per behavior class; the full fixup matrix is
	// pinned one layer down in TestFixWindowsPath.
	tests := []struct {
		input string
		want  string
	}{
		{"C:/a/..", "C:/"},     // bare-drive restore after Clean
		{"C:/..", "C:/"},       // root escape pinned to drive root
		{"C:/a/./b", "C:/a/b"}, // plain clean passes through
		{"/a/..", "/"},         // unix unaffected by the fixup
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := fixWindowsClean(tt.input)
			if got != tt.want {
				t.Errorf("fixWindowsClean(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestCanonicalPath_Dir_NFCNormalization verifies that Dir() normalizes NFD to NFC.
// This ensures the NFC invariant is maintained even for directly-constructed values.
func TestCanonicalPath_Dir_NFCNormalization(t *testing.T) {
	tests := []struct {
		name    string
		nfdPath string // NFD input (decomposed)
		wantDir string // Expected NFC output (precomposed)
	}{
		{
			name:    "e-acute in parent",
			nfdPath: "/users/cafe\u0301/subdir", // café with combining acute
			wantDir: "/users/caf\u00e9",         // café with precomposed é
		},
		{
			name:    "a-umlaut in parent",
			nfdPath: "/data/ba\u0308r/file.txt", // bär with combining diaeresis
			wantDir: "/data/b\u00e4r",           // bär with precomposed ä
		},
		{
			name:    "Windows path with NFD",
			nfdPath: "C:/users/cafe\u0301/docs",
			wantDir: "C:/users/caf\u00e9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Direct construction to test with non-NFC input
			cp := CanonicalPath{path: tt.nfdPath}
			got := cp.Dir()

			if got.String() != tt.wantDir {
				t.Errorf("Dir() = %q (bytes: %x); want %q (bytes: %x)",
					got.String(), []byte(got.String()),
					tt.wantDir, []byte(tt.wantDir))
			}
		})
	}
}

// TestCanonicalPath_Dir_CleansInput verifies that Dir() cleans non-canonical input
// before taking the directory, ensuring semantic correctness and consistency with Join().
func TestCanonicalPath_Dir_CleansInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"dotdot as last", "/a/b/..", "/"},
		{"dot as last", "/a/b/.", "/a"},
		{"redundant slashes", "/a//b/c", "/a/b"},
		{"complex non-clean", "/a/./b/../c/d", "/a/c"},
		{"Windows dotdot as last", "C:/a/b/..", "C:/"},
		{"Windows dot as last", "C:/a/b/.", "C:/a"},
		{"Windows redundant slashes", "C:/a//b/c", "C:/a/b"},
		{"Windows root escape", "C:/a/../..", "C:/"},
		// Already-canonical inputs: Dir() must respect the drive root.
		{"Windows deep path", "C:/a/b/c", "C:/a/b"},
		{"Windows one level", "C:/a", "C:/"},
		{"Windows at root", "C:/", "C:/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Direct construction with non-canonical path
			cp := CanonicalPath{path: tt.input}
			got := cp.Dir()
			if got.String() != tt.want {
				t.Errorf("CanonicalPath{%q}.Dir() = %q; want %q", tt.input, got.String(), tt.want)
			}
			// Verify output is absolute
			if !isAbsolutePath(got.String()) {
				t.Errorf("Dir() result %q is not absolute", got.String())
			}
		})
	}
}

// TestCanonicalPath_Join_WindowsRootEscape tests Join() with ".." on Windows paths.
func TestCanonicalPath_Join_WindowsRootEscape(t *testing.T) {
	tests := []struct {
		name string
		base string
		elem []string
		want string
	}{
		{"single dotdot", "C:/a", []string{".."}, "C:/"},
		{"at root", "C:/", []string{".."}, "C:/"},
		{"multiple dotdot escape", "C:/a/b", []string{"..", "..", ".."}, "C:/"},
		{"normal join", "C:/a", []string{"b", "c"}, "C:/a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := CanonicalPath{path: tt.base}
			got, err := cp.Join(tt.elem...)
			if err != nil {
				t.Fatalf("Join() error = %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("CanonicalPath{%q}.Join(%v) = %q; want %q", tt.base, tt.elem, got.String(), tt.want)
			}
			// Verify result is still absolute
			if !isAbsolutePath(got.String()) {
				t.Errorf("Join() result %q is not absolute", got.String())
			}
		})
	}
}

// TestCanonicalPath_ZeroValue_Methods tests zero-value behavior for Base() and Dir().
func TestCanonicalPath_ZeroValue_Methods(t *testing.T) {
	var zero CanonicalPath

	// Base() should return empty string for zero value
	if got := zero.Base(); got != "" {
		t.Errorf("zero.Base() = %q; want empty string", got)
	}

	// Dir() should return zero value for zero value
	if got := zero.Dir(); !got.IsZero() {
		t.Errorf("zero.Dir().IsZero() = false; want true")
	}
}

// TestCanonicalPath_CrossPlatformInvariants validates the core invariants
// against a real filesystem path so they run on every platform (the
// Unix-literal tests above skip on Windows): forward-slash-only output,
// absoluteness, Base/Dir/Join behavior and their round-trip, cleaning of
// dirty inputs, and value-equality/map-key semantics across constructions.
func TestCanonicalPath_CrossPlatformInvariants(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	testFile := filepath.Join(subDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cp, err := NewCanonicalPath(testFile)
	if err != nil {
		t.Fatalf("NewCanonicalPath(%q): %v", testFile, err)
	}

	// requireInvariants asserts the two representation invariants every
	// CanonicalPath operation must preserve.
	requireInvariants := func(label, s string) {
		t.Helper()
		if strings.Contains(s, "\\") {
			t.Errorf("%s contains backslashes: %q", label, s)
		}
		if !isAbsolutePath(s) {
			t.Errorf("%s is not absolute: %q", label, s)
		}
	}

	requireInvariants("path", cp.String())

	if base := cp.Base(); base != "file.txt" {
		t.Errorf("Base() = %q; want %q", base, "file.txt")
	}

	dir := cp.Dir()
	requireInvariants("Dir()", dir.String())
	if !strings.HasSuffix(dir.String(), "/dir") {
		t.Errorf("Dir() = %q; want suffix /dir", dir.String())
	}

	// Dir + Base must round-trip to the original.
	rejoined, err := dir.Join(cp.Base())
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if rejoined != cp {
		t.Errorf("Dir+Base roundtrip: got %q, want %q", rejoined.String(), cp.String())
	}

	// Join preserves the invariants and cleans relative segments.
	joined, err := cp.Join("extra", "path")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	requireInvariants("Join()", joined.String())
	if !strings.HasSuffix(joined.String(), "/file.txt/extra/path") {
		t.Errorf("Join() = %q; want suffix /file.txt/extra/path", joined.String())
	}

	up, err := cp.Join("..")
	if err != nil {
		t.Fatalf("Join(..): %v", err)
	}
	if up != dir {
		t.Errorf("Join(..) = %q; want Dir() = %q", up.String(), dir.String())
	}

	// Dirty construction (./..-laden) resolves to the same canonical value,
	// so equality and map-key semantics hold across constructions.
	cpDirty, err := NewCanonicalPath(filepath.Join(subDir, "..", "dir", ".", "file.txt"))
	if err != nil {
		t.Fatalf("NewCanonicalPath(dirty): %v", err)
	}
	if cpDirty != cp {
		t.Errorf("dirty and clean constructions differ: %q vs %q", cpDirty.String(), cp.String())
	}
	m := map[CanonicalPath]int{cp: 42}
	if v, ok := m[cpDirty]; !ok || v != 42 {
		t.Errorf("map lookup via equal path failed: ok=%v, v=%d", ok, v)
	}
}

// TestCanonicalizeAbsolutePath_NFCNormalization verifies that NFD (decomposed)
// Unicode is normalized to NFC (composed). This is critical because:
// - macOS HFS+/APFS stores filenames in NFD form
// - User input and most text is typically in NFC form
// - Without normalization, the same file could produce different SourceIDs
func TestCanonicalizeAbsolutePath_NFCNormalization(t *testing.T) {
	// NFD form: base character + combining mark (e.g., "e" + U+0301 COMBINING ACUTE ACCENT)
	// NFC form: precomposed character (e.g., U+00E9 LATIN SMALL LETTER E WITH ACUTE)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "NFD e-acute normalizes to NFC",
			input: "/path/cafe\u0301/file.txt", // "café" with NFD é (e + combining acute)
			want:  "/path/caf\u00e9/file.txt",  // "café" with NFC é (precomposed)
		},
		{
			name:  "NFC stays NFC",
			input: "/path/caf\u00e9/file.txt", // Already NFC
			want:  "/path/caf\u00e9/file.txt",
		},
		{
			name:  "multiple NFD characters normalize",
			input: "/path/re\u0301sume\u0301.txt", // "résumé" with NFD
			want:  "/path/r\u00e9sum\u00e9.txt",   // "résumé" with NFC
		},
		{
			name:  "a-umlaut NFD to NFC",
			input: "/users/ma\u0308dchen/file.txt", // "mädchen" with NFD ä
			want:  "/users/m\u00e4dchen/file.txt",  // "mädchen" with NFC ä
		},
		{
			name:  "n-tilde NFD to NFC",
			input: "/path/espan\u0303ol/file.txt", // "español" with NFD ñ
			want:  "/path/espa\u00f1ol/file.txt",  // "español" with NFC ñ
		},
		{
			name:  "Windows path with NFD",
			input: "C:/Users/cafe\u0301/file.txt",
			want:  "C:/Users/caf\u00e9/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalizeAbsolutePath(tt.input)
			if err != nil {
				t.Fatalf("canonicalizeAbsolutePath(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("canonicalizeAbsolutePath(%q):\n  got:  %q (bytes: %x)\n  want: %q (bytes: %x)",
					tt.input, got, []byte(got), tt.want, []byte(tt.want))
			}
		})
	}
}

// TestCanonicalPath_JoinNFCNormalization verifies that Join normalizes NFD elements to NFC.
func TestCanonicalPath_JoinNFCNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	cp, err := NewCanonicalPath(tmpDir)
	if err != nil {
		t.Fatalf("NewCanonicalPath: %v", err)
	}

	// NFD and NFC versions of the same string
	nfdElement := "cafe\u0301" // NFD é (e + combining acute)
	nfcElement := "caf\u00e9"  // NFC é (precomposed)

	joinedNFD, err := cp.Join(nfdElement)
	if err != nil {
		t.Fatalf("Join(NFD): %v", err)
	}

	joinedNFC, err := cp.Join(nfcElement)
	if err != nil {
		t.Fatalf("Join(NFC): %v", err)
	}

	// Both should produce the same result after NFC normalization
	if joinedNFD.String() != joinedNFC.String() {
		t.Errorf("NFD and NFC joins should be equal:\n  NFD input: %q → %q\n  NFC input: %q → %q",
			nfdElement, joinedNFD.String(), nfcElement, joinedNFC.String())
	}

	// Result should be in NFC form (contain the precomposed character)
	if !strings.Contains(joinedNFD.String(), "\u00e9") {
		t.Errorf("result should contain NFC é (U+00E9), got: %q (bytes: %x)",
			joinedNFD.String(), []byte(joinedNFD.String()))
	}

	// Result should NOT contain the combining acute accent
	if strings.Contains(joinedNFD.String(), "\u0301") {
		t.Errorf("result should not contain combining accent (U+0301), got: %q",
			joinedNFD.String())
	}
}

// TestNewCanonicalPath_UnixBackslashNormalization verifies that backslashes
// in path names (which are valid filename characters on Unix) are normalized
// to forward slashes to maintain the forward-slash invariant.
//
// This test ensures consistency between NewCanonicalPath, Join, and
// canonicalizeAbsolutePath: all normalize backslashes to forward slashes.
func TestNewCanonicalPath_UnixBackslashNormalization(t *testing.T) {
	skipOnWindows(t, "backslash normalization test is for Unix systems where \\ is valid in filenames")

	// On Unix, we can't easily create files with literal backslashes in names
	// due to shell escaping issues. Instead, we test via canonicalizeAbsolutePath
	// which uses the same normalization logic as NewCanonicalPath.

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single backslash in path",
			input: "/path/with\\backslash/file.txt",
			want:  "/path/with/backslash/file.txt",
		},
		{
			name:  "multiple backslashes",
			input: "/path\\to\\file.txt",
			want:  "/path/to/file.txt",
		},
		{
			name:  "mixed slashes",
			input: "/path/to\\file\\name.txt",
			want:  "/path/to/file/name.txt",
		},
		{
			name:  "trailing backslash",
			input: "/path/to/dir\\",
			want:  "/path/to/dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalizeAbsolutePath(tt.input)
			if err != nil {
				t.Fatalf("canonicalizeAbsolutePath(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("canonicalizeAbsolutePath(%q) = %q; want %q", tt.input, got, tt.want)
			}
			// Verify no backslashes remain
			if strings.Contains(got, "\\") {
				t.Errorf("result contains backslashes: %q", got)
			}
		})
	}
}

// TestNewCanonicalPath_BackslashInvariant verifies that the forward-slash
// invariant is maintained: CanonicalPath.String() never contains backslashes.
func TestNewCanonicalPath_BackslashInvariant(t *testing.T) {
	// Test with current working directory (which should exist)
	cp, err := NewCanonicalPath(".")
	if err != nil {
		t.Fatalf("NewCanonicalPath(\".\") error: %v", err)
	}

	if strings.Contains(cp.String(), "\\") {
		t.Errorf("CanonicalPath should not contain backslashes: %q", cp.String())
	}

	// Test Join also maintains the invariant
	joined, err := cp.Join("sub", "dir", "file.txt")
	if err != nil {
		t.Fatalf("Join error: %v", err)
	}

	if strings.Contains(joined.String(), "\\") {
		t.Errorf("Joined path should not contain backslashes: %q", joined.String())
	}
}

// skipOnWindows skips tests that exercise Unix-style absolute paths or
// platform behaviors (symlinks, permissions) that are unreliable on Windows.
func skipOnWindows(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(reason)
	}
}

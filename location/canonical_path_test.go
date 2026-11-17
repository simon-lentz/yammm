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

func TestSourceIDFromPath_Absolute(t *testing.T) {
	// Get current working directory to construct absolute path
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Relative path should become absolute
	cp, err := SourceIDFromPath("testfile.go")
	if err != nil {
		t.Fatalf("SourceIDFromPath failed: %v", err)
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

	// Should be under cwd as the filesystem spells it
	expectedPrefix := identityForm(diskSpelling(t, cwd))
	if !strings.HasPrefix(s, expectedPrefix) {
		t.Errorf("expected path to start with %q, got %q", expectedPrefix, s)
	}
}

func TestSourceIDFromPath_Clean(t *testing.T) {
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
			cp, err := SourceIDFromPath(tt.input)
			if err != nil {
				t.Fatalf("SourceIDFromPath failed: %v", err)
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

func TestSourceIDFromPath_ForwardSlashes(t *testing.T) {
	// Result should use forward slashes on all platforms
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	cp, err := SourceIDFromPath(cwd)
	if err != nil {
		t.Fatalf("SourceIDFromPath failed: %v", err)
	}

	s := cp.String()
	if strings.Contains(s, "\\") {
		t.Errorf("expected forward slashes only, got %q", s)
	}
}

func TestSourceIDFromPath_NonExistentPath(t *testing.T) {
	// Non-existent paths should not error (supports new file creation)
	cp, err := SourceIDFromPath("/nonexistent/path/to/file.yammm")
	if runtime.GOOS == "windows" {
		cp, err = SourceIDFromPath("C:/nonexistent/path/to/file.yammm")
	}

	if err != nil {
		t.Fatalf("SourceIDFromPath should accept non-existent paths, got: %v", err)
	}

	if cp.IsZero() {
		t.Error("result should not be zero")
	}
}

func TestSourceIDFromPath_Symlink(t *testing.T) {
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
	cp, err := SourceIDFromPath(linkedFile)
	if err != nil {
		t.Fatalf("SourceIDFromPath failed: %v", err)
	}

	// An existing path's canonical form resolves every link, the temporary
	// directory's ancestors included (/var -> /private/var on macOS).
	if s, want := cp.String(), identityForm(diskSpelling(t, linkedFile)); s != want {
		t.Errorf("SourceIDFromPath(%q) = %q, want fully resolved %q", linkedFile, s, want)
	}
}

func TestSourceIDFromPath_ErrorHandling(t *testing.T) {
	skipOnWindows(t, "symlink/permission tests not reliable on Windows")

	tmpDir := t.TempDir()

	t.Run("an untraversable directory keeps the typed tail", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root traverses a directory whatever its permission bits")
		}
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

		cp, err := SourceIDFromPath(fileInDir)
		if err != nil {
			t.Fatalf("SourceIDFromPath under an untraversable directory: %v", err)
		}
		if want := identityForm(diskSpelling(t, unreadableDir)) + "/file.txt"; cp.String() != want {
			t.Errorf("SourceIDFromPath = %q, want %q", cp.String(), want)
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

		_, err := SourceIDFromPath(linkA)
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

	t.Run("a dangling symlink resolves to its target", func(t *testing.T) {
		brokenLink := filepath.Join(tmpDir, "broken_link")
		if err := os.Symlink("/nonexistent/target/12345", brokenLink); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		cp, err := SourceIDFromPath(brokenLink)
		if err != nil {
			t.Errorf("broken symlink should fall back (IsNotExist), got error: %v", err)
		}
		if cp.IsZero() {
			t.Error("result should not be zero")
		}
		want, err := SourceIDFromPath("/nonexistent/target/12345")
		if err != nil {
			t.Fatal(err)
		}
		if cp.String() != want.String() {
			t.Errorf("SourceIDFromPath(dangling link) = %q, want its target %q", cp.String(), want.String())
		}
	})
}

func TestCanonicalPath_IsZero(t *testing.T) {
	var zeroCP canonicalPath
	if !zeroCP.IsZero() {
		t.Error("zero value should report IsZero() == true")
	}

	cp, _ := SourceIDFromPath(".")
	if cp.IsZero() {
		t.Error("valid path should not be zero")
	}
}

func TestSourceIDFromPath_Dir(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp, err := SourceIDFromPath("/a/b/c.txt")
	if err != nil {
		t.Fatalf("SourceIDFromPath failed: %v", err)
	}

	dir := cp.Dir()
	if !strings.HasSuffix(dir.String(), "/a/b") {
		t.Errorf("Dir() = %q; want suffix /a/b", dir.String())
	}

	if dir.IsZero() {
		t.Error("Dir() should not return zero value")
	}
}

func TestCanonicalPath_String_Empty(t *testing.T) {
	var cp canonicalPath
	if cp.String() != "" {
		t.Errorf("zero value String() = %q; want empty", cp.String())
	}
}

func TestSourceIDFromPath_Equality(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp1, _ := SourceIDFromPath("/a/b/c")
	cp2, _ := SourceIDFromPath("/a/b/c")
	cp3, _ := SourceIDFromPath("/a/b/d")

	if cp1 != cp2 {
		t.Error("equal paths should be equal")
	}
	if cp1 == cp3 {
		t.Error("different paths should not be equal")
	}
}

func TestSourceIDFromPath_MapKey(t *testing.T) {
	skipOnWindows(t, "Unix path test")

	cp1, _ := SourceIDFromPath("/a/b/c")
	cp2, _ := SourceIDFromPath("/a/b/c")

	m := make(map[SourceID]int)
	m[cp1] = 42

	if m[cp2] != 42 {
		t.Error("equal identities should work as map keys")
	}
}

// TestLooksLikeAbsolute drives the absolute-path predicate behind
// NewSourceID's collision check through every path family it distinguishes.
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
		{"A:/x", true},
		{"z:\\x", true},
		{"1:/x", false},

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
			if got := looksLikeAbsolute(tt.input); got != tt.want {
				t.Errorf("looksLikeAbsolute(%q) = %v; want %v", tt.input, got, tt.want)
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
			cp := canonicalPath{path: tt.nfdPath}
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
		name    string
		windows bool
		input   string
		want    string
	}{
		{"dotdot as last", false, "/a/b/..", "/"},
		{"dot as last", false, "/a/b/.", "/a"},
		{"redundant slashes", false, "/a//b/c", "/a/b"},
		{"complex non-clean", false, "/a/./b/../c/d", "/a/c"},
		{"at root", false, "/", "/"},
		{"Windows dotdot as last", true, "C:/a/b/..", "C:/"},
		{"Windows dot as last", true, "C:/a/b/.", "C:/a"},
		{"Windows redundant slashes", true, "C:/a//b/c", "C:/a/b"},
		{"Windows root escape", true, "C:/a/../..", "C:/"},
		// Already-canonical inputs: Dir() must respect the drive root.
		{"Windows deep path", true, "C:/a/b/c", "C:/a/b"},
		{"Windows one level", true, "C:/a", "C:/"},
		{"Windows at root", true, "C:/", "C:/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.windows != (runtime.GOOS == "windows") {
				t.Skip("the path is written for the other host")
			}
			// Direct construction with non-canonical path
			cp := canonicalPath{path: tt.input}
			got := cp.Dir()
			if got.String() != tt.want {
				t.Errorf("CanonicalPath{%q}.Dir() = %q; want %q", tt.input, got.String(), tt.want)
			}
			if !isHostAbsolute(got.String()) {
				t.Errorf("Dir() result %q is not absolute", got.String())
			}
		})
	}
}

// TestCanonicalPath_ZeroValue_Methods tests zero-value behavior for Dir().
func TestCanonicalPath_ZeroValue_Methods(t *testing.T) {
	var zero canonicalPath

	// Dir() should return zero value for zero value
	if got := zero.Dir(); !got.IsZero() {
		t.Errorf("zero.Dir().IsZero() = false; want true")
	}
}

// TestSourceIDFromPath_CrossPlatformInvariants validates the core invariants
// against a real filesystem path so they run on every platform (the
// Unix-literal tests above skip on Windows): forward-slash-only output,
// absoluteness, Dir, cleaning of dirty inputs, and value-equality/map-key
// semantics across constructions.
func TestSourceIDFromPath_CrossPlatformInvariants(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	testFile := filepath.Join(subDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cp, err := SourceIDFromPath(testFile)
	if err != nil {
		t.Fatalf("SourceIDFromPath(%q): %v", testFile, err)
	}

	// requireInvariants asserts the two representation invariants every
	// identity operation must preserve.
	requireInvariants := func(label, s string) {
		t.Helper()
		if containsHostSeparator(s) {
			t.Errorf("%s contains the host separator: %q", label, s)
		}
		if !isHostAbsolute(s) {
			t.Errorf("%s is not absolute: %q", label, s)
		}
	}

	requireInvariants("path", cp.String())

	dir := cp.Dir()
	requireInvariants("Dir()", dir.String())
	if !strings.HasSuffix(dir.String(), "/dir") {
		t.Errorf("Dir() = %q; want suffix /dir", dir.String())
	}

	// Dirty construction (./..-laden) resolves to the same canonical value,
	// so equality and map-key semantics hold across constructions.
	cpDirty, err := SourceIDFromPath(filepath.Join(subDir, "..", "dir", ".", "file.txt"))
	if err != nil {
		t.Fatalf("SourceIDFromPath(dirty): %v", err)
	}
	if cpDirty != cp {
		t.Errorf("dirty and clean constructions differ: %q vs %q", cpDirty.String(), cp.String())
	}
	m := map[SourceID]int{cp: 42}
	if v, ok := m[cpDirty]; !ok || v != 42 {
		t.Errorf("map lookup via equal path failed: ok=%v, v=%d", ok, v)
	}
}

// TestSourceIDFromPath_BackslashInvariant verifies that the host's separator is
// always written as "/": on Windows no backslash survives canonicalization.
func TestSourceIDFromPath_BackslashInvariant(t *testing.T) {
	// Test with current working directory (which should exist)
	cp, err := SourceIDFromPath(".")
	if err != nil {
		t.Fatalf("SourceIDFromPath(\".\") error: %v", err)
	}

	if containsHostSeparator(cp.String()) {
		t.Errorf("the identity holds the host separator: %q", cp.String())
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

// isHostAbsolute reports whether a canonical path string is absolute on this host.
func isHostAbsolute(s string) bool {
	return filepath.IsAbs(filepath.FromSlash(s))
}

// containsHostSeparator reports whether s holds the host's separator where it
// is not "/": a canonical path writes every separator as "/".
func containsHostSeparator(s string) bool {
	return filepath.Separator != '/' && strings.ContainsRune(s, filepath.Separator)
}

// TestSourceIDFromPath_NFCNormalization holds a decomposed name to its
// composed identity: NFD and NFC spellings of one name give one identity, in
// NFC.
func TestSourceIDFromPath_NFCNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	nfd, err := SourceIDFromPath(filepath.Join(tmpDir, "cafe\u0301"))
	if err != nil {
		t.Fatalf("SourceIDFromPath(NFD): %v", err)
	}
	nfc, err := SourceIDFromPath(filepath.Join(tmpDir, "caf\u00e9"))
	if err != nil {
		t.Fatalf("SourceIDFromPath(NFC): %v", err)
	}
	if nfd != nfc {
		t.Errorf("NFD and NFC spellings give two identities: %q and %q", nfd, nfc)
	}
	if strings.Contains(nfd.String(), "\u0301") || !strings.Contains(nfd.String(), "\u00e9") {
		t.Errorf("the identity %q is not in NFC", nfd)
	}
}

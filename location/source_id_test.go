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

func TestNewSourceID(t *testing.T) {
	// NewSourceID should accept any identifier without validation
	tests := []string{
		"test://unit/person.yammm",
		"inline:schema",
		"<stdin>",
		"embedded://app/builtin.yammm",
		"", // Even empty is allowed (caller's responsibility)
	}

	for _, id := range tests {
		sid := NewSourceID(id)
		if sid.String() != id {
			t.Errorf("NewSourceID(%q).String() = %q; want %q", id, sid.String(), id)
		}
		if sid.IsFilePath() {
			t.Errorf("NewSourceID(%q) should not be a file path", id)
		}
	}
}

func TestMustNewSourceID_Valid(t *testing.T) {
	tests := []string{
		"test://unit/person.yammm",
		"inline:schema",
		"<stdin>",
		"embedded://app/builtin.yammm",
		"relative/path", // Relative paths are allowed (not absolute)
	}

	for _, id := range tests {
		// Should not panic
		sid := MustNewSourceID(id)
		if sid.String() != id {
			t.Errorf("MustNewSourceID(%q).String() = %q; want %q", id, sid.String(), id)
		}
	}
}

func TestMustNewSourceID_Panics(t *testing.T) {
	tests := []string{
		"/absolute/path",
		"C:/windows/path",
		"C:\\windows\\path",
		"//unc/path",
		"\\\\unc\\path",
	}

	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("MustNewSourceID(%q) should panic", id)
				}
			}()
			MustNewSourceID(id)
		})
	}
}

func TestValidateSyntheticSourceID(t *testing.T) {
	validCases := []string{
		"test://unit/person.yammm",
		"inline:schema",
		"<stdin>",
		"relative/path",
		"just-a-name",
	}

	for _, id := range validCases {
		if err := ValidateSyntheticSourceID(id); err != nil {
			t.Errorf("ValidateSyntheticSourceID(%q) = %v; want nil", id, err)
		}
	}

	invalidCases := []struct {
		id      string
		wantErr error
	}{
		{"", ErrEmptySourceID},
		{"/absolute", ErrAbsolutePathSourceID},
		{"C:/windows", ErrAbsolutePathSourceID},
		{"C:\\windows", ErrAbsolutePathSourceID},
		{"//unc", ErrAbsolutePathSourceID},
		{"\\\\unc", ErrAbsolutePathSourceID},
	}

	for _, tc := range invalidCases {
		err := ValidateSyntheticSourceID(tc.id)
		if err == nil {
			t.Errorf("ValidateSyntheticSourceID(%q) = nil; want %v", tc.id, tc.wantErr)
			continue
		}
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("ValidateSyntheticSourceID(%q) = %v; want %v", tc.id, err, tc.wantErr)
		}
	}
}

func TestSourceIDFromPath(t *testing.T) {
	// Should create a file-backed SourceID
	sid, err := SourceIDFromPath(".")
	if err != nil {
		t.Fatalf("SourceIDFromPath(\".\") failed: %v", err)
	}

	if !sid.IsFilePath() {
		t.Error("SourceIDFromPath should create file-backed SourceID")
	}

	if sid.IsZero() {
		t.Error("result should not be zero")
	}

	// String should return an absolute path
	s := sid.String()
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, ":/") {
		t.Errorf("String() = %q; want absolute path", s)
	}
}

func TestMustSourceIDFromPath(t *testing.T) {
	// Should not panic for valid path
	sid := MustSourceIDFromPath(".")
	if !sid.IsFilePath() {
		t.Error("should create file-backed SourceID")
	}
}

func TestSourceIDFromCanonicalPath(t *testing.T) {
	cp, err := NewCanonicalPath(".")
	if err != nil {
		t.Fatalf("NewCanonicalPath failed: %v", err)
	}

	sid := SourceIDFromCanonicalPath(cp)
	if !sid.IsFilePath() {
		t.Error("should create file-backed SourceID")
	}

	// CanonicalPath() should return the exact same value
	gotCP, ok := sid.CanonicalPath()
	if !ok {
		t.Fatal("CanonicalPath() should return ok=true for file-backed SourceID")
	}
	if gotCP != cp {
		t.Error("CanonicalPath() should return the exact stored value")
	}
}

func TestSourceIDFromAbsolutePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "unix absolute",
			input:   "/a/b/c",
			wantErr: false,
		},
		{
			name:    "unix with dotdot",
			input:   "/a/../b",
			wantErr: false,
		},
		{
			name:    "windows absolute",
			input:   "C:/a/b",
			wantErr: false,
		},
		{
			name:    "relative path",
			input:   "a/b/c",
			wantErr: true,
		},
		{
			name:    "UNC path forward slashes rejected",
			input:   "//server/share/file.txt",
			wantErr: true,
		},
		{
			name:    "UNC path backslashes rejected",
			input:   "\\\\server\\share\\file.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid, err := SourceIDFromAbsolutePath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %v", sid)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sid.IsFilePath() {
				t.Error("should be file-backed")
			}
		})
	}
}

func TestSourceIDFromAbsolutePath_CleaningEquivalence(t *testing.T) {
	// Paths with . and .. should produce equal SourceIDs to cleaned paths
	tests := []struct {
		dirty string
		clean string
	}{
		{"/a/../b", "/b"},
		{"/a/./b", "/a/b"},
		{"/a//b", "/a/b"},
	}

	for _, tt := range tests {
		t.Run(tt.dirty, func(t *testing.T) {
			dirtySID, err := SourceIDFromAbsolutePath(tt.dirty)
			if err != nil {
				t.Fatalf("SourceIDFromAbsolutePath(%q) failed: %v", tt.dirty, err)
			}

			cleanSID, err := SourceIDFromAbsolutePath(tt.clean)
			if err != nil {
				t.Fatalf("SourceIDFromAbsolutePath(%q) failed: %v", tt.clean, err)
			}

			if dirtySID != cleanSID {
				t.Errorf("SourceIDFromAbsolutePath(%q) = %v; want equal to %v", tt.dirty, dirtySID, cleanSID)
			}
		})
	}
}

func TestSourceIDFromAbsolutePath_UNCRejection(t *testing.T) {
	// UNC paths must be rejected to prevent SourceID collisions.
	// Without rejection, path.Clean would collapse // to /, causing:
	//   "//server/share" and "/server/share" → same SourceID
	// This violates SourceID injectivity (different paths should produce different IDs).

	tests := []struct {
		name  string
		input string
	}{
		{"forward slash UNC", "//server/share"},
		{"forward slash UNC with file", "//server/share/path/file.txt"},
		{"backslash UNC", "\\\\server\\share"},
		{"backslash UNC with file", "\\\\server\\share\\path\\file.txt"},
		{"triple slash collapses", "///server/share"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SourceIDFromAbsolutePath(tt.input)
			if err == nil {
				t.Errorf("SourceIDFromAbsolutePath(%q) should return error for UNC path", tt.input)
				return
			}
			if !errors.Is(err, ErrUNCPath) {
				t.Errorf("expected ErrUNCPath, got: %v", err)
			}
		})
	}
}

func TestSourceID_IsZero(t *testing.T) {
	var zeroSID SourceID
	if !zeroSID.IsZero() {
		t.Error("zero value should report IsZero() == true")
	}

	syntheticSID := NewSourceID("test://unit")
	if syntheticSID.IsZero() {
		t.Error("synthetic SourceID should not be zero")
	}

	fileSID, _ := SourceIDFromPath(".")
	if fileSID.IsZero() {
		t.Error("file-backed SourceID should not be zero")
	}
}

func TestSourceID_String(t *testing.T) {
	syntheticSID := NewSourceID("test://unit/person.yammm")
	if syntheticSID.String() != "test://unit/person.yammm" {
		t.Errorf("String() = %q; want %q", syntheticSID.String(), "test://unit/person.yammm")
	}

	fileSID, _ := SourceIDFromPath(".")
	// Should be an absolute path
	s := fileSID.String()
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, ":/") {
		t.Errorf("file-backed String() = %q; want absolute path", s)
	}
}

func TestSourceID_CanonicalPath_Synthetic(t *testing.T) {
	sid := NewSourceID("test://unit")
	_, ok := sid.CanonicalPath()
	if ok {
		t.Error("CanonicalPath() should return ok=false for synthetic SourceID")
	}
}

func TestSourceID_Equality(t *testing.T) {
	// Synthetic SourceIDs
	sid1 := NewSourceID("test://unit")
	sid2 := NewSourceID("test://unit")
	sid3 := NewSourceID("test://other")

	if sid1 != sid2 {
		t.Error("equal synthetic SourceIDs should be equal")
	}
	if sid1 == sid3 {
		t.Error("different synthetic SourceIDs should not be equal")
	}

	// File-backed SourceIDs
	if runtime.GOOS != "windows" {
		path1, _ := SourceIDFromAbsolutePath("/a/b/c")
		path2, _ := SourceIDFromAbsolutePath("/a/b/c")
		path3, _ := SourceIDFromAbsolutePath("/a/b/d")

		if path1 != path2 {
			t.Error("equal file-backed SourceIDs should be equal")
		}
		if path1 == path3 {
			t.Error("different file-backed SourceIDs should not be equal")
		}
	}
}

func TestSourceID_MapKey(t *testing.T) {
	// SourceID should work as map key
	sid1 := NewSourceID("test://unit")
	sid2 := NewSourceID("test://unit")

	m := make(map[SourceID]int)
	m[sid1] = 42

	if m[sid2] != 42 {
		t.Error("equal SourceIDs should work as map keys")
	}
}

func TestSourceID_CaseSensitivity(t *testing.T) {
	// Different case should produce distinct SourceIDs
	// This is intentional design: YAMMM cannot know the caller's filesystem semantics
	if runtime.GOOS != "windows" {
		upper, _ := SourceIDFromAbsolutePath("/Users/Simon/schema.yammm")
		lower, _ := SourceIDFromAbsolutePath("/users/simon/schema.yammm")

		if upper == lower {
			t.Error("different-case paths should produce distinct SourceIDs (design decision)")
		}
	}
}

func TestCanonicalizePathForSourceID(t *testing.T) {
	// Should return a canonical path string
	result, err := CanonicalizePathForSourceID(".")
	if err != nil {
		t.Fatalf("CanonicalizePathForSourceID(\".\") failed: %v", err)
	}

	// Should be an absolute path
	if !strings.HasPrefix(result, "/") && !strings.Contains(result, ":/") {
		t.Errorf("result = %q; want absolute path", result)
	}
}

func TestMustCanonicalizePathForSourceID(t *testing.T) {
	// Should not panic for valid path
	result := MustCanonicalizePathForSourceID(".")
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestCanonicalizePathForSourceID_StrictSymlinkResolution(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("non-existent path returns error", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does_not_exist", "file.txt")
		_, err := CanonicalizePathForSourceID(nonExistent)
		if err == nil {
			t.Error("expected error for non-existent path, got nil")
		}
		// Use errors.Is with fs.ErrNotExist for robust error classification.
		// This properly follows the error chain through fmt.Errorf wrapping.
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected fs.ErrNotExist in error chain, got: %v", err)
		}
	})

	t.Run("broken symlink returns error", func(t *testing.T) {
		brokenLink := filepath.Join(tmpDir, "broken_link")
		if err := os.Symlink("/nonexistent/target", brokenLink); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		_, err := CanonicalizePathForSourceID(brokenLink)
		if err == nil {
			t.Error("expected error for broken symlink, got nil")
		}
		// Use errors.Is with fs.ErrNotExist for robust error classification.
		// EvalSymlinks returns fs.ErrNotExist for broken symlinks (target doesn't exist).
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected fs.ErrNotExist in error chain, got: %v", err)
		}
	})

	t.Run("valid symlink resolves correctly", func(t *testing.T) {
		realFile := filepath.Join(tmpDir, "real.txt")
		if err := os.WriteFile(realFile, []byte("test"), 0o600); err != nil {
			t.Fatalf("create real file: %v", err)
		}

		symlink := filepath.Join(tmpDir, "link.txt")
		if err := os.Symlink(realFile, symlink); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		realResult, err := CanonicalizePathForSourceID(realFile)
		if err != nil {
			t.Fatalf("CanonicalizePathForSourceID(realFile) failed: %v", err)
		}

		linkResult, err := CanonicalizePathForSourceID(symlink)
		if err != nil {
			t.Fatalf("CanonicalizePathForSourceID(symlink) failed: %v", err)
		}

		if realResult != linkResult {
			t.Errorf("symlink not resolved: real=%q, link=%q", realResult, linkResult)
		}
	})

	t.Run("matches SourceIDFromPath for existing files", func(t *testing.T) {
		realFile := filepath.Join(tmpDir, "match_test.txt")
		if err := os.WriteFile(realFile, []byte("test"), 0o600); err != nil {
			t.Fatalf("create file: %v", err)
		}

		canonicalized, err := CanonicalizePathForSourceID(realFile)
		if err != nil {
			t.Fatalf("CanonicalizePathForSourceID failed: %v", err)
		}

		sourceID, err := SourceIDFromPath(realFile)
		if err != nil {
			t.Fatalf("SourceIDFromPath failed: %v", err)
		}

		if canonicalized != sourceID.String() {
			t.Errorf("mismatch: CanonicalizePathForSourceID=%q, SourceIDFromPath=%q",
				canonicalized, sourceID.String())
		}
	})
}

func TestMustCanonicalizePathForSourceID_PanicsOnNonExistent(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-existent path, got none")
		}
	}()

	MustCanonicalizePathForSourceID("/nonexistent/path/12345/file.txt")
}

// TestCanonicalizePathForSourceID_UNCRejection verifies that UNC paths are rejected
// for consistency with NewCanonicalPath and SourceIDFromAbsolutePath.
func TestCanonicalizePathForSourceID_UNCRejection(t *testing.T) {
	// Note: We can't easily test actual UNC paths without Windows infrastructure,
	// but the UNC rejection logic is tested via canonicalizeAbsolutePath tests
	// and NewCanonicalPath_UNCRejection. This test documents the expected behavior.
	//
	// The UNC check in CanonicalizePathForSourceID happens after filepath.EvalSymlinks,
	// so we'd need a real UNC path that EvalSymlinks can resolve. Instead, we verify
	// the check exists by examining the code structure and testing via other paths.

	// Test that the error message format is correct when UNC is detected
	// by testing SourceIDFromAbsolutePath which uses canonicalizeAbsolutePath
	tests := []string{
		"//server/share/file.txt",
		"//server/share",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			_, err := SourceIDFromAbsolutePath(path)
			if err == nil {
				t.Errorf("SourceIDFromAbsolutePath(%q) should reject UNC path", path)
				return
			}
			if !errors.Is(err, ErrUNCPath) {
				t.Errorf("expected ErrUNCPath, got: %v", err)
			}
		})
	}
}

// TestNewSourceID_EmptyString documents that NewSourceID("") returns a zero-value
// SourceID (both cp and synthetic fields are empty). This is because NewSourceID
// bypasses validation. Use MustNewSourceID instead to catch empty identifiers.
func TestNewSourceID_EmptyString(t *testing.T) {
	sid := NewSourceID("")

	// Empty string produces a zero SourceID
	if !sid.IsZero() {
		t.Error("NewSourceID(\"\") should return zero SourceID")
	}

	// String() returns empty for zero SourceID
	if sid.String() != "" {
		t.Errorf("String() = %q; want \"\"", sid.String())
	}

	// Not a file path
	if sid.IsFilePath() {
		t.Error("zero SourceID should not be a file path")
	}

	// This demonstrates the problem: zero SourceIDs can cause map key anomalies
	// because the zero value is indistinguishable from an uninitialized SourceID.
	m := make(map[SourceID]int)
	m[sid] = 42

	var uninitializedSID SourceID
	if v, ok := m[uninitializedSID]; ok && v == 42 {
		// This is expected but potentially confusing behavior
		t.Log("zero SourceID and uninitialized SourceID are the same map key (expected but potentially confusing)")
	}
}

// TestNewSourceID_AbsolutePathCollision documents the collision risk when using
// NewSourceID with an absolute path string. This can cause issues because:
// 1. The synthetic identifier matches a file-backed SourceID's string representation
// 2. The two SourceIDs are NOT equal (different fields), but have the same String()
// 3. This breaks the injectivity invariant: different SourceIDs should have different strings
//
// Use MustNewSourceID instead, which validates that identifiers don't look like paths.
func TestNewSourceID_AbsolutePathCollision(t *testing.T) {
	// Create a synthetic SourceID that looks like an absolute path
	// (NewSourceID doesn't validate, so this succeeds)
	syntheticID := NewSourceID("/absolute/path/file.yammm")

	// The synthetic ID is NOT a file path (it's stored in the synthetic field)
	if syntheticID.IsFilePath() {
		t.Error("synthetic ID should not be a file path")
	}

	// But its String() looks like one
	if syntheticID.String() != "/absolute/path/file.yammm" {
		t.Errorf("String() = %q; want %q", syntheticID.String(), "/absolute/path/file.yammm")
	}

	// Create a file-backed SourceID for the same path (if it existed)
	// For demonstration, we use SourceIDFromAbsolutePath which doesn't require
	// the file to exist.
	if runtime.GOOS != "windows" {
		fileID, err := SourceIDFromAbsolutePath("/absolute/path/file.yammm")
		if err != nil {
			t.Fatalf("SourceIDFromAbsolutePath failed: %v", err)
		}

		// The file-backed ID IS a file path
		if !fileID.IsFilePath() {
			t.Error("file-backed ID should be a file path")
		}

		// Both have the same String() representation (collision!)
		if syntheticID.String() != fileID.String() {
			t.Errorf("String() mismatch: synthetic=%q, file=%q",
				syntheticID.String(), fileID.String())
		}

		// But they are NOT equal as SourceIDs (different internal representation)
		if syntheticID == fileID {
			t.Error("synthetic and file-backed SourceIDs should not be equal (different fields)")
		}

		// This demonstrates the collision: two different SourceIDs with the same string
		// This breaks the injectivity invariant and can cause issues with:
		// - Map lookups (different keys, same string representation)
		// - Sorting/ordering (interleaved in output)
		// - Debugging (confusing output)
		t.Log("Collision demonstrated: different SourceIDs with same String() (use MustNewSourceID to prevent this)")
	}
}

// TestMustNewSourceID_RejectsEmptyString verifies that MustNewSourceID panics
// for empty strings, unlike NewSourceID which silently creates a zero SourceID.
func TestMustNewSourceID_RejectsEmptyString(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustNewSourceID(\"\") should panic")
		}
	}()

	MustNewSourceID("")
}

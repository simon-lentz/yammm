package location

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mustSourceID is [NewSourceID] for a test's own fixed name.
func mustSourceID(name string) SourceID {
	id, err := NewSourceID(name)
	if err != nil {
		panic(err)
	}
	return id
}

// TestNewSourceID holds the synthetic door to the identities it admits and the
// names it refuses: one that could not be told apart from another identity.
func TestNewSourceID(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"test://unit/person.yammm",
		"inline:schema",
		"<stdin>",
		"embedded://app/builtin.yammm",
		"relative/path",
		"just-a-name",
		"C:relative",
	} {
		id, err := NewSourceID(name)
		if err != nil {
			t.Errorf("NewSourceID(%q) = %v; want an identity", name, err)
			continue
		}
		if id.String() != name || id.IsFilePath() || id.IsZero() {
			t.Errorf("NewSourceID(%q) = %q, file %v, zero %v; want the name, synthetic", name, id, id.IsFilePath(), id.IsZero())
		}
		if !id.Dir().IsZero() {
			t.Errorf("NewSourceID(%q).Dir() = %q; a synthetic source has no directory", name, id.Dir())
		}
	}
	for _, tc := range []struct {
		name string
		want error
	}{
		{"", ErrEmptySourceID},
		{"/absolute", ErrAbsolutePathSourceID},
		{"C:/windows", ErrAbsolutePathSourceID},
		{"C:\\windows", ErrAbsolutePathSourceID},
		{"//unc", ErrAbsolutePathSourceID},
		{"\\\\unc", ErrAbsolutePathSourceID},
		{"inline:caf\xff", ErrInvalidUTF8Path},
		{"inline:a\nb", ErrUnprintableSourceID},
	} {
		id, err := NewSourceID(tc.name)
		if !errors.Is(err, tc.want) {
			t.Errorf("NewSourceID(%q) = %q, %v; want %v", tc.name, id, err, tc.want)
		}
		if !id.IsZero() {
			t.Errorf("NewSourceID(%q) refused the name but returned %q", tc.name, id)
		}
	}
}

// TestSourceID_Dir holds Dir to the identity of the directory that holds a
// file-backed source, and to the root at a volume root.
func TestSourceID_Dir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file, err := SourceIDFromPath(filepath.Join(dir, "a.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := SourceIDFromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := file.Dir(); got != want {
		t.Errorf("Dir() = %q; want %q", got, want)
	}
	root, err := SourceIDFromPath(filepath.VolumeName(dir) + string(filepath.Separator))
	if err != nil {
		t.Fatal(err)
	}
	if got := root.Dir(); got != root {
		t.Errorf("Dir() of the root = %q; want the root %q", got, root)
	}
	if got := (SourceID{}).Dir(); !got.IsZero() {
		t.Errorf("Dir() of the zero SourceID = %q; want zero", got)
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

// TestSourceIDFromAbsolutePath holds the constructor to the host's path rules:
// a path the host calls absolute is cleaned, normalized to NFC and written with
// forward slashes, and any other path is refused with ErrNotAbsolute.
func TestSourceIDFromAbsolutePath(t *testing.T) {
	type row struct {
		name    string
		input   string
		want    string
		wantErr error
	}
	rows := []row{
		{"empty path", "", "", ErrEmptyPath},
	}
	if runtime.GOOS == "windows" {
		rows = append(rows,
			row{"drive path", "C:/a/b", "C:/a/b", nil},
			row{"backslash separators", `C:\a\b`, "C:/a/b", nil},
			row{"dotdot to the drive root", "C:/a/..", "C:/", nil},
			row{"dotdot above the drive root keeps what follows", "C:/../x", "C:/x", nil},
			row{"network share with backslashes", `\\server\share\file.txt`, "//server/share/file.txt", nil},
			row{"network share with forward slashes", "//server/share/file.txt", "//server/share/file.txt", nil},
			row{"decomposed name", "C:/Users/cafe\u0301/file.txt", "C:/Users/caf\u00e9/file.txt", nil},
		)
	} else {
		rows = append(rows,
			row{"absolute path", "/a/b/c", "/a/b/c", nil},
			row{"dotdot", "/a/../b", "/b", nil},
			row{"double slash inside", "/a//b", "/a/b", nil},
			row{"dot", "/a/./b", "/a/b", nil},
			row{"backslash is a file-name character", `/path/with\backslash/file.txt`, `/path/with\backslash/file.txt`, nil},
			row{"trailing backslash is a file-name character", `/path/to/dir\`, `/path/to/dir\`, nil},
			row{"dotdot beside a backslash stays one name", `/a\..\b`, `/a\..\b`, nil},
			row{"leading double slash names the root", "//server/share/file.txt", "/server/share/file.txt", nil},
			row{"decomposed name", "/path/cafe\u0301/file.txt", "/path/caf\u00e9/file.txt", nil},
			row{"several decomposed characters", "/path/re\u0301sume\u0301.txt", "/path/r\u00e9sum\u00e9.txt", nil},
			row{"composed name stays composed", "/path/caf\u00e9/file.txt", "/path/caf\u00e9/file.txt", nil},
		)
	}

	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			sid, err := SourceIDFromPath(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SourceIDFromPath(%q) error = %v; want %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SourceIDFromPath(%q): %v", tt.input, err)
			}
			if !sid.IsFilePath() {
				t.Error("should be file-backed")
			}
			if got := sid.String(); got != tt.want {
				t.Errorf("SourceIDFromPath(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSourceIDFromAbsolutePath_CleaningEquivalence(t *testing.T) {
	// Paths with . and .. should produce equal SourceIDs to cleaned paths
	root := "/"
	if runtime.GOOS == "windows" {
		root = "C:/"
	}
	tests := []struct {
		dirty string
		clean string
	}{
		{root + "a/../b", root + "b"},
		{root + "a/./b", root + "a/b"},
		{root + "a//b", root + "a/b"},
	}

	for _, tt := range tests {
		t.Run(tt.dirty, func(t *testing.T) {
			dirtySID, err := SourceIDFromPath(tt.dirty)
			if err != nil {
				t.Fatalf("SourceIDFromPath(%q) failed: %v", tt.dirty, err)
			}

			cleanSID, err := SourceIDFromPath(tt.clean)
			if err != nil {
				t.Fatalf("SourceIDFromPath(%q) failed: %v", tt.clean, err)
			}

			if dirtySID != cleanSID {
				t.Errorf("SourceIDFromPath(%q) = %v; want equal to %v", tt.dirty, dirtySID, cleanSID)
			}
		})
	}
}

// TestSourceIDFromAbsolutePath_LeadingDoubleSlash pins the Unix reading of a
// leading "//": the kernel resolves it as the root, so it names the same file.
func TestSourceIDFromAbsolutePath_LeadingDoubleSlash(t *testing.T) {
	skipOnWindows(t, "a leading // names a network share on Windows")

	want, err := SourceIDFromPath("/server/share")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"//server/share", "///server/share"} {
		got, err := SourceIDFromPath(in)
		if err != nil {
			t.Fatalf("SourceIDFromPath(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("SourceIDFromPath(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestSourceID_IsZero(t *testing.T) {
	var zeroSID SourceID
	if !zeroSID.IsZero() {
		t.Error("zero value should report IsZero() == true")
	}

	syntheticSID := mustSourceID("test://unit")
	if syntheticSID.IsZero() {
		t.Error("synthetic SourceID should not be zero")
	}

	fileSID, _ := SourceIDFromPath(".")
	if fileSID.IsZero() {
		t.Error("file-backed SourceID should not be zero")
	}
}

func TestSourceID_String(t *testing.T) {
	syntheticSID := mustSourceID("test://unit/person.yammm")
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

func TestSourceID_Equality(t *testing.T) {
	// Synthetic SourceIDs
	sid1 := mustSourceID("test://unit")
	sid2 := mustSourceID("test://unit")
	sid3 := mustSourceID("test://other")

	if sid1 != sid2 {
		t.Error("equal synthetic SourceIDs should be equal")
	}
	if sid1 == sid3 {
		t.Error("different synthetic SourceIDs should not be equal")
	}

	// File-backed SourceIDs
	if runtime.GOOS != "windows" {
		path1, _ := SourceIDFromPath("/a/b/c")
		path2, _ := SourceIDFromPath("/a/b/c")
		path3, _ := SourceIDFromPath("/a/b/d")

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
	sid1 := mustSourceID("test://unit")
	sid2 := mustSourceID("test://unit")

	m := make(map[SourceID]int)
	m[sid1] = 42

	if m[sid2] != 42 {
		t.Error("equal SourceIDs should work as map keys")
	}
}

func TestSourceID_CaseSensitivity(t *testing.T) {
	// Case is read from the filesystem, so a spelling nothing on disk answers
	// for keeps its typed case.
	if runtime.GOOS != "windows" {
		upper, _ := SourceIDFromPath("/nonexistent-root-7f3a/Simon/schema.yammm")
		lower, _ := SourceIDFromPath("/nonexistent-root-7f3a/simon/schema.yammm")

		if upper == lower {
			t.Error("different-case spellings under a missing root should stay distinct")
		}
	}
}

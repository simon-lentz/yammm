package diag

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestModuleRoot_TextLocations holds text locations over file-backed sources
// to one rule: the root a caller passes is a host path, and a source under it
// is written relative to it, whatever bytes the host gives the root.
func TestModuleRoot_TextLocations(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fileID := func(t *testing.T, p string) location.SourceID {
		t.Helper()
		id, err := location.SourceIDFromAbsolutePath(p)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mkdir := func(t *testing.T, p string) string {
		t.Helper()
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p
	}

	rows := []struct {
		name     string
		unixOnly bool
		setup    func(t *testing.T) (root string, src location.SourceID, want string)
	}{
		{
			name: "the root as the host spells it",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				dir := mkdir(t, filepath.Join(base, "plain"))
				return dir, fileID(t, filepath.Join(dir, "a.yammm")), "a.yammm"
			},
		},
		{
			name: "a source below the root",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				dir := mkdir(t, filepath.Join(base, "nested"))
				return dir, fileID(t, filepath.Join(dir, "sub", "a.yammm")), "sub/a.yammm"
			},
		},
		{
			name: "a root with a trailing separator",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				dir := mkdir(t, filepath.Join(base, "trail"))
				return dir + string(filepath.Separator), fileID(t, filepath.Join(dir, "a.yammm")), "a.yammm"
			},
		},
		{
			name:     "a root of /",
			unixOnly: true,
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				src := fileID(t, filepath.Join(base, "slash", "a.yammm"))
				return "/", src, strings.TrimPrefix(src.String(), "/")
			},
		},
		{
			name: "a file given as the root",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				p := filepath.Join(mkdir(t, filepath.Join(base, "file")), "a.yammm")
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				src := fileID(t, p)
				return p, src, src.String()
			},
		},
		{
			name: "a sibling directory that shares the root's prefix",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				dir := mkdir(t, filepath.Join(base, "p"))
				src := fileID(t, filepath.Join(mkdir(t, filepath.Join(base, "pq")), "a.yammm"))
				return dir, src, src.String()
			},
		},
		{
			name: "a root directory with a decomposed name",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				dir := mkdir(t, filepath.Join(base, "cafe\u0301"))
				return dir, fileID(t, filepath.Join(dir, "a.yammm")), "a.yammm"
			},
		},
		{
			name: "a synthetic source",
			setup: func(*testing.T) (string, location.SourceID, string) {
				return base, location.MustNewSourceID("test://a.yammm"), "test://a.yammm"
			},
		},
		{
			name: "no module root",
			setup: func(t *testing.T) (string, location.SourceID, string) {
				t.Helper()
				src := fileID(t, filepath.Join(base, "none", "a.yammm"))
				return "", src, src.String()
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.unixOnly && runtime.GOOS == "windows" {
				t.Skip("a root of / names no volume on Windows")
			}
			root, src, want := row.setup(t)
			out := formatIssue(NewRenderer(WithModuleRoot(root)),
				NewIssue(Error, E_SYNTAX, "msg").WithSpan(location.Point(src, 1, 1)).Build())
			got, _, _ := strings.Cut(out, ": error[")
			if want += ":1:1"; got != want {
				t.Errorf("location under root %q\n got %q\nwant %q", root, got, want)
			}
		})
	}
}

// TestRenderContracts holds what diag hands a caller: a JSON document keeps
// each span's identity whatever module root the renderer holds, an error
// string ends at its last issue, and an issue with no location renders none.
func TestRenderContracts(t *testing.T) {
	t.Parallel()

	failed := func() Result {
		c := NewCollector(0)
		c.Collect(NewIssue(Error, E_SYNTAX, "first").Build())
		c.Collect(NewIssue(Warning, E_INVALID_NAME, "second").Build())
		return c.Result()
	}
	endsAtItsLastIssue := func(s string) (bool, string) {
		return !strings.HasSuffix(s, "\n"), fmt.Sprintf("%q", s)
	}

	knownBroken := map[string]string{
		"Result.String ends at its last issue":                    "Result.String writes a newline after every issue (B16)",
		"an error from Result.Err ends at its last issue":         "the error string is Result.String (B16)",
		"an error from Result.WithContext ends at its last issue": "the error string ends in Result.String's newline (B16)",
		"an issue with no location renders no location":           "the renderer writes \"<unknown>: \" (B42)",
	}

	rows := []struct {
		name string
		run  func(t *testing.T) (bool, string)
	}{
		{
			name: "JSON keeps each span's identity under a module root",
			run: func(t *testing.T) (bool, string) {
				t.Helper()
				base, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				id, err := location.SourceIDFromAbsolutePath(filepath.Join(base, "a.yammm"))
				if err != nil {
					t.Fatal(err)
				}
				c := NewCollector(0)
				c.Collect(NewIssue(Error, E_SYNTAX, "msg").
					WithSpan(location.Point(id, 1, 1)).
					WithRelated(location.RelatedInfo{Message: "here", Span: location.Point(id, 2, 1)}).
					Build())
				type span struct {
					Source string `json:"source"`
				}
				var doc struct {
					Issues []struct {
						Span    span `json:"span"`
						Related []struct {
							Span span `json:"span"`
						} `json:"related"`
					} `json:"issues"`
				}
				if err := json.Unmarshal(NewRenderer(WithModuleRoot(base)).FormatResultJSON(c.Result()), &doc); err != nil {
					t.Fatal(err)
				}
				ok := len(doc.Issues) == 1 && doc.Issues[0].Span.Source == id.String() &&
					len(doc.Issues[0].Related) == 1 && doc.Issues[0].Related[0].Span.Source == id.String()
				return ok, fmt.Sprintf("document %+v, want every source %q", doc, id)
			},
		},
		{
			name: "Result.String ends at its last issue",
			run:  func(*testing.T) (bool, string) { return endsAtItsLastIssue(failed().String()) },
		},
		{
			name: "an error from Result.Err ends at its last issue",
			run:  func(*testing.T) (bool, string) { return endsAtItsLastIssue(failed().Err().Error()) },
		},
		{
			name: "an error from Result.WithContext ends at its last issue",
			run:  func(*testing.T) (bool, string) { return endsAtItsLastIssue(failed().WithContext("probe").Error()) },
		},
		{
			name: "an issue with no location renders no location",
			run: func(*testing.T) (bool, string) {
				out := formatIssue(NewRenderer(), NewIssue(Error, E_SYNTAX, "msg").Build())
				return out == "error[E_SYNTAX]: msg", fmt.Sprintf("%q", out)
			},
		},
	}

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			ok, detail := row.run(t)
			reason, broken := knownBroken[row.name]
			switch {
			case broken && ok:
				t.Errorf("listed as broken (%s) and now passes: remove its knownBroken entry", reason)
			case broken:
				t.Logf("known broken: %s: %s", reason, detail)
			case !ok:
				t.Error(detail)
			}
		})
	}
	for name := range knownBroken {
		if !names[name] {
			t.Errorf("knownBroken names no row: %q", name)
		}
	}
}

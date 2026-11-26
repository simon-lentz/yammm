package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fmtSyntaxError is a schema the grammar does not match at line 2.
const fmtSyntaxError = "schema \"test\"\ntype {\n"

// A file whose path resolves to no identity still draws the positioned
// E_SYNTAX that any other file draws, named by its quoted spelling, as gofmt
// names a file by its path as typed.
func TestFmt_PositionsASyntaxErrorInAFileNoIdentityNames(t *testing.T) {
	t.Parallel()
	paths := map[string]string{}
	if runtime.GOOS == "darwin" {
		paths["a path that resolves past PATH_MAX"] = pastPathMax(t)
	}
	if p := filepath.Join(t.TempDir(), "\xff.yammm"); os.WriteFile(p, []byte(fmtSyntaxError), 0o600) == nil {
		if _, err := os.Stat(p); err == nil {
			paths["a name that is not valid UTF-8"] = p
		}
	}
	if len(paths) == 0 {
		t.Skip("this host makes no path that resolves to no identity")
	}
	for name, path := range paths {
		code, _, stderr := executeCmdOutput(t, "fmt", path)
		if code != 1 || !strings.Contains(stderr, strconv.Quote(path)+":2:") || !strings.Contains(stderr, "E_SYNTAX") {
			t.Errorf("%s: exit %d, want the positioned E_SYNTAX naming %s: %s", name, code, strconv.Quote(path), stderr)
		}
		code, _, stderr = executeCmdOutput(t, "--json", "fmt", path)
		var doc struct {
			Issues []struct {
				Code string `json:"code"`
				Span *struct {
					Source string `json:"source"`
					Start  struct {
						Line int `json:"line"`
					} `json:"start"`
				} `json:"span"`
			} `json:"issues"`
		}
		if err := json.Unmarshal([]byte(stderr), &doc); err != nil {
			t.Fatalf("%s --json: stderr is no JSON document: %v\n%s", name, err, stderr)
		}
		if code != 1 || len(doc.Issues) != 1 || doc.Issues[0].Code != "E_SYNTAX" || doc.Issues[0].Span == nil ||
			doc.Issues[0].Span.Source != strconv.Quote(path) || doc.Issues[0].Span.Start.Line != 2 {
			t.Errorf("%s --json: exit %d, want one E_SYNTAX at line 2 of %s: %s", name, code, strconv.Quote(path), stderr)
		}
	}
}

// pastPathMax returns base/hop/hop/bad.yammm, which the kernel reads and whose
// resolved form exceeds darwin's PATH_MAX: base/hop and chain/hop each name the
// relative chain below them. darwin refuses a mkdir by a path that resolves that
// far, so the tree is built through a directory descriptor.
func pastPathMax(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	chain := filepath.Join(slices.Repeat([]string{strings.Repeat("d", 200)}, 4)...)
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll(filepath.Join(chain, chain), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"hop", filepath.Join(chain, "hop")} {
		if err := root.Symlink(chain, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	if err := root.WriteFile(filepath.Join(chain, chain, "bad.yammm"), []byte(fmtSyntaxError), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "hop", "hop", "bad.yammm")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat through the links: %v", err)
	}
	return path
}

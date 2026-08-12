package buildversion_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// versionSites are the in-tree files the release policy holds at one version.
// The tag and the two binaries carry it too, but only these are checkable
// without a build: a Go test cannot see the tag it will be built from.
var versionSites = []struct {
	path string
	keys [][]string // JSON paths to every version field in that file
}{
	{
		path: filepath.Join("claude-plugin", ".claude-plugin", "plugin.json"),
		keys: [][]string{{"version"}},
	},
	{
		path: filepath.Join("lsp", "editors", "vscode", "package.json"),
		keys: [][]string{{"version"}},
	},
	{
		// npm writes the version twice; a bump that misses the nested copy
		// leaves the published VSIX describing the wrong release.
		path: filepath.Join("lsp", "editors", "vscode", "package-lock.json"),
		keys: [][]string{{"version"}, {"packages", "", "version"}},
	},
}

// Every in-tree version site agrees. The lockstep policy requires one version
// across the tag, both binaries, the Claude plugin and the VS Code extension,
// and until this test nothing enforced any of it — package-lock.json sat at
// 0.10.0 through two releases because no gate compared it to anything.
func TestVersionLockstep_EveryInTreeSiteAgrees(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	seen := map[string][]string{} // version -> the sites reporting it
	for _, site := range versionSites {
		full := filepath.Join(root, site.path)
		raw, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %s: %v", site.path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", site.path, err)
		}
		for _, key := range site.keys {
			v, err := lookupString(doc, key)
			if err != nil {
				t.Fatalf("%s: %v", site.path, err)
			}
			label := site.path
			if len(key) > 1 {
				label += " (nested)"
			}
			seen[v] = append(seen[v], label)
		}
	}

	if len(seen) != 1 {
		t.Errorf("version sites disagree: %v", seen)
	}
}

func lookupString(doc map[string]any, key []string) (string, error) {
	var cur any = doc
	for _, k := range key {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", &lookupError{key: key, reason: "not an object at " + k}
		}
		cur, ok = m[k]
		if !ok {
			return "", &lookupError{key: key, reason: "missing key " + k}
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", &lookupError{key: key, reason: "not a string"}
	}
	return s, nil
}

type lookupError struct {
	key    []string
	reason string
}

func (e *lookupError) Error() string {
	return "version lookup " + filepath.Join(e.key...) + ": " + e.reason
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test's working directory")
		}
		dir = parent
	}
}

package wiringtest

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

// trackedTree holds the files git tracks at the repository root, to answer
// whether a path a configuration names is one the tree holds.
type trackedTree map[string]bool

func newTrackedTree(t *testing.T) trackedTree {
	t.Helper()
	tree := trackedTree{}
	for _, rel := range trackedFiles(t, repoRoot, func(string) bool { return true }) {
		if _, err := os.Stat(fromRoot(rel)); err == nil {
			tree[rel] = true
		}
	}
	return tree
}

// holdsFile reports whether rel, slash-separated, is a tracked file.
func (tr trackedTree) holdsFile(rel string) bool { return tr[rel] }

// holdsDir reports whether rel, slash-separated, holds a tracked file. The
// root, "." or "", holds every one.
func (tr trackedTree) holdsDir(rel string) bool {
	rel = strings.TrimSuffix(strings.TrimPrefix(rel, "./"), "/")
	if rel == "" || rel == "." {
		return len(tr) > 0
	}
	for file := range tr {
		if strings.HasPrefix(file, rel+"/") {
			return true
		}
	}
	return false
}

// literalPath reports whether a workflow value is a repository path: one with no
// expression, no home directory and no absolute root.
func literalPath(v string) bool {
	return v != "" && !strings.Contains(v, "${{") && !strings.HasPrefix(v, "~") && !strings.HasPrefix(v, "/")
}

// TestWorkflows_NamePathsTheTreeHolds holds every repository path a workflow
// names to the tracked tree: each working directory, each go-version-file and
// node-version-file, and the directory each upload-artifact path globs in. A
// working directory the tree lacks fails its step in CI alone, and an upload
// path it lacks uploads nothing with only a warning.
func TestWorkflows_NamePathsTheTreeHolds(t *testing.T) {
	t.Parallel()
	tree := newTrackedTree(t)
	checked := 0
	dir := func(where, rel string) {
		if !literalPath(rel) {
			return
		}
		checked++
		if !tree.holdsDir(rel) {
			t.Errorf("%s names the directory %s, which holds no tracked file", where, rel)
		}
	}
	for file, wf := range trackedWorkflows(t, repoRoot) {
		dir(file+" defaults", wf.Defaults.Run.WorkingDirectory)
		for name, j := range wf.Jobs {
			dir(file+" job "+name+" defaults", j.Defaults.Run.WorkingDirectory)
			for i, s := range j.Steps {
				where := fmt.Sprintf("%s job %s step %d", file, name, i)
				dir(where, s.WorkDir)
				for _, key := range []string{"go-version-file", "node-version-file"} {
					if v, ok := s.With[key].(string); ok && literalPath(v) {
						checked++
						if !tree.holdsFile(path.Clean(v)) {
							t.Errorf("%s names the %s %s, which is no tracked file", where, key, v)
						}
					}
				}
				if strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
					if v, ok := s.With["path"].(string); ok && strings.ContainsAny(v, "*?[") {
						dir(where+" upload", path.Dir(v))
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no workflow names a repository path; the reading is not reaching the workflows")
	}
}

// goBuildOutput matches the output path of a go build in a run step.
var goBuildOutput = regexp.MustCompile(`go build\b[^\n]*?(?:\\\n[^\n]*?)*?-o\s+"?([^"\s\\]+)`)

// TestExtensionBuilds_PutTheServerInThePackagedExtension holds each job that
// packages the VS Code extension to build the language server under that
// extension's bin directory. go build creates any directory it is given, and
// vsce packages a directory without one, so a server built anywhere else ships
// a VSIX with no server and fails no step.
func TestExtensionBuilds_PutTheServerInThePackagedExtension(t *testing.T) {
	t.Parallel()
	jobs := 0
	for file, wf := range trackedWorkflows(t, repoRoot) {
		for name, j := range wf.Jobs {
			ext := ""
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "vsce package") {
					ext = strings.TrimSuffix(s.WorkDir, "/")
				}
			}
			if ext == "" {
				continue
			}
			jobs++
			builds := 0
			for _, s := range j.Steps {
				for _, m := range goBuildOutput.FindAllStringSubmatch(s.Run, -1) {
					builds++
					if !strings.HasPrefix(m[1], ext+"/bin/") {
						t.Errorf("%s job %s builds %s, outside %s/bin/, the extension it packages", file, name, m[1], ext)
					}
				}
			}
			if builds == 0 {
				t.Errorf("%s job %s packages %s and builds no server into it", file, name, ext)
			}
		}
	}
	if jobs < 2 {
		t.Errorf("found %d jobs that package the extension, want CI's build and the publish workflow's", jobs)
	}
}

// TestDependabot_NamesEachManifestsDirectory holds each dependabot update to a
// directory that holds its ecosystem's manifest. Dependabot reports a directory
// with no manifest in its own log and opens no pull request.
func TestDependabot_NamesEachManifestsDirectory(t *testing.T) {
	t.Parallel()
	tree := newTrackedTree(t)
	var cfg struct {
		Updates []struct {
			Ecosystem string `yaml:"package-ecosystem"`
			Directory string `yaml:"directory"`
		} `yaml:"updates"`
	}
	decodeYAML(t, fromRoot(".github/dependabot.yml"), &cfg)
	manifest := map[string]string{"gomod": "go.mod", "npm": "package.json"}
	checked := 0
	for _, u := range cfg.Updates {
		dir := strings.Trim(u.Directory, "/")
		switch u.Ecosystem {
		case "github-actions":
			checked++
			if !tree.holdsDir(path.Join(dir, ".github/workflows")) {
				t.Errorf("dependabot's github-actions update names %s, which holds no .github/workflows", u.Directory)
			}
		default:
			name, ok := manifest[u.Ecosystem]
			if !ok {
				t.Errorf("dependabot names the ecosystem %s, which this test does not know", u.Ecosystem)
				continue
			}
			checked++
			if !tree.holdsFile(path.Join(dir, name)) {
				t.Errorf("dependabot's %s update names %s, which holds no tracked %s", u.Ecosystem, u.Directory, name)
			}
		}
	}
	if checked == 0 {
		t.Error("dependabot.yml names no update")
	}
}

// TestMakefile_NamesTheExtensionDirectory holds VSCODE_EXT to the directory of
// the extension's tracked manifest: copy-to-vscode copies into it, and clean
// removes its bin directory, which no one sees go missing.
func TestMakefile_NamesTheExtensionDirectory(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(fromRoot("Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^VSCODE_EXT\s*=\s*(\S+)\s*$`).FindAllSubmatch(b, -1)
	if len(m) != 1 {
		t.Fatalf("the Makefile sets VSCODE_EXT on %d lines, want one", len(m))
	}
	if ext := string(m[0][1]); !newTrackedTree(t).holdsFile(path.Join(ext, "package.json")) {
		t.Errorf("VSCODE_EXT is %s, which holds no tracked package.json", ext)
	}
}

// TestGitignore_NamesDirectoriesTheTreeHolds holds each .gitignore pattern
// below a directory to a directory the tree holds. A pattern under a moved
// directory ignores nothing, and the build output it named reaches the gates
// as untracked files. A pattern under a dot directory names a tool's own tree,
// which nothing tracks.
func TestGitignore_NamesDirectoriesTheTreeHolds(t *testing.T) {
	t.Parallel()
	tree := newTrackedTree(t)
	b, err := os.ReadFile(fromRoot(".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		pattern := strings.TrimSuffix(strings.TrimSpace(line), "/")
		if pattern == "" || strings.HasPrefix(pattern, "#") || strings.HasPrefix(pattern, "!") {
			continue
		}
		pattern = strings.TrimPrefix(pattern, "/")
		dir := path.Dir(pattern)
		if dir == "." || strings.HasPrefix(dir, ".") || strings.ContainsAny(dir, "*?[") {
			continue
		}
		checked++
		if !tree.holdsDir(dir) {
			t.Errorf(".gitignore's %q lies under %s, which holds no tracked file", strings.TrimSpace(line), dir)
		}
	}
	if checked == 0 {
		t.Error(".gitignore names no pattern below a directory")
	}
}

// resolveCall matches a `const <name> = path.resolve(<base>, '..', ...)` line of
// a Node script, its base and its '..' arguments.
var resolveCall = regexp.MustCompile(`(?m)^const (\w+) = path\.resolve\((.+?)((?:, '\.\.')*)\);$`)

// TestExtensionIntegration_BuildsTheServerFromTheRepositoryRoot resolves the
// integration runner's extensionRoot and repoRoot from the script's own
// directory: the first holds the extension's package.json, the second the
// go.mod and cmd/yammm-lsp the runner builds. Only CI's integration job runs the
// script, so a root one level off fails there alone.
func TestExtensionIntegration_BuildsTheServerFromTheRepositoryRoot(t *testing.T) {
	t.Parallel()
	const script = "editors/vscode/tests/integration/runTest.mjs"
	tree := newTrackedTree(t)
	b, err := os.ReadFile(fromRoot(script))
	if err != nil {
		t.Fatal(err)
	}
	roots := map[string]string{}
	for _, m := range resolveCall.FindAllStringSubmatch(string(b), -1) {
		base, ups := m[2], strings.Count(m[3], "'..'")
		switch base {
		case "path.dirname(fileURLToPath(import.meta.url))":
			base = path.Dir(script)
		default:
			var ok bool
			if base, ok = roots[base]; !ok {
				continue
			}
		}
		// path.Join keeps a ".." that climbs above the root, where path.Dir
		// would stop at ".".
		for range ups {
			base = path.Join(base, "..")
		}
		roots[m[1]] = base
	}
	ext, ok := roots["extensionRoot"]
	if !ok || !tree.holdsFile(path.Join(ext, "package.json")) {
		t.Errorf("extensionRoot resolves to %q, which holds no tracked package.json", ext)
	}
	repo, ok := roots["repoRoot"]
	if !ok || repo != "." || !tree.holdsFile("go.mod") || !tree.holdsDir("cmd/yammm-lsp") {
		t.Errorf("repoRoot resolves to %q, want the repository root, which holds go.mod and cmd/yammm-lsp", repo)
	}
}

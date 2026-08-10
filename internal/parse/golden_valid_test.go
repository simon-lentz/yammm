package parse

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// Golden set 1 — the valid corpus. Every tracked .yammm fixture that parses
// without a failure diagnostic has its projection frozen here, so a
// regression in what this parser accepts shows up as a golden diff.
//
// This set and the casebook in golden_casebook_test.go are the two halves of
// the parity contract. They exist because the differential harness that used
// to check both against a second implementation is untracked, so retiring it
// removes its corpora from the repository entirely: whatever is not frozen
// here has no regression test at all.

// skipDirs are trees the fixture walk never descends into: not tracked
// fixture corpus, or owned by golden set 2.
var skipDirs = map[string]bool{
	".git":         true,
	".claude":      true,
	"node_modules": true,
	".vscode-test": true,
	"bin":          true,
	"casebook":     true, // golden set 2's sources are malformed by design
}

// corpusEntry is one fixture and the verdict that partitions it.
type corpusEntry struct {
	Path  string   // repo-relative, slash-separated
	Src   string   // not frozen; the fixture file is the record
	Clean bool     // no Fatal or Error diagnostic
	Codes []string // the failure codes that excluded it, when not clean
}

func TestGoldenValidCorpus(t *testing.T) {
	entries := walkCorpus(t)
	if len(entries) < 100 {
		t.Fatalf("corpus walk found %d fixtures; the tracked corpus is far larger, so the root is wrong", len(entries))
	}
	for _, e := range entries {
		if !e.Clean {
			continue
		}
		t.Run(e.Path, func(t *testing.T) {
			got, _ := project(e.Path, e.Src)
			yammmtest.GoldenJSON(t, filepath.Join("golden", "valid", e.Path), got)
		})
	}
}

// TestGoldenValidCorpusPartition freezes which fixtures parse clean and which
// do not, with the codes that excluded each. Without it a fixture could stop
// parsing and silently leave set 1 instead of failing its own golden.
func TestGoldenValidCorpusPartition(t *testing.T) {
	entries := walkCorpus(t)
	var b strings.Builder
	clean, excluded := 0, 0
	for _, e := range entries {
		if e.Clean {
			clean++
			continue
		}
		excluded++
	}
	b.WriteString("# Golden set 1 — corpus partition\n")
	b.WriteString("# Frozen so a fixture cannot leave the clean set unnoticed.\n\n")
	b.WriteString("## clean (projection frozen under golden/valid/)\n")
	for _, e := range entries {
		if e.Clean {
			b.WriteString(e.Path + "\n")
		}
	}
	b.WriteString("\n## excluded (fails production parse by design; covered by its owning package tests)\n")
	for _, e := range entries {
		if !e.Clean {
			b.WriteString(e.Path + "\t" + strings.Join(e.Codes, ",") + "\n")
		}
	}
	yammmtest.Golden(t, filepath.Join("golden", "valid_partition"), []byte(b.String()))
	t.Logf("corpus: %d fixtures, %d clean, %d excluded", len(entries), clean, excluded)
}

// walkCorpus reads every tracked .yammm fixture under the repo root and
// partitions it by whether the parse reports a failure.
func walkCorpus(tb testing.TB) []corpusEntry {
	tb.Helper()
	root := repoRoot(tb)
	var out []corpusEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yammm") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		e := corpusEntry{Path: rel, Src: string(b), Clean: true}
		_, issues := project(rel, e.Src)
		for _, iss := range issues {
			if iss.Severity().IsFailure() {
				e.Clean = false
				e.Codes = append(e.Codes, iss.Code().String())
			}
		}
		sort.Strings(e.Codes)
		e.Codes = dedupeSorted(e.Codes)
		out = append(out, e)
		return nil
	})
	if err != nil {
		tb.Fatalf("walk corpus: %v", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func dedupeSorted(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// repoRoot derives the yammm checkout from this file's own path rather than
// from the working directory, which a benchmark once resolved wrongly and
// measured a corpus nobody intended.
func repoRoot(tb testing.TB) string {
	tb.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller: no caller information")
	}
	for dir := filepath.Dir(self); ; {
		if isModuleRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatalf("no yammm module root above %s", self)
		}
		dir = parent
	}
}

// isModuleRoot reports whether dir holds yammm's own go.mod. Nested modules
// exist in this tree, so only the module path distinguishes them.
func isModuleRoot(dir string) bool {
	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(content), "module github.com/simon-lentz/yammm\n")
}

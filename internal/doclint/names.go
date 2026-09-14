package doclint

import (
	"context"
	"fmt"
	"go/ast"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// AssertProcessFreeNames reports every path under root, and every Test, Fuzz,
// Benchmark and Example function in the module, whose name carries a process
// reference. It returns how many paths and test functions it read, so the
// caller can hold a floor. The package documentation states the vocabulary.
func AssertProcessFreeNames(t TB, root string) (paths, tests int) {
	t.Helper()
	files, err := trackedPaths(context.Background(), root)
	if err != nil {
		t.Errorf("listing the paths of %s: %v", root, err)
		return 0, 0
	}
	for _, rel := range files {
		paths++
		for seg := range strings.SplitSeq(rel, "/") {
			if tok := processToken(seg); tok != "" {
				t.Errorf("%s: the name %q carries the process reference %q; name it for what it holds", rel, seg, tok)
				break
			}
		}
	}

	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return paths, 0
	}
	for _, pkg := range m.Packages {
		for _, f := range pkg.files {
			if !isTestFile(pkg.fset.Position(f.Package).Filename) {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !isTestFunc(fn.Name.Name) {
					continue
				}
				tests++
				if tok := processToken(fn.Name.Name); tok != "" {
					t.Errorf("%s: %s carries the process reference %q; name it for what it pins", pkg.fset.Position(fn.Pos()), fn.Name.Name, tok)
				}
			}
		}
	}
	return paths, tests
}

// trackedPaths returns every path git tracks under root, slash-separated and
// relative to root, testdata included. Outside a work tree it walks the
// filesystem and skips node_modules and dot-directories.
func trackedPaths(ctx context.Context, root string) ([]string, error) {
	if gitTracks(ctx, root) {
		out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z").Output()
		if err != nil {
			return nil, fmt.Errorf("listing the tracked paths of %s: %w", root, err)
		}
		var paths []string
		for name := range strings.SplitSeq(string(out), "\x00") {
			if name != "" {
				paths = append(paths, name)
			}
		}
		return paths, nil
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("relativizing %s: %w", p, err)
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	return paths, nil
}

// isTestFunc reports whether name is a function the go test runner calls.
func isTestFunc(name string) bool {
	for _, prefix := range []string{"Test", "Fuzz", "Benchmark", "Example"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var (
	processWords = map[string]bool{"residue": true, "slate": true, "tranche": true}
	stageWords   = map[string]bool{
		"tier": true, "group": true, "round": true, "unit": true, "step": true, "phase": true,
		"wave": true, "pass": true, "batch": true, "stage": true, "sweep": true,
	}
	numberedStage = regexp.MustCompile(`^(tier|group|round|unit|step|phase|wave|pass|batch|stage|sweep)[0-9]+$`)
	rowID         = regexp.MustCompile(`^[a-z][0-9]+$`)
	// A version (v2) and constant time (o1) share the row-identifier shape.
	legitimateRowShape = regexp.MustCompile(`^(v[0-9]+|o1)$`)
	number             = regexp.MustCompile(`^[0-9]+$`)
)

// processToken returns the first word of name that is a process reference, or
// "" when there is none.
func processToken(name string) string {
	ws := words(name)
	for i, w := range ws {
		next := ""
		if i+1 < len(ws) {
			next = ws[i+1]
		}
		switch {
		case processWords[w], w == "fixpass", w == "fixdiff", numberedStage.MatchString(w):
			return w
		case rowID.MatchString(w) && !legitimateRowShape.MatchString(w):
			return w
		case w == "gate" && (next == "fix" || next == "fixes"),
			w == "fix" && (next == "pass" || next == "diff"),
			stageWords[w] && number.MatchString(next):
			return w + "_" + next
		}
	}
	return ""
}

// words splits name into lowercase words at every non-alphanumeric rune and at
// camel-case boundaries. A digit run stays on the word before it, and an
// all-lowercase run such as a hexadecimal hash is one word.
func words(name string) []string {
	var out []string
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, part := range parts {
		rs := []rune(part)
		start := 0
		for i := 1; i < len(rs); i++ {
			prev, cur := rs[i-1], rs[i]
			if !unicode.IsUpper(cur) {
				continue
			}
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				out = append(out, strings.ToLower(string(rs[start:i])))
				start = i
			}
		}
		out = append(out, strings.ToLower(string(rs[start:])))
	}
	return out
}

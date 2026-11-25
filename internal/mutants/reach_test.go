package main

import (
	"path/filepath"
	"testing"
)

// A test binary go list reports it cannot load, as one whose test file names
// no package, still compiles the file: the mutant is judged, and reads NOBUILD.
// The entries are go list's at go1.26.10 for such a test file.
func TestWorker_CompiledHoldsAFileATestBinaryThatFailsToLoadCompiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := &worker{tree: &tree{dir: dir}}
	pkg := filepath.Join(dir, "a")
	broken := &struct{ Err string }{Err: "expected 'IDENT', found 'EOF'"}
	entries := []listed{
		{ImportPath: "ex/a", Dir: pkg, Name: "a", GoFiles: []string{"a.go"}, TestGoFiles: []string{"a_test.go", "x_test.go"}, Error: broken},
		{ImportPath: "ex/a [ex/a.test]", Dir: pkg, Name: "a", GoFiles: []string{"a.go", "a_test.go", "x_test.go"}, Error: broken},
		{ImportPath: "ex/a.test", Dir: pkg, Name: "main", Deps: []string{"ex/a [ex/a.test]"}, Error: broken},
	}
	if r, known := w.compiled(entries, filepath.Join("a", "x_test.go")); r != reachTested || !known {
		t.Errorf("x_test.go: reach %d, known %t; want a test binary's and known", r, known)
	}
	if r, known := w.compiled(entries, filepath.Join("a", "other.go")); r != reachNone || known {
		t.Errorf("other.go: reach %d, known %t; want none and unknown", r, known)
	}
	for _, e := range []listed{{MFiles: []string{"f"}}, {FFiles: []string{"f"}}, {SwigFiles: []string{"f"}}, {SwigCXXFiles: []string{"f"}}, {InvalidGoFiles: []string{"f"}}} {
		e.ImportPath, e.Dir, e.TestGoFiles = "ex/b", filepath.Join(dir, "b"), []string{"b_test.go"}
		binary := listed{ImportPath: "ex/b.test", Dir: e.Dir, Name: "main", Deps: []string{"ex/b"}}
		if r, _ := w.compiled([]listed{e, binary}, filepath.Join("b", "f")); r != reachTested {
			t.Errorf("a file go list names in %+v reaches %d, want a test binary's", e, r)
		}
	}
}

// A package the run builds holds the file, and no test binary links it: a
// named package with no test files, its import, a command in a directory named
// like a test binary, and a main package named for a package with no test files.
func TestWorker_CompiledTellsABuiltFileFromATestedOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := &worker{tree: &tree{dir: dir}}
	m := listed{ImportPath: "ex/m", Dir: filepath.Join(dir, "m"), Name: "m", GoFiles: []string{"m.go"}}
	for name, entries := range map[string][]listed{
		"a named package with no test files": {m},
		"its importer":                       {m, {ImportPath: "ex/nt", Dir: filepath.Join(dir, "nt"), Name: "nt", GoFiles: []string{"nt.go"}, Deps: []string{"ex/m"}}},
		"a command named like a test binary": {
			m,
			{ImportPath: "ex/tool.test", Dir: filepath.Join(dir, "tool.test"), Name: "main", GoFiles: []string{"main.go"}, Deps: []string{"ex/m"}},
			{ImportPath: "ex/tool", Dir: filepath.Join(dir, "tool"), Name: "tool", GoFiles: []string{"t.go"}, TestGoFiles: []string{"t_test.go"}},
		},
		"a main package named for a package with no test files": {m, {ImportPath: "ex/m.test", Dir: m.Dir, Name: "main", Deps: []string{"ex/m"}}},
	} {
		if r, known := w.compiled(entries, filepath.Join("m", "m.go")); r != reachBuilt || !known {
			t.Errorf("%s: reach %d, known %t; want built and known", name, r, known)
		}
	}
	tested := []listed{
		{ImportPath: "ex/m", Dir: filepath.Join(dir, "m"), Name: "m", GoFiles: []string{"m.go"}, TestGoFiles: []string{"m_test.go"}},
		{ImportPath: "ex/m [ex/m.test]", Dir: filepath.Join(dir, "m"), Name: "m", GoFiles: []string{"m.go", "m_test.go"}},
		{ImportPath: "ex/m.test", Dir: filepath.Join(dir, "m"), Name: "main", Deps: []string{"ex/m [ex/m.test]"}},
	}
	if r, _ := w.compiled(tested, filepath.Join("m", "m.go")); r != reachTested {
		t.Errorf("a package's own test binary: reach %d, want a test binary's", r)
	}
}

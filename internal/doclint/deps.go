package doclint

import (
	"errors"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// depArrow introduces the import list in a package doc's Dependencies block.
const depArrow = "──imports──▶"

// AssertDependencyLines reports every package doc whose dependency line
// disagrees with the directory's real non-stdlib imports, and returns how many
// lines it checked.
//
// The count is returned rather than asserted internally so the caller keeps its
// own floor visible: a walk that reaches no package doc checks no line and
// would otherwise pass.
func AssertDependencyLines(t TB, root string) (checked int) {
	t.Helper()
	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return 0
	}
	for _, p := range m.Packages {
		dir := filepath.Join(root, filepath.FromSlash(p.Dir))
		// A directory whose only Go file is doc.go documents a family's edges
		// rather than its own, and go/build reports it importing nothing.
		docOnly, err := onlyDocGo(dir)
		if err != nil {
			t.Errorf("%s: %v", p.Dir, err)
			continue
		}
		if docOnly {
			continue
		}
		documented, file, ok := p.dependencyLine()
		if !ok {
			continue
		}
		checked++
		actual, err := nonStdImports(dir, m.Path)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		for _, extra := range notIn(documented, actual) {
			t.Errorf("%s: the dependency line names %s, which the directory does not import", file, extra)
		}
		for _, absent := range notIn(actual, documented) {
			t.Errorf("%s: the directory imports %s, which the dependency line does not name", file, absent)
		}
	}
	return checked
}

// onlyDocGo reports whether dir holds Go source and every file of it is doc.go.
func onlyDocGo(dir string) (bool, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", dir, err)
	}
	var found bool
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if e.Name() != "doc.go" {
			return false, nil
		}
		found = true
	}
	return found, nil
}

// nonStdImports returns dir's non-test imports outside the standard library,
// each in the spelling a dependency line uses: relative to the module for an
// in-module package, and the full path for anything else.
func nonStdImports(dir, modulePath string) ([]string, error) {
	bp, err := build.ImportDir(dir, 0)
	// A directory whose Go files are all excluded by build constraints imports
	// nothing in this configuration, which is not a failure to read it.
	if noGo, ok := errors.AsType[*build.NoGoError](err); ok && noGo != nil {
		return nil, nil //nolint:nilerr // no buildable Go file is an empty import set, not a read failure
	}
	if err != nil {
		return nil, fmt.Errorf("reading imports of %s: %w", dir, err)
	}
	out := make([]string, 0, len(bp.Imports))
	for _, ip := range bp.Imports {
		// The module prefix is tested before the dot rule, so a module path
		// with no dot is not classified as standard library.
		rel, inModule := strings.CutPrefix(ip, modulePath+"/")
		if !inModule {
			// A standard-library path has no dot in its first element.
			if first, _, _ := strings.Cut(ip, "/"); !strings.Contains(first, ".") {
				continue
			}
			rel = ip
		}
		out = append(out, rel)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// dependencyLine returns the import paths this directory's doc.go names for
// itself, and the file that names them. Only doc.go is read: a dependency line
// describes a directory's non-test imports, and a test file's package doc
// would be compared against a list that excludes its own.
func (p *Package) dependencyLine() (paths []string, file string, ok bool) {
	for _, f := range p.files {
		if f.Doc == nil {
			continue
		}
		name := p.fset.Position(f.Package).Filename
		if filepath.Base(name) != "doc.go" {
			continue
		}
		if paths, ok = dependencyPaths(f.Doc.Text(), p.Dir); ok {
			return paths, name, true
		}
	}
	return nil, "", false
}

// dependencyPaths returns the import paths a doc comment's dependency lines
// name for subject. The second result distinguishes a line naming nothing
// outside the standard library from no line at all.
func dependencyPaths(doc, subject string) (paths []string, ok bool) {
	for _, line := range depLogicalLines(doc) {
		before, after, found := strings.Cut(line, depArrow)
		if !found || strings.TrimSpace(before) != subject {
			continue
		}
		ok = true
		paths = append(paths, depFields(after)...)
	}
	slices.Sort(paths)
	return slices.Compact(paths), ok
}

// depLogicalLines joins a dependency list wrapped across several comment lines
// into one line, so each arrow is read with its whole list. A wrapped line ends
// with a comma and its continuation is indented.
func depLogicalLines(doc string) []string {
	var out []string
	for line := range strings.SplitSeq(doc, "\n") {
		if n := len(out); n > 0 && isIndented(line) &&
			strings.Contains(out[n-1], depArrow) &&
			strings.HasSuffix(strings.TrimSpace(out[n-1]), ",") {
			out[n-1] += " " + line
			continue
		}
		out = append(out, line)
	}
	return out
}

func isIndented(line string) bool {
	return line != "" && (line[0] == '\t' || line[0] == ' ')
}

// depFields splits a dependency list into import paths. The separators are the
// comma of a plain list and the plus of the parenthesised "stdlib + x" form;
// "stdlib" and "only" are how those forms say "and the standard library", which
// names no import path.
func depFields(list string) []string {
	list = strings.NewReplacer("(", " ", ")", " ").Replace(list)
	fields := strings.FieldsFunc(list, func(r rune) bool {
		return r == ',' || r == '+' || unicode.IsSpace(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == "stdlib" || f == "only" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// notIn returns the elements of from that in does not hold.
func notIn(from, in []string) []string {
	var out []string
	for _, v := range from {
		if !slices.Contains(in, v) {
			out = append(out, v)
		}
	}
	return out
}

package doclint

import (
	"errors"
	"fmt"
	"go/build"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	// depArrow introduces the import list in a package doc's Dependencies
	// block. It is the ONE spelling the gate reads; an ASCII variant is a row
	// the gate does not see, under a heading that says it does.
	depArrow = "──imports──▶"
	// depHeading is the section a dependency claim lives under. A file carrying
	// it owes at least one depArrow row.
	depHeading = "# Dependencies"
)

// AssertDependencyLines reports every dependency ROW in the module that
// disagrees with the imports of the package its subject names, and returns how
// many rows it read and how many "# Dependencies" headings it found.
//
// A row's subject names a package, which need not be the one whose doc.go
// carries it: adapter/doc.go tabulates its whole family, one row per sibling,
// and each row is read against that sibling's own imports. A directory whose
// only Go file is doc.go imports nothing itself, which says nothing about the
// rows it publishes, so there is no doc-only skip.
//
// A heading with no row this gate can read is an ERROR: it is prose where the
// module states a machine-checked claim everywhere else, and the gate that
// cannot read it reports nothing about it. The one spelling is [depArrow].
//
// Both counts are returned rather than asserted internally so the caller keeps
// its own floor visible, and so it can require that every heading was read.
func AssertDependencyLines(t TB, root string) (checked, headings int) {
	t.Helper()
	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return 0, 0
	}
	for _, p := range m.Packages {
		rows, file, hasHeading := p.dependencyRows()
		if !hasHeading {
			continue
		}
		headings++
		if len(rows) == 0 {
			t.Errorf("%s: a # Dependencies heading with no %s row; the gate reads rows, so this claim is unchecked", file, depArrow)
			continue
		}
		for _, row := range rows {
			checked++
			subject, ok := m.packageForSubject(row.subject, p.Dir)
			if !ok {
				t.Errorf("%s: the dependency row names %q, which is no package in this module", file, row.subject)
				continue
			}
			actual, err := nonStdImports(filepath.Join(root, filepath.FromSlash(subject.Dir)), m.Path)
			if err != nil {
				t.Errorf("%s: %v", file, err)
				continue
			}
			for _, extra := range notIn(row.paths, actual) {
				t.Errorf("%s: the %s row names %s, which that directory does not import", file, row.subject, extra)
			}
			for _, absent := range notIn(actual, row.paths) {
				t.Errorf("%s: %s imports %s, which its dependency row does not name", file, row.subject, absent)
			}
		}
	}
	return checked, headings
}

// packageForSubject resolves a row's subject to a package: the module-relative
// directory it spells, or the doc's own directory for a row that names it.
func (m *Module) packageForSubject(subject, own string) (*Package, bool) {
	if subject == own {
		for _, p := range m.Packages {
			if p.Dir == own {
				return p, true
			}
		}
	}
	for _, p := range m.Packages {
		if p.Dir == subject {
			return p, true
		}
	}
	return nil, false
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

// depRow is one arrow line: the package its subject names, and the import paths
// the line claims for it.
type depRow struct {
	subject string
	paths   []string
}

// dependencyRows returns every arrow row this directory's doc.go publishes, the
// file that publishes them, and whether the file carries a # Dependencies
// heading at all. Only doc.go is read: a row describes a directory's non-test
// imports, and a test file's package doc would be compared against a list that
// excludes its own.
func (p *Package) dependencyRows() (rows []depRow, file string, hasHeading bool) {
	for _, f := range p.files {
		if f.Doc == nil {
			continue
		}
		name := p.fset.Position(f.Package).Filename
		if filepath.Base(name) != "doc.go" {
			continue
		}
		doc := f.Doc.Text()
		rows := dependencyRowsIn(doc)
		// Rows are the claim, and a heading is how a file announces it carries
		// them. Either alone is enough to read the file: adapter/doc.go
		// tabulates its family under "Dependency Direction" and owes its rows
		// just the same, and a heading with no row is the error below.
		if len(rows) == 0 && !strings.Contains(doc, depHeading) {
			continue
		}
		return rows, name, true
	}
	return nil, "", false
}

// dependencyRowsIn returns one row per arrow line in a doc comment, in the
// order written, merging the paths of two rows naming one subject.
func dependencyRowsIn(doc string) []depRow {
	var rows []depRow
	index := make(map[string]int)
	for _, line := range depLogicalLines(doc) {
		before, after, found := strings.Cut(line, depArrow)
		if !found {
			continue
		}
		subject := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(before), "//"))
		i, seen := index[subject]
		if !seen {
			index[subject] = len(rows)
			rows = append(rows, depRow{subject: subject})
			i = len(rows) - 1
		}
		rows[i].paths = append(rows[i].paths, depFields(after)...)
	}
	for i := range rows {
		slices.Sort(rows[i].paths)
		rows[i].paths = slices.Compact(rows[i].paths)
	}
	return rows
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

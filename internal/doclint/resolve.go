package doclint

import (
	"go/ast"
	"go/doc/comment"
	"os"
	"path/filepath"
	"strings"
)

// Link is one doc link, with where it was written and what it names.
type Link struct {
	// Pos is "file:line" of the comment group holding the link.
	Pos string
	// Text is the link as it appears between the brackets.
	Text string
	// ImportPath is the target package, empty for the referencing package.
	ImportPath string
	// Recv is the receiver or owning type, empty for a package-level name.
	Recv string
	// Name is the symbol name.
	Name string
}

// Dangling returns every link in m that names nothing, in walk order.
//
// A link whose target package is outside the module is not resolved and not
// reported: the standard library and this module's dependencies keep their own
// promises. A link under the module's own path that names no loaded package
// dangles. Checked reports how many links were resolved, so a caller can tell
// "nothing dangles" from "nothing was read".
func (m *Module) Dangling() (dangling []Link, checked int) {
	for _, pkg := range m.Packages {
		for _, f := range pkg.files {
			imports := importsOf(f)
			parser := m.parserFor(imports)
			for _, cg := range f.Comments {
				for _, link := range m.links(pkg, parser, cg) {
					target, inModule := m.resolve(pkg, link)
					if !inModule {
						continue
					}
					checked++
					if target == nil || !resolves(target, link) {
						dangling = append(dangling, link)
					}
				}
			}
		}
	}
	return dangling, checked
}

// resolves reports whether a link names something in target.
//
// An empty Name is a whole-package link, written as a bare import path. Finding
// the package is the whole of resolving it; there is no symbol to look up.
func resolves(target *Package, l Link) bool {
	if l.Name == "" {
		return true
	}
	if _, found := target.names[key(l)]; found {
		return true
	}
	_, found := target.otherBuildNames[key(l)]
	return found
}

// key renders a link as the name-set key it must match.
func key(l Link) string {
	if l.Recv == "" {
		return l.Name
	}
	return l.Recv + "." + l.Name
}

// resolve returns the package a link names, and false when its import path is
// outside the module. A path under the module that no loaded package holds
// returns nil and true.
func (m *Module) resolve(from *Package, l Link) (pkg *Package, inModule bool) {
	if l.ImportPath == "" {
		return from, true
	}
	if pkg, ok := m.byImport[l.ImportPath]; ok {
		return pkg, true
	}
	return nil, m.contains(l.ImportPath)
}

// contains reports whether importPath is the module's path or under it, and
// in no other module: not in a directory that carries its own go.mod, and not
// under a major-version element such as v2 that names no directory here.
func (m *Module) contains(importPath string) bool {
	rest, ok := strings.CutPrefix(importPath, m.Path)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return false
	}
	rel := strings.TrimPrefix(rest, "/")
	for _, n := range m.nested {
		if rel == n || strings.HasPrefix(rel, n+"/") {
			return false
		}
	}
	first, _, _ := strings.Cut(rel, "/")
	if isMajorVersion(first) {
		info, err := os.Stat(filepath.Join(m.root, first))
		return err == nil && info.IsDir()
	}
	return true
}

// isMajorVersion reports whether elem is a major-version path element: v and
// a number of 2 or more, without a leading zero.
func isMajorVersion(elem string) bool {
	digits, ok := strings.CutPrefix(elem, "v")
	if !ok || digits == "" || digits[0] == '0' || digits == "1" {
		return false
	}
	return strings.Trim(digits, "0123456789") == ""
}

// parserFor builds a comment parser bound to one file's imports.
//
// LookupSym must accept, because resolution is this package's job, not the
// parser's: with the default LookupSym the parser emits no link nodes at all,
// so a package built to fail returns a clean zero. It rejects the empty name so
// a bare "[]" in prose does not become a link to nothing.
//
// LookupPackage consults the file's own imports before falling back to a unique
// in-module package of that name. Import-first is load-bearing: this module has
// an adapter/json package, so a name-only match would resolve a json-qualified
// link in a file importing encoding/json against the wrong package.
func (m *Module) parserFor(imports map[string]string) *comment.Parser {
	return &comment.Parser{
		LookupSym: func(_, name string) bool { return name != "" },
		LookupPackage: func(name string) (string, bool) {
			if p, ok := imports[name]; ok {
				return p, true
			}
			if pkgs := m.byName[name]; len(pkgs) == 1 {
				return pkgs[0].ImportPath, true
			}
			return "", false
		},
	}
}

// links parses one comment group and returns the doc links it holds.
func (m *Module) links(pkg *Package, p *comment.Parser, cg *ast.CommentGroup) []Link {
	pos := pkg.fset.Position(cg.Pos())
	doc := p.Parse(cg.Text())
	var out []Link
	for _, block := range doc.Content {
		out = appendBlockLinks(out, block, pos.String())
	}
	return out
}

func appendBlockLinks(out []Link, b comment.Block, pos string) []Link {
	switch t := b.(type) {
	case *comment.Paragraph:
		return appendTextLinks(out, t.Text, pos)
	case *comment.Heading:
		return appendTextLinks(out, t.Text, pos)
	case *comment.List:
		for _, item := range t.Items {
			for _, inner := range item.Content {
				out = appendBlockLinks(out, inner, pos)
			}
		}
	}
	return out
}

func appendTextLinks(out []Link, texts []comment.Text, pos string) []Link {
	for _, t := range texts {
		switch v := t.(type) {
		case *comment.DocLink:
			out = append(out, Link{
				Pos:        pos,
				Text:       linkText(v),
				ImportPath: v.ImportPath,
				Recv:       v.Recv,
				Name:       v.Name,
			})
		case *comment.Link:
			out = appendTextLinks(out, v.Text, pos)
		}
	}
	return out
}

// linkText reassembles a link the way it was written, for the failure message.
func linkText(l *comment.DocLink) string {
	var b strings.Builder
	if l.ImportPath != "" {
		b.WriteString(l.ImportPath)
		if l.Name == "" {
			return b.String()
		}
		b.WriteByte('.')
	}
	if l.Recv != "" {
		b.WriteString(l.Recv)
		b.WriteByte('.')
	}
	b.WriteString(l.Name)
	return b.String()
}

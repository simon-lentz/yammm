package doclint

import (
	"go/ast"
	"go/token"
	"strings"
)

// AssertDocCommentsRender reports every doc comment this module publishes that
// go/doc/comment does not render as written, and returns how many it read.
//
// Two shapes, both silent in source: a comment block a blank line detaches from
// the declaration it documents, which go/doc drops entirely, and asterisk-pair
// emphasis, which go/doc/comment has no syntax for and renders literally. Test
// files are not read, because nothing in one reaches go doc.
func AssertDocCommentsRender(t TB, root string) (checked int) {
	t.Helper()
	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return 0
	}
	for _, p := range m.Packages {
		for _, f := range p.files {
			if strings.HasSuffix(p.fset.Position(f.Package).Filename, "_test.go") {
				continue
			}
			for _, g := range docGroups(f) {
				checked++
				if strings.Contains(g.Text(), "**") {
					t.Errorf("%s: doc comment uses ** emphasis, which go/doc/comment renders literally", p.fset.Position(g.Pos()))
				}
			}
			for _, d := range detachedDocs(p.fset, f) {
				t.Errorf("%s: a blank line separates this comment block from %s, so go/doc shows no documentation for it",
					p.fset.Position(d.comment.Pos()), d.name)
			}
		}
	}
	return checked
}

// docGroups returns every comment group the file attaches to a declaration it
// documents, the package doc included.
func docGroups(f *ast.File) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	add := func(g *ast.CommentGroup) {
		if g != nil {
			out = append(out, g)
		}
	}
	add(f.Doc)
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GenDecl:
			add(n.Doc)
		case *ast.FuncDecl:
			add(n.Doc)
		case *ast.TypeSpec:
			add(n.Doc)
		case *ast.ValueSpec:
			add(n.Doc)
		case *ast.Field:
			add(n.Doc)
		}
		return true
	})
	return out
}

// detached is a comment block that reads as documentation for a declaration the
// parser did not attach it to.
type detached struct {
	comment *ast.CommentGroup
	name    string
}

// detachedDocs returns every exported declaration in f whose preceding comment
// block a blank line separates from it. The parser attaches a doc comment only
// when it abuts the declaration, so such a block documents nothing.
func detachedDocs(fset *token.FileSet, f *ast.File) []detached {
	var out []detached
	prevEnd := f.Name.End()
	for _, d := range f.Decls {
		name, exported := exportedDeclName(d)
		if exported && declDoc(d) == nil {
			if g := detachedGroupBefore(fset, f, prevEnd, d.Pos()); g != nil {
				out = append(out, detached{comment: g, name: name})
			}
		}
		prevEnd = d.End()
	}
	return out
}

// detachedGroupBefore returns the last comment block in the gap between two
// declarations, when a blank line separates it from the second.
func detachedGroupBefore(fset *token.FileSet, f *ast.File, after, before token.Pos) *ast.CommentGroup {
	var found *ast.CommentGroup
	for _, g := range f.Comments {
		if g.Pos() > after && g.End() < before {
			found = g
		}
	}
	if found == nil {
		return nil
	}
	if fset.Position(found.End()).Line+1 >= fset.Position(before).Line {
		return nil
	}
	return found
}

func declDoc(d ast.Decl) *ast.CommentGroup {
	switch d := d.(type) {
	case *ast.GenDecl:
		return d.Doc
	case *ast.FuncDecl:
		return d.Doc
	}
	return nil
}

// exportedDeclName returns the name a declaration publishes, and whether it
// publishes one at all.
func exportedDeclName(d ast.Decl) (string, bool) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Name.IsExported() {
			return d.Name.Name, true
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				if s.Name.IsExported() {
					return s.Name.Name, true
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if n.IsExported() {
						return n.Name, true
					}
				}
			}
		}
	}
	return "", false
}

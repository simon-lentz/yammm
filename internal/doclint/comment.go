package doclint

import (
	"go/ast"
	"go/doc/comment"
	"go/token"
	"regexp"
	"slices"
	"strings"
)

// AssertDocCommentsRender reports every doc comment this module publishes that
// go/doc/comment does not render as written, and returns how many it read.
//
// Two shapes, both silent in source. A comment block a blank line detaches from
// the declaration it documents, which go/doc drops entirely. And markup go/doc
// has no syntax for, which it renders literally, character for character:
//
//	*emphasis*   **emphasis**   _emphasis_   ```fence```
//
// The markup check reads the PARSED comment rather than its text, so a spelling
// inside a code block is left alone: an indented block is code by go/doc's own
// rule, and code is exactly where these characters are meant literally. Test
// files are not read, because nothing in one reaches go doc.
func AssertDocCommentsRender(t TB, root string) (checked int) {
	t.Helper()
	m, err := Load(root)
	if err != nil {
		t.Errorf("loading %s: %v", root, err)
		return 0
	}
	parser := &comment.Parser{}
	for _, p := range m.Packages {
		for _, f := range p.files {
			if isTestFile(p.fset.Position(f.Package).Filename) {
				continue
			}
			for _, g := range docGroups(f) {
				checked++
				for _, what := range literalMarkup(parser, g.Text()) {
					t.Errorf("%s: doc comment uses %s, which go/doc/comment renders literally",
						p.fset.Position(g.Pos()), what)
				}
			}
			for _, d := range detachedDocs(p.fset, f) {
				t.Errorf("%s: a blank line separates this comment block from %s, so go/doc shows no documentation for it",
					p.fset.Position(d.comment.Pos()), d.name)
			}
			for _, sd := range stackedDocs(f) {
				t.Errorf("%s: this doc comment opens by naming %s, not %s, so it documents the wrong declaration",
					p.fset.Position(sd.doc.Pos()), sd.names, sd.attachedTo)
			}
		}
	}
	return checked
}

// emphasisPattern matches the word-bounded asterisk and underscore spellings a
// reader writes expecting emphasis, which go/doc prints as typed. A name that
// merely carries underscores is quoted in this module's prose, which the word
// boundary then excludes.
var emphasisPattern = regexp.MustCompile(`(^|[\s(\[])(\*\*?[^\s*][^*]*\*\*?|_[^\s_][^_]*_)($|[\s.,;:!?)\]])`)

// literalMarkup returns what a doc comment spells as markup that go/doc renders
// literally. Only Plain text is read: the parser has already separated code
// blocks, where these characters are meant as written.
func literalMarkup(parser *comment.Parser, doc string) []string {
	var out []string
	seen := make(map[string]bool)
	report := func(what string) {
		if !seen[what] {
			seen[what] = true
			out = append(out, what)
		}
	}
	for _, block := range parser.Parse(doc).Content {
		for _, line := range plainLines(block) {
			if emphasisPattern.MatchString(line) {
				report("* or _ emphasis")
			}
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				report("a ``` fence")
			}
		}
	}
	return out
}

// plainLines returns a block's plain text, one line per source line. A Code
// block contributes nothing: its content is meant literally.
func plainLines(block comment.Block) []string {
	var texts []comment.Text
	switch b := block.(type) {
	case *comment.Paragraph:
		texts = b.Text
	case *comment.Heading:
		texts = b.Text
	case *comment.List:
		for _, item := range b.Items {
			for _, inner := range item.Content {
				texts = append(texts, plainTextOf(inner)...)
			}
		}
	case *comment.Code:
		return nil
	}
	var sb strings.Builder
	for _, t := range texts {
		if plain, ok := t.(comment.Plain); ok {
			sb.WriteString(string(plain))
		}
	}
	return strings.Split(sb.String(), "\n")
}

// plainTextOf unwraps a list item's inner block to its text spans.
func plainTextOf(block comment.Block) []comment.Text {
	if p, ok := block.(*comment.Paragraph); ok {
		return p.Text
	}
	return nil
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

// stacked is a doc comment attached to one declaration and opening with the
// name of another in the same file.
type stacked struct {
	doc        *ast.CommentGroup
	names      string // the declaration the text names
	attachedTo string // the declaration the parser attached it to
}

// stackedDocs returns every doc comment whose leading identifier names another
// declaration of the same file. Nothing is detached in this shape — the block
// abuts a declaration — so the detached rule cannot see it, and go doc shows
// the paragraph under a name its author did not mean.
//
// Every declaration is read, exported or not: go doc -u renders both, and an
// unexported doc comment anchors a regression test by name in this module.
// A doc naming a symbol the file does not declare is a reference, not a stack.
func stackedDocs(f *ast.File) []stacked {
	declared := make(map[string]bool)
	for _, d := range f.Decls {
		for _, n := range declaredNames(d) {
			declared[n] = true
		}
	}
	var out []stacked
	for _, d := range f.Decls {
		doc := declDoc(d)
		text := docText(doc)
		if text == "" {
			continue
		}
		names := declaredNames(d)
		first, _, _ := strings.Cut(text, " ")
		if first == "" || slices.Contains(names, first) || !declared[first] {
			continue
		}
		out = append(out, stacked{doc: doc, names: first, attachedTo: strings.Join(names, ", ")})
	}
	return out
}

// declaredNames returns every name a declaration publishes, exported or not,
// a method's own name included: a paragraph stacked onto its neighbour names a
// method as readily as a function, and this module's sharpest instance did.
func declaredNames(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return []string{d.Name.Name}
	case *ast.GenDecl:
		var out []string
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				out = append(out, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					out = append(out, n.Name)
				}
			}
		}
		return out
	}
	return nil
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
//
// Every position a doc can occupy is scanned: the package clause, each
// top-level declaration, each spec of a parenthesised const, var or type block
// and each exported struct field. A Doc holding only directives (//go:build and
// its kin) has no text and counts as absent, because go/doc shows nothing for
// it either.
//
// A gap comment is reported only when its text BEGINS WITH the declaration's
// name. That is the shape of a doc comment by Go's own convention, and it is
// what separates one from the trailing note, banner or commented-out code that
// also live in a gap — a distinction no heuristic about blank lines can make.
func detachedDocs(fset *token.FileSet, f *ast.File) []detached {
	var out []detached
	check := func(name string, doc *ast.CommentGroup, after, before token.Pos) {
		if name == "" || !ast.IsExported(name) || docText(doc) != "" {
			return
		}
		g := detachedGroupBefore(fset, f, after, before)
		if g == nil || !beginsWithName(g.Text(), name) {
			return
		}
		out = append(out, detached{comment: g, name: name})
	}

	check(packageDocName, f.Doc, f.FileStart, f.Package)

	prevEnd := f.Name.End()
	for _, d := range f.Decls {
		name, _ := exportedDeclName(d)
		check(name, declDoc(d), prevEnd, d.Pos())
		if gd, ok := d.(*ast.GenDecl); ok {
			// A spec's own gap exists only inside a parenthesised block; a bare
			// declaration's spec shares the declaration's. Struct fields are
			// scanned either way, since a struct is usually declared bare.
			specEnd := gd.Lparen
			for _, spec := range gd.Specs {
				if gd.Lparen.IsValid() {
					check(specName(spec), specDoc(spec), specEnd, spec.Pos())
					specEnd = spec.End()
				}
				checkStructFields(fset, f, spec, &out)
			}
		}
		prevEnd = d.End()
	}
	return out
}

// packageDocName is the subject a detached package doc documents. It is not an
// identifier, so it is reported by this name rather than by a declaration's.
const packageDocName = "the package"

// checkStructFields scans a type spec's exported struct fields, where a doc can also be
// detached and where go/doc also shows nothing for it.
func checkStructFields(fset *token.FileSet, f *ast.File, spec ast.Spec, out *[]detached) {
	ts, ok := spec.(*ast.TypeSpec)
	if !ok {
		return
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	prev := st.Fields.Opening
	for _, field := range st.Fields.List {
		for _, id := range field.Names {
			if !id.IsExported() || docText(field.Doc) != "" {
				continue
			}
			if g := detachedGroupBefore(fset, f, prev, field.Pos()); g != nil && beginsWithName(g.Text(), id.Name) {
				*out = append(*out, detached{comment: g, name: ts.Name.Name + "." + id.Name})
			}
		}
		prev = field.End()
	}
}

// docText is a comment group's rendered text. A group holding only directives
// renders empty, which is the same as carrying no doc at all.
func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}

// beginsWithName reports whether a comment reads as documentation for name:
// its first word is that name, which is how Go doc comments open.
func beginsWithName(text, name string) bool {
	if name == packageDocName {
		return strings.HasPrefix(strings.TrimSpace(text), "Package ")
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), " ")
	return first == name
}

// specName returns the name a spec inside a parenthesised block publishes.
func specName(spec ast.Spec) string {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Name.Name
	case *ast.ValueSpec:
		for _, n := range s.Names {
			if n.IsExported() {
				return n.Name
			}
		}
	}
	return ""
}

// specDoc returns a spec's own doc comment.
func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// detachedGroupBefore returns the last comment block in the gap between two
// positions, when a blank line separates it from the second.
func detachedGroupBefore(fset *token.FileSet, f *ast.File, after, before token.Pos) *ast.CommentGroup {
	var found *ast.CommentGroup
	for _, g := range f.Comments {
		if g.Pos() >= after && g.End() < before {
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

package markdown

import (
	"bytes"
	"cmp"
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// newParser returns the reader every structural question about the document
// is put to: CommonMark with GitHub's extensions, which is how GitHub reads a
// committed document's blocks. Marshal builds one per call.
func newParser() goldmark.Markdown {
	return goldmark.New(goldmark.WithExtensions(extension.GFM))
}

// sentinel is a paragraph appended after a blank line to ask whether text
// leaves a block open: a block still open at the end of the text takes the
// sentinel as content, and a closed text leaves it a top-level paragraph.
const sentinel = "yammm-end-of-text"

// parse reads src as a whole document.
func parse(md goldmark.Markdown, src []byte) ast.Node {
	return md.Parser().Parse(text.NewReader(src))
}

// probe is a text parsed with the sentinel appended after a blank line.
type probe struct {
	root ast.Node
	src  []byte
}

// probeText parses src with the sentinel appended. The sentinel moves no
// offset inside src and holds no heading, link or table.
func probeText(md goldmark.Markdown, src []byte) probe {
	p := make([]byte, 0, len(src)+2+len(sentinel))
	p = append(append(append(p, src...), "\n\n"...), sentinel...)
	return probe{root: parse(md, p), src: p}
}

// open reports whether the text leaves a block open at its end, and returns
// the top-level block that took the sentinel.
func (p probe) open() (ast.Node, bool) {
	last := p.root.LastChild()
	if para, ok := last.(*ast.Paragraph); ok && para.Lines().Len() == 1 {
		if line := para.Lines().At(0); string(line.Value(p.src)) == sentinel {
			return nil, false
		}
	}
	return last, true
}

// leavesBlockOpen reports whether src leaves a block open at its end, and
// returns the top-level block that takes the text after it.
func leavesBlockOpen(md goldmark.Markdown, src string) (ast.Node, bool) {
	return probeText(md, []byte(src)).open()
}

// closeAuthorText returns doc with the block it leaves open closed, so a doc
// comment cannot swallow the text the generator writes after it. Only a fenced
// code block and an HTML block of CommonMark's types 1 to 5 stay open across a
// blank line, so those are the blocks it closes; each candidate closer is kept
// only when the parser reads the result as closed.
func closeAuthorText(md goldmark.Markdown, doc string) string {
	open, ok := leavesBlockOpen(md, doc)
	if !ok {
		return doc
	}
	for _, closer := range closers(open, []byte(doc)) {
		if _, stillOpen := leavesBlockOpen(md, doc+"\n"+closer); !stillOpen {
			return doc + "\n" + closer
		}
	}
	return doc
}

// closers lists the lines that can close block, in the order to try them. A
// fenced code block closes on a run of its own character at least as long as
// its opener; the run is taken as the longest in the text, which is never
// shorter. An HTML block closes on its type's end condition.
func closers(block ast.Node, src []byte) []string {
	switch b := block.(type) {
	case *ast.FencedCodeBlock:
		return []string{
			strings.Repeat("`", max(3, longestRun(src, '`'))),
			strings.Repeat("~", max(3, longestRun(src, '~'))),
		}
	case *ast.HTMLBlock:
		switch b.HTMLBlockType {
		case ast.HTMLBlockType1:
			// Any of the four end tags closes the block; the one that opened it
			// goes first, so the closer matches the element.
			tags := []string{"pre", "script", "style", "textarea"}
			first := ""
			if b.Lines().Len() > 0 {
				line := b.Lines().At(0)
				first = strings.ToLower(string(line.Value(src)))
			}
			slices.SortStableFunc(tags, func(a, c string) int {
				return cmp.Compare(openingIndex(first, a), openingIndex(first, c))
			})
			out := make([]string, len(tags))
			for i, tag := range tags {
				out[i] = "</" + tag + ">"
			}
			return out
		case ast.HTMLBlockType2:
			return []string{"-->"}
		case ast.HTMLBlockType3:
			return []string{"?>"}
		case ast.HTMLBlockType4:
			return []string{">"}
		case ast.HTMLBlockType5:
			return []string{"]]>"}
		}
	}
	return nil
}

// openingIndex returns where "<tag" first appears in line, or the line's
// length when it does not.
func openingIndex(line, tag string) int {
	if i := strings.Index(line, "<"+tag); i >= 0 {
		return i
	}
	return len(line)
}

// longestRun returns the length of the longest run of c in src.
func longestRun(src []byte, c byte) int {
	longest, run := 0, 0
	for _, b := range src {
		if b == c {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// parsedHeading is one heading as the parser reads the document: where its
// line starts (-1 for a heading with no text, which the generator never
// writes), its level, whether it sits at the top level, and its text as a
// browser shows it, which is what GitHub derives the anchor from.
type parsedHeading struct {
	offset   int
	level    int
	topLevel bool
	text     string
}

// headings returns every heading of the document in document order, those
// inside doc comments and list items included.
func headings(md goldmark.Markdown, root ast.Node, src []byte) ([]parsedHeading, error) {
	var out []parsedHeading
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		offset := -1
		if h.Lines().Len() > 0 {
			offset = lineStart(src, h.Lines().At(0).Start)
		}
		text, err := textContent(md, h, src)
		if err != nil {
			return ast.WalkStop, err
		}
		out = append(out, parsedHeading{
			offset:   offset,
			level:    h.Level,
			topLevel: h.Parent() == root,
			text:     text,
		})
		return ast.WalkSkipChildren, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading headings: %w", err)
	}
	return out, nil
}

// lineStart returns the offset of the line that holds pos.
func lineStart(src []byte, pos int) int {
	pos = min(max(pos, 0), len(src))
	return bytes.LastIndexByte(src[:pos], '\n') + 1
}

// textContent renders n's inline content to HTML and returns what a browser
// shows of it: every tag dropped and every character reference decoded.
func textContent(md goldmark.Markdown, n ast.Node, src []byte) (string, error) {
	var b bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if err := md.Renderer().Render(&b, src, c); err != nil {
			return "", fmt.Errorf("rendering a heading's text: %w", err)
		}
	}
	return html.UnescapeString(stripTags(b.String())), nil
}

// stripTags drops every "<…>" run. The renderer writes a literal "<" as
// "&lt;", so every "<" in its output opens a tag or a comment.
func stripTags(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			return b.String()
		}
		s = s[i+j+1:]
	}
}

// linkTargets counts each internal link destination "#anchor" the parser reads
// in the document, leaving out a link whose text starts where skip says. A
// link with no text has no position and is left out too; the generator never
// writes one.
func linkTargets(root ast.Node, skip func(pos int) bool) (map[string]int, error) {
	out := map[string]int{}
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		l, ok := n.(*ast.Link)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		dest := string(l.Destination)
		if pos := firstTextStart(l); strings.HasPrefix(dest, "#") && pos >= 0 && !skip(pos) {
			out[dest[1:]]++
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading links: %w", err)
	}
	return out, nil
}

// firstTextStart returns the offset of the first text segment under n, or -1.
func firstTextStart(n ast.Node) int {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return t.Segment.Start
		}
		if pos := firstTextStart(c); pos >= 0 {
			return pos
		}
	}
	return -1
}

// parsedTable is one table as the parser reads it: the line its header starts
// on, its header's cell count and its body's row count.
type parsedTable struct {
	cols, rows int
}

// tables maps each table's header line offset to its shape.
func tables(root ast.Node, src []byte) (map[int]parsedTable, error) {
	out := map[int]parsedTable{}
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		t, ok := n.(*extast.Table)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		header, ok := t.FirstChild().(*extast.TableHeader)
		if !ok {
			return ast.WalkSkipChildren, nil
		}
		pos := -1
		if err := ast.Walk(header, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
			if tx, ok := c.(*ast.Text); ok && entering && pos < 0 {
				pos = tx.Segment.Start
			}
			return ast.WalkContinue, nil
		}); err != nil {
			return ast.WalkStop, fmt.Errorf("reading a table header: %w", err)
		}
		if pos >= 0 {
			out[lineStart(src, pos)] = parsedTable{cols: header.ChildCount(), rows: t.ChildCount() - 1}
		}
		return ast.WalkSkipChildren, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading tables: %w", err)
	}
	return out, nil
}

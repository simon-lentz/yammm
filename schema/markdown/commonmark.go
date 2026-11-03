package markdown

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
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
			// Any of the four end tags ends the block; GitHub's tag filter
			// shows the other three as text, and </pre> as nothing.
			return []string{"</pre>"}
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

// parsedHeading is one heading element of the document: its Markdown line's
// offset (-1 for raw HTML or no text), level and nesting, and its text
// content, which GitHub derives the anchor from.
type parsedHeading struct {
	offset   int
	level    int
	topLevel bool
	text     string
}

// headingReader reads a document's heading elements, raw HTML ones included,
// since GitHub anchors every h1 to h6 element it renders. It renders the parsed
// document to HTML and builds the tree an HTML5 parser builds from it. Each
// Markdown heading carries a mark holding a per-reader nonce no text can forge;
// rand.Text writes it in base32, which an unquoted attribute value can hold.
type headingReader struct {
	marker   *headingMarker
	renderer renderer.Renderer
}

func newHeadingReader() headingReader {
	m := &headingMarker{nonce: rand.Text()}
	return headingReader{
		marker: m,
		renderer: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(
				gmhtml.WithUnsafe(),
				renderer.WithNodeRenderers(util.Prioritized(m, 100)),
			),
		).Renderer(),
	}
}

// markAttr names the attribute that marks a Markdown heading element. An
// attribute stays on its element wherever the tree builder moves it, and its
// value is written unquoted, so the mark adds no quote to what an author's
// open attribute reads, as GitHub's own heading tag adds none.
const markAttr = "data-yammm-heading"

// headingMarker renders a Markdown heading as goldmark does, its start tag
// carrying markAttr=nonce-n, where n counts the render's Markdown headings
// from 0.
type headingMarker struct {
	nonce string
	count int
}

func (m *headingMarker) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkStop, fmt.Errorf("rendering a heading: a %s node", n.Kind())
		}
		level := strconv.Itoa(h.Level)
		tag := "</h" + level + ">\n"
		if entering {
			tag = "<h" + level + " " + markAttr + "=" + m.nonce + "-" + strconv.Itoa(m.count) + ">"
			m.count++
		}
		if _, err := w.WriteString(tag); err != nil {
			return ast.WalkStop, fmt.Errorf("rendering a heading: %w", err)
		}
		return ast.WalkContinue, nil
	})
}

// htmlClosers are the lines tried, in order, to close an HTML construct a doc
// comment leaves open: a comment or a tag, a CDATA section, a quoted attribute
// value of either quote, and a noscript element's text. Each is raw HTML to
// Markdown and reads as nothing from a closed state: a comment, a bogus
// comment, a void element, and an end tag no element matches.
var htmlClosers = []string{"<!-- -->", "<![CDATA[ ]]>", `<wbr x='"'>`, "</noscript>"}

// closeAuthorHTML returns doc with the HTML construct it leaves open closed, as
// closeAuthorText closes a Markdown block: a comment, a CDATA section, a tag,
// a quoted attribute value, or a noscript element's text. Left open, it would
// take the tags the generator writes after it as its own content. The closer
// follows doc after sep, a line break for a block and a space for a table
// cell, and is kept only when an HTML5 parser then reads an element written
// after doc.
func (r headingReader) closeAuthorHTML(md goldmark.Markdown, doc, sep string) string {
	if r.readsPast(md, doc) {
		return doc
	}
	for _, closer := range htmlClosers {
		if r.readsPast(md, doc+sep+closer) {
			return doc + sep + closer
		}
	}
	return doc
}

// readsPast reports whether an HTML5 parser reads the p element that follows
// doc, after a blank line, in the HTML doc renders to. Its attribute alone is
// not enough: an open tag takes a following tag's attributes as its own.
func (r headingReader) readsPast(md goldmark.Markdown, doc string) bool {
	const endAttr = "data-yammm-end"
	src := []byte(doc + "\n\n<p " + endAttr + "=\"" + r.marker.nonce + "\"></p>\n")
	var out bytes.Buffer
	r.marker.count = 0
	if err := r.renderer.Render(&out, src, parse(md, src)); err != nil {
		return false
	}
	tree, err := html.Parse(bytes.NewReader(tagFilter(out.Bytes())))
	if err != nil {
		return false
	}
	for n := range tree.Descendants() {
		if n.Type == html.ElementNode && n.Data == "p" && slices.Contains(n.Attr, html.Attribute{Key: endAttr, Val: r.marker.nonce}) {
			return true
		}
	}
	return false
}

// headings returns every heading element of the document in the tree's order:
// the Markdown headings, those inside doc comments and list items included,
// and the heading elements an author wrote as raw HTML. A Markdown heading
// inside author text, where authored says, may be taken into an HTML
// construct the author left open, and is then no heading, as on GitHub.
func (r headingReader) headings(root ast.Node, src []byte, authored func(pos int) bool) ([]parsedHeading, error) {
	var marked []parsedHeading
	if err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			offset := -1
			if h.Lines().Len() > 0 {
				offset = lineStart(src, h.Lines().At(0).Start)
			}
			marked = append(marked, parsedHeading{offset: offset, level: h.Level, topLevel: h.Parent() == root})
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, fmt.Errorf("reading headings: %w", err)
	}
	var out bytes.Buffer
	r.marker.count = 0
	if err := r.renderer.Render(&out, src, root); err != nil {
		return nil, fmt.Errorf("reading headings: rendering the document: %w", err)
	}
	tree, err := html.Parse(bytes.NewReader(tagFilter(out.Bytes())))
	if err != nil {
		return nil, fmt.Errorf("reading headings: parsing the rendered document: %w", err)
	}
	var headings []parsedHeading
	seen := make([]bool, len(marked))
	for n := range tree.Descendants() {
		if n.Type != html.ElementNode || !isHeadingName(n.Data) {
			continue
		}
		k, ok := r.mark(n)
		if !ok {
			headings = append(headings, parsedHeading{offset: -1, text: textOf(n)})
			continue
		}
		if k >= len(marked) || seen[k] {
			return nil, fmt.Errorf("reading headings: Markdown heading %d is read twice or was never written", k)
		}
		seen[k] = true
		h := marked[k]
		h.text = textOf(n)
		headings = append(headings, h)
	}
	for k, ok := range seen {
		if !ok && (marked[k].offset < 0 || !authored(marked[k].offset)) {
			return nil, fmt.Errorf("reading headings: Markdown heading %d is not read as a heading element", k)
		}
	}
	return headings, nil
}

// mark returns the count a Markdown heading element's mark holds.
func (r headingReader) mark(n *html.Node) (int, bool) {
	for _, a := range n.Attr {
		if a.Namespace != "" || a.Key != markAttr {
			continue
		}
		count, ok := strings.CutPrefix(a.Val, r.marker.nonce+"-")
		if !ok {
			return 0, false
		}
		k, err := strconv.Atoi(count)
		return k, err == nil && k >= 0
	}
	return 0, false
}

// textOf returns n's text content: the text of every text node under it.
func textOf(n *html.Node) string {
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}

func isHeadingName(name string) bool {
	return len(name) == 2 && name[0] == 'h' && '1' <= name[1] && name[1] <= '6'
}

// filteredTag matches a tag GitHub's tag filter escapes: GitHub writes its "<"
// as "&lt;", so a browser builds no element from it and reads it as text.
var filteredTag = regexp.MustCompile(`(?i)<(/?(?:title|textarea|style|xmp|iframe|noembed|noframes|script|plaintext))([\t\n\f\r />]|$)`)

// tagFilter applies GitHub's tag filter to rendered HTML.
func tagFilter(out []byte) []byte {
	return filteredTag.ReplaceAll(out, []byte("&lt;$1$2"))
}

// lineStart returns the offset of the line that holds pos.
func lineStart(src []byte, pos int) int {
	pos = min(max(pos, 0), len(src))
	return bytes.LastIndexByte(src[:pos], '\n') + 1
}

// linkTargets counts each internal link "#anchor" that starts where skip says
// no. There the generator writes internal links alone, so any other link,
// image or autolink, with text or without, is schema text read as syntax, and
// is an error.
func linkTargets(root ast.Node, src []byte, skip func(pos int) bool) (map[string]int, error) {
	out := map[string]int{}
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch l := n.(type) {
		case *ast.Link:
			pos := l.Pos()
			if pos < 0 {
				return ast.WalkStop, fmt.Errorf("a link to %q has no place in the document", l.Destination)
			}
			if skip(pos) {
				return ast.WalkContinue, nil
			}
			dest := string(l.Destination)
			if anchor, ok := strings.CutPrefix(dest, "#"); ok {
				out[anchor]++
				return ast.WalkContinue, nil
			}
			return ast.WalkStop, fmt.Errorf("text outside doc comments is read as a link to %q", dest)
		case *ast.Image:
			pos := l.Pos()
			if pos < 0 {
				return ast.WalkStop, fmt.Errorf("an image of %q has no place in the document", l.Destination)
			}
			if !skip(pos) {
				return ast.WalkStop, fmt.Errorf("text outside doc comments is read as an image of %q", l.Destination)
			}
		case *ast.AutoLink:
			label := l.Label(src)
			pos, ok := offsetIn(src, label)
			if !ok {
				return ast.WalkStop, fmt.Errorf("an autolink %q has no place in the document", label)
			}
			if !skip(pos) {
				return ast.WalkStop, fmt.Errorf("text outside doc comments is read as the autolink %q", l.URL(src))
			}
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading links: %w", err)
	}
	return out, nil
}

// offsetIn returns where sub starts in src when sub is a subslice of src
// holding the same bytes there. goldmark exposes an autolink's text only as
// such a subslice.
func offsetIn(src, sub []byte) (int, bool) {
	off := cap(src) - cap(sub)
	if off < 0 || off+len(sub) > len(src) || !bytes.Equal(src[off:off+len(sub)], sub) {
		return 0, false
	}
	return off, true
}

// fencedCodeAt returns the body of the top-level fenced code block whose
// opening line starts at offset.
func fencedCodeAt(root ast.Node, src []byte, offset int) (string, bool) {
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		f, ok := n.(*ast.FencedCodeBlock)
		if !ok || f.Info == nil || lineStart(src, f.Info.Segment.Start) != offset {
			continue
		}
		var b strings.Builder
		for i := range f.Lines().Len() {
			line := f.Lines().At(i)
			b.Write(line.Value(src))
		}
		return b.String(), true
	}
	return "", false
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

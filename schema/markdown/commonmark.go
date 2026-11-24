package markdown

import (
	"bytes"
	"crypto/rand"
	"fmt"
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

// newParser returns the reader of Markdown blocks: CommonMark with GitHub's
// extensions, which is how GitHub reads a committed document's blocks. Marshal
// builds one per call.
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

// parsedHeading is one heading element of a document: its Markdown line's
// offset (-1 for raw HTML or no text) and its text content, which GitHub
// derives the anchor from.
type parsedHeading struct {
	offset int
	text   string
}

// headingReader reads the heading elements of a document that GitHub anchors,
// raw HTML ones included: each h1 to h6 element it renders that is not inside
// another heading and not lost under a select element. It renders the parsed
// document to HTML and builds the tree an HTML5 parser builds from it. Each
// Markdown heading carries a mark holding a per-reader nonce no text can forge;
// rand.Text writes it in base32, which an unquoted attribute value can hold.
type headingReader struct {
	marker   *headingMarker
	renderer renderer.Renderer
	// endElement names the element readsPast writes after a doc comment. It
	// holds the nonce, so no doc comment can write an element of that name
	// before the construct it leaves open.
	endElement string
}

func newHeadingReader() headingReader {
	m := &headingMarker{nonce: rand.Text()}
	return headingReader{
		marker:     m,
		endElement: "yammm-end-" + strings.ToLower(m.nonce),
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
// value of either quote, and a select element. Each is raw HTML to Markdown
// and reads as nothing from a closed state: a comment, a bogus comment, a void
// element, and an end tag no element matches.
var htmlClosers = []string{"<!-- -->", "<![CDATA[ ]]>", `<wbr x='"'>`, "</select>"}

// closeAuthorHTML returns doc with the HTML construct it leaves open closed, as
// closeAuthorText closes a Markdown block: a comment, a CDATA section, a tag,
// a quoted attribute value, or a select element. Left open, it would take the
// tags the generator writes after it as its own content. The closer follows
// doc on the next line and is kept only when an HTML5 parser then reads an
// element written after doc. A doc no closer closes is returned as it is, and
// readsPast is false: Marshal refuses nothing for it.
func (r headingReader) closeAuthorHTML(md goldmark.Markdown, doc string) (text string, readsPast bool) {
	if r.readsPast(md, doc) {
		return doc, true
	}
	for _, closer := range htmlClosers {
		if r.readsPast(md, doc+"\n"+closer) {
			return doc + "\n" + closer, true
		}
	}
	return doc, false
}

// readsPast reports whether an HTML5 parser reads the element that follows
// doc, after a blank line, in the HTML doc renders to. Its start tag alone on
// a line is an HTML block, so Markdown writes no tag of its own before it that
// would end a tag doc leaves open; an open tag takes it as its own attributes,
// and the element is not read. A paragraph goes before doc, as text goes
// before every doc comment in the document: a parser reads a frameset tag one
// way before any text and another way after it.
func (r headingReader) readsPast(md goldmark.Markdown, doc string) bool {
	src := []byte("text\n\n" + doc + "\n\n<" + r.endElement + ">\n")
	tree, err := r.tree(src, parse(md, src))
	if err != nil {
		return false
	}
	var sel selectScope
	for n := range tree.Descendants() {
		sel.visit(n)
		if n.Type == html.ElementNode && n.Data == r.endElement {
			return !sel.lost(n)
		}
	}
	return false
}

// selectScope follows GitHub's parser through the HTML select elements of a
// tree read in document order. GitHub's parser reads a select's content in
// its own insertion mode, which drops every start tag but an option's and
// closes the select at a keygen start tag, inside SVG or MathML too, since it
// drops their start tags; golang.org/x/net/html keeps every element under a
// select and closes none at keygen. Both read a template's content apart: a
// keygen in it closes no select outside it, and nothing in it is lost under
// one. An input or select start tag closes a select in both parsers, and
// GitHub's tag filter escapes a textarea's.
type selectScope struct {
	// closed holds each select a keygen element visited so far is under.
	closed map[*html.Node]bool
}

// visit records n, the next node in document order.
func (s *selectScope) visit(n *html.Node) {
	if n.Type != html.ElementNode || n.Data != "keygen" {
		return
	}
	for p := n.Parent; p != nil && !isTemplate(p); p = p.Parent {
		if isHTMLSelect(p) {
			if s.closed == nil {
				s.closed = make(map[*html.Node]bool)
			}
			s.closed[p] = true
		}
	}
}

// lost reports whether GitHub's parser loses n, the node visit recorded
// last: n is under a select no keygen element closed before it, with no
// template between them.
func (s *selectScope) lost(n *html.Node) bool {
	for p := n.Parent; p != nil && !isTemplate(p); p = p.Parent {
		if isHTMLSelect(p) && !s.closed[p] {
			return true
		}
	}
	return false
}

// isHTMLSelect reports whether n is a select element; one in SVG or MathML
// content is a foreign element of that name, which no parser reads as one.
func isHTMLSelect(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Namespace == "" && n.Data == "select"
}

// isTemplate reports whether n is a template element. One x/net/html puts in
// SVG or MathML under a select counts too: GitHub's parser drops their start
// tags there, so it reads the template as an HTML one.
func isTemplate(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Data == "template"
}

// tree returns the tree an HTML5 parser builds from the HTML root renders to,
// with GitHub's tag filter applied.
func (r headingReader) tree(src []byte, root ast.Node) (*html.Node, error) {
	var out bytes.Buffer
	r.marker.count = 0
	if err := r.renderer.Render(&out, src, root); err != nil {
		return nil, fmt.Errorf("rendering the document: %w", err)
	}
	// GitHub parses with scripting off: a noscript element's content is markup,
	// so a comment opened in it takes the rest of the document.
	tree, err := html.ParseWithOptions(bytes.NewReader(tagFilter(out.Bytes())), html.ParseOptionEnableScripting(false))
	if err != nil {
		return nil, fmt.Errorf("parsing the rendered document: %w", err)
	}
	return tree, nil
}

// headings returns every heading element of the document that GitHub anchors,
// in the tree's order: the Markdown headings, those inside doc comments and
// list items included, and the heading elements written as raw HTML. A
// heading element inside another takes no anchor on GitHub, one below a
// select element is lost to GitHub's parser (see [below]), and a Markdown
// heading that open HTML takes is no heading element, as on GitHub; none is
// returned.
func (r headingReader) headings(root ast.Node, src []byte) ([]parsedHeading, error) {
	// The marker counts Markdown headings in the walk's order.
	var marked []int
	if err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			marked = append(marked, headingOffset(h, src))
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, fmt.Errorf("reading headings: %w", err)
	}
	tree, err := r.tree(src, root)
	if err != nil {
		return nil, fmt.Errorf("reading headings: %w", err)
	}
	var headings []parsedHeading
	seen := make([]bool, len(marked))
	var sel selectScope
	for n := range tree.Descendants() {
		sel.visit(n)
		if n.Type != html.ElementNode || !isHeadingName(n.Data) || belowHeading(n) || sel.lost(n) {
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
		headings = append(headings, parsedHeading{offset: marked[k], text: textOf(n)})
	}
	return headings, nil
}

// belowHeading reports whether n has a heading element above it.
func belowHeading(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && isHeadingName(p.Data) {
			return true
		}
	}
	return false
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

// filteredTags are the elements GitHub's tag filter escapes.
var filteredTags = []string{"title", "textarea", "style", "xmp", "iframe", "noembed", "noframes", "script", "plaintext"}

// tagFilter applies GitHub's tag filter to rendered HTML: the "<" of a start
// or end tag of a filtered element is written "&lt;", so a browser builds no
// element from it and reads it as text. A tag is the name in any ASCII case,
// then a space, a tab, a line end, ">" or "/>", as cmark-gfm's filter reads it.
func tagFilter(out []byte) []byte {
	var filtered []byte
	from := 0
	for at := 0; ; at++ {
		i := bytes.IndexByte(out[at:], '<')
		if i < 0 {
			break
		}
		at += i
		if !isFilteredTag(out[at+1:]) {
			continue
		}
		if filtered == nil {
			filtered = make([]byte, 0, len(out)+16)
		}
		filtered = append(append(filtered, out[from:at]...), "&lt;"...)
		from = at + 1
	}
	if filtered == nil {
		return out
	}
	return append(filtered, out[from:]...)
}

// isFilteredTag reports whether rest, the text after a "<", starts a tag the
// filter escapes. The end of the text ends a name as a line end does: cmark-gfm
// reads every document as ending in one.
func isFilteredTag(rest []byte) bool {
	if len(rest) > 0 && rest[0] == '/' {
		rest = rest[1:]
	}
	for _, name := range filteredTags {
		if !startsWithName(rest, name) {
			continue
		}
		switch after := rest[len(name):]; {
		case len(after) == 0, strings.IndexByte("> \t\n\r", after[0]) >= 0:
			return true
		case after[0] == '/' && len(after) > 1 && after[1] == '>':
			return true
		}
	}
	return false
}

// startsWithName reports whether b starts with name, a lower-case ASCII word,
// in any ASCII case.
func startsWithName(b []byte, name string) bool {
	if len(b) < len(name) {
		return false
	}
	for i := range len(name) {
		c := b[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != name[i] {
			return false
		}
	}
	return true
}

// lineStart returns the offset of the line that holds pos.
func lineStart(src []byte, pos int) int {
	pos = min(max(pos, 0), len(src))
	return bytes.LastIndexByte(src[:pos], '\n') + 1
}

// markdownHeadings maps each Markdown heading of the document to the offset of
// its line, or to -1 when it has no text.
func markdownHeadings(root ast.Node, src []byte) (map[int]*ast.Heading, error) {
	out := map[int]*ast.Heading{}
	if err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			out[headingOffset(h, src)] = h
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return nil, fmt.Errorf("reading headings: %w", err)
	}
	return out, nil
}

// headingOffset returns the offset of the line a Markdown heading's text
// starts on, or -1 when it has no text.
func headingOffset(h *ast.Heading, src []byte) int {
	if h.Lines().Len() == 0 {
		return -1
	}
	return lineStart(src, h.Lines().At(0).Start)
}

// textContent renders n's inline content to HTML and returns what a browser
// shows of it: every tag dropped and every character reference decoded. It
// reads text that holds no raw HTML but the tags of ownHTML.
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

// ownHTML are the tags the generator writes: the code element of a code cell
// that a code span cannot hold, its line break, linkBreak and the span of a
// dollarSpan.
var ownHTML = []string{"<code>", "</code>", "<br>", linkBreak, "<span>", "</span>"}

// ownInlines counts each internal link "#anchor" of a document that holds the
// generator's own text alone. There the generator writes internal links and no
// other, and the tags of ownHTML and no other, so any other link, image,
// autolink or raw HTML is schema text read as syntax, and is an error.
func ownInlines(root ast.Node, src []byte) (map[string]int, error) {
	out := map[string]int{}
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch l := n.(type) {
		case *ast.Link:
			dest := string(l.Destination)
			if anchor, ok := strings.CutPrefix(dest, "#"); ok {
				out[anchor]++
				return ast.WalkContinue, nil
			}
			return ast.WalkStop, fmt.Errorf("text outside doc comments is read as a link to %q", dest)
		case *ast.Image:
			return ast.WalkStop, fmt.Errorf("text outside doc comments is read as an image of %q", l.Destination)
		case *ast.AutoLink:
			return ast.WalkStop, fmt.Errorf("text outside doc comments is read as the autolink %q", l.URL(src))
		case *ast.RawHTML:
			if tag := string(l.Segments.Value(src)); !slices.Contains(ownHTML, tag) {
				return ast.WalkStop, fmt.Errorf("text outside doc comments is read as the HTML %q", tag)
			}
		case *ast.HTMLBlock:
			return ast.WalkStop, fmt.Errorf("text outside doc comments is read as an HTML block: %q", l.Lines().Value(src))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading inline text: %w", err)
	}
	return out, nil
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

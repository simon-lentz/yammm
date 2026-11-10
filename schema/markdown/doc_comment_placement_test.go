package markdown

import (
	"strings"
	"testing"
)

// marshalString loads src as a single-source schema and returns Marshal's
// document, failing the test when Marshal refuses it.
func marshalString(t *testing.T, src string) string {
	t.Helper()
	out, err := Marshal(loadSchema(t, src))
	if err != nil {
		t.Fatalf("Marshal = %v, want nil", err)
	}
	return string(out)
}

// TestMarshal_DescriptionCellLinkIsTheAuthors pins that a link in a property,
// edge-property or DataType doc comment is the author's Markdown, as a link in
// a type's doc comment is: the document generates, and the cell holds the link
// as written, whether it names a heading the document has or not.
func TestMarshal_DescriptionCellLinkIsTheAuthors(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, `schema "s"

/* See [terms](#glossary). */
type Code = String[1, 5]

type Person {
	/* The [Person](#person) key. */
	id String primary
	/* Defined in [terms](#glossary). */
	code Code
	--> KNOWS (one) Person {
		/* Since [they met](#glossary). */
		since Date
	}
}
`)
	for _, cell := range []string{
		"| The [Person](#person) key. |",
		"| Defined in [terms](#glossary). |",
		"| Since [they met](#glossary). |",
		"| See [terms](#glossary). |",
	} {
		if !strings.Contains(doc, cell) {
			t.Errorf("document has no cell %q:\n%s", cell, doc)
		}
	}
}

// TestMarshal_DocCommentUnderABulletReadsAsAtColumnZero pins that a relation's
// or an invariant's doc comment reads under its bullet as it reads at column
// 0, where closeAuthorText judges it. A line that starts with a tab is
// indented code there, so it opens no fence, no HTML comment and no heading
// that would swallow the edge-property table or the invariant's fence, or take
// an anchor a type's heading needs. Each comment holds a continuation line at
// column 0, so its tabs are the doc's text and not layout the parser removes.
func TestMarshal_DocCommentUnderABulletReadsAsAtColumnZero(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{
			"a tab before a fence opener",
			"schema \"s\"\n\ntype State {\n\tcode String primary\n}\n\ntype T {\n\tname String primary\n\t/* The fence syntax:\nsee below.\n\n\t``` opens a block */\n\t--> IN_STATE (one) State { since Date }\n}\n",
			[]string{"\n    \t``` opens a block\n\n    | Property | Type | Modifiers | Description |\n"},
		},
		{
			"a tab before a fence opener in an invariant's doc",
			"schema \"s\"\n\ntype T {\n\tid String primary\n\tage Integer required\n\t/* The fence syntax:\nsee below.\n\n\t``` opens a block */\n\t! \"adults only\" age >= 18\n}\n",
			[]string{"\n    \t``` opens a block\n\n    ```yammm\n"},
		},
		{
			"a tab before an HTML comment holding a heading",
			"schema \"s\"\n\ntype Order {\n\tid String primary\n\t/* Buyer.\nsee below.\n\n\t<!--\n\t# Person\n\t--> */\n\t--> BUYER (one) Person\n}\n\ntype Person {\n\tname String primary\n}\n",
			[]string{"[Person](#person)\n"},
		},
		{
			"a tab before an HTML comment left open",
			"schema \"s\"\n\ntype State {\n\tcode String primary\n}\n\ntype T {\n\tname String primary\n\t/* Note.\nsee below.\n\n\t<!-- note */\n\t--> IN_STATE (one) State { since Date }\n}\n",
			[]string{"\n    \t<!-- note\n\n    | Property | Type | Modifiers | Description |\n"},
		},
		{
			"a tab before a setext underline",
			"schema \"s\"\n\ntype Order {\n\tid String primary\n\t/* Buyer.\nsee below.\n\n\tDetails\n\t--- */\n\t--> BUYER (one) Details\n}\n\ntype Details {\n\tname String primary\n}\n",
			[]string{"[Details](#details)\n"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := marshalString(t, tt.src)
			for _, w := range tt.want {
				if !strings.Contains(doc, w) {
					t.Errorf("document does not hold %q:\n%s", w, doc)
				}
			}
		})
	}
}

// TestMarshal_ALoneCRInADocCommentEndsALine pins that a lone CR in a doc
// comment is written as the line end CommonMark reads it as, so the fence it
// opens is closed before the text after it, and a heading after it takes its
// anchor ahead of the type heading of the same name.
func TestMarshal_ALoneCRInADocCommentEndsALine(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, "schema \"s\"\n/* Example:\r```\rcode */\ntype A {\n  id String primary\n  --> TO (one) Zeta\n}\n/* Aardvark doc.\r# Zeta */\ntype Aardvark {\n  id String primary\n}\ntype Zeta {\n  id String primary\n}\n")
	if strings.Contains(doc, "\r") {
		t.Errorf("document holds a CR:\n%q", doc)
	}
	for _, want := range []string{"Example:\n```\ncode\n```\n", "[Zeta](#zeta-1)"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document does not hold %q:\n%s", want, doc)
		}
	}
}

// TestMarshal_RawHTMLHeadingTakesItsAnchor pins that a heading element an
// author writes as raw HTML takes its anchor in document order, as GitHub
// gives every h1 to h6 element one: a block, an inline one, an upper-case one,
// one with its own markup, and one left open, which the next heading closes.
// An element GitHub's tag filter escapes is text, and so is a heading in code.
func TestMarshal_RawHTMLHeadingTakesItsAnchor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, doc, link string }{
		{"a block heading", "<h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"an inline heading", "Text <h3>Zeta</h3> after.", "[Zeta](#zeta-1)"},
		{"an upper-case tag", "<H4>Zeta</H4>", "[Zeta](#zeta-1)"},
		{"markup inside", "<h2 id=\"x\">Ze<em>t</em>a</h2>", "[Zeta](#zeta-1)"},
		{"a heading left open closes at the next heading", "<h2>Zeta\n\n# Other", "[Zeta](#zeta-1)"},
		{"a heading in code is text", "`<h2>Zeta</h2>`", "[Zeta](#zeta)"},
		{"a filtered tag is text", "<h2>Ze<textarea>ta</h2>", "[Zeta](#zeta)"},
		{"a heading after a filtered tag", "a <title> b <h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a heading after a filtered tag's block", "<textarea>\n<h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a processing instruction is a comment", "<h2>Ze<?x?>ta</h2>", "[Zeta](#zeta-1)"},
		{"an empty comment", "<h2>Ze<!-->ta</h2>", "[Zeta](#zeta-1)"},
		{"an empty comment with a dash", "<h2>Ze<!--->ta</h2>", "[Zeta](#zeta-1)"},
		{"a comment ended by --!>", "<!-- x --!> <h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a bogus end tag is a comment", "<h2>Ze</ x>ta</h2>", "[Zeta](#zeta-1)"},
		{"a quoted attribute holding >", "<h2 title='a>b'>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a double-quoted attribute holding >", "<h2 title=\"a>b\">Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a quote inside an unquoted value", "<h2 x=a\"b>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a quote in an attribute name", "<h2 a\"b>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"an apostrophe in an unquoted value", "<h2 title=it's>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a heading nested in a heading is part of its text", "<h2>Z<em>eta<h3>X</h3></em></h2>", "[Zeta](#zeta)"},
		{"a heading nested in a heading takes no anchor", "<h2><div><h3>Zeta</h3>tail</div></h2>", "[Zeta](#zeta)"},
		{"a heading nested deeper in a heading takes no anchor", "<h1><b><h4><i><h5>Zeta</h5></i></h4>tail</b></h1>", "[Zeta](#zeta)"},
		{"a heading after a nested heading takes its anchor", "<h2><h3>X</h3></h2><h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a heading under a select takes no anchor", "<select><h3>Zeta</h3></select>", "[Zeta](#zeta)"},
		{"a less-than sign before a heading tag", "<<h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a heading in an option takes no anchor", "<select><option><h3>Zeta</h3></option></select>", "[Zeta](#zeta)"},
		{"a heading under a select left open takes no anchor", "<select>\n<h3>Zeta</h3>", "[Zeta](#zeta)"},
		{"a heading after a closed select takes its anchor", "<select></select><h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a keygen element closes a select", "<select><keygen><h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a keygen element in an option closes a select", "<select><option><keygen><h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a heading before a keygen element takes no anchor", "<select><h3>Zeta</h3><keygen>", "[Zeta](#zeta)"},
		{"a heading holding a keygen element takes no anchor", "<select><h3>Ze<keygen>ta</h3>", "[Zeta](#zeta)"},
		{"a keygen element in SVG closes a select", "<select><svg><keygen></svg><h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a keygen element in a template closes no select", "<select><template><keygen></template><h3>Zeta</h3>", "[Zeta](#zeta)"},
		{"a keygen element in a template in SVG closes no select", "<select><svg><template><keygen></template></svg><h3>Zeta</h3>", "[Zeta](#zeta)"},
		{"a heading in a template under a select takes its anchor", "<select><template><h3>Zeta</h3></template></select>", "[Zeta](#zeta-1)"},
		{"a heading in a template left open under a select takes its anchor", "<select>\n<template>\n<h3>Zeta</h3>", "[Zeta](#zeta-1)"},
		{"a select in SVG is no select", "<svg><select><foreignObject><h3>Zeta</h3></foreignObject></select></svg>", "[Zeta](#zeta-1)"},
		{"a select in MathML is no select", "<math><select><mi><h3>Zeta</h3></mi></select></math>", "[Zeta](#zeta-1)"},
		{"h7 is no heading", "<h7>Zeta</h7>", "[Zeta](#zeta)"},
		{"h1 is a heading", "<h1>Zeta</h1>", "[Zeta](#zeta-1)"},
		{"h6 is a heading", "<h6>Zeta</h6>", "[Zeta](#zeta-1)"},
		{"a heading tag after another tag", "<b>x</b> <i>y</i> <h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"a heading tag that ends the comment", "Zeta <h2", "[Zeta](#zeta)"},
		{"a setext heading under a line of equals signs", "Zeta\n===", "[Zeta](#zeta-1)"},
		{"a setext heading under a line of hyphens", "Zeta\n---", "[Zeta](#zeta-1)"},
		{"a setext heading under an indented underline", "Zeta\n   ---  ", "[Zeta](#zeta-1)"},
		{"a setext heading in a quote", "> Zeta\n> ===", "[Zeta](#zeta-1)"},
		{"a heading after a list marker", "- # Zeta", "[Zeta](#zeta-1)"},
		{"a mark with another nonce is a raw heading", "<h2 data-yammm-heading=AAAA-0>Zeta</h2>", "[Zeta](#zeta-1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := marshalString(t, "schema \"s\"\n/* "+tt.doc+" */\ntype A {\n  id String primary\n  --> TO (one) Zeta\n}\ntype Zeta {\n  id String primary\n}\n")
			if !strings.Contains(doc, tt.link) {
				t.Errorf("document does not link %s:\n%s", tt.link, doc)
			}
		})
	}
}

// TestMarshal_ARawHeadingInADescriptionCellTakesItsAnchor pins that a heading
// element a doc comment writes in a table cell takes its anchor in document
// order, as one written in a block does, and that a cell's Markdown heading
// line, which is text there, takes none.
func TestMarshal_ARawHeadingInADescriptionCellTakesItsAnchor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, doc, link string }{
		{"a heading element", "<h2>Zeta</h2>", "[Zeta](#zeta-1)"},
		{"an upper-case heading element after a line", "See\n<H3>Zeta</H3>", "[Zeta](#zeta-1)"},
		{"a Markdown heading line", "# Zeta", "[Zeta](#zeta)"},
		{"an underlined line", "Zeta\n===", "[Zeta](#zeta)"},
		{"a heading element under a select", "<select><h3>Zeta</h3></select>", "[Zeta](#zeta)"},
		{"a heading element after a keygen element in a select", "<select><keygen><h3>Zeta</h3>", "[Zeta](#zeta-1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := marshalString(t, "schema \"s\"\ntype A {\n  /* "+tt.doc+" */\n  id String primary\n  --> TO (one) Zeta\n}\ntype Zeta {\n  id String primary\n}\n")
			if !strings.Contains(doc, tt.link) {
				t.Errorf("document does not link %s:\n%s", tt.link, doc)
			}
		})
	}
}

// TestMarshal_AnOpenRawTextElementClosesOnPre pins that a doc comment's
// unclosed raw-text element is closed with </pre>, which ends the HTML block
// and which GitHub's tag filter leaves an element, not visible text.
func TestMarshal_AnOpenRawTextElementClosesOnPre(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"textarea", "script", "style", "pre"} {
		doc := marshalString(t, "schema \"s\"\n/* <"+tag+">\nx */\ntype A {\n  id String primary\n}\n")
		if want := "<" + tag + ">\nx\n</pre>\n"; !strings.Contains(doc, want) {
			t.Errorf("document does not close <%s> with </pre>:\n%s", tag, doc)
		}
	}
}

// TestMarshal_DescriptionCellKeepsTheAuthorsEscapes pins that a doc comment in
// a table cell keeps its backslashes, so an author's escape renders as it does
// in a type's doc comment, and a pipe renders as the author wrote it.
func TestMarshal_DescriptionCellKeepsTheAuthorsEscapes(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, "schema \"s\"\ntype A {\n  /* Not \\*emphasis\\* and a\\\\b and x|y. */\n  id String primary\n}\n")
	if want := "| Not \\*emphasis\\* and a\\\\b and x\\|y. |"; !strings.Contains(doc, want) {
		t.Errorf("document has no cell %q:\n%s", want, doc)
	}
}

// TestSelfCheck_NamesTheFirstLinkFailureInOrder pins that the self-check's
// error does not depend on map order: with several failing links it names the
// first by anchor, on every run.
func TestSelfCheck_NamesTheFirstLinkFailureInOrder(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n}\n")
	for range 40 {
		g := newTestGenerator(t, s)
		g.emitDocument()
		g.links = append(g.links, "zz", "mm", "aa")
		_, err := g.finish()
		if err == nil || !strings.Contains(err.Error(), "#aa ") {
			t.Fatalf("finish = %v, want the failure for #aa", err)
		}
		g = newTestGenerator(t, s)
		g.emitDocument()
		g.buf.WriteString("\n[a](#zz) [b](#mm) [c](#aa)\n")
		_, err = g.finish()
		if err == nil || !strings.Contains(err.Error(), "#aa ") {
			t.Fatalf("finish = %v, want the unwritten link #aa named", err)
		}
	}
}

// TestMarshal_AnHTMLConstructADocCommentLeavesOpenIsClosed pins that raw HTML
// a doc comment leaves open — a comment, a quoted attribute value, a tag, a
// noscript element's text — is closed after the comment by a line that reads
// as nothing, so it takes none of the generator's text, and the schema
// generates. A Markdown heading inside the comment that the open HTML takes is
// no heading, as on GitHub.
func TestMarshal_AnHTMLConstructADocCommentLeavesOpenIsClosed(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ doc, closer string }{
		{"<div title=\"x\n\n# Heading", "\n<wbr x='\"'>\n"},
		{"<div class='a\n\n## Sub", "\n<wbr x='\"'>\n"},
		{"<div>\n<!-- open\n\n# Heading", "\n<!-- -->\n"},
		{"<!-- a --> <!-- b", "\n<!-- -->\n"},
		{"<pre>\n<!-- x", "\n</pre>\n<!-- -->\n"},
		{"<div title=x", "\n<!-- -->\n"},
		{"<p", "\n<!-- -->\n"},
		{"<P class=x", "\n<!-- -->\n"},
		{"text\n<p", "\n<!-- -->\n"},
		{"<div><p", "\n<!-- -->\n"},
		{"<p title=\"x", "\n<wbr x='\"'>\n"},
		{"<div><yammm-end-of-text", "\n<!-- -->\n"},
		{"<div>yammm-end-of-text</div><div title=\"x", "\n<wbr x='\"'>\n"},
		{"<div>\n<yammm-end-of-text>\n<!-- open", "\n<!-- -->\n"},
		{"<div>\n<yammm-end-of-text></yammm-end-of-text>\n<!-- open", "\n<!-- -->\n"},
		{"<frameset", "\n<!-- -->\n"},
		{"<frameset>\n<!--", "\n<!-- -->\n"},
		{"<frameset>\n<a href=\"", "\n<wbr x='\"'>\n"},
		{"<frameset>", "\n\n"},
		{"<noscript>\nx", "\n\n"},
		{"<svg>\n<![CDATA[ x", "\n<![CDATA[ ]]>\n"},
		{"<div title=\"x\">y</div>", "\n\n"},
		{"<template><col>", "\n\n"},
		{"<select>\n<template>\n<keygen>\n</template>", "\n</select>\n"},
		{strings.Repeat("<div>", 600), "\n\n"},
	} {
		out := marshalString(t, "schema \"s\"\n/* "+tt.doc+" */\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n")
		if !strings.Contains(out, tt.doc+tt.closer) {
			t.Errorf("doc %q is not followed by %q:\n%s", tt.doc, tt.closer, out)
		}
		if !strings.Contains(out, "[B](#b)") {
			t.Errorf("doc %q: the link to B does not target #b:\n%s", tt.doc, out)
		}
	}
}

// TestMarshal_ADescriptionCellTakesNoCloser pins that a doc comment in a table
// cell is written as its inline Markdown and nothing after it: what opens a
// block when the comment is read alone opens nothing in a cell, where a closer
// would end a comment the author never opened and hide the words before it.
func TestMarshal_ADescriptionCellTakesNoCloser(t *testing.T) {
	t.Parallel()

	for _, doc := range []string{"<noscript>", "a <noscript> b", "<!-- two words", "<!X y z", "<? x <!-- y", "<div title=\"x", "<![CDATA[ x"} {
		out := marshalString(t, "schema \"s\"\ntype A {\n  /* "+doc+" */\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n")
		if !strings.Contains(out, "| "+doc+" |\n") || !strings.Contains(out, "[B](#b)") {
			t.Errorf("doc %q is not its cell's whole text:\n%s", doc, out)
		}
	}
}

// TestHeadingReader_AMarkdownHeadingOpenHTMLTakes pins the reader's rule for a
// Markdown heading that no heading element holds: it is no heading, as on
// GitHub, and the read returns the others.
func TestHeadingReader_AMarkdownHeadingOpenHTMLTakes(t *testing.T) {
	t.Parallel()

	r := newHeadingReader()
	src := []byte("# Kept\n\n<div title=\"x\n\n# Taken\n")
	got, err := r.headings(parse(newParser(), src), src)
	if err != nil || len(got) != 1 || got[0].text != "Kept" || got[0].offset != 0 {
		t.Errorf("headings = %+v, %v; want Kept alone", got, err)
	}
}

// TestNewHeadingReader_DrawsItsOwnNonce pins that no two readers share a
// nonce, so no document can hold a mark a later reader accepts.
func TestNewHeadingReader_DrawsItsOwnNonce(t *testing.T) {
	t.Parallel()

	if a, b := newHeadingReader().marker.nonce, newHeadingReader().marker.nonce; a == b || a == "" {
		t.Errorf("nonces %q and %q, want two distinct ones", a, b)
	}
}

// TestTableWriter_KeepsTheFirstFault pins that a table's first bad row names
// the fault the self-check reports.
func TestTableWriter_KeepsTheFirstFault(t *testing.T) {
	t.Parallel()

	var g generator
	tw := g.newTable(0, "A", "B")
	tw.row(tableCell{md: "1"})
	tw.row(tableCell{md: "1"}, tableCell{md: "2\n3"})
	if tw.bad != "table row has 1 cells, its header 2" {
		t.Errorf("bad = %q, want the first row's fault", tw.bad)
	}
}

// TestMarshal_AnAliasIsSchemaText pins that the entry's own tag for an
// imported type is escaped as schema text: an alias www would otherwise make
// GitHub read www.Person as a link.
func TestMarshal_AnAliasIsSchemaText(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": "schema \"e\"\nimport \"w.yammm\" as www\ntype T {\n  id String primary\n  --> AT (one) www.Person\n}\n",
		"w.yammm":     "schema \"w\"\ntype Person {\n  id String primary\n}\n",
	})
	assertLines(t, doc, "### www<!---->.Person", "-   `--> AT (one)` [www<!---->.Person](#wwwperson)")
}

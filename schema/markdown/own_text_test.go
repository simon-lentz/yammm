package markdown

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// documentedGenerator returns a generator that has emitted a schema holding
// doc at every position a schema holds a doc comment.
func documentedGenerator(t *testing.T, doc string) *generator {
	t.Helper()
	docs := map[string]string{}
	for _, position := range docPositions {
		docs[position] = doc
	}
	s, res := schema.LoadSourcesWithEntry(context.Background(), schemaWithDocs(docs), "entry.yammm", ".", schema.WithSourcesOnly(true))
	if err := res.Err(); err != nil {
		t.Fatalf("the schema does not load: %v", err)
	}
	g := newTestGenerator(t, s)
	g.emitDocument()
	return g
}

// TestOwnText_HoldsTheGeneratorsTextAtItsOffsets pins what the self-check
// reads: the document with every doc comment replaced, a block by a paragraph
// at the block's column and a cell by an empty cell, and each offset of the
// generator's text mapped to where that text is there.
func TestOwnText_HoldsTheGeneratorsTextAtItsOffsets(t *testing.T) {
	t.Parallel()

	const doc = "MARK one\n\n- MARK two\n-"
	g := documentedGenerator(t, doc)
	own, at := g.ownText()
	if bytes.Contains(own, []byte("MARK")) {
		t.Errorf("the generator's text holds a doc comment:\n%s", own)
	}
	for _, want := range []string{"# Schema t\n\ndoc\n\n## Class Diagram", "### A\n\ndoc\n\n| Property", "| `id` | `String` | primary |  |\n", "[B](#b)\n\n    doc\n\n    | Property", "    | `since` | `Date` |  |  |\n", "\"adults only\"\n\n    doc\n\n    ```yammm\n", "| `Code` | `String[1, 5]` |  |\n", "(imported as far)\n\ndoc\n\n### far.F"} {
		if !bytes.Contains(own, []byte(want)) {
			t.Errorf("the generator's text does not hold %q:\n%s", want, own)
		}
	}
	real := g.buf.Bytes()
	for _, h := range g.outline {
		line, _, _ := bytes.Cut(real[h.offset:], []byte("\n"))
		if !bytes.HasPrefix(own[at(h.offset):], append(line, '\n')) {
			t.Errorf("heading %q at %d is mapped to %d, which holds %q", h.text, h.offset, at(h.offset), firstLine(own[at(h.offset):]))
		}
	}
	for _, tw := range g.tables {
		if want := tw.indent + "| "; !bytes.HasPrefix(own[at(tw.offset):], []byte(want)) {
			t.Errorf("a table at %d is mapped to %d, which holds %q", tw.offset, at(tw.offset), firstLine(own[at(tw.offset):]))
		}
	}
	if !bytes.HasPrefix(own[at(g.diagramAt):], []byte("```mermaid\n")) {
		t.Errorf("the diagram at %d is mapped to %d, which holds %q", g.diagramAt, at(g.diagramAt), firstLine(own[at(g.diagramAt):]))
	}
	for _, a := range g.authored {
		if after := at(a.end); own[after] != real[a.end] || string(own[after-len(a.standIn):after]) != a.standIn {
			t.Errorf("the text after the doc comment that ends at %d is mapped to %d, which follows %q", a.end, after, own[max(after-8, 0):after])
		}
	}
	if at(0) != 0 || at(len(real)) != len(own) {
		t.Errorf("the document's ends map to %d and %d, want 0 and %d", at(0), at(len(real)), len(own))
	}

	bare := newTestGenerator(t, loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n}\n"))
	bare.emitDocument()
	if own, at := bare.ownText(); &own[0] != &bare.buf.Bytes()[0] || len(own) != bare.buf.Len() || at(7) != 7 {
		t.Errorf("a document with no doc comment is not read as it stands")
	}
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return string(line)
}

// TestSelfCheck_FindsAGeneratorFaultBesideDocComments pins that the check
// still reads everything the generator wrote when doc comments surround it,
// here ones that leave a list item, a tag and a fence of tildes open: each
// fault in the generator's own text is refused, and the document without the
// fault is not.
func TestSelfCheck_FindsAGeneratorFaultBesideDocComments(t *testing.T) {
	t.Parallel()

	const doc = "Notes:\n\n-\n\n<div title=\"x\n\n# B\n\n~~~\n<noscript>"
	if _, err := documentedGenerator(t, doc).finish(); err != nil {
		t.Fatalf("finish = %v, want the document", err)
	}
	for _, tt := range []struct{ name, old, new, want string }{
		{"a heading of another level", "### B\n\n", "#### B\n", `heading "B" is read as level 4`},
		{"a heading whose text moved", "### B\n", "### b\n", `heading "B" is read as level 3 "b"`},
		{"a heading in a quote", "\n\n### B\n", "\n>### B\n", `heading "B" is not read as a top-level heading`},
		{"a link that is no link", "[B](#b)", "[B]#b()", "written 1 times and read 0"},
		{"a link to elsewhere", "[B](#b)", "[B](/b)", `read as a link to "/b"`},
		{"an image", "", "\n![x](y)\n", `read as an image of "y"`},
		{"a table that is no table", "    | --- | --- | --- | --- |", "    | -x- | --- | --- | --- |", "not read as a table"},
		{"a fence left open", "", "\n```\n", "ends inside an open block"},
		{"a heading the generator did not write", "", "\n## Stray\n", `read as the heading "Stray"`},
		{"the first of two headings the generator did not write", "", "\n## First\n\n## Second\n", `read as the heading "First"`},
		{"an HTML block", "", "\n<h2>Stray</h2>\n", "read as an HTML block"},
		{"a tag", "", "\nA <b>stray</b> word.\n", `read as the HTML "<b>"`},
		{"an autolink", "", "\nSee www.example.com for more.\n", "read as the autolink"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := documentedGenerator(t, doc)
			text := g.buf.String()
			if tt.old == "" {
				text += tt.new
			} else {
				if len(tt.old) != len(tt.new) || !strings.Contains(text, tt.old) {
					t.Fatalf("the fault %q does not replace %q byte for byte", tt.new, tt.old)
				}
				text = strings.Replace(text, tt.old, tt.new, 1)
			}
			g.buf.Reset()
			g.buf.WriteString(text)
			if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("finish = %v, want an error holding %q", err, tt.want)
			}
		})
	}
}

// TestSelfCheck_ReadsTheGeneratorsOwnTagsAsItsOwn pins that the check accepts
// every tag the generator writes: the code element of a code cell that a code
// span cannot hold, the <br> of a line break in it, and the empty comment
// that splits what GitHub would read as a link.
func TestSelfCheck_ReadsTheGeneratorsOwnTagsAsItsOwn(t *testing.T) {
	t.Parallel()

	g := documentedGenerator(t, "a doc comment")
	g.buf.WriteString("\n")
	g.newTable(0, "Name", "Definition").row(codeCellOf("a`b\nc\\|d"), tableCell{md: escapeInline("www.example.com and a:b")})
	out, err := g.finish()
	if err != nil {
		t.Fatalf("finish = %v, want the document", err)
	}
	if want := "| <code>a&#96;b<br>c&#92;\\|d</code> | www<!---->.example.com and a<!---->:b |\n"; !strings.HasSuffix(string(out), want) {
		t.Errorf("the document does not end in %q:\n%s", want, out[len(out)-200:])
	}
}

// TestSelfCheck_AnchorsAreTheOutlinesOwn pins the anchor rule the check holds
// the outline to: an anchor is its heading's slug or a numbered form of it,
// and no two headings take one.
func TestSelfCheck_AnchorsAreTheOutlinesOwn(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\ntype Types {\n  id String primary\n}\n")
	fresh := func() *generator {
		g := newTestGenerator(t, s)
		g.emitDocument()
		return g
	}
	if _, err := fresh().finish(); err != nil {
		t.Fatalf("finish = %v, want the document", err)
	}
	for _, tt := range []struct{ anchor, want string }{
		{"types", "two headings take anchor #types"},
		{"types-2", ""},
		{"types-", "takes anchor #types-,"},
		{"types-0", "takes anchor #types-0,"},
		{"types-01", "takes anchor #types-01,"},
		{"types-x", "takes anchor #types-x,"},
		{"types2", "takes anchor #types2,"},
		{"types--2", "takes anchor #types--2,"},
		{"typesx", "takes anchor #typesx,"},
		{"type", "takes anchor #type,"},
		{"", "takes anchor #,"},
	} {
		g := fresh()
		last := len(g.outline) - 1
		if g.outline[last].text != "Types" || g.outline[last].anchor != "types-1" {
			t.Fatalf("the last heading is %q at #%s, want Types at #types-1", g.outline[last].text, g.outline[last].anchor)
		}
		g.outline[last].anchor = tt.anchor
		_, err := g.finish()
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("the type's heading at #%s: finish = %v, want %q", tt.anchor, err, tt.want)
		}
	}
}

// TestMarshal_AHeadingTheReadLosesIsAnchoredAfterTheReadOnes pins that what
// a doc comment's HTML does to the read of the document's headings moves no
// verdict and loses no anchor: a heading of the generator's that the read
// does not find is anchored after every heading the read finds, and a
// document the read cannot build keeps the outline's order.
func TestMarshal_AHeadingTheReadLosesIsAnchoredAfterTheReadOnes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, doc, link string }{
		{"a doc comment heading before the lost ones is counted", "# B\n\n<template><col>", "[B](#b-1)"},
		{"a doc comment heading among the lost ones is not", "<template><col>\n\n# B", "[B](#b)"},
		{"a tree no parser builds", "# B\n\n" + strings.Repeat("<div>", 600), "[B](#b)"},
		{"a heading of the comment's alone", "# B", "[B](#b-1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := marshalString(t, "schema \"s\"\n/* "+tt.doc+" */\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n")
			if !strings.Contains(doc, tt.link+"\n") {
				t.Errorf("the document does not link %s:\n%.600s", tt.link, doc)
			}
		})
	}
}

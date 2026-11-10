package markdown

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// docPositions names every place a schema holds a doc comment that Marshal
// writes: three blocks at column 0, two blocks under a bullet, and three
// description cells.
var docPositions = []string{"schema", "type", "relation", "invariant", "property", "edge", "datatype", "imported"}

// schemaWithDocs returns the sources of a schema that holds docs[position] as
// the doc comment at each position named in docs, with a type, a table and a
// heading after every one of them.
func schemaWithDocs(docs map[string]string) map[string][]byte {
	d := func(position string) string {
		doc, ok := docs[position]
		if !ok {
			return ""
		}
		return "/* " + doc + " */\n"
	}
	entry := d("schema") + "schema \"t\"\n\nimport \"far.yammm\" as far\n\n" +
		d("datatype") + "type Code = String[1, 5]\n\n" +
		d("type") + "type A {\n" +
		d("property") + "\tid String primary\n\tage Integer required\n\tcode Code\n" +
		d("relation") + "\t--> TO (one) B {\n" + d("edge") + "\t\tsince Date\n\t}\n" +
		"\t--> AT (one) far.F\n" +
		d("invariant") + "\t! \"adults only\" age >= 18\n}\n\n" +
		"type B {\n\tid String primary\n}\n"
	far := d("imported") + "schema \"far\"\n\ntype F {\n\tid String primary\n}\n"
	return map[string][]byte{"entry.yammm": []byte(entry), "far.yammm": []byte(far)}
}

// refusedDocs are doc comments that made Marshal refuse a schema that loads,
// at one position or more, and their controls. No doc comment is written with
// the two characters that end a block comment.
var refusedDocs = []struct{ name, doc string }{
	// A table under a bullet after a list item with no content.
	{"an empty bullet item", "-"},
	{"an empty star item", "*"},
	{"an empty plus item", "+"},
	{"an empty ordered item", "1."},
	{"an empty parenthesis item", "1)"},
	{"an empty two-digit item", "10."},
	{"a list that ends in an empty item", "Notes:\n- a\n-"},
	{"an empty item after a blank line", "Notes:\n\n*"},
	{"open questions", "Open questions:\n\n-"},
	{"steps that end in an empty item", "Steps:\n\n1. one\n2."},
	{"a nested empty item", "- a\n  -"},
	{"a full list", "Notes:\n- a\n- b"},

	// An empty heading inside HTML the comment leaves open.
	{"an empty heading after an open p tag", "text\n\n<p\n\n#"},
	{"an empty heading after an open div tag", "<div\n\n#\n\nmore"},
	{"an empty heading inside noscript", "<noscript>\n\n#\n\n</noscript>\n\ntail"},
	{"an empty heading inside an open attribute", "<h2 x=\"\n\n#"},
	{"an empty heading alone", "text\n\n#"},
	{"an empty heading inside a comment", "<!-- draft\n\n#\n\n-->"},
	{"a heading after an open div tag", "<div\n\n# x\n\nmore"},
	{"a heading after an open p tag", "text\n\n<p\n\n# x"},

	// Nesting past an HTML parser's limit.
	{"600 block quotes", strings.Repeat("> ", 600) + "x"},
	{"520 block quotes", strings.Repeat("> ", 520) + "x"},
	{"600 div elements", strings.Repeat("<div>", 600)},
	{"600 b elements", strings.Repeat("<b>", 600)},

	// A cell whose text opens a block when it is read alone.
	{"a tilde fence holding noscript", "~~~\n<noscript>\n~~~"},
	{"a tag named p<noscript", "<h2><p<noscript>"},
	{"a backtick fence left open on noscript", "```\n<noscript>"},
	{"a CDATA section left open on noscript", "<![CDATA[\n<noscript>"},
	{"a reference definition before noscript", "[x]: /u\n<noscript>"},
	{"a processing instruction opener before noscript", "<?<noscript>"},
	{"noscript alone", "<noscript>"},
	{"noscript in indented code", "Example:\n\n    <noscript>"},
	{"a closed fenced example", "```html\n<noscript>enable JS</noscript>\n```"},

	// A frameset.
	{"an open frameset tag", "<frameset"},
	{"a frameset element", "<frameset>"},
	{"a frameset and noscript", "<frameset>\n<noscript>"},
	{"a frameset and an open comment", "<frameset>\n<!--"},
	{"a frameset and an open single-quoted attribute", "<frameset><a href='"},
	{"a frameset and an open double-quoted attribute", "<frameset>\n<a href=\""},
	{"a frameset and an open CDATA section", "<frameset>\n<![CDATA["},
	{"a frameset and an open processing instruction", "<frameset>\n<?x"},
	{"a frameset and an open div tag", "<frameset>\n<div"},

	// Open HTML before an unfinished reference definition.
	{"noscript before a reference definition", "<noscript>\n\n[a]:"},
	{"an open attribute before a reference definition", "<div title=\"x\n\n[a]:"},
	{"inline noscript before a reference definition", "See <noscript> for more.\n\n[a]:"},
	{"a reference definition alone", "Text.\n\n[a]:"},
	{"a comment before a reference definition", "<!-- todo\n\n[a]:"},

	// A template element.
	{"a template holding a col", "<template><col>"},
	{"a template", "<template>"},
	{"an svg foreignObject", "<svg><foreignObject>"},
	{"nested column groups", "<template><col><template><col>"},
	{"column groups on two lines", "<template><col>\n<template><col>"},
	{"a column group and an open comment", "<div><template><col>\n<!-- open"},
	{"a column group and an open attribute", "<div><template><col title=\"x"},
	{"a frameset and a column group", "<frameset><template><col>"},
	{"a template under svg", "<svg><foreignObject><template></template>"},
	{"a column group under MathML", "<math><mi><template><col>"},
	{"a template under an HTML annotation", "<math><annotation-xml encoding=\"text/html\"><template></template>"},
	{"512 column groups", strings.Repeat("<template><col>", 512)},
	{"512 div elements", strings.Repeat("<div>", 512)},

	// An open tag named p.
	{"an open p tag", "<p"},
	{"an open upper-case P tag", "<P"},
	{"an open p tag with an attribute name", "<p x"},
	{"an open p tag with an attribute", "<p class=x"},
	{"text before an open p tag", "text\n<p"},
	{"an open p tag inside a div", "<div><p"},
	{"an open a tag", "<a"},
	{"an open b tag", "<b"},
	{"an open span tag", "<span"},
	{"an open div tag", "<div"},
	{"an open table tag", "<table"},
	{"an open h2 tag", "<h2"},
	{"an open li tag", "<li"},

	// A heading tag cut short by the comment's end.
	{"a doc that ends in <h", "see <h"},
	{"a doc that ends in <H", "see <H"},

	// Elements an HTML parser reads its own way.
	{"a select", "<select>"},
	{"a select and an option", "<select><option>a"},
	{"a select in text", "text <select> more"},
	{"noscript and an open comment", "<noscript>\n<!--"},
	{"Markdown inside a div", "<div>\n*a* \\* [x](y)\n</div>"},
	{"Markdown inside a pre", "<pre>\n*a*\n</pre>"},
	{"a heading and a line", "# Head\nmore"},
	{"a pipe table", "a\n\n| x | y |\n|---|---|\n| 1 | 2 |"},
	{"a list", "- one\n- two"},
	{"a block quote", "> quoted\nmore"},
	{"a style tag with a slash", "<div>\n<style/x>\ntext"},
	{"a plaintext element", "<plaintext>"},
	{"an open textarea", "<textarea>\nx"},
	{"an open attribute", "<div title=\"x"},
	{"an open comment", "<!-- x"},
	{"an unclosed fence of tildes", "~~~\nx"},
	{"a table element left open", "<table><tr><td>"},
}

// TestMarshal_NoDocCommentIsRefused pins that a doc comment's text never
// decides Marshal's verdict: each of these comments, at every position a
// schema holds one, yields a document, and so do two of them at once.
func TestMarshal_NoDocCommentIsRefused(t *testing.T) {
	t.Parallel()

	generate := func(t *testing.T, docs map[string]string) {
		t.Helper()
		s, res := schema.LoadSourcesWithEntry(context.Background(), schemaWithDocs(docs), "entry.yammm", ".", schema.WithSourcesOnly(true))
		if err := res.Err(); err != nil {
			t.Fatalf("the schema does not load: %v", err)
		}
		for _, opts := range [][]Option{nil, {WithClassDiagram(false)}} {
			out, err := Marshal(s, opts...)
			if err != nil {
				t.Errorf("Marshal = %v, want a document", err)
				continue
			}
			for _, want := range []string{"\n### A\n", "\n### B\n", "\n## Data Types\n", "\n### far.F\n"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("the document holds no line %q", want)
				}
			}
		}
	}
	for _, tt := range refusedDocs {
		for _, position := range docPositions {
			t.Run(tt.name+"/"+position, func(t *testing.T) {
				t.Parallel()
				generate(t, map[string]string{position: tt.doc})
			})
		}
		t.Run(tt.name+"/everywhere", func(t *testing.T) {
			t.Parallel()
			docs := map[string]string{}
			for _, position := range docPositions {
				docs[position] = tt.doc
			}
			generate(t, docs)
		})
	}
	// Two comments whose constructs meet: one opens what the other is read in.
	for _, pair := range [][2]string{
		{"<svg><foreignObject>", "<template>"},
		{"<frameset>", "<noscript>"},
		{"<noscript>", "<!--"},
		{"<select>", "<p"},
		{"<template><col><template>", "</template>"},
		{"<template><template>", "</template><col>"},
	} {
		t.Run(pair[0]+" and "+pair[1], func(t *testing.T) {
			t.Parallel()
			generate(t, map[string]string{"type": pair[0], "property": pair[1], "edge": pair[1], "relation": pair[0]})
			generate(t, map[string]string{"schema": pair[0], "invariant": pair[1], "datatype": pair[1]})
		})
	}
}

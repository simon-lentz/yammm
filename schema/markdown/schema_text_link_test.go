package markdown

import (
	"strings"
	"testing"
)

// TestMarshal_SchemaTextIsNeverALink pins that text a schema supplies — an
// invariant message and a constraint in a <code> cell — reads as no link: GitHub autolinks a www
// host, a URL and an email address even through backslash escapes and entity
// codes, so an empty HTML comment, which a browser does not show, splits each
// before its colon, its at sign or the dot after www.
func TestMarshal_SchemaTextIsNeverALink(t *testing.T) {
	t.Parallel()

	doc := marshalSources(t, map[string]string{
		"entry.yammm": `schema "app"

import "mid.yammm" as m

type Car {
	id String primary
	--> AT (one) m.Hub
	! "mail a@b.com or visit https://x.org/p" id != ""
	! "see www.example.com" id != "x"
}
`,
		"mid.yammm": `schema "mid"

import "far.yammm" as f

type Hub extends f.Base {
	id String primary
	tag Enum["a` + "`" + `b a@b.com WWW.q.org ftp://h", "z"]
	--> OWNER (one) f.Person
}
`,
		"far.yammm": `schema "far"

type Person {
	id String primary
}

abstract type Base {
	note String
}
`,
	})

	assertLines(t, doc,
		"-   \"mail a<!---->@b.com or visit https<!---->://x.org/p\"",
		"-   \"see www<!---->.example.com\"",
	)
	if want := "<code>Enum&#91;\"a&#96;b a<!---->@b.com WWW<!---->.q.org ftp<!---->://h\", \"z\"&#93;</code>"; !strings.Contains(doc, want) {
		t.Errorf("document holds no code cell %q:\n%s", want, doc)
	}
}

// TestSelfCheck_SchemaTextReadAsALinkFails pins that the self-check refuses a
// link outside doc comments that the generator did not write: an autolink, an
// external link and an image.
func TestSelfCheck_SchemaTextReadAsALinkFails(t *testing.T) {
	t.Parallel()

	s := loadSchema(t, "schema \"s\"\ntype A {\n  id String primary\n}\n")
	for _, tt := range []struct{ name, text, want string }{
		{"a www autolink", "www.example.com", "autolink"},
		{"a URL autolink", "https://x.org", "autolink"},
		{"an email autolink", "a@b.com", "autolink"},
		{"an angle autolink", "<https://x.org>", "autolink"},
		{"an external link", "[x](https://x.org)", "link to"},
		{"an image", "![x](y.png)", "image"},
		{"a link with no text", "[](https://x.org)", "link to"},
		{"an image with no text", "![](y.png)", "image"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newTestGenerator(t, s)
			g.emitDocument()
			g.buf.WriteString("\n" + tt.text + "\n")
			if _, err := g.finish(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("finish = %v, want an error naming %q", err, tt.want)
			}
		})
	}
	t.Run("a doc comment's autolink is the author's", func(t *testing.T) {
		t.Parallel()
		doc := marshalString(t, "schema \"s\"\n/* See www.example.com and a@b.com [](https://x.org) ![](y.png). */\ntype A {\n  /* https://x.org [](#nowhere) */\n  id String primary\n}\n")
		if !strings.Contains(doc, "See www.example.com and a@b.com [](https://x.org) ![](y.png).") {
			t.Errorf("the doc comment is not written as its author wrote it:\n%s", doc)
		}
	})
}

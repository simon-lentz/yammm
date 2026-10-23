package gogen_test

import (
	"context"
	"go/build/constraint"
	"go/format"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/gogen"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// docWords returns doc's lines that hold text, each as its words with one
// space between them, a form feed read as the white space it is.
func docWords(doc string) []string {
	var out []string
	for line := range strings.SplitSeq(doc, "\n") {
		if words := strings.Fields(line); len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

// documentedCar returns a schema whose type carries typeDoc and whose second
// property carries propertyDoc, each the text between "/*" and "*/"; an empty
// one writes no comment.
func documentedCar(typeDoc, propertyDoc string) string {
	var b strings.Builder
	b.WriteString("schema \"g\"\n\n")
	if typeDoc != "" {
		b.WriteString("/*" + typeDoc + "*/\n")
	}
	b.WriteString("type Car {\n\tid String primary\n")
	if propertyDoc != "" {
		b.WriteString("\t/*" + propertyDoc + "*/\n")
	}
	b.WriteString("\tname String\n}\n")
	return b.String()
}

// TestMarshal_ReturnsSourceGofmtLeavesAsWritten pins doc comments gofmt
// rewrites, most of them ones for which one gofmt pass can write a file a
// second pass writes again: a list item, a numbered item and then an indented
// line, which go/doc/comment reads one way before the first pass and another
// after it, and lines of white space that is neither a space nor a tab, which
// the schema holds as text and Go reads as blank. gofmt itself judges each
// generated file. The backslash row is one a second pass does not settle
// either: gofmt writes it three times.
func TestMarshal_ReturnsSourceGofmtLeavesAsWritten(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"a type's list item, backslash line and indented line":    documentedCar(" \n - :\n\\\n : ", ""),
		"a type's list item, numbered item and indented line":     documentedCar(" - x\n   1. x\n    . ", ""),
		"a type's numbered item, list item and indented line":     documentedCar(" 1. :\n   - :\n    ] ", ""),
		"a property's list item, numbered item and indented line": documentedCar("", " - x\n\t   1. x\n\t    . "),
		"a property documented by a vertical tab alone":           documentedCar("", " \v "),
		"a property documented by a form feed alone":              documentedCar("", " \f "),
		"a type's indented line between lines of form feeds":      documentedCar(" \f\n     \v go:build\f\n    \f\f ", ""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertGofmtLeaves(t, marshalString(t, src))
		})
	}
}

// TestMarshal_DrawnDocCommentsAreGofmtFormatted draws doc comments from the
// words Go reads build lines by, form feeds and vertical tabs among them, on a
// type and on a property, and holds every generated file to three judges:
// gofmt writes it back byte for byte, no comment in it is a build line, and
// the doc go/ast reads for each declaration holds the schema's documentation
// word for word. The property holds no shared state for the race detector to
// watch, so the run without the detector is the one that draws.
func TestMarshal_DrawnDocCommentsAreGofmtFormatted(t *testing.T) {
	raceskip.Skip(t)
	t.Parallel()
	words := []string{"+build", "+build", "//go:build", "go:build", "//", "ignore", "x", "\f", "\f", "\v", " ", " ", "  "}
	rng := rand.New(rand.NewPCG(20261013, 1)) //nolint:gosec // a seeded generator makes the draws repeatable
	content := func(indent string) string {
		var b strings.Builder
		b.WriteString(" ")
		for line := range 1 + rng.IntN(3) {
			if line > 0 {
				b.WriteString("\n" + indent)
			}
			for range 1 + rng.IntN(5) {
				b.WriteString(words[rng.IntN(len(words))])
			}
			// A suffix after the words makes most lines end in other than a blank.
			b.WriteString([]string{"t", "", "\f"}[rng.IntN(3)])
		}
		return b.String() + " "
	}
	for i := range 3000 {
		src := documentedCar(content("   "), content("\t   "))
		s, res := schema.LoadString(context.Background(), src, "gen.yammm")
		if res.HasErrors() {
			t.Fatalf("draw %d does not load: %v\n%q", i, res.Err(), src)
		}
		generated, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("draw %d: Marshal: %v\n%q", i, err, src)
		}
		formatted, err := format.Source(generated)
		if err != nil {
			t.Fatalf("draw %d: gofmt: %v", i, err)
		}
		if string(formatted) != string(generated) {
			t.Fatalf("draw %d: gofmt rewrites the generated file for %q", i, src)
		}
		f, err := parser.ParseFile(token.NewFileSet(), "car.go", generated, parser.ParseComments)
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		for _, group := range f.Comments {
			for _, c := range group.List {
				if constraint.IsPlusBuild(c.Text) || constraint.IsGoBuild(c.Text) {
					t.Fatalf("draw %d: the output holds the build line %q for %q", i, c.Text, src)
				}
			}
		}
		car, ok := s.Type("Car")
		if !ok {
			t.Fatalf("draw %d: the schema holds no Car", i)
		}
		name, ok := car.Property("name")
		if !ok {
			t.Fatalf("draw %d: Car holds no name", i)
		}
		for holder, doc := range map[string]string{"Car": car.Documentation(), "Name": name.Documentation()} {
			got, want := docWords(docOf(f, holder)), docWords(doc)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("draw %d: %s's doc reads %q, want %q, for %q", i, holder, got, want, src)
			}
		}
	}
}

// TestMarshal_DrawnDocLayoutsAreGofmtFormatted draws doc comments from what
// the Go doc comment formatter reads as structure (headings, list items,
// links, deeper lines) and from the white space characters in words, and holds each
// generated file to gofmt writing it back byte for byte. A draw the loader
// refuses is passed over. It skips under the race detector as the test above
// does.
func TestMarshal_DrawnDocLayoutsAreGofmtFormatted(t *testing.T) {
	raceskip.Skip(t)
	t.Parallel()
	words := []string{
		"+build", "//go:build", "go:build", "//", "ignore", "x", "\f", "\v", " ", " ", "  ", "\t",
		string(rune(0xA0)), string(rune(0x85)), string(rune(0x2028)),
		"#", "# ", "- ", "1. ", "*", "[x]", "[x]: ", "http://a.b", ":", ".", "A", "//line ", "//export ", "go:generate", `\`,
	}
	rng := rand.New(rand.NewPCG(20261013, 2)) //nolint:gosec // a seeded generator makes the draws repeatable
	content := func(indent string) string {
		var b strings.Builder
		b.WriteString(" ")
		for line := range 1 + rng.IntN(5) {
			if line > 0 {
				b.WriteString("\n" + []string{indent, indent, indent + " ", indent + "\t", ""}[rng.IntN(5)])
			}
			for range rng.IntN(5) {
				b.WriteString(words[rng.IntN(len(words))])
			}
		}
		return b.String() + " "
	}
	loaded := 0
	for i := range 4000 {
		src := documentedCar(content("   "), content("\t   "))
		s, res := schema.LoadString(context.Background(), src, "gen.yammm")
		if res.HasErrors() {
			continue
		}
		loaded++
		generated, err := gogen.Marshal(s)
		if err != nil {
			t.Fatalf("draw %d: Marshal: %v\n%q", i, err, src)
		}
		formatted, err := format.Source(generated)
		if err != nil {
			t.Fatalf("draw %d: gofmt: %v", i, err)
		}
		if string(formatted) != string(generated) {
			t.Errorf("draw %d: gofmt rewrites the generated file for %q", i, src)
		}
	}
	if loaded < 3000 {
		t.Errorf("%d of 4000 draws loaded; the draws no longer reach the generator", loaded)
	}
}

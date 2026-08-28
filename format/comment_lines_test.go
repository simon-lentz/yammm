package format

import (
	"strings"
	"testing"
)

// The formatter decides line structure once, in classifyText, and every
// text pass reads that decision. These tests pin the defects that existed
// while each pass re-derived it: a doc-comment continuation line shaped
// like a property joined an alignment group, set its column width, and a
// long comment line could be wrapped as if it were a declaration.

// The consumer's reproduction: the second comment line is padded to the
// member column, and `fmt --check` then calls the result formatted.
const docCommentContinuation = `schema "t"

type T {
	/* Census GEOID: the state FIPS code followed by the county FIPS code. "06037"
	   is Los Angeles County, California. */
	geoid String primary
}
`

func TestTokenStream_DocCommentInteriorIsNotAligned(t *testing.T) {
	t.Parallel()

	got, err := TokenStream(docCommentContinuation)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if got != docCommentContinuation {
		t.Errorf("a doc-comment interior line was rewritten:\n got %q\nwant %q", got, docCommentContinuation)
	}
}

// The second direction: the comment line does not only get padded, it sets
// the width for the real members around it. Here the comment's first word
// is longer than any member name, so the members must align to each other
// and not to it.
func TestTokenStream_DocCommentDoesNotSetTheColumnWidth(t *testing.T) {
	t.Parallel()

	input := `schema "t"

type T {
	id String primary
	/* A note on the code. The value
	   identifies Something specific. */
	code String
}
`
	want := `schema "t"

type T {
	id String primary
	/* A note on the code. The value
	   identifies Something specific. */
	code String
}
`
	got, err := TokenStream(input)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if got != want {
		t.Errorf("members aligned to a comment word:\n got %q\nwant %q", got, want)
	}
}

// A comment line longer than the threshold is prose, whatever it contains;
// the wrapper must not read an Enum out of it.
func TestTokenStream_LongCommentLineIsNotWrapped(t *testing.T) {
	t.Parallel()

	comment := "\t// state Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\"] was the old shape\n"
	if DisplayWidth(strings.TrimSuffix(comment, "\n")) <= LineWidthThreshold {
		t.Fatal("fixture comment must exceed the threshold")
	}
	input := "schema \"t\"\n\ntype T {\n" + comment + "\tid String primary\n}\n"

	got, err := TokenStream(input)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if got != input {
		t.Errorf("a long comment line was wrapped:\n got %q\nwant %q", got, input)
	}
}

// A comment inside a multiline Enum is not a value. The construct passes
// through unchanged rather than collapsing with the comment folded in.
func TestTokenStream_CommentInsideMultilineEnumIsKept(t *testing.T) {
	t.Parallel()

	input := `schema "t"

type T {
	id String primary
	state Enum[
		"a",
		// b is retired, see [1
		"c",
	]
}
`
	got, err := TokenStream(input)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if got != input {
		t.Errorf("an Enum holding a comment was rewritten:\n got %q\nwant %q", got, input)
	}
}

// A comment inside a multiline extends list is not a type name. The list is
// re-indented, not collapsed with the comment folded into the declaration.
func TestTokenStream_CommentInsideMultilineExtendsIsKept(t *testing.T) {
	t.Parallel()

	input := `schema "t"

abstract type A {
	id String primary
}

abstract type B {
	name String
}

type C extends
	A,
	// B carries the name
	B
{
	code String
}
`
	got, err := TokenStream(input)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if got != input {
		t.Errorf("an extends list holding a comment was rewritten:\n got %q\nwant %q", got, input)
	}
}

func TestClassifyText(t *testing.T) {
	t.Parallel()

	text := "schema \"t\"\n\n// line comment\n/* head\n   interior\n   tail */\n/* one line */\n\tid String // trailing\n"
	want := []lineClass{
		lineContent, // schema "t"
		lineBlank,   //
		lineComment, // // line comment
		lineComment, // /* head
		lineComment, //    interior
		lineComment, //    tail */
		lineComment, // /* one line */
		lineContent, // id String // trailing
		lineBlank,   // the empty tail after the final newline
	}
	got := classifyText(text)
	if len(got) != len(want) {
		t.Fatalf("classifyText returned %d lines, want %d", len(got), len(want))
	}
	for i, ln := range got {
		if ln.class != want[i] {
			t.Errorf("line %d %q: class = %v, want %v", i, ln.text, ln.class, want[i])
		}
	}
	if joined := joinLines(got); joined != text {
		t.Errorf("joinLines(classifyText(text)) != text:\n got %q\nwant %q", joined, text)
	}
}

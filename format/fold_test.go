package format

import "testing"

// TestLine_SkipSpace pins the helper every line offset is taken through: it
// steps over spaces and tabs from off and stops at anything else.
func TestLine_SkipSpace(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		text      string
		off, want int
	}{
		{"x", 0, 0},
		{"  \tx", 0, 3},
		{"a \t b", 1, 4},
		{"   ", 0, 3},
		{"ab", -1, 0},
		{"ab", 5, 5},
	} {
		if got := (line{text: tt.text}).skipSpace(tt.off); got != tt.want {
			t.Errorf("skipSpace(%q, %d) = %d, want %d", tt.text, tt.off, got, tt.want)
		}
	}
}

// TestFoldsLosslessly pins the one predicate both collapses read: a construct
// folds when every line is code and only its closing line carries a comment.
func TestFoldsLosslessly(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, text string
		want       bool
	}{
		{"code only", "\tv Enum[\n\t\t\"a\",\n\t]", true},
		{"a comment on the closing line", "\tv Enum[\n\t\t\"a\",\n\t] // c", true},
		{"a comment on a value line", "\tv Enum[\n\t\t\"a\", // c\n\t]", false},
		{"a comment on the opening line", "\tv Enum[ // c\n\t\t\"a\",\n\t]", false},
		{"a comment line", "\tv Enum[\n\t\t// c\n\t\t\"a\",\n\t]", false},
		{"a blank line", "\tv Enum[\n\t\t\"a\",\n\n\t\t\"b\",\n\t]", false},
	} {
		if got := foldsLosslessly(classifyLexed(tt.text)); got != tt.want {
			t.Errorf("%s: foldsLosslessly = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestTokenStream_KeepsABlankLineInsideAConstruct asserts the output for a
// multiline enum holding a blank line: it is re-indented, never folded.
func TestTokenStream_KeepsABlankLineInsideAConstruct(t *testing.T) {
	t.Parallel()

	const src = "schema \"s\"\n\ntype T {\n\tid String primary\n\tstatus Enum[\n\t\t\"a\",\n\n\t\t\"b\",\n\t] required\n}\n"
	out, err := TokenStream(src)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if out != src {
		t.Errorf("the construct was rewritten:\n got %q\nwant %q", out, src)
	}
}

// TestTokenStream_FoldsAnExtendsListKeepingItsClosingComment asserts the
// output for a multiline extends list whose brace line carries a comment: it
// folds onto one line, and the comment stays at the end.
func TestTokenStream_FoldsAnExtendsListKeepingItsClosingComment(t *testing.T) {
	t.Parallel()

	const head = "schema \"s\"\n\ntype A {\n\tid String primary\n}\n\ntype B {\n\tid String primary\n}\n\n"
	src := head + "type C extends\n\tA,\n\tB { // the body\n\tname String\n}\n"
	want := head + "type C extends A, B { // the body\n\tname String\n}\n"
	out, err := TokenStream(src)
	if err != nil {
		t.Fatalf("TokenStream: %v", err)
	}
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

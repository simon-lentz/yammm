package diag

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/location"
)

// excerptBlock renders one issue spanning span over content, with excerpts on,
// and returns what follows the issue's first line: the excerpt, when one
// renders.
func excerptBlock(content string, span location.Span) string {
	provider := newMockSourceProvider()
	provider.Add(span.Source, content)
	r := NewRenderer(WithSourceProvider(provider), WithExcerpts(true))
	out := formatIssue(r, NewIssue(Error, E_SYNTAX, "msg").WithSpan(span).Build())
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return out[i:]
	}
	return ""
}

// wantExcerpt is the excerpt owed for line num. All three rows share the
// number row's gutter, so the marks sit under the text they mark.
func wantExcerpt(num, text, marks string) string {
	gutter := strings.Repeat(" ", len(num))
	return "\n" + gutter + " |\n" + num + " | " + text + "\n" + gutter + " | " + marks
}

// TestExcerpt_MarksSitUnderTheText holds the excerpt to the text it marks: the
// mark row copies each tab, gives an East Asian wide or fullwidth rune two
// columns, a combining or enclosing mark, a format character other than the
// soft hyphen and a conjoining Hangul jamo none, and one to the glyph shown for
// a control character or invalid UTF-8. Every line a span can start on renders,
// marked no further than its end; a span starting past it renders no excerpt.
func TestExcerpt_MarksSitUnderTheText(t *testing.T) {
	t.Parallel()

	src := location.MustNewSourceID("test://excerpt.yammm")
	point := func(line, col int) location.Span { return location.Point(src, line, col) }
	span := func(startLine, startCol, endLine, endCol int) location.Span {
		return location.Span{
			Source: src,
			Start:  location.Position{Line: startLine, Column: startCol},
			End:    location.Position{Line: endLine, Column: endCol},
		}
	}

	rows := []struct {
		name    string
		content string
		span    location.Span
		want    string
	}{
		{"a range under plain text", "type User {\n", span(1, 6, 1, 10), wantExcerpt("1", "type User {", "     ^^^^")},
		{"a point is one caret", "  token here\n", point(1, 3), wantExcerpt("1", "  token here", "  ^")},
		{"a tab is copied into the mark row", "\t\tname String\n", span(1, 3, 1, 7), wantExcerpt("1", "\t\tname String", "\t\t^^^^")},
		{"a tab inside the span is copied between the carets", "a\tb x\n", span(1, 1, 1, 4), wantExcerpt("1", "a\tb x", "^\t^")},
		{"a point on a tab is one caret", "a\tb x\n", point(1, 2), wantExcerpt("1", "a\tb x", " ^")},
		{"a span over only tabs is one caret", "a\t\tb x\n", span(1, 2, 1, 4), wantExcerpt("1", "a\t\tb x", " ^")},
		{"a span over only a combining mark is one caret", "é x\n", span(1, 2, 1, 3), wantExcerpt("1", "é x", " ^")},
		{"a wide rune before the span takes two columns", "a \u6f22\u5b57 b\n", point(1, 6), wantExcerpt("1", "a \u6f22\u5b57 b", "       ^")},
		{"a wide rune under the span takes two carets", "a \u6f22\u5b57 b\n", span(1, 3, 1, 5), wantExcerpt("1", "a \u6f22\u5b57 b", "  ^^^^")},
		{"a fullwidth rune before the span takes two columns", "\uff21 x\n", point(1, 3), wantExcerpt("1", "\uff21 x", "   ^")},
		{"a combining mark takes no column", "cafe\u0301 x\n", point(1, 7), wantExcerpt("1", "cafe\u0301 x", "     ^")},
		{"an enclosing mark takes no column", "o\u20dd x\n", point(1, 4), wantExcerpt("1", "o\u20dd x", "  ^")},
		{"a zero-width joiner takes no column", "\U0001f468\u200d\U0001f469 x\n", point(1, 5), wantExcerpt("1", "\U0001f468\u200d\U0001f469 x", "     ^")},
		{"a soft hyphen takes one column", "a\u00adb x\n", point(1, 5), wantExcerpt("1", "a\u00adb x", "    ^")},
		{"a conjoining Hangul vowel and final consonant take no column", "\u1100\u1161\u11a8 x\n", point(1, 5), wantExcerpt("1", "\u1100\u1161\u11a8 x", "   ^")},
		{"an extended Hangul vowel and final consonant take no column", "\u1100\ud7b0\ud7cb x\n", point(1, 5), wantExcerpt("1", "\u1100\ud7b0\ud7cb x", "   ^")},
		{"a C0 control shows as its control picture", "a\x1bb x\n", point(1, 5), wantExcerpt("1", "a\u241bb x", "    ^")},
		{"DEL shows as its control picture", "a\x7fb x\n", point(1, 5), wantExcerpt("1", "a\u2421b x", "    ^")},
		{"a C1 control shows as the replacement character", "a\u009bb x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"a bidirectional override shows as the replacement character", "a\u202eb x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"the first bidirectional isolate shows as the replacement character", "a\u2066b x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"the last bidirectional isolate shows as the replacement character", "a\u2069b x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"the Arabic letter mark shows as the replacement character", "a\u061cb x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"an invalid UTF-8 byte shows as the replacement character", "a\xffb x\n", point(1, 5), wantExcerpt("1", "a\ufffdb x", "    ^")},
		{"the column one past the line's end takes a caret", "type A {\n", point(1, 9), wantExcerpt("1", "type A {", "        ^")},
		{"a span ending past its line's end is marked to the line's end", "type A {\n", span(1, 6, 1, 20), wantExcerpt("1", "type A {", "     ^^^")},
		{"a span starting past the column after its line's end renders no excerpt", "type A {\n", point(1, 20), ""},
		{"a span onto a later line is marked to the end of its first line", "type A {\n\tid String\n}\n", span(1, 6, 2, 4), wantExcerpt("1", "type A {", "     ^^^")},
		{"a span onto a later line, ending past its start column, is marked the same", "type A {\n\tid String\n}\n", span(1, 6, 2, 20), wantExcerpt("1", "type A {", "     ^^^")},
		{"a blank line renders", "a\n\nb\n", point(2, 1), wantExcerpt("2", "", "^")},
		{"the line after a final newline renders", "type A {\n", point(2, 1), wantExcerpt("2", "", "^")},
		{"a CRLF line renders without its line ending", "a\r\nbc x\r\n", point(2, 4), wantExcerpt("2", "bc x", "   ^")},
		{"a two-digit line number widens every gutter row", strings.Repeat("\n", 9) + "xyz\n", span(10, 1, 10, 4), wantExcerpt("10", "xyz", "^^^")},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := excerptBlock(row.content, row.span); got != row.want {
				t.Errorf("excerpt\n got %q\nwant %q", got, row.want)
			}
		})
	}
}

// TestExcerpt_LongLineShowsTheSpan holds a line over 120 runes to a window
// that holds the span's start: each caret sits under a rune it marks, never
// inside an ellipsis, the window shows at most 120 runes of the line, and each
// end the window cuts is marked with "...".
func TestExcerpt_LongLineShowsTheSpan(t *testing.T) {
	t.Parallel()

	src := location.MustNewSourceID("test://long.yammm")
	rows := []struct {
		name                string
		length, col, marked int
	}{
		{"a span near the start of a long line", 200, 10, 1},
		{"a span past column 120", 200, 150, 1},
		{"a span in the middle of a very long line", 400, 200, 1},
		{"a span on the last rune of a long line", 200, 200, 1},
		{"a range running past the window's right edge", 400, 150, 100},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			line := strings.Repeat("a", row.col-1) + strings.Repeat("X", row.marked) + strings.Repeat("b", row.length-row.col-row.marked+1)
			span := location.Point(src, 1, row.col)
			if row.marked > 1 {
				span = location.Range(src, 1, row.col, 1, row.col+row.marked)
			}
			if ok, detail := windowMarksTheSpan(line, excerptBlock(line+"\n", span)); !ok {
				t.Error(detail)
			}
		})
	}
}

// windowMarksTheSpan reports whether block is a line-1 excerpt of line with
// one caret under each "X" the window shows, and no caret elsewhere.
func windowMarksTheSpan(line, block string) (bool, string) {
	rows := strings.Split(block, "\n")
	if len(rows) != 4 {
		return false, fmt.Sprintf("no three-row excerpt: %q", block)
	}
	text, okText := strings.CutPrefix(rows[2], "1 | ")
	marks, okMarks := strings.CutPrefix(rows[3], "  | ")
	if !okText || !okMarks {
		return false, fmt.Sprintf("the rows do not share the gutter %q: %q", "1 | ", block)
	}
	if got, want := strings.Count(marks, "^"), strings.Count(text, "X"); got != want {
		return false, fmt.Sprintf("want %d carets, one per X shown, the mark row is %q", want, marks)
	}
	for caret, c := range []byte(marks) {
		if c == '^' && (caret >= len(text) || text[caret] != 'X') {
			return false, fmt.Sprintf("the caret at column %d is not under X in %q", caret+1, text)
		}
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(text, "..."), "...")
	if n := utf8.RuneCountInString(inner); n > 120 {
		return false, fmt.Sprintf("the window shows %d runes of the line", n)
	}
	if !strings.Contains(line, inner) {
		return false, fmt.Sprintf("the window %q is not a run of the line", inner)
	}
	cutStart, cutEnd := !strings.HasPrefix(line, inner), !strings.HasSuffix(line, inner)
	if cutStart != strings.HasPrefix(text, "...") || cutEnd != strings.HasSuffix(text, "...") {
		return false, fmt.Sprintf("a cut end is unmarked or an uncut end is marked: %q", text)
	}
	return true, ""
}

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
// mark row copies each tab, gives an East Asian wide rune two columns and a
// combining mark none, and every line a span can start on renders.
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

	const shifted = "the gutter rows are two columns wider than the number row, so every mark sits two columns right (B1)"
	knownBroken := map[string]string{
		"a range under plain text":                                                   shifted,
		"a point is one caret":                                                       shifted,
		"a tab is copied into the mark row":                                          "the mark row writes a space for each tab (B9); " + shifted,
		"a wide rune before the span takes two columns":                              "the mark row counts runes, not columns; " + shifted,
		"a wide rune under the span takes two carets":                                "the marks count runes, not columns; " + shifted,
		"a combining mark takes no column":                                           "the mark row counts runes, not columns; " + shifted,
		"the column one past the line's end takes a caret":                           "a start past the last rune writes an empty mark row (B10)",
		"a span onto a later line is marked to the end of its first line":            "an end column at or before the start column marks one rune (B12)",
		"a span onto a later line, ending past its start column, is marked the same": shifted,
		"a blank line renders":                                                       "an empty line renders no excerpt (B13)",
		"the line after a final newline renders":                                     "an empty line renders no excerpt (B13)",
		"a CRLF line renders without its line ending":                                shifted,
		"a two-digit line number widens every gutter row":                            shifted,
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
		{"a wide rune before the span takes two columns", "a \u6f22\u5b57 b\n", point(1, 6), wantExcerpt("1", "a \u6f22\u5b57 b", "       ^")},
		{"a wide rune under the span takes two carets", "a \u6f22\u5b57 b\n", span(1, 3, 1, 5), wantExcerpt("1", "a \u6f22\u5b57 b", "  ^^^^")},
		{"a combining mark takes no column", "cafe\u0301 x\n", point(1, 7), wantExcerpt("1", "cafe\u0301 x", "     ^")},
		{"the column one past the line's end takes a caret", "type A {\n", point(1, 9), wantExcerpt("1", "type A {", "        ^")},
		{"a span onto a later line is marked to the end of its first line", "type A {\n\tid String\n}\n", span(1, 6, 2, 4), wantExcerpt("1", "type A {", "     ^^^")},
		{"a span onto a later line, ending past its start column, is marked the same", "type A {\n\tid String\n}\n", span(1, 6, 2, 20), wantExcerpt("1", "type A {", "     ^^^")},
		{"a blank line renders", "a\n\nb\n", point(2, 1), wantExcerpt("2", "", "^")},
		{"the line after a final newline renders", "type A {\n", point(2, 1), wantExcerpt("2", "", "^")},
		{"a CRLF line renders without its line ending", "a\r\nbc x\r\n", point(2, 4), wantExcerpt("2", "bc x", "   ^")},
		{"a two-digit line number widens every gutter row", strings.Repeat("\n", 9) + "xyz\n", span(10, 1, 10, 4), wantExcerpt("10", "xyz", "^^^")},
	}

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got := excerptBlock(row.content, row.span)
			reason, broken := knownBroken[row.name]
			switch ok := got == row.want; {
			case broken && ok:
				t.Errorf("listed as broken (%s) and now passes: remove its knownBroken entry", reason)
			case broken:
				t.Logf("known broken: %s\n got %q\nwant %q", reason, got, row.want)
			case !ok:
				t.Errorf("excerpt\n got %q\nwant %q", got, row.want)
			}
		})
	}
	for name := range knownBroken {
		if !names[name] {
			t.Errorf("knownBroken names no row: %q", name)
		}
	}
}

// TestExcerpt_LongLineShowsTheSpan holds a line over 120 runes to a window
// that holds the span's start: the caret sits under the rune it marks, never
// inside an ellipsis, the window shows at most 120 runes of the line, and each
// end the window cuts is marked with "...".
func TestExcerpt_LongLineShowsTheSpan(t *testing.T) {
	t.Parallel()

	src := location.MustNewSourceID("test://long.yammm")
	const shifted = "the gutter rows are two columns wider than the number row (B1)"
	knownBroken := map[string]string{
		"a span near the start of a long line":     shifted,
		"a span past column 120":                   "a start past the cut writes an empty mark row (B11, P-G4); " + shifted,
		"a span in the middle of a very long line": "a start past the cut writes an empty mark row (B11, P-G4); " + shifted,
		"a span on the last rune of a long line":   "a start past the cut writes an empty mark row (B11, P-G4); " + shifted,
	}

	rows := []struct {
		name        string
		length, col int
	}{
		{"a span near the start of a long line", 200, 10},
		{"a span past column 120", 200, 150},
		{"a span in the middle of a very long line", 400, 200},
		{"a span on the last rune of a long line", 200, 200},
	}

	names := make(map[string]bool, len(rows))
	for _, row := range rows {
		names[row.name] = true
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			line := strings.Repeat("a", row.col-1) + "X" + strings.Repeat("b", row.length-row.col)
			ok, detail := windowMarksTheSpan(line, excerptBlock(line+"\n", location.Point(src, 1, row.col)))
			reason, broken := knownBroken[row.name]
			switch {
			case broken && ok:
				t.Errorf("listed as broken (%s) and now passes: remove its knownBroken entry", reason)
			case broken:
				t.Logf("known broken: %s\n%s", reason, detail)
			case !ok:
				t.Error(detail)
			}
		})
	}
	for name := range knownBroken {
		if !names[name] {
			t.Errorf("knownBroken names no row: %q", name)
		}
	}
}

// windowMarksTheSpan reports whether block is a line-1 excerpt of line whose
// one caret sits under the line's only "X".
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
	if strings.Count(marks, "^") != 1 {
		return false, fmt.Sprintf("want one caret, the mark row is %q", marks)
	}
	if caret := strings.IndexByte(marks, '^'); caret >= len(text) || text[caret] != 'X' {
		return false, fmt.Sprintf("the caret at column %d is not under X in %q", caret+1, text)
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

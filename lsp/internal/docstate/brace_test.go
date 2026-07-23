package docstate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBraceScanner_ScanLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		line           string
		maxCol         int
		initialDepth   int
		initialComment bool
		wantDepth      int
		wantComment    bool
	}{
		{"empty line", "", 0, 0, false, 0, false},
		{"open brace", "type Foo {", 10, 0, false, 1, false},
		{"close brace", "}", 1, 1, false, 0, false},
		{"brace in string", `name "hello { world"`, 20, 0, false, 0, false},
		{"line comment hides brace", "// {", 4, 0, false, 0, false},
		{"block comment start", "/* { */", 7, 0, false, 0, false},
		{"block comment continuation", "still in comment */", 19, 0, true, 0, false},
		{"nested braces", "{ { } }", 7, 0, false, 0, false},
		{"maxCol truncation", "{ }", 2, 0, false, 1, false},
		{"brace in single-quoted string", "'{'", 3, 0, false, 0, false},
		{"escaped quote in string", `"hello \" {"`, 12, 0, false, 0, false},
		{"url in string no false comment", `name "http://example"`, 21, 0, false, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := BraceScanner{Depth: tt.initialDepth, InBlockComment: tt.initialComment}
			depth := bs.ScanLine(tt.line, tt.maxCol)
			assert.Equal(t, tt.wantDepth, depth, "depth")
			assert.Equal(t, tt.wantComment, bs.InBlockComment, "inBlockComment")
		})
	}
}

func TestComputeBraceDepths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		text        string
		wantDepths  []int
		wantComment []bool
	}{
		{
			name:        "simple type",
			text:        "type Foo {\n\tname String\n}",
			wantDepths:  []int{1, 1, 0},
			wantComment: []bool{false, false, false},
		},
		{
			name:        "multiline block comment",
			text:        "/* start\nstill comment\n*/ type Foo {",
			wantDepths:  []int{0, 0, 1},
			wantComment: []bool{true, true, false},
		},
		{
			name:        "CRLF normalized",
			text:        "type Foo {\r\n}",
			wantDepths:  []int{1, 0},
			wantComment: []bool{false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			depths, inComment := ComputeBraceDepths(tt.text)
			assert.Equal(t, tt.wantDepths, depths, "depths")
			assert.Equal(t, tt.wantComment, inComment, "inComment")
		})
	}
}

func TestInStringOrComment(t *testing.T) {
	t.Parallel()

	// pos is the byte index of the '@' in each line; the annotation detectors
	// classify that content byte.
	tests := []struct {
		name       string
		line       string
		startBlock bool
		want       bool
	}{
		{"bare annotation", "\tstate String @index", false, false},
		{"inside double-quoted string", `email Pattern["user@index.com"]`, false, true},
		{"inside single-quoted string", `x Pattern[' @tag']`, false, true},
		{"inside same-line block comment", "/* see @vector */", false, true},
		{"inside line comment", "// see @index here", false, true},
		{"continued block comment", "@vector still commented */", true, true},
		{"after closed block comment", "/* c */ state @index", false, false},
		{"after closed string", `x Pattern["y"] @index`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := -1
			for i := range len(tt.line) {
				if tt.line[i] == '@' {
					pos = i
					break
				}
			}
			if pos < 0 {
				t.Fatalf("test line %q has no '@'", tt.line)
			}
			assert.Equal(t, tt.want, InStringOrComment(tt.line, pos, tt.startBlock))
		})
	}
}

// LineStartsInBlockComment answers for the START of a line, so a comment opened
// on an earlier line is visible to a scanner that can only see the current one.
func TestLineStartsInBlockComment(t *testing.T) {
	t.Parallel()

	text := "schema \"m\"\n/* opened here\n   still inside\n*/ closed\ncode"
	depths, inComment := ComputeBraceDepths(text)
	doc := &Snapshot{
		Text:      text,
		Version:   3,
		LineState: &LineState{Version: 3, BraceDepth: depths, InBlockComment: inComment},
	}

	tests := []struct {
		line int
		want bool
	}{
		{0, false}, // schema line
		{1, false}, // the line that OPENS the comment starts outside it
		{2, true},  // continuation
		{3, true},  // the line that closes it still starts inside
		{4, false}, // after the close
	}
	for _, tt := range tests {
		if got := doc.LineStartsInBlockComment(tt.line); got != tt.want {
			t.Errorf("line %d: got %v, want %v", tt.line, got, tt.want)
		}
	}

	// A snapshot with no line state, or state computed for an older version,
	// answers false rather than reading stale data.
	if (&Snapshot{Text: text, Version: 3}).LineStartsInBlockComment(2) {
		t.Error("a snapshot without line state should answer false")
	}
	stale := &Snapshot{Text: text, Version: 4, LineState: &LineState{Version: 3, InBlockComment: inComment}}
	if stale.LineStartsInBlockComment(2) {
		t.Error("line state from an older document version must not be trusted")
	}
}

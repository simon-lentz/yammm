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

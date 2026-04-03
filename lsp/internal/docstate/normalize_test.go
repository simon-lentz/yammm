package docstate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
)

func TestNormalizeLineEndings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"LF only", "line1\nline2\nline3", "line1\nline2\nline3"},
		{"CRLF only", "line1\r\nline2\r\nline3", "line1\nline2\nline3"},
		{"CR only", "line1\rline2\rline3", "line1\nline2\nline3"},
		{"mixed CRLF and LF", "line1\r\nline2\nline3\r\n", "line1\nline2\nline3\n"},
		{"mixed all types", "line1\r\nline2\rline3\nline4", "line1\nline2\nline3\nline4"},
		{"empty", "", ""},
		{"no newlines", "single line", "single line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, docstate.NormalizeLineEndings(tt.input))
		})
	}
}

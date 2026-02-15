package textlit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertString(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		out     string
		wantErr bool
	}{
		{name: "plain double", in: `"plain"`, out: "plain"},
		{name: "plain single", in: `'plain'`, out: "plain"},
		{name: "escaped newline", in: `"with\nnewline"`, out: "with\nnewline"},
		{name: "escaped tab", in: `"tab\tend"`, out: "tab\tend"},
		{name: "escaped quote", in: `"quote\"inner"`, out: `quote"inner`},
		{name: "escaped backslash", in: `"backslash\\inner"`, out: `backslash\inner`},
		{name: "unicode escape", in: `"\u0041"`, out: "A"},
		{name: "mixed escapes", in: `"mixed\"quote\n"`, out: "mixed\"quote\n"},
		{name: "unquoted", in: `unquoted`, out: "unquoted"},
		{name: "invalid escape", in: `"bad\q"`, out: `"bad\q"`, wantErr: true},
		{name: "unterminated", in: `"unterminated`, out: `"unterminated`, wantErr: true},
		{name: "empty double quoted", in: `""`, out: ""},
		{name: "empty single quoted", in: `''`, out: ""},
		{name: "single char double", in: `"a"`, out: "a"},
		{name: "single char single", in: `'a'`, out: "a"},
		// A2: Single-quoted strings with escape sequences
		{name: "single quote with escape newline", in: `'\n'`, out: "\n"},
		{name: "single quote with escape tab", in: `'\t'`, out: "\t"},
		{name: "single quote with escape backslash", in: `'\\'`, out: "\\"},
		{name: "single quote with unicode", in: `'\u0041'`, out: "A"},
		// A3: Boundary case - single quote char only (len==1)
		{name: "single quote char only", in: `'`, out: `'`},
		// Embedded double quotes in single-quoted strings
		{name: "single quote with embedded double quote", in: `'He said "hi"'`, out: `He said "hi"`},
		{name: "single quote with multiple double quotes", in: `'"quoted" text "here"'`, out: `"quoted" text "here"`},
		{name: "single quote double quote only", in: `'"'`, out: `"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := ConvertString(tt.in)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.out, out)
		})
	}
}

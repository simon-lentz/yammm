package format_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/schema"
)

// sourceTextAnswer is one row of testdata/source_text_v0210.tsv: a source and
// what format.TokenStream of the v0.21.0 release answered for it.
type sourceTextAnswer struct {
	name, input, output string
	formats             bool
}

func readSourceTextAnswers(t *testing.T) []sourceTextAnswer {
	t.Helper()
	data, err := os.ReadFile("testdata/source_text_v0210.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var rows []sourceTextAnswer
	for line := range strings.Lines(string(data)) {
		cells := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(cells) != 4 {
			t.Fatalf("row %q has %d cells, want 4", line, len(cells))
		}
		input, err := strconv.Unquote(cells[1])
		if err != nil {
			t.Fatalf("row %q: input: %v", cells[0], err)
		}
		output, err := strconv.Unquote(cells[3])
		if err != nil {
			t.Fatalf("row %q: output: %v", cells[0], err)
		}
		rows = append(rows, sourceTextAnswer{cells[0], input, output, cells[2] == "formats"})
	}
	if len(rows) < 100 {
		t.Fatalf("read %d rows, want the whole table", len(rows))
	}
	return rows
}

// TestTokenStream_FormatsWhatTheGrammarMatches pins the formatter's answer for
// a source holding text the source rules refuse, an escape whose value is not
// UTF-8, an escape the language does not have, or a token the grammar does not
// match, at each site the table writes one. Each answer is the v0.21.0 release's:
// the formatter refuses a source the grammar does not match and formats every
// other, whether or not the source loads.
func TestTokenStream_FormatsWhatTheGrammarMatches(t *testing.T) {
	t.Parallel()
	for _, row := range readSourceTextAnswers(t) {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got, err := format.TokenStream(row.input)
			if !row.formats {
				if _, refused := errors.AsType[*format.SyntaxError](err); !refused {
					t.Fatalf("TokenStream = %q, %v; want a *SyntaxError", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("TokenStream refused a source it formatted: %v", err)
			}
			if got != row.output {
				t.Errorf("TokenStream = %q, want %q", got, row.output)
			}
			again, err := format.TokenStream(got)
			if err != nil || again != got {
				t.Errorf("formatting the output again = %q, %v; want it unchanged", again, err)
			}
		})
	}
}

// TestTokenStream_AFormattedSourceTextFaultStillFailsToLoad pins that the
// formatter's answer says nothing about the load: every row holding a byte
// that is not UTF-8, a NUL, or a byte order mark past the start is refused by
// schema.LoadString with E_SYNTAX, formatted or not.
func TestTokenStream_AFormattedSourceTextFaultStillFailsToLoad(t *testing.T) {
	t.Parallel()
	judged := 0
	for _, row := range readSourceTextAnswers(t) {
		raw := []byte(row.input)
		refusedText := !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 || bytes.Contains(raw[1:], []byte("\uFEFF"))
		if !refusedText {
			continue
		}
		judged++
		for _, text := range []string{row.input, row.output} {
			if text == "" {
				continue
			}
			_, result := schema.LoadString(context.Background(), text, "s.yammm")
			if !result.HasCode(diag.E_SYNTAX) {
				t.Errorf("%s: LoadString(%q) reports no E_SYNTAX: %v", row.name, text, result)
			}
		}
	}
	if judged < 85 {
		t.Errorf("judged %d rows, want every row holding refused text", judged)
	}
}

package format

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestLexicalRecordAgreesWithLexer is the second implementation of the record.
// Phase 1 builds it from the tokens it is already emitting; classifyLexed
// builds it by lexing the finished text. The two derive the same facts by
// different routes, so a disagreement is a defect in one of them and neither
// can drift alone.
func TestLexicalRecordAgreesWithLexer(t *testing.T) {
	t.Parallel()

	fixtures := agreementCorpus(t)
	if len(fixtures) < 30 {
		t.Fatalf("agreement corpus is %d fixtures; the walk is not reaching testdata", len(fixtures))
	}

	for _, path := range fixtures {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			emitted, _, err := lexicalLines(string(src))
			if err != nil {
				t.Skipf("fixture does not parse: %v", err)
			}
			independent := classifyLexed(joinLines(emitted))

			if len(emitted) != len(independent) {
				t.Fatalf("phase 1 produced %d lines, the lexer sees %d", len(emitted), len(independent))
			}
			for i := range emitted {
				compareRecords(t, i, emitted[i], independent[i])
			}
		})
	}
}

// TestPhaseRecordsAgreeWithLexer extends the agreement to the lines phases 3
// and 4 read. Each source is formatted twice, because a second pass over
// formatted text is where phase 3 rebuilds a construct without changing it.
func TestPhaseRecordsAgreeWithLexer(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, path := range agreementCorpus(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		ls, _, err := lexicalLines(string(src))
		if err != nil {
			continue
		}
		for pass, text := range []string{string(src), rewrite(ls)} {
			checked++
			t.Run(path+"#pass"+strconv.Itoa(pass+1), func(t *testing.T) {
				t.Parallel()
				lines, _, err := lexicalLines(text)
				if err != nil {
					t.Fatalf("formatted output does not parse: %v", err)
				}
				collapsed := collapseBlankLines(lines)
				agreeWithLexer(t, "phase 3 reads", collapsed)
				agreeWithLexer(t, "phase 4 reads", recordsAfterWrap(collapsed, wrapLongLines(collapsed)))
			})
		}
	}
	if checked < 60 {
		t.Fatalf("checked %d sources; the corpus walk is not reaching testdata", checked)
	}
}

// agreeWithLexer reports every line of ls whose record differs from a lex of
// the lines' own text.
func agreeWithLexer(t *testing.T, boundary string, ls []line) {
	t.Helper()
	independent := classifyLexed(joinLines(ls))
	if len(ls) != len(independent) {
		t.Fatalf("%s %d lines, the lexer sees %d", boundary, len(ls), len(independent))
	}
	for i := range ls {
		if ls[i].class != independent[i].class || ls[i].lex.commentAt != independent[i].lex.commentAt {
			t.Errorf("%s line %d %q: class %v commentAt %d, lexer says class %v commentAt %d",
				boundary, i, ls[i].text, ls[i].class, ls[i].lex.commentAt, independent[i].class, independent[i].lex.commentAt)
		}
	}
}

// compareRecords reports every way two views of one line disagree.
func compareRecords(t *testing.T, i int, emitted, independent line) {
	t.Helper()
	if emitted.text != independent.text {
		t.Errorf("line %d: text %q vs %q", i, emitted.text, independent.text)
		return
	}
	if emitted.class != independent.class {
		t.Errorf("line %d %q: class %v, lexer says %v", i, emitted.text, emitted.class, independent.class)
	}
	if emitted.lex.commentAt != independent.lex.commentAt {
		t.Errorf("line %d %q: commentAt %d, lexer says %d",
			i, emitted.text, emitted.lex.commentAt, independent.lex.commentAt)
	}
	if len(emitted.lex.literals) != len(independent.lex.literals) {
		t.Errorf("line %d %q: %d literals, lexer says %d",
			i, emitted.text, len(emitted.lex.literals), len(independent.lex.literals))
		return
	}
	for j, lit := range emitted.lex.literals {
		if lit != independent.lex.literals[j] {
			t.Errorf("line %d %q: literal %d is %v, lexer says %v",
				i, emitted.text, j, lit, independent.lex.literals[j])
		}
	}
}

// agreementCorpus returns every .yammm under testdata, which is the corpus the
// record must hold over.
func agreementCorpus(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".yammm") {
			found = append(found, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking testdata: %v", err)
	}
	return found
}

package format

import (
	"os"
	"path/filepath"
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

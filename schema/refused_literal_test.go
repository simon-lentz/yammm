package schema_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/schema"
)

// The three diagnostics the source rules give text inside a comment or a
// literal.
const (
	invalidUTF8 = "E_SYNTAX invalid UTF-8 encoding"
	strayMark   = "E_SYNTAX byte order mark (U+FEFF) past the start of the source"
	strayNUL    = `E_SYNTAX NUL character (U+0000) in the source; a string writes it as \0`
)

func typeBody(members ...string) string {
	return "schema \"s\"\ntype T {\n\tid String primary\n\t" + strings.Join(members, "\n\t") + "\n}\n"
}

// loadIssues loads source as main.yammm in a new directory and returns every
// issue as "CODE message", in the result's order, with the directory written
// as <dir> in either form a message gives it: its host path, or its identity,
// which is NFC and '/'-separated. The directory's name holds a decomposed
// character, so the two forms differ on every host.
func loadIssues(t *testing.T, source string, siblings map[string]string) []string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cafe\u0301")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	dir = canonicalPath(t, dir)
	for name, text := range siblings {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	_, result := schema.Load(context.Background(), path)
	placeholder := strings.NewReplacer(rootIdentity(t, dir), "<dir>", dir, "<dir>")
	var issues []string
	for iss := range result.Issues() {
		issues = append(issues, placeholder.Replace(iss.Code().String()+" "+iss.Message()))
	}
	return issues
}

// refusedRuns counts the runs of text the source rules refuse in source, a byte
// order mark at its first byte aside: adjacent bytes that are not valid UTF-8,
// adjacent NULs and adjacent byte order marks are one run each.
func refusedRuns(source string) int {
	const none, invalid, nul, mark = 0, 1, 2, 3
	runs, last := 0, none
	for i := 0; i < len(source); {
		r, size := utf8.DecodeRuneInString(source[i:])
		kind := none
		switch {
		case r == utf8.RuneError && size == 1:
			kind = invalid
		case r == 0:
			kind = nul
		case r == '\uFEFF' && i > 0:
			kind = mark
		}
		if kind != none && kind != last {
			runs++
		}
		last = kind
		i += size
	}
	return runs
}

// TestLoad_OneBreakInALiteralIsOneIssue pins what a load reports for a literal
// that has no value: the source rules' diagnostic at each run of refused text,
// a diagnostic for an escape that breaks the value on its own, and whatever
// stands when the literal is counted as written. Nothing is said about a value
// the source did not write: no duplicate, no shortfall, no lossy layout, no
// file that was not found, and no unknown alias.
func TestLoad_OneBreakInALiteralIsOneIssue(t *testing.T) {
	t.Parallel()
	const (
		badValue   = "E_SYNTAX invalid enum value: unquote string: the value its escapes write is not valid UTF-8"
		badPath    = "E_SYNTAX invalid import path: unquote string: the value its escapes write is not valid UTF-8"
		badMessage = "E_SYNTAX invalid invariant message: unquote string: the value its escapes write is not valid UTF-8"
		badFormat  = "E_SYNTAX invalid timestamp format: unquote string: the value its escapes write is not valid UTF-8"
		badString  = "E_INVALID_INVARIANT invalid string literal: the value its escapes write is not valid UTF-8"
		oneValue   = "E_INVALID_CONSTRAINT enum must have at least two values (got 1)"
		outOfRange = `E_INVALID_INVARIANT invalid integer literal: strconv.ParseInt: parsing "99999999999999999999": value out of range`
		threeGiven = "E_INVALID_CONSTRAINT pattern constraint exceeds maximum of 2 patterns (got 3)"
	)
	rows := []struct {
		name, source string
		// written holds bytes the row's refused literal writes, enough to say
		// whether they are UTF-8, where the row's answer turns on that.
		written string
		want    []string
	}{
		// Text the source rules refuse, inside a literal.
		{"enum: two raw invalid values", typeBody("e Enum[\"\xff\", \"\xfe\"]"), "", []string{invalidUTF8, invalidUTF8}},
		{"enum: one raw invalid value of two", typeBody("e Enum[\"\xff\", \"b\"]"), "", []string{invalidUTF8}},
		{"enum: the same raw NUL value twice", typeBody("e Enum[\"a\x00\", \"a\x00\"]"), "", []string{strayNUL, strayNUL}},
		{"enum: a raw mark value of two", typeBody("e Enum[\"\uFEFF\", \"b\"]"), "", []string{strayMark}},
		{"enum: one raw invalid value alone", typeBody("e Enum[\"\xff\"]"), "", []string{oneValue, invalidUTF8}},
		{"enum: a raw invalid value and a repeat", typeBody("e Enum[\"a\", \"a\", \"\xff\"]"), "", []string{`E_INVALID_CONSTRAINT duplicate enum value "a"`, invalidUTF8}},
		{"enum: an escape and a raw byte, one rune", typeBody("e Enum[\"\\xc3\xa9\", \"b\"]"), "\xc3\xa9", []string{invalidUTF8}},
		{"expression: an escape and a raw byte, one rune", typeBody("! \"m\" id != \"\\xc3\xa9\""), "\xc3\xa9", []string{invalidUTF8}},
		{"expression: a raw byte and an escape, one rune", typeBody("! \"m\" id != \"\xc3\\xa9\""), "\xc3\xa9", []string{invalidUTF8}},
		{"expression: an escape no byte completes, and a raw byte", typeBody("! \"m\" id != \"\\xff\xfe\""), "\xff\xfe", []string{badString, invalidUTF8}},
		{"expression: a raw invalid byte", typeBody("! \"m\" id != \"a\xffb\""), "", []string{invalidUTF8}},
		{"expression: a malformed escape and a raw byte", typeBody("! \"m\" id != \"\\xZZ\xff\""), "", []string{"E_INVALID_INVARIANT invalid string literal: invalid syntax", invalidUTF8}},
		{"expression: a refused literal as an operand of +", typeBody("! \"m\" id + \"a\x00\" == \"x\""), "", []string{strayNUL}},
		{"expression: an invalid byte as an operand of +", typeBody("! \"m\" id + \"a\xff\" == \"x\""), "", []string{invalidUTF8}},
		{"expression: an escape not UTF-8 as an operand of +", typeBody(`! "m" id + "\xff" == "x"`), "\xff", []string{badString}},
		{"enum: a raw lead byte, an escape completing it, and an escape that breaks", typeBody("e Enum[\"\xc3\\xa9\\xff\", \"b\"]"), "\xc3\xa9\xff", []string{badValue, invalidUTF8}},
		{"messages: two raw invalid messages", typeBody("! \"\xff\" id != \"\"", "! \"\xfe\" id == \"\""), "", []string{invalidUTF8, invalidUTF8}},
		{"message: raw invalid, expression literal out of range", typeBody("! \"\xff\" id == 99999999999999999999"), "", []string{invalidUTF8, outOfRange}},
		// An invariant holding a literal with no value keeps its other faults:
		// the literal is judged by the kind the source wrote and by no value,
		// and inherited definitions conflict only when no value would make
		// them all one.
		{"expression: a literal out of range in a duplicate invariant", typeBody("! \"m\" id != \"\"", "! \"m\" id == 99999999999999999999"), "", []string{`E_DUPLICATE_INVARIANT type "T" declares invariant "m" twice`, outOfRange}},
		{"expression: a literal out of range beside an unknown property", typeBody("! \"m\" nosuch < 99999999999999999999"), "", []string{`E_UNKNOWN_PROPERTY unknown property "nosuch" in invariant "m" on type "T"`, outOfRange}},
		{"expression: a literal out of range beside a type fault", typeBody("n Integer", "! \"m\" (n -> Upper) == \"x\" && n < 99999999999999999999"), "", []string{`E_INVALID_INVARIANT Upper takes a string, and its receiver is not one in invariant "m" on type "T"`, outOfRange}},
		{"expression: a literal out of range under an empty message", typeBody("! \"\" id == 99999999999999999999"), "", []string{`E_INVALID_INVARIANT invariant message must not be empty in type "T"`, outOfRange}},
		{"expression: an inherited string with no value against a written nil", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name == \"\xff\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name == nil\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited integer and float, each with no value", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" n == 99999999999999999999\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" n == 1.0e999\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVALID_INVARIANT invalid float literal: strconv.ParseFloat: parsing "1.0e999": value out of range`, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited string with no value against a string", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name == \"\xff\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name == \"x\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"expression: a string with no value as an operand of + beside a number", typeBody("n Integer", "! \"m\" \"\xff\" + n == \"x\""), "", []string{`E_INVALID_INVARIANT + takes two numbers, two strings or two lists in invariant "m" on type "T"`, invalidUTF8}},
		{"expression: an integer with no value indexed", typeBody("! \"m\" 99999999999999999999[0] == 1"), "", []string{`E_INVALID_INVARIANT a number cannot be indexed in invariant "m" on type "T"`, outOfRange}},
		{"expression: a string with no value right of in", typeBody("! \"m\" id in \"\xff\""), "", []string{`E_INVALID_INVARIANT in takes a list on its right in invariant "m" on type "T"`, invalidUTF8}},
		{"expression: an inherited string against a string with no value", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name == \"x\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name == \"\xff\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"expression: an inherited integer with no value in both", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" n == 99999999999999999999\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" n == 99999999999999999999\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{outOfRange, outOfRange}},
		{"expression: an inherited list holding a string with no value against a list", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name in [\"a\", \"\xff\"]\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name in [\"a\", \"b\"]\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"expression: an inherited list holding a string with no value against a shorter list", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name in [\"a\", \"\xff\"]\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name in [\"a\"]\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited integer with no value against a property", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" n == 99999999999999999999\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" n == n\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: a regex with no value as an operand of +", typeBody("! \"m\" id + /(/ == \"x\""), "", []string{`E_INVALID_INVARIANT + takes two numbers, two strings or two lists in invariant "m" on type "T"`, "E_INVALID_INVARIANT invalid regexp literal: error parsing regexp: missing closing ): `(`"}},
		{"expression: an inherited written nil against a string with no value", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" name == nil\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" name == \"\xff\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited integer with no value against a float", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" n == 99999999999999999999\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" n == 1.5\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited call argument with no value against a string argument", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default(\"\xff\")) == \"a\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default(\"x\")) == \"a\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"expression: an inherited call argument with no value against a nil argument", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default(\"\xff\")) == \"a\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default(nil)) == \"a\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited call argument with no value against no argument", "schema \"s\"\nabstract type P {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default(\"\xff\")) == \"a\"\n}\nabstract type Q {\n\tn Integer\n\tname String\n\t! \"m\" (name -> Default()) == \"a\"\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{invalidUTF8, `E_INVALID_INVARIANT Default requires at least 1 argument(s) in invariant "m" on type "Q"`, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: three inherited definitions, the one with no value first", "schema \"s\"\nabstract type S {\n\tx Integer\n\t! \"m\" x == 99999999999999999999\n}\nabstract type A {\n\tx Integer\n\t! \"m\" x == 1\n}\nabstract type C {\n\tx Integer\n\t! \"m\" x == 2\n}\ntype D extends S, A, C {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "D" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: three inherited definitions, the one with no value between", "schema \"s\"\nabstract type S {\n\tx Integer\n\t! \"m\" x == 99999999999999999999\n}\nabstract type A {\n\tx Integer\n\t! \"m\" x == 1\n}\nabstract type C {\n\tx Integer\n\t! \"m\" x == 2\n}\ntype D extends A, S, C {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "D" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited view that gave a literal with no value its value", "schema \"s\"\nabstract type S {\n\tx Integer\n\t! \"m\" x == 99999999999999999999\n}\nabstract type A {\n\tx Integer\n\t! \"m\" x == 1\n}\nabstract type C {\n\tx Integer\n\t! \"m\" x == 2\n}\nabstract type M extends S, A {\n\tx Integer\n}\ntype D extends M, C {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "D" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: three inherited call arguments, the one with no value first", "schema \"s\"\nabstract type S {\n\tx Integer\n\t! \"m\" (x -> Default(99999999999999999999)) == 1\n}\nabstract type A {\n\tx Integer\n\t! \"m\" (x -> Default(1)) == 1\n}\nabstract type C {\n\tx Integer\n\t! \"m\" (x -> Default(2)) == 1\n}\ntype D extends S, A, C {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "D" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"expression: an inherited view whose literal with no value is still open", "schema \"s\"\nabstract type S {\n\tx Integer\n\t! \"m\" x == 99999999999999999999\n}\nabstract type U {\n\tx Integer\n\t! \"m\" x == 99999999999999999999\n}\nabstract type C {\n\tx Integer\n\t! \"m\" x == 2\n}\nabstract type M extends S, U {\n\tx Integer\n}\ntype D extends M, C {\n\tid String primary\n}\n", "", []string{outOfRange, outOfRange}},
		{"expression: a literal out of range in an inherited conflict", "schema \"s\"\nabstract type P {\n\tn Integer\n\t! \"m\" n > 0\n}\nabstract type Q {\n\tn Integer\n\t! \"m\" n < 99999999999999999999\n}\ntype T extends P, Q {\n\tid String primary\n}\n", "", []string{outOfRange, `E_INVARIANT_CONFLICT type "T" inherits definitions of invariant "m" with different expressions (expressions are compared structurally: 1 and 1.0 are different literals)`}},
		{"timestamp: a raw invalid format", typeBody("t Timestamp[\"\xff\"]"), "", []string{invalidUTF8}},
		{"timestamp: a lossy layout holding a NUL", typeBody("t Timestamp[\"2006-01-02\x00\"]"), "", []string{strayNUL}},
		{"pattern: a raw invalid byte", typeBody("p Pattern[\"a\xff\"]"), "", []string{invalidUTF8}},
		{"pattern: three, the last raw invalid and still counted", typeBody("p Pattern[\"a\", \"b\", \"c\xff\"]"), "", []string{threeGiven, invalidUTF8}},
		{"pattern: three, the last holding a NUL", typeBody("p Pattern[\"a\", \"b\", \"c\x00\"]"), "", []string{threeGiven, strayNUL}},
		{"schema name: a raw invalid byte", "schema \"s\xff\"\ntype T {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"import: a raw invalid path with an alias, used", "schema \"s\"\nimport \"./\xff\" as b\ntype T {\n\tid String primary\n\tk b.K\n\t--> REL (one) b.U\n}\n", "", []string{invalidUTF8}},
		{"import: a raw invalid path with no alias", "schema \"s\"\nimport \"./\xff\"\ntype T {\n\tid String primary\n}\n", "", []string{invalidUTF8}},
		{"import: a raw invalid path with no alias, and a reference through n", "schema \"s\"\nimport \"./\xff\"\ntype T {\n\tid String primary\n\tk n.K\n}\n", "", []string{invalidUTF8, `E_UNKNOWN_TYPE unknown type "n.K" in datatype reference: no import declared with alias "n"`}},
		{"import: a raw NUL path with an alias", "schema \"s\"\nimport \"./a\x00\" as b\ntype T {\n\tid String primary\n\tk b.K\n}\n", "", []string{strayNUL}},
		{"import: a raw mark path with an alias", "schema \"s\"\nimport \"./a\uFEFF\" as b\ntype T {\n\tid String primary\n\tk b.K\n}\n", "", []string{strayMark}},
		// An escape whose value is not UTF-8: no text the source rules refuse.
		{"enum: one escape not UTF-8 of two", typeBody(`e Enum["\xff", "b"]`), "\xff", []string{badValue}},
		{"enum: two escapes not UTF-8", typeBody(`e Enum["\xff", "\xfe"]`), "\xff", []string{badValue, badValue}},
		{"enum: one escape not UTF-8 alone", typeBody(`e Enum["\xff"]`), "\xff", []string{oneValue, badValue}},
		{"enum: an escape not UTF-8 and a repeat", typeBody(`e Enum["a", "a", "\xff"]`), "\xff", []string{`E_INVALID_CONSTRAINT duplicate enum value "a"`, badValue}},
		{"datatype: one escape not UTF-8 of two", "schema \"s\"\ntype E = Enum[\"\\xff\", \"b\"]\ntype T {\n\tid String primary\n\te E\n}\n", "\xff", []string{badValue}},
		{"import: an escape not UTF-8 with an alias, used", "schema \"s\"\nimport \"./d\\xff\" as d\ntype T {\n\tid String primary\n\tk d.K\n}\n", "d\xff", []string{badPath}},
		{"import: an escape not UTF-8 with no alias", "schema \"s\"\nimport \"./d\\xff\"\ntype T {\n\tid String primary\n}\n", "d\xff", []string{badPath}},
		{"timestamp: an escape not UTF-8", typeBody(`t Timestamp["\xff"]`), "\xff", []string{badFormat}},
		{"message: an escape not UTF-8, expression literal out of range", typeBody(`! "\xff" id == 99999999999999999999`), "\xff", []string{badMessage}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got := loadIssues(t, row.source, nil)
			if !slices.Equal(got, row.want) {
				t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(row.want, "\n  "))
			}
			// The table's own answers, held to the two facts they rest on.
			atTheBytes := 0
			escapeBreaks := false
			for _, issue := range row.want {
				switch {
				case issue == invalidUTF8 || issue == strayMark || issue == strayNUL:
					atTheBytes++
				case strings.Contains(issue, "the value its escapes write is not valid UTF-8"):
					escapeBreaks = true
				}
			}
			if runs := refusedRuns(row.source); atTheBytes != runs {
				t.Errorf("the row wants %d diagnostics at the bytes and the source holds %d refused runs", atTheBytes, runs)
			}
			if row.written != "" && escapeBreaks == utf8.ValidString(row.written) {
				t.Errorf("the row wants an escape diagnostic = %v, and the bytes written %q are valid UTF-8 = %v", escapeBreaks, row.written, utf8.ValidString(row.written))
			}
		})
	}
}

// TestLoad_ARefusedImportPathIsNotLookedUp pins that an import whose path
// literal holds a byte that is not UTF-8 opens no file: the sibling named with
// U+FFFD, the character that would stand in for the byte, does not compile,
// and the load reports the byte alone.
func TestLoad_ARefusedImportPathIsNotLookedUp(t *testing.T) {
	t.Parallel()
	source := "schema \"s\"\nimport \"./\xff\" as b\ntype T {\n\tid String primary\n}\n"
	got := loadIssues(t, source, map[string]string{"\uFFFD.yammm": "this is no schema\n"})
	if !slices.Equal(got, []string{invalidUTF8}) {
		t.Errorf("issues:\n  %s\nwant the byte's diagnostic alone", strings.Join(got, "\n  "))
	}
}

// TestLoad_ASourceWithNoRefusedTextAnswersAsV0210 holds every row of
// testdata/source_text_v0210.tsv, a source with no text the source rules
// refuse and no escape whose value is not UTF-8, to the issues the v0.21.0
// release reported for it. The rows named in moved differ, each by a rule the
// row states.
func TestLoad_ASourceWithNoRefusedTextAnswersAsV0210(t *testing.T) {
	t.Parallel()
	const (
		noAlias   = `E_UNKNOWN_TYPE unknown type "d.K" in datatype reference: no import declared with alias "d"`
		oneValue  = "E_INVALID_CONSTRAINT enum must have at least two values (got 1)"
		noValue   = "E_INVALID_CONSTRAINT enum must have at least two values (got 0)"
		surrogate = `E_INVALID_CONSTRAINT regex pattern "\\x{D800}" names the surrogate code point U+D800, which no string holds`
		lastOne   = `E_INVALID_CONSTRAINT regex pattern "\\x{DFFF}" names the surrogate code point U+DFFF, which no string holds`
		overThree = "E_INVALID_CONSTRAINT pattern constraint exceeds maximum of 2 patterns (got 3)"
		overFour  = "E_INVALID_CONSTRAINT pattern constraint exceeds maximum of 2 patterns (got 4)"
	)
	type move struct{ gone, added []string }
	moved := map[string]move{
		// The minimum counts every value the source wrote, so a value
		// dropped for its own fault draws its own diagnostic alone.
		"plain: enum with a malformed escape of two": {gone: []string{oneValue}},
		"plain: enum with a malformed escape alone":  {gone: []string{noValue}, added: []string{oneValue}},
		"plain: enum with a repeat":                  {gone: []string{oneValue}},
		"plain: enum with an empty value":            {gone: []string{oneValue}},
		// An import whose path has no value still declares its alias.
		"plain: an import with a malformed escape, used": {gone: []string{noAlias}},
		// A pattern naming a surrogate is refused, and counts as a pattern.
		"surrogate: alone":                                 {added: []string{surrogate}},
		"surrogate: first of two":                          {added: []string{surrogate}},
		"surrogate: first of three":                        {added: []string{surrogate}},
		"surrogate: second of three":                       {added: []string{surrogate}},
		"surrogate: third of three":                        {added: []string{surrogate}},
		"surrogate: two of three":                          {added: []string{surrogate, lastOne}},
		"surrogate: one of three beside one not compiling": {added: []string{surrogate, overThree}},
		"surrogate: one of four beside one not compiling":  {gone: []string{overThree}, added: []string{surrogate, overFour}},
		// The pattern limit counts every pattern written, as the Builder does.
		"plain: pattern of three, one not compiling":      {added: []string{overThree}},
		"plain: pattern of three, one a malformed escape": {added: []string{overThree}},
		"plain: pattern of four, one not compiling":       {gone: []string{overThree}, added: []string{overFour}},
	}
	data, err := os.ReadFile("testdata/source_text_v0210.tsv")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for line := range strings.Lines(string(data)) {
		cells := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(cells) != 3 {
			t.Fatalf("row %q has %d cells, want 3", line, len(cells))
		}
		name := cells[0]
		source, err := strconv.Unquote(cells[1])
		if err != nil {
			t.Fatalf("%s: source: %v", name, err)
		}
		joined, err := strconv.Unquote(cells[2])
		if err != nil {
			t.Fatalf("%s: issues: %v", name, err)
		}
		if refusedRuns(source) != 0 {
			t.Fatalf("%s: the source holds text the source rules refuse", name)
		}
		var want []string
		if joined != "" {
			want = strings.Split(joined, "\n")
		}
		m := moved[name]
		for _, gone := range m.gone {
			at := slices.Index(want, gone)
			if at < 0 {
				t.Fatalf("%s: v0.21.0 did not report %q", name, gone)
			}
			want = slices.Delete(want, at, at+1)
		}
		want = append(want, m.added...)
		seen++
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := loadIssues(t, source, nil)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
		})
	}
	if seen < 30 {
		t.Errorf("read %d rows, want the whole table", seen)
	}
}

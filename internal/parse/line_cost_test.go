package parse

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestParse_ARunOfRefusedTextIsOneIssue pins that adjacent faults of one kind
// are one diagnostic over the whole run, however long, and that a change of
// kind starts another.
func TestParse_ARunOfRefusedTextIsOneIssue(t *testing.T) {
	t.Parallel()
	const head = "schema \"s\"\n// "
	const tail = "\ntype T {\n\tid String primary\n}\n"
	rows := []struct {
		name, text string
		want       []string // the bytes each diagnostic covers, in order
	}{
		{"one hundred thousand NULs", strings.Repeat("\x00", 100_000), []string{strings.Repeat("\x00", 100_000)}},
		{"one hundred thousand marks", strings.Repeat("\uFEFF", 100_000), []string{strings.Repeat("\uFEFF", 100_000)}},
		{"one hundred thousand invalid bytes", strings.Repeat("\xff", 100_000), []string{strings.Repeat("\xff", 100_000)}},
		{"three kinds, adjacent", "\x00\x00\uFEFF\uFEFF\xff\xfe", []string{"\x00\x00", "\uFEFF\uFEFF", "\xff\xfe"}},
		{"one kind, parted", "\x00\x00a\x00", []string{"\x00\x00", "\x00"}},
		{"a mark parted by its own first byte", "\uFEFF\xef\uFEFF", []string{"\uFEFF", "\xef", "\uFEFF"}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			src := head + row.text + tail
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			var got []string
			for _, iss := range issues {
				if iss.Code() != diag.E_SYNTAX {
					t.Errorf("unexpected %s", iss.Code())
				}
				got = append(got, src[iss.Span().Start.Byte:iss.Span().End.Byte])
			}
			if len(got) != len(row.want) {
				t.Fatalf("%d diagnostics, want %d", len(got), len(row.want))
			}
			for i := range got {
				if got[i] != row.want[i] {
					t.Errorf("diagnostic %d covers %d bytes %q…, want %d bytes", i, len(got[i]), got[i][:min(len(got[i]), 8)], len(row.want[i]))
				}
			}
		})
	}
}

// linePosition returns the line and column of offset in src by one scan from
// the start: a line ends at "\n", at "\r\n" and at a bare "\r", and a column
// counts characters, a byte that is not valid UTF-8 as one.
func linePosition(src string, offset int) (line, column int) {
	line, column = 1, 1
	for i := 0; i < offset; {
		switch {
		case src[i] == '\r' && i+1 < len(src) && src[i+1] == '\n':
			line, column, i = line+1, 1, i+2
		case src[i] == '\n' || src[i] == '\r':
			line, column, i = line+1, 1, i+1
		default:
			_, size := utf8.DecodeRuneInString(src[i:])
			column, i = column+1, i+size
		}
	}
	return line, column
}

// TestParse_EveryPositionAgreesWithAScanFromTheStart holds the line and column
// of every diagnostic's two ends, and of every type's, property's, property
// name's, constraint's and enum literal's span, to linePosition, over sources whose lines are long, hold characters of
// every width and bytes that are no character, and end in each of the three
// ways. The spans are asked for forwards and backwards along one line.
func TestParse_EveryPositionAgreesWithAScanFromTheStart(t *testing.T) {
	t.Parallel()
	var long strings.Builder
	long.WriteString("schema \"s\"\ntype T { id String primary")
	for i := range 300 {
		fmt.Fprintf(&long, " /* é%d 🎉 */ p%d Integer[5, 1] q%d Enum[\"é\", \"\\xff\", \"ü%d\"]", i, i, i, i)
	}
	long.WriteString(" }\n")
	sources := map[string]string{
		"faults along one comment":       "schema \"s\"\n// é" + strings.Repeat("\x00a\uFEFF🎉\xffü\xe2\x82", 200) + "\ntype T {\n\tid String primary\n}\n",
		"faults along one string":        checkSource("e Enum[\"é" + strings.Repeat("\x00a\uFEFF🎉\xffü", 200) + "\", \"b\"]"),
		"many members on one line":       long.String(),
		"carriage returns":               "schema \"s\"\r// é \x00\r\ntype T {\r\tid String primary\r\n\tn Integer[5, 1] // ü \xff \x00\r}\r",
		"a doc comment over three lines": "schema \"s\"\n/* é \x00\n🎉 \xff ü \uFEFF\r\n \x00 */\ntype T {\n\tid String primary\n}\n",
		"a leading byte order mark":      "\uFEFFschema \"s\" // é \x00 ü \xff\ntype T {\n\tid String primary\n}\n",
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			checked := 0
			check := func(what string, span location.Span) {
				t.Helper()
				checked++
				for _, end := range []location.Position{span.Start, span.End} {
					line, column := linePosition(src, end.Byte)
					if end.Line != line || end.Column != column {
						t.Errorf("%s: byte %d is at %d:%d, want %d:%d", what, end.Byte, end.Line, end.Column, line, column)
					}
				}
			}
			for _, iss := range issues {
				check(iss.Message(), iss.Span())
			}
			for _, td := range f.Types {
				check("type "+td.Name, td.Span)
				for _, p := range td.Properties {
					check("property "+p.Name, p.Span)
					check("name of "+p.Name, p.NameSpan)
					check("constraint of "+p.Name, p.Constraint.Span)
					for _, lit := range p.Constraint.EnumLits {
						check("literal "+strconv.Quote(lit.Raw), lit.Span)
					}
				}
			}
			if len(issues) == 0 || checked < 4 {
				t.Fatalf("checked %d spans and %d diagnostics, want a source that draws both", checked, len(issues))
			}
		})
	}
}

// TestParse_PositionsCostALinesLengthOnce pins that deriving every position
// of a parse counts runes over a number of bytes in proportion to the source,
// for each source below, among them a line of refused runs in a comment, a line
// of properties and a line of enum values, each at one length and at eight
// times that length. Counting
// from the line's start at every position counts the line's length squared.
func TestParse_PositionsCostALinesLengthOnce(t *testing.T) {
	t.Parallel()
	sources := map[string]func(n int) string{
		"refused runs in a comment": func(n int) string {
			return "schema \"s\"\n// " + strings.Repeat("\x00a", n) + "\ntype T {\n\tid String primary\n}\n"
		},
		"byte order marks in a comment": func(n int) string {
			return "schema \"s\"\n// " + strings.Repeat("\uFEFFa", n) + "\ntype T {\n\tid String primary\n}\n"
		},
		"refused runs in a string": func(n int) string {
			return checkSource("e Enum[\"a" + strings.Repeat("\x00b", n) + "\", \"c\"]")
		},
		"properties on one line": func(n int) string {
			var b strings.Builder
			b.WriteString("schema \"s\"\ntype T { id String primary")
			for i := range n {
				b.WriteString(" p" + strconv.Itoa(i) + " Integer[5, 1]")
			}
			b.WriteString(" }\n")
			return b.String()
		},
		"enum values on one line": func(n int) string {
			values := make([]string, n)
			for i := range values {
				values[i] = strconv.Quote("v" + strconv.Itoa(i))
			}
			return checkSource("e Enum[" + strings.Join(values, ", ") + "]")
		},
	}
	// Each position counts the runes from the mark below it, for its offset
	// and its line's start, so a byte is counted more than once; the bound is
	// a small multiple. The sources below count between one and twelve bytes
	// a byte.
	const perByte = 16
	for name, source := range sources {
		for _, n := range []int{1000, 8000} {
			t.Run(name+"/"+strconv.Itoa(n), func(t *testing.T) {
				t.Parallel()
				src := source(n)
				b := parsed(src, location.NewSourceID("s.yammm"))
				if len(b.issues) < n && !strings.Contains(name, "enum") {
					t.Fatalf("%d issues, want one a unit", len(b.issues))
				}
				t.Logf("%d bytes, %d counted", len(src), b.counted)
				if b.counted < len(src)/2 {
					t.Errorf("positions counted runes over %d bytes of a %d-byte source: the count misses the long line", b.counted, len(src))
				}
				if b.counted > perByte*len(src) {
					t.Errorf("positions counted runes over %d bytes of a %d-byte source: more than %d a byte", b.counted, len(src), perByte)
				}
			})
		}
	}
}

// TestParse_NestedPositionsCostTheSourceOnce pins that the positions of
// constructs nested on one line cost in proportion to the source in the order
// the parser asks for them: a nested List's start is asked for after the
// enclosing List's end, so each pair of positions crosses the rest of the nest. Each source is
// parsed at one depth and at eight times it, and the runes counted may grow
// at most twice as fast as the source.
func TestParse_NestedPositionsCostTheSourceOnce(t *testing.T) {
	t.Parallel()
	sources := map[string]func(n int) string{
		"a List nested on one line": func(n int) string {
			return checkSource("v " + strings.Repeat("List<", n) + "String" + strings.Repeat(">", n))
		},
		"parentheses nested on one line": func(n int) string {
			return checkSource("n Integer\n\t! \"nested\" " + strings.Repeat("(", n) + "n" + strings.Repeat(")", n) + " > 0")
		},
	}
	const depth, factor = 500, 8
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			counted := func(n int) (src string, bytes int) {
				src = source(n)
				b := parsed(src, location.NewSourceID("s.yammm"))
				if len(b.issues) > 0 {
					t.Fatalf("depth %d: %s", n, b.issues[0].Message())
				}
				return src, b.counted
			}
			srcSmall, small := counted(depth)
			srcLarge, large := counted(depth * factor)
			grew := float64(len(srcLarge)) / float64(len(srcSmall))
			t.Logf("%d bytes count %d; %d bytes count %d", len(srcSmall), small, len(srcLarge), large)
			if float64(large) > 2*grew*float64(small) {
				t.Errorf("the source grew %.1f times and the runes counted %.1f times", grew, float64(large)/float64(small))
			}
		})
	}
}

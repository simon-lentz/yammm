package markdown

import (
	"math/rand/v2"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// foldBytes returns how many bytes one descriptionCell call over doc allocates,
// as the least of five calls. It must run with no parallel test beside it.
func foldBytes(doc string) uint64 {
	least := ^uint64(0)
	for range 5 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		cell := descriptionCell(doc)
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(cell)
		least = min(least, after.TotalAlloc-before.TotalAlloc)
	}
	return least
}

// TestDescriptionCell_CostIsLinearInItsLines pins the fold's cost by exact
// counts. It allocates the same number of times for a cell of 1,000 lines and
// one of 16,000: the folded line and its string, the escaped copy when the
// cell holds a pipe, one table when it reads a quoted attribute value, the
// stack of open pre and listing elements when it reads one, the stack of link
// openers when it reads a "[" that opens one, and the two parenthesis tables when it reads a link
// destination not in angle brackets. Its
// bytes grow with the text and no faster, and so do the bytes its searches for
// a closer read. It is not parallel: it reads the process's allocation
// counters, and it runs with the collector off.
func TestDescriptionCell_CostIsLinearInItsLines(t *testing.T) {
	// A collection that runs beside a measured call allocates on its own.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	for _, tt := range []struct {
		name, line string
		allocs     float64
	}{
		{"plain lines", "line of the text\n", 2},
		{"a code span a line", "line `c`\n", 2},
		{"a code span across each break", "a `x\ny` b\n", 2},
		{"a backslash a line", "line of the text\\\n", 2},
		{"a backslash and a space a line", "line of the text\\ \n", 2},
		{"a backslash before a blank line", "a\\\n\n", 2},
		{"a pre element of eight lines that end in a backslash", "<pre>\n" + strings.Repeat("a\\\n", 8) + "</pre>\n", 3},
		{"three open declarations a line", "<!A <!B <!C x\n", 2},
		{"a tag a line", "<a\nhref=x>y</a>\n", 2},
		{"a quoted attribute a line", "<a\nhref=\"x\">y</a>\n", 3},
		{"an open quoted attribute a line", "<a href=\"x\n", 3},
		{"an attribute that closes on the next line's quote", "<a href=\"x y\n", 3},
		{"both quotes left open", "<a b=\"x\n<a c='y\n", 4},
		{"an unclosed run a line", "`` a ``` b\n", 2},
		{"an open comment a line", "<!-- a\n", 2},
		{"an open processing instruction a line", "<?a\n", 2},
		{"an open CDATA section a line", "<![CDATA[ a\n", 3},
		{"a link tail across each break", "[a](\nb \"t\nu\") c\n", 5},
		{"a link with parentheses a line", "[a](b(c)) d\n", 5},
		{"an image with an open destination a line", "![a](b(c\n", 5},
		{"a pointy destination a line", "[a](<b\\\nc>) d\n", 3},
		{"an open declaration a line", "<!A b\n", 2},
		{"an open tag a line", "<a b c\n", 2},
		{"a pre line", "<pre>\n  a\n</pre>\n", 3},
		{"a pipe a line", "a | b\n", 3},
		{"trailing spaces", "a                \n", 2},
		{"three failed tails a line", "[a](b c [d](e f [g](h i\n", 5},
		{"a star a line", "*\n", 2},
		{"a www link holding brackets a line", "see www.x.co/[a](b c\n", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runtime.GC() // the collector is off: free the row before
			small, large := strings.Repeat(tt.line, 1000)+"end", strings.Repeat(tt.line, 16000)+"end"
			for _, doc := range []string{small, large} {
				if got := testing.AllocsPerRun(5, func() { descriptionCell(doc) }); got != tt.allocs {
					t.Errorf("a cell of %d bytes: %v allocations, want %v", len(doc), got, tt.allocs)
				}
			}
			smallBytes, largeBytes := foldBytes(small), foldBytes(large)
			if largeBytes > 17*smallBytes {
				t.Errorf("bytes = %d at 1,000 lines and %d at 16,000: more than 17 times as many for 16 times the text", smallBytes, largeBytes)
			}
			for _, doc := range []string{small, large} {
				f := lineFolder{inlineScanner: inlineScanner{src: doc}}
				f.fold()
				if f.read > searchBound*len(doc) {
					t.Errorf("a cell of %d bytes: its searches read %d bytes, want at most %d times the text", len(doc), f.read, searchBound)
				}
			}
		})
	}
}

// searchBound is how many times the text's length the fold's searches for a
// closer may read: the backtick runs twice, each of the four closers once
// where it fails and once over the spans it ends, the two quote tables once
// each, and the start tags, each up to the next "<".
const searchBound = 12

// TestDescriptionCell_SearchesReadTheTextABoundedNumberOfTimes pins the bound
// over texts made of the pieces the searches look for: 20,000 drawn units,
// each repeated 200 times.
func TestDescriptionCell_SearchesReadTheTextABoundedNumberOfTimes(t *testing.T) {
	t.Parallel()

	pieces := []string{"<a", " ", " ", "b=", "c=", `"`, "'", `"`, "'", "x", "\n", ">", "/", "=", "<!--", "-->", "`", "``", "<?", "?>", "<!x", "<![CDATA[", "]]>", "</a", `\`}
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a seeded generator makes the draws repeatable
	for range 20000 {
		var unit strings.Builder
		for range 2 + r.IntN(12) {
			unit.WriteString(pieces[r.IntN(len(pieces))])
		}
		doc := strings.Repeat(unit.String(), 200)
		f := lineFolder{inlineScanner: inlineScanner{src: doc}}
		f.fold()
		if f.read > searchBound*len(doc) {
			t.Fatalf("200 of %q: the searches read %d bytes of %d, want at most %d times the text", unit.String(), f.read, len(doc), searchBound)
		}
	}
}

// TestDescriptionCell_SearchesReadEachRunLengthOnce pins the bound on a text
// that holds one backtick run of each length a code span can open, each
// followed by text: a search for a closing run of a length already searched to
// the end reads nothing more.
func TestDescriptionCell_SearchesReadEachRunLengthOnce(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for n := 1; n <= maxBackticks; n++ {
		b.WriteString(strings.Repeat("`", n) + " text\n")
	}
	doc := b.String()
	f := lineFolder{inlineScanner: inlineScanner{src: doc}}
	f.fold()
	if f.read > 2*len(doc) {
		t.Errorf("runs of 1 to %d backticks in %d bytes: the searches read %d bytes, want at most twice the text", maxBackticks, len(doc), f.read)
	}
}

// TestInlineScanner_CountsWhatAStartTagReads pins that the count the bound is
// held to takes in a start tag's own read: a tag with no end is read to the
// end of the text, once.
func TestInlineScanner_CountsWhatAStartTagReads(t *testing.T) {
	t.Parallel()

	doc := "<a " + strings.Repeat("b ", 1000)
	f := lineFolder{inlineScanner: inlineScanner{src: doc}}
	f.fold()
	if want := len(doc) - len("<a"); f.read != want {
		t.Errorf("a start tag with no end in %d bytes: the searches read %d bytes, want %d", len(doc), f.read, want)
	}
}

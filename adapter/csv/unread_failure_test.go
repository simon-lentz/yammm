package csv

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// witness records whether its reader returned errTransient, which is the
// oracle these tests hold a result to: the parse reports the failure exactly
// when its reader returned one.
type witness struct {
	r      io.Reader
	failed bool
	// onFailure runs as the failure is returned, before the parser sees it.
	onFailure func()
}

func (w *witness) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if err == errTransient { //nolint:errorlint // the scripts return the sentinel itself
		w.failed = true
		if w.onFailure != nil {
			w.onFailure()
		}
	}
	return n, err
}

type scriptStep = struct {
	data string
	err  error
}

// failureScripts returns every way errTransient can meet data cut at one
// point: returned with the bytes before the cut, or alone by the Read after
// them. A cut at the end leaves the reader failing after its last byte.
func failureScripts(data string) map[string][]scriptStep {
	scripts := make(map[string][]scriptStep)
	for cut := 0; cut <= len(data); cut++ {
		scripts[fmt.Sprintf("with the bytes before %d", cut)] = []scriptStep{{data[:cut], errTransient}}
		if cut > 0 {
			scripts[fmt.Sprintf("by the Read after %d", cut)] = []scriptStep{{data[:cut], nil}, {"", errTransient}}
		}
	}
	return scripts
}

// transientFatals counts the Fatal E_ADAPTER_IO issues that carry errTransient.
func transientFatals(res diag.Result) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Severity() == diag.Fatal && issue.Code() == diag.E_ADAPTER_IO &&
			strings.Contains(issue.Message(), errTransient.Error()) {
			n++
		}
	}
	return n
}

// describe renders what the tests compare of an issue.
func describe(issue diag.Issue) string {
	return fmt.Sprintf("%v %v at %v: %s", issue.Severity(), issue.Code(), issue.Span(), issue.Message())
}

// entryPoints are the two parse methods, each over r, with a type column named
// "kind" where the method takes one.
var entryPoints = map[string]func(context.Context, io.Reader) diag.Result{
	"ParseTyped": func(ctx context.Context, r io.Reader) diag.Result {
		_, res := New().ParseTyped(ctx, location.MustNewSourceID("test://x.csv"), "P", r, nil)
		return res
	},
	"ParseWithTypeColumn": func(ctx context.Context, r io.Reader) diag.Result {
		_, res := New(WithTypeColumn("kind")).ParseWithTypeColumn(ctx, location.MustNewSourceID("test://x.csv"), r,
			func(string) *schema.Type { return nil })
		return res
	},
}

// The package documentation says "No failure is lost". A parse that ends at a
// header it refuses reads no further, so the failure its reader returned beside
// the header's bytes, or which encoding/csv dropped at a quote fault, has no
// later Read to arrive by: the parse reports it as it ends. The oracle is the
// reader, which knows whether it returned the failure.
func TestParse_AFailureTheReaderReturnedIsReportedHoweverTheParseEnds(t *testing.T) {
	t.Parallel()
	inputs := map[string]string{
		"clean":                         "kind,id\nP,p1\nP,p2\n",
		"bare quote":                    "kind,i\"d\nP,p1\n",
		"bare quote, no line end":       "kind,i\"d",
		"character after a quote":       "kind,\"id\"x\nP,p1\n",
		"character after, no line end":  "kind,\"id\"x",
		"unnamed column":                "kind,\nP,p1\n",
		"unnamed column, no line end":   "kind,",
		"repeated column":               "kind,id,id\nP,p1,p1\n",
		"repeated column, no line end":  "kind,id,id",
		"unnamed and repeated":          "kind,,id,id,\nP,,p1,p1,\n",
		"no type column":                "id,name\np1,Ann\n",
		"no type column, no line end":   "id,name",
		"no type column, repeated":      "id,id\np1,p1\n",
		"quote fault in a record":       "kind,id\nP,p\"1\nP,p2\n",
		"byte order mark, repeated":     "\xef\xbb\xbfkind,kind\nP,P\n",
		"blank lines, then an unnamed":  "\n\nkind,\nP,p1\n",
		"header alone, repeated, CR LF": "kind,kind\r\n",
	}
	for entry, parse := range entryPoints {
		for name, data := range inputs {
			for script, steps := range failureScripts(data) {
				r := &witness{r: &scripted{steps: slices.Clone(steps)}}
				res := parse(t.Context(), r)
				want := 0
				if r.failed {
					want = 1
				}
				if got := transientFatals(res); got != want {
					t.Errorf("%s, %s, the failure %s: %d Fatal E_ADAPTER_IO carrying it, the reader returned it %d times: %s",
						entry, name, script, got, want, res)
				}
			}
		}
	}
}

// The same guarantee where the parse ends at a cancelled context: the reader
// returns the failure beside bytes the parse goes on to read, and the context
// is cancelled before the Read that would have returned the failure again. The
// failure stands beside the cancellation's own Fatal and names the same record
// count.
func TestParse_AFailureTheReaderReturnedIsReportedAtACancellation(t *testing.T) {
	t.Parallel()
	for entry, parse := range entryPoints {
		for _, c := range []struct {
			steps   []scriptStep
			records int
		}{
			{[]scriptStep{{"kind,id\n", errTransient}}, 0},
			{[]scriptStep{{"kind,id\nP,p1\nP,p2\n", errTransient}}, 0},
			{[]scriptStep{{"kind,id\nP,p\"1\n", errTransient}}, 0},
			{[]scriptStep{{"kind,id\nP,p1\n", nil}, {"P,p2\n", errTransient}}, 2},
			{[]scriptStep{{"kind,id\nP,p1\nP,p2\nP,p3\n", nil}, {"P,p4\nP,p5", errTransient}}, 4},
		} {
			name := fmt.Sprintf("%s, %d Reads of %q", entry, len(c.steps), c.steps[0].data)
			ctx, cancel := context.WithCancel(t.Context())
			r := &witness{r: &scripted{steps: slices.Clone(c.steps)}, onFailure: cancel}
			res := parse(ctx, r)
			cancel()
			var got []string
			for issue := range res.Issues() {
				line := fmt.Sprintf("%v %v: %s", issue.Severity(), issue.Code(), issue.Message())
				if details := issue.Details(); len(details) > 0 {
					line += " of " + details[0].Value
				}
				got = append(got, line)
			}
			// The failure names the row type where the method was given one.
			typeName := map[string]string{"ParseTyped": " of P"}[entry]
			want := []string{
				fmt.Sprintf("fatal E_ADAPTER_IO: csv read failed after %d records: %s%s", c.records, errTransient, typeName),
				fmt.Sprintf("fatal E_CONTEXT_CANCELLED: csv parse cancelled after %d records", c.records),
			}
			slices.Sort(got)
			if !r.failed || !slices.Equal(got, want) {
				t.Errorf("%s: the reader failed: %v; issues\n%s\nwant\n%s", name, r.failed, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		}
	}
}

// What a refused header's result holds when its reader had failed: the header's
// own Errors, as the same header draws from a reader that does not fail, and
// one Fatal more, which carries no position and names the records read.
func TestParse_ARefusedHeaderKeepsItsErrorsBesideTheFailure(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		entry, name string
		steps       []scriptStep
		header      string
	}{
		{"ParseTyped", "bare quote, one Read", []scriptStep{{"i\"d,name\np1,Ann\n", errTransient}}, "i\"d,name\n"},
		{"ParseTyped", "bare quote, no line end, the next Read", []scriptStep{{"i\"d,name", nil}, {"", errTransient}}, "i\"d,name"},
		{"ParseTyped", "character after a quote, no line end, the next Read", []scriptStep{{"\"id\"x,name", nil}, {"", errTransient}}, "\"id\"x,name"},
		{"ParseTyped", "repeated column, one Read", []scriptStep{{"id,id\np1,p1\n", errTransient}}, "id,id\n"},
		{"ParseTyped", "unnamed column, one Read", []scriptStep{{"id,\np1,\n", errTransient}}, "id,\n"},
		{"ParseTyped", "two unnamed and a repeated, one Read", []scriptStep{{",id,id,\n", errTransient}}, ",id,id,\n"},
		{"ParseWithTypeColumn", "no type column, one Read", []scriptStep{{"id,name\np1,Ann\n", errTransient}}, "id,name\n"},
		{"ParseWithTypeColumn", "bare quote, no line end, the next Read", []scriptStep{{"kind,i\"d", nil}, {"", errTransient}}, "kind,i\"d"},
		{"ParseWithTypeColumn", "repeated column, one Read", []scriptStep{{"kind,kind\n", errTransient}}, "kind,kind\n"},
	} {
		parse := entryPoints[c.entry]
		var want []string
		for issue := range parse(t.Context(), strings.NewReader(c.header)).Issues() {
			want = append(want, describe(issue))
		}
		if len(want) == 0 || strings.Contains(strings.Join(want, " "), "fatal") {
			t.Fatalf("%s, %s: the header alone draws %q, so the case asserts nothing", c.entry, c.name, want)
		}
		var got []string
		var fatals []diag.Issue
		for issue := range parse(t.Context(), &scripted{steps: c.steps}).Issues() {
			if issue.Severity() == diag.Fatal {
				fatals = append(fatals, issue)
				continue
			}
			got = append(got, describe(issue))
		}
		if !slices.Equal(got, want) || len(fatals) != 1 {
			t.Errorf("%s, %s: %d Fatal issues beside\n%s\nwant one beside the header's own:\n%s",
				c.entry, c.name, len(fatals), strings.Join(got, "\n"), strings.Join(want, "\n"))
			continue
		}
		last := fatals[0]
		if last.Code() != diag.E_ADAPTER_IO || last.HasSpan() ||
			last.Message() != "csv read failed after 0 records: "+errTransient.Error() {
			t.Errorf("%s, %s: the Fatal is %s, want an E_ADAPTER_IO with no position naming 0 records", c.entry, c.name, describe(last))
		}
		// The failure names the row type as a record's read failure does.
		wantDetails := []diag.Detail{{Key: diag.DetailKeyTypeName, Value: "P"}}
		if c.entry == "ParseWithTypeColumn" {
			wantDetails = nil
		}
		if !slices.Equal(last.Details(), wantDetails) {
			t.Errorf("%s, %s: the failure's details are %v, want %v", c.entry, c.name, last.Details(), wantDetails)
		}
	}
}

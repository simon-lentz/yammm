package csv

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// parseBoth runs input through both entry points: ParseTyped as Entity, and
// ParseWithTypeColumn with a "kind" column holding Entity prepended to every
// line but a blank one. The second input shifts no line, so a span is the same in both.
func parseBoth(t *testing.T, a func(...Option) *Adapter, st *schema.Type, input string) map[string]struct {
	raws   []instance.RawInstance
	result diag.Result
} {
	t.Helper()
	const typeName = "Entity"
	id := location.MustNewSourceID("test://data/structure.csv")
	out := make(map[string]struct {
		raws   []instance.RawInstance
		result diag.Result
	})

	raws, result := a().ParseTyped(t.Context(), id, typeName, strings.NewReader(input), st)
	out["ParseTyped"] = struct {
		raws   []instance.RawInstance
		result diag.Result
	}{raws, result}

	var typed strings.Builder
	header := true
	for _, line := range strings.SplitAfter(input, "\n") {
		switch {
		case line == "" || line == "\n":
			// encoding/csv skips a blank line, so it stays blank.
		case header:
			typed.WriteString("kind,")
			header = false
		default:
			typed.WriteString(typeName + ",")
		}
		typed.WriteString(line)
	}
	byType, result := a(WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(), id, strings.NewReader(typed.String()),
		func(string) *schema.Type { return st })
	out["ParseWithTypeColumn"] = struct {
		raws   []instance.RawInstance
		result diag.Result
	}{byType[typeName], result}
	return out
}

func ids(raws []instance.RawInstance) []string {
	var out []string
	for _, r := range raws {
		id, _ := r.Properties["id"].(string)
		out = append(out, id)
	}
	return out
}

// A record the reader refuses — a record of another length, a bare quote, a
// quote fault in the record's first field — is reported where it stands, and
// the parse goes on to the next record. The first-field fault leaves the reader
// with no recorded field, so reading its position from the reader would panic.
func TestParse_ARefusedRecordIsReportedAndTheNextOneIsRead(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")

	input := "id,name\n" +
		"e1,Ann\n" +
		"e2\n" + // line 3: wrong number of fields
		"e3,An\"n\n" + // line 4: a bare quote
		"\"e4\"x,Bob\n" + // line 5: a quote fault in the first field
		"e5,Cy\n"
	for entry, got := range parseBoth(t, New, st, input) {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			if want := []string{"e1", "e5"}; !slices.Equal(ids(got.raws), want) {
				t.Errorf("instances %v, want %v: a refused record must not end the parse", ids(got.raws), want)
			}
			if got.result.HasFatal() {
				t.Errorf("a refused record is not Fatal: %s", got.result)
			}
			var lines []int
			for issue := range got.result.Issues() {
				if issue.Code() != E_CSV_COERCE || issue.Severity() != diag.Error {
					t.Errorf("unexpected diagnostic %s %v: %s", issue.Code(), issue.Severity(), issue.Message())
					continue
				}
				lines = append(lines, issue.Span().Start.Line)
			}
			if want := []int{3, 4, 5}; !slices.Equal(lines, want) {
				t.Errorf("diagnostics on lines %v, want %v", lines, want)
			}
		})
	}
}

// A bare quote is a diagnostic, not a value the file never stated: with
// encoding/csv's LazyQuotes on, `An"n` read as the name An"n.
func TestParse_ABareQuoteIsNotReadAsAValue(t *testing.T) {
	t.Parallel()
	for entry, got := range parseBoth(t, New, nil, "id,name\ne1,An\"n\n") {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			if len(got.raws) != 0 {
				t.Errorf("the record was read as %v", got.raws[0].Properties)
			}
			if _, ok := issueContaining(got.result, "bare \" in non-quoted-field"); !ok {
				t.Errorf("no bare-quote diagnostic: %s", got.result)
			}
		})
	}
}

// A quoted field that is never closed runs to the end of the input: its record
// is the last one reported, at the line the input ends on, and no later line is
// read as a value inside it.
func TestParse_AQuotedFieldNeverClosedIsTheLastRecordReported(t *testing.T) {
	t.Parallel()
	for entry, got := range parseBoth(t, New, nil, "id,name\ne1,Ann\ne2,\"Bo\ne3,Cy\n") {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			if want := []string{"e1"}; !slices.Equal(ids(got.raws), want) {
				t.Errorf("instances %v, want %v", ids(got.raws), want)
			}
			issue, ok := issueContaining(got.result, "extraneous or missing \" in quoted-field")
			if got.result.Len() != 1 || !ok || issue.Severity() != diag.Error || issue.Span().Start.Line != 4 {
				t.Errorf("want one Error on line 4, where the input ends inside the quote, got %s", got.result)
			}
		})
	}
}

// failingReader serves data, then fails every later Read with err and counts
// the failed calls, as a dropped network body or a failing device does. After
// giveUp failures it reports io.EOF, so a parse that reads on past a failure
// returns and fails the test instead of growing without bound.
type failingReader struct {
	data     string
	err      error
	failures atomic.Int64
}

const giveUp = 1000

func (r *failingReader) Read(p []byte) (int, error) {
	if r.data != "" {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if r.failures.Add(1) > giveUp {
		return 0, io.EOF
	}
	return 0, r.err
}

var errDeviceGone = errors.New("device gone")

// A reader that fails stops the parse: encoding/csv keeps no sticky error, so
// reading on repeats the failure without end. The failure is an I/O failure, so
// it is Fatal, and the records read before it are kept and counted. An error
// that only wraps io.EOF is a failure too, not the end of the input.
func TestParse_AFailingReaderStopsTheParseAtFatal(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		data    string
		err     error
		records int
	}{
		{"after a record", "id,name\ne1,Ann\n", errDeviceGone, 1},
		{"after the header", "id,name\n", errDeviceGone, 0},
		{"before the header", "", errDeviceGone, 0},
		{"with an error that wraps io.EOF", "id,name\ne1,Ann\n", fmt.Errorf("%w: %w", errDeviceGone, io.EOF), 1},
	} {
		for _, entry := range []string{"ParseTyped", "ParseWithTypeColumn"} {
			t.Run(c.name+"/"+entry, func(t *testing.T) {
				t.Parallel()
				data := c.data
				if entry == "ParseWithTypeColumn" && data != "" {
					data = strings.Replace(data, "id,name\n", "kind,id,name\n", 1)
					data = strings.ReplaceAll(data, "e1,", "Entity,e1,")
				}
				r := &failingReader{data: data, err: c.err}
				done := make(chan diag.Result, 1)
				var produced int
				go func() {
					id := location.MustNewSourceID("test://data/failing.csv")
					if entry == "ParseTyped" {
						raws, result := New().ParseTyped(t.Context(), id, "Entity", r, nil)
						produced = len(raws)
						done <- result
						return
					}
					byType, result := New(WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(), id, r,
						func(string) *schema.Type { return nil })
					produced = len(byType["Entity"])
					done <- result
				}()
				var result diag.Result
				select {
				case result = <-done:
				case <-time.After(10 * time.Second):
					t.Fatalf("the parse did not return: %d failed reads so far", r.failures.Load())
				}
				if !result.HasFatal() {
					t.Errorf("a failing reader is not Fatal: %s", result)
				}
				fatal := 0
				for issue := range result.Issues() {
					if issue.Severity() == diag.Fatal {
						fatal++
						if !strings.Contains(issue.Message(), errDeviceGone.Error()) {
							t.Errorf("the Fatal diagnostic %q does not carry the reader's error", issue.Message())
						}
						if c.data != "" && !strings.Contains(issue.Message(), fmt.Sprintf("after %d records", c.records)) &&
							!strings.Contains(issue.Message(), "reading header") {
							t.Errorf("the Fatal diagnostic %q does not count the %d records read", issue.Message(), c.records)
						}
					}
				}
				if fatal != 1 {
					t.Errorf("%d Fatal diagnostics, want 1: %s", fatal, result)
				}
				if produced != c.records {
					t.Errorf("%d instances, want the %d read before the failure", produced, c.records)
				}
				if n := r.failures.Load(); n > 4 {
					t.Errorf("the reader was read %d times after it failed", n)
				}
			})
		}
	}
}

// An input that ends before a header, and a header the reader refuses, are
// faults in the input, not I/O failures: an Error, never a Fatal.
func TestParse_AFaultBeforeTheFirstRecordIsAnError(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, input string }{
		{"an empty document", ""},
		{"a bare quote in the header", "id,na\"me\ne1,Ann\n"},
	} {
		for entry, got := range parseBoth(t, New, nil, c.input) {
			t.Run(c.name+"/"+entry, func(t *testing.T) {
				t.Parallel()
				if got.result.HasFatal() || !got.result.HasErrors() {
					t.Errorf("want an Error and no Fatal, got %s", got.result)
				}
			})
		}
	}
}

// A header that names a column twice, or leaves one unnamed, is refused at the
// header, with one diagnostic for each repeated name and each unnamed column:
// a duplicate column last-won silently, and an unnamed one became a property
// named "". The header's line is the reader's, since blank lines before it are
// skipped.
func TestParse_AHeaderColumnNamedTwiceOrNotAtAllIsRefused(t *testing.T) {
	t.Parallel()
	// The type column parseBoth prepends moves an unnamed column one place right.
	for _, c := range []struct {
		name, input string
		line        int
		want        map[string][]string
	}{
		{"named three times", "id,name,name,name\ne1,first,second,third\n", 1, map[string][]string{
			"ParseTyped":          {`header names column "name" more than once`},
			"ParseWithTypeColumn": {`header names column "name" more than once`},
		}},
		{"two not named", "id,,name,\ne1,x,Ann,y\n", 1, map[string][]string{
			"ParseTyped":          {"header column 2 has no name", "header column 4 has no name"},
			"ParseWithTypeColumn": {"header column 3 has no name", "header column 5 has no name"},
		}},
		{"after blank lines", "\n\nid,id\ne1,e2\n", 3, map[string][]string{
			"ParseTyped":          {`header names column "id" more than once`},
			"ParseWithTypeColumn": {`header names column "id" more than once`},
		}},
	} {
		for entry, got := range parseBoth(t, New, nil, c.input) {
			t.Run(c.name+"/"+entry, func(t *testing.T) {
				t.Parallel()
				if len(got.raws) != 0 {
					t.Errorf("a refused header still read %v", got.raws[0].Properties)
				}
				var messages []string
				for issue := range got.result.Issues() {
					messages = append(messages, issue.Message())
					if issue.Severity() != diag.Error || issue.Span().Start.Line != c.line {
						t.Errorf("want an Error on line %d, got %v on line %d", c.line, issue.Severity(), issue.Span().Start.Line)
					}
				}
				if !slices.Equal(messages, c.want[entry]) {
					t.Errorf("diagnostics %q, want %q", messages, c.want[entry])
				}
			})
		}
	}
}

// A key-component column that names no key of the association's target is a
// name the parser does not judge: it reaches the validator in the edge object,
// which reports it as it reports the JSON object's key, with or without
// WithSchema. An empty one is skipped, as another type's column is.
func TestParseTyped_AKeyComponentNamingNoKeyOfTheTargetReachesTheValidator(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	st, _ := s.Type("Employee")
	id := location.MustNewSourceID("test://data/bogus_key.csv")
	header := "employee_id,name,works_at._target_company_id,works_at._target_bogus\n"

	for adapter, a := range map[string]*Adapter{"WithSchema": New(WithSchema(s)), "without WithSchema": New()} {
		raws, result := a.ParseTyped(t.Context(), id, "Employee", strings.NewReader(header+"e1,Ann,c1,x\n"), st)
		if !result.OK() {
			t.Errorf("%s: the parser judges no name: %s", adapter, result)
		}
		if got := raws[0].Properties["works_at"]; !mapEqual(got, map[string]any{"_target_company_id": "c1", "_target_bogus": "x"}) {
			t.Errorf("%s: works_at = %#v", adapter, got)
		}
		_, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Employee", raws[0])
		if !vres.HasCode(instance.ErrUnknownEdgeField) {
			t.Errorf("%s: the validator does not report the key: %s", adapter, vres)
		}
	}

	raws, result := New(WithSchema(s)).ParseTyped(t.Context(), id, "Employee", strings.NewReader(header+"e1,Ann,c1,\n"), st)
	if !result.OK() {
		t.Errorf("an empty cell in the column is skipped: %s", result)
	}
	if got := raws[0].Properties["works_at"]; !mapEqual(got, map[string]any{"_target_company_id": "c1"}) {
		t.Errorf("works_at = %#v", got)
	}
}

// A plain column spelled like an association's field and that field's dotted
// columns both write one key: two values for one key, as a JSON object that
// repeats a member. The row is refused, naming both, and the group is kept,
// since it is the field's only valid form; the plain value is not silently
// lost. With either cell empty only one value is written, and nothing is
// refused.
func TestParseTyped_APlainColumnAndADottedGroupWritingOneKeyIsRefused(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	st, _ := s.Type("Employee")
	id := location.MustNewSourceID("test://data/one_key.csv")
	header := "employee_id,name,works_at,works_at._target_company_id\n"
	for _, c := range []struct {
		name, row string
		refused   bool
		want      any
	}{
		{"both write", "e1,Ann,x,c1\n", true, map[string]any{"_target_company_id": "c1"}},
		{"the plain cell empty", "e1,Ann,,c1\n", false, map[string]any{"_target_company_id": "c1"}},
		{"the group empty", "e1,Ann,x,\n", false, "x"},
	} {
		raws, result := New(WithSchema(s)).ParseTyped(t.Context(), id, "Employee", strings.NewReader(header+c.row), st)
		issue, refused := issueContaining(result, `column "works_at" and the dotted columns works_at.* both write the key "works_at"`)
		if refused != c.refused {
			t.Errorf("%s: refused = %v, want %v: %s", c.name, refused, c.refused, result)
		}
		if refused && (issue.Code() != E_CSV_COERCE || issue.Span().Start.Line != 2) {
			t.Errorf("%s: want E_CSV_COERCE on line 2, got %s on line %d", c.name, issue.Code(), issue.Span().Start.Line)
		}
		if got := raws[0].Properties["works_at"]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: works_at = %#v, want %#v", c.name, got, c.want)
		}
	}
}

func mapEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for k, v := range want {
		if m[k] != v {
			return false
		}
	}
	return true
}

// A type-column value is checked by the grammar's type-name rule before it is
// resolved, as the JSON adapter checks a top-level key, so a mis-delimited or
// mistyped discriminator is named as one.
func TestParseWithTypeColumn_AValueThatIsNoTypeNameIsRefused(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/kinds.csv")
	input := "kind,id\n" +
		",e0\n" + // line 2: no type name at all, as JSON's empty key
		"not a type,e1\n" + // line 3
		"entity,e2\n" + // line 4: a type name starts upper case
		"a.b.C,e3\n" + // line 5: one dot at most
		"Integer,e4\n" + // line 6: a reserved datatype
		"Ghost,e5\n" + // a type name no schema declares: the resolver's to answer
		"base.Entity,e6\n"
	var resolved []string
	byType, result := New(WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(), id, strings.NewReader(input),
		func(name string) *schema.Type {
			resolved = append(resolved, name)
			return nil
		})

	if want := []string{"Ghost", "base.Entity"}; !slices.Equal(resolved, want) {
		t.Errorf("resolved %v, want only %v", resolved, want)
	}
	if len(byType) != 2 || len(byType["Ghost"]) != 1 || len(byType["base.Entity"]) != 1 {
		t.Errorf("parsed types %v", byType)
	}
	var lines []int
	for issue := range result.Issues() {
		if issue.Code() != diag.E_INVALID_TYPE_TAG {
			t.Errorf("unexpected %s: %s", issue.Code(), issue.Message())
			continue
		}
		if !strings.Contains(issue.Message(), `type column "kind"`) {
			t.Errorf("message %q does not name the column", issue.Message())
		}
		lines = append(lines, issue.Span().Start.Line)
	}
	if want := []int{2, 3, 4, 5, 6}; !slices.Equal(lines, want) {
		t.Errorf("E_INVALID_TYPE_TAG on lines %v, want %v", lines, want)
	}
}

// A group whose cells disagree on the target count names the two columns that
// disagree, and the same two on every run: the count was taken from whichever
// cell Go's map order reached first.
func TestParseTyped_ATargetCountDisagreementNamesTheSameColumnsEveryRun(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "edge_properties.yammm")
	st, _ := s.Type("Order")
	input := "order_id,carries._target_item_id,carries.quantity\no1,a|b|c,1|2\n"
	const want = `association "carries" columns disagree on target count: "carries._target_item_id" holds 3, "carries.quantity" holds 2`
	for range 50 {
		_, result := New().ParseTyped(t.Context(), location.MustNewSourceID("test://o.csv"), "Order", strings.NewReader(input), st)
		issue, ok := firstIssue(result)
		if !ok || issue.Message() != want {
			t.Fatalf("diagnostic %q, want %q", issue.Message(), want)
		}
	}
}

var _ io.Reader = (*failingReader)(nil)

// The type column may stand anywhere in the header; the columns either side of
// it keep their places.
func TestParseWithTypeColumn_TheTypeColumnNeedNotBeFirst(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	byType, result := New(WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(),
		location.MustNewSourceID("test://data/last.csv"), strings.NewReader("id,kind,name\ne1,Entity,Ann\n"),
		func(string) *schema.Type { return st })
	if !result.OK() || len(byType["Entity"]) != 1 {
		t.Fatalf("parse: %v, %s", byType, result)
	}
	if got := byType["Entity"][0].Properties; !mapEqual(got, map[string]any{"id": "e1", "name": "Ann"}) {
		t.Errorf("properties %#v", got)
	}
}

// An edge value that does not coerce is reported and kept as its text, as a
// property value is, so the validator can name it too.
func TestParseTyped_AnEdgeValueThatDoesNotCoerceKeepsItsText(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "edge_properties.yammm")
	st, _ := s.Type("Order")
	raws, result := New().ParseTyped(t.Context(), location.MustNewSourceID("test://o.csv"), "Order",
		strings.NewReader("order_id,carries._target_item_id,carries.quantity\no1,a,many\n"), st)
	if _, ok := issueContaining(result, `column "carries".quantity`); !ok {
		t.Fatalf("no coercion diagnostic: %s", result)
	}
	targets, _ := raws[0].Properties["carries"].([]any)
	if len(targets) != 1 || !mapEqual(targets[0], map[string]any{"_target_item_id": "a", "quantity": "many"}) {
		t.Errorf("carries = %#v", raws[0].Properties["carries"])
	}
}

// A type column missing from the header is reported at the header's own line,
// which blank lines before it move.
func TestParseWithTypeColumn_AMissingTypeColumnIsReportedAtTheHeadersLine(t *testing.T) {
	t.Parallel()
	_, result := New(WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(),
		location.MustNewSourceID("test://data/nokind.csv"), strings.NewReader("\n\nid,name\ne1,Ann\n"),
		func(string) *schema.Type { return nil })
	issue, ok := issueContaining(result, `type column "kind" not found in header`)
	if !ok || issue.Span().Start.Line != 3 {
		t.Errorf("want the refusal on line 3, got %s", result)
	}
}

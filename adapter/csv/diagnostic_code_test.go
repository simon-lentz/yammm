package csv

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Every fault this package reports carries one of four codes, and which one
// says what the caller must do about it: fix the data, fix the file, fix the
// call, or fix the device. The table drives every emission site, and the
// header's own read twice, since it chooses between two codes at run time — so
// a site that moves between classes fails here.
func TestParse_CodeSaysWhichFaultItIs(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name       string
		schemaFile string
		typeName   string
		opts       []Option
		input      string
		reader     io.Reader
		typeColumn bool
		wantCode   diag.Code
		wantSev    diag.Severity
		mentions   string
	}{
		// The cell's text is wrong for the type its member declares. The row is
		// still produced and the cell keeps its text.
		{
			name: "a plain column that does not coerce", schemaFile: "basic.yammm", typeName: "Entity",
			input:    "id,count\ne1,notanint\n",
			wantCode: E_CSV_COERCE, wantSev: diag.Error, mentions: `column "count"`,
		},
		{
			name: "an edge property that does not coerce", schemaFile: "edge_properties.yammm", typeName: "Order",
			input:    "order_id,carries._target_item_id,carries.quantity\no1,i1,notanint\n",
			wantCode: E_CSV_COERCE, wantSev: diag.Error, mentions: `column "carries".quantity`,
		},

		// The adapter was built with a setting this parse cannot use. No record
		// is read and the remedy is in the caller's code.
		{
			name: "a list separator the parser cannot find again", schemaFile: "basic.yammm", typeName: "Entity",
			opts:     []Option{WithListSeparator(`\|`)},
			input:    "id,name\ne1,Ann\n",
			wantCode: E_CSV_CONFIG, wantSev: diag.Error, mentions: "list separator",
		},
		{
			name: "a delimiter encoding/csv refuses", schemaFile: "basic.yammm", typeName: "Entity",
			opts:     []Option{WithDelimiter('"')},
			input:    "id,name\ne1,Ann\n",
			wantCode: E_CSV_CONFIG, wantSev: diag.Error, mentions: "invalid field or comment delimiter",
		},
		{
			name: "ParseWithTypeColumn with no type column set", schemaFile: "basic.yammm", typeName: "Entity",
			input:      "kind,id,name\nEntity,e1,Ann\n",
			typeColumn: true,
			wantCode:   E_CSV_CONFIG, wantSev: diag.Error, mentions: "requires WithTypeColumn",
		},

		// The file is not well formed for this configuration. The same code the
		// JSON adapter reports a malformed document under.
		{
			name: "a record whose field count differs from the header's", schemaFile: "basic.yammm", typeName: "Entity",
			input:    "id,name\ne1\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "csv parse",
		},
		{
			name: "a header column with no name", schemaFile: "basic.yammm", typeName: "Entity",
			input:    "id,\ne1,Ann\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "has no name",
		},
		{
			name: "a header naming one column twice", schemaFile: "basic.yammm", typeName: "Entity",
			input:    "id,id\ne1,e2\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "more than once",
		},
		{
			name: "a header the reader refuses", schemaFile: "basic.yammm", typeName: "Entity",
			input:    "\"id\"x,name\ne1,Ann\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "reading header",
		},
		{
			name: "a type column absent from the header", schemaFile: "basic.yammm", typeName: "Entity",
			opts:       []Option{WithTypeColumn("kind")},
			input:      "id,name\ne1,Ann\n",
			typeColumn: true,
			wantCode:   diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "not found in header",
		},
		{
			name: "an edge group whose columns disagree on target count", schemaFile: "edge_properties.yammm", typeName: "Order",
			input:    "order_id,carries._target_item_id,carries.quantity\no1,i1|i2,5\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "disagree on target count",
		},
		{
			name: "a plain column and its dotted group both writing one key", schemaFile: "with_relations.yammm", typeName: "Employee",
			input:    "employee_id,name,works_at,works_at._target_company_id\ne1,Ann,x,c1\n",
			wantCode: diag.E_ADAPTER_PARSE, wantSev: diag.Error, mentions: "both write the key",
		},
		{
			name: "a reader that fails before the header", schemaFile: "basic.yammm", typeName: "Entity",
			reader:   &failingReader{err: errDeviceGone},
			wantCode: diag.E_ADAPTER_IO, wantSev: diag.Fatal, mentions: "reading header",
		},
		{
			name: "a reader that fails after a record", schemaFile: "basic.yammm", typeName: "Entity",
			reader:   &failingReader{data: "id,name\ne1,Ann\n", err: errDeviceGone},
			wantCode: diag.E_ADAPTER_IO, wantSev: diag.Fatal, mentions: "csv read failed after",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := loadTestSchema(t, c.schemaFile)
			st, _ := s.Type(c.typeName)
			id := location.MustNewSourceID("test://data/codes.csv")

			var r io.Reader = strings.NewReader(c.input)
			if c.reader != nil {
				r = c.reader
			}
			opts := append([]Option{WithSchema(s)}, c.opts...)

			var result diag.Result
			if c.typeColumn {
				_, result = New(opts...).ParseWithTypeColumn(t.Context(), id, r,
					func(string) *schema.Type { return st })
			} else {
				_, result = New(opts...).ParseTyped(t.Context(), id, c.typeName, r, st)
			}

			issue, ok := issueContaining(result, c.mentions)
			if !ok {
				t.Fatalf("no diagnostic mentions %q: %s", c.mentions, result)
			}
			if issue.Code() != c.wantCode {
				t.Errorf("code = %s, want %s: %s", issue.Code(), c.wantCode, issue.Message())
			}
			if issue.Severity() != c.wantSev {
				t.Errorf("severity = %v, want %v: %s", issue.Severity(), c.wantSev, issue.Message())
			}
			// Each input carries one fault, so every issue it draws is that
			// fault's. Locating one issue and judging it alone cannot see a
			// spurious issue of another code beside it.
			for other := range result.Issues() {
				if other.Code() != c.wantCode {
					t.Errorf("a second code beside %s: %s %s", c.wantCode, other.Code(), other.Message())
				}
			}
		})
	}
}

// Each class excludes the other two. A per-site assertion pins the code a fault
// DOES draw; only a whole-result read pins the codes it does not, and the split
// is worth nothing if a parse carries both classes at once.
func TestParse_EachClassExcludesTheOthers(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")

	for _, c := range []struct {
		name   string
		input  string
		forbid []diag.Code
	}{
		{
			name:   "only structural faults",
			input:  "id,name\ne1,Ann\ne2\n\"e3\"x,Cy\n",
			forbid: []diag.Code{E_CSV_COERCE, E_CSV_CONFIG},
		},
		{
			name:   "only cells that do not coerce",
			input:  "id,count\ne1,notanint\ne2,alsonot\n",
			forbid: []diag.Code{diag.E_ADAPTER_PARSE, E_CSV_CONFIG},
		},
	} {
		for entry, got := range parseBoth(t, New, st, c.input) {
			t.Run(c.name+"/"+entry, func(t *testing.T) {
				t.Parallel()
				if got.result.OK() {
					t.Fatalf("the input drew no diagnostic, so it asserts nothing: %s", c.input)
				}
				for issue := range got.result.Issues() {
					if slices.Contains(c.forbid, issue.Code()) {
						t.Errorf("%s beside the expected class: %s", issue.Code(), issue.Message())
					}
				}
			})
		}
	}
}

// E_CSV_CONFIG is an adapter code, which is what a consumer grouping
// diagnostics by category reads it as. The registry gate holds its NAME to two
// documents and its category to nothing.
func TestConfigCode_IsRegisteredAsAnAdapterCode(t *testing.T) {
	t.Parallel()
	if got := E_CSV_CONFIG.Category(); got != diag.CategoryAdapter {
		t.Errorf("E_CSV_CONFIG category = %v, want %v", got, diag.CategoryAdapter)
	}
	if got := E_CSV_COERCE.Category(); got != diag.CategoryAdapter {
		t.Errorf("E_CSV_COERCE category = %v, want %v", got, diag.CategoryAdapter)
	}
}

// A setting the adapter cannot use is refused before the parse reads a byte,
// and the refusal names the configuration rather than the input, so it carries
// no span. That holds for every such setting alike: the list separator and the
// delimiter are one question, and encoding/csv answering the second at the
// header read would give one class two shapes.
func TestParse_ARefusedSettingIsReportedBeforeAnyRead(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/settings.csv")

	// Every documented text is pinned whole: docs/VERSIONING.md quotes the
	// delimiter's, and the type-column text lives in one literal now that the
	// sentinel that carried it is gone.
	for _, c := range []struct {
		name    string
		opt     Option
		message string
	}{
		{
			"a list separator the parser cannot find again", WithListSeparator(`\|`),
			`csv adapter: list separator "\\|" begins with the escape character \, which the parser reads as an escape`,
		},
		{
			"a delimiter encoding/csv refuses", WithDelimiter('"'),
			`csv adapter: delimiter '"': csv: invalid field or comment delimiter`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			typed := &countingReader{r: strings.NewReader("id,name\ne1,Ann\n")}
			raws, res := New(c.opt).ParseTyped(t.Context(), id, "Entity", typed, st)
			byCol := &countingReader{r: strings.NewReader("kind,id,name\nEntity,e1,Ann\n")}
			byType, colRes := New(c.opt, WithTypeColumn("kind")).ParseWithTypeColumn(t.Context(), id, byCol,
				func(string) *schema.Type { return st })
			for _, result := range []diag.Result{res, colRes} {
				if _, ok := issueContaining(result, c.message); !ok {
					t.Errorf("want the message %q: %s", c.message, result)
				}
			}

			if len(raws) != 0 || len(byType) != 0 {
				t.Errorf("a refused setting produced instances: %d and %d", len(raws), len(byType))
			}
			if typed.reads != 0 || byCol.reads != 0 {
				t.Errorf("a refused setting read the input: %d and %d reads", typed.reads, byCol.reads)
			}
			for _, result := range []diag.Result{res, colRes} {
				n := 0
				for issue := range result.Issues() {
					n++
					if issue.Code() != E_CSV_CONFIG || issue.Severity() != diag.Error {
						t.Errorf("got %s %v, want an Error E_CSV_CONFIG: %s", issue.Code(), issue.Severity(), issue.Message())
					}
					if !issue.Span().IsZero() {
						t.Errorf("the refusal names the configuration, not the input, and carries a span: %v", issue.Span())
					}
				}
				if n != 1 {
					t.Errorf("want one refusal, got %d: %s", n, result)
				}
			}
		})
	}
}

// ParseWithTypeColumn with no type column set is the third member of that
// class, and the only one a settings option cannot express: it is the ABSENCE
// of one. It is refused like the others — before any read, with no span — and
// its text is the one the deleted sentinel carried.
func TestParseWithTypeColumn_NoTypeColumnIsRefusedBeforeAnyRead(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	input := &countingReader{r: strings.NewReader("kind,id,name\nEntity,e1,Ann\n")}

	byType, res := New().ParseWithTypeColumn(t.Context(), location.MustNewSourceID("test://data/nocol.csv"), input,
		func(string) *schema.Type { return st })

	if len(byType) != 0 || input.reads != 0 {
		t.Errorf("got %d types and %d reads, want none", len(byType), input.reads)
	}
	issue, ok := issueContaining(res, "csv adapter: ParseWithTypeColumn requires WithTypeColumn to be set")
	if !ok {
		t.Fatalf("the refusal's text moved: %s", res)
	}
	if issue.Message() != "csv adapter: ParseWithTypeColumn requires WithTypeColumn to be set" {
		t.Errorf("message = %q", issue.Message())
	}
	if issue.Code() != E_CSV_CONFIG || issue.Severity() != diag.Error || !issue.Span().IsZero() {
		t.Errorf("got %s %v span %v, want a spanless Error E_CSV_CONFIG", issue.Code(), issue.Severity(), issue.Span())
	}
}

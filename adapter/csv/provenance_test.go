package csv

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// firstIssue returns the result's first issue in arrival order.
func firstIssue(result diag.Result) (diag.Issue, bool) {
	for issue := range result.Issues() {
		return issue, true
	}
	return diag.Issue{}, false
}

// issueContaining returns the issue whose message holds want. A test that reads
// the FIRST issue instead passes on whichever diagnostic the input happens to
// raise first, which is how a fixture with the wrong column names can assert
// nothing it claims to.
func issueContaining(result diag.Result, want string) (diag.Issue, bool) {
	for issue := range result.Issues() {
		if strings.Contains(issue.Message(), want) {
			return issue, true
		}
	}
	return diag.Issue{}, false
}

// TestParseTyped_RecordsProvenancePerRow pins the three parts of a row's
// provenance: the document's identity, the row's path, and the line the record
// starts on.
func TestParseTyped_RecordsProvenancePerRow(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/entities.csv")

	input := "id,name,count,score,active,created_at\ne1,Alice,5,3.14,true,2024-06-15\ne2,Bob,10,2.71,false,2024-01-01\n"
	results, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(input), st)
	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}

	for i, want := range []struct {
		path string
		line int
	}{{"$.Entity[0]", 2}, {"$.Entity[1]", 3}} {
		prov := results[i].Provenance
		if prov == nil {
			t.Fatalf("row %d: no provenance", i)
		}
		if got := prov.SourceName(); got != id.String() {
			t.Errorf("row %d: source name %q, want %q", i, got, id.String())
		}
		if got := prov.Path().String(); got != want.path {
			t.Errorf("row %d: path %q, want %q", i, got, want.path)
		}
		span := prov.Span()
		if span.Source != id {
			t.Errorf("row %d: span source %v, want %v", i, span.Source, id)
		}
		if span.Start.Line != want.line || span.Start.Column != 1 {
			t.Errorf("row %d: span at %d:%d, want %d:1", i, span.Start.Line, span.Start.Column, want.line)
		}
	}
}

// TestParseTyped_ProvenanceLineSurvivesAQuotedNewline pins the defect the record
// ordinal had: a quoted newline moves every later record's line, and the
// ordinal cannot see it.
func TestParseTyped_ProvenanceLineSurvivesAQuotedNewline(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/wrapped.csv")

	input := "id,name,count,score,active,created_at\n" +
		"e1,\"Ada\nLovelace\",5,3.14,true,2024-06-15\n" +
		"e2,Bob,10,2.71,false,2024-01-01\n"
	results, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(input), st)
	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}

	if got := results[0].Provenance.Span().Start.Line; got != 2 {
		t.Errorf("the wrapped record starts on line %d, want 2", got)
	}
	// The second record is the third RECORD but the fourth LINE.
	if got := results[1].Provenance.Span().Start.Line; got != 4 {
		t.Errorf("the record after a quoted newline is on line %d, want 4", got)
	}
}

// TestParseTyped_CoercionDiagnosticsCarryTheRecordSpan pins that a cell
// diagnostic locates its record and names no ordinal.
func TestParseTyped_CoercionDiagnosticsCarryTheRecordSpan(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/bad.csv")

	input := "id,name,count,score,active,created_at\n" +
		"e1,\"Ada\nLovelace\",5,3.14,true,2024-06-15\n" +
		"e2,Bob,notanumber,2.71,false,2024-01-01\n"
	_, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(input), st)

	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("no diagnostic")
	}
	if !issue.HasSpan() {
		t.Fatalf("diagnostic %q carries no span", issue.Message())
	}
	if issue.Span().Source != id {
		t.Errorf("span source %v, want %v", issue.Span().Source, id)
	}
	if got := issue.Span().Start.Line; got != 4 {
		t.Errorf("span on line %d, want 4", got)
	}
	if strings.Contains(issue.Message(), "row ") {
		t.Errorf("message still names a record ordinal: %q", issue.Message())
	}
}

// TestParseTyped_MalformedRecordCarriesTheParseErrorSpan pins the error arm,
// which cannot read FieldPos: a parse error leaves the reader with no recorded
// field, so its position comes from the error itself.
func TestParseTyped_MalformedRecordCarriesTheParseErrorSpan(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/malformed.csv")

	// A short record: the reader fixes the field count from the header.
	input := "id,name,count,score,active,created_at\ne1,Alice\n"
	_, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(input), st)

	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("no diagnostic")
	}
	if !issue.HasSpan() {
		t.Fatalf("diagnostic %q carries no span", issue.Message())
	}
	if got := issue.Span().Start.Line; got != 2 {
		t.Errorf("span on line %d, want 2 (%s)", got, issue.Message())
	}
	if strings.Contains(issue.Message(), "row ") {
		t.Errorf("message still names a record ordinal: %q", issue.Message())
	}
}

// TestParseWithTypeColumn_RecordsProvenancePerRow pins that the second entry
// point records the same three parts, indexing each type's own slice while the
// span stays on the record's own line.
func TestParseWithTypeColumn_RecordsProvenancePerRow(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/mixed.csv")

	input := "kind,id,name,count,score,active,created_at\n" +
		"Entity,e1,\"Ada\nLovelace\",5,3.14,true,2024-06-15\n" +
		"Entity,e2,Bob,10,2.71,false,2024-01-01\n"
	a := New(WithTypeColumn("kind"))
	parsed, result := a.ParseWithTypeColumn(t.Context(), id, strings.NewReader(input),
		func(name string) *schema.Type {
			if name == "Entity" {
				return st
			}
			return nil
		})
	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}

	rows := parsed["Entity"]
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for i, want := range []struct {
		path string
		line int
	}{{"$.Entity[0]", 2}, {"$.Entity[1]", 4}} {
		prov := rows[i].Provenance
		if prov == nil {
			t.Fatalf("row %d: no provenance", i)
		}
		if got := prov.Path().String(); got != want.path {
			t.Errorf("row %d: path %q, want %q", i, got, want.path)
		}
		if got := prov.Span().Start.Line; got != want.line {
			t.Errorf("row %d: span on line %d, want %d", i, got, want.line)
		}
	}
}

// TestParseTyped_PathIndexesTheInstancesProduced pins the rule the package doc
// states, and the rule adapter/json does NOT follow: a record the reader
// refuses consumes no index, so the path counts instances and the span counts
// lines.
func TestParseTyped_PathIndexesTheInstancesProduced(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/gap.csv")

	// The middle record is short, which the reader refuses because the header
	// fixed the field count.
	input := "id,name,count,score,active,created_at\n" +
		"e1,Alice,5,3.14,true,2024-06-15\n" +
		"e2,Bob\n" +
		"e3,Carol,10,2.71,false,2024-01-01\n"
	results, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(input), st)
	if _, ok := firstIssue(result); !ok {
		t.Fatalf("the short record drew no diagnostic")
	}
	if len(results) != 2 {
		t.Fatalf("got %d instances, want 2", len(results))
	}

	for i, want := range []struct {
		path string
		line int
	}{{"$.Entity[0]", 2}, {"$.Entity[1]", 4}} {
		prov := results[i].Provenance
		if got := prov.Path().String(); got != want.path {
			t.Errorf("instance %d: path %q, want %q", i, got, want.path)
		}
		if got := prov.Span().Start.Line; got != want.line {
			t.Errorf("instance %d: span on line %d, want %d", i, got, want.line)
		}
	}
}

// TestParseWithTypeColumn_IndexesEachTypeSeparately pins the per-type index on
// the only fixture that can see it: a single-type file makes the count of types
// and the count of instances the same number.
func TestParseWithTypeColumn_IndexesEachTypeSeparately(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/interleaved.csv")

	input := "kind,id,name,count,score,active,created_at\n" +
		"Entity,e1,A,1,1.0,true,2024-01-01\n" +
		"Other,o1,B,2,2.0,true,2024-01-01\n" +
		"Entity,e2,C,3,3.0,true,2024-01-01\n" +
		",x1,D,4,4.0,true,2024-01-01\n" +
		"Other,o2,E,5,5.0,true,2024-01-01\n"
	a := New(WithTypeColumn("kind"))
	parsed, result := a.ParseWithTypeColumn(t.Context(), id, strings.NewReader(input),
		func(name string) *schema.Type {
			if name == "Entity" {
				return st
			}
			return nil
		})

	for _, want := range []struct {
		typeName string
		paths    []string
		lines    []int
	}{
		{"Entity", []string{"$.Entity[0]", "$.Entity[1]"}, []int{2, 4}},
		{"Other", []string{"$.Other[0]", "$.Other[1]"}, []int{3, 6}},
	} {
		rows := parsed[want.typeName]
		if len(rows) != len(want.paths) {
			t.Fatalf("%s: got %d rows, want %d", want.typeName, len(rows), len(want.paths))
		}
		for i := range rows {
			prov := rows[i].Provenance
			if got := prov.SourceName(); got != id.String() {
				t.Errorf("%s[%d]: source name %q, want %q", want.typeName, i, got, id.String())
			}
			if got := prov.Path().String(); got != want.paths[i] {
				t.Errorf("%s[%d]: path %q, want %q", want.typeName, i, got, want.paths[i])
			}
			if got := prov.Span().Source; got != id {
				t.Errorf("%s[%d]: span source %v, want %v", want.typeName, i, got, id)
			}
			if got := prov.Span().Start.Line; got != want.lines[i] {
				t.Errorf("%s[%d]: span on line %d, want %d", want.typeName, i, got, want.lines[i])
			}
		}
	}

	// The row whose type column is empty draws a diagnostic and consumes no
	// type's index.
	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("the empty type column drew no diagnostic")
	}
	if strings.Contains(issue.Message(), "row ") {
		t.Errorf("message names a record ordinal: %q", issue.Message())
	}
	if got := issue.Span().Start.Line; got != 5 {
		t.Errorf("empty type column reported on line %d, want 5", got)
	}
}

// TestParseWithTypeColumn_MalformedRecordCarriesASpan pins the refusal arm on
// the second entry point, which only ParseTyped's was pinned before.
func TestParseWithTypeColumn_MalformedRecordCarriesASpan(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/short.csv")
	a := New(WithTypeColumn("kind"))
	_, result := a.ParseWithTypeColumn(t.Context(), id, strings.NewReader("kind,id\nEntity,e1\nEntity\n"),
		func(string) *schema.Type { return nil })

	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("no diagnostic")
	}
	if !issue.HasSpan() {
		t.Fatalf("diagnostic %q carries no span", issue.Message())
	}
	if got := issue.Span().Start.Line; got != 3 {
		t.Errorf("span on line %d, want 3", got)
	}
}

// TestParseTyped_HeaderDiagnosticsCarryASpan pins the two refusals that happen
// before any record is read. Both are on line 1, and neither had a span.
func TestParseTyped_HeaderDiagnosticsCarryASpan(t *testing.T) {
	t.Parallel()
	id := location.MustNewSourceID("test://data/header.csv")

	_, result := New().ParseTyped(t.Context(), id, "Entity", strings.NewReader(""), nil)
	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("an empty document drew no diagnostic")
	}
	if !issue.HasSpan() || issue.Span().Start.Line != 1 {
		t.Errorf("header read failure: span %+v, want line 1", issue.Span().Start)
	}

	a := New(WithTypeColumn("missing"))
	_, result2 := a.ParseWithTypeColumn(t.Context(), id, strings.NewReader("a,b\n1,2\n"),
		func(string) *schema.Type { return nil })
	issue2, ok := firstIssue(result2)
	if !ok {
		t.Fatalf("a missing type column drew no diagnostic")
	}
	if !issue2.HasSpan() || issue2.Span().Start.Line != 1 {
		t.Errorf("type column not found: span %+v, want line 1", issue2.Span().Start)
	}
}

// TestParseTyped_UnplacedDottedColumnsAreReportedAtTheRecord pins where a
// dotted column the row's type cannot place is reported: the parser carries it
// to the validator, as a JSON key, and the validator's diagnostic carries the
// record's span through the instance's provenance.
func TestParseTyped_UnplacedDottedColumnsAreReportedAtTheRecord(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "with_relations.yammm")
	st, _ := s.Type("Employee")
	id := location.MustNewSourceID("test://data/dotted.csv")

	for _, c := range []struct {
		name   string
		header string
		code   diag.Code
		key    string
	}{
		{"no such association field", "employee_id,name,nosuch._target_company_id", instance.ErrUnknownField, "nosuch"},
		{"neither a component nor an edge property", "employee_id,name,works_at.bogus", instance.ErrUnknownEdgeField, "bogus"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raws, result := New().ParseTyped(t.Context(), id, "Employee", strings.NewReader(c.header+"\ne1,Ada,x\n"), st)
			if !result.OK() {
				t.Fatalf("the parser judges no name: %s", result)
			}
			_, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Employee", raws[0])
			for issue := range vres.Issues() {
				if issue.Code() != c.code || !strings.Contains(issue.Message(), c.key) {
					continue
				}
				if !issue.HasSpan() || issue.Span().Start.Line != 2 || issue.Span().Source != id {
					t.Errorf("diagnostic %q: span %+v, want line 2 of %s", issue.Message(), issue.Span(), id)
				}
				return
			}
			t.Errorf("no %s naming %q: %s", c.code, c.key, vres)
		})
	}
}

// TestParseTyped_EdgeGroupDiagnosticsCarryTheRecordSpan pins the two
// diagnostics assembleEdgeGroup raises, which reach the caller with a span only
// because recordToProps threads one down to it.
func TestParseTyped_EdgeGroupDiagnosticsCarryTheRecordSpan(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "edge_properties.yammm")
	st, _ := s.Type("Order")
	id := location.MustNewSourceID("test://data/edges.csv")

	for _, c := range []struct {
		name  string
		input string
		want  string
		line  int
	}{
		{
			// Two cells of one association naming different target counts.
			"columns disagree on target count",
			"order_id,carries._target_item_id,carries.quantity\no1,a|b,1\n",
			"columns disagree on target count",
			2,
		},
		{
			// An edge property that will not coerce to its declared Integer.
			"an edge property that will not coerce",
			"order_id,carries._target_item_id,carries.quantity\no1,a,notanumber\n",
			`column "carries".quantity`,
			2,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, result := New().ParseTyped(t.Context(), id, "Order", strings.NewReader(c.input), st)

			issue, ok := issueContaining(result, c.want)
			if !ok {
				t.Fatalf("no diagnostic containing %q", c.want)
			}
			if !issue.HasSpan() {
				t.Fatalf("diagnostic %q carries no span", issue.Message())
			}
			if got := issue.Span().Start.Line; got != c.line {
				t.Errorf("span on line %d, want %d (%s)", got, c.line, issue.Message())
			}
			if strings.Contains(issue.Message(), "row ") {
				t.Errorf("message names a record ordinal: %q", issue.Message())
			}
		})
	}
}

// TestParseTyped_CancellationReportsWhereItStopped pins the cancellation
// diagnostic's count and span, neither of which was asserted.
func TestParseTyped_CancellationReportsWhereItStopped(t *testing.T) {
	t.Parallel()
	s := loadTestSchema(t, "basic.yammm")
	st, _ := s.Type("Entity")
	id := location.MustNewSourceID("test://data/cancel.csv")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	input := "id,name,count,score,active,created_at\ne1,A,1,1.0,true,2024-01-01\n"
	_, result := New().ParseTyped(ctx, id, "Entity", strings.NewReader(input), st)

	issue, ok := firstIssue(result)
	if !ok {
		t.Fatalf("a cancelled context drew no diagnostic")
	}
	if got := issue.Code(); got != diag.E_CONTEXT_CANCELLED {
		t.Errorf("code %v, want %v", got, diag.E_CONTEXT_CANCELLED)
	}
	// Cancelled before the first record: no record has a position yet, so the
	// diagnostic carries no span and reports a count of zero.
	if issue.HasSpan() {
		t.Errorf("cancelled before any record, yet the span is %+v", issue.Span())
	}
	if !strings.Contains(issue.Message(), "after 0 records") {
		t.Errorf("message %q does not report the count", issue.Message())
	}
	if strings.Contains(issue.Message(), "row ") {
		t.Errorf("message names a record ordinal: %q", issue.Message())
	}
}

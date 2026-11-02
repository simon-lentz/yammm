package snapshot_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The reader holds a document's records to the facts graph.Add derives them
// by: a document that loads is one Add could have built.
const readerFactsSchema = `schema "facts"

type Company {
	id String primary
}

type Employee {
	id String primary
	--> WORKS_AT (one) Company
	--> KNOWS (_:many) Company
	--> AUDITS (one:many) Company
}

type Order {
	id String primary
	*-> LINES (many) Line
	*-> TAGS (many) Tag
}

part type Line {
	note String
}

part type Tag {
	code String primary
}
`

// readerFactsDoc marshals a graph Add built: Companies c1 and o1, the second
// sharing Order o1's key; Employee e1 with an
// edge to c1 under WORKS_AT and AUDITS; Order o1 with one keyless Line and the
// Tags t1 and t2.
func readerFactsDoc(t *testing.T) (schema.Schema, string) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), readerFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	to := map[string]any{"_target_id": "c1"}
	g := graph.New(s)
	for _, r := range []diag.Result{
		g.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "c1"})),
		g.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "o1"})),
		g.Add(t.Context(), instancetest.Valid(t, s, "Employee", map[string]any{"id": "e1", "works_at": to, "audits": []any{to}})),
		g.Add(t.Context(), instancetest.Valid(t, s, "Order", map[string]any{
			"id":    "o1",
			"lines": []any{map[string]any{"note": "a"}},
			"tags":  []any{map[string]any{"code": "t1"}, map[string]any{"code": "t2"}},
		})),
	} {
		if r.HasErrors() {
			t.Fatalf("building the graph: %s", r)
		}
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	return s, string(data)
}

// readerEdit is one edit of the fixture document: from and to are formats
// over the table rows by type name, spelled {Company} and so on.
type readerEdit struct{ from, to string }

func applyReaderEdits(t *testing.T, doc string, edits []readerEdit) string {
	t.Helper()
	var pairs []string
	for _, name := range []string{"Company", "Employee", "Order", "Line", "Tag"} {
		pairs = append(pairs, "{"+name+"}", strconv.Itoa(tableRow(t, doc, name)))
	}
	rows := strings.NewReplacer(pairs...)
	for _, e := range edits {
		doc = editOnce(t, doc, rows.Replace(e.from), rows.Replace(e.to))
	}
	return doc
}

const (
	worksAtEdge = `,"WORKS_AT":[{"target_type":{Company},"target_key":["c1"],"properties":{}}]`
	auditsEdge  = `"AUDITS":[{"target_type":{Company},"target_key":["c1"],"properties":{}}],`
	noRecords   = `"unresolved":[]`
	noDuplicate = `"duplicates":[]`
)

func unresolvedAt(relation, reason string, required bool, targetKey string) string {
	return fmt.Sprintf(`{"source_type":{Employee},"source_key":["e1"],"relation":%q,"target_type":{Company},"target_key":%s,"required":%t,"reason":%q}`,
		relation, targetKey, required, reason)
}

func records(rs ...string) readerEdit {
	return readerEdit{noRecords, `"unresolved":[` + strings.Join(rs, ",") + `]`}
}

func TestLoad_HoldsRecordsToTheFactsAddDerivesThemBy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		edits []readerEdit
		code  diag.Code
		want  string
	}{
		{
			"a required association with no record",
			[]readerEdit{{worksAtEdge, ""}},
			diag.E_SNAPSHOT_MALFORMED, `holds no edge and no record under the required association "WORKS_AT"`,
		},
		{
			"an absent record under an optional association",
			[]readerEdit{records(unresolvedAt("KNOWS", "absent", false, "null"))},
			diag.E_SNAPSHOT_MALFORMED, `states reason "absent" under "KNOWS"`,
		},
		{
			"an empty record beside an edge",
			[]readerEdit{records(unresolvedAt("AUDITS", "empty", true, "null"))},
			diag.E_SNAPSHOT_MALFORMED, `holds an empty record beside another record`,
		},
		{
			"two absent records in one association",
			[]readerEdit{
				{auditsEdge, ""},
				records(unresolvedAt("AUDITS", "absent", true, "null"), unresolvedAt("AUDITS", "absent", true, "null")),
			},
			diag.E_SNAPSHOT_MALFORMED, `holds an absent record beside another record`,
		},
		{
			"a target_missing record naming a target a root holds",
			[]readerEdit{records(unresolvedAt("KNOWS", "target_missing", false, `["c1"]`))},
			diag.E_SNAPSHOT_MALFORMED, `names target`,
		},
		{
			"a duplicate whose conflict is another type's root at its key",
			[]readerEdit{{
				noDuplicate,
				`"duplicates":[{"type":{Order},"key":["o1"],"instance":{"key":["o1"],"properties":{"id":"o1"},"provenance":null},"conflict":{"type":{Company},"key":["o1"]}}]`,
			}},
			diag.E_SNAPSHOT_MALFORMED, `a duplicate's conflict is the root at its own type and key`,
		},
		{
			"a duplicate whose conflict is another root",
			[]readerEdit{{
				noDuplicate,
				`"duplicates":[{"type":{Company},"key":["c1"],"instance":{"key":["c1"],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":{Employee},"key":["e1"]}}]`,
			}},
			diag.E_SNAPSHOT_MALFORMED, `a duplicate's conflict is the root at its own type and key`,
		},
		{
			"a part type's duplicate under the parent v0.21.1 writes",
			[]readerEdit{{
				noDuplicate,
				`"duplicates":[{"type":{Tag},"key":["t1"],"instance":{"key":["t1"],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":{Tag},"key":["t1"]},"parent_type":{Order},"parent_key":["o1"],"relation":"TAGS"}]`,
			}},
			diag.E_SNAPSHOT_MALFORMED, `states a parent or a relation`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, doc := readerFactsDoc(t)
			edited := applyReaderEdits(t, doc, c.edits)
			g, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
			if !g.IsZero() {
				t.Error("Load returned a graph Add could not build")
			}
			for issue := range res.Issues() {
				if issue.Severity() == diag.Error && issue.Code() == c.code && strings.Contains(issue.Message(), c.want) {
					return
				}
			}
			t.Errorf("want Error %s naming %q, got: %s", c.code, c.want, res)
		})
	}
}

// Each record Add does make loads, so the reader refuses only what Add would
// never record.
func TestLoad_AcceptsEachRecordAddMakes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		edits []readerEdit
	}{
		{"the document as written", nil},
		{"an absent record under a required (one)", []readerEdit{
			{worksAtEdge, ""},
			records(unresolvedAt("WORKS_AT", "absent", true, "null")),
		}},
		{"an empty record under a required (many)", []readerEdit{
			{auditsEdge, ""},
			records(unresolvedAt("AUDITS", "empty", true, "null")),
		}},
		{"a target_missing record beside an edge", []readerEdit{records(unresolvedAt("AUDITS", "target_missing", true, `["c9"]`))}},
		{"a duplicate at its own type and key", []readerEdit{{
			noDuplicate,
			`"duplicates":[{"type":{Company},"key":["c1"],"instance":{"key":["c1"],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":{Company},"key":["c1"]}}]`,
		}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, doc := readerFactsDoc(t)
			edited := applyReaderEdits(t, doc, c.edits)
			if _, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false)); res.HasErrors() {
				t.Errorf("a record Add makes was refused: %s\n%s", res, edited)
			}
		})
	}
}

// Three absent records in one slot are one break, reported once.
func TestLoad_ReportsAStandingAloneBreakOncePerSlot(t *testing.T) {
	t.Parallel()
	s, doc := readerFactsDoc(t)
	absent := unresolvedAt("AUDITS", "absent", true, "null")
	edited := applyReaderEdits(t, doc, []readerEdit{{auditsEdge, ""}, records(absent, absent, absent)})
	n := 0
	for issue := range snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false)).Issues() {
		if strings.Contains(issue.Message(), "beside another record") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the slot's break was reported %d times, want once", n)
	}
}

// stampsSchema keys Clerk by a Timestamp, so one key has several spellings, and
// gives it its required association by inheritance.
const stampsSchema = `schema "stamps"

type Slot {
	id String primary
}

abstract type Base {
	--> BOOKS (one) Slot
	--> SEES (_:many) Clerk
}

type Clerk extends Base {
	at Timestamp primary
}
`

// The reader compares every record's address in canonical form, and reads a
// type's inherited associations as its own. Verify reports what the reader
// finds alone, so each refusal is read there.
func TestVerify_RecordFactsCompareCanonicalKeysAndInheritedAssociations(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), stampsSchema, "stamps.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	g := graph.New(s)
	for _, r := range []diag.Result{
		g.Add(t.Context(), instancetest.Valid(t, s, "Slot", map[string]any{"id": "s1"})),
		g.Add(t.Context(), instancetest.Valid(t, s, "Clerk", map[string]any{
			"at":    "2024-01-01T00:00:00Z",
			"books": map[string]any{"_target_id": "s1"},
		})),
	} {
		if r.HasErrors() {
			t.Fatalf("building the graph: %s", r)
		}
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	doc := string(data)
	clerk, slot := tableRow(t, doc, "Clerk"), tableRow(t, doc, "Slot")
	const canonical, foreign = `"2024-01-01T00:00:00Z"`, `"2024-01-01T00:00:00+00:00"`
	books := fmt.Sprintf(`,"edges":{"BOOKS":[{"target_type":%d,"target_key":["s1"],"properties":{}}]}`, slot)
	record := func(relation, key, target, reason string, required bool) string {
		return fmt.Sprintf(`"unresolved":[{"source_type":%d,"source_key":[%s],"relation":%q,"target_type":%d,"target_key":%s,"required":%t,"reason":%q}]`,
			clerk, key, relation, map[bool]int{true: slot, false: clerk}[relation == "BOOKS"], target, required, reason)
	}
	rootKey := `"key":[` + canonical + `],"properties"`

	for _, c := range []struct {
		name  string
		edits []readerEdit
		want  string
	}{
		{"a root spelled another way that holds its edge", []readerEdit{{rootKey, `"key":[` + foreign + `],"properties"`}}, ""},
		{"an absent record whose source key is spelled another way", []readerEdit{
			{books, ""},
			{`"unresolved":[]`, record("BOOKS", foreign, "null", "absent", true)},
		}, ""},
		{
			"an inherited required association with no record",
			[]readerEdit{{books, ""}},
			`holds no edge and no record under the required association "BOOKS"`,
		},
		{"a target_missing record naming a root by another spelling", []readerEdit{
			{`"unresolved":[]`, record("SEES", canonical, "["+foreign+"]", "target_missing", false)},
		}, "which a root holds"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e.from, e.to)
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
			if c.want == "" {
				if res.HasErrors() {
					t.Errorf("a document Add could build was refused: %s", res)
				}
				return
			}
			for issue := range res.Issues() {
				if issue.Severity() == diag.Error && issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), c.want) {
					return
				}
			}
			t.Errorf("want Error E_SNAPSHOT_MALFORMED naming %q, got: %s", c.want, res)
		})
	}
}

// A break is refused once, by the rule it breaks: a rule whose own guard
// declines a shape another rule already refuses adds no claim of its own.
func TestLoad_OneBreakDrawsOnlyItsOwnRefusal(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		edits  []readerEdit
		absent string
	}{
		{
			"a target_missing record of another target type",
			[]readerEdit{
				records(`{"source_type":{Employee},"source_key":["e1"],"relation":"KNOWS","target_type":{Employee},"target_key":["e1"],"required":false,"reason":"target_missing"}`),
			},
			"which a root holds",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, doc := readerFactsDoc(t)
			edited := applyReaderEdits(t, doc, c.edits)
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
			if !res.HasErrors() {
				t.Fatal("the document was accepted")
			}
			if strings.Contains(res.String(), c.absent) {
				t.Errorf("a second rule claimed the break (%q): %s", c.absent, res)
			}
		})
	}
}

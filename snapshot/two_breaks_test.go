package snapshot_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// sbUnderCount names why a document holding the breaks of a row of a and a row
// of b draws fewer issues than the two rows draw apart.
type sbUnderCount struct {
	cause string
	a, b  []string
}

var sbUnderCounts = []sbUnderCount{
	{
		cause: "One schema change fixes every position that names a type, so a type that cannot hold a root is one break.",
		a:     []string{"group of a part type", "root of a part type"},
		b:     []string{"root duplicate of a part type"},
	},
	{
		cause: "A refused stored key is that one break, so a record's key is not compared with it.",
		a:     []string{"duplicate instance whose key disagrees with its property"},
		b:     []string{"duplicate stating another key than its instance"},
	},
	{
		cause: "A root whose key a check refused holds no (one) count and no record fact.",
		a: []string{
			"stored key component not a scalar",
			"stored key disagrees with its property",
			"stored key of another arity",
		},
		b: []string{
			"absent record beside an edge under a (one) association",
			"absent record beside two edges under a (one) association",
			"absent record twice beside an edge under a (one) association",
			"absent record twice beside two edges under a (one) association",
			"absent record under an optional (one) association beside two edges",
			"edge to a refused root's stated address beside an accepted one under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"record outside its bounds beside an edge under a (one) association",
			"record with no source row at one key, beside another root holding nothing under its association",
			"refused absent record beside an absent record and two edges under a (one) association",
			"target_missing record naming a root",
			"two edges under a (one) association",
		},
	},
	{
		cause: "No check judges the records or the slots at an address two roots stand at.",
		a: []string{
			"absent record twice under a (one) association",
			"empty record twice under a (many) association",
			"record with no source row under one association, beside a root holding nothing under another",
			"root with no record under a required association",
			"two records outside their bounds under a (one) association",
		},
		b: []string{
			"two roots at one address, the second holding an edge the first has an absent record for",
		},
	},
	{
		cause: "No check judges the records or the slots at an address two roots stand at.",
		a: []string{
			"composed duplicate of a keyless part whose stated conflict is another type",
			"composed duplicate whose conflict is a sibling at another key",
			"composed duplicate with no child at its key",
			"keyed (one) duplicate with a null conflict key of another type",
		},
		b: []string{
			"two roots at one address, the second holding a child",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"record with no source row at one key, beside another root holding nothing under its association",
		},
		b: []string{
			"absent record beside an edge under a (one) association",
			"absent record twice beside an edge under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"record outside its bounds beside an edge under a (one) association",
			"record with an undocumented reason beside an edge under a (one) association",
			"target_missing record naming a root beside an edge under a required (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"absent record beside an edge under a (one) association",
		},
		b: []string{
			"absent record beside two edges under a (one) association",
			"absent record twice beside an edge under a (one) association",
			"absent record twice beside two edges under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"refused absent record beside an absent record and two edges under a (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"record outside its bounds beside an edge under a (one) association",
		},
		b: []string{
			"absent record beside two edges under a (one) association",
			"absent record twice beside two edges under a (one) association",
			"edge to a refused root's stated address beside an accepted one under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"refused absent record beside an absent record and two edges under a (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"absent record twice beside an edge under a (one) association",
		},
		b: []string{
			"absent record beside two edges under a (one) association",
			"absent record twice beside two edges under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"refused absent record beside an absent record and two edges under a (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"empty record beside an edge and a target_missing record under a (one) association",
		},
		b: []string{
			"absent record twice beside two edges under a (one) association",
			"edge to a refused root's stated address beside an accepted one under a (one) association",
			"edge to no root beside an absent record",
			"refused absent record beside an absent record and two edges under a (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"absent record beside two edges under a (one) association",
		},
		b: []string{
			"empty record beside an edge and a target_missing record under a (one) association",
		},
	},
	{
		cause: "One association reports each record fact and its (one) count once, and the two rows edit its records.",
		a: []string{
			"target_missing record naming a root beside an edge under a required (one) association",
		},
		b: []string{
			"edge to a refused root's stated address beside an accepted one under a (one) association",
		},
	},
	{
		cause: "An edge a check refused is no (one) count and no record beside another.",
		a: []string{
			"edge target row out of range under a required association",
		},
		b: []string{
			"absent record beside an edge under a (one) association",
			"empty record beside an edge and a target_missing record under a (one) association",
			"record outside its bounds beside an edge under a (one) association",
		},
	},
	{
		cause: "A duplicate record a check refused resolves no conflict.",
		a: []string{
			"composed duplicate parent row out of range",
			"composed duplicate with no parent row",
		},
		b: []string{
			"composed duplicate under no root",
			"composed duplicate whose conflict is a sibling at another key",
			"keyed (many) duplicate with a null conflict key",
			"keyed (many) duplicate with an empty conflict key",
			"keyed (many) duplicate with an empty conflict key beside a sole sibling",
			"keyed (many) duplicate with an empty conflict key of another type",
		},
	},
	{
		cause: "A duplicate record a check refused resolves no conflict.",
		a: []string{
			"composed duplicate under no root",
		},
		b: []string{
			"composed duplicate whose conflict is a sibling at another key",
			"keyed (many) duplicate with a null conflict key",
			"keyed (many) duplicate with an empty conflict key",
			"keyed (many) duplicate with an empty conflict key beside a sole sibling",
			"keyed (many) duplicate with an empty conflict key of another type",
		},
	},
	{
		cause: "A duplicate record a check refused resolves no conflict.",
		a: []string{
			"duplicate with no row",
		},
		b: []string{
			"duplicate instance stating another row",
			"duplicate instance whose key disagrees with its property",
			"duplicate instance with an undeclared property",
			"root duplicate whose conflict is another root",
		},
	},
	{
		cause: "A duplicate record a check refused resolves no conflict.",
		a: []string{
			"composed duplicate of another type than its composition",
			"composed duplicate of another type whose stated conflict is its slot's",
		},
		b: []string{
			"keyed (one) duplicate with a null conflict key of another type",
		},
	},
	{
		cause: "A duplicate record a check refused resolves no conflict.",
		a: []string{
			"composed duplicate with no row",
		},
		b: []string{
			"composed duplicate whose conflict is a sibling at another key",
		},
	},
	{
		cause: "A slot refused for its name is the one issue of the duplicate that names it.",
		a: []string{
			"children under an undeclared composition",
		},
		b: []string{
			"composed duplicate under an undeclared relation",
		},
	},
	{
		cause: "A slot refused for its children holds no occupant a conflict resolves against.",
		a: []string{
			"two (many) siblings at one key",
		},
		b: []string{
			"composed duplicate whose conflict is a sibling at another key",
			"composed duplicate with no child at its key",
		},
	},
	{
		cause: "A slot refused for its children holds no occupant a conflict resolves against.",
		a: []string{
			"composed duplicate of a keyless part whose stated conflict is another type",
		},
		b: []string{
			"two children of a keyless (one) composition beside its duplicate",
		},
	},
	{
		cause: "A slot refused for its children holds no occupant a conflict resolves against.",
		a: []string{
			"keyed (one) duplicate with a null conflict key of another type",
		},
		b: []string{
			"two children of a (one) composition beside its duplicate",
		},
	},
	{
		cause: "A conflict a refused child states is judged no further.",
		a: []string{
			"composed child of another type",
			"composed child with no row",
		},
		b: []string{
			"composed duplicate whose conflict is a sibling at another key",
		},
	},
	{
		cause: "A conflict a refused child states is judged no further.",
		a: []string{
			"composed duplicate whose conflict is a sibling at another key",
		},
		b: []string{
			"(many) sibling whose key is its neighbour's and not its property's",
		},
	},
	{
		cause: "A conflict a refused child states is judged no further.",
		a: []string{
			"keyed (one) duplicate with a null conflict key of another type",
		},
		b: []string{
			"(one) composed child with no row beside its duplicate",
		},
	},
	{
		cause: "The two rows edit one child, so the siblings no longer share a key.",
		a: []string{
			"(many) sibling whose key is its neighbour's and not its property's",
			"composed child of another type",
			"composed child with no row",
		},
		b: []string{
			"two (many) siblings at the key of one whose key is not its property's",
		},
	},
	{
		cause: "The two rows state one member of one object, which is one break.",
		a: []string{
			"edge under an undeclared relation",
		},
		b: []string{
			"refused edge whose target is no root",
		},
	},
}

func (u sbUnderCount) names(a, b string) bool {
	return slices.Contains(u.a, a) && slices.Contains(u.b, b) || slices.Contains(u.a, b) && slices.Contains(u.b, a)
}

func sbUnderCause(a, b string) (string, bool) {
	for _, u := range sbUnderCounts {
		if u.names(a, b) {
			return u.cause, true
		}
	}
	return "", false
}

// sbJudged counts the failures of a read beside the required record's warning,
// which each record draws alone under revalidation.
func sbJudged(res diag.Result) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Severity().IsFailure() && issue.Code() != diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
			n++
		}
	}
	return n
}

// sbApply applies each list of edits, and reports false where an edit no longer
// matches once: the two rows then write to one place.
func sbApply(doc string, edits ...[][2]string) (string, bool) {
	for _, list := range edits {
		for _, e := range list {
			if strings.Count(doc, e[0]) != 1 {
				return "", false
			}
			doc = strings.Replace(doc, e[0], e[1], 1)
		}
	}
	return doc, true
}

func sbRevalidated(row sbRow) int {
	n := 0
	for _, w := range row.want {
		if w.onlyReval {
			n++
		}
	}
	return n
}

// Two breaks draw the issues of each, so a document holding the breaks of two
// rows draws the sum of what the rows draw apart. It draws no more in any mode.
// It draws less only for a cause [sbUnderCounts] names, or where a revalidation
// finding is lost because a check refused its root, so revalidation does not run
// it. RebuildSnapshot draws what the reader draws over every pair of rows both
// judge. The table is exact: every pair it names draws less.
func TestRead_TwoBreaksDrawTheIssuesOfEach(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	rows := sbRows()
	verify := func(mode sbMode, text string) int {
		return sbJudged(snapshot.Verify(t.Context(), []byte(text), s, mode.opts...))
	}
	apart := make([]int, len(rows))
	var plain map[[2]int]int
	for _, mode := range sbModes() {
		for i, row := range rows {
			text, ok := sbApply(doc, row.edits)
			if !ok {
				t.Fatalf("row %q does not apply", row.name)
			}
			apart[i] = verify(mode, text)
		}
		pairs := make(map[[2]int]int)
		for i, a := range rows {
			for j := i + 1; j < len(rows); j++ {
				b := rows[j]
				text, ok := sbApply(doc, a.edits, b.edits)
				if !ok {
					continue
				}
				n := verify(mode, text)
				pairs[[2]int{i, j}] = n
				sum := apart[i] + apart[j]
				cause, named := sbUnderCause(a.name, b.name)
				switch {
				case n > sum:
					t.Errorf("%s: %q and %q draw %d issues, more than their %d apart", mode.name, a.name, b.name, n, sum)
				case n < sum && !named && !sbLostToRevalidation(mode, plain[[2]int{i, j}], n, sbRevalidated(a)+sbRevalidated(b)):
					t.Errorf("%s: %q and %q draw %d issues of their %d apart, for no named cause", mode.name, a.name, b.name, n, sum)
				case n == sum && named && !mode.reval:
					t.Errorf("%s: %q and %q draw all %d issues, and the table names %q", mode.name, a.name, b.name, n, cause)
				}
			}
		}
		if !mode.reval {
			plain = pairs
		}
	}
	for pair, n := range plain {
		a, b := rows[pair[0]], rows[pair[1]]
		if a.rebuild == 0 || b.rebuild == 0 {
			continue
		}
		text, _ := sbApply(doc, a.edits, b.edits)
		if rebuilt := sbJudged(snapshot.RebuildDocument([]byte(text), s)); rebuilt != n {
			t.Errorf("%q and %q: RebuildSnapshot draws %d issues, the reader %d", a.name, b.name, rebuilt, n)
		}
	}
}

// sbLostToRevalidation reports whether a pair's count under revalidation holds
// every issue its plain read draws and at most revalued findings beside them.
func sbLostToRevalidation(mode sbMode, plain, n, revalued int) bool {
	return mode.reval && revalued > 0 && n >= plain && n <= plain+revalued
}

// A reference check runs before the (one) count and the record facts at both
// layers, so an edge to no root under a (one) association draws the dangling
// reference alone, and the edge is no count: a (one) association with an edge
// to p2 and an edge to p7 holds one record.
// An element that states a value for one field is held to its facts.
func TestRead_AnElementOfOneFieldStatesSomething(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for anchor, elements := range map[string][]string{
		sbDups: {
			`{"type":1}`, `{"key":["c1"]}`, `{"conflict":{}}`, `{"parent_type":3}`, `{"parent_key":["o1"]}`, `{"relation":"TAGS"}`,
			`{"instance":{"key":["c1"]}}`, `{"instance":{"type":1}}`, `{"instance":{"properties":{"a":1}}}`,
			`{"instance":{"edges":{"a":[]}}}`, `{"instance":{"composed":{"a":[]}}}`, `{"instance":{"provenance":{}}}`,
		},
		sbUnresolved: {
			`{"source_type":4}`, `{"source_key":["p2"]}`, `{"relation":"KNOWS"}`, `{"target_type":4}`, `{"target_key":["p8"]}`,
			`{"required":true}`, `{"reason":"absent"}`, `{"properties":{"a":1}}`,
		},
	} {
		for _, element := range elements {
			t.Run(element, func(t *testing.T) {
				t.Parallel()
				res := snapshot.Verify(t.Context(), []byte(editOnce(t, doc, anchor, anchor+element+`,`)), s, snapshot.WithIssueLimit(0))
				if sbFailures(res) == 0 || strings.Contains(res.String(), "states nothing") {
					t.Errorf("want the element's own facts refused: %s", res)
				}
			})
		}
	}
}

// An element of empty values states nothing, as a null does.
func TestRead_AnElementOfEmptyValuesStatesNothing(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for anchor, elements := range map[string][]string{
		sbDups:       {`{"key":[]}`, `{"instance":{}}`, `{"parent_key":[]}`, `{"instance":{"properties":{},"edges":{},"composed":{}}}`},
		sbUnresolved: {`{"source_key":[]}`, `{"target_key":[]}`, `{"properties":{}}`, `{"required":false}`, `{"reason":""}`},
	} {
		for _, element := range elements {
			t.Run(element, func(t *testing.T) {
				t.Parallel()
				res := snapshot.Verify(t.Context(), []byte(editOnce(t, doc, anchor, anchor+element+`,`)), s, snapshot.WithIssueLimit(0))
				if sbFailures(res) != 1 || !strings.Contains(res.String(), "states nothing") {
					t.Errorf("want one issue that the element states nothing: %s", res)
				}
			})
		}
	}
}

// A refused child states its key in its slot, whichever spelling the duplicate's
// conflict reads. A conflict that names it elsewhere is its own break, and a slot
// a nested root's refusal leaves undeclared is refused for its own name.
func TestRead_ARefusedChildAndItsDuplicateDrawTheirOwnBreaks(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for _, c := range []struct {
		name    string
		edits   [][2]string
		phrases []string
	}{
		{
			"a child with no row whose key property states the duplicate's key",
			[][2]string{{sbTagT1, `{"key":["tz"],"type":null,"properties":{"code":"t1"},"provenance":null}`}},
			[]string{"carries no types-table row"},
		},
		{
			"a nested root of a refused key under a slot its type does not declare, a duplicate naming it",
			[][2]string{
				{sbFirstGroup, sbFirstGroup + `{"type":null,"items":[{"key":[{"a":1}],"properties":{},"provenance":null}]},`},
				{sbOuterBox + `:{`, `"BOX":[{"key":[{"a":1}],"type":0,"properties":{"n":1},"composed":{"NOPE":[` + sbInnerBox + `],`},
				{sbDups, sbDups + `{"type":0,"key":null,"instance":{"key":null,"properties":{"n":5},"provenance":null},"conflict":{"type":0,"key":null},"parent_type":0,"parent_key":[{"a":1}],"relation":"NOPE"},`},
			},
			[]string{`duplicate record 0 names "NOPE"`, `holds composed children under "NOPE"`, "whose component 0 is not a scalar", "instances entry 0 carries no types-table row"},
		},
		{
			"a child of another type, a duplicate at another key stating it as the conflict",
			[][2]string{
				{sbTagT1, `{"key":["t1"],"type":5,"properties":{"code":"t1"},"provenance":null}`},
				{sbTagDup, strings.Replace(strings.ReplaceAll(sbTagDup, `"t1"`, `"t9"`), `"conflict":{"type":6,"key":["t9"]}`, `"conflict":{"type":5,"key":["t1"]}`, 1)},
			},
			[]string{"states conflict sb#Seal", "is typed sb#Seal"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			for _, phrase := range c.phrases {
				if !strings.Contains(res.String(), phrase) {
					t.Errorf("no issue naming %q: %s", phrase, res)
				}
			}
			if sbFailures(res) != len(c.phrases) {
				t.Errorf("%d issues, want %d: %s", sbFailures(res), len(c.phrases), res)
			}
		})
	}
}

// Under a cap, the document's reference checks arrive after the record checks, so
// the record's issue is the one kept.
func TestRead_ACapKeepsARecordsIssueOverAnEdges(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := editOnce(t, editOnce(t, doc, sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p7"],"properties":{}}]`), sbTagConflict, `"conflict":{"type":6,"key":["t9"]}`)
	res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(1))
	if !strings.Contains(res.String(), "duplicate conflict") {
		t.Errorf("want the duplicate's issue kept: %s", res)
	}
}

// An edge that names no root beside another record under a (one) association
// leaves two records the validator reads as one, whichever the document writes
// first, so the root is not revalidated. A lone one is read as the data it is.
func TestRead_ADanglingEdgeBesideAnotherRecordStopsRevalidation(t *testing.T) {
	t.Parallel()
	const (
		sound    = `{"target_type":1,"target_key":["c1"],"properties":{"since":99999}}`
		dangling = `{"target_type":1,"target_key":["c7"],"properties":{"since":1999}}`
		other    = `{"target_type":1,"target_key":["c8"],"properties":{"since":99999}}`
	)
	s, doc := singleBreakDoc(t)
	for _, c := range []struct {
		name   string
		edges  string
		record string
		want   []diag.Code
	}{
		{"the sound edge first", sound + `,` + dangling, "", []diag.Code{diag.E_SNAPSHOT_DANGLING_REFERENCE}},
		{"the sound edge last", dangling + `,` + sound, "", []diag.Code{diag.E_SNAPSHOT_DANGLING_REFERENCE}},
		{"two edges that name no root", dangling + `,` + other, "", []diag.Code{diag.E_SNAPSHOT_DANGLING_REFERENCE, diag.E_SNAPSHOT_DANGLING_REFERENCE}},
		{"one edge that names no root beside a record", dangling, sbLost("p1", "EMPLOYER", 1, `["c9"]`, `{"since":99999}`), []diag.Code{diag.E_SNAPSHOT_DANGLING_REFERENCE, diag.W_SNAPSHOT_UNRESOLVED_REQUIRED}},
		{"one edge that names no root", other, "", []diag.Code{diag.E_SNAPSHOT_DANGLING_REFERENCE, diag.E_CONSTRAINT_FAIL}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := editOnce(t, doc, sbEmployer, `"EMPLOYER":[`+c.edges+`]`)
			if c.record != "" {
				edited = editOnce(t, edited, sbUnresolved, sbUnresolved+c.record+`,`)
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithRevalidation(diag.Error), snapshot.WithIssueLimit(0))
			// The document's two required records at p2 draw their warning at this severity.
			if n := sbFailures(res) - 2; n != len(c.want) {
				t.Errorf("%d issues beside the two records' warnings, want %v: %s", n, c.want, res)
			}
			for _, code := range c.want {
				if !hasCode(res, code) {
					t.Errorf("no %s: %s", code, res)
				}
			}
		})
	}
}

// A break of a record's key or conflict leaves every other fact of that record
// judged. The rebuild derives no conflict from the record, and a document's parent
// key reaches its parts only with a parent type: rebuilt is what it draws.
func TestRead_EachBreakOfARecordDrawsItsOwnIssue(t *testing.T) {
	t.Parallel()
	const (
		tagInstance  = `"instance":{"key":["t1"],"properties":{"code":"t1"},"provenance":null}`
		boxRoot      = `}]},{"type":0,"items":[{"key":null,"properties":{"n":1},"provenance":null}]}],"diagnostics"`
		tagRoot      = `}]},{"type":6,"items":[{"key":["t1"],"properties":{"code":"t1"},"provenance":null}]}],"diagnostics"`
		rootDupStart = `{"type":1,"key":["c1"],`
	)
	unresolved := func(record string) [][2]string { return [][2]string{{sbUnresolved, sbUnresolved + record + `,`}} }
	instanceOf := func(to string) [][2]string { return [][2]string{{sbDupInstance, to}} }
	tagDup := func(to string) [][2]string { return [][2]string{{sbTagDup, to}} }
	for _, c := range []struct {
		name          string
		edits         [][2]string
		read, rebuilt int
	}{
		{"a record of no root with no target row", unresolved(`{"source_type":4,"source_key":["p7"],"relation":"KNOWS","target_type":null,"target_key":["p8"],"required":false,"reason":"target_missing"}`), 1, 1},
		{"a record of no root with a target row past the table", unresolved(`{"source_type":4,"source_key":["p7"],"relation":"KNOWS","target_type":99,"target_key":["p8"],"required":false,"reason":"target_missing"}`), 1, 1},
		{"an absent record of no root with no target row", unresolved(`{"source_type":4,"source_key":["p7"],"relation":"EMPLOYER","target_type":null,"target_key":null,"required":true,"reason":"absent"}`), 1, 1},

		{"a root duplicate of a null instance", instanceOf(`"instance":null`), 1, 1},
		{"a root duplicate of an empty instance", instanceOf(`"instance":{}`), 1, 1},
		{"a root duplicate whose instance has a null key", instanceOf(`"instance":{"key":null,"properties":{"id":"c1"},"provenance":null}`), 1, 1},
		{"a root duplicate whose instance key disagrees with the record and its property", instanceOf(`"instance":{"key":["cx"],"properties":{"id":"c1"},"provenance":null}`), 1, 1},
		{"a root duplicate whose conflict is at the record's key, not the instance's", instanceOf(`"instance":{"key":["cx"],"properties":{"id":"cx"},"provenance":null}`), 1, 1},

		{"a root duplicate of a part type whose own key is refused", [][2]string{
			{sbDups, sbDups + `{"type":6,"key":["tx"],"instance":{"key":["tx"],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":6,"key":["tx"]}},`},
		}, 2, 2},
		{"a root duplicate's key and conflict at another address, with no root there", [][2]string{{sbRootDup, `{"type":1,"key":["zz"],` + sbDupInstance + `,"conflict":{"type":1,"key":["zz"]}}`}}, 2, 0},
		{"a composed duplicate's key and conflict at another address, with no child there", tagDup(`{"type":6,"key":["t9"],` + tagInstance + `,"conflict":{"type":6,"key":["t9"]},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"}`), 2, 0},
		{"a composed duplicate's key and conflict at another child", tagDup(`{"type":6,"key":["t2"],` + tagInstance + `,"conflict":{"type":6,"key":["t2"]},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"}`), 1, 0},

		{"no conflict, a slot its type does not declare", tagDup(`{"type":6,"key":["t1"],` + tagInstance + `,"conflict":null,"parent_type":3,"parent_key":["o1"],"relation":"NOPE"}`), 2, 1},
		{"a conflict row past the table, a parent that is no root", tagDup(`{"type":6,"key":["t1"],` + tagInstance + `,"conflict":{"type":99,"key":["t1"]},"parent_type":3,"parent_key":["o9"],"relation":"TAGS"}`), 2, 1},
		{"a root duplicate with a conflict row past the table and a parent key", [][2]string{{sbRootDup, rootDupStart + sbDupInstance + `,"conflict":{"type":99,"key":["c1"]},"parent_key":["o1"]}`}}, 2, 0},
		{"no conflict, a relation and no parent row", tagDup(`{"type":6,"key":["t1"],` + tagInstance + `,"conflict":null,"relation":"TAGS"}`), 2, 1},
		{"a Seal under TAGS with a conflict row past the table", tagDup(`{"type":5,"key":["t1"],"instance":{"key":["t1"],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":99,"key":["t1"]},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"}`), 2, 1},

		{"a keyless root, a duplicate naming it as its parent", [][2]string{
			{sbLastGroup, boxRoot},
			{sbDups, sbDups + `{"type":0,"key":null,"instance":{"key":null,"properties":{"n":3},"provenance":null},"conflict":{"type":0,"key":null},"parent_type":0,"parent_key":null,"relation":"BOX"},`},
		}, 1, 1},
		{"a root and a root duplicate of a part type", [][2]string{
			{sbLastGroup, tagRoot},
			{sbDups, sbDups + `{"type":6,"key":["t1"],` + tagInstance + `,"conflict":{"type":6,"key":["t1"]}},`},
		}, 1, 1},
		{"two roots of a part type at one key", [][2]string{
			{sbLastGroup, `}]},{"type":6,"items":[{"key":["t1"],"properties":{"code":"t1"},"provenance":null},{"key":["t1"],"properties":{"code":"t1"},"provenance":null}]}],"diagnostics"`},
		}, 1, 1},
		{"two root duplicates of a part type", [][2]string{
			{sbDups, sbDups + `{"type":6,"key":["t1"],` + tagInstance + `,"conflict":{"type":6,"key":["t1"]}},{"type":6,"key":["t2"],"instance":{"key":["t2"],"properties":{"code":"t2"},"provenance":null},"conflict":{"type":6,"key":["t2"]}},`},
		}, 1, 1},
		{"a keyless root and a root duplicate of a part type", [][2]string{
			{sbLastGroup, boxRoot},
			{sbDups, sbDups + `{"type":0,"key":null,"instance":{"key":null,"properties":{"n":3},"provenance":null},"conflict":{"type":0,"key":null}},`},
		}, 1, 1},
		{"two sound roots at one address, the first holding a slot its type does not declare, a duplicate naming it", [][2]string{
			{sbO1, sbO1 + `"NOPE":[` + sbTagT1 + `],`},
			{sbLastOrder, `,"provenance":null},{"key":["o1"],"properties":{"id":"o1"},"provenance":null}]},{"type":4,"items":[`},
			{sbDups, sbDups + `{"type":6,"key":["t9"],"instance":{"key":["t9"],"properties":{"code":"t9"},"provenance":null},"conflict":{"type":6,"key":["t9"]},"parent_type":3,"parent_key":["o1"],"relation":"NOPE"},`},
		}, 2, 2},
		{"two sound roots at one address, the second holding a slot its type does not declare, a duplicate naming it", [][2]string{
			{sbLastOrder, `,"provenance":null},{"key":["o1"],"properties":{"id":"o1"},"composed":{"NOPE":[` + sbTagT1 + `]},"provenance":null}]},{"type":4,"items":[`},
			{sbDups, sbDups + `{"type":6,"key":["t9"],"instance":{"key":["t9"],"properties":{"code":"t9"},"provenance":null},"conflict":{"type":6,"key":["t9"]},"parent_type":3,"parent_key":["o1"],"relation":"NOPE"},`},
		}, 2, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, doc := singleBreakDoc(t)
			for _, e := range c.edits {
				doc = editOnce(t, doc, e[0], e[1])
			}
			read := snapshot.Verify(t.Context(), []byte(doc), s, snapshot.WithIssueLimit(0))
			if sbFailures(read) != c.read {
				t.Errorf("the reader draws %d issues, want %d: %s", sbFailures(read), c.read, read)
			}
			rebuilt := snapshot.RebuildDocument([]byte(doc), s)
			if sbFailures(rebuilt) != c.rebuilt {
				t.Errorf("RebuildSnapshot draws %d issues, want %d: %s", sbFailures(rebuilt), c.rebuilt, rebuilt)
			}
		})
	}
}

const numberKeySchema = `schema "nk"

type N {
	id String primary
	--> LINK (_:many) N
	*-> P (_:many) K
}

part type K {
	code String primary
}
`

// A key property holds its number however the document spells it, so a reference to
// that number is a reference to the address the refused root states. A negative zero
// does not read back, so it states no address and a reference to it is its own break.
func TestRead_AKeyPropertyStatesItsNumberInAnySpelling(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberKeySchema, "nk.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, props := range []map[string]any{
		{"id": "n10"},
		{"id": "n20", "LINK": []any{map[string]any{"_target_id": "n20"}}},
		{"id": "n30", "P": []any{map[string]any{"code": "k7"}}},
	} {
		vi, res := v.ValidateOne(t.Context(), "N", instance.RawInstance{Properties: props})
		if res.HasErrors() {
			t.Fatalf("validate: %s", res)
		}
		g.Add(t.Context(), vi)
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	const (
		n10     = `{"key":["n10"],"properties":{"id":"n10"},"provenance":null}`
		link    = `"LINK":[{"target_type":1,"target_key":["n20"],"properties":{}}]`
		k7      = `{"key":["k7"],"type":0,"properties":{"code":"k7"},"provenance":null}`
		dups    = `"duplicates":[`
		records = `"unresolved":[`
	)
	stated := func(spelled string) [2]string {
		return [2]string{n10, `{"key":["n10"],"properties":{"id":` + spelled + `},"provenance":null}`}
	}
	edgeTo := func(key string) [2]string {
		return [2]string{link, `"LINK":[{"target_type":1,"target_key":[` + key + `],"properties":{}}]`}
	}
	dupOf := func(parent, key, conflict string) [2]string {
		return [2]string{dups, dups + `{"type":0,"key":[` + key + `],"instance":{"key":[` + key + `],"properties":{"code":` + key + `},"provenance":null},"conflict":{"type":0,"key":[` + conflict + `]},"parent_type":1,"parent_key":[` + parent + `],"relation":"P"}`}
	}
	recordOf := func(key string) [2]string {
		return [2]string{records, records + `{"source_type":1,"source_key":[` + key + `],"relation":"LINK","target_type":1,"target_key":["n99"],"required":false,"reason":"target_missing"}`}
	}
	for _, c := range []struct {
		name  string
		edits [][2]string
		want  int
	}{
		{"1.0 and an edge to 1", [][2]string{stated(`1.0`), edgeTo(`1`)}, 1},
		{"1e0 and an edge to 1", [][2]string{stated(`1e0`), edgeTo(`1`)}, 1},
		{"1 and an edge to 1", [][2]string{stated(`1`), edgeTo(`1`)}, 1},
		{"a number past int64 and an edge to its nearest float", [][2]string{stated(`18446744073709551615`), edgeTo(`18446744073709552000`)}, 1},
		{"1.0 and a record of 1", [][2]string{stated(`1.0`), recordOf(`1`)}, 1},
		{"1.0 and a duplicate whose parent is 1", [][2]string{stated(`1.0`), dupOf(`1`, `"k7"`, `"k7"`)}, 1},
		{"1.0 and a duplicate whose conflict is 1", [][2]string{{k7, `{"key":["k9"],"type":0,"properties":{"code":1.0},"provenance":null}`}, dupOf(`"n30"`, `1`, `1`)}, 1},
		{"-0.0 and a duplicate whose parent is -0.0", [][2]string{stated(`-0.0`), dupOf(`-0.0`, `"k7"`, `"k7"`)}, 2},
		{"-0e0 and a duplicate whose parent is -0e0", [][2]string{stated(`-0e0`), dupOf(`-0e0`, `"k7"`, `"k7"`)}, 2},
		{"-0.0 and a record of -0.0", [][2]string{stated(`-0.0`), recordOf(`-0.0`)}, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			read := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if strings.Contains(read.String(), "failed to read") {
				t.Fatalf("the edited document does not parse: %s", read)
			}
			if sbFailures(read) != c.want {
				t.Errorf("the reader draws %d issues, want %d: %s", sbFailures(read), c.want, read)
			}
			rebuilt := snapshot.RebuildDocument([]byte(edited), s)
			if sbFailures(rebuilt) != c.want {
				t.Errorf("RebuildSnapshot draws %d issues, want %d: %s", sbFailures(rebuilt), c.want, rebuilt)
			}
		})
	}
}

// A record a row check refused names no reference, so its source is judged no
// further, whichever row the check refused.
func TestRead_ARecordOfARefusedRowNamesNoReference(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for _, targetRow := range []string{`null`, `99`} {
		edited := editOnce(t, doc, sbUnresolved, sbUnresolved+`{"source_type":4,"source_key":["p7"],"relation":"KNOWS","target_type":`+targetRow+`,"target_key":["p8"],"required":false,"reason":"target_missing"},`)
		res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
		if hasCode(res, diag.E_SNAPSHOT_DANGLING_REFERENCE) || !strings.Contains(res.String(), "target") {
			t.Errorf("target row %s: want the row's refusal alone: %s", targetRow, res)
		}
	}
}

const rootlessTypeSchema = `schema "rl"

abstract type Base {
	id String primary
	--> OWNER (one) Company
	--> PATRON (_:one) Company
}

type Company {
	id String primary
}
`

// A record's source row and target row are two fields a schema change fixes apart.
func TestRead_ARecordOfTwoRefusedRowsDrawsTheIssueOfEach(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := editOnce(t, doc, sbUnresolved, sbUnresolved+`{"source_type":99,"source_key":["p7"],"relation":"KNOWS","target_type":99,"target_key":["p8"],"required":false,"reason":"target_missing"},`)
	res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
	if sbFailures(res) != 2 || !strings.Contains(res.String(), "source") || !strings.Contains(res.String(), "target") {
		t.Errorf("the reader draws %d issues, want one for each row: %s", sbFailures(res), res)
	}
}

// A root of a type that cannot hold one is its entry's one refusal, so it owes no
// required association and is in no (one) count, at the reader and the rebuild. A
// break its records show by themselves stays its own.
func TestRead_ARootOfATypeThatCannotHoldOneHoldsNoAddress(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), rootlessTypeSchema, "rl.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	g := graph.New(s)
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "Company", instance.RawInstance{Properties: map[string]any{"id": "c1"}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g.Add(t.Context(), vi)
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	doc = editOnce(t, doc, `"types":[{"schema":"rl","name":"Company"}]`, `"types":[{"schema":"rl","name":"Base"},{"schema":"rl","name":"Company"}]`)
	doc = strings.Replace(doc, `{"type":0,"items":[{"key":["c1"]`, `{"type":1,"items":[{"key":["c1"]`, 1)
	const absent = `{"source_type":0,"source_key":["b1"],"relation":"OWNER","target_type":1,"target_key":null,"required":true,"reason":"absent"}`
	const optional = `{"source_type":0,"source_key":["b1"],"relation":"PATRON","target_type":1,"target_key":null,"required":false,"reason":"absent"}`
	root := func(edges string) [2]string {
		return [2]string{`"instances":[`, `"instances":[{"type":0,"items":[{"key":["b1"],"properties":{"id":"b1"},` + edges + `"provenance":null}]},`}
	}
	for _, c := range []struct {
		name  string
		edits [][2]string
		want  int
	}{
		{"holding nothing under a required association", [][2]string{root(``)}, 1},
		{"holding two edges under a (one) association", [][2]string{root(`"edges":{"OWNER":[{"target_type":1,"target_key":["c1"],"properties":{}},{"target_type":1,"target_key":["c1"],"properties":{}}]},`)}, 1},
		{"holding two absent records", [][2]string{root(``), {sbUnresolved, sbUnresolved + absent + `,` + absent}}, 1},
		{"holding an absent record under an optional association", [][2]string{root(``), {sbUnresolved, sbUnresolved + optional}}, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			read := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if sbFailures(read) != c.want || !hasCode(read, diag.E_SNAPSHOT_INVALID_ROOT) {
				t.Errorf("the reader draws %d issues, want %d: %s", sbFailures(read), c.want, read)
			}
			rebuilt := snapshot.RebuildDocument([]byte(edited), s)
			if sbFailures(rebuilt) != c.want {
				t.Errorf("RebuildSnapshot draws %d issues, want %d: %s", sbFailures(rebuilt), c.want, rebuilt)
			}
		})
	}
}

// A refusal of a key names the key as the document spells it: the number it reads
// as can read back, where the literal does not.
func TestRead_ARefusedKeyIsNamedAsSpelled(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	root := func(key string) [][2]string {
		return [][2]string{{sbC2, `{"key":` + key + `,"properties":{"id":"c2"},"provenance":null}`}}
	}
	employer := [2]string{sbEmployer, `"EMPLOYER":[{"target_type":1,"target_key":[[2.0]],"properties":{"since":1999}}]`}
	for _, c := range []struct {
		name  string
		edits [][2]string
		want  string
		pk    string
	}{
		{"a negative zero", root(`[-0]`), `stores key [-0], whose component 0`, `[-0]`},
		{"a number another rendering reads back", root(`[1.0]`), `stores key [1.0], whose component 0`, `[1.0]`},
		{"a nested number", root(`[[1.0]]`), `stores key [[1.0]], whose component 0`, `[[1.0]]`},
		{
			"a nested number of a duplicate's instance",
			[][2]string{{sbDups, sbDups + `{"type":1,"key":[[1.0]],"instance":{"key":[[1.0]],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":1,"key":["c1"]}},`}},
			`stores key [[1.0]], whose component 0`, `[[1.0]]`,
		},
		{
			"a duplicate record's own key",
			[][2]string{{sbDups, sbDups + `{"type":1,"key":[1.0],` + sbDupInstance + `,"conflict":{"type":1,"key":["c1"]}},`}},
			`states key [1.0] but its instance carries ["c1"]`, "",
		},
		{
			"a duplicate record with no key of its own",
			[][2]string{{sbDups, sbDups + `{"type":1,` + sbDupInstance + `,"conflict":{"type":1,"key":["c1"]}},`}},
			`states key [] but its instance carries ["c1"]`, "",
		},
		{"an edge's target", [][2]string{employer}, `carries target key [[2.0]], whose component 0`, ""},
		{
			"an edge's target and its refused source",
			[][2]string{employer, {sbP1, `{"key":[1.0],"properties":{"age":30,"id":"p1"},"edges":{`}},
			`edge 0 of [1.0] under "EMPLOYER" carries target key [[2.0]]`, "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if !strings.Contains(res.String(), c.want) {
				t.Fatalf("the refusal does not name the key as spelled: %s", res)
			}
			if c.pk == "" {
				return
			}
			for issue := range res.Issues() {
				if !strings.Contains(issue.Message(), c.want) {
					continue
				}
				for _, d := range issue.Details() {
					if d.Key == diag.DetailKeyPrimaryKey && d.Value != c.pk {
						t.Errorf("the detail names %s, want %s", d.Value, c.pk)
					}
				}
			}
		})
	}
}

// A root of a type that cannot hold one has its key read for its address before
// the key rule refuses it, and the refusal still names it as spelled.
func TestRead_ARootOfATypeThatCannotHoldOneNamesItsKeyAsSpelled(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), rootlessTypeSchema, "rl.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	g := graph.New(s)
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	doc = editOnce(t, doc, `"types":[]`, `"types":[{"schema":"rl","name":"Base"}]`)
	doc = editOnce(t, doc, `"instances":[]`, `"instances":[{"type":0,"items":[{"key":[[1.0]],"properties":{"id":"b1"},"provenance":null}]}]`)
	read := snapshot.Verify(t.Context(), []byte(doc), s, snapshot.WithIssueLimit(0))
	if !strings.Contains(read.String(), `stores key [[1.0]], whose component 0`) {
		t.Errorf("the refusal does not name the key as spelled: %s", read)
	}
}

// Every root asks whether a record of no source row could stand at it, so the
// records are read once per relation, never once per root.
func TestRead_RecordsOfNoSourceRowCostLinearly(t *testing.T) {
	s, doc := singleBreakDoc(t)
	allocations := func(k int) float64 {
		var roots, records strings.Builder
		for i := range k {
			fmt.Fprintf(&roots, `{"key":["k%d"],"properties":{"id":"k%d"},"provenance":null},`, i, i)
			fmt.Fprintf(&records, `{"source_type":999,"source_key":["x%d"],"relation":"EMPLOYER","target_type":1,"target_key":null,"required":true,"reason":"absent"},`, i)
		}
		edited := []byte(editOnce(t, editOnce(t, doc, `{"type":4,"items":[`, `{"type":4,"items":[`+roots.String()), sbUnresolved, sbUnresolved+records.String()))
		return testing.AllocsPerRun(1, func() {
			if res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(1)); res.OK() {
				t.Fatal("the document is accepted")
			}
		})
	}
	if small, large := allocations(100), allocations(400); large > 6*small {
		t.Errorf("%.0f allocations at 100 roots and %.0f at 400, want growth near 4 times", small, large)
	}
}

// The duplicates of one slot read its refused children once, so their cost grows
// with the slot, never with its square.
func TestRead_DuplicatesBesideRefusedChildrenCostLinearly(t *testing.T) {
	s, doc := singleBreakDoc(t)
	allocations := func(k int) float64 {
		var children, records strings.Builder
		for i := range k {
			fmt.Fprintf(&children, `{"key":["x%d"],"type":99,"properties":{"code":"s%d"},"provenance":null},`, i, i)
			fmt.Fprintf(&records, `{"type":6,"key":["d%d"],"instance":{"key":["d%d"],"properties":{"code":"d%d"},"provenance":null},"conflict":{"type":6,"key":["d%d"]},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"},`, i, i, i, i)
		}
		edited := []byte(editOnce(t, editOnce(t, doc, sbTagT1, children.String()+sbTagT1), sbDups, sbDups+records.String()))
		return testing.AllocsPerRun(1, func() {
			if res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0)); res.OK() {
				t.Fatal("the document is accepted")
			}
		})
	}
	if small, large := allocations(100), allocations(400); large > 6*small {
		t.Errorf("%.0f allocations at 100 duplicates and %.0f at 400, want growth near 4 times", small, large)
	}
}

func TestRead_AReferenceCheckRunsBeforeTheOneCount(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := []byte(editOnce(t, doc, sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}},{"target_type":4,"target_key":["p7"],"properties":{}}]`))
	read := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0))
	if sbFailures(read) != 1 || !hasCode(read, diag.E_SNAPSHOT_DANGLING_REFERENCE) {
		t.Errorf("the reader draws %s, want the dangling reference alone", read)
	}
	rebuilt := snapshot.RebuildDocument(edited, s)
	if sbFailures(rebuilt) != 1 || !strings.Contains(rebuilt.String(), `edge target`) {
		t.Errorf("RebuildSnapshot draws %s, want the edge's reference alone", rebuilt)
	}
}

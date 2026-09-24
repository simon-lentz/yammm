package snapshot_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The reader holds every association record, and every root or root duplicate,
// to the rules graph.RebuildSnapshot holds its parts to, in the walk Verify and
// Load share. Each refusal case hand-edits a Marshal-produced document and
// asserts the refusal of the fault it names.

const readerSchema = `schema "rd"

type Company {
	id String primary
}

type Office {
	id String primary
}

type Event {
	at Timestamp primary
	--> HOST (_:one) Company
}

abstract type Base {
	id String primary
}

part type Tag {
	label String
}

type Person {
	id String primary
	--> WORKS_AT (_:one) Company
	--> BOSS (one:one) Person
	--> KNOWS (_:many) Person
	--> FRIENDS (one:many) Person
	*-> TAGS (many) Tag
}
`

func loadReaderSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), readerSchema, "rd.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s
}

func readerDoc(t *testing.T, s *schema.Schema, rows ...map[string]any) string {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, row := range rows {
		typ := row["$type"].(string)
		props := map[string]any{}
		for k, val := range row {
			if k != "$type" {
				props[k] = val
			}
		}
		vi, res := v.ValidateOne(context.Background(), typ, instance.RawInstance{Properties: props})
		if res.HasErrors() {
			t.Fatalf("validate %s: %s", typ, res)
		}
		g.Add(context.Background(), vi)
	}
	data, res := snapshot.Marshal(context.Background(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	return string(data)
}

func editOnce(t *testing.T, doc, from, to string) string {
	t.Helper()
	if n := strings.Count(doc, from); n != 1 {
		t.Fatalf("anchor %q occurs %d times in %s", from, n, doc)
	}
	return strings.Replace(doc, from, to, 1)
}

func tableRow(t *testing.T, doc, name string) int {
	t.Helper()
	i := strings.Index(doc, `"types":[`)
	j := strings.Index(doc[i:], `]`)
	for k, r := range strings.Split(doc[i+len(`"types":[`):i+j], "},{") {
		if strings.Contains(r, `"name":"`+name+`"`) {
			return k
		}
	}
	t.Fatalf("no row %s in %s", name, doc)
	return -1
}

// readBoth returns Verify's and Load's results for doc.
func readBoth(t *testing.T, s *schema.Schema, doc string) map[string]diag.Result {
	t.Helper()
	_, l := snapshot.Load(context.Background(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
	return map[string]diag.Result{"Verify": snapshot.Verify(context.Background(), []byte(doc), s, snapshot.WithIntegrityCheck(false)), "Load": l}
}

func requireReaderRule(t *testing.T, name string, got map[string]diag.Result, code diag.Code, phrase string) {
	t.Helper()
	for surface, res := range got {
		found := false
		for issue := range res.Issues() {
			if issue.Code() == code && issue.Severity().IsFailure() && strings.Contains(issue.Message(), phrase) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s, %s: no refusal %s naming %q: %s", name, surface, code, phrase, res)
		}
	}
}

// personDoc holds p1 with an unresolved WORKS_AT and KNOWS record, and an edge
// to p0 under BOSS, so each record can be edited alone.
func personDoc(t *testing.T, s *schema.Schema) string {
	t.Helper()
	return readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Office", "id": "o1"},
		map[string]any{"$type": "Person", "id": "p0", "BOSS": map[string]any{"_target_id": "p0"}, "FRIENDS": []any{map[string]any{"_target_id": "p0"}}},
		map[string]any{
			"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p0"},
			"FRIENDS":  []any{map[string]any{"_target_id": "p0"}},
			"WORKS_AT": map[string]any{"_target_id": "c9"}, "KNOWS": []any{map[string]any{"_target_id": "p8"}},
		},
	)
}

func TestReader_RefusesAnUnresolvedRecordTheGraphCannotStage(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := personDoc(t, s)
	for name, res := range readBoth(t, s, doc) {
		if res.HasErrors() {
			t.Fatalf("control, %s: the clean document was refused: %s", name, res)
		}
	}
	company, office := tableRow(t, doc, "Company"), tableRow(t, doc, "Office")
	for _, c := range []struct {
		name, from, to, phrase string
		code                   diag.Code
	}{
		{"an undeclared relation", `"relation":"WORKS_AT"`, `"relation":"BOGUS"`, `under "BOGUS", which the type does not declare as an association`, diag.E_GRAPH_UNKNOWN_RELATION},
		{"a composition's name", `"relation":"WORKS_AT"`, `"relation":"TAGS"`, `under "TAGS", which the type does not declare as an association`, diag.E_GRAPH_UNKNOWN_RELATION},
		{
			"another target type", fmt.Sprintf(`"relation":"WORKS_AT","target_type":%d`, company),
			fmt.Sprintf(`"relation":"WORKS_AT","target_type":%d`, office), "which the association declares as", diag.E_SNAPSHOT_TYPE_MISMATCH,
		},
		{"a target key of the wrong arity", `"target_key":["c9"]`, `"target_key":["c9","x"]`, "carries a 2-part target key; the target declares 1", diag.E_SNAPSHOT_MALFORMED},
	} {
		requireReaderRule(t, c.name, readBoth(t, s, editOnce(t, doc, c.from, c.to)), c.code, c.phrase)
	}
}

// A (one) association holding two records is reported once, and the count
// folds two spellings of one source instant.
func TestReader_CountsAOneOnceByItsCanonicalSource(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Event", "at": canonicalStamp, "HOST": map[string]any{"_target_id": "c1"}},
		map[string]any{"$type": "Event", "at": "2021-01-01T00:00:00Z", "HOST": map[string]any{"_target_id": "c9"}},
	)
	respelled := editOnce(t, doc, `"source_key":["2021-01-01T00:00:00Z"]`, `"source_key":["`+rawStamp+`"]`)
	for surface, res := range readBoth(t, s, respelled) {
		n := 0
		for issue := range res.Issues() {
			if issue.Code() == diag.E_GRAPH_CARDINALITY {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d E_GRAPH_CARDINALITY, want one: %s", surface, n, res)
		}
	}
}

// A required association given an empty list draws an "empty" record, which
// names no target and so has no target key to judge.
func TestReader_AcceptsAnEmptyRecord(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Person", "id": "p0", "BOSS": map[string]any{"_target_id": "p0"}, "FRIENDS": []any{}},
	)
	if !strings.Contains(doc, `"reason":"empty"`) {
		t.Fatalf("fixture is vacuous: no empty record in %s", doc)
	}
	for surface, res := range readBoth(t, s, doc) {
		if res.HasErrors() {
			t.Errorf("%s refused an empty record Graph.Add made: %s", surface, res)
		}
	}
}

// A root duplicate's type must be able to hold a root, and the refusal names
// the duplicate record, not an instances entry.
func TestReader_RefusesARootDuplicateOfAnIneligibleType(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{
			"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p1"}, "FRIENDS": []any{map[string]any{"_target_id": "p1"}},
			"TAGS": []any{map[string]any{"label": "a"}},
		},
		map[string]any{"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p1"}, "FRIENDS": []any{map[string]any{"_target_id": "p1"}}},
	)
	person, tag := tableRow(t, doc, "Person"), tableRow(t, doc, "Tag")
	edited := editOnce(t, doc, fmt.Sprintf(`"duplicates":[{"type":%d`, person), fmt.Sprintf(`"duplicates":[{"type":%d`, tag))
	requireReaderRule(t, "a part type", readBoth(t, s, edited), diag.E_SNAPSHOT_INVALID_ROOT, "duplicate record 0 denotes")
}

// A composed child under a name the type does not declare as a composition is
// refused by Verify as well as by Load.
func TestReader_RefusesChildrenUnderAnUndeclaredComposition(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{
			"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p1"}, "FRIENDS": []any{map[string]any{"_target_id": "p1"}},
			"TAGS": []any{map[string]any{"label": "a"}},
		},
	)
	requireReaderRule(t, "an undeclared name", readBoth(t, s, editOnce(t, doc, `"TAGS":[`, `"NOPE":[`)),
		diag.E_SNAPSHOT_INVALID_COMPOSED, `under "NOPE", which the type does not declare as a composition`)
}

// W_SNAPSHOT_UNRESOLVED_REQUIRED follows the schema, whatever the document's
// own required flag says.
func TestReader_TakesRequiredFromTheSchema(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := personDoc(t, s)
	count := func(d string) int {
		_, res := snapshot.Load(context.Background(), []byte(d), s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Warning))
		n := 0
		for issue := range res.Issues() {
			if issue.Code() == diag.W_SNAPSHOT_UNRESOLVED_REQUIRED {
				n++
			}
		}
		return n
	}
	flipped := strings.ReplaceAll(doc, `"required":false`, `"required":true`)
	if flipped == doc {
		t.Fatal("fixture is vacuous: no optional record to flip")
	}
	if a, b := count(doc), count(flipped); a != 0 || b != 0 {
		t.Errorf("optional records warned %d times as written and %d times flagged required, want none", a, b)
	}
}

// A type breaking two root rules draws the rule Graph.Add names first: a
// keyless abstract type is keyless.
func TestReader_JudgesARootTypeInAddsOrder(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"o\"\n\nabstract type A {\n\tname String\n}\n\ntype B {\n\tid String primary\n}\n", "o.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	doc := readerDoc(t, s, map[string]any{"$type": "B", "id": "b1"})
	got := readBoth(t, s, editOnce(t, doc, `"name":"B"`, `"name":"A"`))
	requireReaderRule(t, "a keyless abstract type", got, diag.E_SNAPSHOT_INVALID_ROOT, "which declares no primary key")
	for surface, res := range got {
		if strings.Contains(res.String(), "is abstract") {
			t.Errorf("%s named the abstract rule Graph.Add checks after the key: %s", surface, res)
		}
	}
}

// A stored key or a record's target key whose component is a JSON array or
// object, or a number no float64 holds, is one graph.ParseKey cannot read back. Every read refuses it, the
// schema-less Info included.
func TestReader_RefusesAKeyComponentParseKeyCannotRead(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := personDoc(t, s)
	for _, c := range []struct{ name, from, to, phrase string }{
		{"an instance key", `"key":["c1"]`, `"key":[["c1"]]`, "whose component 0 is not a scalar"},
		{"a record's target key", `"target_key":["c9"]`, `"target_key":[{"id":"c9"}]`, "whose component 0 is not a scalar"},
		{"a number no float64 holds", `"key":["c1"]`, `"key":[1e400]`, "whose component 0 is not a scalar"},
	} {
		edited := editOnce(t, doc, c.from, c.to)
		requireReaderRule(t, c.name, readBoth(t, s, edited), diag.E_SNAPSHOT_MALFORMED, c.phrase)
		_, info := snapshot.Info(t.Context(), []byte(edited))
		if !info.HasCode(diag.E_SNAPSHOT_MALFORMED) || !strings.Contains(info.String(), c.phrase) {
			t.Errorf("%s, Info: %s, want the component refused", c.name, info)
		}
	}
}

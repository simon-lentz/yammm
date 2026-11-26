package snapshot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const storedNullSchema = `schema "sn"

type U {
	id String primary
}

type T {
	id String primary
	s String
	n Integer
	--> TO_U (_:one) U { w String }
}
`

// storedNullDoc returns a document whose T holds s and n and an edge to U
// carrying w, each of which a test spells null.
func storedNullDoc(t *testing.T) (schema.Schema, string) {
	t.Helper()
	s, err := schema.LoadString(t.Context(), storedNullSchema, "sn.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	doc := readerDoc(t, s,
		map[string]any{"$type": "U", "id": "u1"},
		map[string]any{"$type": "T", "id": "t1", "s": "x", "n": int64(7), "to_u": map[string]any{"_target_id": "u1", "w": "y"}},
	)
	return s, doc
}

// TestUnmarshal_AStoredNullIsTheMemberLeftOut holds the reader to the
// validator's reading of absence: a property or edge property stored as null
// is read as the member left out, as a null created_at or parent_type is, and
// the next Marshal writes no member for it.
func TestUnmarshal_AStoredNullIsTheMemberLeftOut(t *testing.T) {
	t.Parallel()
	s, doc := storedNullDoc(t)
	doc = editOnce(t, doc, `"s":"x"`, `"s":null`)
	doc = editOnce(t, doc, `"n":7`, `"n":null`)
	doc = editOnce(t, doc, `"w":"y"`, `"w":null`)

	snap, err := snapshot.Unmarshal(context.Background(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for inst := range snap.Instances() {
		if inst.TypeName() != "T" {
			continue
		}
		for _, name := range []string{"s", "n"} {
			if v, ok := inst.Properties().Get(name); ok {
				t.Errorf("property %s stored null reads as %#v, want it left out", name, v.Any())
			}
		}
	}
	for e := range snap.Edges() {
		if v, ok := e.Properties().Get("w"); ok {
			t.Errorf("edge property w stored null reads as %#v, want it left out", v.Any())
		}
	}

	out, err := snapshot.Marshal(context.Background(), snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, member := range []string{`"s":`, `"n":`, `"w":`} {
		if strings.Contains(string(out), member) {
			t.Errorf("Marshal writes %s for a member read as left out: %s", member, out)
		}
	}
}

// TestRead_AnUndeclaredNameStoredNullIsRefused holds the name rule before the
// null rule: a name the type or association does not declare is refused
// whatever it holds, null included.
func TestRead_AnUndeclaredNameStoredNullIsRefused(t *testing.T) {
	t.Parallel()
	s, doc := storedNullDoc(t)
	for _, c := range []struct{ name, from, to, phrase string }{
		{"an instance's property", `"s":"x"`, `"s":"x","zzz":null`, `holds property "zzz", which its type does not declare`},
		{"an edge's property", `"w":"y"`, `"w":"y","qq":null`, `"qq"`},
	} {
		requireReaderRule(t, c.name, readBoth(t, s, editOnce(t, doc, c.from, c.to)), diag.E_SNAPSHOT_MALFORMED, c.phrase)
	}
}

// TestRead_AnAbsentRecordWhoseEdgePropertyIsStoredNullCarriesNone holds an
// absent record to the null rule: a property stored null is the member left
// out, so the record carries no edge property.
func TestRead_AnAbsentRecordWhoseEdgePropertyIsStoredNullCarriesNone(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "sa"

type U {
	id String primary
}

type T {
	id String primary
	--> TO_U (one) U { w String }
}
`, "sa.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	doc := readerDoc(t, s, map[string]any{"$type": "T", "id": "t1"})
	if !strings.Contains(doc, `"reason":"absent"`) {
		t.Fatalf("fixture holds no absent record: %s", doc)
	}
	held := editOnce(t, doc, `"reason":"absent"`, `"reason":"absent","properties":{"w":null}`)
	for surface, res := range readBoth(t, s, held) {
		for issue := range res.Errors() {
			t.Errorf("%s: an absent record whose one property is stored null is refused: %s", surface, issue.Message())
		}
	}
	carried := editOnce(t, doc, `"reason":"absent"`, `"reason":"absent","properties":{"w":"v"}`)
	requireReaderRule(t, "an absent record carrying a property", readBoth(t, s, carried), diag.E_SNAPSHOT_MALFORMED, "carries edge properties")
}

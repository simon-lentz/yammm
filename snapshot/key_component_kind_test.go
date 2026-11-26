package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

const keyKindSchema = `schema "kk"

type U {
	id String primary
}

type T {
	id String primary
	--> TO_U (_:one) U
}
`

// TestRead_AKeyComponentStoredAsNoJSONStringIsRefused holds the reader to the
// stored-key rule: every primary-key kind stores text, so a root stored under a
// key whose component is a JSON number or boolean is no root a validator
// built, and the key's property agreeing with it does not excuse it.
func TestRead_AKeyComponentStoredAsNoJSONStringIsRefused(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), keyKindSchema, "kk.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	edged := readerDoc(t, s,
		map[string]any{"$type": "U", "id": "u1"},
		map[string]any{"$type": "T", "id": "t1", "to_u": map[string]any{"_target_id": "u1"}},
	)
	for _, c := range []struct {
		name, doc, from, to, phrase string
	}{
		{"a root's key and its property, a number", edged, `"key":["t1"],"properties":{"id":"t1"}`, `"key":[5],"properties":{"id":5}`, "component 0 as a JSON number"},
		{"a root's key and its property, a boolean", edged, `"key":["t1"],"properties":{"id":"t1"}`, `"key":[true],"properties":{"id":true}`, "component 0 as a JSON boolean"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := editOnce(t, c.doc, c.from, c.to)
			requireReaderRule(t, c.name, readBoth(t, s, doc), diag.E_SNAPSHOT_MALFORMED, c.phrase)
		})
	}
}

// TestRead_ARecordWhoseSourceKeyHoldsAListIsRefused holds an unresolved
// record's source key to the scalars graph.ParseKey reads back, judged before
// any address it would name.
func TestRead_ARecordWhoseSourceKeyHoldsAListIsRefused(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), keyKindSchema, "kk.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	doc := readerDoc(t, s,
		map[string]any{"$type": "T", "id": "t1", "to_u": map[string]any{"_target_id": "u9"}},
	)
	doc = editOnce(t, doc, `"source_key":["t1"]`, `"source_key":[["t1"]]`)
	requireReaderRule(t, "a list at a record's source key", readBoth(t, s, doc), diag.E_SNAPSHOT_MALFORMED,
		`states source key [["t1"]], whose component 0 is not a scalar graph.ParseKey reads back`)
}

// TestRead_AComponentOfACompositeKeyIsJudgedWhereItStands holds the kind rule
// to every component of a key: the first component that is no JSON string is
// the one reported, under its own key property, and a later component's
// disagreement does not replace it.
func TestRead_AComponentOfACompositeKeyIsJudgedWhereItStands(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "kc"

type C {
	a String primary
	b String primary
}
`, "kc.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	doc := readerDoc(t, s, map[string]any{"$type": "C", "a": "x", "b": "y"})
	for _, c := range []struct{ name, from, to, phrase string }{
		{
			"a number at the second component", `"key":["x","y"],"properties":{"a":"x","b":"y"}`,
			`"key":["x",5],"properties":{"a":"x","b":5}`, `holds component 1 as a JSON number; its key property "b" stores text`,
		},
		{
			"a number at the first component before a disagreement", `"key":["x","y"],"properties":{"a":"x","b":"y"}`,
			`"key":[5,"y"],"properties":{"a":5,"b":"z"}`, `holds component 0 as a JSON number; its key property "a" stores text`,
		},
	} {
		requireReaderRule(t, c.name, readBoth(t, s, editOnce(t, doc, c.from, c.to)), diag.E_SNAPSHOT_MALFORMED, c.phrase)
	}
}

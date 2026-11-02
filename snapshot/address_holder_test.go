package snapshot_test

import (
	"context"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestRead_ARootWhoseKeyIsRefusedHoldsNoAddress pins that a root whose stored
// key disagrees with its key property holds no address, so the root whose key
// is sound holds it in either document order: the pair draws the key's one
// issue, never a duplicate key, and the sound root's own breaks still report.
func TestRead_ARootWhoseKeyIsRefusedHoldsNoAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Person", "id": "p0"},
		map[string]any{"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p0"}, "WORKS_AT": map[string]any{"_target_id": "c1"}},
	)
	doc = regexp.MustCompile(`"integrity_hash":"[^"]*"`).ReplaceAllString(doc, `"integrity_hash":""`)
	const twin = `{"key":["p1"],"properties":{"id":"p9"},"provenance":null}`
	before := func(doc string) string {
		return editOnce(t, doc, `{"key":["p1"],"properties":{"id":"p1"}`, twin+`,{"key":["p1"],"properties":{"id":"p1"}`)
	}
	after := func(doc string) string {
		return editOnce(t, doc, `"provenance":null}]}],"diagnostics"`, `"provenance":null},`+twin+`]}],"diagnostics"`)
	}
	overflow := editOnce(t, doc, `"WORKS_AT":[{"target_type":0,"target_key":["c1"],"properties":{}}]`,
		`"WORKS_AT":[{"target_type":0,"target_key":["c1"],"properties":{}},{"target_type":0,"target_key":["c1"],"properties":{}}]`)
	for name, c := range map[string]struct {
		doc  string
		want []diag.Code
	}{
		"the twin before the root":                           {before(doc), []diag.Code{diag.E_SNAPSHOT_MALFORMED}},
		"the twin after the root":                            {after(doc), []diag.Code{diag.E_SNAPSHOT_MALFORMED}},
		"the twin before a root whose (one) holds two edges": {before(overflow), []diag.Code{diag.E_SNAPSHOT_MALFORMED, diag.E_GRAPH_CARDINALITY}},
		"the twin after a root whose (one) holds two edges":  {after(overflow), []diag.Code{diag.E_SNAPSHOT_MALFORMED, diag.E_GRAPH_CARDINALITY}},
		// The refused root alone: its edges are counted toward no (one).
		"no twin, the root's own key refused and its (one) of two edges": {
			editOnce(t, overflow, `{"key":["p1"],"properties":{"id":"p1"}`, `{"key":["p1"],"properties":{"id":"p9"}`),
			[]diag.Code{diag.E_SNAPSHOT_MALFORMED},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, l := snapshot.Load(ctx, []byte(c.doc), s)
			for surface, res := range map[string]diag.Result{"Load": l, "Verify": snapshot.Verify(ctx, []byte(c.doc), s)} {
				var got []diag.Code
				for issue := range res.Issues() {
					got = append(got, issue.Code())
				}
				if len(got) != len(c.want) {
					t.Errorf("%s: %s, want %v", surface, res, c.want)
					continue
				}
				for _, code := range c.want {
					if !hasCode(res, code) {
						t.Errorf("%s: %s, want %v", surface, res, c.want)
					}
				}
				if !strings.Contains(res.String(), `disagrees with property "id"`) {
					t.Errorf("%s: %s names no key break", surface, res)
				}
			}
		})
	}
}

// TestRead_ARootOfAKeylessTypeHoldsNoAddress pins that roots of a type that
// declares no primary key hold no address: the group draws its type's one
// E_SNAPSHOT_INVALID_ROOT, and no duplicate key for the roots that share the
// empty key.
func TestRead_ARootOfAKeylessTypeHoldsNoAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s, map[string]any{"$type": "Company", "id": "c1"})
	doc = regexp.MustCompile(`"integrity_hash":"[^"]*"`).ReplaceAllString(doc, `"integrity_hash":""`)
	doc = editOnce(t, doc, `{"schema":"rd","name":"Company"}]`, `{"schema":"rd","name":"Company"},{"schema":"rd","name":"Tag"}]`)
	doc = editOnce(t, doc, `"provenance":null}]}]`, `"provenance":null}]},{"type":1,"items":[`+
		`{"key":null,"properties":{"label":"a"},"provenance":null},{"key":null,"properties":{"label":"b"},"provenance":null},`+
		`{"key":null,"properties":{"label":"c"},"provenance":null}]}]`)
	_, l := snapshot.Load(ctx, []byte(doc), s)
	for surface, res := range map[string]diag.Result{"Load": l, "Verify": snapshot.Verify(ctx, []byte(doc), s)} {
		if res.Len() != 1 || !hasCode(res, diag.E_SNAPSHOT_INVALID_ROOT) {
			t.Errorf("%s: %s, want one %s", surface, res, diag.E_SNAPSHOT_INVALID_ROOT)
		}
	}
}

// Two roots whose stored keys hold a component graph.ParseKey cannot read back
// draw one refusal each: neither key is an address, so neither is a duplicate.
func TestRead_RootsWithUnreadableKeysAreNoDuplicates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s, map[string]any{"$type": "Company", "id": "c1"})
	doc = regexp.MustCompile(`"integrity_hash":"[^"]*"`).ReplaceAllString(doc, `"integrity_hash":""`)
	bad := `{"key":[["x"]],"properties":{"id":"c1"},"provenance":null}`
	doc = editOnce(t, doc, `{"key":["c1"],"properties":{"id":"c1"},"provenance":null}`, bad+`,`+bad)
	_, l := snapshot.Load(ctx, []byte(doc), s)
	_, i := snapshot.Info(ctx, []byte(doc))
	for surface, res := range map[string]diag.Result{"Load": l, "Verify": snapshot.Verify(ctx, []byte(doc), s), "Info": i} {
		if res.Len() != 2 || hasCode(res, diag.E_DUPLICATE_PK) || !strings.Contains(res.String(), "is not a scalar graph.ParseKey reads back") {
			t.Errorf("%s: %s, want two refusals of the key and no %s", surface, res, diag.E_DUPLICATE_PK)
		}
	}
}

// A sound root takes the address from the refused root before it, so the document
// holds the target a record names, and a third root at the address is a duplicate.
func TestRead_ASoundRootHoldsTheAddressARefusedRootStoodAt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Person", "id": "p0", "KNOWS": []any{map[string]any{"_target_id": "p8"}}},
		map[string]any{"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p0"}, "WORKS_AT": map[string]any{"_target_id": "c1"}},
	)
	doc = regexp.MustCompile(`"integrity_hash":"[^"]*"`).ReplaceAllString(doc, `"integrity_hash":""`)
	const (
		root = `{"key":["p1"],"properties":{"id":"p1"}`
		twin = `{"key":["p1"],"properties":{"id":"p9"},"provenance":null},`
	)
	refusedThenSound := editOnce(t, doc, root, twin+root)
	if _, res := snapshot.Load(ctx, []byte(doc), s); res.HasErrors() {
		t.Fatalf("control: %s", res)
	}
	for name, c := range map[string]struct {
		doc  string
		want map[diag.Code]int
	}{
		"refused then sound, a record names it": {
			editOnce(t, refusedThenSound, `"target_key":["p8"]`, `"target_key":["p1"]`),
			map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 2},
		},
		"refused, sound, sound": {
			editOnce(t, refusedThenSound, root, `{"key":["p1"],"properties":{"id":"p1"},"provenance":null},`+root),
			map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1, diag.E_DUPLICATE_PK: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, l := snapshot.Load(ctx, []byte(c.doc), s)
			for surface, res := range map[string]diag.Result{"Load": l, "Verify": snapshot.Verify(ctx, []byte(c.doc), s)} {
				got := map[diag.Code]int{}
				for issue := range res.Issues() {
					got[issue.Code()]++
				}
				if !maps.Equal(got, c.want) {
					t.Errorf("%s: %s, want %v", surface, res, c.want)
				}
			}
		})
	}
}

// A root of a type that cannot hold one still stands at its stored key, so a record
// it states dangles from nowhere: the document draws the type's one refusal.
func TestRead_ARootOfAnIneligibleTypeStandsAtItsKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "ineligible"

abstract type Keyless {
	v String
	--> NEEDS (one) Target
}

type Target {
	id String primary
}
`, "ineligible.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	keyless := mustTypeID(t, s, "Keyless")
	key := immutable.WrapKey([]any{"a"})
	doc := snappartstest.Document(t, s, snapparts.Parts{
		Instances:  []snapparts.Instance{{TypeID: keyless, PrimaryKey: key, Properties: immutable.WrapProperties(map[string]any{"v": "x"})}},
		Unresolved: []snapparts.Unresolved{{SourceType: keyless, SourceKey: key, Relation: "NEEDS", Reason: "absent"}},
	})
	got := snapshot.Verify(ctx, doc, s)
	if got.Len() != 1 || got.HasCode(diag.E_SNAPSHOT_DANGLING_REFERENCE) {
		t.Errorf("Verify: %s, want the type's one refusal", got)
	}
}

// A child's stored key is no root's address, nor is the key its properties state:
// an edge naming that key dangles beside the child's own two breaks.
func TestRead_AChildsStatedKeyIsNoRootsAddress(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "child_key"

part type Item {
	code String primary
}

type Holder {
	id String primary
	*-> ITEMS (_:many) Item
	--> POINTS (_:one) Solid
}

type Solid {
	id String primary
}
`, "child_key.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	holder, solid := mustTypeID(t, s, "Holder"), mustTypeID(t, s, "Solid")
	key := func(k string) immutable.Key { return immutable.WrapKey([]any{k}) }
	doc := snappartstest.Document(t, s, snapparts.Parts{
		Instances: []snapparts.Instance{{
			TypeID: holder, PrimaryKey: key("h1"), Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
			Composed: map[string][]snapparts.Instance{"ITEMS": {{
				TypeID: solid, PrimaryKey: key("stored"), Properties: immutable.WrapProperties(map[string]any{"id": "stated"}),
			}}},
		}},
		Edges: []snapparts.Edge{{Relation: "POINTS", SourceType: holder, SourceKey: key("h1"), TargetKey: key("stated")}},
	})
	got := snapshot.Verify(ctx, doc, s, snapshot.WithIssueLimit(0))
	if !got.HasCode(diag.E_SNAPSHOT_DANGLING_REFERENCE) {
		t.Errorf("Verify: %s, want %s for the edge", got, diag.E_SNAPSHOT_DANGLING_REFERENCE)
	}
}

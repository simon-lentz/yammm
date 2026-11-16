package snapshot_test

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const keyTextSchema = `schema "key_text"

type K {
	id UUID primary
	at Timestamp primary
	--> TO_M (_) M
}

type M {
	id UUID primary
	at Timestamp primary
}

type T {
	id String primary
	--> TO_K (_) K
	*-> PARTS (_:many) P
}

part type P {
	id UUID primary
	at Timestamp primary
}
`

// Each key below is a UUID and a Timestamp, in the text each kind stores.
const (
	rootUUID, rootAt     = "550e8400-e29b-41d4-a716-446655440001", "2020-01-02T03:04:05Z"
	targetUUID, targetAt = "550e8400-e29b-41d4-a716-446655440002", "2021-01-02T03:04:05Z"
	partUUID, partAt     = "550e8400-e29b-41d4-a716-446655440003", "2022-01-02T03:04:05Z"
)

// keyTextInputs are the instances keyTextDocument adds, each key component
// spelled by uuid or at: a K root with a record under TO_M naming a missing M,
// a second K at its address, which is a duplicate, and a T root with an edge
// to the K and a composed P.
func keyTextInputs(uuid, at func(string) string) []struct {
	typ   string
	props map[string]any
	ok    bool
} {
	k := func() map[string]any { return map[string]any{"id": uuid(rootUUID), "at": at(rootAt)} }
	withRecord := k()
	withRecord["to_m"] = map[string]any{"_target_id": uuid(targetUUID), "_target_at": at(targetAt)}
	return []struct {
		typ   string
		props map[string]any
		ok    bool
	}{
		{"K", withRecord, true},
		{"K", k(), false},
		{"T", map[string]any{
			"id":    "t1",
			"to_k":  map[string]any{"_target_id": uuid(rootUUID), "_target_at": at(rootAt)},
			"parts": []any{map[string]any{"id": uuid(partUUID), "at": at(partAt)}},
		}, true},
	}
}

// keyTextDocument marshals the graph keyTextInputs describe, every key spelled
// by uuid or at, and re-signs it. Every key position of the format holds a UUID
// and a Timestamp component.
func keyTextDocument(t *testing.T, uuid, at func(string) string) (schema.Schema, []byte) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), keyTextSchema, "key_text.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	g := graph.New(s)
	for _, in := range keyTextInputs(uuid, at) {
		if r := g.Add(t.Context(), instancetest.Valid(t, s, in.typ, in.props)); r.OK() != in.ok {
			t.Fatalf("add %s: %s", in.typ, r)
		}
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	return s, data
}

func asWritten(s string) string { return s }

// keyPosition is one key the format states, the text that holds it in
// keyTextDocument's document, its UUID and Timestamp components, and the
// issues Info reports when it alone is respelled.
type keyPosition struct {
	name, holder, uuid, at string
	info                   map[diag.Code]int
}

func keyPositions() []keyPosition {
	key := func(uuid, at string) string { return `["` + uuid + `","` + at + `"]` }
	root, target, part := key(rootUUID, rootAt), key(targetUUID, targetAt), key(partUUID, partAt)
	dangling := func(n int) map[diag.Code]int { return map[diag.Code]int{diag.E_SNAPSHOT_DANGLING_REFERENCE: n} }
	return []keyPosition{
		// The edge, the record and the duplicate's conflict each name the root.
		{"a root's key", `"items":[{"key":` + root, rootUUID, rootAt, dangling(3)},
		{"a keyed part's key", `"key":` + part, partUUID, partAt, map[diag.Code]int{}},
		{"an edge's target key", `"target_key":` + root, rootUUID, rootAt, dangling(1)},
		{"a record's source key", `"source_key":` + root, rootUUID, rootAt, dangling(1)},
		{"a record's target key", `"target_key":` + target, targetUUID, targetAt, map[diag.Code]int{}},
		{"a duplicate's key", `"key":` + root + `,"instance"`, rootUUID, rootAt, map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1}},
		{"a duplicate's conflict key", `"key":` + root + `}`, rootUUID, rootAt, dangling(1)},
	}
}

// keySpelling is another spelling of one value its kind reads.
type keySpelling struct {
	name   string
	uuid   bool
	respel func(string) string
}

func keySpellings() []keySpelling {
	return []keySpelling{
		{"UUID in upper case", true, strings.ToUpper},
		{"UUID in braces", true, func(u string) string { return "{" + u + "}" }},
		{"UUID as a urn", true, func(u string) string { return "urn:uuid:" + u }},
		{"UUID as bare hex", true, func(u string) string { return strings.ReplaceAll(u, "-", "") }},
		{"Timestamp at +00:00", false, func(a string) string { return strings.TrimSuffix(a, "Z") + "+00:00" }},
		{"Timestamp at -00:00", false, func(a string) string { return strings.TrimSuffix(a, "Z") + "-00:00" }},
		{"Timestamp with a zero fraction", false, func(a string) string { return strings.TrimSuffix(a, "Z") + ".000Z" }},
	}
}

// respellAt rewrites the one key at p with sp, and re-signs the document.
func respellAt(t *testing.T, doc []byte, p keyPosition, sp keySpelling) []byte {
	t.Helper()
	if n := bytes.Count(doc, []byte(p.holder)); n != 1 {
		t.Fatalf("%s: the document holds %q %d times, want once:\n%s", p.name, p.holder, n, doc)
	}
	component := p.at
	if sp.uuid {
		component = p.uuid
	}
	spelled := sp.respel(component)
	holder := strings.Replace(p.holder, `"`+component+`"`, `"`+spelled+`"`, 1)
	return rehashDocument(t, bytes.Replace(doc, []byte(p.holder), []byte(holder), 1))
}

// respelledText is what the issue for the key at p respelled by sp says: the
// component it names and the text that component's kind stores.
func respelledText(p keyPosition, sp keySpelling) []string {
	index, stored := 1, p.at
	if sp.uuid {
		index, stored = 0, p.uuid
	}
	return []string{fmt.Sprintf("spells component %d in a form", index), "; it stores " + diag.QuoteValue(stored)}
}

// errorCodes returns the codes of the Error and Fatal issues res counts.
func errorCodes(res diag.Result) map[diag.Code]int {
	out := res.CodeCounts(diag.Error)
	for code, n := range res.CodeCounts(diag.Fatal) {
		out[code] += n
	}
	return out
}

// A key component its kind stores as other text is one malformation, at every
// key position the format has: a document holds one spelling of a key, as
// every writer writes it, so Load and Verify read it as no other key. Info
// reads no kind and compares keys as written, so it reports the component only
// where it parts two keys that name one address.
func TestRead_AKeyComponentSpelledOtherwiseThanItsKindStoresIsMalformed(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	for _, p := range keyPositions() {
		for _, sp := range keySpellings() {
			data := respellAt(t, doc, p, sp)
			t.Run(p.name+"/"+sp.name, func(t *testing.T) {
				t.Parallel()
				snap, res := snapshot.Load(t.Context(), data, s)
				if !snap.IsZero() {
					t.Error("Load returned a snapshot")
				}
				want := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1}
				if got := errorCodes(res); !maps.Equal(got, want) {
					t.Errorf("Load: %v, want %v:\n%s", got, want, res)
				}
				for _, says := range respelledText(p, sp) {
					if !strings.Contains(res.String(), says) {
						t.Errorf("Load: no issue says %q:\n%s", says, res)
					}
				}
				if got := errorCodes(snapshot.Verify(t.Context(), data, s)); !maps.Equal(got, want) {
					t.Errorf("Verify: %v, want %v", got, want)
				}
				if _, ires := snapshot.Info(t.Context(), data); !maps.Equal(errorCodes(ires), p.info) {
					t.Errorf("Info: %v, want %v:\n%s", errorCodes(ires), p.info, ires)
				}
			})
		}
	}
}

// A key its kind stores as written is no respelling: a String key holding a
// UUID's text in upper case, and one instant at another offset, which is
// another address.
func TestRead_AKeyItsKindStoresAsWrittenIsNotRespelled(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	upper := strings.ToUpper(rootUUID)
	for _, c := range []struct {
		name     string
		from, to string
		want     map[diag.Code]int
	}{
		{"the document as written", "", "", map[diag.Code]int{}},
		{
			"a String key holding a UUID in upper case",
			`"key":["t1"],"properties":{"id":"t1"}`, `"key":["` + upper + `"],"properties":{"id":"` + upper + `"}`,
			map[diag.Code]int{},
		},
		{
			"an edge naming the root's instant at another offset",
			`"target_key":["` + rootUUID + `","` + rootAt + `"]`, `"target_key":["` + rootUUID + `","2020-01-02T04:04:05+01:00"]`,
			map[diag.Code]int{diag.E_SNAPSHOT_DANGLING_REFERENCE: 1},
		},
	} {
		data := doc
		if c.from != "" {
			if bytes.Count(doc, []byte(c.from)) != 1 {
				t.Fatalf("%s: the document does not hold %q once", c.name, c.from)
			}
			data = rehashDocument(t, bytes.Replace(doc, []byte(c.from), []byte(c.to), 1))
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, res := snapshot.Load(t.Context(), data, s)
			if got := errorCodes(res); !maps.Equal(got, c.want) {
				t.Errorf("Load: %v, want %v:\n%s", got, c.want, res)
			}
			if got := errorCodes(snapshot.Verify(t.Context(), data, s)); !maps.Equal(got, c.want) {
				t.Errorf("Verify: %v, want %v", got, c.want)
			}
		})
	}
}

// Info compares keys as written, since it reads no schema, so it reports no
// edge or record dangling that Load resolves: Load accepts no key its kind
// stores as other text.
func TestInfo_ReportsNoDanglingReferenceLoadResolves(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	accepted := 0
	check := func(name string, data []byte) {
		if _, res := snapshot.Load(t.Context(), data, s); res.HasErrors() {
			return
		}
		accepted++
		if _, res := snapshot.Info(t.Context(), data); res.HasCode(diag.E_SNAPSHOT_DANGLING_REFERENCE) {
			t.Errorf("%s: Load accepts the document and Info reports a dangling reference:\n%s", name, res)
		}
	}
	check("the document as written", doc)
	for _, p := range keyPositions() {
		for _, sp := range keySpellings() {
			data := respellAt(t, doc, p, sp)
			check(p.name+"/"+sp.name, data)
		}
	}
	if accepted == 0 {
		t.Fatal("Load accepted no document, so nothing was compared")
	}
}

// storedKeys matches every key a document states.
var storedKeys = regexp.MustCompile(`"(key|target_key|source_key)":\[[^\]]*\]`)

// keysOf returns every key data states, sorted.
func keysOf(data []byte) []string {
	var out []string
	for _, m := range storedKeys.FindAll(data, -1) {
		out = append(out, string(m))
	}
	slices.Sort(out)
	return out
}

// A document Load accepts keeps every key on a Marshal of what it loaded, so a
// reload addresses what the document addressed.
func TestLoad_AnAcceptedDocumentKeepsEveryKeyOnReMarshal(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	accepted := 0
	check := func(name string, data []byte) {
		if _, res := snapshot.Load(t.Context(), data, s); res.HasErrors() {
			return
		}
		accepted++
		// Revalidation hands the validator its own copy of each key.
		for mode, opts := range map[string][]snapshot.LoadOption{
			"plain":      nil,
			"revalidate": {snapshot.WithRevalidation(diag.Error)},
		} {
			snap, res := snapshot.Load(t.Context(), data, s, opts...)
			if res.HasErrors() {
				t.Fatalf("%s, %s: %s", name, mode, res)
			}
			out, mr := snapshot.Marshal(t.Context(), snap)
			if mr.HasErrors() {
				t.Fatalf("%s, %s: marshal: %s", name, mode, mr)
			}
			if got, want := keysOf(out), keysOf(data); !slices.Equal(got, want) {
				t.Errorf("%s, %s: keys after a reload = %q, want %q", name, mode, got, want)
			}
		}
	}
	check("the document as written", doc)
	for _, p := range keyPositions() {
		for _, sp := range keySpellings() {
			data := respellAt(t, doc, p, sp)
			check(p.name+"/"+sp.name, data)
		}
	}
	if accepted == 0 {
		t.Fatal("Load accepted no document, so nothing was compared")
	}
}

// The writer writes every spelling a validator reads as the text its kind
// stores, at every key position, so a document it wrote holds no key the reader
// refuses.
func TestMarshal_WritesEverySpellingOfAKeyAsItsKindStoresIt(t *testing.T) {
	t.Parallel()
	_, want := keyTextDocument(t, asWritten, asWritten)
	uuids, stamps := keySpellings()[:4], keySpellings()[4:]
	for i := range uuids {
		u, a := uuids[i], stamps[i%len(stamps)]
		s, doc := keyTextDocument(t, u.respel, a.respel)
		if got := keysOf(doc); !slices.Equal(got, keysOf(want)) {
			t.Errorf("%s and %s: keys = %q, want %q", u.name, a.name, got, keysOf(want))
		}
		if _, res := snapshot.Load(t.Context(), doc, s); res.HasErrors() {
			t.Errorf("%s and %s: the reader refuses what the writer wrote: %s", u.name, a.name, res)
		}
	}
}

// A duplicate's conflict key is a key position of its own: a component its
// kind stores as other text there is a malformation beside any other break of
// the record, a parent the record states or another of its keys respelled.
func TestRead_ADuplicatesConflictKeyIsJudgedBesideTheRecordsOtherBreaks(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	key := `["` + rootUUID + `","` + rootAt + `"]`
	upper := `["` + strings.ToUpper(rootUUID) + `","` + rootAt + `"]`
	conflict := [2]string{`"conflict":{"type":0,"key":` + key, `"conflict":{"type":0,"key":` + upper}
	for _, c := range []struct {
		name  string
		edits [][2]string
		want  int
	}{
		{"a relation the record states", [][2]string{{`"duplicates":[{"type":0,`, `"duplicates":[{"type":0,"relation":"PARTS",`}}, 1},
		{"a relation the record states, and the conflict key respelled", [][2]string{{`"duplicates":[{"type":0,`, `"duplicates":[{"type":0,"relation":"PARTS",`}, conflict}, 2},
		{"the instance's stored key respelled, and the conflict key", [][2]string{{`"instance":{"key":` + key, `"instance":{"key":` + upper}, conflict}, 2},
		{"the record's key respelled, and the conflict key", [][2]string{{`"duplicates":[{"type":0,"key":` + key, `"duplicates":[{"type":0,"key":` + upper}, conflict}, 2},
	} {
		data := doc
		for _, e := range c.edits {
			if n := bytes.Count(data, []byte(e[0])); n != 1 {
				t.Fatalf("%s: the document holds %q %d times, want once:\n%s", c.name, e[0], n, data)
			}
			data = bytes.Replace(data, []byte(e[0]), []byte(e[1]), 1)
		}
		data = rehashDocument(t, data)
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: c.want}
			if _, res := snapshot.Load(t.Context(), data, s); !maps.Equal(errorCodes(res), want) {
				t.Errorf("Load: %v, want %v:\n%s", errorCodes(res), want, res)
			}
			if res := snapshot.Verify(t.Context(), data, s); !maps.Equal(errorCodes(res), want) {
				t.Errorf("Verify: %v, want %v:\n%s", errorCodes(res), want, res)
			}
		})
	}
}

// Two children of one keyed (many) slot are compared at their keys as written.
// A child whose key component its kind stores as other text is malformed and
// no second holder of its slot, so only children both written in stored text
// share a key.
func TestRead_TwoChildrenOfOneSlotCompareTheirKeysAsWritten(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	child := func(at string) string {
		return `{"key":["` + partUUID + `","` + at + `"],"type":2,"properties":{"at":"` + at + `","id":"` + partUUID + `"},"provenance":null}`
	}
	offset := strings.TrimSuffix(partAt, "Z") + "+00:00"
	for _, c := range []struct {
		name          string
		first, second string
		want          map[diag.Code]int
	}{
		{"both in stored text", partAt, partAt, map[diag.Code]int{diag.E_DUPLICATE_COMPOSED_PK: 1}},
		{"one at +00:00", partAt, offset, map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1}},
		{"both at +00:00", offset, offset, map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 2}},
	} {
		from := `"PARTS":[` + child(partAt) + `]`
		if n := bytes.Count(doc, []byte(from)); n != 1 {
			t.Fatalf("the document holds %q %d times, want once:\n%s", from, n, doc)
		}
		data := rehashDocument(t, bytes.Replace(doc, []byte(from), []byte(`"PARTS":[`+child(c.first)+`,`+child(c.second)+`]`), 1))
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, res := snapshot.Load(t.Context(), data, s); !maps.Equal(errorCodes(res), c.want) {
				t.Errorf("Load: %v, want %v:\n%s", errorCodes(res), c.want, res)
			}
			if res := snapshot.Verify(t.Context(), data, s); !maps.Equal(errorCodes(res), c.want) {
				t.Errorf("Verify: %v, want %v:\n%s", errorCodes(res), c.want, res)
			}
		})
	}
}

// A respelled key beside a break that stands for it draws that one break: a
// record or duplicate whose row names no type is judged at no type's key, and
// a component graph.ParseKey cannot read back is refused before its
// neighbours. A refused root stands at its key as written and at the address
// its properties state, so a reference to its key in stored text dangles. A
// row two entries reference is one break more, drawn at its first entry.
func TestRead_ARespelledKeyBesideAnotherBreak(t *testing.T) {
	t.Parallel()
	s, doc := keyTextDocument(t, asWritten, asWritten)
	const otherUUID = "550e8400-e29b-41d4-a716-446655440009"
	key := `["` + rootUUID + `","` + rootAt + `"]`
	upper := `["` + strings.ToUpper(rootUUID) + `","` + rootAt + `"]`
	root := func(k, id string) string {
		return `{"key":` + k + `,"properties":{"at":"` + rootAt + `","id":"` + id + `"},"provenance":null}`
	}
	refusedRoot := func(entries string) [2]string {
		return [2]string{`{"type":0,"items":[` + root(key, rootUUID) + `]}`, entries}
	}
	malformed := func(n int) map[diag.Code]int { return map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: n} }
	standsAsWritten := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1, diag.E_SNAPSHOT_DANGLING_REFERENCE: 3}
	for _, c := range []struct {
		name string
		edit [2]string
		want map[diag.Code]int
	}{
		{
			"a duplicate whose row names no type",
			[2]string{`"duplicates":[{"type":0,"key":` + key + `,"instance":{"key":` + key, `"duplicates":[{"type":9,"key":` + upper + `,"instance":{"key":` + upper},
			malformed(1),
		},
		{
			"a record whose source row names no type",
			[2]string{`"source_type":0,"source_key":` + key, `"source_type":9,"source_key":` + upper},
			malformed(1),
		},
		{
			"an edge's target key with an unreadable component",
			[2]string{`"target_key":` + key, `"target_key":[[1],"` + strings.TrimSuffix(rootAt, "Z") + `+00:00"]`},
			malformed(1),
		},
		{
			"a refused root under a row that names no type",
			[2]string{`{"type":0,"items":[` + root(key, rootUUID), `{"type":9,"items":[` + root(upper, otherUUID)},
			standsAsWritten,
		},
		{
			"a refused root in the first of two entries of one row",
			refusedRoot(`{"type":0,"items":[` + root(upper, otherUUID) + `]},{"type":0,"items":[]}`),
			map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 2, diag.E_SNAPSHOT_DANGLING_REFERENCE: 3},
		},
		{
			"a refused root in the second of two entries of one row",
			refusedRoot(`{"type":0,"items":[]},{"type":0,"items":[` + root(upper, otherUUID) + `]}`),
			standsAsWritten,
		},
	} {
		if n := bytes.Count(doc, []byte(c.edit[0])); n != 1 {
			t.Fatalf("%s: the document holds %q %d times, want once:\n%s", c.name, c.edit[0], n, doc)
		}
		data := rehashDocument(t, bytes.Replace(doc, []byte(c.edit[0]), []byte(c.edit[1]), 1))
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, res := snapshot.Load(t.Context(), data, s); !maps.Equal(errorCodes(res), c.want) {
				t.Errorf("Load: %v, want %v:\n%s", errorCodes(res), c.want, res)
			}
			if res := snapshot.Verify(t.Context(), data, s); !maps.Equal(errorCodes(res), c.want) {
				t.Errorf("Verify: %v, want %v:\n%s", errorCodes(res), c.want, res)
			}
		})
	}
}

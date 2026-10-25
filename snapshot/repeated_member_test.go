package snapshot_test

import (
	"context"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"
)

// Marshal never writes an object that repeats a member name, and the reader
// already refuses a repeated top-level section. A repeat anywhere below is
// refused the same way, with the integrity hash blanked as a hand edit leaves
// it, which turns the hash check off under the default options: encoding/json
// would keep the last of two members, or merge two objects under one field.
// Every surface that holds the whole document refuses it, UpdateMetadata among
// them, whose fresh hash would otherwise bless the document. A member of an
// object the reader decodes into a struct, spelled in another case than the
// format declares it, is refused too: encoding/json matches it to the declared
// member.
func TestReader_RefusesAnObjectThatRepeatsAMember(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	doc := readerDoc(t, s,
		map[string]any{"$type": "Company", "id": "c1"},
		map[string]any{"$type": "Person", "id": "p0"},
		map[string]any{"$type": "Person", "id": "p1", "BOSS": map[string]any{"_target_id": "p0"}, "WORKS_AT": map[string]any{"_target_id": "c1"}},
	)
	doc = regexp.MustCompile(`"integrity_hash":"[^"]*"`).ReplaceAllString(doc, `"integrity_hash":""`)
	read := func(doc string) map[string]diag.Result {
		_, l := snapshot.Load(context.Background(), []byte(doc), s)
		_, i := snapshot.Info(context.Background(), []byte(doc))
		_, h := snapshot.HeaderOnly(context.Background(), []byte(doc))
		_, u := snapshot.UpdateMetadata(context.Background(), []byte(doc), map[string]string{"k": "v"})
		return map[string]diag.Result{
			"Load": l, "Verify": snapshot.Verify(context.Background(), []byte(doc), s), "Info": i,
			"HeaderOnly": h, "UpdateMetadata": u,
		}
	}
	for surface, res := range read(doc) {
		if res.HasErrors() {
			t.Fatalf("control, %s: the document with its hash blanked is refused: %s", surface, res)
		}
	}
	for _, c := range []struct{ name, from, to, member string }{
		{"an edges relation", `"WORKS_AT":[`, `"WORKS_AT":[],"WORKS_AT":[`, "WORKS_AT"},
		{"an edges relation spelled with an escape", `"WORKS_AT":[`, `"WORKS_AT":[],"WORKS\u005fAT":[`, "WORKS_AT"},
		{"an instance member spelled with bytes that are not UTF-8", `"key":["p1"]`, "\"key\":[\"p1\"],\"k\xff\":1,\"k\xfe\":2", "k\uFFFD"},
		{"an instance's properties object in another case", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p1"},"Properties":{"id":"p0"}`, "Properties"},
		{"an instance's key spelled with the Kelvin sign", `"key":["p1"]`, "\"key\":[\"p1\"],\"\u212aey\":[\"p0\"]", "\u212aey"},
		{"an edge's target key in another case", `"WORKS_AT":[{"target_type":0,"target_key":["c1"]`, `"WORKS_AT":[{"target_type":0,"target_key":["c1"],"TARGET_KEY":["c9"]`, "TARGET_KEY"},
		{"a property repeating the first of an object of 18 names", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p1","n01":1,"n02":1,"n03":1,"n04":1,"n05":1,"n06":1,"n07":1,"n08":1,"n09":1,"n10":1,"n11":1,"n12":1,"n13":1,"n14":1,"n15":1,"n16":1,"id":"p0"}`, "id"},
		{"a property repeating the ninth of an object of 18 names", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p1","n01":1,"n02":1,"n03":1,"n04":1,"n05":1,"n06":1,"n07":1,"n08":1,"n09":1,"n10":1,"n11":1,"n12":1,"n13":1,"n14":1,"n15":1,"n16":1,"n08":2}`, "n08"},
		{"a property whose name holds an escaped quote", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p1","a\"b":1,"a\u0022b":2}`, `a\"b`},
		{"a header member whose last value breaks a header rule", `"version":4`, `"version":4,"version":5`, "version"},
		{"a header member in another case", `"schema_name":"rd"`, `"schema_name":"rd","Schema_Name":"zz"`, "Schema_Name"},
		{"the edges object", `"edges":{`, `"edges":{},"edges":{`, "edges"},
		{"a property", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p0","id":"p1"}`, "id"},
		{"an instance's properties object", `"key":["p1"],"properties":{"id":"p1"}`, `"key":["p1"],"properties":{"id":"p1"},"properties":{"id":"p1"}`, "properties"},
		{"an instance's key", `"key":["p1"]`, `"key":["p0"],"key":["p1"]`, "key"},
		{"a header member", `"schema_name":"rd"`, `"schema_name":"zz","schema_name":"rd"`, "schema_name"},
		{"a type-table member", `"name":"Company"`, `"name":"Person","name":"Company"`, "name"},
	} {
		edited := editOnce(t, doc, c.from, c.to)
		for surface, res := range read(edited) {
			found := false
			for issue := range res.Issues() {
				if issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), `"`+c.member+`"`) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s, %s: want E_SNAPSHOT_MALFORMED naming %q, got %s", c.name, surface, c.member, res)
			}
		}
	}
}

// A map's keys compare exactly, as encoding/json decodes them: metadata keys
// that differ only in case are three keys, written and read back as such.
func TestReader_KeepsMapKeysThatDifferOnlyInCase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	meta := map[string]string{"key": "a", "Key": "b", "KEY": "c"}
	data, res := snapshot.Marshal(ctx, graph.New(s).Snapshot(), snapshot.WithMetadata(meta))
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	if _, lres := snapshot.Load(ctx, data, s); lres.HasErrors() {
		t.Errorf("Load: %s", lres)
	}
	info, hres := snapshot.HeaderOnly(ctx, data)
	if hres.HasErrors() {
		t.Fatalf("HeaderOnly: %s", hres)
	}
	if !maps.Equal(info.Metadata, meta) {
		t.Errorf("metadata reads back %q, want %q", info.Metadata, meta)
	}
}

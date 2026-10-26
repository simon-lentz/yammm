package snapshot_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Every part below is 40 bytes of ASCII, past the 32 bytes a message repeats.
var (
	lidSchemaName = strings.Repeat("s", 40)
	lidOwnerName  = "Owner" + strings.Repeat("o", 35)
	lidPartName   = "Part" + strings.Repeat("p", 36)
	lidRelation   = "PARTS" + strings.Repeat("r", 35)
	lidReason     = "target_missing" + strings.Repeat("x", 26)
)

// lidCut is the message form of an ASCII text longer than 32 bytes.
func lidCut(s string) string {
	return s[:32] + "… (" + strconv.Itoa(len(s)) + " bytes)"
}

// lidInfoDoc is a document whose types table holds two rows no schema
// resolves, Owner at row 0 and Part at row 1, both under one long schema name.
func lidInfoDoc(instances, duplicates, unresolved string) []byte {
	return fmt.Appendf(nil,
		`{"yammm_snapshot":{"version":%d,"schema_name":"lid","schema_source":"test://lid.yammm","schema_hash":"sha256:00","schema_hash_algorithm":%d,"integrity_hash":"","features":[]},"types":[{"schema":%q,"name":%q},{"schema":%q,"name":%q}],"instances":[%s],"diagnostics":{"duplicates":[%s],"unresolved":[%s]}}`,
		snapshot.MinReadableVersion, schema.StructuralHashVersion,
		lidSchemaName, lidOwnerName, lidSchemaName, lidPartName, instances, duplicates, unresolved)
}

// lidRequire fails unless res holds an issue of code whose message is want, and
// unless no message repeats a long part whole.
func lidRequire(t *testing.T, res diag.Result, code diag.Code, want string) {
	t.Helper()
	found := false
	for issue := range res.Issues() {
		msg := issue.Message()
		for _, whole := range []string{lidSchemaName, lidOwnerName, lidPartName, lidRelation, lidReason} {
			if strings.Contains(msg, whole) {
				t.Errorf("%s repeats %q whole: %s", issue.Code(), whole, msg)
			}
		}
		if issue.Code() == code && msg == want {
			found = true
		}
	}
	if !found {
		t.Errorf("no %s issue reads\n  %s\nin\n%s", code, want, res)
	}
}

func TestInfo_AWrittenTypeIdentityIsCutByItsParts(t *testing.T) {
	owner := lidCut(lidSchemaName) + "#" + lidCut(lidOwnerName)
	part := lidCut(lidSchemaName) + "#" + lidCut(lidPartName)
	rootA := `{"type":0,"items":[{"key":["a"],"properties":{},"provenance":null}]}`
	tests := []struct {
		name                              string
		instances, duplicates, unresolved string
		code                              diag.Code
		want                              string
	}{
		{
			name:      "two groups of one row",
			instances: `{"type":0,"items":[]},{"type":0,"items":[]}`,
			code:      diag.E_SNAPSHOT_MALFORMED,
			want:      "instances entries 0 and 1 both reference types-table row 0 (" + owner + ")",
		},
		{
			name:      "root stating another row",
			instances: `{"type":0,"items":[{"key":["a"],"type":1,"properties":{},"provenance":null}]}`,
			code:      diag.E_SNAPSHOT_TYPE_MISMATCH,
			want:      `root instance ["a"] declares type row 1 (` + part + `) inside the section entry of row 0 (` + owner + `)`,
		},
		{
			name:      "composed child with edges",
			instances: `{"type":0,"items":[{"key":["a"],"properties":{},"composed":{"PARTS":[{"key":["x"],"type":1,"properties":{},"edges":{"E":[{"target_type":0,"target_key":["a"],"properties":{}}]},"provenance":null}]},"provenance":null}]}`,
			code:      diag.E_SNAPSHOT_INVALID_COMPOSED,
			want:      "composed child " + part + " has edges (composed children must not carry edges)",
		},
		{
			name:      "stored key component not a scalar",
			instances: `{"type":0,"items":[{"key":[["a"]],"properties":{},"provenance":null}]}`,
			code:      diag.E_SNAPSHOT_MALFORMED,
			want:      `instance of ` + owner + ` stores key [["a"]], whose component 0 is not a scalar graph.ParseKey reads back`,
		},
		{
			name:       "duplicate instance stating another row",
			instances:  rootA,
			duplicates: `{"type":0,"key":["a"],"instance":{"key":["a"],"type":1,"properties":{},"provenance":null},"conflict":{"type":0,"key":["a"]}}`,
			code:       diag.E_SNAPSHOT_TYPE_MISMATCH,
			want:       `duplicate instance ` + owner + `[["a"]] declares type row 1 (` + part + `) but its record states row 0 (` + owner + `)`,
		},
		{
			name:       "root duplicate whose conflict is absent",
			duplicates: `{"type":0,"key":["z"],"instance":{"key":["z"],"properties":{},"provenance":null},"conflict":{"type":0,"key":["z"]}}`,
			code:       diag.E_SNAPSHOT_DANGLING_REFERENCE,
			want:       `duplicate conflict ` + owner + `[["z"]] references non-existent instance`,
		},
		{
			name:       "composed duplicate whose parent is absent",
			duplicates: `{"type":1,"key":["k"],"instance":{"key":["k"],"properties":{},"provenance":null},"conflict":{"type":1,"key":["k"]},"parent_type":0,"parent_key":["nope"],"relation":"PARTS"}`,
			code:       diag.E_SNAPSHOT_DANGLING_REFERENCE,
			want:       `duplicate parent ` + owner + `[["nope"]] does not resolve to a root instance`,
		},
		{
			name:       "composed duplicate whose conflict is absent",
			instances:  rootA,
			duplicates: fmt.Sprintf(`{"type":1,"key":["k"],"instance":{"key":["k"],"properties":{},"provenance":null},"conflict":{"type":1,"key":["k"]},"parent_type":0,"parent_key":["a"],"relation":%q}`, lidRelation),
			code:       diag.E_SNAPSHOT_DANGLING_REFERENCE,
			want:       `duplicate conflict ` + part + `[["k"]] under ` + owner + `[["a"]].` + lidCut(lidRelation) + ` references non-existent instance`,
		},
		{
			name:       "unresolved record whose source is absent",
			unresolved: `{"source_type":0,"source_key":["nope"],"relation":"R","target_type":1,"target_key":["t"],"required":false,"reason":"target_missing"}`,
			code:       diag.E_SNAPSHOT_DANGLING_REFERENCE,
			want:       `unresolved edge source ` + owner + `[["nope"]] references non-existent instance`,
		},
		{
			name:      "edge target key component not a scalar",
			instances: fmt.Sprintf(`{"type":0,"items":[{"key":["a"],"properties":{},"edges":{%q:[{"target_type":1,"target_key":[["t"]],"properties":{}}]},"provenance":null}]}`, lidRelation),
			code:      diag.E_SNAPSHOT_MALFORMED,
			want:      `edge 0 of ["a"] under "` + lidRelation[:32] + `"… (40 bytes) carries target key [["t"]], whose component 0 is not a scalar graph.ParseKey reads back`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := snapshot.Info(t.Context(), lidInfoDoc(tt.instances, tt.duplicates, tt.unresolved))
			lidRequire(t, res, tt.code, tt.want)
		})
	}
}

const lidLoadSchema = `schema "lid"

type Company {
	id String primary
}

type Person {
	id String primary
	--> EMPLOYER (one) Company
}
`

func TestLoad_ANameTheDocumentWritesIsCut(t *testing.T) {
	s, res := schema.LoadString(t.Context(), lidLoadSchema, "lid.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	doc := func(duplicates, unresolved string) []byte {
		return fmt.Appendf(nil,
			`{"yammm_snapshot":{"version":%d,"schema_name":"lid","schema_source":"test://lid.yammm","schema_hash":%q,"schema_hash_algorithm":%d,"integrity_hash":"","features":[]},"types":[{"schema":"lid","name":"Company"},{"schema":"lid","name":"Person"}],"instances":[{"type":0,"items":[{"key":["c1"],"properties":{"id":"c1"},"provenance":null}]},{"type":1,"items":[{"key":["p1"],"properties":{"id":"p1"},"edges":{"EMPLOYER":[{"target_type":0,"target_key":["c1"],"properties":{}}]},"provenance":null}]}],"diagnostics":{"duplicates":[%s],"unresolved":[%s]}}`,
			snapshot.MinReadableVersion, schema.StructuralHash(s), schema.StructuralHashVersion, duplicates, unresolved)
	}
	tests := []struct {
		name                   string
		duplicates, unresolved string
		code                   diag.Code
		want                   string
	}{
		{
			name:       "duplicate under a relation the parent does not declare",
			duplicates: fmt.Sprintf(`{"type":0,"key":["c1"],"instance":{"key":["c1"],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":0,"key":["c1"]},"parent_type":1,"parent_key":["p1"],"relation":%q}`, lidRelation),
			code:       diag.E_SNAPSHOT_INVALID_COMPOSED,
			want:       `duplicate record 0 names "` + lidRelation[:32] + `"… (40 bytes), which lid#Person does not declare as a composition`,
		},
		{
			name:       "required record stating an undocumented reason",
			unresolved: fmt.Sprintf(`{"source_type":1,"source_key":["p1"],"relation":"EMPLOYER","target_type":0,"target_key":null,"required":true,"reason":%q}`, lidReason),
			code:       diag.W_SNAPSHOT_UNRESOLVED_REQUIRED,
			want:       `required association "EMPLOYER" of lid#Person[["p1"]] is unresolved (` + lidCut(lidReason) + `)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := snapshot.Load(t.Context(), doc(tt.duplicates, tt.unresolved), s, snapshot.WithRevalidation(diag.Warning))
			lidRequire(t, res, tt.code, tt.want)
		})
	}
}

// A name the schema declares is shown whole, however long: only a name the
// document writes is cut.
func TestLoad_ADeclaredAssociationNameIsWhole(t *testing.T) {
	declared := "EMPLOYER_" + strings.Repeat("E", 31)
	s, res := schema.LoadString(t.Context(), strings.Replace(lidLoadSchema, "EMPLOYER", declared, 1), "lid.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	doc := func(targetKey string) []byte {
		return fmt.Appendf(nil,
			`{"yammm_snapshot":{"version":%d,"schema_name":"lid","schema_source":"test://lid.yammm","schema_hash":%q,"schema_hash_algorithm":%d,"integrity_hash":"","features":[]},"types":[{"schema":"lid","name":"Company"},{"schema":"lid","name":"Person"}],"instances":[{"type":0,"items":[{"key":["c1"],"properties":{"id":"c1"},"provenance":null}]},{"type":1,"items":[{"key":["p1"],"properties":{"id":"p1"},"edges":{%q:[{"target_type":0,"target_key":%s,"properties":{}}]},"provenance":null}]}],"diagnostics":{"duplicates":[],"unresolved":[]}}`,
			snapshot.MinReadableVersion, schema.StructuralHash(s), schema.StructuralHashVersion, declared, targetKey)
	}
	for _, tt := range []struct {
		name, targetKey, want string
	}{
		{"a target key of another arity", `["c1","x"]`, `edge 0 of lid#Person[["p1"]] under "` + declared + `" carries a 2-part target key; the target declares 1`},
		{"a target key component that is not a scalar", `[["c1"]]`, `edge 0 of ["p1"] under "` + declared + `" carries target key [["c1"]], whose component 0 is not a scalar graph.ParseKey reads back`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, res := snapshot.Load(t.Context(), doc(tt.targetKey), s)
			for issue := range res.Issues() {
				if issue.Code() == diag.E_SNAPSHOT_MALFORMED && issue.Message() == tt.want {
					return
				}
			}
			t.Errorf("no E_SNAPSHOT_MALFORMED issue reads\n  %s\nin\n%s", tt.want, res)
		})
	}
}

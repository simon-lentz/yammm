package snapshot_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// malformedMessages returns the message of every E_SNAPSHOT_MALFORMED issue.
func malformedMessages(result diag.Result) []string {
	var out []string
	for issue := range result.Issues() {
		if issue.Code() == diag.E_SNAPSHOT_MALFORMED {
			out = append(out, issue.Message())
		}
	}
	return out
}

// TestReaders_AFifthTopLevelKeyIsCut pins that every reader holding the whole
// document cuts a fifth top-level key the document wrote, as it cuts any
// value the data writes.
func TestReaders_AFifthTopLevelKeyIsCut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	key := strings.Repeat("k", 40)
	doc := spliceAfter(t, data, `,"`+key+`":1`)
	want := `document carries a fifth top-level key "` + strings.Repeat("k", 32) + `"… (40 bytes); the format has exactly 4`

	readers := []struct {
		name string
		read func() diag.Result
	}{
		{"Load", func() diag.Result { _, r := snapshot.Load(ctx, doc, s); return r }},
		{"Verify", func() diag.Result { return snapshot.Verify(ctx, doc, s) }},
		{"Info", func() diag.Result { _, r := snapshot.Info(ctx, doc); return r }},
	}
	for _, rd := range readers {
		t.Run(rd.name, func(t *testing.T) {
			t.Parallel()
			msgs := malformedMessages(rd.read())
			if len(msgs) != 1 || msgs[0] != want {
				t.Fatalf("E_SNAPSHOT_MALFORMED messages = %q, want [%q]", msgs, want)
			}
		})
	}
}

// longNamedSchema declares one type whose name is longer than a value a
// message shows whole.
func longNamedSchema(t *testing.T, typeName string) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://long_named.yammm")).
		AddType(typeName).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("longNamedSchema: %s", result)
	}
	return s
}

// TestLoad_ARepeatedTypesRowIsShownWholeOnlyWhereTheSchemaDeclaresIt pins
// the repeated-row message's two renderings: a row the schema resolves names
// a declared type and is shown whole however long its name, and a row it does
// not resolve is the document's text and its type name is cut.
func TestLoad_ARepeatedTypesRowIsShownWholeOnlyWhereTheSchemaDeclaresIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	declared := "Account" + strings.Repeat("Ledger", 6)
	s := longNamedSchema(t, declared)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, declared, []any{"a1"}, map[string]any{"id": "a1"}))
	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	row := `{"schema":"test","name":"` + declared + `"}`
	if !bytes.Contains(data, []byte(`"types":[`+row+`]`)) {
		t.Fatalf("fixture shape changed; types section is not [%s]:\n%s", row, data)
	}

	undeclared := "Unknown" + strings.Repeat("Ledger", 6)
	cases := []struct {
		name  string
		types string
		want  string
	}{
		{
			name:  "declared",
			types: row + "," + row,
			want:  "types table rows 0 and 1 carry one identity test#" + declared,
		},
		{
			name:  "undeclared",
			types: `{"schema":"test","name":"` + undeclared + `"},{"schema":"test","name":"` + undeclared + `"}`,
			want:  "types table rows 0 and 1 carry one identity test#" + undeclared[:32] + "… (43 bytes)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := bytes.Replace(data, []byte(`"types":[`+row+`]`), []byte(`"types":[`+tc.types+`]`), 1)
			_, result := snapshot.Load(ctx, doc, s, snapshot.WithIntegrityCheck(false))
			msgs := malformedMessages(result)
			if !slices.Contains(msgs, tc.want) {
				t.Fatalf("E_SNAPSHOT_MALFORMED messages = %q, want one %q", msgs, tc.want)
			}
		})
	}
}

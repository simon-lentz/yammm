package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
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
	data, err := snapshot.Marshal(ctx, snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	key := strings.Repeat("k", 40)
	doc := spliceAfter(t, data, `,"`+key+`":1`)
	want := `document carries a fifth top-level key "` + strings.Repeat("k", 32) + `"… (40 bytes); the format has exactly 4`

	readers := []struct {
		name string
		read func() diag.Result
	}{
		{"Load", func() diag.Result {
			_, r := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(ctx, doc, s, snapshot.WithWarnings(warn))
			})
			return r
		}},
		{"Verify", func() diag.Result {
			return diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
				return snapshot.Verify(ctx, doc, s, snapshot.WithWarnings(warn))
			})
		}},
		{"Info", func() diag.Result {
			_, r := diagtest.Issues(t, func() (*snapshot.Info, error) {
				return snapshot.Stat(ctx, doc)
			})
			return r
		}},
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
func longNamedSchema(t *testing.T, typeName string) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), fmt.Sprintf(`schema "test"

type %s {
	id String primary
}
`, typeName), "long_named.yammm")
	if err != nil {
		t.Fatalf("longNamedSchema: %s", err)
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
	data, err := snapshot.Marshal(ctx, snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
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
			_, result := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
				return snapshot.Unmarshal(ctx, doc, s, snapshot.WithIntegrityCheck(false), snapshot.WithWarnings(warn))
			})
			msgs := malformedMessages(result)
			if !slices.Contains(msgs, tc.want) {
				t.Fatalf("E_SNAPSHOT_MALFORMED messages = %q, want one %q", msgs, tc.want)
			}
		})
	}
}

// A read with no schema has no declaration to show a repeated row whole by, so
// every such reader cuts it, whatever a schema would declare.
func TestSchemalessReaders_ARepeatedTypesRowIsCut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	declared := "Account" + strings.Repeat("Ledger", 6)
	s := longNamedSchema(t, declared)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, declared, []any{"a1"}, map[string]any{"id": "a1"}))
	data, err := snapshot.Marshal(ctx, snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	row := `{"schema":"test","name":"` + declared + `"}`
	if !bytes.Contains(data, []byte(`"types":[`+row+`]`)) {
		t.Fatalf("fixture shape changed; types section is not [%s]:\n%s", row, data)
	}
	doc := bytes.Replace(data, []byte(`"types":[`+row+`]`), []byte(`"types":[`+row+`,`+row+`]`), 1)
	want := "types table rows 0 and 1 carry one identity test#" + declared[:32] + "… (43 bytes)"

	readers := []struct {
		name string
		read func() diag.Result
	}{
		{"Info", func() diag.Result {
			_, r := diagtest.Issues(t, func() (*snapshot.Info, error) {
				return snapshot.Stat(ctx, doc)
			})
			return r
		}},
		{"ReadHeader", func() diag.Result {
			_, r := diagtest.Issues(t, func() (*snapshot.Header, error) {
				return snapshot.ReadHeader(ctx, bytes.NewReader(doc))
			})
			return r
		}},
		{"UpdateMetadata", func() diag.Result {
			_, r := diagtest.Issues(t, func() ([]byte, error) {
				return snapshot.UpdateMetadata(ctx, doc, map[string]string{"k": "v"})
			})
			return r
		}},
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			t.Parallel()
			if msgs := malformedMessages(reader.read()); !slices.Contains(msgs, want) {
				t.Fatalf("E_SNAPSHOT_MALFORMED messages = %q, want one %q", msgs, want)
			}
		})
	}
}

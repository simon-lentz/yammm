package snapshot_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// TestDecode_OneMalformationOneDiagnostic pins that a single defect is reported
// once. The first-key arm was the only one in decodeHeader that both collected
// an issue and returned an error, and every caller wraps a returned error into
// a collected one — so one malformation produced two identical-code issues.
func TestDecode_OneMalformationOneDiagnostic(t *testing.T) {
	t.Parallel()
	s, _ := attWireFixture(t)
	bad := []byte(`{"types":[],"instances":[],"diagnostics":{"duplicates":[],"unresolved":[]}}`)

	_, res := snapshot.Load(t.Context(), bad, s, snapshot.WithIntegrityCheck(false))
	n := 0
	for range res.Issues() {
		n++
	}
	if n != 1 {
		msgs := []string{}
		for iss := range res.Issues() {
			msgs = append(msgs, iss.Code().String()+": "+iss.Message())
		}
		t.Errorf("one malformation produced %d issues:\n  %s", n, strings.Join(msgs, "\n  "))
	}
}

// TestDecode_UnresolvedDuplicateRowDoesNotNameRowZero pins requireRow's own
// promise — a nil or out-of-range reference "never binds to row 0". The
// diagnostics that follow used the sentinel, so a record naming no type
// reported the type sitting in row 0 instead.
func TestDecode_UnresolvedDuplicateRowDoesNotNameRowZero(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	dup := `"duplicates":[{"type":null,"key":["x"],` +
		`"instance":{"key":["x"],"properties":{},"edges":{"NOPE":[{"target_type":0,"target_key":["x"],"properties":{}}]},"provenance":null},` +
		`"conflict":{"type":0,"key":["x"]}}]`
	edited := strings.Replace(string(data), `"duplicates":[]`, dup, 1)
	if edited == string(data) {
		t.Skipf("fixture carries no empty duplicates array: %s", data)
	}

	_, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
	for iss := range res.Issues() {
		if iss.Code() != diag.E_SNAPSHOT_EDGES_ON_DUPLICATE {
			continue
		}
		if !strings.Contains(iss.Message(), "(no types-table row)") {
			t.Errorf("a duplicate naming no type reported: %s", iss.Message())
		}
		for _, d := range iss.Details() {
			if d.Key == diag.DetailKeyTypeName {
				t.Errorf("an unresolved row still carried a type detail: %q", d.Value)
			}
		}
	}
}

// TestDecode_ARepeatedUnknownRowIsUnknownOnce pins that a repeated types row
// is judged once: the repeat is malformed, and the identity no schema declares
// draws one E_SNAPSHOT_UNKNOWN_TYPE, not one per row.
func TestDecode_ARepeatedUnknownRowIsUnknownOnce(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)
	const row = `{"schema":"attwire","name":"Thing"}`
	gone := `{"schema":"attwire","name":"Gone"}`
	edited := strings.Replace(string(data), row, row+","+gone+","+gone, 1)
	if edited == string(data) {
		t.Fatalf("fixture carries no %s row: %s", row, data)
	}

	for _, read := range []struct {
		name string
		res  diag.Result
	}{
		{"Load", func() diag.Result {
			_, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
			return res
		}()},
		{"Verify", snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))},
	} {
		counts := map[diag.Code]int{}
		for iss := range read.res.Issues() {
			counts[iss.Code()]++
		}
		if counts[diag.E_SNAPSHOT_MALFORMED] != 1 || counts[diag.E_SNAPSHOT_UNKNOWN_TYPE] != 1 {
			t.Errorf("%s: a repeated unknown row drew %v, want one E_SNAPSHOT_MALFORMED and one E_SNAPSHOT_UNKNOWN_TYPE:\n%s",
				read.name, counts, read.res.String())
		}
	}
}

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

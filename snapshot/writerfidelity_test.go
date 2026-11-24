package snapshot_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// The writer states what the snapshot holds and mints nothing of its own.
// These tests pin the three positions where it did otherwise.

// TestWriter_RefusesNonWhitespaceIndent pins that Marshal cannot report success
// while emitting bytes its own Load refuses. The indent is written between JSON
// tokens, so a non-whitespace one produces a document that is not JSON.
func TestWriter_RefusesNonWhitespaceIndent(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)
	snap, err := snapshot.Unmarshal(t.Context(), data, s)
	if err != nil {
		t.Fatalf("load fixture: %s", err)
	}

	// A vertical tab, a form feed and a no-break space are whitespace to Go and
	// not to JSON, which allows only the four the control uses.
	for _, bad := range []string{"xx", "\v", "\f", "\u00a0"} {
		out, err := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent(bad))
		if !errors.Is(err, diag.E_SNAPSHOT_MALFORMED) {
			t.Errorf("Marshal accepted the indent %q and returned %d bytes: %s", bad, len(out), err)
		}
		if out != nil {
			t.Errorf("Marshal returned bytes alongside its refusal of %q", bad)
		}
	}

	// The control: JSON whitespace indents still marshal, and still parse.
	for _, ok := range []string{"", "  ", "\t", "\n", "\r\n"} {
		got, err := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent(ok))
		if err != nil {
			t.Errorf("indent %q refused: %s", ok, err)
			continue
		}
		if !json.Valid(got) {
			t.Errorf("indent %q produced invalid JSON", ok)
		}
	}
}

// TestWriter_CanonicalizesTheKeyLikeItsProperty pins one spelling per value.
// The key is the address — edge targets, duplicate coordinates and an adapter's
// merge key read it — so a key in the caller's form beside a canonicalized
// property gave one entity two addresses.
func TestWriter_CanonicalizesTheKeyLikeItsProperty(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), `schema "wk"

type Doc {
	uid UUID primary
	other UUID
}
`, "wk.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	const upper = "3F2504E0-4F89-11D3-9A0C-0305E82C3301"
	const lower = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	snap := snapshottest.BuildSnapshot(t, s, instancetest.Valid(t, s, "Doc", map[string]any{"uid": upper, "other": upper}))
	data, readErr := snapshot.Marshal(t.Context(), snap)
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	doc := string(data)

	if strings.Contains(doc, upper) {
		t.Errorf("the document carries the un-canonicalized spelling %s:\n%s", upper, doc)
	}
	if !strings.Contains(doc, `"key":["`+lower+`"]`) {
		t.Errorf("the key is not canonicalized:\n%s", doc)
	}
	if !strings.Contains(doc, `"uid":"`+lower+`"`) {
		t.Errorf("the backing property is not canonicalized:\n%s", doc)
	}
}

// TestWriter_PreservesAProvenancePathAsWritten pins that a path the document
// carried is not replaced by one the writer mints: a path is held as text, so
// one the path methods would never write, the empty one among them, reads
// clean and is written back byte for byte.
func TestWriter_PreservesAProvenancePathAsWritten(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	for _, written := range []string{"", "not a path [", `$["Item"]`, "$[ 0 ]"} {
		quoted := strconv.Quote(written)
		edited := strings.Replace(string(data), `"provenance":null`,
			`"provenance":{"source_name":"s","path":`+quoted+`}`, 1)
		if edited == string(data) {
			t.Skipf("fixture carries no null provenance: %s", data)
		}

		snap, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
			return snapshot.Unmarshal(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false), snapshot.WithWarnings(warn))
		})
		if res.Len() != 0 {
			t.Errorf("path %s: the read reported %s; a path as written is no finding", quoted, res)
		}
		again, err := snapshot.Marshal(t.Context(), snap)
		if err != nil {
			t.Fatalf("marshal: %s", err)
		}
		if !strings.Contains(string(again), `"path":`+quoted) {
			t.Errorf("the path %s was not written back:\n%s", quoted, again)
		}
	}
}

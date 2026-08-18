package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Path parity.
//
// yammm reads a .ys document through four surfaces that see different amounts
// of it: Load and Verify run the whole pipeline, Info runs the same structural
// validation and stops before materialization, and UpdateMetadata rewrites the
// header without decoding the body. A rule added to one of them and not to its
// twins is the defect class this file exists to catch — most sharply when the
// surface that misses the rule is the one that WRITES, because its output
// carries a fresh integrity hash and a document no reader accepts then
// verifies.
//
// These tests pin agreement rather than any particular verdict. A rule added
// on one path turns them red on the paths that did not get it.

// visibility says which surfaces can see a defect from what they read.
type visibility int

const (
	// fromShape means the defect is in the header, the types table, or the
	// top-level key sequence — every surface holding the whole document can see
	// it: Load, Verify, Info, HeaderOnly and UpdateMetadata. HeaderOnlyRead and
	// ScanDir are excluded by construction, not by omission: they read through a
	// reader capped at MaxHeaderSize and never hold the body.
	fromShape visibility = iota
	// fromBody means the defect needs the instances or diagnostics section
	// decoded, so UpdateMetadata cannot see it and is not asked to.
	fromBody
)

// withoutIntegrityHash blanks the stored hash so every surface sees the same
// bytes and the structural verdict is what the test observes. Info takes no
// skip-integrity option, so blanking is the only way to compare all four.
func withoutIntegrityHash(t *testing.T, data []byte) []byte {
	t.Helper()
	const anchor = `"integrity_hash":"`
	start := bytes.Index(data, []byte(anchor))
	if start < 0 {
		t.Fatalf("fixture shape changed; %s not found", anchor)
	}
	valueStart := start + len(anchor)
	end := bytes.IndexByte(data[valueStart:], '"')
	if end < 0 {
		t.Fatalf("fixture shape changed; integrity_hash value is unterminated")
	}
	out := append([]byte{}, data[:valueStart]...)
	return append(out, data[valueStart+end:]...)
}

// spliceAfter inserts text immediately before the document's final brace.
func spliceAfter(t *testing.T, data []byte, text string) []byte {
	t.Helper()
	if !bytes.HasSuffix(data, []byte("}")) {
		t.Fatalf("fixture shape changed; document does not end with a brace")
	}
	out := append([]byte{}, data[:len(data)-1]...)
	out = append(out, text...)
	return append(out, '}')
}

// swapSections exchanges the instances and diagnostics sections.
func swapSections(t *testing.T, data []byte) []byte {
	t.Helper()
	is, ie := sectionRange(t, data, "instances")
	ds, de := sectionRange(t, data, "diagnostics")
	out := append([]byte{}, data[:is]...)
	out = append(out, data[ds:de]...)
	out = append(out, data[is:ie]...)
	return append(out, data[de:]...)
}

// TestParity_EverySurfaceAgrees is the parity corpus. Each case is a document
// yammm must refuse, and the assertion is that every surface able to see the
// defect refuses it — not that any one of them does.
func TestParity_EverySurfaceAgrees(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	clean := withoutIntegrityHash(t, v3Doc(t))

	cases := []struct {
		name string
		seen visibility
		edit func(*testing.T, []byte) []byte
	}{
		{"instances absent", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return dropSection(t, d, "instances")
		}},
		{"instances null", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return nullSection(t, d, "instances")
		}},
		{"diagnostics absent", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return dropSection(t, d, "diagnostics")
		}},
		{"diagnostics null", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return nullSection(t, d, "diagnostics")
		}},
		{"sections out of order", fromShape, swapSections},
		{"instances key repeated", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			is, ie := sectionRange(t, d, "instances")
			return spliceAfter(t, d, string(d[is:ie]))
		}},
		{"a fifth top-level key", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return spliceAfter(t, d, `,"extra":1`)
		}},
		{"two groups claim one row", fromBody, func(t *testing.T, d []byte) []byte {
			t.Helper()
			out := bytes.Replace(d, []byte(`{"type":1,`), []byte(`{"type":0,`), 1)
			if bytes.Equal(out, d) {
				t.Fatalf("fixture shape changed; no second group to re-point:\n%s", d)
			}
			return out
		}},
		{"missing only the final brace", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return d[:len(d)-1]
		}},
		{"truncated mid-body", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			// json.Decoder.More reports false at the object's closing brace and
			// at end of input alike, so a truncation is exactly what a
			// key-sequence scan misses unless it reads the closing token.
			return d[:len(d)-24]
		}},
		{"trailing bytes after the object", fromShape, func(t *testing.T, d []byte) []byte {
			t.Helper()
			return append(append([]byte{}, d...), []byte(`{"second":1}`)...)
		}},
		{"a group row out of range", fromBody, func(t *testing.T, d []byte) []byte {
			t.Helper()
			out := bytes.Replace(d, []byte(`{"type":0,`), []byte(`{"type":99,`), 1)
			if bytes.Equal(out, d) {
				t.Fatalf("fixture shape changed; no group row to re-point:\n%s", d)
			}
			return out
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edited := tc.edit(t, clean)
			if bytes.Equal(edited, clean) {
				t.Fatalf("edit is vacuous: the document is unchanged")
			}

			verdicts := map[string]bool{
				"Load":   refusedBy(t, func() diag.Result { _, r := snapshot.Load(ctx, edited, s); return r }),
				"Verify": refusedBy(t, func() diag.Result { return snapshot.Verify(ctx, edited, s) }),
				"Info":   refusedBy(t, func() diag.Result { _, r := snapshot.Info(ctx, edited); return r }),
			}
			if tc.seen == fromShape {
				verdicts["UpdateMetadata"] = refusedBy(t, func() diag.Result {
					_, r := snapshot.UpdateMetadata(ctx, edited, nil)
					return r
				})
				verdicts["HeaderOnly"] = refusedBy(t, func() diag.Result {
					_, r := snapshot.HeaderOnly(ctx, edited)
					return r
				})
			}

			var accepted []string
			for name, refused := range verdicts {
				if !refused {
					accepted = append(accepted, name)
				}
			}
			if len(accepted) > 0 {
				t.Errorf("surfaces disagree: %v accepted a document the others refuse — a rule reached one path and not its twin", accepted)
			}
		})
	}
}

// TestParity_EverySurfaceAcceptsAWellFormedDocument is the positive half. It
// is what stops the corpus above from passing on a surface that refuses
// everything.
func TestParity_EverySurfaceAcceptsAWellFormedDocument(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	clean := withoutIntegrityHash(t, v3Doc(t))

	if _, res := snapshot.Load(ctx, clean, s); res.HasErrors() {
		t.Errorf("Load refused a well-formed document: %v", res)
	}
	if res := snapshot.Verify(ctx, clean, s); res.HasErrors() {
		t.Errorf("Verify refused a well-formed document: %v", res)
	}
	if info, res := snapshot.Info(ctx, clean); res.HasErrors() || info == nil {
		t.Errorf("Info refused a well-formed document: %v", res)
	}
	if out, res := snapshot.UpdateMetadata(ctx, clean, map[string]string{"k": "v"}); res.HasErrors() || len(out) == 0 {
		t.Errorf("UpdateMetadata refused a well-formed document: %v", res)
	}
	if h, res := snapshot.HeaderOnly(ctx, clean); res.HasErrors() || h == nil {
		t.Errorf("HeaderOnly refused a well-formed document: %v", res)
	}
}

// refusedBy reports whether a surface returned an Error-severity diagnostic.
func refusedBy(t *testing.T, run func() diag.Result) bool {
	t.Helper()
	return run().HasErrors()
}

// tiedUnresolvedSchema declares a (many) association carrying edge properties,
// so one instance can name one missing target twice and differ only in the
// properties. Those two unresolved records agree on every field an ordering
// built from identity, key and relation can see.
const tiedUnresolvedSchema = `schema "tied"

type Person {
	id String primary
	--> EMPLOYERS (many) Company / staff (many) { since Timestamp }
}

type Company {
	id String primary
}
`

// TestMarshal_IsDeterministicWhenUnresolvedRecordsTie pins Marshal's documented
// determinism against the one input that breaks a non-total ordering.
// Graph.pending is a map, so each Snapshot call collects the unresolved records
// in a fresh iteration order; if any two of them compare equal, the sort leaves
// their positions to that order and the same graph writes different bytes.
// Snapshot is called once per round precisely to redraw that order.
func TestMarshal_IsDeterministicWhenUnresolvedRecordsTie(t *testing.T) {
	ctx := context.Background()
	s, res := schema.LoadString(t.Context(), tiedUnresolvedSchema, "tied.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	v := instance.NewValidator(s)

	g := graph.New(s)
	for i := range 40 {
		raw := instance.RawInstance{Properties: map[string]any{
			"id": fmt.Sprintf("p%02d", i),
			"EMPLOYERS": []any{
				map[string]any{"_target_id": fmt.Sprintf("c%02d", i), "since": "2020-01-01T00:00:00Z"},
				map[string]any{"_target_id": fmt.Sprintf("c%02d", i), "since": "2021-01-01T00:00:00Z"},
			},
		}}
		valid, vres := v.ValidateOne(ctx, "Person", raw)
		if vres.HasErrors() {
			t.Fatalf("validate p%02d: %s", i, vres)
		}
		if r := g.Add(ctx, valid); r.HasErrors() {
			t.Fatalf("add p%02d: %s", i, r)
		}
	}

	if n := len(g.Snapshot().Unresolved()); n != 80 {
		t.Fatalf("fixture is vacuous: %d unresolved records, want 80 tied pairs", n)
	}

	var first []byte
	for round := range 12 {
		data, mres := snapshot.Marshal(ctx, g.Snapshot())
		if mres.HasErrors() {
			t.Fatalf("marshal round %d: %s", round, mres)
		}
		if round == 0 {
			first = data
			continue
		}
		if !bytes.Equal(data, first) {
			t.Fatalf("round %d wrote different bytes from round 0: the unresolved ordering is not total, so map-iteration order reaches the document", round)
		}
	}
}

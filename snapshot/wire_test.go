package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"
)

// corpusEntry is one Marshal output plus a label identifying the shape
// that produced it. The wire-format contract tests iterate every entry
// so a contract violation surfaces on whichever shape first breaks the
// invariant.
type corpusEntry struct {
	label string
	data  []byte
}

// marshalCorpus returns a representative set of Marshal outputs covering
// every Option combination the wire-format contracts must hold across.
// The corpus spans: empty vs populated snapshot × {compact, indent2sp,
// indent-tab} × various CreatedAt / Metadata / indent combinations.
// Small enough to iterate per-test without noticeable runtime impact.
//
// PR-6 adds a third snapshot shape — "unresolved_with_props" — so the
// wire-format contracts are pinned against documents that carry the
// new unresolvedWire.Properties field populated.
func marshalCorpus(t *testing.T) []corpusEntry {
	t.Helper()
	s := testSchema(t)
	emptySnap := buildSnapshot(t, s)
	populatedSnap := buildSnapshot(
		t, s,
		mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}),
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"title": "Acme"}),
	)
	// A snapshot with a cross-batch unresolved edge carrying properties.
	// Exercises the unresolvedWire.Properties field against every
	// Option combination the wire-format tests iterate.
	es := testSchemaWithEdgeProps(t)
	unresolvedWithPropsSnap := buildSnapshot(
		t, es,
		mustValidInstanceWithEdgeProps(t, es, "Person",
			[]any{"p1"}, map[string]any{"name": "Alice"},
			"EMPLOYER", []any{"missing"},
			map[string]any{"role": "Engineer", "since": int64(2020)}),
	)

	fixedTime := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	optionVariants := []struct {
		label string
		opts  []snapshot.Option
	}{
		{"compact", nil},
		{"compact+createdAt", []snapshot.Option{snapshot.WithCreatedAt(fixedTime)}},
		{"compact+metadata", []snapshot.Option{snapshot.WithMetadata(map[string]string{"k": "v"})}},
		{"indent2sp", []snapshot.Option{snapshot.WithIndent("  ")}},
		{"indent2sp+createdAt+metadata", []snapshot.Option{
			snapshot.WithIndent("  "),
			snapshot.WithCreatedAt(fixedTime),
			snapshot.WithMetadata(map[string]string{"k": "v", "phase": "link"}),
		}},
		{"indentTab", []snapshot.Option{snapshot.WithIndent("\t")}},
	}

	ctx := context.Background()
	var corpus []corpusEntry
	for _, snapPair := range []struct {
		name string
		snap graph.Snapshot
	}{
		{"empty", emptySnap},
		{"populated", populatedSnap},
		{"unresolved_with_props", unresolvedWithPropsSnap},
	} {
		for _, v := range optionVariants {
			data, err := snapshot.Marshal(ctx, snapPair.snap, v.opts...)
			require.NoError(t, err, "marshal %s %s", snapPair.name, v.label)
			corpus = append(corpus, corpusEntry{
				label: snapPair.name + "/" + v.label,
				data:  data,
			})
		}
	}
	return corpus
}

// TestWireFormat_TopLevelKeyOrder pins the field-order contract: every
// Marshal-produced document has exactly four top-level keys in the order
// yammm_snapshot → types → instances → diagnostics. Parses via
// json.Decoder.Token() (not json.Unmarshal, which would hide key order
// by decoding into a map).
func TestWireFormat_TopLevelKeyOrder(t *testing.T) {
	corpus := marshalCorpus(t)
	const (
		want0 = "yammm_snapshot"
		want1 = "types"
		want2 = "instances"
		want3 = "diagnostics"
	)
	for _, entry := range corpus {
		t.Run(entry.label, func(t *testing.T) {
			dec := json.NewDecoder(bytes.NewReader(entry.data))
			// Expect opening '{'.
			tok, err := dec.Token()
			require.NoError(t, err, "token opening brace")
			delim, ok := tok.(json.Delim)
			require.True(t, ok && delim == '{', "expected '{', got %v", tok)

			seen := []string{}
			for dec.More() {
				keyTok, err := dec.Token()
				require.NoError(t, err, "key token")
				key, ok := keyTok.(string)
				require.True(t, ok, "expected string key, got %T", keyTok)
				seen = append(seen, key)
				// Skip the value.
				var skip json.RawMessage
				require.NoError(t, dec.Decode(&skip), "skip value for key %q", key)
			}
			require.Len(t, seen, 4, "top-level keys observed: %v", seen)
			require.Equal(t, want0, seen[0], "first key")
			require.Equal(t, want1, seen[1], "second key")
			require.Equal(t, want2, seen[2], "third key")
			require.Equal(t, want3, seen[3], "fourth key")
		})
	}
}

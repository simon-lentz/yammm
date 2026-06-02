package snapshot_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
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
	// Exercises the v2 unresolvedWire.Properties field against every
	// Option combination the wire-format tests iterate.
	unresolvedWithPropsSnap := buildSnapshot(
		t, s,
		mustValidInstanceWithEdgeProps(t, s, "Person",
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
		snap *graph.Snapshot
	}{
		{"empty", emptySnap},
		{"populated", populatedSnap},
		{"unresolved_with_props", unresolvedWithPropsSnap},
	} {
		for _, v := range optionVariants {
			data, res := snapshot.Marshal(ctx, snapPair.snap, v.opts...)
			require.NoError(t, res.Err(), "marshal %s %s", snapPair.name, v.label)
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
			require.Equal(t, 4, len(seen), "top-level keys observed: %v", seen)
			require.Equal(t, want0, seen[0], "first key")
			require.Equal(t, want1, seen[1], "second key")
			require.Equal(t, want2, seen[2], "third key")
			require.Equal(t, want3, seen[3], "fourth key")
		})
	}
}

// TestWireFormat_BodySuffixContract pins the body-byte-range stability
// contract: for every Marshal output, the byte offset immediately after
// the yammm_snapshot header value's closing '}' addresses a ',' byte,
// the document ends with '}', and the suffix beginning at that offset
// matches the expected ,"types"... or ,\n<indent>"types"... pattern.
//
// This mirrors the bodyOffset capture logic UpdateMetadata uses without
// importing the unexported streamDecoder internals — the test parses the
// document directly with a fresh json.Decoder and computes the same
// InputOffset it relies on.
func TestWireFormat_BodySuffixContract(t *testing.T) {
	corpus := marshalCorpus(t)
	for _, entry := range corpus {
		t.Run(entry.label, func(t *testing.T) {
			data := entry.data
			require.GreaterOrEqual(t, len(data), 2, "data too short")
			require.Equal(t, byte('{'), data[0], "must start with '{'")
			require.Equal(t, byte('}'), data[len(data)-1], "must end with '}'")

			dec := json.NewDecoder(bytes.NewReader(data))
			// Opening '{'.
			_, err := dec.Token()
			require.NoError(t, err)
			// First key.
			keyTok, err := dec.Token()
			require.NoError(t, err)
			key, ok := keyTok.(string)
			require.True(t, ok && key == "yammm_snapshot",
				"first key must be yammm_snapshot, got %v", keyTok)
			// Decode the header value as raw bytes (consumes through the
			// closing '}' of the header object).
			var headerRaw json.RawMessage
			require.NoError(t, dec.Decode(&headerRaw))

			bodyOffset := dec.InputOffset()
			require.Greater(t, bodyOffset, int64(0), "bodyOffset must be > 0")
			require.Less(t, bodyOffset, int64(len(data)), "bodyOffset must be < len(data)")
			require.Equal(t, byte(','), data[bodyOffset],
				"byte at bodyOffset must be ',', got %q (data near offset: %q)",
				data[bodyOffset], safeSlice(data, bodyOffset, 20))

			suffix := data[bodyOffset:]
			isIndented := bytes.Contains(data[:bodyOffset], []byte("\n"))
			if isIndented {
				// The suffix begins with ",\n<indent>\"types\": ".
				require.True(t, bytes.HasPrefix(suffix, []byte(",\n")),
					"indented suffix must start with ,\\n; got %q",
					safeSlice(suffix, 0, 20))
				require.Contains(t, string(suffix[:40]), `"types"`,
					"indented suffix must reach the types key quickly")
			} else {
				require.True(t, bytes.HasPrefix(suffix, []byte(`,"types":`)),
					"compact suffix must start with ,\"types\":; got %q",
					safeSlice(suffix, 0, 20))
			}
		})
	}
}

// safeSlice returns a bounds-checked string excerpt of data starting at
// start for up to n bytes, with newlines escaped for readability in
// error messages.
func safeSlice(data []byte, start, n int64) string {
	if start < 0 {
		start = 0
	}
	end := min(start+n, int64(len(data)))
	return strings.ReplaceAll(string(data[start:end]), "\n", "\\n")
}

package snapshot_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// makeSnapshotBytes builds a small populated snapshot marshaled with the
// given options. Returned bytes are independent across calls.
//
//nolint:contextcheck // buildSnapshot is a shared test helper that uses context.Background() internally; threading ctx through would cascade to every snapshot test.
func makeSnapshotBytes(ctx context.Context, t *testing.T, opts ...snapshot.Option) []byte {
	t.Helper()
	s := testSchema(t)
	snap := buildSnapshot(
		t, s,
		mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}),
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"title": "Acme"}),
	)
	data, res := snapshot.Marshal(ctx, snap, opts...)
	require.NoError(t, res.Err(), "Marshal")
	return data
}

func TestUpdateMetadata_HappyPath(t *testing.T) {
	ctx := context.Background()
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"pre": "existing"}))

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link", "pre": "existing"})
	require.NoError(t, res.Err())

	loaded, loadRes := snapshot.HeaderOnly(ctx, out)
	require.NoError(t, loadRes.Err())
	assert.Equal(t, "link", loaded.Metadata["phase"])
	assert.Equal(t, "existing", loaded.Metadata["pre"])
}

func TestUpdateMetadata_Idempotence(t *testing.T) {
	// When newMeta matches the existing header metadata exactly,
	// UpdateMetadata reproduces the input byte-for-byte.
	ctx := context.Background()
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v", "phase": "link"}))
	header, res := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, res.Err())

	out, upRes := snapshot.UpdateMetadata(ctx, data, header.Metadata)
	require.NoError(t, upRes.Err())
	assert.True(t, bytes.Equal(data, out),
		"idempotent UpdateMetadata should produce byte-identical output")
}

func TestUpdateMetadata_CrossCheckVsLoadMarshal(t *testing.T) {
	// UpdateMetadata(data, newMeta) bytes must equal
	// Marshal(Load(data), WithMetadata(newMeta)) bytes for every supported
	// Option combination.
	ctx := context.Background()
	s := testSchema(t)
	fixedTime := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	variants := []struct {
		name string
		opts []snapshot.Option
	}{
		{"compact", nil},
		{"indent2sp", []snapshot.Option{snapshot.WithIndent("  ")}},
		{"indentTab", []snapshot.Option{snapshot.WithIndent("\t")}},
		{"compact+createdAt", []snapshot.Option{snapshot.WithCreatedAt(fixedTime)}},
		{"indent2sp+createdAt", []snapshot.Option{snapshot.WithIndent("  "), snapshot.WithCreatedAt(fixedTime)}},
	}

	newMeta := map[string]string{"phase": "link", "pipeline_completed": "true"}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			original := makeSnapshotBytes(ctx, t, v.opts...)
			fast, res := snapshot.UpdateMetadata(ctx, original, newMeta)
			require.NoError(t, res.Err())

			loaded, loadRes := snapshot.Load(ctx, original, s)
			require.NoError(t, loadRes.Err())
			marshalOpts := append([]snapshot.Option{}, v.opts...)
			marshalOpts = append(marshalOpts, snapshot.WithMetadata(newMeta))
			slow, marshalRes := snapshot.Marshal(ctx, loaded, marshalOpts...)
			require.NoError(t, marshalRes.Err())

			assert.True(t, bytes.Equal(fast, slow),
				"fast/slow divergence for %s:\n  fast len=%d\n  slow len=%d",
				v.name, len(fast), len(slow))
		})
	}
}

func TestUpdateMetadata_IndentPreservation(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		indent string
	}{
		{"compact", ""},
		{"2-space", "  "},
		{"tab", "\t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var opts []snapshot.Option
			if tc.indent != "" {
				opts = []snapshot.Option{snapshot.WithIndent(tc.indent)}
			}
			data := makeSnapshotBytes(ctx, t, opts...)
			out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
			require.NoError(t, res.Err())

			if tc.indent == "" {
				assert.False(t, bytes.Contains(out, []byte("\n")),
					"compact input must round-trip as compact output")
			} else {
				assert.True(t, bytes.Contains(out, []byte("\n"+tc.indent)),
					"indented output must preserve indent unit %q", tc.indent)
			}
		})
	}
}

func TestUpdateMetadata_RoundTripBodyBytesVerbatim(t *testing.T) {
	// Verify the body suffix (everything from the ',' after the header
	// onward) is byte-identical between input and output.
	ctx := context.Background()
	data := makeSnapshotBytes(ctx, t)

	origOffset := bodyOffsetOf(t, data)
	origSuffix := data[origOffset:]

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	require.NoError(t, res.Err())

	outOffset := bodyOffsetOf(t, out)
	outSuffix := out[outOffset:]
	assert.True(t, bytes.Equal(origSuffix, outSuffix),
		"body suffix must be preserved byte-for-byte")
}

func TestUpdateMetadata_HashEquivalence(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t)

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	require.NoError(t, res.Err())

	_, loadRes := snapshot.Load(ctx, out, s)
	require.NoError(t, loadRes.Err(), "Load must accept UpdateMetadata output")
}

func TestUpdateMetadata_CreatedAtPreserved(t *testing.T) {
	ctx := context.Background()
	fixedTime := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	data := makeSnapshotBytes(ctx, t, snapshot.WithCreatedAt(fixedTime))
	origHeader, res := snapshot.HeaderOnly(ctx, data)
	require.NoError(t, res.Err())
	require.NotEmpty(t, origHeader.CreatedAt)

	out, upRes := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	require.NoError(t, upRes.Err())

	outHeader, res := snapshot.HeaderOnly(ctx, out)
	require.NoError(t, res.Err())
	assert.Equal(t, origHeader.CreatedAt, outHeader.CreatedAt,
		"CreatedAt must be preserved byte-for-byte when no override is provided")
}

func TestUpdateMetadata_CreatedAtOverridden(t *testing.T) {
	ctx := context.Background()
	origTime := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	data := makeSnapshotBytes(ctx, t, snapshot.WithCreatedAt(origTime))

	out, res := snapshot.UpdateMetadata(ctx, data,
		map[string]string{"phase": "link"},
		snapshot.WithUpdateCreatedAt(newTime))
	require.NoError(t, res.Err())

	outHeader, headerRes := snapshot.HeaderOnly(ctx, out)
	require.NoError(t, headerRes.Err())
	assert.Equal(t, newTime.Format(time.RFC3339), outHeader.CreatedAt)
}

func TestUpdateMetadata_EmptyMetadata(t *testing.T) {
	ctx := context.Background()

	t.Run("populated->nil", func(t *testing.T) {
		data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v"}))
		out, res := snapshot.UpdateMetadata(ctx, data, nil)
		require.NoError(t, res.Err())
		header, headerRes := snapshot.HeaderOnly(ctx, out)
		require.NoError(t, headerRes.Err())
		assert.Empty(t, header.Metadata, "nil newMeta must clear metadata (omitempty)")
	})

	t.Run("nil->populated", func(t *testing.T) {
		data := makeSnapshotBytes(ctx, t)
		out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
		require.NoError(t, res.Err())
		header, headerRes := snapshot.HeaderOnly(ctx, out)
		require.NoError(t, headerRes.Err())
		assert.Equal(t, "link", header.Metadata["phase"])
	})

	t.Run("emptyMap->omitted", func(t *testing.T) {
		data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v"}))
		out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{})
		require.NoError(t, res.Err())
		header, headerRes := snapshot.HeaderOnly(ctx, out)
		require.NoError(t, headerRes.Err())
		assert.Empty(t, header.Metadata, "empty map must clear metadata (omitempty)")
	})
}

func TestUpdateMetadata_TamperingDetected(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v"}))

	out, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	require.NoError(t, res.Err())

	offset := bodyOffsetOf(t, out) + 10
	require.Less(t, offset, int64(len(out)), "offset in bounds")
	out[offset] ^= 0x01

	_, loadRes := snapshot.Load(ctx, out, s)
	require.True(t, loadRes.HasErrors(), "tampered body should fail Load")
}

func TestUpdateMetadata_MalformedInput(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"just opening brace", []byte("{")},
		{"not json", []byte("not a json document at all")},
		{"missing yammm_snapshot key", []byte(`{"types":["Person"]}`)},
		{"wrong first key", []byte(`{"types":["Person"],"yammm_snapshot":{}}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, res := snapshot.UpdateMetadata(ctx, tc.data, map[string]string{"k": "v"})
			assert.True(t, res.HasErrors(), "malformed input must produce diagnostics")
			assert.True(t, res.HasCode(diag.E_SNAPSHOT_MALFORMED),
				"expected E_SNAPSHOT_MALFORMED, got: %v", res)
		})
	}
}

func TestUpdateMetadata_Cancellation(t *testing.T) {
	data := makeSnapshotBytes(context.Background(), t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, res := snapshot.UpdateMetadata(ctx, data, map[string]string{"phase": "link"})
	require.True(t, res.HasErrors())
	assert.True(t, res.HasCode(diag.E_CONTEXT_CANCELLED))
}

func TestUpdateMetadata_ConcurrentAccess(t *testing.T) {
	// N goroutines calling UpdateMetadata on the same shared input byte
	// slice must produce valid outputs without data races.
	ctx := context.Background()
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v"}))

	// Use a fixed byte array for per-worker payloads (avoids gosec G115 on
	// int→rune conversion).
	workerLabels := []byte{'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H'}
	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	errCh := make(chan error, N)
	for i := range N {
		go func(idx int) {
			defer wg.Done()
			meta := map[string]string{"worker": string(workerLabels[idx : idx+1])}
			out, res := snapshot.UpdateMetadata(ctx, data, meta)
			if res.HasErrors() {
				errCh <- res.Err()
				return
			}
			if len(out) == 0 {
				errCh <- assert.AnError
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent worker error: %v", err)
	}
}

// Each malformed shape pins the exact code its branch draws, so neither
// branch can go dead behind the other.
func TestUpdateMetadata_BodyOffsetFailure(t *testing.T) {
	ctx := context.Background()
	header := fmt.Sprintf(`{"yammm_snapshot":{"version":3,"schema_name":"x","schema_source":"s","schema_hash":"h","schema_hash_algorithm":%d,"integrity_hash":"","features":[]}`, schema.StructuralHashVersion)

	// A document that ends at the header: the decoder rejects it for the
	// missing types key before the body offset is ever consulted.
	_, res := snapshot.UpdateMetadata(ctx, []byte(header+`}`), map[string]string{"k": "v"})
	if !res.HasCode(diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("a document with no types key drew %v, want %s", res, diag.E_SNAPSHOT_MALFORMED)
	}
	if res.HasCode(diag.E_UPDATE_METADATA_BODY_OFFSET) {
		t.Errorf("the body-offset guard fired before the decoder rejected the document: %v", res)
	}

	// A complete document whose byte after the header is not ',': the header
	// parses clean and the body-offset shape check is the branch that fires.
	spaced := header + ` ,"types":[],"instances":[],"diagnostics":{"duplicates":[],"unresolved":[]}}`
	_, res = snapshot.UpdateMetadata(ctx, []byte(spaced), map[string]string{"k": "v"})
	if !res.HasCode(diag.E_UPDATE_METADATA_BODY_OFFSET) {
		t.Errorf("a non-Marshal-shaped body boundary drew %v, want %s", res, diag.E_UPDATE_METADATA_BODY_OFFSET)
	}
}

func TestUpdateMetadata_GoldenCorpus(t *testing.T) {
	// Load every golden .ys in snapshot/testdata/updatemetadata/ and
	// verify UpdateMetadata handles each cleanly.
	ctx := context.Background()
	s := testSchema(t)

	dir := goldenDir(t)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "golden dir must exist")
	require.NotEmpty(t, entries, "golden corpus must have at least one fixture")

	newMeta := map[string]string{"phase": "link"}

	foundAny := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ys") {
			continue
		}
		foundAny = true
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, err)

			out, res := snapshot.UpdateMetadata(ctx, data, newMeta)
			require.NoError(t, res.Err(), "UpdateMetadata on %s", e.Name())

			_, loadRes := snapshot.Load(ctx, out, s)
			require.NoError(t, loadRes.Err(), "Load of UpdateMetadata output for %s", e.Name())

			header, headerRes := snapshot.HeaderOnly(ctx, out)
			require.NoError(t, headerRes.Err())
			assert.Equal(t, "link", header.Metadata["phase"])
		})
	}
	require.True(t, foundAny, "corpus dir must contain at least one .ys fixture")
}

func TestUpdateMetadata_BestEffortIndent(t *testing.T) {
	// A parseable-but-irregular-indent input should not error — it
	// should produce a valid output under best-effort indent detection.
	ctx := context.Background()
	data := makeSnapshotBytes(ctx, t, snapshot.WithIndent("  "))
	// Replace the leading "{\n  \"yammm_snapshot\": " with "{ \"yammm_snapshot\": "
	// (valid JSON, non-standard whitespace).
	mutated := bytes.Replace(data, []byte("{\n  \"yammm_snapshot\": "), []byte("{ \"yammm_snapshot\": "), 1)
	require.NotEqual(t, string(data), string(mutated), "mutation must have landed")

	out, res := snapshot.UpdateMetadata(ctx, mutated, map[string]string{"phase": "link"})
	require.NoError(t, res.Err(), "best-effort should not fail")
	_, headerRes := snapshot.HeaderOnly(ctx, out)
	require.NoError(t, headerRes.Err())
}

func TestUpdateMetadata_CrossMarshalParityProperty(t *testing.T) {
	// Property test: for a curated corpus of snapshots × Option
	// combinations, UpdateMetadata(Marshal(snap, opts...), newMeta) must
	// produce a document that Load accepts and whose metadata matches
	// newMeta.
	ctx := context.Background()
	s := testSchema(t)
	fixedTime := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)

	snaps := []struct {
		name string
		snap *graph.Snapshot
	}{
		{"empty", buildSnapshot(t, s)},
		{"populated", buildSnapshot(
			t, s,
			mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}),
			mustValidInstance(t, s, "Person", []any{"p2"}, map[string]any{"name": "Bob"}),
			mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"title": "Acme"}),
		)},
	}

	optionVariants := [][]snapshot.Option{
		nil,
		{snapshot.WithIndent("  ")},
		{snapshot.WithIndent("\t")},
		{snapshot.WithCreatedAt(fixedTime)},
		{snapshot.WithIndent("  "), snapshot.WithCreatedAt(fixedTime)},
		{snapshot.WithMetadata(map[string]string{"seed": "value"})},
		{snapshot.WithIndent("\t"), snapshot.WithMetadata(map[string]string{"seed": "value"})},
	}

	newMeta := map[string]string{"phase": "link", "pipeline_completed": "true"}

	for _, sn := range snaps {
		for optIdx, opts := range optionVariants {
			label := sn.name + "/opts" + string(rune('0'+optIdx))
			t.Run(label, func(t *testing.T) {
				data, res := snapshot.Marshal(ctx, sn.snap, opts...)
				require.NoError(t, res.Err())

				out, upRes := snapshot.UpdateMetadata(ctx, data, newMeta)
				require.NoError(t, upRes.Err())

				_, loadRes := snapshot.Load(ctx, out, s)
				require.NoError(t, loadRes.Err())

				header, headerRes := snapshot.HeaderOnly(ctx, out)
				require.NoError(t, headerRes.Err())
				assert.Equal(t, "link", header.Metadata["phase"])
				assert.Equal(t, "true", header.Metadata["pipeline_completed"])
			})
		}
	}
}

// -- UpdateMetadataOrReMarshal coverage --

func TestUpdateMetadataOrReMarshal_HappyPathParity(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	data := makeSnapshotBytes(ctx, t, snapshot.WithMetadata(map[string]string{"k": "v"}))
	newMeta := map[string]string{"phase": "link"}

	fast, fastRes := snapshot.UpdateMetadata(ctx, data, newMeta)
	require.NoError(t, fastRes.Err())

	wrapped, wrappedRes := snapshot.UpdateMetadataOrReMarshal(ctx, data, newMeta, s)
	require.NoError(t, wrappedRes.Err())
	assert.False(t, wrappedRes.HasWarnings(), "happy path must not emit fallback warning")
	assert.True(t, bytes.Equal(fast, wrapped),
		"wrapper output must equal primitive output on well-formed input")
}

func TestUpdateMetadataOrReMarshal_FallbackOnMalformed(t *testing.T) {
	// An input with a garbled first key triggers E_SNAPSHOT_MALFORMED
	// on the fast path. Load will also reject this input, so the
	// wrapper's both-paths-failed merge path fires. This is the
	// documented behavior for truly unrecoverable input.
	ctx := context.Background()
	s := testSchema(t)

	data := makeSnapshotBytes(ctx, t)
	bad := bytes.Replace(data, []byte(`{"yammm_snapshot":`), []byte(`{"garbled":`), 1)
	require.NotEqual(t, string(data), string(bad))

	newMeta := map[string]string{"phase": "link"}
	out, res := snapshot.UpdateMetadataOrReMarshal(ctx, bad, newMeta, s)
	// Both paths fail for this input; merged-diagnostics path.
	assert.Nil(t, out, "both paths failed → nil bytes")
	assert.True(t, res.HasErrors(), "merged failure must surface")
}

func TestUpdateMetadataOrReMarshal_BothPathsFailed(t *testing.T) {
	ctx := context.Background()
	s := testSchema(t)
	bad := []byte("this is not json at all")

	out, res := snapshot.UpdateMetadataOrReMarshal(ctx, bad, map[string]string{"k": "v"}, s)
	assert.Nil(t, out, "unrecoverable input must return nil bytes")
	assert.True(t, res.HasErrors(), "error surface must be non-empty")
}

func TestUpdateMetadataOrReMarshal_CancellationPropagates(t *testing.T) {
	// Marshal fixture with a live ctx, then cancel before calling the
	// primitive so only the UpdateMetadata path observes cancellation.
	s := testSchema(t)
	data := makeSnapshotBytes(context.Background(), t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, res := snapshot.UpdateMetadataOrReMarshal(ctx, data, map[string]string{"k": "v"}, s)
	require.True(t, res.HasErrors())
	assert.True(t, res.HasCode(diag.E_CONTEXT_CANCELLED))
	assert.False(t, res.HasWarnings(),
		"cancellation must not fire W_UPDATE_METADATA_FALLBACK")
}

// -- test helpers --

// bodyOffsetOf computes the byte offset of the first byte after the
// yammm_snapshot header value's closing '}'. Uses json.Decoder.InputOffset
// exactly as the implementation does.
func bodyOffsetOf(t *testing.T, data []byte) int64 {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 2)
	require.Equal(t, byte('{'), data[0])
	dec := json.NewDecoder(bytes.NewReader(data))
	_, err := dec.Token() // '{'
	require.NoError(t, err)
	tok, err := dec.Token() // first key
	require.NoError(t, err)
	require.Equal(t, "yammm_snapshot", tok.(string))
	var headerRaw json.RawMessage
	require.NoError(t, dec.Decode(&headerRaw))
	return dec.InputOffset()
}

// goldenDir returns the filesystem path to the UpdateMetadata golden
// corpus directory, computed relative to the package dir.
func goldenDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "testdata", "updatemetadata")
}

// TestMain seeds the golden corpus before any test runs and fails loudly
// when the committed fixtures no longer match the deterministic generator:
// silent drift here would let TestUpdateMetadata_GoldenCorpus validate
// against stale bytes.
func TestMain(m *testing.M) {
	wd, _ := os.Getwd()
	corpusDir := filepath.Join(wd, "testdata", "updatemetadata")
	if err := seedCorpus(corpusDir); err != nil {
		_, _ = os.Stderr.WriteString("seedCorpus: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// seedCorpus generates the four golden fixtures, writing any that are
// missing and returning an error if a committed fixture differs from the
// generator's output (fixture drift).
func seedCorpus(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	ctx := context.Background()

	// Build a deterministic schema and snapshot (no *testing.T available).
	// Must mirror testSchema exactly so TestUpdateMetadata_GoldenCorpus's
	// Load call with testSchema accepts the fixtures — schema hashes must
	// match.
	s, schemaRes := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()
	if schemaRes.HasErrors() {
		return schemaRes.Err()
	}

	// A tiny populated snapshot for deterministic content.
	typ, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person", typ.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)
	g := graph.New(s)
	g.Add(ctx, inst)
	snap := g.Snapshot()

	// Four fixture shapes.
	fixtures := []struct {
		name string
		opts []snapshot.Option
	}{
		{"compact.ys", nil},
		{"indent2sp.ys", []snapshot.Option{snapshot.WithIndent("  ")}},
		{"indentTab.ys", []snapshot.Option{snapshot.WithIndent("\t")}},
		{"metadata.ys", []snapshot.Option{snapshot.WithMetadata(map[string]string{"seed": "corpus"})}},
	}
	for _, f := range fixtures {
		data, res := snapshot.Marshal(ctx, snap, f.opts...)
		if res.HasErrors() {
			return res.Err()
		}
		path := filepath.Join(dir, f.name)
		committed, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return err
			}
		case err != nil:
			return err
		case !bytes.Equal(committed, data):
			return fmt.Errorf("fixture drift: %s no longer matches the generator output; delete the file to regenerate or fix the generator", path)
		}
	}
	return nil
}

// rehashDocument recomputes a hand-edited document's integrity hash so every
// read path accepts the edited bytes.
func rehashDocument(t *testing.T, data []byte) []byte {
	t.Helper()
	const key = `"integrity_hash":"`
	i := bytes.Index(data, []byte(key))
	if i < 0 {
		t.Fatal("rehashDocument: no integrity_hash value found")
	}
	start := i + len(key)
	end := bytes.IndexByte(data[start:], '"')
	if end < 0 {
		t.Fatal("rehashDocument: unterminated integrity_hash value")
	}
	end += start

	canon := append(append([]byte{}, data[:start]...), data[end:]...)
	sum := sha256.Sum256(canon)
	newVal := fmt.Sprintf("sha256:%x", sum)

	out := append(append(append([]byte{}, data[:start]...), newVal...), data[end:]...)
	return out
}

// TestUpdateMetadataOrReMarshal_FallbackRecoversNonMarshalShape pins the
// fallback path: an input whose body boundary is not Marshal-shaped is
// refused by the fast path and recovered through Load + Marshal, with the
// transition surfaced as W_UPDATE_METADATA_FALLBACK naming the trigger.
func TestUpdateMetadataOrReMarshal_FallbackRecoversNonMarshalShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "title": "Acme"}))
	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	// A JSON-insignificant space after the header object breaks the
	// body-suffix shape while every read path still accepts the bytes.
	spaced := bytes.Replace(data, []byte(`},"types":`), []byte(`} ,"types":`), 1)
	if bytes.Equal(spaced, data) {
		t.Fatal("fixture shape changed; header boundary not found")
	}
	spaced = rehashDocument(t, spaced)

	if _, fastRes := snapshot.UpdateMetadata(ctx, spaced, nil); !hasCode(fastRes, diag.E_UPDATE_METADATA_BODY_OFFSET) {
		t.Fatalf("the fast path accepted a non-Marshal shape: %v", fastRes)
	}

	when := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	newMeta := map[string]string{"stage": "fallback"}
	out, outRes := snapshot.UpdateMetadataOrReMarshal(ctx, spaced, newMeta, s,
		snapshot.WithUpdateCreatedAt(when))
	if outRes.HasErrors() {
		t.Fatalf("the fallback failed: %v", outRes)
	}

	var sawFallback bool
	for iss := range outRes.Issues() {
		if iss.Code() != diag.W_UPDATE_METADATA_FALLBACK {
			continue
		}
		sawFallback = true
		var codes string
		for _, d := range iss.Details() {
			if d.Key == "triggering_codes" {
				codes = d.Value
			}
		}
		if !strings.Contains(codes, "E_UPDATE_METADATA_BODY_OFFSET") {
			t.Errorf("fallback warning names %q, want it to carry E_UPDATE_METADATA_BODY_OFFSET", codes)
		}
	}
	if !sawFallback {
		t.Fatalf("no W_UPDATE_METADATA_FALLBACK warning on the fallback path: %v", outRes)
	}

	// The correctness floor: byte-identical to a direct Marshal of the
	// loaded document under the equivalent options.
	loaded, loadRes := snapshot.Load(ctx, spaced, s)
	if err := loadRes.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	want, wantRes := snapshot.Marshal(ctx, loaded,
		snapshot.WithMetadata(newMeta), snapshot.WithCreatedAt(when))
	if err := wantRes.Err(); err != nil {
		t.Fatalf("floor marshal: %v", err)
	}
	if !bytes.Equal(out, want) {
		t.Errorf("fallback output diverges from the Load + Marshal floor\n got:  %s\n want: %s", out, want)
	}
}

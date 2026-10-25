package snapshot_test

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"
)

// encoding/json writes each byte of a string that is not UTF-8 as U+FFFD, so a
// metadata key or value holding one would read back as other text, and two
// such keys as one repeated member, which the reader refuses. Marshal and
// UpdateMetadata refuse the metadata instead of writing a document Load
// refuses or reads as another.
func TestMetadata_RefusesTextThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	g := graph.New(s)
	doc := readerDoc(t, s, map[string]any{"$type": "Company", "id": "c1"})
	for name, c := range map[string]struct {
		meta map[string]string
		want string
	}{
		"a key":                  {map[string]string{"run\xff": "a"}, `metadata key "run\xff" is not valid UTF-8`},
		"a value":                {map[string]string{"run": "a\xff"}, `metadata value of "run" is not valid UTF-8`},
		"two keys of one decode": {map[string]string{"\xff": "a", "\xfe": "b"}, `metadata key "\xfe" is not valid UTF-8`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, mres := snapshot.Marshal(ctx, g.Snapshot(), snapshot.WithMetadata(c.meta))
			out, ures := snapshot.UpdateMetadata(ctx, []byte(doc), c.meta)
			re, rres := snapshot.UpdateMetadataOrReMarshal(ctx, []byte(doc), c.meta, s)
			for surface, r := range map[string]struct {
				bytes []byte
				res   diag.Result
			}{"Marshal": {data, mres}, "UpdateMetadata": {out, ures}, "UpdateMetadataOrReMarshal": {re, rres}} {
				issues := r.res.Len()
				if r.bytes != nil || issues != 1 || !r.res.HasErrors() || !strings.Contains(r.res.String(), c.want) {
					t.Errorf("%s returned %d bytes and %s, want none and one Error naming %s", surface, len(r.bytes), r.res, c.want)
				}
				for issue := range r.res.Issues() {
					if issue.Code() != diag.E_SNAPSHOT_MALFORMED {
						t.Errorf("%s: %s, want %s", surface, issue.Code(), diag.E_SNAPSHOT_MALFORMED)
					}
				}
			}
		})
	}
}

// Valid text, U+FFFD and characters past ASCII among it, is written and read
// back as it was given, by Marshal and by UpdateMetadata.
func TestMetadata_ValidTextReadsBackAsWritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadReaderSchema(t)
	meta := map[string]string{"run": "a�b", "étape": "日本", "": "empty key"}
	data, res := snapshot.Marshal(ctx, graph.New(s).Snapshot(), snapshot.WithMetadata(meta))
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	updated, res := snapshot.UpdateMetadata(ctx, []byte(readerDoc(t, s, map[string]any{"$type": "Company", "id": "c1"})), meta)
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata: %s", res)
	}
	for surface, doc := range map[string][]byte{"Marshal": data, "UpdateMetadata": updated} {
		if _, lres := snapshot.Load(ctx, doc, s); lres.HasErrors() {
			t.Errorf("%s: Load refuses the document: %s", surface, lres)
		}
		info, hres := snapshot.HeaderOnly(ctx, doc)
		if hres.HasErrors() {
			t.Fatalf("%s: HeaderOnly: %s", surface, hres)
		}
		if !maps.Equal(info.Metadata, meta) {
			t.Errorf("%s: metadata reads back %q, want %q", surface, info.Metadata, meta)
		}
	}
}

// UpdateMetadataOrReMarshal keeps UpdateMetadata's order: a cancelled context
// is reported as cancellation, and a document that is no snapshot draws its
// own refusal beside the metadata's.
func TestUpdateMetadataOrReMarshal_RefusesBadMetadataInItsPlace(t *testing.T) {
	t.Parallel()
	s := loadReaderSchema(t)
	bad := map[string]string{"k\xff": "v"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, res := snapshot.UpdateMetadataOrReMarshal(ctx, []byte(`{}`), bad, s); res.Len() != 1 || !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Errorf("a cancelled call: %s, want one %s", res, diag.E_CONTEXT_CANCELLED)
	}
	_, res := snapshot.UpdateMetadataOrReMarshal(context.Background(), []byte(`xx`), bad, s)
	if res.Len() != 2 || !strings.Contains(res.String(), "is not a yammm snapshot") || !strings.Contains(res.String(), "metadata key") {
		t.Errorf("a document that is no snapshot, with bad metadata: %s, want both refusals", res)
	}
	doc := strings.Replace(readerDoc(t, s, map[string]any{"$type": "Company", "id": "c1"}), `"version":4`, `"version":99`, 1)
	_, res = snapshot.UpdateMetadataOrReMarshal(context.Background(), []byte(doc), bad, s)
	if res.Len() != 2 || !res.HasCode(diag.E_SNAPSHOT_UNSUPPORTED_VERSION) || !strings.Contains(res.String(), "metadata key") {
		t.Errorf("a header no read path accepts, with bad metadata: %s, want both refusals", res)
	}
}

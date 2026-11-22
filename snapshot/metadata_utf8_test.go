package snapshot_test

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/snapshot"
)

// encoding/json writes each byte of a string that is not UTF-8 as U+FFFD, so a
// metadata key or value holding one would read back as other text, and two such
// keys as one repeated member. Marshal and UpdateMetadata refuse the metadata
// instead of writing a document that reads back as another.
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
			for surface, r := range map[string]struct {
				bytes []byte
				res   diag.Result
			}{"Marshal": {data, mres}, "UpdateMetadata": {out, ures}} {
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
		if _, lres := snapshot.Unmarshal(ctx, doc, s); lres.HasErrors() {
			t.Errorf("%s: Load refuses the document: %s", surface, lres)
		}
		info, hres := snapshot.ReadHeader(ctx, bytes.NewReader(doc))
		if hres.HasErrors() {
			t.Fatalf("%s: ReadHeader: %s", surface, hres)
		}
		if !maps.Equal(info.Metadata, meta) {
			t.Errorf("%s: metadata reads back %q, want %q", surface, info.Metadata, meta)
		}
	}
}

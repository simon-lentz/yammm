package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const provenanceTextSchema = `schema "prov"

part type Item {
	code String primary
}

type Holder {
	id String primary
	*-> ITEMS (_:many) Item
}
`

// JSON would write invalid provenance text as U+FFFD, and the document would read
// back as other text. Marshal refuses it at every position an instance stands, as it
// refuses such metadata, and writes no bytes.
func TestMarshal_RefusesProvenanceTextThatIsNotUTF8(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, provenanceTextSchema, "prov.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	holder, item := mustTypeID(t, s, "Holder"), mustTypeID(t, s, "Item")
	key := immutable.WrapKey([]any{"k"})
	root := func(p location.Provenance) snapparts.Instance {
		return snapparts.Instance{
			TypeID: holder, PrimaryKey: key, Properties: immutable.WrapProperties(map[string]any{"id": "k"}), Provenance: p,
		}
	}
	child := func(p location.Provenance) map[string][]snapparts.Instance {
		return map[string][]snapparts.Instance{"ITEMS": {{
			TypeID: item, PrimaryKey: key, Properties: immutable.WrapProperties(map[string]any{"code": "k"}), Provenance: p,
		}}}
	}
	badName := location.NewProvenance("feed\xff", path.Root(), location.Span{})
	badPath := location.NewProvenance("feed", path.Root(), location.Span{}).WithRawPath("$.\xff")

	withChild := root(location.Provenance{})
	withChild.Composed = child(badName)
	for name, parts := range map[string]snapparts.Parts{
		"a root's source name":      {Instances: []snapparts.Instance{root(badName)}},
		"a root's raw path":         {Instances: []snapparts.Instance{root(badPath)}},
		"a child's source name":     {Instances: []snapparts.Instance{withChild}},
		"a duplicate's source name": {Instances: []snapparts.Instance{root(location.Provenance{})}, Duplicates: []snapparts.Instance{root(badName)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, res := snapshot.Marshal(ctx, snapparts.Rebuild[graph.Snapshot](s, parts))
			if data != nil || res.Len() != 1 || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) {
				t.Errorf("Marshal wrote %d bytes, %s; want no bytes and one %s", len(data), res, diag.E_SNAPSHOT_MALFORMED)
			}
		})
	}
}

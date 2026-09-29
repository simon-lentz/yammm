package json

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A data file is read by people and never embedded in HTML, so markup
// characters are written as themselves.
func TestMarshalObject_WritesMarkupCharactersAsThemselves(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, "schema \"m\"\n\ntype Rule {\n\tid String primary\n\ttext String\n}\n", "m.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, _ := s.Type("Rule")
	const text = "endDate > startDate && size < 10"
	g := graph.New(s)
	inst := instance.NewValidInstance("Rule", ty.ID(), immutable.WrapKey([]any{"r<1>"}),
		immutable.WrapProperties(map[string]any{"id": "r<1>", "text": text}), nil, nil, nil)
	if r := g.Add(ctx, inst); r.Err() != nil {
		t.Fatalf("add: %v", r.Err())
	}
	for _, opts := range [][]WriteOption{nil, {WithIndent("\t")}} {
		doc, err := New().MarshalObject(ctx, g.Snapshot(), opts...)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(doc), `"`+text+`"`) || !strings.Contains(string(doc), `"r<1>"`) {
			t.Errorf("markup characters were escaped:\n%s", doc)
		}
		if strings.HasSuffix(string(doc), "\n") {
			t.Error("the document ends in a newline")
		}
		parsed, res := New().ParseObject(ctx, location.NewSourceID("m.json"), doc)
		if err := res.Err(); err != nil {
			t.Fatalf("reparse: %v", err)
		}
		if got, _ := parsed["Rule"][0].Properties["text"].(string); got != text {
			t.Errorf("read back %q, want %q", got, text)
		}
	}
}

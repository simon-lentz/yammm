package json

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
)

const twoRelationsSchema = `schema "tworelations"

type Station {
	id String primary
}

type Reading {
	id String primary
	--> BETA (_:many) Station {
		weight Float
	}
	--> ALPHA (_:many) Station {
		weight Float
	}
}
`

func TestWriters_TwoRefusedRelationsNameTheFirstInRelationOrder(t *testing.T) {
	ctx := context.Background()
	s, res := schema.LoadString(ctx, twoRelationsSchema, "tworelations.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	weight := map[string]any{"weight": json.Number("1e400")}
	snap := snappartstest.Loaded(t, s, snapparts.Parts{
		Instances: []snapparts.Instance{
			heldRoot(t, s, "Station", "s1", map[string]any{"id": "s1"}),
			heldRoot(t, s, "Reading", "r1", map[string]any{"id": "r1"}),
		},
		Edges: []snapparts.Edge{
			heldEdge(t, s, "Reading", "r1", "ALPHA", "s1", weight),
			heldEdge(t, s, "Reading", "r1", "BETA", "s1", weight),
		},
	})
	_, err := New().MarshalObject(ctx, snap)
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("MarshalObject: %v does not match ErrUnrepresentable", err)
	}
	if !strings.Contains(err.Error(), `edge "ALPHA"`) {
		t.Errorf("the refusal does not name the first relation: %v", err)
	}
}

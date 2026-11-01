package json

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
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
	stationT, _ := s.Type("Station")
	readingT, _ := s.Type("Reading")
	target := instance.NewValidEdgeData([]instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(immutable.WrapKey([]any{"s1"}), immutable.WrapProperties(map[string]any{"weight": math.Inf(1)})),
	})
	g := graph.New(s)
	for _, inst := range []instance.ValidInstance{
		instance.NewValidInstance("Station", stationT.ID(), immutable.WrapKey([]any{"s1"}),
			immutable.WrapProperties(map[string]any{"id": "s1"}), nil, nil, location.Provenance{}),
		instance.NewValidInstance("Reading", readingT.ID(), immutable.WrapKey([]any{"r1"}),
			immutable.WrapProperties(map[string]any{"id": "r1"}),
			map[string]instance.ValidEdgeData{"ALPHA": target, "BETA": target}, nil, location.Provenance{}),
	} {
		if r := g.Add(ctx, inst); r.HasErrors() {
			t.Fatalf("graph.Add: %s", r)
		}
	}
	_, err := New().MarshalObject(ctx, g.Snapshot())
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("MarshalObject: %v does not match ErrUnrepresentable", err)
	}
	if !strings.Contains(err.Error(), `edge "ALPHA"`) {
		t.Errorf("the refusal does not name the first relation: %v", err)
	}
}

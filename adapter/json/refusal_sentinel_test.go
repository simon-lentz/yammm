package json

import (
	"context"
	"errors"
	"io"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const nonFiniteSchema = `schema "nonfinite"

type Station {
	id String primary
}

type Reading {
	id String primary
	ratio Float
	series List<Float>
	--> AT (_:many) Station {
		weight Float
	}
}
`

// nonFiniteSnapshot builds a graph through instance.NewValidInstance, which
// runs no validation, because a validated Float is never non-finite. props and
// weight place the value; the rest of the graph is finite.
func nonFiniteSnapshot(t *testing.T, props map[string]any, weight any) *graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), nonFiniteSchema, "nonfinite.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	stationT, _ := s.Type("Station")
	readingT, _ := s.Type("Reading")
	g := graph.New(s)
	stationAt := instance.NewValidInstance("Station", stationT.ID(), immutable.WrapKey([]any{"s1"}),
		immutable.WrapProperties(map[string]any{"id": "s1"}), nil, nil, nil)
	all := map[string]any{"id": "r1"}
	maps.Copy(all, props)
	reading := instance.NewValidInstance("Reading", readingT.ID(), immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(all),
		map[string]*instance.ValidEdgeData{"AT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"s1"}), immutable.WrapProperties(map[string]any{"weight": weight})),
		})}, nil, nil)
	for _, inst := range []*instance.ValidInstance{stationAt, reading} {
		if r := g.Add(context.Background(), inst); r.HasErrors() {
			t.Fatalf("graph.Add: %s", r)
		}
	}
	return g.Snapshot()
}

var nonFinite = math.Inf(1)

// JSON has no number for NaN or an infinity, and every constructor of a
// snapshot holds its structure to what the writer renders, so a non-finite
// float is the one shape of the data the writer refuses. It is refused as the
// class wherever a value stands, naming the site.
func TestWriters_ANonFiniteFloatIsRefusedAsTheClass(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, mentions string
		props          map[string]any
		weight         any
	}{
		{"a property", `property "ratio"`, map[string]any{"ratio": math.Inf(1)}, 1.5},
		{"a list element", `property "series"`, map[string]any{"series": []any{1.5, math.Inf(-1)}}, 1.5},
		{"NaN", `property "ratio"`, map[string]any{"ratio": math.NaN()}, 1.5},
		{"inside a string-keyed map", `property "ratio"`, map[string]any{"ratio": map[string]any{"x": math.NaN()}}, 1.5},
		{"inside an int-keyed map", `property "ratio"`, map[string]any{"ratio": map[int]any{1: math.NaN()}}, 1.5},
		{"inside an array", `property "ratio"`, map[string]any{"ratio": [2]float64{1, math.NaN()}}, 1.5},
		{"behind a pointer", `property "ratio"`, map[string]any{"ratio": &nonFinite}, 1.5},
		{"an edge property", `edge property "weight"`, nil, math.Inf(1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := nonFiniteSnapshot(t, c.props, c.weight)
			a := New()
			_, err := a.MarshalObject(context.Background(), snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalObject: %v does not match ErrUnrepresentable", err)
			}
			if err != nil && !strings.Contains(err.Error(), c.mentions) {
				t.Errorf("the refusal does not name %s: %v", c.mentions, err)
			}
			if _, werr := a.WriteObject(context.Background(), io.Discard, snap); !errors.Is(werr, ErrUnrepresentable) {
				t.Errorf("WriteObject: %v does not match ErrUnrepresentable", werr)
			}
		})
	}
}

// failingWriter refuses every write with errWrite.
type failingWriter struct{}

var errWrite = errors.New("disk full")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

// The class separates a refusal of the data from a nil argument, a
// cancellation and an I/O failure, which reach a caller through one return
// value.
func TestWriters_TheRefusalClassCoversTheDataAlone(t *testing.T) {
	t.Parallel()

	_, nilErr := New().MarshalObject(context.Background(), nil)
	if !errors.Is(nilErr, ErrNilResult) {
		t.Errorf("a nil result = %v, want ErrNilResult", nilErr)
	}
	if errors.Is(nilErr, ErrUnrepresentable) {
		t.Errorf("a nil result matched the refusal class: %v", nilErr)
	}

	finite := nonFiniteSnapshot(t, map[string]any{"ratio": 1.5}, 2.5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().MarshalObject(ctx, finite); err == nil {
		t.Error("a cancelled marshal reported success")
	} else {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the fixture did not reach the cancellation, so it guards nothing: %v", err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("a cancellation matched the refusal class: %v", err)
		}
	}

	if _, err := New().WriteObject(context.Background(), failingWriter{}, finite); !errors.Is(err, errWrite) {
		t.Fatalf("the fixture did not reach the write, so it guards nothing: %v", err)
	} else if errors.Is(err, ErrUnrepresentable) {
		t.Errorf("an I/O failure matched the refusal class: %v", err)
	}
}

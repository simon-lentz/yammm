package json

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
)

const readingSchema = `schema "readings"

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

// readingSnapshot reads a document holding props beside Reading r1's key, and
// weight on its one edge.
func readingSnapshot(t *testing.T, props map[string]any, weight any) graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), readingSchema, "readings.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	all := map[string]any{"id": "r1"}
	maps.Copy(all, props)
	return snappartstest.Loaded(t, s, snapparts.Parts{
		Instances: []snapparts.Instance{
			heldRoot(t, s, "Station", "s1", map[string]any{"id": "s1"}),
			heldRoot(t, s, "Reading", "r1", all),
		},
		Edges: []snapparts.Edge{heldEdge(t, s, "Reading", "r1", "AT", "s1", map[string]any{"weight": weight})},
	})
}

// A number no finite float64 holds is one a document alone delivers, and no reader
// of JSON reads it back as a number. It is refused as the class wherever a value
// stands, naming the site.
func TestWriters_ANumberNoFloatHoldsIsRefusedAsTheClass(t *testing.T) {
	t.Parallel()
	huge := json.Number("1e400")
	for _, c := range []struct {
		name, mentions string
		props          map[string]any
		weight         any
	}{
		{"a property", `property "ratio"`, map[string]any{"ratio": huge}, 1.5},
		{"a list element", `property "series"`, map[string]any{"series": []any{1.5, json.Number("-1e400")}}, 1.5},
		{"inside a string-keyed map", `property "ratio"`, map[string]any{"ratio": map[string]any{"x": huge}}, 1.5},
		{"an edge property", `edge property "weight"`, nil, huge},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := readingSnapshot(t, c.props, c.weight)
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

	_, nilErr := New().MarshalObject(context.Background(), graph.Snapshot{})
	if !errors.Is(nilErr, ErrZeroSnapshot) {
		t.Errorf("a nil result = %v, want ErrZeroSnapshot", nilErr)
	}
	if errors.Is(nilErr, ErrUnrepresentable) {
		t.Errorf("a nil result matched the refusal class: %v", nilErr)
	}

	finite := readingSnapshot(t, map[string]any{"ratio": 1.5}, 2.5)
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

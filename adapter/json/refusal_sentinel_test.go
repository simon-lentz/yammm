package json

import (
	"context"
	"errors"
	"io"
	"maps"
	"math"
	"strings"
	"testing"
	"time"

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

// JSON has no number for NaN or an infinity, and a validated Float is never
// non-finite, so only a bypass-built snapshot holds one. It is refused as the
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

// refusingJSON and refusingText spell themselves for encoding/json and refuse,
// as a caller's own type can; spelled spells itself and succeeds. Each is a
// named type over string, and spelledStruct a struct that spells itself.
type (
	refusingJSON  string
	refusingText  string
	spelled       string
	spelledStruct struct{ s string }
)

func (refusingJSON) MarshalJSON() ([]byte, error) { return nil, errors.New("no spelling") }
func (refusingText) MarshalText() ([]byte, error) { return nil, errors.New("no spelling") }
func (s spelled) MarshalJSON() ([]byte, error)    { return []byte(`{"spelled":"` + string(s) + `"}`), nil }

func (s spelledStruct) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.s + `"`), nil
}

// A named type over a scalar is written as the base value it holds, the one
// form every writer writes: its own method is never asked, so one that would
// refuse or spell another value writes the text it holds. A struct that spells
// itself is no such form, and is refused as the class, naming the site.
func TestWriters_ANamedTypeIsWrittenAsItsBaseValue(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, want string
		props      map[string]any
		weight     any
	}{
		{"a MarshalJSON refusal at a property", `"ratio":"x"`, map[string]any{"ratio": refusingJSON("x")}, 1.5},
		{"a MarshalText refusal at a property", `"ratio":"x"`, map[string]any{"ratio": refusingText("x")}, 1.5},
		{"a refusal inside a list", `"series":[1.5,"x"]`, map[string]any{"series": []any{1.5, refusingJSON("x")}}, 1.5},
		{"a refusal at an edge property", `"weight":"w"`, nil, refusingText("w")},
		{"a type spelling another value", `"ratio":"x"`, map[string]any{"ratio": spelled("x")}, 1.5},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data, err := New().MarshalObject(context.Background(), nonFiniteSnapshot(t, c.props, c.weight))
			if err != nil || !strings.Contains(string(data), c.want) {
				t.Errorf("MarshalObject = %s, %v; want the base value written, %s", data, err, c.want)
			}
		})
	}
	snap := nonFiniteSnapshot(t, map[string]any{"ratio": spelledStruct{"x"}}, 1.5)
	if _, err := New().MarshalObject(context.Background(), snap); !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), `property "ratio"`) {
		t.Errorf("a struct that spells itself: MarshalObject = %v; want a refusal matching ErrUnrepresentable naming property \"ratio\"", err)
	}
	if _, err := New().WriteObject(context.Background(), io.Discard, snap); !errors.Is(err, ErrUnrepresentable) {
		t.Errorf("a struct that spells itself: WriteObject = %v; want a refusal matching ErrUnrepresentable", err)
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

// A time key whose layout writes a zone abbreviation the layout cannot read
// back is held by the graph and refused by the writer at the edge to it. The
// refusal cuts the target key's text.
func TestWriters_ATargetKeyTheLayoutCannotReadBackIsRefusedAndCut(t *testing.T) {
	t.Parallel()
	const src = `schema "lossykey"

type Target {
	at Timestamp["2006-01-02 15:04 MST"] primary
}

type Holder {
	id String primary
	--> LINK (_:many) Target
}
`
	s, res := schema.LoadString(t.Context(), src, "lossykey.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	targetT, _ := s.Type("Target")
	holderT, _ := s.Type("Holder")
	at := time.Date(2020, 1, 2, 12, 3, 4, 5, time.FixedZone("Zone", 3600))
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instance.NewValidInstance("Target", targetT.ID(), immutable.WrapKey([]any{at}),
			immutable.WrapProperties(map[string]any{"at": at}), nil, nil, nil),
		instance.NewValidInstance("Holder", holderT.ID(), immutable.WrapKey([]any{"h1"}),
			immutable.WrapProperties(map[string]any{"id": "h1"}),
			map[string]*instance.ValidEdgeData{"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{at}), immutable.WrapProperties(nil)),
			})}, nil, nil),
	} {
		if r := g.Add(context.Background(), inst); r.HasErrors() {
			t.Fatalf("graph.Add: %s", r)
		}
	}
	const (
		whole = `["2020-01-02T12:03:04.000000005+01:00"]`
		want  = `json adapter: edge "LINK" to ["2020-01-02T12:03:04.000000005+… (39 bytes): target key component 0 holds `
	)
	_, err := New().MarshalObject(context.Background(), g.Snapshot())
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("MarshalObject: %v does not match ErrUnrepresentable", err)
	}
	if !strings.HasPrefix(err.Error(), want) || strings.Contains(err.Error(), whole) {
		t.Errorf("MarshalObject = %v; want it to begin %s and not hold the whole key %s", err, want, whole)
	}
}

package snapshot_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// FuzzUpdateMetadata exercises UpdateMetadata against arbitrary byte
// inputs and metadata maps. The fuzzer asserts two contracts: the
// function must never panic, and on success the output must parse as a
// valid .ys header.
//
// Seed corpus covers the four Marshal-produced shapes UpdateMetadata is
// expected to handle natively: compact × {empty, populated metadata} ×
// indented × {empty, populated metadata}. Go's persistent corpus at
// testdata/fuzz/FuzzUpdateMetadata/ captures any discovered inputs that
// surface new panic or wrong-output cases across fuzz sessions.
func FuzzUpdateMetadata(f *testing.F) {
	// Seed shapes — four canonical Marshal outputs.
	shapes := []struct {
		name string
		opts []snapshot.Option
	}{
		{"compact-empty", nil},
		{"compact-populated", []snapshot.Option{snapshot.WithMetadata(map[string]string{"k": "v"})}},
		{"indent2sp-empty", []snapshot.Option{snapshot.WithIndent("  ")}},
		{"indent2sp-populated", []snapshot.Option{
			snapshot.WithIndent("  "),
			snapshot.WithMetadata(map[string]string{"k": "v", "phase": "link"}),
		}},
	}
	for _, sh := range shapes {
		data := buildFuzzSeed(f, sh.opts...)
		f.Add(data, "phase", "link")
	}

	f.Fuzz(func(t *testing.T, data []byte, metaKey, metaVal string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("UpdateMetadata panicked on fuzz input (len=%d): %v", len(data), r)
			}
		}()

		newMeta := map[string]string{metaKey: metaVal}
		out, res := snapshot.UpdateMetadata(context.Background(), data, newMeta)
		if res.HasErrors() {
			return
		}
		header, headerRes := snapshot.HeaderOnly(context.Background(), out)
		if headerRes.HasErrors() {
			t.Errorf("UpdateMetadata returned OK but output does not parse: %v", headerRes)
			return
		}
		if header == nil {
			t.Error("UpdateMetadata returned OK but output header is nil")
		}
	})
}

// buildFuzzSeed returns a Marshal-produced .ys byte slice for use as a
// fuzz seed. Takes testing.TB (satisfied by *testing.F) so the same
// helper shape as the T-suite applies.
func buildFuzzSeed(tb testing.TB, opts ...snapshot.Option) []byte {
	tb.Helper()
	s, sres := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()
	if sres.HasErrors() {
		tb.Fatalf("fuzz seed schema: %v", sres.Err())
	}
	typ, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person", typ.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)
	g := graph.New(s)
	g.Add(context.Background(), inst)
	snap := g.Snapshot()
	data, res := snapshot.Marshal(context.Background(), snap, opts...)
	if res.HasErrors() {
		tb.Fatalf("fuzz seed marshal: %v", res.Err())
	}
	return data
}

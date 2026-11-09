// The Windows clock behind process times ticks too coarsely for these runs.

//go:build unix

package graph_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/costratio"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// maxLinearRatio separates eight times the listings at linear cost, measured at 8
// to 21 on a loaded host, from quadratic cost, measured at 41 to 93.
const maxLinearRatio = 32

const listingSchema = "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n"

// listings validates n listings of X x, the i-th naming target "t<i>".
func listings(t *testing.T, s schema.Schema, n int) []instance.ValidInstance {
	t.Helper()
	out := make([]instance.ValidInstance, n)
	for i := range out {
		out[i] = instancetest.Valid(t, s, "X", map[string]any{
			"id": "x", "m": []any{map[string]any{"_target_id": "t" + strconv.Itoa(i)}},
		})
	}
	return out
}

func targets(t *testing.T, s schema.Schema, n int) []instance.ValidInstance {
	t.Helper()
	out := make([]instance.ValidInstance, n)
	for i := range out {
		out[i] = instancetest.Valid(t, s, "T", map[string]any{"id": "t" + strconv.Itoa(i)})
	}
	return out
}

// Each merge into a held root tests every target it names against what the root
// holds in one lookup, so n merges of one key cost linear time.
func TestAddOrMerge_ListingsOfOneKeyCostLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ctx := t.Context()
	s := loadEntry(t, listingSchema)
	for name, targetsFirst := range map[string]bool{"to held targets": true, "to missing targets": false} {
		ratio := costratio.Ratio(t, 500, func(n int) time.Duration {
			g := graph.New(s)
			if targetsFirst {
				for _, target := range targets(t, s, n) {
					mustOK(t, g.Add(ctx, target))
				}
			}
			merges := listings(t, s, n)
			start := costratio.CPUTime(t)
			for _, listing := range merges {
				if _, res := g.AddOrMerge(ctx, listing); res.HasErrors() {
					t.Fatal(res)
				}
			}
			return costratio.CPUTime(t) - start
		})
		if ratio >= maxLinearRatio {
			t.Errorf("%s: eight times the merges cost %.0f times the time, want under %d", name, ratio, maxLinearRatio)
		}
	}
}

// A target added after the merges resolves its one unresolved record in one
// lookup, so n forward references of one source resolve in linear time.
func TestAdd_ResolvingAMergedSourcesForwardReferencesCostsLinearTime(t *testing.T) {
	raceskip.Skip(t)
	ctx := t.Context()
	s := loadEntry(t, listingSchema)
	// Below 2000 listings the resolution's own cost hides a quadratic index.
	ratio := costratio.Ratio(t, 2000, func(n int) time.Duration {
		g := graph.New(s)
		for _, listing := range listings(t, s, n) {
			if _, res := g.AddOrMerge(ctx, listing); res.HasErrors() {
				t.Fatal(res)
			}
		}
		adds := targets(t, s, n)
		start := costratio.CPUTime(t)
		for _, target := range adds {
			mustOK(t, g.Add(ctx, target))
		}
		return costratio.CPUTime(t) - start
	})
	if ratio >= maxLinearRatio {
		t.Errorf("eight times the forward references cost %.0f times the time to resolve, want under %d", ratio, maxLinearRatio)
	}
}

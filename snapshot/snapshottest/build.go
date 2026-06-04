package snapshottest

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Record describes one input to [BuildTestSnapshot]: a type name and its
// raw instance shape, suitable for validator + graph batch assembly.
//
// Records compose the same shape as the (typeName, raw) pairs every
// rdata pipeline's loop hand-writes, so test fixtures can mirror the
// production call shape directly.
type Record struct {
	// TypeName is the canonical instance tag form
	// (e.g., "Person" for local types, "alias.Type" for imported).
	TypeName string

	// Raw is the property-and-edge map yammm validates against the
	// schema's type definition.
	Raw instance.RawInstance
}

// BuildTestSnapshot validates each record against s via
// [graph.BatchAssembler] and returns the finalized snapshot. Calls
// tb.Fatalf on any validation, add, or check failure — intended for
// happy-path test fixtures only. Use [graph.BatchAssembler] directly
// when the test needs to exercise diagnostic-laden inputs.
//
// Uses [instance.RecommendedOptions] for validator configuration,
// matching the manual-loop pattern across rdata pipelines so test
// fixtures and production code share the same validation strictness
// (case-sensitive property names, no unknown fields).
//
// Records are submitted sequentially in the order provided; mixed-type
// batches are supported in a single call. The returned [graph.Snapshot]
// is independent of the assembler and safe to pass to [snapshot.Marshal]
// / [snapshot.Verify] / [graph.Snapshot]'s own accessors.
func BuildTestSnapshot(tb testing.TB, s *schema.Schema, records ...Record) *graph.Snapshot {
	tb.Helper()
	ctx := context.Background()
	ba := graph.NewBatchAssembler(
		ctx, s,
		graph.WithValidatorOptions(instance.RecommendedOptions()...),
	)
	for i, rec := range records {
		if err := ba.Add(rec.TypeName, rec.Raw); err != nil {
			tb.Fatalf("BuildTestSnapshot: Add[%d] (%s): %v", i, rec.TypeName, err)
			return nil
		}
	}
	res, err := ba.Finalize(ctx)
	if err != nil {
		tb.Fatalf("BuildTestSnapshot: Finalize: %v", err)
		return nil
	}
	return res.Snapshot
}

package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// Load deserializes a previously-saved snapshot from the yammm snapshot
// persistence format (.ys).
//
// Load follows the library's standard diagnostic pattern: it returns
// (T, diag.Result) where Result carries structured issues with stable codes.
// On success, the Result is OK (or contains only warnings). On failure,
// the Result has Error-severity issues and the snapshot is nil.
//
// The schema parameter must be the same schema (or a compatible schema)
// used when the data was originally validated. Load verifies compatibility
// using [schema.StructuralHash].
//
// Load does not re-validate instance data. The persisted snapshot is
// assumed to contain valid data. However, Load performs structural
// validation of the .ys format itself.
//
// Dynamic numeric values materialize as int64 and float64 only: the wire
// carries no width, so a float32 stored at write time returns as float64
// carrying the same value exactly — converting it back to float32 returns
// the original.
//
// The returned Snapshot's Schema() method returns the schema provided to
// Load, not the schema used at original construction time.
//
// The returned Snapshot's Diagnostics() method returns diag.OK() —
// construction diagnostics are transient and not persisted.
//
// Panics if s is nil (programming error).
func Load(ctx context.Context, data []byte, s *schema.Schema, opts ...LoadOption) (*graph.Snapshot, diag.Result) {
	if s == nil {
		panic("snapshot.Load: nil Schema")
	}
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	cfg := applyLoadOptions(opts)
	sd := newStreamDecoder(data, s, cfg)

	// Steps 1-3: Decode and validate header, types, schema hash.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	groups, diags, ok := sd.runPipeline(ctx)
	if !ok {
		return nil, sd.collector.Result()
	}

	snap, result := graph.RebuildSnapshot(s, sd.loadDocument(groups, diags))
	if result.HasErrors() {
		sd.collector.Merge(result)
		return nil, sd.collector.Result()
	}
	return snap, sd.collector.Result()
}

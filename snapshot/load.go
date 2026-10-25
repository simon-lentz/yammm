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
// Load does not re-validate instance data by default: whether a value
// satisfies its constraint, and whether an invariant holds, is the writer's
// claim (see the package's Validity Contract). [WithRevalidation] runs root
// instances, with their composed children, edges and target_missing records,
// back through the real validator;
// [WithValueConformance] is the narrower canonical-form check. Whatever the
// options say, Load refuses a document that breaks the .ys format or a fact of
// the graph package doc's "Structural facts".
//
// A dynamic numeric value materializes as an int64 or a float64, save the two
// cases this paragraph ends on: the wire carries no width, so a float32
// stored at write time returns as a float64 that converts back to the original
// float32 — the float64 its shortest 32-bit wire form parses to, or its own
// value where that form would round twice. A literal no finite float64 can
// hold returns as a json.Number, unconverted, and so does a number nested more
// than 64 levels deep in a dynamic value.
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
	if err := checkDocument(data); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}

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

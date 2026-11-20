package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snapread"
	"github.com/simon-lentz/yammm/schema"
)

func init() {
	snapread.UnmarshalWithHeader = func(ctx context.Context, data []byte, s schema.Schema) (graph.Snapshot, any, diag.Result) {
		checkUnmarshalArgs(ctx, s)
		var h Header
		snap, res := unmarshal(ctx, data, s, nil, &h)
		if res.HasErrors() {
			return snap, (*Header)(nil), res
		}
		return snap, &h, res
	}
}

// Unmarshal deserializes a previously-saved snapshot from the yammm snapshot
// persistence format (.ys).
//
// Unmarshal follows the library's standard diagnostic pattern: it returns
// (T, diag.Result) where Result carries structured issues with stable codes.
// On success, the Result is OK (or contains only warnings). On failure,
// the Result has an Error or a Fatal issue and the snapshot is zero.
//
// The schema parameter must be the same schema (or a compatible schema)
// used when the data was originally validated. Unmarshal verifies compatibility
// using [schema.StructuralHash].
//
// Unmarshal does not re-validate instance data by default: whether a value
// satisfies its constraint, and whether an invariant holds, is the writer's
// claim (see the package's Validity Contract). [WithRevalidation] runs each
// root the structural checks left whole, with its composed children, edges and
// target_missing records, back through the real validator. Whatever the
// options say, Unmarshal refuses a document that breaks the .ys format, a string of
// invalid UTF-8 or half a surrogate pair among such breaks, or a fact of the
// graph package doc's "Structural facts", save that
// [WithIntegrityCheck](false) skips the integrity hash.
//
// A number materializes by the value rule at the kind its position declares. At
// a Float, and at a Vector element, a number literal is its nearest float64, so
// 5 loads as 5.0, -0 keeps its sign and an integer past int64 rounds. Elsewhere
// an integer literal an int64 holds is an int64, a float literal a float64, and
// an integer literal past int64 stays the json.Number its text spells. At any
// position, a literal no finite float64 holds stays the json.Number.
//
// The returned Snapshot's Schema() method returns the schema provided to
// Unmarshal, not the schema used at original construction time.
//
// Panics if s is zero or ctx is nil (programming error).
func Unmarshal(ctx context.Context, data []byte, s schema.Schema, opts ...LoadOption) (graph.Snapshot, diag.Result) {
	checkUnmarshalArgs(ctx, s)
	return unmarshal(ctx, data, s, opts, nil)
}

func checkUnmarshalArgs(ctx context.Context, s schema.Schema) {
	if s.IsZero() {
		panic("snapshot.Unmarshal: zero Schema")
	}
	if ctx == nil {
		panic("snapshot.Unmarshal: nil context")
	}
}

// unmarshal is [Unmarshal] after its argument checks. When header is non-nil and
// the document is accepted, it stores the decoded header there, which the
// snapshot does not carry.
func unmarshal(ctx context.Context, data []byte, s schema.Schema, opts []LoadOption, header *Header) (graph.Snapshot, diag.Result) {
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return graph.Snapshot{}, c.Result()
	}

	cfg := applyLoadOptions(opts)
	sd := newStreamDecoder(data, s, cfg)
	if err := checkDocument(data); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return graph.Snapshot{}, sd.collector.Result()
	}

	// Steps 1-3: Decode and validate header, types, schema hash.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return graph.Snapshot{}, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return graph.Snapshot{}, sd.collector.Result()
	}

	groups, diags, ok := sd.runPipeline(ctx)
	if !ok {
		return graph.Snapshot{}, sd.collector.Result()
	}

	if header != nil {
		*header = sd.decodedHeader()
	}
	return snapparts.Rebuild[graph.Snapshot](s, sd.loadDocument(groups, diags)), sd.collector.Result()
}

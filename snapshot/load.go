package snapshot

import (
	"bytes"
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snapread"
	"github.com/simon-lentz/yammm/schema"
)

func init() {
	snapread.HeaderOf = func(ctx context.Context, data []byte) (any, error) {
		if ctx == nil {
			panic("snapread.HeaderOf: nil context")
		}
		h, err := readHeader(ctx, bytes.NewReader(data), int64(len(data))+1)
		if err != nil {
			return nil, err
		}
		return h, nil
	}
	snapread.UnmarshalWithHeader = func(ctx context.Context, data []byte, s schema.Schema) (graph.Snapshot, any, error) {
		checkUnmarshalArgs(ctx, s)
		var h Header
		snap, err := unmarshal(ctx, data, s, newConfig(nil), &h)
		if err != nil {
			return graph.Snapshot{}, nil, err
		}
		return snap, &h, nil
	}
}

// readTakes is the options [Unmarshal] and [Verify] take.
var readTakes = []optionKind{optIssueLimit, optRevalidation, optIntegrityCheck, optWarnings}

// Unmarshal deserializes a previously-saved snapshot from the yammm snapshot
// persistence format (.ys).
//
// A document Unmarshal refuses returns a zero Snapshot and a
// *[diag.ResultError] holding the failures, Error and Fatal issues with stable
// codes. An issue below failure severity is never in the error: it goes to
// [WithWarnings]. Unmarshal takes [WithIssueLimit], [WithRevalidation],
// [WithIntegrityCheck] and [WithWarnings], and refuses any other option with an
// error naming it.
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
func Unmarshal(ctx context.Context, data []byte, s schema.Schema, opts ...Option) (graph.Snapshot, error) {
	checkUnmarshalArgs(ctx, s)
	cfg := newConfig(opts)
	if err := cfg.refuse("Unmarshal", readTakes...); err != nil {
		return graph.Snapshot{}, err
	}
	return unmarshal(ctx, data, s, cfg, nil)
}

func checkUnmarshalArgs(ctx context.Context, s schema.Schema) {
	if s.IsZero() {
		panic("snapshot.Unmarshal: zero Schema")
	}
	if ctx == nil {
		panic("snapshot.Unmarshal: nil context")
	}
}

// unmarshal is [Unmarshal] after its argument and option checks. When header
// is non-nil and the document is accepted, it stores the decoded header there,
// which the snapshot does not carry.
func unmarshal(ctx context.Context, data []byte, s schema.Schema, cfg config, header *Header) (graph.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return graph.Snapshot{}, cancelled(err)
	}

	sd := newStreamDecoder(data, s, cfg)
	groups, diags, ok := sd.readDocument(ctx)
	if !ok {
		return graph.Snapshot{}, sd.collector.Err()
	}

	if header != nil {
		*header = sd.decodedHeader()
	}
	return snapparts.Rebuild[graph.Snapshot](s, sd.loadDocument(groups, diags)), nil
}

// readDocument runs every check [Unmarshal] and [Verify] run on a document, the
// document's text and shape, its header and types table, then its body, and
// returns the decoded body only when the document is clean.
func (sd *streamDecoder) readDocument(ctx context.Context) ([]instanceGroupWire, diagWire, bool) {
	if err := checkDocument(sd.data); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, diagWire{}, false
	}

	// Steps 1-3: Decode and validate header, types, schema hash.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, diagWire{}, false
	}
	if sd.collector.HasErrors() {
		return nil, diagWire{}, false
	}
	return sd.runPipeline(ctx)
}

// cancelled returns the failure a call reports when ctx is already done at its
// entry: one Fatal E_CONTEXT_CANCELLED.
func cancelled(err error) error {
	return failure(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
}

// failure returns the error that holds issue alone.
func failure(issue diag.Issue) error {
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(issue)
	return c.Err()
}

package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/schema"
)

// Verify runs the checks [Unmarshal] runs on a .ys file before it materializes a
// Snapshot, and returns the error Unmarshal would return: nil for a document
// Unmarshal accepts, else a *[diag.ResultError] holding the failures, with
// Unmarshal's codes. It takes Unmarshal's options and refuses any other.
//
// Verify stops before materialization, so it builds no Snapshot and no graph
// instance; under [WithRevalidation] a validator run that passes builds an instance
// it discards. It does decode the instances section
// first, holding every instance's properties, so peak memory scales with
// document size rather than with key count.
//
// Panics if s is zero or ctx is nil (programming error).
func Verify(ctx context.Context, data []byte, s schema.Schema, opts ...Option) error {
	if s.IsZero() {
		panic("snapshot.Verify: zero Schema")
	}
	if ctx == nil {
		panic("snapshot.Verify: nil context")
	}
	cfg := newConfig(opts)
	if err := cfg.refuse("Verify", readTakes...); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return cancelled(err)
	}

	sd := newStreamDecoder(data, s, cfg)
	sd.readDocument(ctx)
	return sd.collector.Err()
}

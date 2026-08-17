package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Verify runs all structural validation checks that [Load] performs on a .ys
// file without materializing a Snapshot. Returns diag.Result with the same
// diagnostic codes as Load.
//
// Verify runs Load's pipeline and stops before materialization, so it builds
// no Snapshot and no instance objects. It does decode the instances section
// first, holding every instance's properties, so peak memory scales with
// document size rather than with key count.
//
// Panics if s is nil (programming error).
func Verify(ctx context.Context, data []byte, s *schema.Schema, opts ...LoadOption) diag.Result {
	if s == nil {
		panic("snapshot.Verify: nil Schema")
	}
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return c.Result()
	}

	cfg := applyLoadOptions(opts)
	sd := newStreamDecoder(data, s, cfg)

	// Steps 1-3: Decode and validate header, types, schema hash.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return sd.collector.Result()
	}

	sd.runPipeline(ctx)
	return sd.collector.Result()
}

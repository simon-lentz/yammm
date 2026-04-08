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
// Verify uses the same validation logic as Load but discards instance data
// after validation, keeping only an instance existence index and edge
// references. Memory usage is O(keys + edge references) — typically less
// than 5% of file size for property-heavy snapshots.
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

	// Decode remaining sections (instances + diagnostics).
	instances, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return sd.collector.Result()
	}

	// Step 4: Validate instances (lightweight path — validateInstances).
	exists, refs, err := sd.validateInstances(ctx, instances)
	if err != nil {
		return sd.collector.Result()
	}

	// Step 5: Validate diagnostics section.
	sd.validateDiagnostics(diags, exists)

	// Step 6: Integrity verification.
	sd.verifyIntegrity()

	// Step 7: Structural validation — edge references.
	sd.validateEdgeRefs(refs, exists)

	return sd.collector.Result()
}

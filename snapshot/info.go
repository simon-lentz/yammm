package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
)

// SnapshotInfo contains summary metadata and statistics extracted from a
// .ys file without full deserialization.
//
// SnapshotInfo uses exported fields rather than accessor methods. This is a
// deliberate departure from the library's typical pattern — SnapshotInfo is
// a read-only data transfer object with no invariants to protect.
type SnapshotInfo struct { //nolint:revive // intentional stutter — mirrors .ys format section name
	// Header fields.
	Version             int
	Features            []string
	SchemaName          string
	SchemaSource        string
	SchemaHash          string
	SchemaHashAlgorithm int
	IntegrityHash       string
	CreatedAt           string            // RFC 3339 or empty
	Metadata            map[string]string // user-provided annotations, nil if absent

	// Content summary.
	Types           []string
	InstanceCounts  map[string]int // type name → count
	TotalInstances  int
	TotalEdges      int
	DuplicateCount  int
	UnresolvedCount int

	// File metadata.
	FileSize        int64  // len(data)
	IntegrityStatus string // "ok", "mismatch", or "skipped"
}

// Info reads summary metadata and statistics from a .ys file without
// loading the schema or materializing instance objects.
//
// Info uses the shared streamDecoder infrastructure. Memory usage is
// constant regardless of snapshot size.
//
// Info verifies the integrity hash and reports the result in IntegrityStatus.
// Schema hash cannot be verified (no schema available).
//
// Returns (nil, result) with Error-severity diagnostics for malformed input.
//
// Info follows the library's standard (T, diag.Result) return pattern.
func Info(ctx context.Context, data []byte) (*SnapshotInfo, diag.Result) {
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	sd := newStreamDecoder(data, nil, loadConfig{})

	// Decode header + types.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	// Decode remaining sections.
	instances, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}

	// Count instances and edges.
	counts, totalEdges := sd.countInstances(instances)

	totalInstances := 0
	for _, c := range counts {
		totalInstances += c
	}

	// Verify integrity hash.
	integrityStatus := sd.verifyIntegrity()

	info := &SnapshotInfo{
		Version:             sd.header.Version,
		Features:            sd.header.Features,
		SchemaName:          sd.header.SchemaName,
		SchemaSource:        sd.header.SchemaSource,
		SchemaHash:          sd.header.SchemaHash,
		SchemaHashAlgorithm: sd.header.SchemaHashAlgorithm,
		IntegrityHash:       sd.header.IntegrityHash,
		CreatedAt:           sd.header.CreatedAt,
		Metadata:            sd.header.Metadata,
		Types:               sd.types,
		InstanceCounts:      counts,
		TotalInstances:      totalInstances,
		TotalEdges:          totalEdges,
		DuplicateCount:      len(diags.Duplicates),
		UnresolvedCount:     len(diags.Unresolved),
		FileSize:            int64(len(data)),
		IntegrityStatus:     integrityStatus,
	}

	return info, sd.collector.Result()
}

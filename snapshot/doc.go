// Package snapshot provides serialization and deserialization of [graph.Snapshot]
// values to and from the yammm snapshot persistence format (.ys).
//
// The .ys format is a JSON-based persistence format that preserves full structural
// fidelity: instances with properties, primary keys, edges, compositions, provenance,
// duplicates, and unresolved edge records all survive a Marshal/Load round-trip.
//
// The format includes a schema structural hash for compatibility verification, an
// integrity hash for corruption detection, and a features array for forward
// compatibility.
//
// # Usage
//
//	// Serialize
//	data, result := snapshot.Marshal(ctx, snap)
//
//	// Deserialize
//	snap, result := snapshot.Load(ctx, data, s)
//
//	// Validate without loading
//	result := snapshot.Verify(ctx, data, s)
//
//	// Read metadata only
//	info, result := snapshot.Info(ctx, data)
//
//	// Read header only (dispatch-style workloads)
//	header, result := snapshot.HeaderOnly(ctx, data)
//
//	// Read header only from an io.Reader (no pre-materialized bytes)
//	header, result := snapshot.HeaderOnlyRead(ctx, file)
//
//	// Compare the header's schema hash against a loaded schema
//	if !header.SchemaHashMatches(s) { /* stale-schema path */ }
//
// # Functions
//
// [Marshal] serializes a *graph.Snapshot to .ys bytes. Output is deterministic
// by default (no timestamp unless [WithCreatedAt] is used).
//
// [Load] deserializes .ys bytes back to a *graph.Snapshot, verifying structural
// integrity and schema compatibility.
//
// [Verify] validates a .ys file without materializing a Snapshot — useful for
// CI pipelines and pre-flight checks.
//
// [Info] reads summary metadata and statistics from a .ys file without loading
// the schema or materializing instance objects. Returns a [SnapshotInfo] with
// schema name, hash, instance counts per type, totals, and integrity status.
// Cost scales with file size (the instance body is scanned to populate counts).
//
// [HeaderOnly] reads header metadata from a .ys file without decoding the
// instance body or verifying the integrity hash. Returns a [HeaderInfo] with
// the header fields plus the types array. Cost is proportional to the header
// size, not the total file size — the right choice for dispatch-style
// workloads that scan many .ys files to classify state or compare schema
// hashes. When counts, diagnostics, or verified integrity are required, use
// [Info] instead.
//
// [HeaderOnlyRead] is the streaming sibling of [HeaderOnly]: it accepts an
// io.Reader and parses the header without requiring the caller to
// pre-materialize the full document into memory. Intended for dispatch
// callers that open each .ys file with os.Open (rather than os.ReadFile)
// and only need header metadata. Reads at most [MaxHeaderSize] bytes
// from the reader; larger headers are rejected with a distinguished
// E_SNAPSHOT_MALFORMED message.
//
// [HeaderInfo.SchemaHashMatches] is the nil-safe dispatch-site helper
// for comparing a header's schema hash against a loaded *schema.Schema.
// Use it after [HeaderOnly] or [HeaderOnlyRead] when the dispatch
// decision depends on whether the snapshot was produced under a
// matching schema version.
//
// # Marshal Options
//
// [Marshal] accepts [Option] values:
//
//   - [WithIndent]: pretty-print JSON output with the given indent string
//   - [WithCreatedAt]: embed a creation timestamp in the snapshot
//   - [WithMetadata]: embed arbitrary key-value metadata
//
// # Load Options
//
// [Load] and [Verify] accept [LoadOption] values:
//
//   - [WithSkipIntegrityCheck]: skip SHA-256 integrity verification (useful for debugging)
//
// # Error Handling
//
// All functions return [diag.Result]:
//
//   - Fatal: I/O failure or context cancellation
//   - Error: schema hash mismatch, integrity check failure, structural corruption
//   - OK: success (may include warnings)
//
// # File Extension
//
// The conventional file extension is .ys (yammm snapshot).
//
// # Thread Safety
//
// All functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	snapshot  ──imports──▶  graph, schema, instance, diag, location, immutable
package snapshot

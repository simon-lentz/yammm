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
// # Validity Contract
//
// Validation happens at [github.com/simon-lentz/yammm/instance.Validator] —
// the one door that sets the unforgeable
// [github.com/simon-lentz/yammm/instance.ValidInstance.Validated] bit. The
// exported bypass constructors assert a caller's claim and leave it false.
//
// The .ys header carries the writing library's attestation: whether every
// root and composed child was validator-built, and whether every Required
// association resolved. The integrity hash protects that claim against
// tampering — and against nothing else. [graph.RebuildSnapshot] is exported,
// so any process can assemble and sign a document whose header claims what
// its instances never earned; the unforgeable point is the instance layer,
// and the attestation is the writer's word, not a proof.
//
// [Load] returns what was written. A .ys can hold a graph that fails the
// graph layer's Add-time relation guards, values outside their constraints,
// and invariant violations; [WithRevalidation] is the option that reports
// all of it — the real validator, run per root at load time.
// [WithValueConformance] is the narrower canonical-form check. Duplicates
// and unresolved records ride the document as data
// ([graph.Snapshot.Duplicates], [graph.Snapshot.Unresolved]); a rejected
// duplicate's payload is outside the attestation.
//
// [schema.StructuralHash] is the schema identity the header pins: an
// identity over the rules that decide what instance data is valid.
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
//	// Catch the same schema shape recorded under different source paths,
//	// which the hash cannot see
//	if unknown := header.UnknownTypes(s); len(unknown) > 0 { /* stale-schema path */ }
//
//	// Write .ys bytes atomically to disk (tmp+fsync+rename)
//	if err := snapshot.WriteFile(path, data); err != nil { /* ... */ }
//
//	// Iterate every .ys file in a directory (header-only, lazy)
//	for entry, err := range snapshot.ScanDir(ctx, dir) {
//	    if err != nil { /* dir-level or ctx-cancel failure */ }
//	    if entry.Result.HasErrors() { /* per-file failure */ }
//	    use(entry.Header)
//	}
//
//	// Rewrite metadata on an existing .ys without reloading the body
//	// (fast path — reuses body bytes verbatim)
//	out, result := snapshot.UpdateMetadata(ctx, data, newMeta)
//
//	// Same, with automatic Load+Marshal fallback on non-Marshal-shaped inputs
//	out, result := snapshot.UpdateMetadataOrReMarshal(ctx, data, newMeta, s)
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
// [WriteFile] writes bytes to a path atomically using the
// tmp+fsync+rename pattern. The staging file at path+[TmpSuffix] is
// fsync'd before rename; on any error during the write, WriteFile
// attempts to clean up the staging file and returns a wrapped error.
// A crash between fsync and rename leaves the staging file behind as
// a partial write; consumer-side cleanup (e.g., directory sweeps)
// reference [TmpSuffix] rather than hard-coding ".tmp" so the
// convention stays single-source-of-truth across the snapshot
// package.
//
// [ScanDir] iterates every .ys file in a directory and yields one
// [ScanEntry] per file, with the header parsed lazily via
// [HeaderOnlyRead] on demand. The iterator's second yielded value is
// non-nil only for operation-level failures (dir-open, context
// cancellation); per-file failures (corrupt header, per-file I/O)
// surface on [ScanEntry.Result] and iteration continues. Files whose
// basename ends with [TmpSuffix] are skipped so crash-residual
// staging files are not confused for complete snapshots.
// [ScanDirSlice] is the materializing convenience wrapper.
// [ScanDirWith] and [ScanDirSliceWith] are the same two under [ScanOption]
// values, where [WithScanFilter] rejects a file before it is opened.
//
// [UpdateMetadata] rewrites the header of an existing .ys document
// with a new metadata map, reusing the body bytes verbatim and
// recomputing only the SHA-256 integrity hash. On a 20 MB input the
// fast path is ~50× faster than the equivalent Load + Marshal round
// trip on M2-class hardware; the lower-bound CI gate is 3×. Depends
// on the field-order and body-suffix stability contracts documented
// in wire.go; future Marshal-side shape changes must respect those
// contracts or update this primitive in lockstep.
//
// [UpdateMetadataOrReMarshal] is the default consumer entry point:
// it runs [UpdateMetadata] on the happy path and transparently falls
// back to [Load] + [Marshal] on recoverable Fatals (body-offset
// failure, malformed header, or any non-cancellation Fatal), surfacing
// a Warning-severity [diag.W_UPDATE_METADATA_FALLBACK] on the returned
// [diag.Result] so operators can observe fallback frequency.
//
// [WithUpdateCreatedAt] overrides the created_at header field on
// [UpdateMetadata] and [UpdateMetadataOrReMarshal]; the default is to
// preserve the existing value byte-for-byte.
//
// # Marshal Options
//
// [Marshal] accepts [Option] values:
//
//   - [WithIndent]: pretty-print JSON output with the given indent string
//   - [WithCreatedAt]: embed a creation timestamp in the snapshot
//   - [WithMetadata]: embed arbitrary key-value metadata
//
// # Update Options
//
// [UpdateMetadata] and [UpdateMetadataOrReMarshal] accept [UpdateOption]
// values:
//
//   - [WithUpdateCreatedAt]: override the existing created_at (preserved by default)
//
// # Load Options
//
// [Load] and [Verify] accept [LoadOption] values:
//
//   - [WithSkipIntegrityCheck]: skip SHA-256 integrity verification (useful for debugging)
//   - [WithValueConformance]: report stored Timestamp/Date/UUID values that do not conform to their constraints (Warning)
//   - [WithRevalidation]: run every instance back through the real validator, reported at the given severity
//
// # Scan Options
//
// [ScanDirWith] and [ScanDirSliceWith] accept [ScanOption] values:
//
//   - [WithScanFilter]: reject a file before it is opened
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
// # Wire Format Versions
//
// The .ys wire format uses a version field in the header for forward
// evolution, and this package reads one version. yammm v0.12.0
// introduced v3 — the types section is a table of full type identities
// that every other position references by row index — and retired every
// earlier version, because the v1/v2 name forms cannot express a
// transitively imported type or separate two same-named types in
// different schemas.
//
// [MinReadableVersion] names the lowest version this package accepts on
// read paths; the accept range is the closed interval
// [[MinReadableVersion], currentVersion], which is [3, 3] at yammm
// v0.12.0. Documents outside the range surface an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION] with the observed version and
// the supported range named in the message. An older reader (yammm
// v0.11.0 and earlier) rejects a v3 document the same way, so an
// operator running an older binary sees a structured diagnostic rather
// than a misread types section. See docs/VERSIONING.md for the full
// pre-1.0 / post-1.0 wire-format policy.
//
// # Thread Safety
//
// All functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	snapshot  ──imports──▶  graph, instance, schema, diag, location, location/path, immutable
//
// The instance edge exists for [WithRevalidation]: re-validation runs the
// real validator, so the option's fidelity is the validator's own.
package snapshot

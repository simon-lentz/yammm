// Package snapshot provides serialization and deserialization of [graph.Snapshot]
// values to and from the yammm snapshot persistence format (.ys).
//
// The .ys format is a JSON-based persistence format that preserves structural
// fidelity: instances with properties, primary keys, edges, compositions, provenance,
// duplicates, and unresolved edge records all survive a Marshal/Load round-trip.
// Provenance survives as source name and path, with a zero span, and a
// duplicate's Diagnostic is not persisted.
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
// association resolved. The integrity hash is an unkeyed SHA-256 of the
// document: it detects corruption and an edit nobody rehashed, and anyone can
// recompute it, so it stops no deliberate one. [graph.RebuildSnapshot] is exported,
// so any process can assemble and sign a document whose header claims what
// its instances never earned; the unforgeable point is the instance layer,
// and the attestation is the writer's word, not a proof.
//
// [Load] refuses a document whose structure no caller of [graph.Graph.Add]
// could have built: one that breaks a fact of the graph package doc's
// "Structural facts" — type identity, root and denoted types, composition
// slots, keys and their key properties, declared names, association shapes,
// the unresolved records graph.Graph.Add derives, and each duplicate's derived
// conflict — or holds two roots at one address ([diag.E_DUPLICATE_PK]). No option
// excuses them, and [Verify] and [Info] run the same checks; a schema-less
// read judges none that needs a schema.
//
// What a .ys can still hold is data a validated graph would not: values
// outside their constraints and invariant violations. [WithRevalidation] is
// the option that reports them — the real validator, run per root at load
// time — and [WithValueConformance] the narrower canonical-form check.
// Duplicates and unresolved records ride the document as data
// ([graph.Snapshot.Duplicates], [graph.Snapshot.Unresolved]); a rejected
// duplicate's payload is outside the attestation, and whether an unresolved
// record's association is required is read from the schema, never from the
// document.
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
// by default (no timestamp unless [WithCreatedAt] or [WithCreatedAtFrom] is
// used).
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
// the header fields plus the types array. It decodes the header alone, and
// still scans the whole document once to check its top-level shape;
// [HeaderOnlyRead] reads the header alone. Either suits dispatch-style
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
// [WriteFile] writes bytes to a path atomically. It stages the bytes in a
// file of its own beside the path, fsyncs and closes it, then renames it into
// place. Concurrent writers of one path never share a staging file, so each
// rename commits one writer's bytes whole, and the last rename wins. The
// staging name is the path's stem, a random token, the path's extension and
// [TmpSuffix], as snap.12345.ys.tmp for snap.ys.
//
// On an error during the write, WriteFile removes its own staging file and
// returns the error wrapped with the failing step. It never removes a staging
// file it did not create, because that file can belong to a live writer. A
// crash between fsync and rename leaves the staging file behind. A sweep finds
// that residue by [TmpSuffix], or by the extension and [TmpSuffix] together.
//
// The file mode is 0o666 under the process umask, the mode os.Create gives. A
// caller that needs a stricter mode changes it after WriteFile returns.
// WriteFile does not fsync the parent directory, so on some filesystems the
// rename is not durable across a crash. It does not check that the bytes are
// a valid .ys document.
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
// recomputing only the SHA-256 integrity hash. The fast path is
// several times faster than the equivalent Load + Marshal round trip;
// [TestUpdateMetadataRatioFloor] enforces a floor of 3×. Depends
// on the field-order and body-suffix stability contracts documented
// in wire.go; future Marshal-side shape changes must respect those
// contracts or update this primitive in lockstep.
//
// [UpdateMetadataOrReMarshal] is the default consumer entry point:
// it runs [UpdateMetadata] on the happy path and transparently falls
// back to [Load] + [Marshal] on any Error or Fatal but a cancellation
// (a body-offset failure or a malformed header among them), surfacing
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
//   - [WithCreatedAtFrom]: embed the creation timestamp another header states
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
//   - [WithIssueLimit]: maximum issues stored (default 100; 0 for unlimited); the walk always completes
//   - [WithIntegrityCheck]: with false, skip SHA-256 integrity verification (useful for debugging)
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
// The read, write and update functions return [diag.Result]; [WriteFile]
// returns an error, and [ScanDir] and [ScanDirWith] yield one per entry:
//
//   - Fatal: I/O failure, context cancellation, a body the metadata update
//     cannot locate, or a broken invariant (E_INTERNAL)
//   - Error: schema hash mismatch, integrity check failure, structural
//     corruption; and at [Marshal], a value the wire cannot carry, a tree
//     nested past the reader's bound, an indent that is not whitespace, or a
//     type whose schema is outside the entry schema's import closure
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
// Version 4 keys each table row by the declaring schema's NAME rather
// than its source path. A name travels between machines and a path does
// not: one schema text loaded from two directories produced two
// documents carrying an identical schema_hash — [schema.StructuralHash]
// excludes source paths by design — and mutually unreadable type
// tables, so a document written on one machine failed to load against
// the same schema elsewhere with one E_SNAPSHOT_UNKNOWN_TYPE per row.
// Schema names are unique across an import closure, which is what makes
// them sufficient to denote a type.
//
// [MinReadableVersion] names the lowest version this package accepts on
// read paths; the accept range is the closed interval
// [[MinReadableVersion], currentVersion]. The two constants are ONE
// value — this package reads only the version it writes — so widening
// the range is a deliberate act rather than an edit that forgets to.
// Documents outside the range surface an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION] with the observed version and
// the supported range named in the message. A v3 document is refused,
// not migrated; an older reader rejects a v4 document the same way, so
// an operator running an older binary sees a structured diagnostic
// rather than a misread types section. See docs/VERSIONING.md for the
// full pre-1.0 / post-1.0 wire-format policy.
//
// # Thread Safety
//
// All functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	snapshot  ──imports──▶  graph, instance, schema, diag, location, location/path, immutable, internal/value
//
// The instance edge exists for [WithRevalidation], whose re-validation runs
// the real validator, so the option's fidelity is the validator's own, and for
// [instance.MaxComposedDepth], the depth bound the reader shares with it.
package snapshot

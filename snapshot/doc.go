// Package snapshot provides serialization and deserialization of [graph.Snapshot]
// values to and from the yammm snapshot persistence format (.ys).
//
// The .ys format is a JSON-based persistence format that preserves structural
// fidelity: instances with properties, primary keys, edges, compositions, provenance,
// duplicates, and unresolved edge records all survive a Marshal/Load round-trip.
// Provenance survives as source name and path, with a zero span.
//
// The format includes a schema structural hash for compatibility verification, an
// integrity hash for corruption detection, and a features array, which this
// version writes empty and refuses any entry of.
//
// # Validity Contract
//
// Validation happens at [github.com/simon-lentz/yammm/instance.Validator], the
// one source of a [github.com/simon-lentz/yammm/instance.ValidInstance].
//
// The .ys header carries the writing library's attestation of values and
// associations, whose rules [graph.Attestation] states. The integrity hash is an unkeyed SHA-256 of the
// document: it detects corruption and an edit nobody rehashed, save in a document
// whose stored hash is empty, which no check compares. Anyone can
// recompute it, so it stops no deliberate edit: any process can write a document
// whose header claims what its instances never earned and hash it. The unforgeable
// point is the instance layer, and the attestation is the writer's word, not a proof.
//
// Every reader of a document refuses, in the text it reads, what encoding/json
// would silently rewrite to U+FFFD: a string holding invalid UTF-8 or a surrogate
// escape that is half of no pair. [Load], [Verify], [Info], [HeaderOnly] and [UpdateMetadata] judge the text
// of the whole document before its header, with [diag.E_SNAPSHOT_MALFORMED].
// [HeaderOnlyRead] and [ScanDir] read up to the end of the types table and judge
// that text last, with the same code. A fault they find first, such as an
// unsupported version, is the one they report.
//
// [Load] refuses a document whose structure no caller of [graph.Graph.Add]
// could have built: one that breaks a fact of the graph package doc's
// "Structural facts", or holds two roots of a type that can hold one, whose
// stored keys the key rule both accepts at one address ([diag.E_DUPLICATE_PK]).
// No option excuses them, and [Verify] and [Info] run the same checks; a
// schema-less read judges none that needs a schema.
//
// What a .ys can still hold is data a validated graph would not: an instance
// the validator refuses for its values rather than its structure, such as a
// property outside its constraint, a required property absent, or an
// invariant violation. [WithRevalidation] is the option that reports them —
// the real validator, run at load time over each root the structural checks
// left whole, as the option's doc defines it —
// and [WithValueConformance] the narrower
// canonical-form check.
// Duplicates and unresolved records ride the document as data
// ([graph.Snapshot.Duplicates], [graph.Snapshot.Unresolved]); a rejected
// duplicate's payload is outside the attestation, and whether an unresolved
// record's association is required is read from the schema, never from the
// document.
//
// [schema.StructuralHash] is the schema identity the header pins: an
// identity over the rules that decide what instance data is valid.
//
// Programmer errors panic, naming the function: a nil context at every function
// that takes one, and a zero schema or snapshot where one is required. [ScanDir]
// panics when it is called, never when its sequence runs.
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
//	// Read summary metadata and statistics, without the schema
//	info, result := snapshot.Info(ctx, data)
//
//	// Read the header and the types table (dispatch-style workloads)
//	header, result := snapshot.HeaderOnly(ctx, data)
//
//	// Read the header and the types table from an io.Reader (no pre-materialized bytes)
//	header, result := snapshot.HeaderOnlyRead(ctx, file)
//
//	// Compare the header's schema hash against a loaded schema
//	if !header.SchemaHashMatches(s) { /* stale-schema path */ }
//
//	// Write .ys bytes atomically to disk (tmp+fsync+rename)
//	if err := snapshot.WriteFile(path, data); err != nil { /* ... */ }
//
//	// Iterate the .ys files of a directory that ScanDir admits (header and types table, lazy)
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
// [Marshal] serializes a graph.Snapshot to .ys bytes. Output is deterministic
// by default (no timestamp unless [WithCreatedAt] or [WithCreatedAtFrom] is
// used).
//
// [Load] deserializes .ys bytes back to a graph.Snapshot, verifying structural
// integrity and schema compatibility.
//
// [Verify] validates a .ys file without materializing a Snapshot — useful for
// CI pipelines and pre-flight checks.
//
// [Info] reads summary metadata and statistics from a .ys file without loading
// the schema or materializing instance objects. Returns a [SnapshotInfo], which
// holds the schema name, hash, instance counts per type, totals, and integrity
// status among its fields.
// Cost scales with file size (the instance body is scanned to populate counts).
//
// [HeaderOnly] reads header metadata from a .ys file without decoding the
// instance body or verifying the integrity hash. Returns a [HeaderInfo] with
// the header fields, the types array and the file size. It decodes only the header
// and the types table, and its scan of the whole document checks the top-level
// shape, the member names of every object and the text of every string.
// [HeaderOnlyRead] decodes the members up to the types table and that table,
// and no further, though its decoder's buffer can read past them. Either suits dispatch-style
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
// for comparing a header's schema hash against a loaded schema.Schema.
// Use it after [HeaderOnly] or [HeaderOnlyRead] when the dispatch
// decision depends on whether the snapshot was produced under a
// matching schema version.
//
// [WriteFile] writes bytes to a path atomically. It stages the bytes in a
// file of its own beside the file it replaces, fsyncs and closes it, then
// renames it into place. Concurrent writers of one path never share a staging file, so each
// rename commits one writer's bytes whole, and the last rename wins. The
// staging name is the replaced file's stem, a random token, its extension and
// [TmpSuffix], as snap.12345.ys.tmp for snap.ys; through a symbolic link the
// replaced file is the one the link names.
//
// WriteFile evaluates a ".." in the path on its text first, as the schema
// loader evaluates it: the path is cleaned, so a ".." cancels the element before
// it rather than following a symbolic link, and a relative path that climbs out
// of the working directory is made absolute from it as [filepath.Abs] spells
// it. A symbolic link at the path is then followed, as a chain, and the link
// survives: the file it names is written, and a dangling link creates that
// file. A link's target is read as the kernel reads it: on Unix a ".." in the
// target takes the parent of the directory reached on disk, and on Windows it
// is evaluated on the text. The staging file is made in the directory of the
// file the rename replaces.
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
// [ScanDir] iterates the .ys files in a directory that its filtering rule
// admits and yields one [ScanEntry] per file, with the header parsed lazily via
// [HeaderOnlyRead] on demand. The iterator's second yielded value is
// non-nil only for operation-level failures (dir-open, context
// cancellation); per-file failures (corrupt header, a failed open or read)
// surface on [ScanEntry.Result] and iteration continues. A failed stat raises
// nothing and leaves the entry's size and modification time zero. Files whose
// basename ends with [TmpSuffix] are skipped so crash-residual
// staging files are not confused for complete snapshots. A directory name that is
// not empty is cleaned before the read, so each [ScanEntry.Path] names an entry of
// the listed directory. An empty name fails as a missing directory does.
// [ScanDirSlice] is the materializing convenience wrapper.
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
// back to [Load] + [Marshal] on any Error or Fatal (a body-offset failure
// or a malformed header among them) but a cancellation or metadata that is
// not valid UTF-8, which no path can write. A fallback that succeeds
// surfaces a Warning-severity [diag.W_UPDATE_METADATA_FALLBACK] on the
// returned [diag.Result] so operators can observe fallback frequency.
//
// [UpdateMetadata] and [UpdateMetadataOrReMarshal] keep the text of the existing
// created_at, except that the fallback path drops, with a Warning, a value it
// cannot parse.
//
// # Marshal Options
//
// [Marshal] accepts [Option] values:
//
//   - [WithIndent]: pretty-print JSON output with the given indent string
//   - [WithCreatedAt]: embed a creation timestamp in the snapshot
//   - [WithCreatedAtFrom]: embed the creation timestamp another header states
//   - [WithMetadata]: embed key-value metadata, each key and value valid UTF-8
//
// # Load Options
//
// [Load] and [Verify] accept [LoadOption] values:
//
//   - [WithIssueLimit]: maximum issues stored (default 100; 0 for unlimited); the walk completes unless the context is cancelled
//   - [WithIntegrityCheck]: with false, skip SHA-256 integrity verification (useful for debugging)
//   - [WithValueConformance]: report a root's or a composed child's stored Timestamp/Date/UUID values that do not conform to their constraints (Warning)
//   - [WithRevalidation]: run each root the structural checks left whole, with its composed children, edges and target_missing records, back through the real validator, reported at the given severity
//
// # Error Handling
//
// The read, write and update functions return [diag.Result]; [WriteFile]
// returns an error, and [ScanDir] yields a [diag.Result] per entry, in
// [ScanEntry.Result]:
//
//   - Fatal: an I/O failure, a cancelled context, a body the metadata update
//     cannot locate, or a broken invariant (E_INTERNAL)
//   - Error: the input is refused. Among the causes are a schema hash
//     mismatch, an integrity check failure, a structural break, and a
//     version, feature or type this library does not read; at [Marshal], an
//     indent that is not JSON whitespace, or a provenance source name or raw path
//     that is not valid UTF-8; and at [Marshal] and the metadata update, a
//     metadata key or value that is not valid UTF-8
//   - Any severity: a [WithRevalidation] finding takes the severity the
//     caller passed
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
// A document of the expected shape outside the range draws an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION], whose message names the observed
// version and the supported range. Every function that reads a document, save
// [HeaderOnlyRead] and the scans built on it ([ScanDir], [ScanDirSlice]),
// reports one that also breaks the shape or the text rule as
// [diag.E_SNAPSHOT_MALFORMED] instead. A v3 document is refused,
// not migrated; an older reader rejects a v4 document the same way, so
// an operator running an older binary sees a structured diagnostic
// rather than a misread types section. See docs/API.md's Versioning Policy
// for the full pre-1.0 / post-1.0 wire-format policy.
//
// # Thread Safety
//
// All package functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	snapshot  ──imports──▶  graph, instance, schema, diag, location, location/path, immutable, internal/value, internal/hostpath, internal/snapparts
//
// The instance edge exists for [WithRevalidation], whose re-validation runs
// the real validator, so the option's fidelity is the validator's own, and for
// [instance.MaxComposedDepth], the depth bound the reader shares with it.
package snapshot

// Package snapshot provides serialization and deserialization of [graph.Snapshot]
// values to and from the yammm snapshot persistence format (.ys).
//
// The .ys format is a JSON-based persistence format that preserves structural
// fidelity: instances with properties, primary keys, edges, compositions, provenance,
// duplicates, and unresolved edge records all survive a Marshal/Unmarshal round-trip.
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
// document: it detects corruption and an edit nobody rehashed, and a document
// whose header holds no hash fails the check. Anyone can recompute it, so it stops no deliberate edit: any process can write a document
// whose header claims what its instances never earned and hash it. The unforgeable
// point is the instance layer, and the attestation is the writer's word, not a proof.
//
// Every reader of a document refuses, in the text it reads, what encoding/json
// would silently rewrite to U+FFFD: a string holding invalid UTF-8 or a surrogate
// escape that is half of no pair. [Unmarshal], [Verify], [Stat] and [UpdateMetadata] judge the text
// of the whole document before its header, with [diag.E_SNAPSHOT_MALFORMED].
// [ReadHeader] and [ScanDir] read up to the end of the types table and judge
// that text last, with the same code. A fault they find first, such as an
// unsupported version, is the one they report.
//
// [Unmarshal] refuses a document whose structure no caller of [graph.Graph.Add]
// could have built: one that breaks a fact of the graph package doc's
// "Structural facts", or holds two roots of a type that can hold one, whose
// stored keys the key rule both accepts at one address ([diag.E_DUPLICATE_PK]).
// No option excuses them, and [Verify] and [Stat] run the same checks; a
// schema-less read judges none that needs a schema.
//
// What a .ys can still hold is data a validated graph would not: an instance
// the validator refuses for its values rather than its structure, such as a
// property outside its constraint, a required property absent, or an
// invariant violation. [WithRevalidation] is the option that reports them —
// the real validator, run at load time over each root the structural checks
// left whole, as the option's doc defines it.
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
//	data, err := snapshot.Marshal(ctx, snap)
//
//	// Deserialize
//	snap, err := snapshot.Unmarshal(ctx, data, s)
//
//	// Validate without loading
//	err := snapshot.Verify(ctx, data, s)
//
//	// Read the header and a summary of the body, without the schema
//	info, err := snapshot.Stat(ctx, data)
//
//	// Read the header and the types table from an io.Reader (dispatch-style workloads)
//	header, err := snapshot.ReadHeader(ctx, file)
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
//	    if entry.Err != nil { /* per-file failure */ }
//	    use(entry.Header)
//	}
//
//	// Replace the metadata of an existing .ys, keeping every byte outside the header
//	out, err := snapshot.UpdateMetadata(ctx, data, newMeta)
//
// # Functions
//
// [Marshal] serializes a graph.Snapshot to .ys bytes. Output is deterministic
// by default (no timestamp unless [WithCreatedAt] is used).
//
// [Unmarshal] deserializes .ys bytes back to a graph.Snapshot, verifying structural
// integrity and schema compatibility.
//
// [Verify] validates a .ys file without materializing a Snapshot — useful for
// CI pipelines and pre-flight checks.
//
// [Stat] reads a .ys document's header and summarizes its body without loading
// the schema or materializing instance objects. It returns an [Info], which
// embeds the [Header] and holds instance counts per type, totals and the
// integrity status. Its cost scales with the document's size.
//
// [ReadHeader] reads a document's header and types table from an io.Reader,
// at most [MaxHeaderSize] bytes, without decoding the body or verifying the
// integrity hash. It suits dispatch-style workloads that open many .ys files to
// classify state or compare schema hashes; [Header.SchemaHashMatches] is the
// nil-safe comparison of a header's schema hash with a loaded schema.Schema.
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
// it. The cleaning keeps a path that ends in a separator or a "." element
// naming a directory, as a Unix kernel reads it, so no file is written there.
// When its last element is a file, sits under one, or does not exist in an
// existing directory, WriteFile refuses it before staging, on every host, with
// an error matching syscall.ENOTDIR. Under a directory that does not exist, or
// when its stat fails otherwise, it is refused with the host's own error, and an
// existing directory is refused as one spelled without the separator is. A symbolic link at the path is then followed, as a chain,
// and the link survives: the file it names is written, and a dangling link
// creates that file. A link's target is read as the kernel reads it: on Unix a ".." in the
// target takes the parent of the directory reached on disk, and on Windows it
// is evaluated on the text. The same rule judges each link target in
// directory form, at the file the kernel reaches through it. The staging file is made in the directory of the
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
// [ReadHeader] on demand. The iterator's second yielded value is
// non-nil only for operation-level failures (dir-open, context
// cancellation); per-file failures (corrupt header, a failed open or read)
// surface on [ScanEntry.Err] and iteration continues. A failed stat raises
// nothing and leaves the entry's size and modification time zero. Files whose
// basename ends with [TmpSuffix] are skipped so crash-residual
// staging files are not confused for complete snapshots. A directory name that is
// not empty is cleaned before the read, so each [ScanEntry.Path] names an entry of
// the listed directory. An empty name fails as a missing directory does, and a
// cleaned name whose element is not a directory, or sits under one that is not,
// fails with an error matching [syscall.ENOTDIR] on every host; an element a
// ".." cancels is never read.
//
// [UpdateMetadata] replaces the metadata of an existing .ys document and
// recomputes its integrity hash, keeping every byte outside the header. The
// Updating Metadata section below states what it writes and refuses.
//
// # Updating Metadata
//
// [UpdateMetadata] updates every document [Unmarshal] accepts under its
// default options, whatever whitespace stands around its sections, and a
// header of any size: it holds the whole document, so [MaxHeaderSize] does not
// bind it. It splices a new header between the bytes before the header value
// and the bytes after it, at the offsets the header decode ends at, and keeps
// those bytes as they were.
//
// The header it writes holds the members [Marshal] writes, in Marshal's order:
// the version, schema name, schema hash and hash algorithm the input states,
// the new integrity hash, the input's features, the bytes of its created_at as
// written, the new metadata, and the input's attestation. A null created_at
// and an absent attestation are not written. The header is compact, or, when a
// line feed directly follows the opening brace, indented by the whitespace
// between that line feed and the header's name: the indent Marshal wrote. A
// header member the format does not declare, such as the schema_source an
// earlier writer wrote, is dropped.
//
// It refuses, with the code a reader uses for the same fault, a document whose
// shape or text no reader accepts, a header or types table a reader refuses
// without a schema, and a document whose integrity hash does not verify, a
// header that holds no hash among them, unless [WithIntegrityCheck](false)
// skips that check.
// It then refuses metadata that is not valid UTF-8. It decodes no instance: a
// body that a verified hash seals is the body its writer sealed, so a body that
// [Unmarshal] refuses for its instances is updated and still refused.
//
// It checks the document's shape and text, decodes the header, and hashes
// the document twice: once to verify the stored hash and once to seal
// the new header; under WithIntegrityCheck(false) it hashes once. [TestUpdateMetadataRatioFloor] holds it to at most a third of
// the CPU time [Unmarshal] and [Marshal] take on the same document.
//
// # Options
//
// [Marshal], [Unmarshal], [Verify] and [UpdateMetadata] take the one [Option]
// type, and each refuses an option it does not take with an error, with no
// diagnostic code, that names the option and the call. [Stat], [ReadHeader],
// [ScanDir] and [WriteFile] take none.
//
// [Marshal] takes:
//
//   - [WithIndent]: pretty-print JSON output with the given indent string
//   - [WithCreatedAt]: embed a creation timestamp, written as RFC 3339 at its own precision
//   - [WithMetadata]: embed key-value metadata, each key and value valid UTF-8
//
// [Unmarshal] and [Verify] take:
//
//   - [WithIssueLimit]: maximum failures the error holds (default 100; 0 for unlimited); the walk completes unless the context is cancelled
//   - [WithIntegrityCheck]: with false, skip SHA-256 integrity verification (useful for debugging)
//   - [WithRevalidation]: run each root the structural checks left whole, with its composed children, edges and target_missing records, back through the real validator, reported at the given severity
//   - [WithWarnings]: receive each issue below failure severity as it is found
//
// [UpdateMetadata] takes [WithIntegrityCheck].
//
// # Reading Without a Schema
//
// [Stat] returns (nil, err) when the header or the types table breaks a
// rule — an unreadable or malformed header, an unsupported version, an
// unrecognized feature, an unrecognized schema hash algorithm, a types table
// that states one identity twice — when the document's top level is not the
// format's four keys, in order, each once and none null, with nothing after
// them, when an object repeats a member name or spells one the format declares
// in another case, when a string is not valid UTF-8 or holds a surrogate escape
// that is half of no pair, when a section does not decode, or when the context
// is cancelled. A document that decodes but fails a structural check returns an
// [Info] beside an error holding Error-severity diagnostics: an integrity
// mismatch, a header that holds no hash among them, reports [IntegrityMismatch],
// an instances group whose row names no table entry is reported and left out of
// the instance counts, and an edge or a record naming no row is reported and
// still counted. Read the error before the Info.
//
// Stat resolves no schema, so a document it summarizes cleanly can still fail
// [Unmarshal] or [Verify] on the schema hash, on schema resolution, on each check
// of the graph package doc's "Structural facts" that needs a schema, on a
// Timestamp, Date or UUID key component its kind stores as other text, and under
// [WithRevalidation] on the values. Stat compares keys as written, so it reports
// such a component only where it parts two keys that name one address: a
// reference and the root it names as a dangling reference, a duplicate's key and
// the key its instance carries as malformed, and a duplicate's conflict and the
// key the duplicate carries as malformed. Stat runs the checks that need none,
// such as a key component [graph.ParseKey] cannot read back, an undocumented
// reason and two roots whose stored keys it accepts at one address, comparing
// keys as the reader's number rule renders them.
//
// [ReadHeader] refuses a header longer than [MaxHeaderSize] with
// E_SNAPSHOT_MALFORMED and a message naming the limit. It stops at the types
// table: a document whose sections are absent, repeated, out of order, or
// followed by trailing bytes, or whose objects repeat a member name, reads its
// header without complaint, save that a first section other than the header and
// a missing types section are refused. Once the header and the table read
// without a fault, E_SNAPSHOT_MALFORMED refuses a string up to the end of the
// table that is not valid UTF-8 or that holds a surrogate escape that is half of
// no pair. A reader that returns io.EOF or io.ErrUnexpectedEOF, or an error
// wrapping either, partway through the header draws Error-severity
// E_SNAPSHOT_MALFORMED, since a truncated header is a malformed document.
// Another reader error that stops the decode draws Fatal E_SNAPSHOT_IO, the
// first such error as a detail, and a reader error after the types table is not
// read. ctx is checked at entry; a Read call on the reader is not cancellable.
//
// # Error Handling
//
// The read, write and update functions return an error: nil on success, an
// error naming a refused option, or a *[diag.ResultError] whose Result holds
// the failures. Read it with errors.AsType, and test a code with errors.Is.
// [WriteFile] returns its own error, and [ScanDir] yields a per-entry error in
// [ScanEntry.Err]. An issue below failure severity is never in an error: only
// [Unmarshal] and [Verify] draw one, and they send it to [WithWarnings].
//
//   - Fatal: an I/O failure, a cancelled context, or a broken invariant
//     (E_INTERNAL)
//   - Error: the input is refused. Among the causes are a schema hash
//     mismatch, an integrity check failure, a structural break, and a
//     version, feature or type this library does not read; at [Marshal], an
//     indent that is not JSON whitespace, a provenance source name or path
//     that is not valid UTF-8, or a creation time with no RFC 3339 form; at
//     every reader, a created_at that is not an RFC 3339 time or that states
//     a leap second or the zero instant; and at [Marshal]
//     and the metadata update, a metadata key or value that is not valid UTF-8
//   - Any severity: a [WithRevalidation] finding takes the severity the
//     caller passed; below Error it reaches [WithWarnings]
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
// This package reads only the version it writes: the accept range is that one
// version, and widening it is a deliberate act rather than an edit that forgets
// to. A document of the expected shape at another version draws an Error-severity
// [diag.E_SNAPSHOT_UNSUPPORTED_VERSION], whose message names the observed
// version and the supported range. Every function that reads a document, save
// [ReadHeader] and [ScanDir], which is built on it, reports one that also breaks
// the shape or the text rule as
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
//	snapshot  ──imports──▶  graph, instance, schema, diag, location, instance/path, immutable, internal/value, internal/hostpath, internal/snapparts, internal/snapread, internal/jsonstring
//
// The instance edge exists for [WithRevalidation], whose re-validation runs
// the real validator, so the option's fidelity is the validator's own, for
// [instance.NewProvenance], which gives each instance the reader loads its
// provenance, and for [instance.MaxComposedDepth], the depth bound the reader
// shares with it. The internal/snapread edge hands the module's CLI the header
// of the decode that accepted a document, and a header read with no size cap
// over bytes already in memory.
package snapshot

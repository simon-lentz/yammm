package snapshot

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// TypeRef is a type identity as a .ys document states it: declaring schema
// path plus name. It is the schema-less display surface for the Info and
// HeaderOnly readers, which run without an import closure to resolve
// against; two same-named types in different schemas stay distinct. TypeRef
// renders and does not parse: the Info surfaces are a report projection, so
// there is no UnmarshalText and they serialize one way by design.
type TypeRef struct {
	SchemaPath string
	Name       string
}

// String renders the identity as "path#name". The '#' separator is
// deliberate beside [schema.TypeID]'s "path:name": TypeID's rendering is
// byte-order-bearing (the types-table sort rides it), so the display form
// stays visibly distinct rather than moving wire bytes to unify them.
func (r TypeRef) String() string {
	return r.SchemaPath + "#" + r.Name
}

// MarshalText renders the "path#name" form, so a map keyed by TypeRef and a
// TypeRef slice both serialize as strings under encoding/json. There is no
// inverse; [TestTypeRef_IsWriteOnly] pins that as a decision rather than a gap.
func (r TypeRef) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

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

	// Content summary. Types is the whole denotation set; InstanceCounts
	// holds one entry per type the snapshot itself holds.
	Types           []TypeRef
	InstanceCounts  map[TypeRef]int
	TotalInstances  int
	TotalEdges      int
	DuplicateCount  int
	UnresolvedCount int

	// Attestation is the header's validity claim, or nil for a document
	// written before v0.15.0. See [github.com/simon-lentz/yammm/graph.Attestation].
	Attestation *graph.Attestation

	// File metadata.
	FileSize        int64  // len(data)
	IntegrityStatus string // "ok", "mismatch", or "skipped"
}

// Info reads summary metadata and statistics from a .ys file without
// loading the schema or materializing instance objects.
//
// Info uses the shared streamDecoder infrastructure. It holds the decoded
// instances section in memory, so peak usage scales with document size.
//
// Info verifies the integrity hash and reports the result in IntegrityStatus.
// Schema hash cannot be verified (no schema available).
//
// Cost scales with file size because Info decodes the instance sections
// to populate [SnapshotInfo.InstanceCounts] and [SnapshotInfo.TotalEdges].
// For dispatch-style workloads that only need header-level fields, use
// [HeaderOnly] — it returns after parsing the header and skips the
// instance body, making its cost proportional to the header size rather
// than the full file.
//
// Returns (nil, result) when the document cannot be summarized at all: an
// unreadable header, an unsupported version, an unrecognized feature, an
// undecodable section, or a cancelled context. A document that decodes but
// fails a structural check returns a summary beside Error-severity
// diagnostics — an integrity mismatch reports
// IntegrityStatus "mismatch", and a reference naming no table row is reported
// and left out of the counts. Read the result before the summary.
//
// Info runs the same structural validation as [Load] and [Verify] and stops
// before materialization, so the three surfaces classify every document
// identically. It resolves no schema, so a document Info summarizes cleanly
// can still fail Load or Verify on schema resolution — never on structure.
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
	groups, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}
	if err := sd.validateBody(ctx, groups, diags); err != nil {
		return nil, sd.collector.Result()
	}
	counts, totalEdges := sd.countInstances(groups)
	duplicateCount, unresolvedCount := len(diags.Duplicates), len(diags.Unresolved)

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
		Types:               sd.typeRefs(),
		Attestation:         sd.headerAttestation(),
		InstanceCounts:      counts,
		TotalInstances:      totalInstances,
		TotalEdges:          totalEdges,
		DuplicateCount:      duplicateCount,
		UnresolvedCount:     unresolvedCount,
		FileSize:            int64(len(data)),
		IntegrityStatus:     integrityStatus,
	}

	return info, sd.collector.Result()
}

// HeaderInfo contains header metadata extracted from a .ys file without
// decoding the instance body or verifying the integrity hash. It is the
// fast, dispatch-friendly counterpart of [SnapshotInfo].
//
// HeaderInfo uses exported fields rather than accessor methods, matching
// the [SnapshotInfo] convention: it is a read-only data transfer object
// with no invariants to protect.
type HeaderInfo struct {
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

	// Types array (adjacent to the header in the wire format; read in
	// the same streaming pass by decodeHeader).
	Types []TypeRef

	// Attestation is the header's validity claim, or nil for a document
	// written before v0.15.0. See [github.com/simon-lentz/yammm/graph.Attestation].
	Attestation *graph.Attestation

	// FileSize is the document's size in bytes, or zero when unknown. Three
	// cases: [HeaderOnly] reports len(data); [ScanDir] reports the file's size
	// on disk; a bare [HeaderOnlyRead] reports zero, because a size is not
	// knowable from an io.Reader.
	FileSize int64
}

// HeaderOnly reads header metadata from a .ys file without decoding the
// instance body or verifying the integrity hash.
//
// HeaderOnly is the right choice for dispatch-style workloads that scan
// many .ys files to classify lifecycle state, compare schema hashes, or
// inspect metadata annotations like CreatedAt. Its cost is proportional
// to the header size (< 1 KiB for typical .ys files), not the total
// file size — a property that [Info] cannot offer because it populates
// instance counts and diagnostic counts by scanning the body.
//
// HeaderOnly holds the whole document, so it checks the outermost shape:
// sections absent, repeated, out of order, or followed by trailing bytes are
// rejected here. [HeaderOnlyRead] and [ScanDir] read through a capped reader
// and never hold the body, so they cannot make that check — and say so on
// their own documentation rather than accepting a misshapen document
// silently.
//
// Integrity is not verified. The returned [HeaderInfo.IntegrityHash] is
// the value stored in the file, not a verification result. Callers that
// need to confirm the hash matches the document content should use
// [Verify]. Similarly, schema-hash correctness against a loaded schema
// is not checked here — HeaderOnly never loads or consults a schema.
// The [HeaderInfo.SchemaHashMatches] helper performs that cross-check
// at the dispatch site when needed.
//
// For anything that needs instance counts, diagnostic counts, or a
// verified integrity status, use [Info] instead. For callers reading
// from an io.Reader (e.g., an os.File) without first materializing the
// full document into memory, use [HeaderOnlyRead].
//
// Returns (nil, result) with Error-severity diagnostics for malformed
// input. Follows the library's standard (T, diag.Result) return pattern.
func HeaderOnly(ctx context.Context, data []byte) (*HeaderInfo, diag.Result) {
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	// HeaderOnly holds the whole document, so it owes the outermost-shape check
	// every other whole-document surface runs. HeaderOnlyRead and ScanDir do
	// not hold it and say so on their own godoc.
	c := diag.NewCollector(0)
	if err := checkTopLevelKeys(data); err != nil {
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, c.Result()
	}

	sd := newStreamDecoder(data, nil, loadConfig{headerOnly: true})

	// Decode header + types only — no instance body, no integrity check.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	info := &HeaderInfo{
		Version:             sd.header.Version,
		Features:            sd.header.Features,
		SchemaName:          sd.header.SchemaName,
		SchemaSource:        sd.header.SchemaSource,
		SchemaHash:          sd.header.SchemaHash,
		SchemaHashAlgorithm: sd.header.SchemaHashAlgorithm,
		IntegrityHash:       sd.header.IntegrityHash,
		CreatedAt:           sd.header.CreatedAt,
		Metadata:            sd.header.Metadata,
		Types:               sd.typeRefs(),
		Attestation:         sd.headerAttestation(),
		FileSize:            int64(len(data)),
	}

	return info, sd.collector.Result()
}

// MaxHeaderSize is the upper bound on bytes [HeaderOnlyRead] will read
// from an io.Reader before returning E_SNAPSHOT_MALFORMED. The limit
// protects against malformed or malicious inputs; .ys files whose header
// section exceeds this bound are rejected rather than consuming unbounded
// memory during dispatch.
//
// 16 MiB accommodates typical .ys headers (< 1 KiB) with substantial
// margin for callers that carry large work-set arrays in header metadata —
// for example a consumer's state-batched pipelines, which persist target_keys /
// processed_keys / failed_keys per batch and whose densest states produce
// headers in the 100 KiB–1 MiB range. Callers whose headers legitimately
// exceed even this bound should split metadata across multiple documents
// or use [Info] / [Load] (which do not cap input size) against a
// pre-materialized byte slice.
const MaxHeaderSize = 16 * 1024 * 1024

// HeaderOnlyRead reads header metadata from a .ys stream without
// materializing the instance body. Equivalent to [HeaderOnly] but accepts
// an io.Reader, avoiding the caller-side requirement to read the entire
// document into memory before dispatch.
//
// HeaderOnlyRead reads at most [MaxHeaderSize] bytes from r. Inputs whose
// header section exceeds this bound are rejected with E_SNAPSHOT_MALFORMED
// and a message naming the limit, so operators can distinguish the cap
// from a generic JSON-parse failure. Integrity is not verified (matching
// [HeaderOnly]).
//
// Schema cross-check: HeaderOnlyRead takes no schema parameter and performs
// no schema-hash verification during the read, matching [HeaderOnly]. Dispatch callers compare
// [HeaderInfo.SchemaHash] against their loaded schema themselves via the
// [HeaderInfo.SchemaHashMatches] helper — a cheap string equality check
// against schema.StructuralHash(s) that captures intent at dispatch
// sites. Callers that need schema-hash verification inside the read call
// should materialize the bytes first and use [HeaderOnly] or [Info].
//
// Read-error handling: a reader that returns io.EOF,
// io.ErrUnexpectedEOF, or any other error partway through the header
// surfaces as E_SNAPSHOT_MALFORMED on the returned diag.Result, not as
// a bare error return. This preserves the library's uniform diagnostic
// surface: a truncated header is a malformed document, whether the
// truncation is on disk or in transit.
//
// ctx cancellation is checked at function entry; individual Read calls
// on r are not cancellable mid-read. Readers backed by slow or network
// I/O should apply their own per-read deadline wrapping if finer-grained
// cancellation is required.
//
// Cost: proportional to the actual header size (typically < 1 KiB), not
// the underlying file size. The returned [HeaderInfo.FileSize] is not
// populated (zero value) in the reader variant — a size is not knowable
// from an io.Reader. Consumers that need one use the []byte form via
// [HeaderOnly], or [ScanDir], which stats the handle it opened and fills
// the field in for every entry it yields.
//
// Shape: HeaderOnlyRead reads through a reader capped at [MaxHeaderSize] and
// never holds the body, so it cannot check the document's outermost shape. A
// document whose sections are absent, repeated, out of order, or followed by
// trailing bytes reads its header here without complaint. It does not accept
// such a document by omission — it does not see it. Use [HeaderOnly], [Verify]
// or [Load] when that matters.
//
// Returns (nil, result) with Error-severity diagnostics for malformed
// input. Follows the library's standard (T, diag.Result) return pattern.
//
// See also [ScanDir] for directory-wide iteration that uses this
// primitive per file.
func HeaderOnlyRead(ctx context.Context, r io.Reader) (*HeaderInfo, diag.Result) {
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	lr := newLimitReader(r, MaxHeaderSize)
	sd := newStreamDecoderFromReader(lr, nil, loadConfig{headerOnly: true})

	// Decode header + types only — no instance body, no integrity check.
	if err := sd.decodeHeader(); err != nil {
		msg := err.Error()
		if lr.exceeded {
			msg = fmt.Sprintf("header exceeded MaxHeaderSize (%d bytes)", MaxHeaderSize)
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, msg).Build())
		return nil, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	info := &HeaderInfo{
		Version:             sd.header.Version,
		Features:            sd.header.Features,
		SchemaName:          sd.header.SchemaName,
		SchemaSource:        sd.header.SchemaSource,
		SchemaHash:          sd.header.SchemaHash,
		SchemaHashAlgorithm: sd.header.SchemaHashAlgorithm,
		IntegrityHash:       sd.header.IntegrityHash,
		CreatedAt:           sd.header.CreatedAt,
		Metadata:            sd.header.Metadata,
		Types:               sd.typeRefs(),
		Attestation:         sd.headerAttestation(),
		// FileSize intentionally left 0: not known from an io.Reader.
	}

	return info, sd.collector.Result()
}

// headerAttestation converts the decoded header's claim for the Info
// surfaces. Nil means the document was written before v0.15.0.
func (sd *streamDecoder) headerAttestation() *graph.Attestation {
	a := sd.header.Attestation
	if a == nil {
		return nil
	}
	return &graph.Attestation{Values: a.Values, Associations: a.Associations}
}

// CreatedAtTime parses the header's CreatedAt field.
//
// It reports false when CreatedAt is empty and when it is malformed, because a
// caller that only wants the timestamp cannot act on the difference and a
// caller that guesses picks the zero time, which sorts first and silently
// corrupts any chronological ordering. A caller that must tell absent from
// corrupt tests CreatedAt != "" first, then calls this.
//
// The layout is RFC 3339. yammm writes UTC at second precision, so a time
// passed to [WithCreatedAt] does not round-trip its sub-second part. The parser
// accepts more than yammm writes, deliberately: [UpdateMetadata] preserves a
// foreign header's bytes, and fractional seconds and non-UTC offsets both parse
// under this layout and keep their offset in the result.
func (h *HeaderInfo) CreatedAtTime() (time.Time, bool) {
	if h == nil {
		return time.Time{}, false
	}
	return parseCreatedAt(h.CreatedAt)
}

// SchemaHashMatches reports whether the header's SchemaHash equals the
// structural hash of s under the same algorithm version. This is the
// documented cross-check dispatch callers perform after [HeaderOnly] or
// [HeaderOnlyRead] to detect snapshots produced under a different schema
// version — e.g., a consumer's stale-schema path.
//
// The comparison is a cheap string equality against
// schema.StructuralHash(s); the helper exists so the cross-check is a
// single method call that signals intent at dispatch sites and keeps the
// "forgot to compare" foot-gun out of consumer code paths.
//
// Nil-safety: SchemaHashMatches returns false without panicking when
// either the receiver or s is nil, when the header's SchemaHash is
// empty, when the header's SchemaHashAlgorithm does not match
// [schema.StructuralHashVersion] (a hash produced by a different
// algorithm is not comparable, even if the string happens to match), or
// when schema.StructuralHash(s) returns the empty string. An empty or
// version-mismatched SchemaHash never matches anything; dispatch callers
// treat the false return as "unknown or incompatible schema, do not
// proceed" and surface a structured error rather than silently
// continuing.
func (h *HeaderInfo) SchemaHashMatches(s *schema.Schema) bool {
	if h == nil || s == nil || h.SchemaHash == "" {
		return false
	}
	if h.SchemaHashAlgorithm != schema.StructuralHashVersion {
		return false
	}
	hash := schema.StructuralHash(s)
	if hash == "" {
		return false
	}
	return h.SchemaHash == hash
}

// UnknownTypes returns the header's types-table rows that s's import closure
// does not declare. An empty result means every row binds at [Load].
//
// It is the complement of [HeaderInfo.SchemaHashMatches], and dispatch callers
// want both: the hash catches a changed schema shape under one source path, and
// this catches the same shape under a changed path. A snapshot written against
// one schema layout and read against another passes the hash — StructuralHash
// hashes names and never source paths — then fails at [Load] with one
// E_SNAPSHOT_UNKNOWN_TYPE per row. Both checks run on a header-only read, before
// any body decode.
//
// Nil-safety: a nil receiver returns nil, and a nil schema returns every row,
// because a closure that declares nothing declares no row.
//
// [SnapshotInfo] carries the same Types rows and deliberately has no such
// method: a caller holding one has already paid the full decode, which is the
// cost this method exists to avoid.
func (h *HeaderInfo) UnknownTypes(s *schema.Schema) []TypeRef {
	if h == nil {
		return nil
	}
	return unknownTypeRows(h.Types, s)
}

// CreatedAtTime parses the header's CreatedAt field. See
// [HeaderInfo.CreatedAtTime] for the empty-versus-malformed rule and the
// precision the writers commit to.
func (s SnapshotInfo) CreatedAtTime() (time.Time, bool) {
	return parseCreatedAt(s.CreatedAt)
}

// parseCreatedAt is the one CreatedAt parser, so the two carriers cannot
// disagree on what counts as a usable timestamp.
func parseCreatedAt(createdAt string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

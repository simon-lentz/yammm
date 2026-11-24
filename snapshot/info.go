package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// TypeRef is a type identity as a .ys document states it: declaring schema
// name plus type name. It is the schema-less display surface for [Stat] and
// [ReadHeader], which run without an import closure to resolve against; two same-named types in different schemas stay distinct because
// schema names are unique across a closure. TypeRef renders and does not
// parse: [Header] and [Info] are a report projection, so there is no
// UnmarshalText and they serialize one way by design.
type TypeRef struct {
	Schema string
	Name   string
}

// String renders the identity as "schema#name". It is the ONE rendering of a
// type identity in this package: the types table stores it, the reader's
// diagnostics attach it, and the writer's attach it too — a document and the
// two halves that produce and consume it cannot disagree on how a type is
// named.
func (r TypeRef) String() string {
	return r.Schema + "#" + r.Name
}

// MarshalText renders the "schema#name" form, so a map keyed by TypeRef and a
// TypeRef slice both serialize as strings under encoding/json. Its error is
// always nil. There is no inverse; [TestTypeRef_IsWriteOnly] pins that as a
// decision rather than a gap.
func (r TypeRef) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// Header is the metadata a .ys document's header and types table state. It is
// a read-only data transfer object with no invariants to protect, so its fields
// are exported. [ReadHeader] returns it, and [Info] embeds it.
type Header struct {
	Version             int
	SchemaName          string
	SchemaHash          string
	SchemaHashAlgorithm int
	IntegrityHash       string            // as stored, not a verification result
	CreatedAt           time.Time         // zero when the header states none
	Metadata            map[string]string // user-provided annotations, nil if absent

	// Types holds one entry per types-table row.
	Types []TypeRef

	// Attestation is the header's validity claim, or nil for a document that
	// states none. See [github.com/simon-lentz/yammm/graph.Attestation].
	Attestation *graph.Attestation
}

// Info is a .ys document's header and a summary of its body, as [Stat] reads
// them. InstanceCounts holds one entry per type an instances group names whose
// row resolves: the number of its roots.
type Info struct {
	Header

	InstanceCounts  map[TypeRef]int
	TotalInstances  int
	TotalEdges      int
	DuplicateCount  int
	UnresolvedCount int

	IntegrityStatus IntegrityStatus
}

// IntegrityStatus is the result of [Stat]'s integrity-hash check. The zero
// IntegrityStatus names no status.
type IntegrityStatus uint8

const (
	// IntegrityOK: the stored hash matches the document's content.
	IntegrityOK IntegrityStatus = iota + 1
	// IntegrityMismatch: the stored hash does not match the content, or the
	// header holds no hash.
	IntegrityMismatch
)

// String returns "ok" or "mismatch", and IntegrityStatus(n) for any other
// value.
func (s IntegrityStatus) String() string {
	switch s {
	case IntegrityOK:
		return "ok"
	case IntegrityMismatch:
		return "mismatch"
	default:
		return fmt.Sprintf("IntegrityStatus(%d)", uint8(s))
	}
}

// Stat reads a .ys document's header and summarizes its body without a schema
// and without materializing an instance, so its cost scales with the document's
// size. It runs each structural check [Unmarshal] runs that needs no schema,
// records the integrity check in [Info.IntegrityStatus], and its error, a
// *[diag.ResultError], holds at most 100 failures. It returns (nil, err) for a
// document it cannot decode, one whose header or types table breaks a rule and
// a cancelled context, and an Info beside the error for a document that decodes
// and whose body breaks a check, as the package doc's Reading Without a Schema
// section states. It draws no warning.
func Stat(ctx context.Context, data []byte) (*Info, error) {
	if ctx == nil {
		panic("snapshot.Stat: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelled(err)
	}

	sd := newStreamDecoder(data, schema.Schema{}, newConfig(nil))
	if err := checkDocument(data); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Err()
	}

	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Err()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Err()
	}

	groups, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Err()
	}
	integrityStatus, err := sd.validateBody(ctx, groups, diags)
	if err != nil {
		return nil, sd.collector.Err()
	}
	counts, totalEdges := sd.countInstances(groups)

	totalInstances := 0
	for _, c := range counts {
		totalInstances += c
	}

	return &Info{
		Header:          sd.decodedHeader(),
		InstanceCounts:  counts,
		TotalInstances:  totalInstances,
		TotalEdges:      totalEdges,
		DuplicateCount:  len(diags.Duplicates),
		UnresolvedCount: len(diags.Unresolved),
		IntegrityStatus: integrityStatus,
	}, sd.collector.Err()
}

// MaxHeaderSize is the upper bound on bytes [ReadHeader] reads before it
// returns E_SNAPSHOT_MALFORMED. 16 MiB holds a typical header (< 1 KiB) and
// large work-set arrays in header metadata. A caller whose header exceeds it
// reads the bytes with [Stat] or [Unmarshal] and updates them with
// [UpdateMetadata], none of which caps its input.
const MaxHeaderSize = 16 * 1024 * 1024

// ReadHeader reads a .ys document's header and types table from r, at most
// [MaxHeaderSize] bytes, without decoding the body or verifying the integrity
// hash. It stops at the types table, so it judges neither the document's
// outermost shape nor its member names beyond refusing a top level that is no
// JSON object, a first section other than the header and a missing types
// section, nor the text of a string past the table.
// It accepts a schema hash algorithm this package does not read and draws no
// warning for it: [Header.SchemaHashAlgorithm] states the algorithm, and
// [Header.SchemaHashMatches] compares the header with a loaded schema. A header
// it refuses returns (nil, err), err a *[diag.ResultError]. The package doc's
// Reading Without a Schema section states what it reads, and its Error Handling
// section the header refusals every reader makes.
func ReadHeader(ctx context.Context, r io.Reader) (*Header, error) {
	if ctx == nil {
		panic("snapshot.ReadHeader: nil context")
	}
	return readHeader(ctx, r, MaxHeaderSize)
}

// readHeader is [ReadHeader] with limit in place of [MaxHeaderSize].
func readHeader(ctx context.Context, r io.Reader, limit int64) (*Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, cancelled(err)
	}

	lr := newLimitReader(r, limit)
	cfg := newConfig(nil)
	cfg.headerOnly = true
	// The decoder keeps no bytes it has decoded, so the text rule reads this copy.
	var read bytes.Buffer
	sd := newStreamDecoderFromReader(io.TeeReader(lr, &read), schema.Schema{}, cfg)

	if err := sd.decodeHeader(); err != nil {
		if lr.readErr != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_SNAPSHOT_IO,
				"read header: "+lr.readErr.Error()).
				WithDetail(diag.DetailKeyDetail, lr.readErr.Error()).
				Build())
			return nil, sd.collector.Err()
		}
		msg := err.Error()
		if lr.exceeded {
			msg = fmt.Sprintf("header exceeded MaxHeaderSize (%d bytes)", MaxHeaderSize)
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, msg).Build())
		return nil, sd.collector.Err()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Err()
	}
	if err := checkText(read.Bytes()[:sd.typesEnd]); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Err()
	}

	h := sd.decodedHeader()
	return &h, nil
}

// decodedHeader returns the header and types table sd decoded.
func (sd *streamDecoder) decodedHeader() Header {
	return Header{
		Version:             sd.header.Version,
		SchemaName:          sd.header.SchemaName,
		SchemaHash:          sd.header.SchemaHash,
		SchemaHashAlgorithm: sd.header.SchemaHashAlgorithm,
		IntegrityHash:       sd.header.IntegrityHash,
		CreatedAt:           sd.createdAt,
		Metadata:            sd.header.Metadata,
		Types:               sd.typeRefs(),
		Attestation:         sd.headerAttestation(),
	}
}

// headerAttestation converts the decoded header's claim for [Header]. Nil means the document states no claim, as Marshal writes one of a
// snapshot that holds none.
func (sd *streamDecoder) headerAttestation() *graph.Attestation {
	a := sd.header.Attestation
	if a == nil {
		return nil
	}
	return &graph.Attestation{Values: a.Values, Associations: a.Associations}
}

// SchemaHashMatches reports whether the header's SchemaHash equals
// schema.StructuralHash(s), which hashes s's import closure on each call. It
// returns false for a nil receiver, a zero s, an empty SchemaHash, or a
// SchemaHashAlgorithm other than [schema.StructuralHashVersion], so false means
// an unknown or incompatible schema.
func (h *Header) SchemaHashMatches(s schema.Schema) bool {
	if h == nil || s.IsZero() || h.SchemaHash == "" {
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

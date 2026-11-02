package snapshot

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// UpdateMetadata rewrites the header of an existing .ys document with a
// new metadata map, reusing the body bytes verbatim and recomputing only
// the integrity hash.
//
// Invariants:
//
//   - The body (types, instances, diagnostics sections) is NOT
//     re-serialized. The input bytes' body byte-range is reused
//     unchanged.
//   - The returned document's integrity hash is computed over the new
//     canonical form (new header + existing body) and, for a body in
//     [Marshal]'s bytes, matches what [Marshal] would produce for the same
//     logical content, byte-for-byte.
//   - CreatedAt keeps the existing header's decoded text, written as [Marshal]
//     writes it. A header Marshal wrote keeps its bytes, and an empty or null
//     created_at leaves no member.
//   - Metadata replaces the existing map entirely; the caller is
//     responsible for including pass-through fields in newMeta. There
//     is no merge — intentionally. A merge would require a diff
//     vocabulary, and the caller already has the old metadata via
//     [HeaderOnly].
//   - Indentation is preserved from the input. UpdateMetadata does not
//     re-indent, because re-indenting would require re-serializing the
//     body and defeat the performance premise. Byte-identical round-trip
//     is guaranteed ONLY for inputs produced by Marshal (with any
//     supported Option combination). A hand-edited input keeps its body's
//     bytes; its header is written again in the indent that follows a
//     newline directly after the opening brace, before the "yammm_snapshot"
//     key, or compact otherwise. Whitespace before the opening brace draws
//     Error E_SNAPSHOT_MALFORMED, and whitespace between the header's closing
//     brace and the comma after it Fatal E_UPDATE_METADATA_BODY_OFFSET;
//     [UpdateMetadataOrReMarshal] falls back to [Load] and [Marshal] for
//     either input, unless newMeta holds a key or value that is not valid
//     UTF-8.
//
// Wire-format precondition. UpdateMetadata depends on the field-order
// and body-suffix stability contracts documented in the comment block
// that follows wire.go's imports. Future changes to those contracts must update or remove this
// primitive in lockstep.
//
// Intended caller pattern:
//
//	data, err := os.ReadFile(ysPath)
//	if err != nil { return err }
//	header, res := snapshot.HeaderOnly(ctx, data)
//	if res.HasErrors() { return res.Err() }
//	newMeta := maps.Clone(header.Metadata)
//	if newMeta == nil { newMeta = map[string]string{} }
//	newMeta["pipeline_completed"] = "true"
//	out, res := snapshot.UpdateMetadata(ctx, data, newMeta)
//
// UpdateMetadata copies the body bytes verbatim and decodes no instance: it
// re-verifies no integrity hash, resolves no schema, and of the body it
// decodes the types table alone. It does refuse every input the failure modes
// below name, because the output carries a freshly computed integrity hash, and a
// document those modes name must not come back with a hash that matches. Callers needing
// more than that pair UpdateMetadata with a prior [Verify] on the same bytes;
// [HeaderOnly] is not that check, because it decodes no instance either.
//
// Diagnostics are capped at the same default [Load] uses (100); this surface
// takes no option that raises it.
//
// Failure modes and error codes:
//
//   - [diag.E_SNAPSHOT_MALFORMED] — the first byte of the input is not '{', the
//     header or the types table does not parse, the header states no features
//     field or a null one, the top level is not one closed JSON object holding the
//     four keys the format fixes, in their fixed order, each once, none null and
//     each valid JSON, with nothing after it, the types table states one identity
//     on two rows, an object repeats a member name or spells one the format declares
//     in another case, a string of the document is not valid UTF-8 or holds a
//     surrogate escape that is half of no pair, or a key or value of newMeta is not
//     valid UTF-8.
//   - [diag.E_SNAPSHOT_UNSUPPORTED_FEATURE] — the header names a feature this
//     version does not implement. Refused rather than carried through, because
//     the output would claim support it does not have.
//   - [diag.E_SNAPSHOT_UNSUPPORTED_VERSION] — the header states a version
//     no read path accepts. The document is refused rather than
//     relabelled; UpdateMetadata is a header rewrite, never a migration.
//   - [diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM] — the header states a
//     schema hash algorithm this version does not implement. Refused for the
//     same reason as an unsupported version: the output would carry a schema
//     identity this library cannot check.
//   - [diag.E_UPDATE_METADATA_BODY_OFFSET] — the header parsed cleanly
//     but the body-boundary tracking could not resolve a valid byte
//     range for the body. This indicates the input does not match the
//     shape Marshal produces. Byte-identical recovery is not possible;
//     consumers using [UpdateMetadataOrReMarshal] get automatic
//     Load + Marshal fallback, save for newMeta that is not valid UTF-8,
//     and a W_UPDATE_METADATA_FALLBACK warning when the fallback succeeds.
//   - [diag.E_CONTEXT_CANCELLED] — ctx was cancelled at entry.
//
// Thread safety: UpdateMetadata is a pure function over its inputs.
// It is safe for concurrent use; it does not mutate data or newMeta.
//
// Metadata-key convention (non-enforced): the library treats a key as an opaque
// string and requires only that it be valid UTF-8, and the CLI's --metadata and
// --set also refuse an empty key and cannot spell one holding '='. Consumers using structured metadata should namespace their
// keys (e.g., myapp.pipeline_completed) to avoid collisions with
// other metadata consumers who share a .ys file.
func UpdateMetadata(
	ctx context.Context,
	data []byte,
	newMeta map[string]string,
) ([]byte, diag.Result) {
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	if len(data) < 2 || data[0] != '{' {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			"snapshot.UpdateMetadata: input is not a yammm snapshot (missing opening '{')").Build())
		return nil, c.Result()
	}

	// The output carries a freshly computed integrity hash, so a shape no
	// read path accepts must not be blessed here. The shape and member-name
	// checks read tokens and bytes and decode no value, so the body stays
	// undecoded and the trust-the-body contract holds.
	if err := checkDocument(data); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: %v", err)).Build())
		return nil, c.Result()
	}

	// Decode the existing header via the streaming decoder. We skip the
	// integrity check: UpdateMetadata's contract is "trust caller-provided
	// body bytes"; a fresh hash is computed at write time.
	loadCfg := defaultLoadConfig()
	loadCfg.skipIntegrityCheck = true
	sd := newStreamDecoder(data, schema.Schema{}, loadCfg)
	if err := sd.decodeHeader(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: header parse failed: %v", err)).Build())
		return nil, c.Result()
	}
	// decodeHeader reports a rejected version or features field on the collector
	// and returns nil, so the error alone would accept unreadable documents.
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	// Resolve the body offset. decodeHeader captures dec.InputOffset()
	// immediately after the header object's closing '}'; in Marshal-
	// produced output the byte at that offset is the ',' preceding the
	// types key.
	bodyOffset := sd.bodyOffset
	if bodyOffset <= 0 || bodyOffset >= int64(len(data)) {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_UPDATE_METADATA_BODY_OFFSET,
			"snapshot.UpdateMetadata: body-boundary offset could not be resolved").Build())
		return nil, c.Result()
	}
	if data[bodyOffset] != ',' {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_UPDATE_METADATA_BODY_OFFSET,
			fmt.Sprintf("snapshot.UpdateMetadata: expected ',' at body offset %d, got %q — input does not match Marshal-produced shape",
				bodyOffset, data[bodyOffset])).Build())
		return nil, c.Result()
	}

	// Judged once the document is: a fault of the document is reported first.
	if err := invalidMetadata(newMeta); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: %v", err)).Build())
		return nil, c.Result()
	}

	// Detect input indentation.
	indent := detectIndent(data)

	// Build the header prefix pattern exactly as assembleCompact /
	// assembleIndented does (see marshal.go). The prefix is the opening
	// '{' plus the "yammm_snapshot" key (and its ": " separator for
	// indented mode); the header value (the inner object) is inserted
	// next, and the body suffix completes the document.
	var prefix string
	if indent != "" {
		prefix = "{\n" + indent + `"yammm_snapshot": `
	} else {
		prefix = `{"yammm_snapshot":`
	}

	// Non-nil: decodeHeader reports a nil features field and the gate above
	// returns on it, so no document reaching here carries one.
	features := sd.header.Features

	// Normalize newMeta: empty maps round-trip as omitempty (no key emitted),
	// matching WithMetadata's shallow-copy-or-nil normalization at
	// option.go:57-59.
	var meta map[string]string
	if len(newMeta) > 0 {
		meta = maps.Clone(newMeta)
	}

	hdr := marshalHeaderWire{
		SchemaName:    sd.header.SchemaName,
		SchemaSource:  sd.header.SchemaSource,
		SchemaHash:    sd.header.SchemaHash,
		IntegrityHash: "", // canonical-form placeholder; filled in pass 2
		Features:      features,
		CreatedAt:     sd.header.CreatedAt,
		Metadata:      meta,
		// Verbatim, nil included: fabricating a claim on a pre-v0.15.0
		// document would attest to a body nothing here read.
		Attestation: sd.header.Attestation,
	}

	bodySuffix := data[bodyOffset:]

	// Pass 1: build canonical form with empty integrity hash, compute SHA-256.
	headerBytes := buildHeaderBytes(hdr, indent, sd.header.Version, sd.header.SchemaHashAlgorithm)
	hdr.IntegrityHash = sha256Sum([]byte(prefix), headerBytes, bodySuffix)

	// Pass 2: rebuild header with the computed hash and concatenate.
	headerBytes = buildHeaderBytes(hdr, indent, sd.header.Version, sd.header.SchemaHashAlgorithm)
	out := make([]byte, 0, len(prefix)+len(headerBytes)+len(bodySuffix))
	out = append(out, prefix...)
	out = append(out, headerBytes...)
	out = append(out, bodySuffix...)

	return out, sd.collector.Result()
}

// UpdateMetadataOrReMarshal runs [UpdateMetadata] on data; when the fast path
// reports an Error or a Fatal (E_SNAPSHOT_MALFORMED at Error,
// E_UPDATE_METADATA_BODY_OFFSET at Fatal, or another), it transparently falls
// back to [Load] + [Marshal] using s for the Load, unless ctx was cancelled or
// newMeta holds a key or value that is not valid UTF-8 (both below). The
// fallback carries the indent that follows a line feed directly after an opening
// brace at the input's first byte, and any other input comes back compact. When the
// input's created_at parses, the fallback also carries its text across, as the fast
// path keeps it, so the result differs from a direct Marshal only where the input
// document did.
//
// Panics if s is zero (programming error), on every call. Only the fallback reads
// s, and a panic on that path alone hides the bug until a document is malformed.
//
// When the fallback path succeeds, the returned [diag.Result] carries a
// Warning-severity [diag.W_UPDATE_METADATA_FALLBACK], the warnings the Load
// and Marshal legs stored, and one further Warning if the input stated a
// created_at this path could not parse. When its Load fails, the result is Load's alone,
// so no break draws two issues. A cancelled Load may not have reached the break, so it keeps
// the refusal beside its own result. When its Marshal fails, the result is
// the refusal and Marshal's issues. Neither carries a W_UPDATE_METADATA_FALLBACK. The warning's details
// include the distinct Error and Fatal codes that triggered it via
// a comma-joined "triggering_codes" entry so consumers can log or surface
// the transition without inspecting the error chain. Callers who want
// to treat the warning as an error check HasWarnings() or iterate
// BySeverity(Warning); callers who just want the output bytes use the
// returned slice directly.
//
// Prefer this helper for most consumers. The primitive [UpdateMetadata]
// remains available for consumers who genuinely need the strict fast
// path — e.g., benchmarks isolating the fast-path cost, or workflows
// where any Load + Marshal round-trip is operationally unacceptable and
// the caller would rather surface the failure than silently recover.
//
// Cost on the happy path matches [UpdateMetadata]. On the fallback path it
// adds the refused UpdateMetadata and a HeaderOnly to Load + Marshal, which [TestUpdateMetadataRatioFloor] holds at least
// three times slower on a 14.7 MB .ys; the W_UPDATE_METADATA_FALLBACK warning
// makes the transition visible in consumer logs and dashboards.
//
// E_CONTEXT_CANCELLED is not a fallback trigger — it propagates as
// cancellation without re-attempting via the slow path. Nor is a newMeta key
// or value that is not valid UTF-8, which no path can write: the result names
// it once, with E_SNAPSHOT_MALFORMED, beside any refusal of the document.
//
// Thread safety: UpdateMetadataOrReMarshal is a pure function over
// its inputs. It is safe for concurrent use; it does not mutate
// data, newMeta, or s.
func UpdateMetadataOrReMarshal(
	ctx context.Context,
	data []byte,
	newMeta map[string]string,
	s schema.Schema,
) ([]byte, diag.Result) {
	// Checked before the fast path, not inside the fallback that dereferences
	// it: a zero schema is a caller's bug on every input, and only some inputs
	// reach Load. Deferring the panic let a caller run for months and crash on
	// the first document the fast path refused.
	if s.IsZero() {
		panic("snapshot.UpdateMetadataOrReMarshal: zero Schema")
	}
	out, res := UpdateMetadata(ctx, data, newMeta)
	if !res.HasErrors() {
		return out, res
	}
	if hasCancellation(res) {
		return nil, res
	}
	// Metadata no path can write takes no fallback: Marshal would refuse it
	// again after a whole Load. UpdateMetadata names it unless it stopped at
	// the document first.
	if err := invalidMetadata(newMeta); err != nil {
		if strings.Contains(res.String(), err.Error()) {
			return nil, res
		}
		c := diag.NewCollector(0)
		c.Merge(res)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			"snapshot.UpdateMetadataOrReMarshal: "+err.Error()).Build())
		return nil, c.Result()
	}

	triggeringCodes := distinctTriggeringCodes(res)

	// Fall back to Load + Marshal.
	// Load's result stands alone, so no break draws two issues. A cancelled Load
	// may not have reached the break, so the refusal stands beside its result.
	loaded, loadRes := Load(ctx, data, s)
	if loadRes.HasErrors() {
		if hasCancellation(loadRes) {
			return nil, mergeResults(res, loadRes)
		}
		return nil, loadRes
	}

	var fallbackWarnings []diag.Issue
	marshalOpts := []Option{WithMetadata(newMeta)}
	// The fallback must not lose what the input document stated. The primitive
	// rebuilds the header in place and keeps both; Load + Marshal reconstructs
	// it, so the indent and the created_at have to be carried across or a
	// pretty-printed document comes back compact and a stamped one comes back
	// unstamped.
	if indent := detectIndent(data); indent != "" {
		marshalOpts = append(marshalOpts, WithIndent(indent))
	}
	if hdr, hdrRes := HeaderOnly(ctx, data); !hdrRes.HasErrors() && hdr != nil && hdr.CreatedAt != "" {
		// Parsed to know it is RFC 3339, then carried as text: a
		// re-format through time.Time drops fractional seconds and the
		// offset, which the fast path keeps.
		if _, err := time.Parse(time.RFC3339Nano, hdr.CreatedAt); err == nil {
			marshalOpts = append(marshalOpts, withCreatedAtText(hdr.CreatedAt))
		} else {
			// Dropping it silently loses a value the input stated. The
			// primitive keeps the header's text whatever it says. This
			// path cannot, so it reports what it could not carry.
			fallbackWarnings = append(fallbackWarnings, diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_VALUE_DROPPED,
				fmt.Sprintf("input header states created_at %s, which is not RFC 3339; the re-marshalled document carries none", diag.QuoteValue(hdr.CreatedAt))).
				Build())
		}
	}
	reMarshaled, marshalRes := Marshal(ctx, loaded, marshalOpts...)
	if marshalRes.HasErrors() {
		merged := mergeResults(res, marshalRes)
		return nil, merged
	}

	warn := diag.NewIssue(diag.Warning, diag.W_UPDATE_METADATA_FALLBACK,
		"snapshot.UpdateMetadataOrReMarshal: fell back to Load + Marshal after UpdateMetadata refused input").
		WithDetail(diag.DetailKeyTriggeringCodes, strings.Join(triggeringCodes, ",")).
		Build()
	// The legs' own warnings travel with it. A fresh collector holding only
	// this warning discarded everything Load and Marshal reported — a
	// provenance-path fallback on the way in vanished from the caller's view.
	c := diag.NewCollector(0)
	c.Collect(warn)
	for _, iss := range fallbackWarnings {
		c.Collect(iss)
	}
	c.Merge(loadRes)
	c.Merge(marshalRes)
	return reMarshaled, c.Result()
}

// detectIndent sniffs the input's indentation unit. Matches Marshal's
// emission shape: assembleCompact writes `{"yammm_snapshot":...` (q == 1);
// assembleIndented writes `{\n<indent>"yammm_snapshot": ...` (data[1]
// == '\n' and indent == data[2:q]).
func detectIndent(data []byte) string {
	if len(data) < 2 || data[0] != '{' {
		return ""
	}
	// Find the first '"' at position >= 1.
	q := -1
	for i := 1; i < len(data); i++ {
		if data[i] == '"' {
			q = i
			break
		}
	}
	if q < 0 {
		return ""
	}
	if q == 1 {
		return ""
	}
	if q >= 2 && data[1] == '\n' {
		return string(data[2:q])
	}
	// Pathological shape: best-effort compact.
	return ""
}

// hasCancellation reports whether the result carries an
// E_CONTEXT_CANCELLED issue at any severity.
func hasCancellation(r diag.Result) bool {
	for iss := range r.Issues() {
		if iss.Code() == diag.E_CONTEXT_CANCELLED {
			return true
		}
	}
	return false
}

// distinctTriggeringCodes returns the distinct Fatal- and Error-severity codes
// in r, sorted for deterministic detail output: the fallback triggers on
// either, and E_SNAPSHOT_MALFORMED arrives at Error.
func distinctTriggeringCodes(r diag.Result) []string {
	seen := make(map[string]struct{})
	for iss := range r.BySeverity(diag.Fatal) {
		seen[iss.Code().String()] = struct{}{}
	}
	for iss := range r.BySeverity(diag.Error) {
		seen[iss.Code().String()] = struct{}{}
	}
	codes := make([]string, 0, len(seen))
	for c := range seen {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// mergeResults returns a Result holding a's and b's issues. It merges both, so
// a truncated input's dropped issues stay counted and its truncation carries.
func mergeResults(a, b diag.Result) diag.Result {
	c := diag.NewCollector(0)
	c.Merge(a)
	c.Merge(b)
	return c.Result()
}

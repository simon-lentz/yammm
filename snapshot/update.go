package snapshot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// UpdateOption configures [UpdateMetadata] and [UpdateMetadataOrReMarshal].
type UpdateOption func(*updateConfig)

type updateConfig struct {
	createdAtSet bool
	createdAt    time.Time
}

// WithUpdateCreatedAt overrides the created_at header field in the
// resulting document. If not provided, the existing created_at (if any)
// is preserved byte-for-byte from the input header.
func WithUpdateCreatedAt(t time.Time) UpdateOption {
	return func(c *updateConfig) {
		c.createdAtSet = true
		c.createdAt = t
	}
}

func applyUpdateOptions(opts []UpdateOption) updateConfig {
	var cfg updateConfig
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

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
//     canonical form (new header + existing body) and matches what
//     [Marshal] would produce for the same logical content, byte-for-byte.
//   - CreatedAt is preserved from the existing header unless explicitly
//     overridden via [WithUpdateCreatedAt].
//   - Metadata replaces the existing map entirely; the caller is
//     responsible for including pass-through fields in newMeta. There
//     is no merge — intentionally. A merge would require a diff
//     vocabulary, and the caller already has the old metadata via
//     [HeaderOnly].
//   - Indentation is preserved from the input. UpdateMetadata does not
//     re-indent, because re-indenting would require re-serializing the
//     body and defeat the performance premise. Byte-identical round-trip
//     is guaranteed ONLY for inputs produced by Marshal (with any
//     supported Option combination). Hand-edited inputs with pathological
//     whitespace round through without error but with best-effort indent
//     fidelity.
//
// Wire-format precondition. UpdateMetadata depends on the field-order
// and body-suffix stability contracts documented in wire.go's package
// Godoc. Future changes to those contracts must update or remove this
// primitive in lockstep.
//
// Intended caller pattern:
//
//	data, err := os.ReadFile(ysPath)
//	if err != nil { return err }
//	header, res := snapshot.HeaderOnly(ctx, data)
//	if res.HasErrors() { return res.Err() }
//	newMeta := maps.Clone(header.Metadata)
//	newMeta["pipeline_completed"] = "true"
//	var opts []snapshot.UpdateOption
//	if header.CreatedAt != "" {
//	    t, _ := time.Parse(time.RFC3339, header.CreatedAt)
//	    opts = append(opts, snapshot.WithUpdateCreatedAt(t))
//	}
//	out, res := snapshot.UpdateMetadata(ctx, data, newMeta, opts...)
//
// UpdateMetadata copies the body bytes verbatim and does not validate what
// they contain: it re-verifies no integrity hash, resolves no schema, and
// reads no instance. It does read the types table, because that section is
// decoded with the header, and it refuses a table that states one identity
// twice — the output carries a freshly computed integrity hash, so accepting
// a table no read path accepts would hand back a malformed document that
// passes verification. Callers needing more than that pair UpdateMetadata
// with a prior [HeaderOnly] call (cheap structural validation) or [Verify]
// (full verification) on the same bytes.
//
// Failure modes and error codes:
//
//   - [diag.E_SNAPSHOT_MALFORMED] — the input header does not parse, or its
//     types table states one identity on two rows.
//   - [diag.E_SNAPSHOT_UNSUPPORTED_VERSION] — the header states a version
//     no read path accepts. The document is refused rather than
//     relabelled; UpdateMetadata is a header rewrite, never a migration.
//   - [diag.E_UPDATE_METADATA_BODY_OFFSET] — the header parsed cleanly
//     but the body-boundary tracking could not resolve a valid byte
//     range for the body. This indicates the input does not match the
//     shape Marshal produces. Byte-identical recovery is not possible;
//     consumers using [UpdateMetadataOrReMarshal] get automatic
//     Load + Marshal fallback with a paired W_UPDATE_METADATA_FALLBACK
//     warning.
//   - [diag.E_CONTEXT_CANCELLED] — ctx was cancelled at entry.
//
// Thread safety: UpdateMetadata is a pure function over its inputs.
// It is safe for concurrent use; it does not mutate data or newMeta.
//
// Metadata-key convention (non-enforced): yammm treats keys as opaque
// strings. Consumers using structured metadata should namespace their
// keys (e.g., myapp.pipeline_completed) to avoid collisions with
// other metadata consumers who share a .ys file.
func UpdateMetadata(
	ctx context.Context,
	data []byte,
	newMeta map[string]string,
	opts ...UpdateOption,
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

	// Decode the existing header via the streaming decoder. We skip the
	// integrity check: UpdateMetadata's contract is "trust caller-provided
	// body bytes"; a fresh hash is computed at write time.
	sd := newStreamDecoder(data, nil, loadConfig{skipIntegrityCheck: true})
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

	// Apply option overrides and preserve/override CreatedAt.
	cfg := applyUpdateOptions(opts)
	createdAt := sd.header.CreatedAt // byte-for-byte preserve by default
	if cfg.createdAtSet {
		createdAt = cfg.createdAt.UTC().Format(time.RFC3339)
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
		CreatedAt:     createdAt,
		Metadata:      meta,
	}

	bodySuffix := data[bodyOffset:]

	// Pass 1: build canonical form with empty integrity hash, compute SHA-256.
	headerBytes := buildHeaderBytes(hdr, indent, sd.header.Version, sd.header.SchemaHashAlgorithm)
	canon := make([]byte, 0, len(prefix)+len(headerBytes)+len(bodySuffix))
	canon = append(canon, prefix...)
	canon = append(canon, headerBytes...)
	canon = append(canon, bodySuffix...)

	h := sha256.Sum256(canon)
	hdr.IntegrityHash = fmt.Sprintf("sha256:%x", h)

	// Pass 2: rebuild header with the computed hash and concatenate.
	headerBytes = buildHeaderBytes(hdr, indent, sd.header.Version, sd.header.SchemaHashAlgorithm)
	out := make([]byte, 0, len(prefix)+len(headerBytes)+len(bodySuffix))
	out = append(out, prefix...)
	out = append(out, headerBytes...)
	out = append(out, bodySuffix...)

	return out, sd.collector.Result()
}

// UpdateMetadataOrReMarshal runs [UpdateMetadata] on data; on any failure
// that indicates the input is not Marshal-shaped
// (E_UPDATE_METADATA_BODY_OFFSET, E_SNAPSHOT_MALFORMED, or any other
// Fatal-severity issue that is NOT E_CONTEXT_CANCELLED), transparently
// falls back to [Load] + [Marshal] using s for the Load. The fallback
// result is byte-identical to what a direct Marshal call would
// produce — the correctness floor for the primitive.
//
// When the fallback path fires, the returned [diag.Result] carries one
// Warning-severity issue with code [diag.W_UPDATE_METADATA_FALLBACK].
// Its details include the original triggering Fatal code(s) via a
// comma-joined "triggering_codes" entry so consumers can log or surface
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
// Cost on the happy path matches [UpdateMetadata] (~20-40 ms on a
// 20 MB .ys). On the fallback path, cost matches Load + Marshal
// (~1 s on the same file); the W_UPDATE_METADATA_FALLBACK warning makes
// the transition visible in consumer logs and dashboards.
//
// E_CONTEXT_CANCELLED is not a fallback trigger — it propagates as
// cancellation without re-attempting via the slow path.
//
// Thread safety: UpdateMetadataOrReMarshal is a pure function over
// its inputs. It is safe for concurrent use; it does not mutate
// data, newMeta, or s.
func UpdateMetadataOrReMarshal(
	ctx context.Context,
	data []byte,
	newMeta map[string]string,
	s *schema.Schema,
	opts ...UpdateOption,
) ([]byte, diag.Result) {
	out, res := UpdateMetadata(ctx, data, newMeta, opts...)
	if !res.HasErrors() {
		return out, res
	}
	if hasCancellation(res) {
		return nil, res
	}

	triggeringCodes := distinctFatalCodes(res)

	// Fall back to Load + Marshal.
	loaded, loadRes := Load(ctx, data, s)
	if loadRes.HasErrors() {
		merged := mergeResults(res, loadRes)
		return nil, merged
	}

	marshalOpts := updateOptsToMarshalOpts(opts)
	marshalOpts = append(marshalOpts, WithMetadata(newMeta))
	reMarshaled, marshalRes := Marshal(ctx, loaded, marshalOpts...)
	if marshalRes.HasErrors() {
		merged := mergeResults(res, marshalRes)
		return nil, merged
	}

	warn := diag.NewIssue(diag.Warning, diag.W_UPDATE_METADATA_FALLBACK,
		"snapshot.UpdateMetadataOrReMarshal: fell back to Load + Marshal after UpdateMetadata refused input").
		WithDetail("triggering_codes", strings.Join(triggeringCodes, ",")).
		Build()
	c := diag.NewCollector(0)
	c.Collect(warn)
	return reMarshaled, c.Result()
}

// detectIndent sniffs the input's indentation unit using the pinned
// algorithm from the api_enhancements plan. Matches Marshal's emission
// shape: assembleCompact writes `{"yammm_snapshot":...` (q == 1);
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

// distinctFatalCodes extracts the distinct Fatal-severity codes from a
// result, sorted alphabetically for deterministic detail output.
func distinctFatalCodes(r diag.Result) []string {
	seen := make(map[string]struct{})
	for iss := range r.BySeverity(diag.Fatal) {
		seen[iss.Code().String()] = struct{}{}
	}
	// Also include Error-severity codes because some malformed-input
	// failure modes (E_SNAPSHOT_MALFORMED from decodeHeader) surface at
	// Error rather than Fatal; the fallback triggers on any error-severity
	// issue outside cancellation.
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

// mergeResults returns a new Result containing every issue from a and b.
func mergeResults(a, b diag.Result) diag.Result {
	c := diag.NewCollector(0)
	for iss := range a.Issues() {
		c.Collect(iss)
	}
	for iss := range b.Issues() {
		c.Collect(iss)
	}
	return c.Result()
}

// updateOptsToMarshalOpts converts the UpdateOption slice into the
// equivalent Marshal Option slice for the fallback path. Today only
// WithUpdateCreatedAt requires conversion; future UpdateOption
// additions must extend this translator.
func updateOptsToMarshalOpts(opts []UpdateOption) []Option {
	cfg := applyUpdateOptions(opts)
	var out []Option
	if cfg.createdAtSet {
		out = append(out, WithCreatedAt(cfg.createdAt))
	}
	return out
}

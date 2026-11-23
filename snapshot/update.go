package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// UpdateMetadata returns data with its header's metadata replaced by newMeta and
// its integrity hash recomputed, and every byte outside the header kept as it
// was. It updates every document [Unmarshal] accepts under its default options
// but decodes no instance. It checks the document's shape and, unless
// [WithIntegrityCheck](false) is given, its integrity hash, so with the check
// on the new hash seals no body the verified one did not. It takes
// [WithIntegrityCheck] alone, and returns nil and a *[diag.ResultError] for a
// document it refuses. The package doc's
// Updating Metadata section states the header it writes, its refusals and its
// cost.
func UpdateMetadata(
	ctx context.Context,
	data []byte,
	newMeta map[string]string,
	opts ...Option,
) ([]byte, error) {
	if ctx == nil {
		panic("snapshot.UpdateMetadata: nil context")
	}
	cfg := newConfig(opts)
	if err := cfg.refuse("UpdateMetadata", optIntegrityCheck); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelled(err)
	}

	if err := checkDocument(data); err != nil {
		return nil, failure(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: %v", err)).Build())
	}

	sd := newStreamDecoder(data, schema.Schema{}, cfg)
	if err := sd.decodeHeader(); err != nil {
		return nil, failure(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: header parse failed: %v", err)).Build())
	}
	// decodeHeader reports a rejected version, feature or hash algorithm on the
	// collector and returns nil, so the error alone would accept unreadable
	// documents.
	if sd.collector.HasErrors() {
		return nil, sd.collector.Err()
	}
	if !cfg.skipIntegrityCheck && sd.verifyIntegrity() != IntegrityOK {
		return nil, sd.collector.Err()
	}

	// Judged once the document is: a fault of the document is reported first.
	if err := invalidMetadata(newMeta); err != nil {
		return nil, failure(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("snapshot.UpdateMetadata: %v", err)).Build())
	}

	var meta map[string]string
	if len(newMeta) > 0 {
		meta = maps.Clone(newMeta)
	}
	hdr := marshalHeaderWire{
		SchemaName: sd.header.SchemaName,
		SchemaHash: sd.header.SchemaHash,
		// Non-nil: decodeHeader refuses a nil features field.
		Features: sd.header.Features,
		Metadata: meta,
		// Verbatim, nil included: fabricating a claim on a document that
		// states none would attest to a body nothing here read.
		Attestation: sd.header.Attestation,
	}
	if string(sd.header.CreatedAt) != "null" {
		hdr.CreatedAt = sd.header.CreatedAt
	}

	prefix, suffix := data[:sd.headerOffset], data[sd.bodyOffset:]
	indent := headerIndent(data)
	version, hashAlgo := sd.header.Version, sd.header.SchemaHashAlgorithm
	hdr.IntegrityHash = sha256Sum(prefix, buildHeaderBytes(hdr, indent, version, hashAlgo), suffix)
	header := buildHeaderBytes(hdr, indent, version, hashAlgo)

	out := make([]byte, 0, len(prefix)+len(header)+len(suffix))
	out = append(out, prefix...)
	out = append(out, header...)
	out = append(out, suffix...)
	return out, nil
}

// headerIndent returns the indent [Marshal] wrote data with: the whitespace
// between the line feed that directly follows the document's opening brace
// and the header's name, or "" when no line feed follows the brace. An indent
// may hold a line feed itself, so the first one after the brace ends the
// prefix, never the last. data has passed [checkDocument], so only whitespace
// stands before the brace and between it and the name.
func headerIndent(data []byte) string {
	brace := bytes.IndexByte(data, '{')
	name := brace + 1 + bytes.IndexByte(data[brace+1:], '"')
	if name > brace+1 && data[brace+1] == '\n' {
		return string(data[brace+2 : name])
	}
	return ""
}

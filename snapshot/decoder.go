package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

const maxComposedDepth = 32

// streamDecoder is the shared infrastructure for Verify, Load, Info, and
// HeaderOnlyRead. Byte-based callers set data; reader-based callers set
// reader and can only invoke decodeHeader, because decodeSectionsV3 and
// verifyIntegrity require the full byte slice.
type streamDecoder struct {
	data      []byte           // raw input bytes; nil for reader-based callers
	reader    io.Reader        // one-shot reader; non-nil only when data == nil
	header    headerWire       // decoded header
	typeTable []typeTableEntry // decoded types table
	tableIDs  []schema.TypeID  // identity per table row, zero where unresolved
	collector *diag.Collector  // accumulates diagnostics

	// loadCfg holds deserialization options (e.g., skip integrity check).
	loadCfg loadConfig

	// schema is the provided schema (nil for Info and HeaderOnlyRead).
	schema *schema.Schema

	// bodyOffset is the byte offset of the first byte following the
	// yammm_snapshot header value's closing '}'. Captured by
	// decodeHeader via json.Decoder.InputOffset immediately after the
	// header value is decoded, before the types-key loop. In
	// Marshal-produced output the byte at bodyOffset is ',' (separating
	// the header from the types key). Consumed by UpdateMetadata to
	// reuse body bytes verbatim; other callers ignore the field.
	// -1 until captured.
	bodyOffset int64
}

// newStreamDecoder creates a new streamDecoder from raw .ys bytes.
func newStreamDecoder(data []byte, s *schema.Schema, cfg loadConfig) *streamDecoder {
	return &streamDecoder{
		data:       data,
		collector:  diag.NewCollector(0),
		loadCfg:    cfg,
		schema:     s,
		bodyOffset: -1,
	}
}

// newStreamDecoderFromReader creates a new streamDecoder backed by an
// io.Reader. The reader is consumed once by decodeHeader; decodeSectionsV3
// and verifyIntegrity require the full byte slice and must not be called
// on a reader-based decoder.
func newStreamDecoderFromReader(r io.Reader, s *schema.Schema, cfg loadConfig) *streamDecoder {
	return &streamDecoder{
		reader:     r,
		collector:  diag.NewCollector(0),
		loadCfg:    cfg,
		schema:     s,
		bodyOffset: -1,
	}
}

// decodeHeader reads and validates the header and types table from the input.
// Returns error for JSON codec failures; validation issues go into the collector.
//
// For byte-based decoders (reader == nil) a fresh bytes.NewReader is created
// per call so decodeHeader remains idempotent — decodeSectionsV3 can re-scan
// from the start without the header reader having consumed prefix bytes.
// A nil data slice is passed through as bytes.NewReader(nil), which yields
// io.EOF immediately and surfaces as a malformed-input diagnostic.
// For reader-based decoders (reader != nil) the single reader is consumed
// once; calling decodeHeader a second time finds the reader drained.
func (sd *streamDecoder) decodeHeader() error {
	var input io.Reader
	if sd.reader != nil {
		input = sd.reader
	} else {
		input = bytes.NewReader(sd.data)
	}
	dec := json.NewDecoder(input)
	dec.UseNumber()

	// Expect top-level object.
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("expected JSON object: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected JSON object, got %v", tok)
	}

	// First key must be "yammm_snapshot".
	tok, err = dec.Token()
	if err != nil {
		return fmt.Errorf("expected first key: %w", err)
	}
	firstKey, ok := tok.(string)
	if !ok || firstKey != "yammm_snapshot" {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			"yammm_snapshot header must be the first key in the top-level object").Build())
		return fmt.Errorf("first key is %q, expected \"yammm_snapshot\"", firstKey)
	}

	// Decode header.
	if err := dec.Decode(&sd.header); err != nil {
		return fmt.Errorf("failed to decode header: %w", err)
	}

	// Capture the body-suffix byte offset. InputOffset points to the byte
	// immediately after the header value's closing '}'. In Marshal-produced
	// output this is ',' (separating yammm_snapshot from the types key).
	// UpdateMetadata consumes this offset to reuse body bytes verbatim.
	sd.bodyOffset = dec.InputOffset()

	// Validate version.
	if iss, ok := acceptVersion(sd.header.Version, MinReadableVersion, currentVersion); !ok {
		sd.collector.Collect(iss)
		return nil
	}

	// Validate features — must not be null (required field).
	if sd.header.Features == nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			"features field is required (use empty array [] for V1)").Build())
		return nil
	}
	for _, f := range sd.header.Features {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNSUPPORTED_FEATURE,
			fmt.Sprintf("unrecognized feature %q", f)).
			WithDetail(diag.DetailKeyFeature, f).
			Build())
	}

	// Validate schema hash algorithm.
	if sd.header.SchemaHashAlgorithm != schema.StructuralHashVersion {
		sd.collector.Collect(diag.NewIssue(diag.Warning, diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM,
			fmt.Sprintf("unrecognized schema hash algorithm version %d; schema hash verification skipped",
				sd.header.SchemaHashAlgorithm)).
			WithDetail(diag.DetailKeyHashAlgorithm, strconv.Itoa(sd.header.SchemaHashAlgorithm)).
			Build())
	}

	// Verify schema hash (only if schema is provided and algorithm is recognized).
	if sd.schema != nil && sd.header.SchemaHashAlgorithm == schema.StructuralHashVersion {
		expectedHash := schema.StructuralHash(sd.schema)
		if expectedHash != sd.header.SchemaHash {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA,
				fmt.Sprintf("schema structural hash mismatch for %q", sd.header.SchemaName)).
				WithDetail(diag.DetailKeyExpectedHash, expectedHash).
				WithDetail(diag.DetailKeyActualHash, sd.header.SchemaHash).
				WithDetail(diag.DetailKeySchemaName, sd.header.SchemaName).
				Build())
		}
	}

	// Decode remaining keys to find "types".
	for dec.More() {
		tok, err = dec.Token()
		if err != nil {
			return fmt.Errorf("expected key: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}
		if key == "types" {
			if err := dec.Decode(&sd.typeTable); err != nil {
				return fmt.Errorf("failed to decode types table: %w", err)
			}
			sd.resolveTypeTable()
			return nil
		}
		// Skip non-types keys.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return fmt.Errorf("failed to skip key %q: %w", key, err)
		}
	}

	return errors.New("types key not found in document")
}

// verifyIntegrity verifies the integrity hash.
func (sd *streamDecoder) verifyIntegrity() string {
	if sd.loadCfg.skipIntegrityCheck {
		return "skipped"
	}
	if sd.header.IntegrityHash == "" {
		return "skipped"
	}

	// Locate "integrity_hash" key and replace its value with "".
	canonical := replaceIntegrityHash(sd.data, sd.header.IntegrityHash)
	if canonical == nil {
		// Could not locate the integrity hash in the raw bytes.
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INTEGRITY_MISMATCH,
			"could not locate integrity_hash in document for verification").Build())
		return "mismatch"
	}

	h := sha256Sum(canonical)
	if h != sd.header.IntegrityHash {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INTEGRITY_MISMATCH,
			"integrity hash does not match document content").
			WithDetail(diag.DetailKeyExpectedHash, sd.header.IntegrityHash).
			WithDetail(diag.DetailKeyActualHash, h).
			WithHint("the file may be corrupted, truncated, or modified").
			Build())
		return "mismatch"
	}

	return "ok"
}

// replaceIntegrityHash replaces the integrity_hash value in the raw bytes
// with "" (empty string) to reconstruct the canonical form.
func replaceIntegrityHash(data []byte, _ string) []byte {
	// Find the integrity_hash key in the data.
	// The key appears as "integrity_hash" followed by : and the value.
	keyBytes := []byte(`"integrity_hash"`)
	idx := bytes.Index(data, keyBytes)
	if idx < 0 {
		return nil
	}

	// Advance past the key to find the colon.
	pos := idx + len(keyBytes)
	for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\n' || data[pos] == '\r') {
		pos++
	}
	if pos >= len(data) || data[pos] != ':' {
		return nil
	}
	pos++ // skip colon

	// Skip whitespace after colon.
	for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\n' || data[pos] == '\r') {
		pos++
	}
	if pos >= len(data) || data[pos] != '"' {
		return nil
	}

	// Find the end of the quoted string value.
	valueStart := pos // position of opening quote
	pos++             // skip opening quote
	for pos < len(data) && data[pos] != '"' {
		if data[pos] == '\\' {
			pos++ // skip escaped char
		}
		pos++
	}
	if pos >= len(data) {
		return nil
	}
	valueEnd := pos + 1 // position after closing quote

	// Replace the value with "".
	var result []byte
	result = append(result, data[:valueStart]...)
	result = append(result, `""`...)
	result = append(result, data[valueEnd:]...)
	return result
}

// sha256Sum computes the SHA-256 hash of data and formats it as "sha256:<hex>".
func sha256Sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

// formatWireKey formats a wire key ([]any) as a canonical string.
// This matches immutable.Key.String() output.
func formatWireKey(key []any) string {
	if key == nil {
		return "[]"
	}
	k := immutable.WrapKey(normalizeSlice(key))
	return k.String()
}

// normalizeSlice applies NormalizeValue to each element in a slice.
func normalizeSlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = immutable.NormalizeValue(v)
	}
	return result
}

// normalizeMap applies NormalizeValue to each value in a map.
func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = immutable.NormalizeValue(v)
	}
	return result
}

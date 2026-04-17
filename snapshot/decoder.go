package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

const maxComposedDepth = 32

// instanceExistence tracks which instances exist for structural validation.
// Used by both Verify (primary output) and Load (derived from InstanceParts keys).
type instanceExistence map[string]map[string]bool // typeName → keyString → true

// edgeRef is a lightweight edge reference for structural validation.
type edgeRef struct {
	sourceType string
	sourceKey  string
	targetType string
	targetKey  string
}

// streamDecoder is the shared infrastructure for Verify, Load, Info, and
// HeaderOnlyRead. Byte-based callers set data and leave reader nil;
// reader-based callers set reader and leave data nil. decodeSections and
// verifyIntegrity require data and must not be called on a reader-based
// decoder (HeaderOnlyRead, the sole reader-based caller, only invokes
// decodeHeader).
type streamDecoder struct {
	data      []byte          // raw input bytes; nil for reader-based callers
	reader    io.Reader       // one-shot reader; non-nil only when data == nil
	header    headerWire      // decoded header
	types     []string        // decoded types array
	collector *diag.Collector // accumulates diagnostics

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
// io.Reader. The reader is consumed once by decodeHeader; decodeSections
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

// decodeHeader reads and validates the header and types array from the input.
// Returns error for JSON codec failures; validation issues go into the collector.
//
// For byte-based decoders (reader == nil) a fresh bytes.NewReader is created
// per call so decodeHeader remains idempotent — decodeSections can re-scan
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
	if sd.header.Version != currentVersion {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNSUPPORTED_VERSION,
			fmt.Sprintf("unsupported snapshot format version %d (supported: %d)", sd.header.Version, currentVersion)).
			WithDetail(diag.DetailKeyVersion, strconv.Itoa(sd.header.Version)).
			Build())
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
			if err := dec.Decode(&sd.types); err != nil {
				return fmt.Errorf("failed to decode types: %w", err)
			}

			// Validate types against schema.
			if sd.schema != nil {
				for _, typeName := range sd.types {
					if _, ok := sd.schema.Type(typeName); !ok {
						sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
							fmt.Sprintf("type %q not found in schema", typeName)).
							WithDetail(diag.DetailKeyTypeName, typeName).
							Build())
					}
				}
			}
			return nil // Successfully decoded header + types.
		}
		// Skip non-types keys.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return fmt.Errorf("failed to skip key %q: %w", key, err)
		}
	}

	return errors.New("types key not found in document")
}

// decodeRemainingSection finds and decodes a section from the raw data by re-scanning.
// This is used after decodeHeader has consumed the header and types.
func (sd *streamDecoder) decodeSections() (map[string][]instWire, diagWire, error) {
	dec := json.NewDecoder(bytes.NewReader(sd.data))
	dec.UseNumber()

	// Skip opening brace.
	if _, err := dec.Token(); err != nil {
		return nil, diagWire{}, fmt.Errorf("expected opening brace: %w", err)
	}

	var instances map[string][]instWire
	var diags diagWire
	diagsFound := false

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, diagWire{}, fmt.Errorf("expected key token: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}

		switch key {
		case "instances":
			if err := dec.Decode(&instances); err != nil {
				return nil, diagWire{}, fmt.Errorf("failed to decode instances: %w", err)
			}
		case "diagnostics":
			if err := dec.Decode(&diags); err != nil {
				return nil, diagWire{}, fmt.Errorf("failed to decode diagnostics: %w", err)
			}
			diagsFound = true
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, diagWire{}, fmt.Errorf("failed to skip key %q: %w", key, err)
			}
		}
	}

	if instances == nil {
		instances = make(map[string][]instWire)
	}
	if !diagsFound {
		diags = diagWire{Duplicates: []dupWire{}, Unresolved: []unresolvedWire{}}
	}

	return instances, diags, nil
}

// validateInstances performs lightweight validation for Verify.
// Returns the instance existence set and edge references.
func (sd *streamDecoder) validateInstances(
	ctx context.Context,
	instances map[string][]instWire,
) (instanceExistence, []edgeRef, error) {
	exists := make(instanceExistence)
	var refs []edgeRef

	// Check types/instances key consistency.
	sd.checkTypesConsistency(instances)

	for typeName, insts := range instances {
		if err := ctx.Err(); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, nil, fmt.Errorf("context cancelled: %w", err)
		}

		if exists[typeName] == nil {
			exists[typeName] = make(map[string]bool, len(insts))
		}

		for _, inst := range insts {
			sd.processInstance(typeName, inst, 0, func(typeName string, inst instWire, _ schema.TypeID) {
				keyStr := formatWireKey(inst.Key)
				exists[typeName][keyStr] = true

				// Collect edge references.
				for _, edgeTargets := range inst.Edges {
					for _, et := range edgeTargets {
						refs = append(refs, edgeRef{
							sourceType: typeName,
							sourceKey:  keyStr,
							targetType: et.TargetType,
							targetKey:  formatWireKey(et.TargetKey),
						})
					}
				}
			})
		}
	}

	return exists, refs, nil
}

// decodeInstances performs full materialization for Load.
func (sd *streamDecoder) decodeInstances(
	ctx context.Context,
	instances map[string][]instWire,
) (instanceExistence, map[string][]graph.InstanceParts, []graph.EdgeParts, error) {
	exists := make(instanceExistence)
	parts := make(map[string][]graph.InstanceParts, len(instances))
	var edgeParts []graph.EdgeParts

	// Check types/instances key consistency.
	sd.checkTypesConsistency(instances)

	for typeName, insts := range instances {
		if err := ctx.Err(); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, nil, nil, fmt.Errorf("context cancelled: %w", err)
		}

		if exists[typeName] == nil {
			exists[typeName] = make(map[string]bool, len(insts))
		}

		for _, inst := range insts {
			sd.processInstance(typeName, inst, 0, func(typeName string, inst instWire, typeID schema.TypeID) {
				ip := sd.wireToInstanceParts(typeName, inst, typeID)
				parts[typeName] = append(parts[typeName], ip)

				keyStr := ip.PrimaryKey.String()
				exists[typeName][keyStr] = true

				// Build EdgeParts from per-instance edges.
				for relName, targets := range inst.Edges {
					for _, et := range targets {
						ep := graph.EdgeParts{
							Relation:   relName,
							SourceType: typeName,
							SourceKey:  ip.PrimaryKey,
							TargetType: et.TargetType,
							TargetKey:  immutable.WrapKey(normalizeSlice(et.TargetKey)),
							Properties: immutable.WrapProperties(normalizeMap(et.Properties)),
						}
						edgeParts = append(edgeParts, ep)
					}
				}
			})
		}
	}

	return exists, parts, edgeParts, nil
}

// countInstances token-scans the instances section for Info.
// Returns per-type instance counts and total edge count.
func (sd *streamDecoder) countInstances(
	instances map[string][]instWire,
) (map[string]int, int) {
	counts := make(map[string]int, len(instances))
	totalEdges := 0

	for typeName, insts := range instances {
		counts[typeName] = len(insts)
		for _, inst := range insts {
			for _, targets := range inst.Edges {
				totalEdges += len(targets)
			}
		}
	}

	return counts, totalEdges
}

// processInstance validates a single instWire and calls emit on success.
// Validation: type_id cross-check, composed edge invariant, depth limit,
// provenance path parse.
func (sd *streamDecoder) processInstance(
	typeName string,
	inst instWire,
	depth int,
	emit func(typeName string, inst instWire, typeID schema.TypeID),
) {
	// Depth limit.
	if depth > maxComposedDepth {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DEPTH_EXCEEDED,
			fmt.Sprintf("composed nesting depth %d exceeds limit %d", depth, maxComposedDepth)).
			WithDetail(diag.DetailKeyDepth, strconv.Itoa(depth)).
			WithDetail(diag.DetailKeyTypeName, typeName).
			Build())
		return
	}

	// Resolve TypeID from schema.
	var typeID schema.TypeID
	if sd.schema != nil {
		schemaType, ok := sd.schema.Type(typeName)
		if ok {
			typeID = schemaType.ID()
		}

		// Cross-validate type_id when present.
		if inst.TypeID != nil && ok {
			if inst.TypeID.Name != typeID.Name() {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPEID_MISMATCH,
					fmt.Sprintf("persisted type_id name %q does not match schema type %q",
						inst.TypeID.Name, typeID.Name())).
					WithDetail(diag.DetailKeyTypeName, typeName).
					Build())
			}
		}
	}

	// Composed children edge invariant (only for depth > 0, i.e., composed children).
	if depth > 0 && len(inst.Edges) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("composed child %s has edges (composed children must not carry edges)", typeName)).
			WithDetail(diag.DetailKeyTypeName, typeName).
			Build())
	}

	// Provenance path parse (warning on fallback).
	if inst.Provenance != nil && inst.Provenance.Path != "" {
		if _, err := path.Parse(inst.Provenance.Path); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Warning, diag.E_SNAPSHOT_PATH_FALLBACK,
				fmt.Sprintf("provenance path %q could not be parsed, falling back to root path", inst.Provenance.Path)).
				WithDetail(diag.DetailKeyOriginalPath, inst.Provenance.Path).
				WithDetail(diag.DetailKeyTypeName, typeName).
				Build())
		}
	}

	// Process composed children recursively.
	for relName, children := range inst.Composed {
		for _, child := range children {
			childTypeName := relName
			if child.TypeID != nil {
				childTypeName = child.TypeID.Name
			}
			// For composed children, use the child's type name from the composed map context.
			// The child itself determines its type from the composed relation target.
			sd.processInstance(childTypeName, child, depth+1, nil)
		}
	}

	if emit != nil {
		emit(typeName, inst, typeID)
	}
}

// wireToInstanceParts converts a wire instance to InstanceParts.
func (sd *streamDecoder) wireToInstanceParts(
	typeName string,
	inst instWire,
	typeID schema.TypeID,
) graph.InstanceParts {
	ip := graph.InstanceParts{
		TypeName:   typeName,
		TypeID:     typeID,
		PrimaryKey: immutable.WrapKey(normalizeSlice(inst.Key)),
		Properties: immutable.WrapProperties(normalizeMap(inst.Properties)),
	}

	// Composed children.
	if len(inst.Composed) > 0 {
		ip.Composed = make(map[string][]graph.InstanceParts, len(inst.Composed))
		for relName, children := range inst.Composed {
			childParts := make([]graph.InstanceParts, 0, len(children))
			for _, child := range children {
				childTypeName := relName
				if child.TypeID != nil {
					childTypeName = child.TypeID.Name
				}
				// Resolve child TypeID from schema.
				var childTypeID schema.TypeID
				if sd.schema != nil {
					if ct, ok := sd.schema.Type(childTypeName); ok {
						childTypeID = ct.ID()
					}
				}
				cp := sd.wireToInstanceParts(childTypeName, child, childTypeID)
				childParts = append(childParts, cp)
			}
			ip.Composed[relName] = childParts
		}
	}

	// Provenance.
	if inst.Provenance != nil {
		parsedPath, parseErr := path.Parse(inst.Provenance.Path)
		if parseErr != nil {
			parsedPath = path.Root()
		}
		prov := location.NewProvenance(inst.Provenance.SourceName, parsedPath, location.Span{})
		if parseErr != nil {
			prov = prov.WithRawPath(inst.Provenance.Path)
		}
		ip.Provenance = prov
	}

	return ip
}

// checkTypesConsistency verifies the types array matches instances map keys.
func (sd *streamDecoder) checkTypesConsistency(instances map[string][]instWire) {
	// Types in the types array but not in instances.
	for _, t := range sd.types {
		if _, ok := instances[t]; !ok {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
				fmt.Sprintf("type %q in types array but not in instances", t)).
				WithDetail(diag.DetailKeyTypeName, t).
				Build())
		}
	}

	// Types in instances but not in the types array.
	typeSet := make(map[string]bool, len(sd.types))
	for _, t := range sd.types {
		typeSet[t] = true
	}
	for t := range instances {
		if !typeSet[t] {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
				fmt.Sprintf("type %q in instances but not in types array", t)).
				WithDetail(diag.DetailKeyTypeName, t).
				Build())
		}
	}
}

// validateDiagnostics validates duplicate and unresolved records.
func (sd *streamDecoder) validateDiagnostics(diags diagWire, exists instanceExistence) {
	for _, dup := range diags.Duplicates {
		keyStr := formatWireKey(dup.Key)

		// Validate duplicate conflict reference.
		if typeIdx := exists[dup.Type]; typeIdx == nil || !typeIdx[keyStr] {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("duplicate conflict for %s[%s] references non-existent instance", dup.Type, keyStr)).
				WithDetail(diag.DetailKeyTypeName, dup.Type).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}

		// Duplicate instance must not have composed children.
		if len(dup.Instance.Composed) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have composed children", dup.Type, keyStr)).
				WithDetail(diag.DetailKeyTypeName, dup.Type).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}

		// Duplicate instance must not have edges.
		if len(dup.Instance.Edges) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_EDGES_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have edges", dup.Type, keyStr)).
				WithDetail(diag.DetailKeyTypeName, dup.Type).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}
	}
}

// validateEdgeRefs checks that all edge target references resolve.
func (sd *streamDecoder) validateEdgeRefs(refs []edgeRef, exists instanceExistence) {
	for _, ref := range refs {
		typeIdx := exists[ref.targetType]
		if typeIdx == nil || !typeIdx[ref.targetKey] {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("edge in %s[%s] references %s[%s] which does not exist",
					ref.sourceType, ref.sourceKey, ref.targetType, ref.targetKey)).
				WithDetail(diag.DetailKeyTypeName, ref.sourceType).
				WithDetail(diag.DetailKeyPrimaryKey, ref.sourceKey).
				WithDetail(diag.DetailKeyTargetType, ref.targetType).
				WithDetail(diag.DetailKeyTargetPK, ref.targetKey).
				WithHint("ensure the target instance is included in the snapshot").
				Build())
		}
	}
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

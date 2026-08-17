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

// streamDecoder is the shared infrastructure for Verify, Load, Info, and
// HeaderOnlyRead. Byte-based callers set data; reader-based callers set
// reader and can only invoke decodeHeader, because decodeSections and
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

	// bodyOffset is the byte offset of the body suffix UpdateMetadata
	// reuses verbatim; -1 until decodeHeader captures it.
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

// newStreamDecoderFromReader creates a streamDecoder backed by an io.Reader,
// which decodeHeader consumes once; decodeSections and verifyIntegrity need
// the full byte slice and must not be called on it.
func newStreamDecoderFromReader(r io.Reader, s *schema.Schema, cfg loadConfig) *streamDecoder {
	return &streamDecoder{
		reader:     r,
		collector:  diag.NewCollector(0),
		loadCfg:    cfg,
		schema:     s,
		bodyOffset: -1,
	}
}

// decodeHeader reads and validates the header and types table: JSON codec
// failures return an error, validation issues go to the collector. A
// byte-based decoder re-scans from a fresh reader per call and stays
// idempotent; a reader-based decoder is consumed once.
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

	// InputOffset here is the byte after the header value's closing '}' —
	// in Marshal-produced output, the ',' that starts the body suffix.
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

// decodeSections decodes the instances and diagnostics sections by
// re-scanning the raw data.
func (sd *streamDecoder) decodeSections() ([]instanceGroupWire, diagWire, error) {
	dec := json.NewDecoder(bytes.NewReader(sd.data))
	dec.UseNumber()

	if _, err := dec.Token(); err != nil {
		return nil, diagWire{}, fmt.Errorf("expected opening brace: %w", err)
	}

	var groups []instanceGroupWire
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
			if err := dec.Decode(&groups); err != nil {
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

	if groups == nil {
		groups = []instanceGroupWire{}
	}
	if !diagsFound {
		diags = diagWire{Duplicates: []dupWire{}, Unresolved: []unresolvedWire{}}
	}

	return groups, diags, nil
}

// slotCoord addresses one root instance's relation slot. Only root parents
// carry addressable slots: a duplicate's parent coordinates must resolve in
// the root instance index, so deeper slots are never referenced.
type slotCoord struct {
	parentRow int
	parentKey string
	relation  string
}

// slotChild is one occupant of a slot, by table row and key string.
type slotChild struct {
	row int
	key string
}

// edgeRef is a lightweight edge reference for structural validation.
type edgeRef struct {
	sourceRow int
	sourceKey string
	targetRow int
	targetKey string
}

// docIndex carries the structural facts one walk of the instances section
// produces: root existence, slot occupancy, and edge references.
type docIndex struct {
	exists map[int]map[string]bool
	slots  map[slotCoord][]slotChild
	refs   []edgeRef
}

func (idx *docIndex) rootExists(row int, key string) bool {
	return idx.exists[row][key]
}

// walkInstances runs the one structural pass over the instances section.
// Verify and Load share it; nothing downstream re-validates.
func (sd *streamDecoder) walkInstances(ctx context.Context, groups []instanceGroupWire) (*docIndex, error) {
	idx := &docIndex{
		exists: make(map[int]map[string]bool, len(groups)),
		slots:  make(map[slotCoord][]slotChild),
	}

	seenRows := make(map[int]int, len(groups))
	for gi, g := range groups {
		if err := ctx.Err(); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, fmt.Errorf("context cancelled: %w", err)
		}
		row, ok := sd.requireRow(g.Type, fmt.Sprintf("instances entry %d", gi))
		if !ok {
			continue
		}
		if first, dup := seenRows[row]; dup {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("instances entries %d and %d both reference types-table row %d (%s)",
					first, gi, row, sd.refAt(row))).Build())
			continue
		}
		seenRows[row] = gi
		if idx.exists[row] == nil {
			idx.exists[row] = make(map[string]bool, len(g.Items))
		}
		for _, item := range g.Items {
			sd.walkInstance(row, item, 0, nil, idx)
		}
	}
	return idx, nil
}

// walkInstance validates one instance at its table row and registers its
// structural facts. slot is non-nil only for a root's direct composed child.
func (sd *streamDecoder) walkInstance(row int, inst instWire, depth int, slot *slotCoord, idx *docIndex) {
	if depth > maxComposedDepth {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DEPTH_EXCEEDED,
			fmt.Sprintf("composed nesting depth %d exceeds limit %d", depth, maxComposedDepth)).
			WithDetail(diag.DetailKeyDepth, strconv.Itoa(depth)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			Build())
		return
	}

	if depth == 0 && inst.Type != nil && *inst.Type != row {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
			fmt.Sprintf("root instance %s declares type row %d (%s) inside the section entry of row %d (%s)",
				formatWireKey(inst.Key), *inst.Type, sd.refAt(*inst.Type), row, sd.refAt(row))).Build())
	}

	if depth > 0 && len(inst.Edges) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("composed child %s has edges (composed children must not carry edges)", sd.refAt(row))).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			Build())
	}

	sd.checkProvenancePath(inst, row)

	keyStr := formatWireKey(inst.Key)
	if depth == 0 {
		idx.exists[row][keyStr] = true
	}
	if slot != nil {
		idx.slots[*slot] = append(idx.slots[*slot], slotChild{row: row, key: keyStr})
	}

	for relName, targets := range inst.Edges {
		for ei, e := range targets {
			targetRow, ok := sd.requireRow(e.TargetType,
				fmt.Sprintf("edge %d under %s.%s", ei, sd.refAt(row), relName))
			if !ok {
				continue
			}
			if depth == 0 {
				idx.refs = append(idx.refs, edgeRef{
					sourceRow: row,
					sourceKey: keyStr,
					targetRow: targetRow,
					targetKey: formatWireKey(e.TargetKey),
				})
			}
		}
	}

	for relName, children := range inst.Composed {
		for _, child := range children {
			childRow, ok := sd.composedChildRow(child, relName, row)
			if !ok {
				continue
			}
			var childSlot *slotCoord
			if depth == 0 {
				childSlot = &slotCoord{parentRow: row, parentKey: keyStr, relation: relName}
			}
			sd.walkInstance(childRow, child, depth+1, childSlot, idx)
		}
	}
}

// composedChildRow reads a composed child's declared table row. The writer
// emits one at every non-root position, so its absence is a malformed
// document rather than a value to recover from context.
func (sd *streamDecoder) composedChildRow(child instWire, relName string, parentRow int) (int, bool) {
	return sd.requireRow(child.Type,
		fmt.Sprintf("composed child under %s.%s", sd.refAt(parentRow), relName))
}

// checkProvenancePath warns when a provenance path will not parse. The
// materializer falls back to the root path silently, so the read surface is
// where the loss becomes visible.
func (sd *streamDecoder) checkProvenancePath(inst instWire, row int) {
	if inst.Provenance == nil || inst.Provenance.Path == "" {
		return
	}
	if _, err := path.Parse(inst.Provenance.Path); err == nil {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Warning, diag.E_SNAPSHOT_PATH_FALLBACK,
		fmt.Sprintf("provenance path %q could not be parsed, falling back to root path", inst.Provenance.Path)).
		WithDetail(diag.DetailKeyOriginalPath, inst.Provenance.Path).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		Build())
}

// validateDiagnostics validates duplicate and unresolved records against the
// walked index. Every conflict resolves at its stated address: a root
// conflict through the instance index, a composed conflict through the
// parent's slot, where a null stated key needs a sole occupant.
func (sd *streamDecoder) validateDiagnostics(diags diagWire, idx *docIndex) {
	for di, dup := range diags.Duplicates {
		row, rowOK := sd.requireRow(dup.Type, fmt.Sprintf("duplicate record %d", di))
		keyStr := formatWireKey(dup.Key)

		if rowOK && dup.Instance.Type != nil && *dup.Instance.Type != row {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
				fmt.Sprintf("duplicate instance %s[%s] declares type row %d (%s) but its record states row %d (%s)",
					sd.refAt(row), keyStr, *dup.Instance.Type, sd.refAt(*dup.Instance.Type), row, sd.refAt(row))).Build())
		}

		// Marshal rewrites the record's key from the instance it carries, so a
		// disagreement is an address the next write would silently discard.
		if instKey := formatWireKey(dup.Instance.Key); instKey != keyStr {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states key %s but its instance carries %s",
					di, keyStr, instKey)).Build())
		}

		sd.checkProvenancePath(dup.Instance, row)

		if len(dup.Instance.Composed) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have composed children", sd.refAt(row), keyStr)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}
		if len(dup.Instance.Edges) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_EDGES_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have edges", sd.refAt(row), keyStr)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}

		if dup.Conflict == nil {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d (%s[%s]) carries no conflict block", di, sd.refAt(row), keyStr)).Build())
			continue
		}
		conflictRow, conflictOK := sd.requireRow(dup.Conflict.Type,
			fmt.Sprintf("duplicate record %d conflict", di))
		if !conflictOK {
			continue
		}
		conflictKey := formatWireKey(dup.Conflict.Key)

		if dup.Relation == "" {
			// Parent coordinates address a slot, and a root duplicate has none.
			if dup.ParentType != nil || len(dup.ParentKey) > 0 {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
					fmt.Sprintf("duplicate record %d states no relation but carries parent coordinates", di)).Build())
			}
			if !idx.rootExists(conflictRow, conflictKey) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
					fmt.Sprintf("duplicate conflict %s[%s] references non-existent instance",
						sd.refAt(conflictRow), conflictKey)).
					WithDetail(diag.DetailKeyTypeName, sd.refAt(conflictRow)).
					WithDetail(diag.DetailKeyPrimaryKey, conflictKey).
					Build())
			}
			continue
		}

		if dup.ParentType == nil {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d carries relation %q with no parent coordinates", di, dup.Relation)).Build())
			continue
		}
		parentRow, ok := sd.requireRow(dup.ParentType, fmt.Sprintf("duplicate record %d parent", di))
		if !ok {
			continue
		}
		parentKey := formatWireKey(dup.ParentKey)
		if !idx.rootExists(parentRow, parentKey) {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("duplicate parent %s[%s] does not resolve to a root instance",
					sd.refAt(parentRow), parentKey)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(parentRow)).
				WithDetail(diag.DetailKeyPrimaryKey, parentKey).
				Build())
			continue
		}
		if !sd.conflictInSlot(idx, slotCoord{parentRow: parentRow, parentKey: parentKey, relation: dup.Relation},
			conflictRow, dup.Conflict.Key, conflictKey) {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("duplicate conflict %s[%s] under %s[%s].%s references non-existent instance",
					sd.refAt(conflictRow), conflictKey, sd.refAt(parentRow), parentKey, dup.Relation)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(conflictRow)).
				WithDetail(diag.DetailKeyPrimaryKey, conflictKey).
				Build())
		}
	}

	for ui, u := range diags.Unresolved {
		sourceRow, ok := sd.requireRow(u.SourceType, fmt.Sprintf("unresolved record %d source", ui))
		if ok {
			sourceKey := formatWireKey(u.SourceKey)
			if !idx.rootExists(sourceRow, sourceKey) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
					fmt.Sprintf("unresolved edge source %s[%s] references non-existent instance",
						sd.refAt(sourceRow), sourceKey)).
					WithDetail(diag.DetailKeyTypeName, sd.refAt(sourceRow)).
					WithDetail(diag.DetailKeyPrimaryKey, sourceKey).
					Build())
			}
		}
		sd.requireRow(u.TargetType, fmt.Sprintf("unresolved record %d target", ui))
	}
}

// conflictInSlot reports whether the stated conflict address resolves in the
// slot: a non-empty key selects among the occupants, an empty one addresses a
// sole occupant. The arm is chosen by length, not nil-ness, because an empty
// JSON array decodes non-nil and graph.resolveDuplicateConflict uses Key.Len().
func (sd *streamDecoder) conflictInSlot(idx *docIndex, slot slotCoord, conflictRow int, rawKey []any, keyStr string) bool {
	occupants := idx.slots[slot]
	if len(rawKey) > 0 {
		for _, occ := range occupants {
			if occ.row == conflictRow && occ.key == keyStr {
				return true
			}
		}
		return false
	}
	return len(occupants) == 1 && occupants[0].row == conflictRow
}

// validateEdgeRefs checks that all edge target references resolve.
func (sd *streamDecoder) validateEdgeRefs(idx *docIndex) {
	for _, ref := range idx.refs {
		if idx.rootExists(ref.targetRow, ref.targetKey) {
			continue
		}
		sourceRef := sd.refAt(ref.sourceRow)
		targetRef := sd.refAt(ref.targetRow)
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
			fmt.Sprintf("edge in %s[%s] references %s[%s] which does not exist",
				sourceRef, ref.sourceKey, targetRef, ref.targetKey)).
			WithDetail(diag.DetailKeyTypeName, sourceRef).
			WithDetail(diag.DetailKeyPrimaryKey, ref.sourceKey).
			WithDetail(diag.DetailKeyTargetType, targetRef).
			WithDetail(diag.DetailKeyTargetPK, ref.targetKey).
			WithHint("ensure the target instance is included in the snapshot").
			Build())
	}
}

// runPipeline decodes and validates the document body, gating the collector
// after the last collection point: no loop that can collect runs after the
// error check that decides whether a snapshot returns. Returns the decoded
// body only when the document is clean.
func (sd *streamDecoder) runPipeline(ctx context.Context) ([]instanceGroupWire, diagWire, bool) {
	groups, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, diagWire{}, false
	}
	idx, err := sd.walkInstances(ctx, groups)
	if err != nil {
		return nil, diagWire{}, false
	}
	sd.validateDiagnostics(diags, idx)
	sd.verifyIntegrity()
	sd.validateEdgeRefs(idx)

	if sd.collector.HasErrors() {
		return nil, diagWire{}, false
	}
	return groups, diags, true
}

// loadDocument materializes a validated document. It runs after the
// pipeline's gate, so every reference is already validated and nothing here
// collects a diagnostic.
func (sd *streamDecoder) loadDocument(groups []instanceGroupWire, diags diagWire) graph.SnapshotParts {
	types := make([]schema.TypeID, 0, len(groups))
	instParts := make(map[schema.TypeID][]graph.InstanceParts, len(groups))
	var edgeParts []graph.EdgeParts

	for _, g := range groups {
		row := *g.Type
		id := sd.tableIDs[row]
		types = append(types, id)
		parts := make([]graph.InstanceParts, 0, len(g.Items))
		for _, item := range g.Items {
			ip := sd.instanceParts(row, item)
			parts = append(parts, ip)
			for relName, targets := range item.Edges {
				for _, e := range targets {
					edgeParts = append(edgeParts, graph.EdgeParts{
						Relation:   relName,
						SourceType: id,
						SourceKey:  ip.PrimaryKey,
						TargetType: sd.tableIDs[*e.TargetType],
						TargetKey:  immutable.WrapKey(normalizeSlice(e.TargetKey)),
						Properties: immutable.WrapProperties(normalizeMap(e.Properties)),
					})
				}
			}
		}
		instParts[id] = parts
	}

	dupParts := make([]graph.DuplicateParts, 0, len(diags.Duplicates))
	for _, dw := range diags.Duplicates {
		dp := graph.DuplicateParts{
			Type:         sd.tableIDs[*dw.Type],
			Key:          immutable.WrapKey(normalizeSlice(dw.Key)),
			Instance:     sd.instanceParts(*dw.Type, dw.Instance),
			ConflictType: sd.tableIDs[*dw.Conflict.Type],
			ConflictKey:  immutable.WrapKey(normalizeSlice(dw.Conflict.Key)),
		}
		if dw.Relation != "" {
			dp.ParentType = sd.tableIDs[*dw.ParentType]
			dp.ParentKey = immutable.WrapKey(normalizeSlice(dw.ParentKey))
			dp.Relation = dw.Relation
		}
		dupParts = append(dupParts, dp)
	}

	unresParts := make([]graph.UnresolvedParts, 0, len(diags.Unresolved))
	for _, uw := range diags.Unresolved {
		up := graph.UnresolvedParts{
			SourceType: sd.tableIDs[*uw.SourceType],
			SourceKey:  immutable.WrapKey(normalizeSlice(uw.SourceKey)),
			Relation:   uw.Relation,
			TargetType: sd.tableIDs[*uw.TargetType],
			Required:   uw.Required,
			Reason:     uw.Reason,
			Properties: immutable.WrapProperties(normalizeMap(uw.Properties)),
		}
		if uw.TargetKey != nil {
			up.TargetKey = immutable.WrapKey(normalizeSlice(uw.TargetKey))
		}
		unresParts = append(unresParts, up)
	}

	return graph.SnapshotParts{
		Types:      types,
		Instances:  instParts,
		Edges:      edgeParts,
		Duplicates: dupParts,
		Unresolved: unresParts,
	}
}

// instanceParts converts a validated wire instance to InstanceParts. The
// instance name is derived from the resolved identity, never carried on the
// wire, so a name cannot disagree with the identity beside it.
func (sd *streamDecoder) instanceParts(row int, inst instWire) graph.InstanceParts {
	id := sd.tableIDs[row]
	ip := graph.InstanceParts{
		TypeName:   schema.TagForm(sd.schema, id),
		TypeID:     id,
		PrimaryKey: immutable.WrapKey(normalizeSlice(inst.Key)),
		Properties: immutable.WrapProperties(normalizeMap(inst.Properties)),
	}

	if len(inst.Composed) > 0 {
		ip.Composed = make(map[string][]graph.InstanceParts, len(inst.Composed))
		for relName, children := range inst.Composed {
			childParts := make([]graph.InstanceParts, 0, len(children))
			for _, child := range children {
				childParts = append(childParts, sd.instanceParts(*child.Type, child))
			}
			ip.Composed[relName] = childParts
		}
	}

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

// countInstances token-counts the instances section for Info, keyed by each
// group row's identity so two same-named types never merge. An unresolvable
// row draws the same diagnostic the pipeline raises for it, so Info reports a
// short count rather than presenting one as complete.
func (sd *streamDecoder) countInstances(groups []instanceGroupWire) (map[TypeRef]int, int) {
	counts := make(map[TypeRef]int, len(groups))
	totalEdges := 0

	for gi, g := range groups {
		row, ok := sd.requireRow(g.Type, fmt.Sprintf("instances entry %d", gi))
		if !ok {
			continue
		}
		counts[TypeRef(sd.typeTable[row])] += len(g.Items)
		for _, inst := range g.Items {
			for _, targets := range inst.Edges {
				totalEdges += len(targets)
			}
		}
	}

	return counts, totalEdges
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

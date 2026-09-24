package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// maxComposedDepth is the validator's bound: what the validator accepts, the
// wire carries, and nothing deeper is ever written or read.
const maxComposedDepth = instance.MaxComposedDepth

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
	tableTags []string         // rendered tag per table row; nil when schema is nil
	collector *diag.Collector  // accumulates diagnostics

	// keyPositions is the canonicalizing primary-key positions per table row,
	// built once on first use. Every address the reader resolves went through
	// TypeByID and a PrimaryKeys walk per call — the same work graph's
	// canonicalizer memoizes per schema, left per-call one layer down.
	keyPositions [][]wireKeyPosition

	// loadCfg holds deserialization options (e.g., skip integrity check).
	loadCfg loadConfig

	// schema is the provided schema (nil for Info and HeaderOnlyRead).
	schema *schema.Schema

	// revalidator is non-nil only when WithRevalidation was passed and a
	// schema is present; walkInstance then runs every root back through it.
	revalidator *instance.Validator

	// cancelReported is set once a revalidated row has reported the context's
	// cancellation, so the walk's own poll does not report it a second time.
	cancelReported bool

	// bodyOffset is the byte offset of the body suffix UpdateMetadata
	// reuses verbatim; -1 until decodeHeader captures it.
	bodyOffset int64
}

// newStreamDecoder creates a new streamDecoder from raw .ys bytes.
func newStreamDecoder(data []byte, s *schema.Schema, cfg loadConfig) *streamDecoder {
	return &streamDecoder{
		data:        data,
		collector:   diag.NewCollector(cfg.issueLimit),
		loadCfg:     cfg,
		schema:      s,
		revalidator: newRevalidator(s, cfg),
		bodyOffset:  -1,
	}
}

// newRevalidator builds the validator the revalidation walk runs, or nil
// when nobody asked or no schema is present to validate against. It caps a
// row at the load's limit, so the load has one cap and an unlimited load
// stores every finding a row draws.
func newRevalidator(s *schema.Schema, cfg loadConfig) *instance.Validator {
	if !cfg.revalidate || s == nil {
		return nil
	}
	return instance.NewValidator(s, instance.WithIssueLimit(cfg.issueLimit))
}

// newStreamDecoderFromReader creates a streamDecoder backed by an io.Reader,
// which decodeHeader consumes once; decodeSections and verifyIntegrity need
// the full byte slice and must not be called on it.
func newStreamDecoderFromReader(r io.Reader, s *schema.Schema, cfg loadConfig) *streamDecoder {
	return &streamDecoder{
		reader:     r,
		collector:  diag.NewCollector(cfg.issueLimit),
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
		// Returned, not collected: every caller wraps a returned error into one
		// collected E_SNAPSHOT_MALFORMED, so collecting here too reported one
		// malformation twice — and this was the only arm in decodeHeader that
		// did both.
		return fmt.Errorf("yammm_snapshot must be the first key in the top-level object; got %q", firstKey)
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

	// Validate schema hash algorithm. A body-reading surface refuses the
	// document: where a body would be trusted, an uncheckable schema
	// identity is a refusal. A header-only read stays non-fatal, so
	// dispatch still receives a HeaderInfo whose SchemaHashMatches reports
	// false — the stale-schema classification consumers route on.
	if sd.header.SchemaHashAlgorithm != schema.StructuralHashVersion {
		sev, msg := diag.Error, "unrecognized schema hash algorithm version %d; the document cannot be checked against a schema"
		if sd.loadCfg.headerOnly {
			sev, msg = diag.Warning, "unrecognized schema hash algorithm version %d; schema hash verification skipped"
		}
		sd.collector.Collect(diag.NewIssue(sev, diag.E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM,
			fmt.Sprintf(msg, sd.header.SchemaHashAlgorithm)).
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
// re-scanning the raw data. The document's outermost shape is checked first
// and in one place, so this function decodes a body whose four keys are known
// present, ordered, unique and non-null.
func (sd *streamDecoder) decodeSections() ([]instanceGroupWire, diagWire, error) {
	if err := checkTopLevelKeys(sd.data); err != nil {
		return nil, diagWire{}, err
	}

	dec := json.NewDecoder(bytes.NewReader(sd.data))
	dec.UseNumber()

	if _, err := dec.Token(); err != nil {
		return nil, diagWire{}, fmt.Errorf("expected opening brace: %w", err)
	}

	var groups []instanceGroupWire
	var diags diagWire

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
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, diagWire{}, fmt.Errorf("failed to skip key %q: %w", key, err)
			}
		}
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
// produces: root existence, slot occupancy, and edge references. ones counts
// the records under each (one) association, edges and unresolved records
// together, and is allocated at the first such record.
type docIndex struct {
	exists map[int]map[string]bool
	slots  map[slotCoord][]slotChild
	refs   []edgeRef
	ones   map[slotCoord]int
	order  []slotCoord
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
			if !sd.cancelReported {
				sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			}
			return nil, fmt.Errorf("context cancelled: %w", err)
		}
		row, ok := sd.requireRow(g.Type, func() string { return fmt.Sprintf("instances entry %d", gi) })
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
		sd.checkRootTypeEligible(row, fmt.Sprintf("instances entry %d", gi))
		if idx.exists[row] == nil {
			idx.exists[row] = make(map[string]bool, len(g.Items))
		}
		for _, item := range g.Items {
			sd.walkInstance(ctx, row, item, 0, nil, idx)
		}
	}
	return idx, nil
}

// checkRootTypeEligible refuses a group or root duplicate at position whose
// type cannot hold a root instance, by the graph package doc's "Root type
// eligibility" section. An empty group is held to it too: adapter/json and
// adapter/csv key their output by a denoted type's name. No load option excuses a member, and a
// schema-less read checks none.
func (sd *streamDecoder) checkRootTypeEligible(row int, position string) {
	if sd.schema == nil || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return // resolveTypeTable already reported the row
	}
	if !schema.Addressable(sd.schema, sd.tableIDs[row]) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNNAMEABLE_TYPE,
			fmt.Sprintf("%s denotes %s, which this schema reaches only through an intermediate import and cannot name",
				position, sd.refAt(row))).
			WithHint("import the schema that declares it directly, then read the document again").
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyTypeSchema, sd.tableIDs[row].SchemaPath().String()).
			Build())
		return
	}
	// Graph.Add's order, so a type breaking two rules draws the rule Add names.
	var rule string
	switch {
	case !t.HasPrimaryKey():
		rule = "declares no primary key"
	case t.IsPart():
		rule = "is a part type, which is reachable only as a composed child"
	case t.IsAbstract():
		rule = "is abstract"
	default:
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_ROOT,
		fmt.Sprintf("%s denotes %s, which %s", position, sd.refAt(row), rule)).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		Build())
}

// walkInstance validates one instance at its table row and registers its
// structural facts. slot is non-nil only for a root's direct composed child.
func (sd *streamDecoder) walkInstance(ctx context.Context, row int, inst instWire, depth int, slot *slotCoord, idx *docIndex) {
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
	sd.checkValueConformance(inst, row)
	sd.checkStoredKey(row, inst)
	sd.checkDeclaredProperties(row, inst.Key, inst.Properties)

	// Every address in the index is the CANONICAL key — the spelling the
	// rebuilt instance will carry — so two roots that spell one instant two
	// ways are one address here, as they are in the graph.
	keyStr := sd.canonicalWireKey(row, inst.Key)
	if depth == 0 {
		// Two roots at one address is not data the wire can carry: the format
		// has a diagnostics section for a rejected duplicate, and the graph
		// layer puts it there. Admitting the pair produced a snapshot whose
		// own accessors disagreed — RebuildSnapshot appends every instance to
		// the type's slice while its index keeps only the last, so InstancesOf
		// reported two and every edge on that key bound to one.
		if idx.exists[row][keyStr] {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %q", keyStr, sd.refAt(row))).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}
		idx.exists[row][keyStr] = true
	}
	if slot != nil {
		idx.slots[*slot] = append(idx.slots[*slot], slotChild{row: row, key: keyStr})
	}

	for relName, targets := range inst.Edges {
		for ei, e := range targets {
			sd.checkDeclaredEdgeProperties(row, inst.Key, relName, e.Properties)
			targetRow, ok := sd.requireRow(e.TargetType, func() string {
				return fmt.Sprintf("edge %d under %s.%s", ei, sd.refAt(row), relName)
			})
			if depth == 0 {
				sd.checkAssociation(idx, fmt.Sprintf("edge %d", ei), row, inst.Key, keyStr, relName, e.TargetType, e.TargetKey, "")
			}
			if !ok {
				continue
			}
			if depth == 0 {
				idx.refs = append(idx.refs, edgeRef{
					sourceRow: row,
					sourceKey: keyStr,
					targetRow: targetRow,
					targetKey: sd.canonicalWireKey(targetRow, e.TargetKey),
				})
			}
		}
	}

	for relName, children := range inst.Composed {
		target, judged := sd.checkComposedSlot(row, relName, len(children))
		if judged {
			sd.checkSiblingKeys(row, formatWireKey(inst.Key), relName, target, children)
		}
		for _, child := range children {
			// The writer emits a row at every non-root position, so an absent
			// one is malformed rather than a value to recover from context.
			childRow, ok := sd.requireRow(child.Type, func() string {
				return fmt.Sprintf("composed child under %s.%s", sd.refAt(row), relName)
			})
			if !ok {
				continue
			}
			if judged {
				sd.checkComposedChildType(row, relName, childRow, child.Key, target)
			}
			var childSlot *slotCoord
			if depth == 0 {
				childSlot = &slotCoord{parentRow: row, parentKey: keyStr, relation: relName}
			}
			sd.walkInstance(ctx, childRow, child, depth+1, childSlot, idx)
		}
	}

	if depth == 0 {
		sd.revalidateRoot(ctx, row, inst)
	}
}

// checkComposedSlot holds one slot to the rules graph.Add applies when it
// attaches a child, and returns the composition's declared target for the
// per-child check. A slot under a name the parent's type does not declare as a
// composition is refused: no declared target exists to read a child back as. A
// (one) slot holding several children is refused: the (one) composed key
// segment carries no discriminating element, so the adapter would mint one
// byte-identical _composed_key for both. A schema-less read, or a parent row
// that resolves to no type, makes no claim, as every other schema-derived check
// here.
func (sd *streamDecoder) checkComposedSlot(row int, relation string, occupants int) (schema.TypeID, bool) {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return schema.TypeID{}, false
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return schema.TypeID{}, false
	}
	rel, ok := t.Relation(relation)
	if !ok || rel.Kind() != schema.RelationComposition {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("%s holds composed children under %q, which the type does not declare as a composition",
				sd.refAt(row), relation)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
		return schema.TypeID{}, false
	}
	if occupants > 1 && !rel.IsMany() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("composition %q under %s: (one) cardinality violated, got %d children",
				relation, sd.refAt(row), occupants)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyRelationName, relation).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName()).Build())
	}
	return rel.TargetID(), true
}

// checkSiblingKeys refuses two children of one (many) slot at one canonical
// key, as graph.Add refuses them. A keyless part has no key to repeat, and a
// child of another type is checkComposedChildType's to report.
func (sd *streamDecoder) checkSiblingKeys(row int, parentKey, relation string, target schema.TypeID, children []instWire) {
	if len(children) < 2 {
		return
	}
	t, ok := sd.schema.TypeByID(target)
	if !ok || !t.HasPrimaryKey() {
		return
	}
	seen := make(map[string]int, len(children))
	for i, child := range children {
		childRow, ok := sd.rowAt(child.Type)
		if !ok || childRow >= len(sd.tableIDs) || sd.tableIDs[childRow] != target {
			continue
		}
		key := sd.canonicalWireKey(childRow, child.Key)
		if first, dup := seen[key]; dup {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("composition %q under %s[%s] holds children %d and %d at key %s",
					relation, sd.refAt(row), parentKey, first, i, key)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
				WithDetail(diag.DetailKeyRelationName, relation).
				WithDetail(diag.DetailKeyPrimaryKey, key).
				Build())
			continue
		}
		seen[key] = i
	}
}

// checkComposedChildType refuses a composed child whose type row names a type
// other than its composition's declared target. graph.Add matches the target by
// identity and nothing wider, and a writer renders the child under the
// composition's field, so such a child reads back as the target: a silent
// retype. A row that did not resolve is resolveTypeTable's to report.
func (sd *streamDecoder) checkComposedChildType(parentRow int, relation string, childRow int, key []any, want schema.TypeID) {
	if childRow >= len(sd.tableIDs) {
		return
	}
	got := sd.tableIDs[childRow]
	if got.IsZero() || got == want {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
		fmt.Sprintf("composed child %s under %s.%s is typed %s, which the composition declares as %s",
			formatWireKey(key), sd.refAt(parentRow), relation, sd.refAt(childRow), sd.identRef(want))).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(parentRow)).
		WithDetail(diag.DetailKeyRelationName, relation).
		WithDetail(diag.DetailKeyExpected, sd.identRef(want)).
		WithDetail(diag.DetailKeyGot, sd.refAt(childRow)).
		Build())
}

// checkStoredKey refuses an instance whose stored key is not the key its own
// properties state, by the rule graph.Add and graph.RebuildSnapshot apply: the
// key is non-empty, has one component per declared primary key, and each
// component agrees with its property, which is present and not null. Agreement
// is judged in canonical form, so two spellings of one instant agree. Without
// it an edge to the instance is written as a foreign key that reads back
// addressing another instance, or none. A component graph.ParseKey cannot read
// back is refused on every read; beyond that, a schema-less read, a row that resolves
// to no type, and a keyless type make no claim.
func (sd *streamDecoder) checkStoredKey(row int, inst instWire) {
	if i := unreadableWireComponent(inst.Key); i >= 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("instance of %s stores key %s, whose component %d is not a scalar graph.ParseKey reads back", sd.refAt(row), formatWireKey(inst.Key), i)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyPrimaryKey, formatWireKey(inst.Key)).
			Build())
		return
	}
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok || !t.HasPrimaryKey() {
		return
	}
	var reason string
	declared := keyArity(t)
	switch {
	case len(inst.Key) == 0:
		reason = "is empty"
	case len(inst.Key) != declared:
		reason = fmt.Sprintf("has %d components; the type declares %d", len(inst.Key), declared)
	default:
		positions := sd.rowKeyPositions(row)
		i := 0
		for pk := range t.PrimaryKeys() {
			prop := inst.Properties[pk.Name()]
			if prop == nil {
				reason = fmt.Sprintf("cannot agree with key property %q: it is absent or null", pk.Name())
				break
			}
			if !wireComponentAgrees(inst.Key[i], prop, canonicalizingConstraint(positions, i)) {
				reason = fmt.Sprintf("component %d disagrees with property %q, which holds %s",
					i, pk.Name(), formatWireKey([]any{prop}))
				break
			}
			i++
		}
	}
	if reason == "" {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
		fmt.Sprintf("instance of %s stores key %s, which %s", sd.refAt(row), formatWireKey(inst.Key), reason)).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		WithDetail(diag.DetailKeyPrimaryKey, formatWireKey(inst.Key)).
		Build())
}

// unreadableWireComponent returns the index of the first component of a
// decoded key that graph.ParseKey cannot read back — a JSON array or object,
// or a number no finite float64 holds — or -1.
func unreadableWireComponent(key []any) int {
	for i, v := range key {
		switch v := v.(type) {
		case []any, map[string]any:
			return i
		case json.Number:
			if _, err := graph.ParseKey("[" + v.String() + "]"); err != nil {
				return i
			}
		}
	}
	return -1
}

// keyArity returns the number of primary-key components t declares.
func keyArity(t *schema.Type) int {
	n := 0
	for range t.PrimaryKeys() {
		n++
	}
	return n
}

// canonicalizingConstraint returns the constraint deciding key component i's
// canonical form, or nil when its kind has none.
func canonicalizingConstraint(positions []wireKeyPosition, i int) schema.Constraint {
	for _, pos := range positions {
		if pos.index == i {
			return pos.constraint
		}
	}
	return nil
}

// wireComponentAgrees reports whether a stored key component and its property
// hold one value, as graph's keyComponentAgrees decides it: both sides in the
// form c stores where c renders them, identical scalar kinds compared
// directly, and anything else by its canonical key rendering.
func wireComponentAgrees(component, property any, c schema.Constraint) bool {
	x, y := immutable.NormalizeValue(component), immutable.NormalizeValue(property)
	if xs, ok := x.(string); ok {
		if ys, ok := y.(string); ok && xs == ys {
			return true
		}
	}
	if c != nil {
		if cx, err := value.Canonical(x, c); err == nil {
			x = cx
		}
		if cy, err := value.Canonical(y, c); err == nil {
			y = cy
		}
	}
	switch xv := x.(type) {
	case string:
		if yv, ok := y.(string); ok {
			return xv == yv
		}
	case int64:
		if yv, ok := y.(int64); ok {
			return xv == yv
		}
	case bool:
		if yv, ok := y.(bool); ok {
			return xv == yv
		}
	}
	return immutable.WrapKey([]any{x}).String() == immutable.WrapKey([]any{y}).String()
}

// revalidateRoot reconstructs one root's raw form — properties, edges as
// _target_-keyed objects, composed children as nested arrays — and runs it
// through the real validator, reporting each finding at the option's
// severity. The reconstruction inverts what the writer did to the
// validator's output, so clean data reports nothing.
func (sd *streamDecoder) revalidateRoot(ctx context.Context, row int, inst instWire) {
	if sd.revalidator == nil {
		return
	}
	if row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return
	}
	keyStr := formatWireKey(inst.Key)
	props := sd.rebuildRawProperties(t, inst, 0)
	_, res := sd.revalidator.ValidateOne(ctx, sd.tableTags[row], instance.RawInstance{Properties: props})
	// MergeRetag keeps the validator's order, so a capped collector evicts by
	// when a finding was raised, not by how its message sorts.
	sev, typeRef := sd.loadCfg.revalidateSeverity, sd.refAt(row)
	sd.collector.MergeRetag(res,
		func(s diag.Severity, code diag.Code) (diag.Severity, bool) {
			if code == diag.E_CONTEXT_CANCELLED {
				sd.cancelReported = true
			}
			if reportsTheRun(code) {
				return s, true
			}
			return sev, true
		},
		func(issue diag.Issue) diag.Issue {
			if reportsTheRun(issue.Code()) {
				return issue
			}
			return retagIssue(issue, sev, typeRef, keyStr)
		})
}

// reportsTheRun reports whether code states something about the revalidation run
// rather than the data. Such an issue keeps its severity, so a Fatal E_INTERNAL
// never reads as the caller's chosen Warning on data left unchecked.
func reportsTheRun(code diag.Code) bool {
	return code == diag.E_CONTEXT_CANCELLED || code == diag.E_INTERNAL
}

// rebuildRawProperties inverts the wire encoding back to the validator's
// input shape for one instance: edges become _target_<pk>-keyed objects with
// edge properties beside ((one) an object, (many) an array), composed
// children become nested arrays of the same form, keyed by the relation's
// JSON field name. An edge checkAssociation refused is skipped: the document
// is refused already, and the validator would only report it again.
func (sd *streamDecoder) rebuildRawProperties(t *schema.Type, inst instWire, depth int) map[string]any {
	// Bounded like walkInstance, which it follows. walkInstance stops at
	// maxComposedDepth and then runs revalidateRoot on the SAME untruncated
	// wire tree, so an unbounded rebuild recursed past the depth the reader had
	// already refused — on a document deep enough, until the stack gave out.
	if depth > maxComposedDepth {
		return nil
	}
	// The validator judges the document's own text: a number stays the
	// decoder's json.Number, which the checkers read exactly, so a wide integer
	// is refused as written rather than passed as the float64 that
	// materialization (instanceParts) reads by the wire's numeric contract.
	props := make(map[string]any, len(inst.Properties)+len(inst.Edges)+len(inst.Composed))
	maps.Copy(props, inst.Properties)

	for _, relName := range slices.Sorted(maps.Keys(inst.Edges)) {
		rel, found := t.Relation(relName)
		if !found || rel.Kind() != schema.RelationAssociation {
			continue
		}
		targetType, ok := sd.schema.TypeByID(rel.TargetID())
		if !ok {
			continue
		}
		pks := targetType.PrimaryKeysSlice()
		targets := inst.Edges[relName]
		arr := make([]any, 0, len(targets))
		for _, e := range targets {
			if !sd.wireTypeMatches(e.TargetType, rel.TargetID()) || len(e.TargetKey) != len(pks) {
				continue
			}
			obj := make(map[string]any, len(pks)+len(e.Properties))
			for i, comp := range e.TargetKey {
				obj["_target_"+pks[i].Name()] = comp
			}
			maps.Copy(obj, e.Properties)
			arr = append(arr, obj)
		}
		if rel.IsMany() {
			props[rel.FieldName()] = arr
			continue
		}
		if len(arr) > 0 {
			props[rel.FieldName()] = arr[0]
		}
	}

	for _, relName := range slices.Sorted(maps.Keys(inst.Composed)) {
		rel, found := t.Relation(relName)
		if !found || rel.Kind() != schema.RelationComposition {
			continue // checkComposedSlot refused the slot
		}
		childType, ok := sd.schema.TypeByID(rel.TargetID())
		if !ok {
			continue
		}
		children := inst.Composed[relName]
		arr := make([]any, 0, len(children))
		for _, child := range children {
			if !sd.wireTypeMatches(child.Type, rel.TargetID()) {
				continue // checkComposedChildType refused the child
			}
			arr = append(arr, sd.rebuildRawProperties(childType, child, depth+1))
		}
		props[rel.FieldName()] = arr
	}

	return props
}

// wireTypeMatches reports whether a nullable wire row denotes want. The graph
// layer resolves an edge target and a composed child by the relation's
// declared target ALONE — every staged edge in graph/build.go carries
// rel.TargetID(), and its composed check is `child.TypeID() != rel.TargetID()`
// — so the wire's own row is either the same identity or a contradiction.
// There is no subtype widening to allow for.
//
// An unresolvable row matches: walkInstances already reported it, and
// revalidation reporting it a second time under a different code helps nobody.
func (sd *streamDecoder) wireTypeMatches(row *int, want schema.TypeID) bool {
	r, ok := sd.rowAt(row)
	if !ok || r >= len(sd.tableIDs) {
		return true
	}
	got := sd.tableIDs[r]
	if got.IsZero() {
		return true // the row did not resolve; resolveTypeTable owns that
	}
	return got == want
}

// wireTypeRef renders what a nullable wire row denotes, for a diagnostic.
func (sd *streamDecoder) wireTypeRef(row *int) string {
	r, ok := sd.rowAt(row)
	if !ok {
		return "no types-table row"
	}
	return sd.refAt(r)
}

// identRef renders a schema identity in the document's own form, so an
// expected-versus-got pair reads in one vocabulary.
//
// The table is consulted first because a row already holds the rendering. An
// identity the document does not denote is rendered from the closure instead —
// NOT through schema.TagForm, which produces a bare or alias-qualified name and
// would put the two halves of one diagnostic in two vocabularies. That case is
// not rare: it is what happens when the relation's declared target is the type
// the document failed to name.
func (sd *streamDecoder) identRef(id schema.TypeID) string {
	for i, tid := range sd.tableIDs {
		if tid == id {
			return sd.refAt(i)
		}
	}
	if sd.schema != nil {
		for _, cs := range sd.schema.Closure() {
			if cs.SourceID() == id.SchemaPath() {
				return TypeRef{Schema: cs.Name(), Name: id.Name()}.String()
			}
		}
	}
	return TypeRef{Schema: id.SchemaPath().String(), Name: id.Name()}.String()
}

// retagIssue rebuilds a validator issue at the revalidation severity,
// prefixed with the loaded instance it came from. diag.Issue is immutable
// after Build, so a rebuild is the only way to move the severity; details,
// hint, span, path and related locations all travel.
func retagIssue(issue diag.Issue, sev diag.Severity, typeRef, keyStr string) diag.Issue {
	b := diag.NewIssue(sev, issue.Code(),
		fmt.Sprintf("revalidation of %s[%s]: %s", typeRef, keyStr, issue.Message())).
		WithDetails(issue.Details()...).
		WithRelated(issue.Related()...)
	if issue.HasSpan() {
		b = b.WithSpan(issue.Span())
	}
	if issue.Hint() != "" {
		b = b.WithHint(issue.Hint())
	}
	if issue.SourceName() != "" || issue.Path() != "" {
		b = b.WithPath(issue.SourceName(), issue.Path())
	}
	return b.Build()
}

// checkProvenancePath warns when a provenance path will not parse. The
// materializer falls back to the root path silently, so the read surface is
// where the loss becomes visible. The empty path is included deliberately:
// path.Parse rejects it, so it is discarded like any other unparseable path
// and must draw the same warning.
func (sd *streamDecoder) checkProvenancePath(inst instWire, row int) {
	if inst.Provenance == nil {
		return
	}
	if _, err := path.Parse(inst.Provenance.Path); err == nil {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_FALLBACK,
		fmt.Sprintf("provenance path %q could not be parsed, falling back to root path", inst.Provenance.Path)).
		WithDetail(diag.DetailKeyOriginalPath, inst.Provenance.Path).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		Build())
}

// checkDeclaredProperties refuses a stored property name the row's type does not
// declare, own or inherited. The validator stores values under declared names
// alone and [graph.Graph.Add] refuses any other, so such a document describes a
// graph no caller could have built, and every writer downstream would have to
// drop the name or write it as the field it spells. Structural, like the other
// refusals no option excuses; a schema-less read makes no claim.
func (sd *streamDecoder) checkDeclaredProperties(row int, key []any, props map[string]any) {
	if sd.schema == nil || len(props) == 0 || row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return
	}
	for _, name := range undeclaredWireNames(props, func(n string) bool { _, ok := t.Property(n); return ok }) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("instance %s[%s] holds property %q, which its type does not declare",
				sd.refAt(row), formatWireKey(key), name)).
			WithDetails(diag.TypeProp(sd.refAt(row), name)...).
			Build())
	}
}

// checkDeclaredEdgeProperties is checkDeclaredProperties for the properties of
// an edge or unresolved record under relation. A relation the row's type does
// not declare as an association has no declared edge properties to judge
// against; checkAssociation refuses that relation.
func (sd *streamDecoder) checkDeclaredEdgeProperties(row int, key []any, relation string, props map[string]any) {
	if sd.schema == nil || len(props) == 0 || row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return
	}
	rel, ok := t.Relation(relation)
	if !ok || !rel.IsAssociation() {
		return
	}
	for _, name := range undeclaredWireNames(props, func(n string) bool { _, ok := rel.Property(n); return ok }) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("edge %q of %s[%s] holds edge property %q, which the association does not declare",
				relation, sd.refAt(row), formatWireKey(key), name)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyRelationName, relation).
			WithDetail(diag.DetailKeyPropertyName, name).
			Build())
	}
}

// undeclaredWireNames returns the keys of props that declared rejects, in
// sorted order. The clean case allocates nothing.
func undeclaredWireNames(props map[string]any, declared func(string) bool) []string {
	var out []string
	for name := range props {
		if !declared(name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// checkValueConformance reports a stored value its own schema constraint
// cannot render. Off unless the caller asked for it, because Load documents
// that it does not re-validate instance data and this walk would otherwise be
// a cost every reader pays for a check nobody requested.
//
// It sits here rather than at materialization so Load, Verify and Info all
// reach it: Verify stops before a Snapshot exists, and a check placed after
// that point would be silently absent from the surface whose whole job is to
// answer whether a document is sound.
//
// Scope is the three kinds with a canonical stored form. Bounds, enums,
// patterns and invariants are not checked; silence is not proof of validity,
// and the option's documentation says so.
func (sd *streamDecoder) checkValueConformance(inst instWire, row int) {
	if !sd.loadCfg.valueConformance || sd.schema == nil || len(inst.Properties) == 0 {
		return
	}
	if row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(inst.Properties)) {
		raw := inst.Properties[name]
		if raw == nil {
			continue
		}
		prop, ok := t.Property(name)
		if !ok {
			continue
		}
		if !value.Canonicalizes(prop.Constraint()) {
			continue
		}
		// raw is the decoder's own value, never normalized: NormalizeValue
		// rewrites a list in place, and revalidation reads this same wire tree
		// afterwards and must see the document's numbers as written. A number is
		// refused by these kinds whatever form it takes.
		if _, err := value.Canonical(raw, prop.Constraint()); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_VALUE_NONCONFORMING,
				fmt.Sprintf("property %q of %s does not conform to its %s constraint: %s",
					name, formatWireKey(inst.Key), prop.Constraint().Kind(), err)).
				WithDetails(diag.TypeProp(sd.refAt(row), name)...).
				Build())
		}
	}
}

// validateDiagnostics validates duplicate and unresolved records against the
// walked index. Every conflict resolves at its stated address: a root
// conflict through the instance index, a composed conflict through the
// parent's slot, where a null stated key needs a sole occupant.
func (sd *streamDecoder) validateDiagnostics(diags diagWire, idx *docIndex) {
	for di, dup := range diags.Duplicates {
		row, rowOK := sd.requireRow(dup.Type, func() string { return fmt.Sprintf("duplicate record %d", di) })
		keyStr := formatWireKey(dup.Key)
		// requireRow's own godoc promises that a nil or out-of-range reference
		// "never binds to row 0", and the diagnostics below broke that promise
		// by rendering the sentinel: a record naming no type reported the type
		// in row 0, pointing debugging at something unrelated.
		dupRef := "(no types-table row)"
		if rowOK {
			dupRef = sd.refAt(row)
		}

		if rowOK && dup.Instance.Type != nil && *dup.Instance.Type != row {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
				fmt.Sprintf("duplicate instance %s[%s] declares type row %d (%s) but its record states row %d (%s)",
					sd.refAt(row), keyStr, *dup.Instance.Type, sd.refAt(*dup.Instance.Type), row, sd.refAt(row))).Build())
		}

		// Marshal rewrites the record's key from the instance it carries, so a
		// disagreement is an address the next write would silently discard.
		// Compared canonically where the row resolved, as every other address in
		// this function is: two spellings of one instant are one address, not a
		// disagreement. An unresolved row canonicalizes under nothing — binding
		// it to row 0 is what requireRow refuses to do — so the raw forms decide
		// and the record's own type error stands beside this one. The message
		// keeps the document's own spelling either way.
		instKey := formatWireKey(dup.Instance.Key)
		stated, carried := keyStr, instKey
		if rowOK {
			stated, carried = sd.canonicalWireKey(row, dup.Key), sd.canonicalWireKey(row, dup.Instance.Key)
		}
		if stated != carried {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states key %s but its instance carries %s",
					di, keyStr, instKey)).Build())
		}

		if rowOK {
			sd.checkProvenancePath(dup.Instance, row)
			sd.checkDeclaredProperties(row, dup.Instance.Key, dup.Instance.Properties)
			sd.checkStoredKey(row, dup.Instance)
			// A ROOT duplicate is a rejected root instance, so its type must be
			// able to hold one. A COMPOSED duplicate is not: a part type is
			// exactly what belongs there, and Relation is what separates them.
			if dup.Relation == "" {
				sd.checkRootTypeEligible(row, fmt.Sprintf("duplicate record %d", di))
			}
		}

		if len(dup.Instance.Composed) > 0 {
			b := diag.NewIssue(diag.Error, diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have composed children", dupRef, keyStr)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr)
			if rowOK {
				b = b.WithDetail(diag.DetailKeyTypeName, dupRef)
			}
			sd.collector.Collect(b.Build())
		}
		if len(dup.Instance.Edges) > 0 {
			b := diag.NewIssue(diag.Error, diag.E_SNAPSHOT_EDGES_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have edges", dupRef, keyStr)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr)
			if rowOK {
				b = b.WithDetail(diag.DetailKeyTypeName, dupRef)
			}
			sd.collector.Collect(b.Build())
		}

		if dup.Conflict == nil {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d (%s[%s]) carries no conflict block", di, dupRef, keyStr)).Build())
			continue
		}
		conflictRow, conflictOK := sd.requireRow(dup.Conflict.Type, func() string {
			return fmt.Sprintf("duplicate record %d conflict", di)
		})
		if !conflictOK {
			continue
		}
		conflictKey := sd.canonicalWireKey(conflictRow, dup.Conflict.Key)

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
		parentRow, ok := sd.requireRow(dup.ParentType, func() string {
			return fmt.Sprintf("duplicate record %d parent", di)
		})
		if !ok {
			continue
		}
		parentKey := sd.canonicalWireKey(parentRow, dup.ParentKey)
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
		sd.checkUnresolvedReason(ui, u)
		sourceRow, ok := sd.requireRow(u.SourceType, func() string {
			return fmt.Sprintf("unresolved record %d source", ui)
		})
		if ok {
			sd.checkDeclaredEdgeProperties(sourceRow, u.SourceKey, u.Relation, u.Properties)
			sourceKey := sd.canonicalWireKey(sourceRow, u.SourceKey)
			sd.warnUnresolvedRequired(sourceRow, u)
			sd.checkAssociation(idx, fmt.Sprintf("unresolved record %d", ui), sourceRow, u.SourceKey, sourceKey,
				u.Relation, u.TargetType, u.TargetKey, u.Reason)
			if !idx.rootExists(sourceRow, sourceKey) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
					fmt.Sprintf("unresolved edge source %s[%s] references non-existent instance",
						sd.refAt(sourceRow), sourceKey)).
					WithDetail(diag.DetailKeyTypeName, sd.refAt(sourceRow)).
					WithDetail(diag.DetailKeyPrimaryKey, sourceKey).
					Build())
			}
		}
		sd.requireRow(u.TargetType, func() string { return fmt.Sprintf("unresolved record %d target", ui) })
	}
}

// unresolvedReasons is the closed set graph.UnresolvedEdge documents. The
// two reasons naming a reference that never had a target carry no target key
// and no properties, so a record stating one and carrying either describes a
// state the graph model cannot hold.
var unresolvedReasons = map[string]bool{"target_missing": true, "absent": true, "empty": true}

// checkUnresolvedReason holds a record to the reasons graph.UnresolvedEdge
// documents, and a reason naming no target to carry no target key or edge
// properties, as graph.RebuildSnapshot does; Verify and Info then report what
// Load refuses.
func (sd *streamDecoder) checkUnresolvedReason(ui int, u unresolvedWire) {
	if !unresolvedReasons[u.Reason] {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %q, which is not one of target_missing, absent or empty",
				ui, u.Reason)).Build())
		return
	}
	if u.Reason == "target_missing" {
		return
	}
	if len(u.TargetKey) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %q but carries a target key %s",
				ui, u.Reason, formatWireKey(u.TargetKey))).Build())
	}
	if len(u.Properties) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %q but carries edge properties",
				ui, u.Reason)).Build())
	}
}

// warnUnresolvedRequired reports an unresolved record under a required
// association at the revalidation severity. Whether it is required is read
// from the schema, never from the record's own field. Without the option the
// record is well-formed data (Snapshot.Unresolved), so no reader pays for it.
func (sd *streamDecoder) warnUnresolvedRequired(sourceRow int, u unresolvedWire) {
	if !sd.loadCfg.revalidate {
		return
	}
	rel, ok := sd.associationAt(sourceRow, u.Relation)
	if !ok || rel.IsOptional() {
		return
	}
	sd.collector.Collect(diag.NewIssue(sd.loadCfg.revalidateSeverity, diag.W_SNAPSHOT_UNRESOLVED_REQUIRED,
		fmt.Sprintf("required association %q of %s[%s] is unresolved (%s)",
			u.Relation, sd.refAt(sourceRow), formatWireKey(u.SourceKey), u.Reason)).
		WithDetail(diag.DetailKeyRelationName, u.Relation).
		Build())
}

// associationAt returns relation as an association of the type at row, or
// false for a schema-less read, a row that resolves to no type, or a relation
// that is not an association of it.
func (sd *streamDecoder) associationAt(row int, relation string) (*schema.Relation, bool) {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return nil, false
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return nil, false
	}
	rel, ok := t.Relation(relation)
	if !ok || !rel.IsAssociation() {
		return nil, false
	}
	return rel, true
}

// checkAssociation holds one record, a root's edge (reason "") or an
// unresolved record, to what graph.Add stages: an association of the source's
// type, at its declared target, with a target key of the target's arity. It
// counts the record against a (one) association; checkOnes judges the counts.
func (sd *streamDecoder) checkAssociation(idx *docIndex, what string, sourceRow int, sourceKey []any, keyStr, relation string,
	targetRow *int, targetKey []any, reason string,
) {
	if i := unreadableWireComponent(targetKey); i >= 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("%s of %s under %q carries target key %s, whose component %d is not a scalar graph.ParseKey reads back",
				what, formatWireKey(sourceKey), relation, formatWireKey(targetKey), i)).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
	}
	if sd.schema == nil || sourceRow < 0 || sourceRow >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[sourceRow])
	if !ok {
		return
	}
	source := sd.refAt(sourceRow)
	at := fmt.Sprintf("%s of %s[%s] under %q", what, source, formatWireKey(sourceKey), relation)
	rel, ok := t.Relation(relation)
	if !ok || !rel.IsAssociation() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			at+", which the type does not declare as an association").
			WithDetail(diag.DetailKeyTypeName, source).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
		return
	}
	missing := reason == "absent" || reason == "empty"
	if !sd.wireTypeMatches(targetRow, rel.TargetID()) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
			fmt.Sprintf("%s targets %s, which the association declares as %s",
				at, sd.wireTypeRef(targetRow), sd.identRef(rel.TargetID()))).
			WithDetail(diag.DetailKeyTypeName, source).
			WithDetail(diag.DetailKeyRelationName, relation).
			WithDetail(diag.DetailKeyExpected, sd.identRef(rel.TargetID())).
			WithDetail(diag.DetailKeyGot, sd.wireTypeRef(targetRow)).
			Build())
	} else if target, ok := sd.schema.TypeByID(rel.TargetID()); ok && !missing {
		if declared := keyArity(target); len(targetKey) != declared {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("%s carries a %d-part target key; the target declares %d", at, len(targetKey), declared)).
				WithDetail(diag.DetailKeyTypeName, source).
				WithDetail(diag.DetailKeyRelationName, relation).
				Build())
		}
	}
	if !rel.IsMany() {
		slot := slotCoord{parentRow: sourceRow, parentKey: keyStr, relation: relation}
		if idx.ones == nil {
			idx.ones = make(map[slotCoord]int)
		}
		if idx.ones[slot] == 0 {
			idx.order = append(idx.order, slot)
		}
		idx.ones[slot]++
	}
}

// checkOnes refuses each (one) association holding more than one record,
// edges and unresolved records together: graph.Add stages at most one.
func (sd *streamDecoder) checkOnes(idx *docIndex) {
	for _, slot := range idx.order {
		if n := idx.ones[slot]; n > 1 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("(one) association %q of %s[%s] holds %d records",
					slot.relation, sd.refAt(slot.parentRow), slot.parentKey, n)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(slot.parentRow)).
				WithDetail(diag.DetailKeyRelationName, slot.relation).
				Build())
		}
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
	if err := sd.validateBody(ctx, groups, diags); err != nil {
		return nil, diagWire{}, false
	}
	sd.verifyIntegrity()

	if sd.collector.HasErrors() {
		return nil, diagWire{}, false
	}
	return groups, diags, true
}

// validateBody runs every structural check the format defines over a decoded
// body. Load, Verify and Info share it: the rules are one definition and only
// the gating policy differs, so a surface that summarises a document cannot
// report it clean while a surface that loads one refuses it.
func (sd *streamDecoder) validateBody(ctx context.Context, groups []instanceGroupWire, diags diagWire) error {
	idx, err := sd.walkInstances(ctx, groups)
	if err != nil {
		return err
	}
	sd.validateDiagnostics(diags, idx)
	sd.checkOnes(idx)
	sd.validateEdgeRefs(idx)
	return nil
}

// loadDocument materializes a validated document. It runs after the
// pipeline's gate, so every reference is already validated and nothing here
// collects a diagnostic.
func (sd *streamDecoder) loadDocument(groups []instanceGroupWire, diags diagWire) graph.SnapshotParts {
	types := make([]schema.TypeID, 0, len(groups))
	total := 0
	for _, g := range groups {
		total += len(g.Items)
	}
	instParts := make([]graph.InstanceParts, 0, total)
	var edgeParts []graph.EdgeParts

	for _, g := range groups {
		row := *g.Type
		id := sd.tableIDs[row]
		types = append(types, id)
		for _, item := range g.Items {
			ip := sd.instanceParts(row, item)
			instParts = append(instParts, ip)
			// Sorted, not map order: the rebuild sorts with an unstable sort, so
			// a deterministic input order is what keeps two Loads of one
			// document identical.
			for _, relName := range slices.Sorted(maps.Keys(item.Edges)) {
				for _, e := range item.Edges[relName] {
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
			Reason:     uw.Reason,
			Properties: immutable.WrapProperties(normalizeMap(uw.Properties)),
		}
		if uw.TargetKey != nil {
			up.TargetKey = immutable.WrapKey(normalizeSlice(uw.TargetKey))
		}
		unresParts = append(unresParts, up)
	}

	// The header's claim rides the parts verbatim, and is nil when the
	// document carries none, as one written from a snapshot with no claim does.
	// Collapsing that to a zero value made the next Marshal write
	// {"values":false,"associations":false}, turning silence into a claim the
	// document never made.
	var att *graph.Attestation
	if a := sd.header.Attestation; a != nil {
		att = &graph.Attestation{Values: a.Values, Associations: a.Associations}
	}

	return graph.SnapshotParts{
		Types:       types,
		Instances:   instParts,
		Edges:       edgeParts,
		Duplicates:  dupParts,
		Unresolved:  unresParts,
		Attestation: att,
	}
}

// instanceParts converts a validated wire instance to InstanceParts. The
// wire carries no instance name, and neither do the parts: RebuildSnapshot
// renders it from the identity.
func (sd *streamDecoder) instanceParts(row int, inst instWire) graph.InstanceParts {
	id := sd.tableIDs[row]
	ip := graph.InstanceParts{
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
		// A path kept as stated, when it fails to parse or parses to another
		// spelling, is what a marshal writes back: the document's own bytes.
		if parseErr != nil || parsedPath.String() != inst.Provenance.Path {
			prov = prov.WithRawPath(inst.Provenance.Path)
		}
		ip.Provenance = prov
	}

	return ip
}

// countInstances token-counts the instances section for Info, keyed by each
// group row's identity so two same-named types never merge. It reports
// nothing: validateBody has already classified the document, and a counting
// pass that reported would double every diagnostic it raised.
func (sd *streamDecoder) countInstances(groups []instanceGroupWire) (map[TypeRef]int, int) {
	counts := make(map[TypeRef]int, len(groups))
	totalEdges := 0

	for _, g := range groups {
		row, ok := sd.rowAt(g.Type)
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

	prefix, suffix, ok := integrityHashSpans(sd.data)
	if !ok {
		// Could not locate the integrity hash in the raw bytes.
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INTEGRITY_MISMATCH,
			"could not locate integrity_hash in document for verification").Build())
		return "mismatch"
	}

	h := sha256Sum(prefix, emptyJSONString, suffix)
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

// emptyJSONString is the integrity_hash value the canonical form carries.
var emptyJSONString = []byte(`""`)

// integrityHashSpans locates the integrity_hash value and returns the bytes
// on either side of it. The canonical form is prefix + `""` + suffix, which
// the caller hashes segment by segment rather than materializing.
func integrityHashSpans(data []byte) (prefix, suffix []byte, ok bool) {
	// Find the integrity_hash key in the data.
	// The key appears as "integrity_hash" followed by : and the value.
	keyBytes := []byte(`"integrity_hash"`)
	idx := bytes.Index(data, keyBytes)
	if idx < 0 {
		return nil, nil, false
	}

	// Advance past the key to find the colon.
	pos := idx + len(keyBytes)
	for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\n' || data[pos] == '\r') {
		pos++
	}
	if pos >= len(data) || data[pos] != ':' {
		return nil, nil, false
	}
	pos++ // skip colon

	// Skip whitespace after colon.
	for pos < len(data) && (data[pos] == ' ' || data[pos] == '\t' || data[pos] == '\n' || data[pos] == '\r') {
		pos++
	}
	if pos >= len(data) || data[pos] != '"' {
		return nil, nil, false
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
		return nil, nil, false
	}
	valueEnd := pos + 1 // position after closing quote

	return data[:valueStart], data[valueEnd:], true
}

// sha256Sum hashes the concatenation of segments and formats the digest as
// "sha256:<hex>". It is the one definition of that form.
func sha256Sum(segments ...[]byte) string {
	h := sha256.New()
	for _, seg := range segments {
		h.Write(seg)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
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

// canonicalWireKey renders a wire key as the address the rebuilt instance
// will carry: each component under a Timestamp, Date or UUID primary-key
// constraint of the row's type is rewritten to its canonical text, as
// graph.RebuildSnapshot rewrites it. A component the constraint cannot render,
// a row that resolves to no type, or a schema-less read leave the key as
// written. Every position that addresses an instance goes through it, so the
// document's index and the graph's agree on what one key is.
func (sd *streamDecoder) canonicalWireKey(row int, key []any) string {
	if key == nil {
		return "[]"
	}
	components := normalizeSlice(key)
	for _, pos := range sd.rowKeyPositions(row) {
		if pos.index >= len(components) {
			break
		}
		if canonical, err := value.Canonical(components[pos.index], pos.constraint); err == nil {
			components[pos.index] = canonical
		}
	}
	return immutable.WrapKey(components).String()
}

// wireKeyPosition names one primary-key component whose kind canonicalizes: its
// index in the key and the constraint deciding its canonical form. It mirrors
// graph's keyPosition, which the same range memoized on that side.
type wireKeyPosition struct {
	index      int
	constraint schema.Constraint
}

// rowKeyPositions returns the canonicalizing key positions of a table row,
// building the whole table on first use. A schema-less read and a row that
// resolves to no type have none, which leaves every key as written.
func (sd *streamDecoder) rowKeyPositions(row int) []wireKeyPosition {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return nil
	}
	if sd.keyPositions == nil {
		sd.keyPositions = make([][]wireKeyPosition, len(sd.tableIDs))
		for r, id := range sd.tableIDs {
			t, ok := sd.schema.TypeByID(id)
			if !ok {
				continue
			}
			i := 0
			for pk := range t.PrimaryKeys() {
				if value.Canonicalizes(pk.Constraint()) {
					sd.keyPositions[r] = append(sd.keyPositions[r], wireKeyPosition{index: i, constraint: pk.Constraint()})
				}
				i++
			}
		}
	}
	return sd.keyPositions[row]
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

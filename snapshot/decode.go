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
	tableTags []string         // rendered tag per table row; nil when schema is nil
	collector *diag.Collector  // accumulates diagnostics

	// loadCfg holds deserialization options (e.g., skip integrity check).
	loadCfg loadConfig

	// schema is the provided schema (nil for Info and HeaderOnlyRead).
	schema *schema.Schema

	// revalidator is non-nil only when WithRevalidation was passed and a
	// schema is present; walkInstance then runs every root back through it.
	revalidator *instance.Validator

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
// when nobody asked or no schema is present to validate against.
func newRevalidator(s *schema.Schema, cfg loadConfig) *instance.Validator {
	if !cfg.revalidate || s == nil {
		return nil
	}
	return instance.NewValidator(s)
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
		sd.checkRootTypeEligible(row, gi, len(g.Items))
		if idx.exists[row] == nil {
			idx.exists[row] = make(map[string]bool, len(g.Items))
		}
		for _, item := range g.Items {
			sd.walkInstance(ctx, row, item, 0, nil, idx)
		}
	}
	return idx, nil
}

// checkRootTypeEligible refuses an instances group whose type cannot hold a
// root instance. [github.com/simon-lentz/yammm/graph.Graph.Add] refuses an
// abstract type, a part type and one declaring no primary key, so a document
// stating any of the three describes a graph no caller could have built — and
// no option excuses it: WithRevalidation runs the validator over an
// instance's PROPERTIES, which says nothing about whether its type may stand
// alone.
//
// It is defence in depth rather than the only guard: since
// [github.com/simon-lentz/yammm/graph.RebuildSnapshot] refuses the same three,
// this library cannot write such a document — but a foreign writer can, and a
// reader that admitted one would hand the caller a snapshot no adapter can
// consume.
//
// A schema-less read (Info, HeaderOnly) resolves no types and checks nothing.
func (sd *streamDecoder) checkRootTypeEligible(row, gi, items int) {
	// An EMPTY group states that the snapshot holds the type, not that it holds
	// a root instance of it — the writer emits one for every type the snapshot
	// denotes, part types included, and the format documents that shape.
	if items == 0 || sd.schema == nil || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return // resolveTypeTable already reported the row
	}
	var rule string
	switch {
	case t.IsAbstract():
		rule = "is abstract"
	case t.IsPart():
		rule = "is a part type, which is reachable only as a composed child"
	case !t.HasPrimaryKey():
		rule = "declares no primary key"
	default:
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_ROOT,
		fmt.Sprintf("instances entry %d holds root instances of %s, which %s",
			gi, sd.refAt(row), rule)).
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

	keyStr := formatWireKey(inst.Key)
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
			targetRow, ok := sd.requireRow(e.TargetType, func() string {
				return fmt.Sprintf("edge %d under %s.%s", ei, sd.refAt(row), relName)
			})
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
			// The writer emits a row at every non-root position, so an absent
			// one is malformed rather than a value to recover from context.
			childRow, ok := sd.requireRow(child.Type, func() string {
				return fmt.Sprintf("composed child under %s.%s", sd.refAt(row), relName)
			})
			if !ok {
				continue
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
	props := sd.rebuildRawProperties(t, keyStr, inst, 0)
	_, res := sd.revalidator.ValidateOne(ctx, sd.tableTags[row], instance.RawInstance{Properties: props})
	for issue := range res.Issues() {
		// The severity map moves findings ABOUT THE DATA. A cancellation and
		// the validator's own internal failure are statements about the run:
		// retagging a Fatal E_INTERNAL to the caller's chosen Warning made
		// Load return HasErrors() false on data the validator reported it
		// could not finish checking.
		if issue.Code() == diag.E_CONTEXT_CANCELLED || issue.Code() == diag.E_INTERNAL {
			sd.collector.Collect(issue)
			continue
		}
		sd.collector.Collect(retagIssue(issue, sd.loadCfg.revalidateSeverity, sd.refAt(row), keyStr))
	}
}

// rebuildRawProperties inverts the wire encoding back to the validator's
// input shape for one instance: edges become _target_<pk>-keyed objects with
// edge properties beside ((one) an object, (many) an array), composed
// children become nested arrays of the same form, keyed by the relation's
// JSON field name. A relation name the type does not declare, a target-key
// arity mismatch, or extra targets on a (one) are reported at the option's
// severity — never silently dropped, because a document holding one is
// exactly what re-validation exists to find.
func (sd *streamDecoder) rebuildRawProperties(t *schema.Type, keyStr string, inst instWire, depth int) map[string]any {
	// Bounded like walkInstance, which it follows. walkInstance stops at
	// maxComposedDepth and then runs revalidateRoot on the SAME untruncated
	// wire tree, so an unbounded rebuild recursed past the depth the reader had
	// already refused — on a document deep enough, until the stack gave out.
	if depth > maxComposedDepth {
		return nil
	}
	// Normalized like instanceParts' materialization: the decoder reads
	// numbers as json.Number, which the validator does not accept.
	props := make(map[string]any, len(inst.Properties)+len(inst.Edges)+len(inst.Composed))
	maps.Copy(props, normalizeMap(inst.Properties))
	sev := sd.loadCfg.revalidateSeverity
	ref := schema.TagForm(sd.schema, t.ID())

	for _, relName := range slices.Sorted(maps.Keys(inst.Edges)) {
		rel, found := t.Relation(relName)
		if !found || rel.Kind() != schema.RelationAssociation {
			sd.collector.Collect(diag.NewIssue(sev, diag.E_GRAPH_UNKNOWN_RELATION,
				fmt.Sprintf("revalidation of %s[%s]: document carries edges under %q, which the type does not declare as an association",
					ref, keyStr, relName)).
				WithDetail(diag.DetailKeyTypeName, ref).
				WithDetail(diag.DetailKeyRelationName, relName).
				Build())
			continue
		}
		targetType, ok := sd.schema.TypeByID(rel.TargetID())
		if !ok {
			continue
		}
		pks := targetType.PrimaryKeysSlice()
		targets := inst.Edges[relName]
		arr := make([]any, 0, len(targets))
		for ei, e := range targets {
			if !sd.wireTypeMatches(e.TargetType, rel.TargetID()) {
				sd.collector.Collect(diag.NewIssue(sev, diag.E_SNAPSHOT_TYPE_MISMATCH,
					fmt.Sprintf("revalidation of %s[%s]: edge %d under %q targets %s, which the association declares as %s",
						ref, keyStr, ei, relName, sd.wireTypeRef(e.TargetType), sd.identRef(rel.TargetID()))).
					WithDetail(diag.DetailKeyTypeName, ref).
					WithDetail(diag.DetailKeyRelationName, relName).
					WithDetail(diag.DetailKeyExpected, sd.identRef(rel.TargetID())).
					WithDetail(diag.DetailKeyGot, sd.wireTypeRef(e.TargetType)).
					Build())
				continue
			}
			if len(e.TargetKey) != len(pks) {
				sd.collector.Collect(diag.NewIssue(sev, diag.E_SNAPSHOT_MALFORMED,
					fmt.Sprintf("revalidation of %s[%s]: edge %d under %q carries %d key components for a %d-part target key",
						ref, keyStr, ei, relName, len(e.TargetKey), len(pks))).
					WithDetail(diag.DetailKeyTypeName, ref).
					WithDetail(diag.DetailKeyRelationName, relName).
					Build())
				continue
			}
			obj := make(map[string]any, len(pks)+len(e.Properties))
			for i, comp := range normalizeSlice(e.TargetKey) {
				obj["_target_"+pks[i].Name()] = comp
			}
			maps.Copy(obj, normalizeMap(e.Properties))
			arr = append(arr, obj)
		}
		if rel.IsMany() {
			props[rel.FieldName()] = arr
			continue
		}
		if len(arr) > 1 {
			sd.collector.Collect(diag.NewIssue(sev, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("revalidation of %s[%s]: (one) association %q carries %d targets",
					ref, keyStr, relName, len(arr))).
				WithDetail(diag.DetailKeyTypeName, ref).
				WithDetail(diag.DetailKeyRelationName, relName).
				Build())
		}
		if len(arr) > 0 {
			props[rel.FieldName()] = arr[0]
		}
	}

	for _, relName := range slices.Sorted(maps.Keys(inst.Composed)) {
		rel, found := t.Relation(relName)
		if !found || rel.Kind() != schema.RelationComposition {
			sd.collector.Collect(diag.NewIssue(sev, diag.E_GRAPH_UNKNOWN_RELATION,
				fmt.Sprintf("revalidation of %s[%s]: document carries composed children under %q, which the type does not declare as a composition",
					ref, keyStr, relName)).
				WithDetail(diag.DetailKeyTypeName, ref).
				WithDetail(diag.DetailKeyRelationName, relName).
				Build())
			continue
		}
		childType, ok := sd.schema.TypeByID(rel.TargetID())
		if !ok {
			continue
		}
		children := inst.Composed[relName]
		arr := make([]any, 0, len(children))
		for _, child := range children {
			if !sd.wireTypeMatches(child.Type, rel.TargetID()) {
				sd.collector.Collect(diag.NewIssue(sev, diag.E_SNAPSHOT_TYPE_MISMATCH,
					fmt.Sprintf("revalidation of %s[%s]: composed child %s under %q is typed %s, which the composition declares as %s",
						ref, keyStr, formatWireKey(child.Key), relName,
						sd.wireTypeRef(child.Type), sd.identRef(rel.TargetID()))).
					WithDetail(diag.DetailKeyTypeName, ref).
					WithDetail(diag.DetailKeyRelationName, relName).
					WithDetail(diag.DetailKeyExpected, sd.identRef(rel.TargetID())).
					WithDetail(diag.DetailKeyGot, sd.wireTypeRef(child.Type)).
					Build())
				continue
			}
			arr = append(arr, sd.rebuildRawProperties(childType, formatWireKey(child.Key), child, depth+1))
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
func (sd *streamDecoder) identRef(id schema.TypeID) string {
	for i, tid := range sd.tableIDs {
		if tid == id {
			return sd.refAt(i)
		}
	}
	return schema.TagForm(sd.schema, id)
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
	sd.collector.Collect(diag.NewIssue(diag.Warning, diag.E_SNAPSHOT_PATH_FALLBACK,
		fmt.Sprintf("provenance path %q could not be parsed, falling back to root path", inst.Provenance.Path)).
		WithDetail(diag.DetailKeyOriginalPath, inst.Provenance.Path).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		Build())
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
		if _, err := value.Canonical(immutable.NormalizeValue(raw), prop.Constraint()); err != nil {
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
		if instKey := formatWireKey(dup.Instance.Key); instKey != keyStr {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states key %s but its instance carries %s",
					di, keyStr, instKey)).Build())
		}

		if rowOK {
			sd.checkProvenancePath(dup.Instance, row)
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
		parentRow, ok := sd.requireRow(dup.ParentType, func() string {
			return fmt.Sprintf("duplicate record %d parent", di)
		})
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
		sd.checkUnresolvedReason(ui, u)
		sourceRow, ok := sd.requireRow(u.SourceType, func() string {
			return fmt.Sprintf("unresolved record %d source", ui)
		})
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
		sd.requireRow(u.TargetType, func() string { return fmt.Sprintf("unresolved record %d target", ui) })
	}
}

// unresolvedReasons is the closed set graph.UnresolvedEdge documents. The
// two reasons naming a reference that never had a target carry no target key
// and no properties, so a record stating one and carrying either describes a
// state the graph model cannot hold.
var unresolvedReasons = map[string]bool{"target_missing": true, "absent": true, "empty": true}

// checkUnresolvedReason holds the reader to the contract the writer already
// meets. Without it a hand-edited record loads a target key and properties
// into a value graph.UnresolvedEdge documents as always empty, and the next
// Marshal discards them silently.
func (sd *streamDecoder) checkUnresolvedReason(ui int, u unresolvedWire) {
	// Gated on the revalidation option: the record is well-formed data
	// (Snapshot.Unresolved), so without the option no reader pays a new
	// diagnostic for it.
	if sd.loadCfg.revalidate && u.Required {
		sourceRef := "?"
		if row, ok := sd.rowAt(u.SourceType); ok {
			sourceRef = sd.refAt(row)
		}
		sd.collector.Collect(diag.NewIssue(sd.loadCfg.revalidateSeverity, diag.W_SNAPSHOT_UNRESOLVED_REQUIRED,
			fmt.Sprintf("required association %q of %s[%s] is unresolved (%s)",
				u.Relation, sourceRef, formatWireKey(u.SourceKey), u.Reason)).
			WithDetail(diag.DetailKeyRelationName, u.Relation).
			Build())
	}
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
	sd.validateEdgeRefs(idx)
	return nil
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

	// The header's claim rides the parts verbatim; an absent field is a
	// pre-v0.15.0 document and reads as both false.
	// nil when the document carries no attestation — a pre-v0.15.0 file, say.
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
// instance name is derived from the resolved identity, never carried on the
// wire, so a name cannot disagree with the identity beside it.
func (sd *streamDecoder) instanceParts(row int, inst instWire) graph.InstanceParts {
	id := sd.tableIDs[row]
	ip := graph.InstanceParts{
		TypeName:   sd.tableTags[row],
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

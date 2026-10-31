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

// streamDecoder is the shared infrastructure of the package's readers.
// Byte-based callers set data; reader-based callers set
// reader and can only invoke decodeHeader, because decodeSections and
// verifyIntegrity require the full byte slice.
type streamDecoder struct {
	data      []byte           // raw input bytes; nil for reader-based callers
	reader    io.Reader        // one-shot reader; non-nil only when data == nil
	header    headerWire       // decoded header
	typeTable []typeTableEntry // decoded types table
	tableIDs  []schema.TypeID  // identity per table row, zero where unresolved
	tableTags []string         // rendered tag per table row; nil when schema is nil
	// rootlessRows holds each row [streamDecoder.checkRootTypeEligible] refused.
	rootlessRows map[int]bool
	collector    *diag.Collector // accumulates diagnostics

	// keyPositions is the canonicalizing primary-key positions per table row,
	// built once on first use. Every address the reader resolves went through
	// TypeByID and a PrimaryKeys walk per call — the same work graph's
	// canonicalizer memoizes per schema, left per-call one layer down.
	keyPositions [][]wireKeyPosition

	// closureNames is the import closure's schema names, built once on first
	// use by closureHolds.
	closureNames map[string]struct{}

	// loadCfg holds deserialization options (e.g., skip integrity check).
	loadCfg loadConfig

	// schema is the provided schema, nil for a reader that validates against none.
	schema *schema.Schema

	// revalidator is non-nil only when WithRevalidation was passed and a
	// schema is present; [streamDecoder.revalidate] then runs each root the
	// structural checks left whole back through it.
	revalidator *instance.Validator

	// cancelReported is set once a revalidated root has reported the context's
	// cancellation, so revalidate's own poll does not report it a second time.
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
		return fmt.Errorf("expected JSON object, got %s", diag.ValueText(fmt.Sprint(tok)))
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
		return fmt.Errorf("yammm_snapshot must be the first key in the top-level object; got %s", diag.QuoteValue(firstKey))
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
			"unrecognized feature "+diag.QuoteValue(f)).
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
				"schema structural hash mismatch for "+diag.QuoteValue(sd.header.SchemaName)).
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
			return fmt.Errorf("failed to skip key %s: %w", diag.QuoteValue(key), err)
		}
	}

	return errors.New("types key not found in document")
}

// decodeSections decodes the instances and diagnostics sections by
// re-scanning the raw data. Its callers run [checkDocument] before the header,
// so it decodes a body whose four keys are present, ordered, unique and
// non-null, and in which no object repeats a member name or spells a declared
// one in another case: encoding/json keeps the last of two members of one
// name, merges two objects under one struct field, and matches a struct
// field's name in any case, so either would decode as data Marshal never
// wrote.
func (sd *streamDecoder) decodeSections() ([]instanceGroupWire, diagWire, error) {
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

// edgeRef is an edge no check of its own refused, for the reference check. slot
// and own place it among its source's records, and one reports that its
// association is a (one) association of a known type.
type edgeRef struct {
	targetRow int
	targetKey string
	slot      slotCoord
	own       int
	one       bool
}

// docIndex carries the structural facts one walk of the instances section
// produces: root existence, slot occupancy, and edge references. ones counts
// the records under each (one) association, edges and unresolved records
// together, and is allocated at the first such record.
//
// A unit a check refused feeds none of them: an edge or an unresolved record
// feeds no reference and no (one) count, and a slot in refusedSlots or
// unheldSlots or a record in refusedRecords is read by no later check. One
// break then draws one issue. The reference check refuses edges too, so an edge
// waits in refs until [streamDecoder.validateEdgeRefs] feeds it. The record facts
// refuse records too, so an unresolved record waits in pending until
// [docIndex.countPending] counts it.
type docIndex struct {
	// exists holds each address a root stands at, true where a sound root holds
	// it: one of a type that can hold a root, whose stored key checkStoredKey
	// accepts.
	exists map[int]map[string]bool
	slots  map[slotCoord][]slotChild
	refs   []edgeRef
	// ones counts the records under each (one) association of each sound root,
	// and order holds each such association in the order met.
	ones  map[oneCoord]int
	order []oneCoord
	// pending holds each unresolved record checkRecord accepted under a (one)
	// association of a root that holds its address.
	pending []pendingOne
	// revalidate holds each root the walk accepted whole, in document order,
	// for [streamDecoder.revalidate]. Only a revalidating read fills it.
	revalidate []rootAt
	// soundRoots counts the sound roots at each address, relation left empty.
	soundRoots map[slotCoord]int
	// edges counts the edges of each root that stands at an address, per
	// relation name, keyed as a slot, an edge the reader refused among them:
	// the document holds it there.
	// soundEdges counts the edges no check refused. checkRecordFacts reads
	// both: a refused edge is held, and no record beside another.
	edges      map[slotCoord]int
	soundEdges map[slotCoord]int
	// refusedSlots holds each composition slot the walk refused of a root that
	// holds its address.
	refusedSlots map[slotCoord]bool
	// unheldSlots holds the same of a root whose key a check refused, at both
	// its addresses; [docIndex.slotRefused] reads it.
	unheldSlots map[slotCoord]bool
	// refusedRecords holds the index of each unresolved record a check refused.
	refusedRecords map[int]bool
	// refusedRoots holds each address, relation left empty, whose holder a
	// check refused an association of: a required one it holds nothing under,
	// or a (one) one that holds more than one record.
	// danglingOnes counts the edges under a (one) association that name no root.
	refusedRoots map[slotCoord]bool
	danglingOnes map[oneCoord]int
	// shared holds each address two roots stand at, their keys sound or not,
	// relation left empty. An unresolved record there names the address, not a
	// root: it counts toward no (one) association, stands beside no other
	// record, and a duplicate parented there resolves no conflict.
	shared map[slotCoord]bool
	// stated holds each address a refused unit states, relation left empty:
	// the address a root's key properties name where checkStoredKey refused
	// its stored key, and both addresses of each root in a group entry the
	// walk refused for repeating a row, its stored key's and its key
	// properties'. statedChildren holds the same for a root's
	// direct composed child, by slot. A reference there that no root or child
	// resolves draws no issue: the refusal is the break's one issue. A
	// schema-less read names no key property, so it states stored keys alone.
	stated         map[slotCoord]bool
	statedChildren map[slotCoord]map[slotChild]bool
	// unrowed holds each root of a group entry whose types row the walk
	// refused. Its row is unknown, so both its addresses are stated at every
	// row; unrowedAt holds each row's canonical keys, built on first use.
	// refusedChildren holds each root's direct composed child the walk refused for
	// its row, by slot: one with no row, or one of a type other than the composition's.
	unrowed         []instWire
	unrowedAt       map[int]map[string]bool
	refusedChildren map[slotCoord][]instWire
	// refusedChildrenAt holds the addresses of refusedChildren as read at a row,
	// built on first use, so a slot is read once however many duplicates name it.
	refusedChildrenAt map[int]map[slotChild]map[string]bool
	// occupants holds slots as a set per slot, built on first use.
	occupants map[slotCoord]map[slotChild]bool
}

// rootExists reports whether a root stands at the address, its key sound or
// not: a reference to it resolves.
func (idx *docIndex) rootExists(row int, key string) bool {
	_, ok := idx.exists[row][key]
	return ok
}

func markSlot(set *map[slotCoord]bool, slot slotCoord) {
	if *set == nil {
		*set = make(map[slotCoord]bool)
	}
	(*set)[slot] = true
}

// slotRefused reports whether a duplicate parented at slot meets a refusal of
// the slot the walk made: the holder's, or, where no sound root stands at the
// address, a refused root's.
func (idx *docIndex) slotRefused(slot slotCoord) bool {
	return idx.refusedSlots[slot] || idx.unheldSlots[slot] && !idx.exists[slot.parentRow][slot.parentKey]
}

// unjudged reports whether two roots stand at the address.
func (idx *docIndex) unjudged(row int, key string) bool {
	return idx.shared[slotCoord{parentRow: row, parentKey: key}]
}

// occupies reports whether child stands in slot. A slot is read once, however
// many duplicates name it.
func (idx *docIndex) occupies(slot slotCoord, child slotChild) bool {
	set, indexed := idx.occupants[slot]
	if !indexed {
		set = make(map[slotChild]bool, len(idx.slots[slot]))
		for _, occ := range idx.slots[slot] {
			set[occ] = true
		}
		if idx.occupants == nil {
			idx.occupants = make(map[slotCoord]map[slotChild]bool)
		}
		idx.occupants[slot] = set
	}
	return set[child]
}

// held reports whether a sound root holds the address alone.
func (idx *docIndex) held(row int, key string) bool {
	return idx.exists[row][key] && !idx.unjudged(row, key)
}

func (idx *docIndex) state(row int, key string) {
	if idx.stated == nil {
		idx.stated = make(map[slotCoord]bool)
	}
	idx.stated[slotCoord{parentRow: row, parentKey: key}] = true
}

func (idx *docIndex) stateChild(slot slotCoord, child slotChild) {
	if idx.statedChildren == nil {
		idx.statedChildren = make(map[slotCoord]map[slotChild]bool)
	}
	if idx.statedChildren[slot] == nil {
		idx.statedChildren[slot] = make(map[slotChild]bool)
	}
	idx.statedChildren[slot][child] = true
}

// refusedAddress reports whether a refused unit states the root address, so a
// reference there that resolves to no root is judged no further.
func (sd *streamDecoder) refusedAddress(idx *docIndex, row int, key string) bool {
	if idx.stated[slotCoord{parentRow: row, parentKey: key}] {
		return true
	}
	if len(idx.unrowed) == 0 {
		return false
	}
	at, ok := idx.unrowedAt[row]
	if !ok {
		at = make(map[string]bool, len(idx.unrowed))
		for _, inst := range idx.unrowed {
			at[sd.canonicalWireKey(row, inst.Key)] = true
			if stated, ok := sd.statedKey(row, inst); ok {
				at[stated] = true
			}
		}
		if idx.unrowedAt == nil {
			idx.unrowedAt = make(map[int]map[string]bool)
		}
		idx.unrowedAt[row] = at
	}
	return at[key]
}

// childStated reports whether a refused child of slot states the address, so a
// conflict there that no occupant resolves is judged no further. An empty key
// names a slot's sole occupant, which a child the walk refused can be.
func (sd *streamDecoder) childStated(idx *docIndex, slot slotCoord, row int, rawKey []any, key string) bool {
	if idx.statedChildren[slot][slotChild{row: row, key: key}] {
		return true
	}
	children := idx.refusedChildren[slot]
	if len(children) == 0 {
		return false
	}
	if len(rawKey) == 0 {
		return true
	}
	at := slotChild{row: row, key: slot.parentKey + "\x00" + slot.relation}
	if idx.refusedChildrenAt[slot.parentRow] == nil {
		if idx.refusedChildrenAt == nil {
			idx.refusedChildrenAt = make(map[int]map[slotChild]map[string]bool)
		}
		idx.refusedChildrenAt[slot.parentRow] = make(map[slotChild]map[string]bool)
	}
	stated, ok := idx.refusedChildrenAt[slot.parentRow][at]
	if !ok {
		stated = make(map[string]bool, len(children))
		for _, child := range children {
			stated[sd.canonicalWireKey(row, child.Key)] = true
			if s, ok := sd.statedKey(row, child); ok {
				stated[s] = true
			}
		}
		idx.refusedChildrenAt[slot.parentRow][at] = stated
	}
	return stated[key]
}

func (idx *docIndex) refuseRecord(ui int) {
	if idx.refusedRecords == nil {
		idx.refusedRecords = make(map[int]bool)
	}
	idx.refusedRecords[ui] = true
}

// oneCoord addresses a (one) association of one root: the root's slot, and
// its ordinal among the sound roots at its address. The root
// that holds the address is ordinal 0, the one its records count toward.
type oneCoord struct {
	slot slotCoord
	root int
}

// countOne counts one record under a (one) association of a root.
func (idx *docIndex) countOne(at oneCoord) {
	if idx.ones == nil {
		idx.ones = make(map[oneCoord]int)
	}
	if idx.ones[at] == 0 {
		idx.order = append(idx.order, at)
	}
	idx.ones[at]++
}

// rootAt is the root item of the types-table row row.
type rootAt struct {
	row  int
	item *instWire
}

// pendingOne is unresolved record ui under the (one) association at.
type pendingOne struct {
	ui int
	at oneCoord
}

// countPending counts each pending record no check refused.
func (idx *docIndex) countPending() {
	for _, p := range idx.pending {
		if !idx.refusedRecords[p.ui] {
			idx.countOne(p.at)
		}
	}
}

// mark is the collector's Error and Fatal count at a point of the walk.
type mark int

func (sd *streamDecoder) mark() mark { return mark(sd.collector.ErrorCount()) }

// refusedSince reports whether a check has refused anything since m: whether
// the collector has gained an Error or a Fatal issue.
func (sd *streamDecoder) refusedSince(m mark) bool { return sd.collector.ErrorCount() > int(m) }

// sortedNames appends m's keys to buf in order and returns it. A caller sizes
// buf on its stack, so a root with few relations allocates nothing for them.
func sortedNames[V any](m map[string]V, buf []string) []string {
	for k := range m {
		buf = append(buf, k)
	}
	slices.Sort(buf)
	return buf
}

// walkInstances runs the one structural pass over the instances section, and
// queues each root it accepted whole for revalidation.
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
			idx.unrowed = append(idx.unrowed, g.Items...)
			continue
		}
		if first, dup := seenRows[row]; dup {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("instances entries %d and %d both reference types-table row %d (%s)",
					first, gi, row, sd.refText(row))).Build())
			for _, item := range g.Items {
				idx.state(row, sd.canonicalWireKey(row, item.Key))
				if stated, ok := sd.statedKey(row, item); ok {
					idx.state(row, stated)
				}
			}
			continue
		}
		seenRows[row] = gi
		eligible := sd.checkRootTypeEligible(row, fmt.Sprintf("instances entry %d", gi))
		if idx.exists[row] == nil {
			idx.exists[row] = make(map[string]bool, len(g.Items))
		}
		for ii := range g.Items {
			start := sd.mark()
			// A type that cannot hold a root is the entry's one refusal, so a
			// reference to any root of it is judged no further, and it holds no
			// address and no count.
			if !eligible {
				idx.state(row, sd.canonicalWireKey(row, g.Items[ii].Key))
			}
			sd.walkInstance(row, g.Items[ii], 0, nil, eligible, idx)
			// Revalidation judges the values of a root the structural pass
			// accepted whole. A refused one already refuses the document, and the
			// validator would report its break again under another code.
			if sd.revalidator != nil && eligible && !sd.refusedSince(start) {
				idx.revalidate = append(idx.revalidate, rootAt{row: row, item: &g.Items[ii]})
			}
		}
	}
	return idx, nil
}

// checkRootTypeEligible refuses a type that cannot hold a root instance, by the
// graph package doc's "Root type eligibility" section, at the first group or root
// duplicate position that names it, and reports whether the type can hold one. An
// empty group is held to it too: adapter/json and adapter/csv key their output by a
// denoted type's name. No load option excuses a member, and a schema-less read
// checks none.
func (sd *streamDecoder) checkRootTypeEligible(row int, position string) bool {
	if sd.schema == nil || row >= len(sd.tableIDs) {
		return true
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return true // resolveTypeTable already reported the row
	}
	// One schema change fixes every position that names the type, so it is one break.
	if sd.rootlessRows[row] {
		return false
	}
	refuse := func() {
		if sd.rootlessRows == nil {
			sd.rootlessRows = make(map[int]bool)
		}
		sd.rootlessRows[row] = true
	}
	if !schema.Addressable(sd.schema, sd.tableIDs[row]) {
		refuse()
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNNAMEABLE_TYPE,
			fmt.Sprintf("%s denotes %s, which this schema reaches only through an intermediate import and cannot name",
				position, sd.refText(row))).
			WithHint("import the schema that declares it directly, then read the document again").
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyTypeSchema, sd.tableIDs[row].SchemaPath().String()).
			Build())
		return false
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
		return true
	}
	refuse()
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_ROOT,
		fmt.Sprintf("%s denotes %s, which %s", position, sd.refText(row), rule)).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		Build())
	return false
}

// walkInstance validates one instance at its table row and registers its
// structural facts, and reports whether its stored key is one a later check
// may compare. slot is non-nil only for a root's direct composed child. Every
// loop that can collect runs in a fixed order, so a capped collector keeps the
// same issues on every read.
func (sd *streamDecoder) walkInstance(row int, inst instWire, depth int, slot *slotCoord, eligible bool, idx *docIndex) bool {
	if depth > maxComposedDepth {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DEPTH_EXCEEDED,
			fmt.Sprintf("composed nesting depth %d exceeds limit %d", depth, maxComposedDepth)).
			WithDetail(diag.DetailKeyDepth, strconv.Itoa(depth)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			Build())
		return false
	}

	if depth == 0 && inst.Type != nil && *inst.Type != row {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
			fmt.Sprintf("root instance %s declares type row %d (%s) inside the section entry of row %d (%s)",
				diag.ValueText(formatWireKey(inst.Key)), *inst.Type, sd.refText(*inst.Type), row, sd.refText(row))).Build())
	}

	if depth > 0 && len(inst.Edges) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("composed child %s has edges (composed children must not carry edges)", sd.refText(row))).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			Build())
	}

	sd.checkProvenancePath(inst, row)
	sd.checkValueConformance(inst, row)
	keyed := sd.checkStoredKey(row, inst)
	sd.checkDeclaredProperties(row, inst.Key, inst.Properties)

	// Every address in the index is the CANONICAL key — the spelling the
	// rebuilt instance will carry — so two roots that spell one instant at one
	// offset two ways are one address here, as they are in the graph.
	keyStr := sd.canonicalWireKey(row, inst.Key)
	statedAt := ""
	if !keyed {
		if stated, ok := sd.statedKey(row, inst); ok && stated != keyStr {
			switch {
			case depth == 0:
				idx.state(row, stated)
				statedAt = stated
			case slot != nil:
				idx.stateChild(*slot, slotChild{row: row, key: stated})
			}
		}
	}
	// addressed reports that this root holds its address. One that does not
	// feeds no slot there: they are the holder's.
	//
	// A root of a type that declares a key stands at its stored address, so a
	// reference to it resolves. Only a root of a type that can hold one, whose
	// key checkStoredKey accepts, holds the address, whichever comes first: a
	// refused one is no duplicate and owns no slot or count there. own is the
	// root's ordinal among the sound roots at the address, -1 for no such root:
	// each one's own (one) edges are a count, as the document nests them in it.
	sound := keyed && eligible
	addressed, own, secondHolder := false, -1, false
	if depth == 0 && sd.declaresKey(row) {
		addr := slotCoord{parentRow: row, parentKey: keyStr}
		holder, present := idx.exists[row][keyStr]
		if present {
			if idx.shared == nil {
				idx.shared = make(map[slotCoord]bool)
			}
			idx.shared[addr] = true
		}
		if sound {
			if idx.soundRoots == nil {
				idx.soundRoots = make(map[slotCoord]int)
			}
			own = idx.soundRoots[addr]
			idx.soundRoots[addr]++
		}
		switch {
		case !present:
			idx.exists[row][keyStr] = sound
			addressed = sound
		case !sound:
		case !holder:
			idx.exists[row][keyStr] = true
			addressed = true
		default:
			// Two roots at one address is not data the wire can carry: the format
			// has a diagnostics section for a rejected duplicate, and the graph
			// layer puts it there. Admitting the pair produced a snapshot whose
			// own accessors disagreed — RebuildSnapshot appends every instance to
			// the type's slice while its index keeps only the last, so InstancesOf
			// reported two and every edge on that key bound to one.
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %s", diag.ValueText(keyStr), sd.refQuoted(row))).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
			secondHolder = true
		}
	}
	if slot != nil {
		idx.slots[*slot] = append(idx.slots[*slot], slotChild{row: row, key: keyStr})
	}

	if depth == 0 {
		sd.walkEdges(row, inst, keyStr, own, sd.declaresKey(row), idx)
	}

	var relations [4]string
	for _, relName := range sortedNames(inst.Composed, relations[:0]) {
		children := inst.Composed[relName]
		start := sd.mark()
		target, judged := sd.checkComposedSlot(row, relName, len(children))
		slotRefused := sd.refusedSince(start)
		var childSlot *slotCoord
		if addressed {
			childSlot = &slotCoord{parentRow: row, parentKey: keyStr, relation: relName}
		}
		refuseSlot := func() {
			switch {
			case addressed:
				markSlot(&idx.refusedSlots, *childSlot)
			case secondHolder:
				// Whichever root comes first, a slot either refused is refused.
				markSlot(&idx.refusedSlots, slotCoord{parentRow: row, parentKey: keyStr, relation: relName})
			case depth == 0 && !sound:
				markSlot(&idx.unheldSlots, slotCoord{parentRow: row, parentKey: keyStr, relation: relName})
				if statedAt != "" {
					markSlot(&idx.unheldSlots, slotCoord{parentRow: row, parentKey: statedAt, relation: relName})
				}
			}
		}
		if slotRefused {
			refuseSlot()
		}
		keyOK := make([]bool, len(children))
		for i, child := range children {
			// The writer emits a row at every non-root position, so an absent
			// one is malformed rather than a value to recover from context.
			childStart := sd.mark()
			childRow, ok := sd.requireRow(child.Type, func() string {
				return fmt.Sprintf("composed child under %s.%s", sd.refText(row), diag.ValueText(relName))
			})
			if ok && judged {
				sd.checkComposedChildType(row, relName, childRow, child.Key, target)
			}
			if childSlot != nil && sd.refusedSince(childStart) {
				if idx.refusedChildren == nil {
					idx.refusedChildren = make(map[slotCoord][]instWire)
				}
				idx.refusedChildren[*childSlot] = append(idx.refusedChildren[*childSlot], child)
			}
			if !ok {
				continue
			}
			keyOK[i] = sd.walkInstance(childRow, child, depth+1, childSlot, true, idx)
		}
		if judged && !slotRefused {
			start := sd.mark()
			sd.checkSiblingKeys(row, formatWireKey(inst.Key), relName, target, children, keyOK)
			// Two children at one key refuse the slot as two under a (one) slot do:
			// it holds no occupants a duplicate's conflict is resolved against.
			if sd.refusedSince(start) {
				refuseSlot()
			}
		}
	}
	return keyed
}

// walkEdges holds each edge of the root at row to the facts it shows by itself
// and feeds the index. Every edge of a root that stands at an address counts as
// held there. An edge a check refused names no reference, and every other waits
// in refs for [streamDecoder.validateEdgeRefs]; own is the root's ordinal among
// the sound roots at its address.
func (sd *streamDecoder) walkEdges(row int, inst instWire, keyStr string, own int, stands bool, idx *docIndex) {
	var relations [4]string
	for _, relName := range sortedNames(inst.Edges, relations[:0]) {
		slot := slotCoord{parentRow: row, parentKey: keyStr, relation: relName}
		if len(inst.Edges[relName]) == 0 {
			sd.checkEdgeListName(row, inst.Key, relName)
			continue
		}
		for ei, e := range inst.Edges[relName] {
			if stands {
				if idx.edges == nil {
					idx.edges = make(map[slotCoord]int)
				}
				idx.edges[slot]++
			}
			start := sd.mark()
			sd.checkDeclaredEdgeProperties(row, inst.Key, relName, e.Properties)
			targetRow, _ := sd.requireRow(e.TargetType, func() string {
				return fmt.Sprintf("edge %d under %s.%s", ei, sd.refText(row), diag.ValueText(relName))
			})
			rel := sd.checkAssociation(fmt.Sprintf("edge %d", ei), row, inst.Key, relName, e.TargetType, e.TargetKey, "", true)
			if sd.refusedSince(start) {
				continue
			}
			idx.refs = append(idx.refs, edgeRef{
				targetRow: targetRow,
				targetKey: sd.canonicalWireKey(targetRow, e.TargetKey),
				slot:      slot,
				own:       own,
				one:       rel != nil && !rel.IsMany(),
			})
		}
	}
}

// declaresKey reports whether the type at row declares a primary key, the one
// kind of type whose root holds an address. A schema-less read, or a row that
// resolves to no type, compares every root's key as written.
func (sd *streamDecoder) declaresKey(row int) bool {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return true
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	return !ok || t.HasPrimaryKey()
}

// declaresComposition reports whether the type at row declares relation as a
// composition. A schema-less read, or a row that resolves to no type, reports
// true: it judges no name.
func (sd *streamDecoder) declaresComposition(row int, relation string) bool {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return true
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return true
	}
	rel, ok := t.Relation(relation)
	return ok && rel.Kind() == schema.RelationComposition
}

// checkEdgeListName holds the name of a root's empty or null edge list to the
// rule checkAssociation holds each edge to: an association the root's type
// declares. No edge in the list reaches that check, and Marshal writes no
// empty list, so the name is the list's one fact.
func (sd *streamDecoder) checkEdgeListName(row int, key []any, relation string) {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok {
		return
	}
	if rel, ok := t.Relation(relation); ok && rel.Kind() == schema.RelationAssociation {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
		fmt.Sprintf("the empty edge list of %s[%s] under %s, which the type does not declare as an association",
			sd.refText(row), diag.ValueText(formatWireKey(key)), diag.QuoteValue(relation))).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		WithDetail(diag.DetailKeyRelationName, relation).
		Build())
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
			fmt.Sprintf("%s holds composed children under %s, which the type does not declare as a composition",
				sd.refText(row), diag.QuoteValue(relation))).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
		return schema.TypeID{}, false
	}
	if occupants > 1 && !rel.IsMany() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("composition %q under %s: (one) cardinality violated, got %d children",
				relation, sd.refText(row), occupants)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyRelationName, relation).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName()).Build())
	}
	return rel.TargetID(), true
}

// checkSiblingKeys refuses two children of one slot at one canonical key, as
// graph.Add refuses them. The caller passes no slot checkComposedSlot refused,
// so a slot holding two children is a (many) one. A keyless part has no key to
// repeat, a child of another type is checkComposedChildType's to report, and a
// child whose key keyOK does not vouch for is checkStoredKey's.
func (sd *streamDecoder) checkSiblingKeys(row int, parentKey, relation string, target schema.TypeID, children []instWire, keyOK []bool) {
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
		if !ok || !keyOK[i] || childRow >= len(sd.tableIDs) || sd.tableIDs[childRow] != target {
			continue
		}
		key := sd.canonicalWireKey(childRow, child.Key)
		if first, dup := seen[key]; dup {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("composition %q under %s[%s] holds children %d and %d at key %s",
					relation, sd.refText(row), diag.ValueText(parentKey), first, i, diag.ValueText(key))).
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
			diag.ValueText(formatWireKey(key)), sd.refText(parentRow), relation, sd.refText(childRow), sd.identRef(want))).
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
// is judged in canonical form, so two spellings of one instant at one offset agree. Without
// it an edge to the instance is written as a foreign key that reads back
// addressing another instance, or none. A component graph.ParseKey cannot read
// back is refused on every read; beyond that, a schema-less read, a row that resolves
// to no type, and a keyless type make no claim. It reports whether it refused
// nothing.
func (sd *streamDecoder) checkStoredKey(row int, inst instWire) bool {
	if i := unreadableWireComponent(inst.Key); i >= 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("instance of %s stores key %s, whose component %d is not a scalar graph.ParseKey reads back", sd.refText(row), diag.ValueText(literalWireKey(inst.Key)), i)).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
			WithDetail(diag.DetailKeyPrimaryKey, literalWireKey(inst.Key)).
			Build())
		return false
	}
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return true
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok || !t.HasPrimaryKey() {
		return true
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
					i, pk.Name(), diag.ValueText(formatWireKey([]any{prop})))
				break
			}
			i++
		}
	}
	if reason == "" {
		return true
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
		fmt.Sprintf("instance of %s stores key %s, which %s", sd.refText(row), diag.ValueText(formatWireKey(inst.Key)), reason)).
		WithDetail(diag.DetailKeyTypeName, sd.refAt(row)).
		WithDetail(diag.DetailKeyPrimaryKey, formatWireKey(inst.Key)).
		Build())
	return false
}

// statedKey returns the canonical address inst's key properties name, or false
// where its type declares no key or a key property is absent or unreadable.
func (sd *streamDecoder) statedKey(row int, inst instWire) (string, bool) {
	if sd.schema == nil || row < 0 || row >= len(sd.tableIDs) {
		return "", false
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[row])
	if !ok || !t.HasPrimaryKey() {
		return "", false
	}
	var key []any
	for pk := range t.PrimaryKeys() {
		prop := inst.Properties[pk.Name()]
		if prop == nil {
			return "", false
		}
		// As materialization reads it: a property's spelling of a number is no
		// address, and the wire's numeric contract is.
		key = append(key, immutable.NormalizeValue(prop))
	}
	if unreadableWireComponent(key) >= 0 {
		return "", false
	}
	return sd.canonicalWireKey(row, key), true
}

// unreadableWireComponent returns the index of the first component of a
// decoded key that graph.ParseKey cannot read back as written — a JSON array
// or object, a number no finite float64 holds, or a number that renders back
// otherwise — or -1.
func unreadableWireComponent(key []any) int {
	for i, v := range key {
		switch v := v.(type) {
		case []any, map[string]any:
			return i
		case json.Number:
			// A literal must read back as written: one that renders otherwise, as
			// 1.0 or a uint64 past int64 does, puts the instance at another
			// address than the one the document states.
			if !readsBack("[" + v.String() + "]") {
				return i
			}
		case float64:
			// A normalized number, as a negative zero, which renders otherwise.
			written, err := json.Marshal([]any{v})
			if err != nil || !readsBack(string(written)) {
				return i
			}
		}
	}
	return -1
}

// literalWireKey renders key with each number as the document spells it: a
// component [unreadableWireComponent] refused can render as a key that reads back.
func literalWireKey(key []any) string {
	if key == nil {
		return "[]"
	}
	literal, _ := json.Marshal(key) //nolint:errchkjson // a decoded key always marshals
	return string(literal)
}

func readsBack(written string) bool {
	back, err := graph.ParseKey(written)
	return err == nil && graph.FormatKey(back...) == written
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

// revalidate reports WithRevalidation's findings after every structural check,
// so a capped collector keeps each structural issue over a finding no more
// severe than it: each unresolved record no check refused under a required
// association, then what [streamDecoder.revalidateRoot] finds in each root the
// walk accepted whole.
// It runs no root a later check refused a unit of: an association in
// [docIndex.refusedRoots], a record at its address, or a record whose source
// row resolves to none and could stand there. Nor does it run a root at an
// address another root it walked stands at: a record there names the address and not a
// root, so which records are the root's is not known. An edge that names no root
// stops a root only as [streamDecoder.checkOnes] marks it.
func (sd *streamDecoder) revalidate(ctx context.Context, unresolved []unresolvedWire, idx *docIndex) error {
	if !sd.loadCfg.revalidate {
		return nil
	}
	for ui, u := range unresolved {
		// A record whose source row resolves to none is a refused one.
		if row, _ := sd.rowAt(u.SourceType); !idx.refusedRecords[ui] {
			sd.warnUnresolvedRequired(row, u)
		}
	}
	if len(idx.revalidate) == 0 {
		return nil
	}
	refused := maps.Clone(idx.refusedRoots)
	lost := make(map[slotCoord][]int)
	var unrowed []unresolvedWire
	for ui, u := range unresolved {
		row, ok := sd.rowAt(u.SourceType)
		if !ok {
			unrowed = append(unrowed, u)
			continue
		}
		source := slotCoord{parentRow: row, parentKey: sd.canonicalWireKey(row, u.SourceKey)}
		switch {
		case idx.refusedRecords[ui]:
			markSlot(&refused, source)
		case u.Reason == "target_missing":
			lost[source] = append(lost[source], ui)
		}
	}
	names := recordNames{sd: sd, records: unrowed}
	for _, r := range idx.revalidate {
		if err := ctx.Err(); err != nil {
			if !sd.cancelReported {
				sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			}
			return fmt.Errorf("context cancelled: %w", err)
		}
		key := sd.canonicalWireKey(r.row, r.item.Key)
		if address := (slotCoord{parentRow: r.row, parentKey: key}); !refused[address] && !idx.unjudged(r.row, key) &&
			!names.may(address) {
			sd.revalidateRoot(ctx, r.row, *r.item, unresolved, lost[address])
		}
	}
	return nil
}

// revalidateRoot reconstructs one root's raw form — properties, edges as
// _target_-keyed objects, composed children as nested arrays — and runs it
// through the real validator, reporting each finding at the option's
// severity. The reconstruction inverts what the writer did to the
// validator's output, so clean data reports nothing. The caller passes only a
// root the structural checks left whole, and lost, the indexes of the
// target_missing records at its address. Those are edges to the validator,
// which judged them before graph.Add found no target, so each one joins the
// root's edges.
func (sd *streamDecoder) revalidateRoot(ctx context.Context, row int, inst instWire, unresolved []unresolvedWire, lost []int) {
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
	if len(lost) > 0 {
		// A copy: the document's own edges are materialized after this pass.
		edges := make(map[string][]edgeWire, len(inst.Edges)+len(lost))
		maps.Copy(edges, inst.Edges)
		for _, ui := range lost {
			u := unresolved[ui]
			edges[u.Relation] = slices.Concat(edges[u.Relation],
				[]edgeWire{{TargetType: u.TargetType, TargetKey: u.TargetKey, Properties: u.Properties}})
		}
		inst.Edges = edges
	}
	props := sd.rebuildRawProperties(t, inst)
	_, res := sd.revalidator.ValidateOne(ctx, sd.tableTags[row], instance.RawInstance{Properties: props})
	// MergeRetag keeps the validator's order, so a capped collector evicts by
	// when a finding was raised, not by how its message sorts.
	sev, typeText := sd.loadCfg.revalidateSeverity, sd.refText(row)
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
			return retagIssue(issue, sev, typeText, keyStr)
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
// JSON field name. It reads a tree the structural checks left whole,
// whose edges revalidateRoot has joined the root's target_missing records to,
// so every relation is one t declares, at its declared target and arity, and a
// (one) association holds at most one record.
func (sd *streamDecoder) rebuildRawProperties(t *schema.Type, inst instWire) map[string]any {
	// The validator judges the document's own text: a number stays the
	// decoder's json.Number, which the checkers read exactly, so a wide integer
	// is refused as written rather than passed as the float64 that
	// materialization (instanceParts) reads by the wire's numeric contract.
	props := make(map[string]any, len(inst.Properties)+len(inst.Edges)+len(inst.Composed))
	maps.Copy(props, inst.Properties)

	for _, relName := range slices.Sorted(maps.Keys(inst.Edges)) {
		rel, _ := t.Relation(relName)
		targetType, _ := sd.schema.TypeByID(rel.TargetID())
		pks := targetType.PrimaryKeysSlice()
		targets := inst.Edges[relName]
		arr := make([]any, 0, len(targets))
		for _, e := range targets {
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
		rel, _ := t.Relation(relName)
		childType, _ := sd.schema.TypeByID(rel.TargetID())
		children := inst.Composed[relName]
		arr := make([]any, 0, len(children))
		for _, child := range children {
			arr = append(arr, sd.rebuildRawProperties(childType, child))
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
// An unresolvable row matches: requireRow reports it where it stands.
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

// wireTypeText is wireTypeRef for a message, a row rendered as refText renders it.
func (sd *streamDecoder) wireTypeText(row *int) string {
	r, ok := sd.rowAt(row)
	if !ok {
		return "no types-table row"
	}
	return sd.refText(r)
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
// prefixed with the loaded instance it came from, typeText as refText renders
// it. diag.Issue is immutable
// after Build, so a rebuild is the only way to move the severity; details,
// hint, span, path and related locations all travel.
func retagIssue(issue diag.Issue, sev diag.Severity, typeText, keyStr string) diag.Issue {
	b := diag.NewIssue(sev, issue.Code(),
		fmt.Sprintf("revalidation of %s[%s]: %s", typeText, diag.ValueText(keyStr), issue.Message())).
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
		fmt.Sprintf("provenance path %s could not be parsed, falling back to root path", diag.QuoteValue(inst.Provenance.Path))).
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
			fmt.Sprintf("instance %s[%s] holds property %s, which its type does not declare",
				sd.refText(row), diag.ValueText(formatWireKey(key)), diag.QuoteValue(name))).
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
			fmt.Sprintf("edge %q of %s[%s] holds edge property %s, which the association does not declare",
				relation, sd.refText(row), diag.ValueText(formatWireKey(key)), diag.QuoteValue(name))).
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
					name, diag.ValueText(formatWireKey(inst.Key)), prop.Constraint().Kind(), err)).
				WithDetails(diag.TypeProp(sd.refAt(row), name)...).
				Build())
		}
	}
}

// validateDiagnostics validates duplicate and unresolved records against the
// walked index. Every conflict resolves at its stated address: a root
// conflict through the instance index, a composed conflict through the
// parent's slot, where a null stated key needs a sole occupant. A record a
// check refused is held to no fact that reads what the check refused: a
// duplicate whose position holds no conflict, or whose stored key is refused,
// has none resolved, and an unresolved record a check refused feeds no (one)
// count and is no record beside an absent or empty one.
func (sd *streamDecoder) validateDiagnostics(diags diagWire, idx *docIndex) {
	for di, dup := range diags.Duplicates {
		if dup.statesNothing() {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states nothing", di)).Build())
			continue
		}
		row, rowOK := sd.requireRow(dup.Type, func() string { return fmt.Sprintf("duplicate record %d", di) })
		keyStr := formatWireKey(dup.Key)
		// requireRow's own godoc promises that a nil or out-of-range reference
		// "never binds to row 0", and the diagnostics below broke that promise
		// by rendering the sentinel: a record naming no type reported the type
		// in row 0, pointing debugging at something unrelated.
		dupRef, dupText := "(no types-table row)", "(no types-table row)"
		if rowOK {
			dupRef, dupText = sd.refAt(row), sd.refText(row)
		}

		if rowOK && dup.Instance.Type != nil && *dup.Instance.Type != row {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
				fmt.Sprintf("duplicate instance %s[%s] declares type row %d (%s) but its record states row %d (%s)",
					sd.refText(row), diag.ValueText(keyStr), *dup.Instance.Type, sd.refText(*dup.Instance.Type), row, sd.refText(row))).Build())
		}

		// Marshal rewrites the record's key from the instance it carries, so a
		// disagreement is an address the next write would silently discard.
		// Compared canonically where the row resolved, as every other address in
		// this function is: two spellings of one instant at one offset are one
		// address, not a disagreement. An unresolved row canonicalizes under
		// nothing — binding it to row 0 is what requireRow refuses to do — so the
		// normalized forms decide and the record's own type error stands beside this one.
		// The message keeps the document's own spelling either way.
		stated, carried := keyStr, formatWireKey(dup.Instance.Key)
		if rowOK {
			stated, carried = sd.canonicalWireKey(row, dup.Key), sd.canonicalWireKey(row, dup.Instance.Key)
		}

		// keyed reports that the instance's stored key is one a conflict can be
		// compared with; a key checkStoredKey refused has no conflict to resolve.
		eligible, keyed := true, true
		if rowOK {
			sd.checkProvenancePath(dup.Instance, row)
			sd.checkDeclaredProperties(row, dup.Instance.Key, dup.Instance.Properties)
			keyed = sd.checkStoredKey(row, dup.Instance)
			// A ROOT duplicate is a rejected root instance, so its type must be
			// able to hold one. A COMPOSED duplicate is not: a part type is
			// exactly what belongs there, and Relation is what separates them.
			if dup.Relation == "" {
				eligible = sd.checkRootTypeEligible(row, fmt.Sprintf("duplicate record %d", di))
			}
		}
		// A refused stored key is that one break, so it is compared with nothing.
		if keyed && stated != carried {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states key %s but its instance carries %s",
					di, diag.ValueText(literalWireKey(dup.Key)), diag.ValueText(literalWireKey(dup.Instance.Key)))).Build())
		}

		if len(dup.Instance.Composed) > 0 {
			b := diag.NewIssue(diag.Error, diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have composed children", dupText, diag.ValueText(keyStr))).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr)
			if rowOK {
				b = b.WithDetail(diag.DetailKeyTypeName, dupRef)
			}
			sd.collector.Collect(b.Build())
		}
		if len(dup.Instance.Edges) > 0 {
			b := diag.NewIssue(diag.Error, diag.E_SNAPSHOT_EDGES_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have edges", dupText, diag.ValueText(keyStr))).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr)
			if rowOK {
				b = b.WithDetail(diag.DetailKeyTypeName, dupRef)
			}
			sd.collector.Collect(b.Build())
		}

		// A conflict the record does not state is one break, and its position is
		// still judged.
		var conflictRow int
		var conflictKey string
		conflictOK := dup.Conflict != nil
		if conflictOK {
			conflictRow, conflictOK = sd.requireRow(dup.Conflict.Type, func() string {
				return fmt.Sprintf("duplicate record %d conflict", di)
			})
			if conflictOK {
				conflictKey = sd.canonicalWireKey(conflictRow, dup.Conflict.Key)
			}
		} else {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d (%s[%s]) carries no conflict block", di, dupText, diag.ValueText(keyStr))).Build())
		}

		if dup.Relation == "" {
			// Parent coordinates address a slot, and a root duplicate has none.
			if dup.ParentType != nil || len(dup.ParentKey) > 0 {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
					fmt.Sprintf("duplicate record %d states no relation but carries parent coordinates", di)).Build())
			}
			// No root is of a type that cannot hold one, and a refused key is no
			// address, so either record has no conflict to resolve.
			if !eligible || !keyed || !conflictOK {
				continue
			}
			// A conflict a refused unit states is judged no further for its
			// existence alone: its type and key are still the record's own.
			if !idx.rootExists(conflictRow, conflictKey) && !sd.refusedAddress(idx, conflictRow, conflictKey) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
					fmt.Sprintf("duplicate conflict %s[%s] references non-existent instance",
						sd.refText(conflictRow), diag.ValueText(conflictKey))).
					WithDetail(diag.DetailKeyTypeName, sd.refAt(conflictRow)).
					WithDetail(diag.DetailKeyPrimaryKey, conflictKey).
					Build())
				continue
			}
			// graph.Add records a root duplicate against the root at its own
			// type and key, and graph.RebuildSnapshot derives it so. A conflict
			// standing at the key the record itself states adds no break to that key's.
			if rowOK && (conflictRow != row || conflictKey != carried && conflictKey != stated) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
					fmt.Sprintf("duplicate record %d states conflict %s[%s]; a root duplicate's conflict is the root at its own type and key, %s[%s]",
						di, sd.refText(conflictRow), diag.ValueText(conflictKey), dupText, diag.ValueText(carried))).Build())
			}
			continue
		}

		if dup.ParentType == nil {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d carries relation %s with no parent coordinates", di, diag.QuoteValue(dup.Relation))).Build())
			continue
		}
		parentRow, ok := sd.requireRow(dup.ParentType, func() string {
			return fmt.Sprintf("duplicate record %d parent", di)
		})
		if !ok {
			continue
		}
		parentKey := sd.canonicalWireKey(parentRow, dup.ParentKey)
		slot := slotCoord{parentRow: parentRow, parentKey: parentKey, relation: dup.Relation}
		// A parent a refused unit states draws no issue for its existence; the
		// checks below that read the record and its parent's type still run.
		statedParent := false
		if !idx.rootExists(parentRow, parentKey) {
			if !sd.refusedAddress(idx, parentRow, parentKey) {
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
					fmt.Sprintf("duplicate parent %s[%s] does not resolve to a root instance",
						sd.refText(parentRow), diag.ValueText(parentKey))).
					WithDetail(diag.DetailKeyTypeName, sd.refAt(parentRow)).
					WithDetail(diag.DetailKeyPrimaryKey, parentKey).
					Build())
				continue
			}
			statedParent = true
		}
		// A slot the walk refused for its name is the walk's one issue; one it
		// refused for its children still judges the record's own position, but
		// holds no occupants the record's conflict is resolved against.
		slotRefused := idx.slotRefused(slot)
		if slotRefused && !sd.declaresComposition(parentRow, dup.Relation) {
			continue
		}
		rel, ok := sd.checkDuplicateSlot(di, row, rowOK, slot)
		if !ok {
			continue
		}
		// A keyed (many) slot holds its conflict at a key, so a record with a key
		// of its own states one, whatever the slot holds. By length: an empty
		// JSON array decodes non-nil.
		if keyed && conflictOK && rel != nil && rel.IsMany() && len(dup.Conflict.Key) == 0 && len(dup.Instance.Key) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states no conflict key; under the (many) composition %q its conflict is the child at its own key, %s",
					di, slot.relation, diag.ValueText(carried))).Build())
			continue
		}
		// A stated parent, a parent whose key the walk refused, or an address
		// two roots share holds no slot the record's conflict is resolved in.
		if statedParent || slotRefused || !keyed || !conflictOK || !idx.held(parentRow, parentKey) {
			continue
		}
		occupant, found := sd.conflictInSlot(idx, slot, conflictRow, dup.Conflict.Key, conflictKey)
		if !found && sd.childStated(idx, slot, conflictRow, dup.Conflict.Key, conflictKey) {
			continue
		}
		if !found {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("duplicate conflict %s[%s] under %s[%s].%s references non-existent instance",
					sd.refText(conflictRow), diag.ValueText(conflictKey), sd.refText(parentRow), diag.ValueText(parentKey), diag.ValueText(dup.Relation))).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(conflictRow)).
				WithDetail(diag.DetailKeyPrimaryKey, conflictKey).
				Build())
			continue
		}
		// graph.AddComposed records a keyed (many) duplicate against the child at
		// its own key. A (one) slot's occupant is its sole child. A conflict
		// standing at the key the record itself states adds no break to that key's.
		if rowOK && rel != nil && rel.IsMany() && occupant.key != carried && occupant.key != stated {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("duplicate record %d states conflict %s[%s]; under the (many) composition %q its conflict is the child at its own key, %s",
					di, sd.refText(occupant.row), diag.ValueText(occupant.key), slot.relation, diag.ValueText(carried))).Build())
		}
	}

	for ui, u := range diags.Unresolved {
		if u.statesNothing() {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("unresolved record %d states nothing", ui)).Build())
			idx.refuseRecord(ui)
			continue
		}
		slot, rel, refused := sd.checkRecord(ui, u, idx)
		switch {
		case refused:
			idx.refuseRecord(ui)
		case rel != nil && !rel.IsMany() && idx.held(slot.parentRow, slot.parentKey):
			// A record of a root whose key a check refused, or of an address two
			// roots share, counts toward no (one) association.
			idx.pending = append(idx.pending, pendingOne{ui: ui, at: oneCoord{slot: slot}})
		}
	}
}

// checkRecord holds unresolved record ui to every fact the record shows by
// itself and reports whether it refused it, with the association the record is
// under and its place there. It writes to idx only the memo
// [streamDecoder.refusedAddress] keeps.
func (sd *streamDecoder) checkRecord(ui int, u unresolvedWire, idx *docIndex) (slotCoord, *schema.Relation, bool) {
	start := sd.mark()
	documented := sd.checkUnresolvedReason(ui, u)
	sourceRow, ok := sd.requireRow(u.SourceType, func() string {
		return fmt.Sprintf("unresolved record %d source", ui)
	})
	sd.requireRow(u.TargetType, func() string { return fmt.Sprintf("unresolved record %d target", ui) })
	var slot slotCoord
	var rel *schema.Relation
	if ok {
		sd.checkDeclaredEdgeProperties(sourceRow, u.SourceKey, u.Relation, u.Properties)
		sourceKey := sd.canonicalWireKey(sourceRow, u.SourceKey)
		slot = slotCoord{parentRow: sourceRow, parentKey: sourceKey, relation: u.Relation}
		rel = sd.checkAssociation(fmt.Sprintf("unresolved record %d", ui), sourceRow, u.SourceKey,
			u.Relation, u.TargetType, u.TargetKey, u.Reason, documented)
		// A record a check refused names no reference to resolve, as a refused
		// edge names none. A source a refused unit states draws no issue for
		// its existence, and the record's own facts are still judged.
		if !sd.refusedSince(start) && !idx.rootExists(sourceRow, sourceKey) &&
			!sd.refusedAddress(idx, sourceRow, sourceKey) {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("unresolved edge source %s[%s] references non-existent instance",
					sd.refText(sourceRow), diag.ValueText(sourceKey))).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(sourceRow)).
				WithDetail(diag.DetailKeyPrimaryKey, sourceKey).
				Build())
		}
	}
	return slot, rel, sd.refusedSince(start)
}

// checkRecordFacts holds the unresolved records to the facts graph.Add derives
// them by, as graph.RebuildSnapshot does: every root holds an edge or a record
// under each required association of its type; an absent or empty record stands
// alone, once, and only under a required association; and a target_missing
// record names a target no root holds. Every record counts as held where it
// stands, a refused one too, and no refused record is judged again. Each fact
// refuses the records it reports, so no (one) count holds them; the records
// beside them stay in it. A schema-less read makes no claim.
func (sd *streamDecoder) checkRecordFacts(diags diagWire, idx *docIndex) {
	if sd.schema == nil {
		return
	}
	// rel is nil where the record's source row resolves and declares no such
	// association: the record is held there and judged by no fact.
	type record struct {
		slot slotCoord
		rel  *schema.Relation
		ok   bool
	}
	records := make([]record, len(diags.Unresolved))
	for ui, u := range diags.Unresolved {
		row, ok := sd.rowAt(u.SourceType)
		if !ok {
			continue
		}
		records[ui] = record{slot: slotCoord{parentRow: row, parentKey: sd.canonicalWireKey(row, u.SourceKey), relation: u.Relation}, ok: true}
		if rel, ok := sd.associationAt(row, u.Relation); ok {
			records[ui].rel = rel
		}
	}
	// The facts that judge one record run first, so a record they refuse is
	// neither in the (one) count nor another record beside an absent one.
	for ui, u := range diags.Unresolved {
		rec := records[ui]
		if rec.rel == nil || idx.refusedRecords[ui] {
			continue
		}
		switch u.Reason {
		case "absent", "empty":
			if rec.rel.IsOptional() {
				idx.refuseRecord(ui)
				sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
					fmt.Sprintf("unresolved record %d states reason %q under %q of %s[%s], which is optional, where graph.Add records none",
						ui, u.Reason, u.Relation, sd.refText(rec.slot.parentRow), diag.ValueText(rec.slot.parentKey))).Build())
			}
		case "target_missing":
			targetRow, ok := sd.rowAt(u.TargetType)
			if ok && targetRow < len(sd.tableIDs) && sd.tableIDs[targetRow] == rec.rel.TargetID() {
				// A root whose key a check refused is one graph.Add holds not, so a
				// record naming it is no second break.
				if key := sd.canonicalWireKey(targetRow, u.TargetKey); idx.exists[targetRow][key] {
					idx.refuseRecord(ui)
					sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
						fmt.Sprintf("unresolved record %d names target %s[%s], which a root holds, where graph.Add resolves an edge",
							ui, sd.refText(targetRow), diag.ValueText(key))).Build())
				}
			}
		}
	}
	// held and missing count every record where it stands, for the required
	// association fact; beside counts the records no check refused, for the
	// fact that an absent or empty record stands alone.
	held := make(map[slotCoord]int, len(idx.edges)+len(diags.Unresolved))
	for slot, n := range idx.edges {
		held[slot] += n
	}
	beside := make(map[slotCoord]int, len(idx.soundEdges)+len(diags.Unresolved))
	for slot, n := range idx.soundEdges {
		beside[slot] += n
	}
	missing := make(map[slotCoord]int)
	for ui, u := range diags.Unresolved {
		if !records[ui].ok {
			continue
		}
		slot := records[ui].slot
		if u.Reason == "absent" || u.Reason == "empty" {
			missing[slot]++
		} else {
			held[slot]++
		}
		if !idx.refusedRecords[ui] {
			beside[slot]++
		}
	}
	reported := make(map[slotCoord]bool)
	for ui, u := range diags.Unresolved {
		rec := records[ui]
		if rec.rel == nil || idx.refusedRecords[ui] || (u.Reason != "absent" && u.Reason != "empty") {
			continue
		}
		if beside[rec.slot] < 2 || !idx.held(rec.slot.parentRow, rec.slot.parentKey) {
			continue
		}
		// Every absent and empty record of the slot is the one break, reported
		// once.
		idx.refuseRecord(ui)
		if !reported[rec.slot] {
			reported[rec.slot] = true
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("%q of %s[%s] holds an %s record beside another record, where graph.Add records it alone",
					u.Relation, sd.refText(rec.slot.parentRow), diag.ValueText(rec.slot.parentKey), u.Reason)).Build())
		}
	}
	// A record whose source row resolves to none could name any root at its
	// key, so no root there draws the required association's fact for it: the
	// row's refusal is the break's one issue.
	var unrowed []unresolvedWire
	for ui, u := range diags.Unresolved {
		if !records[ui].ok {
			unrowed = append(unrowed, u)
		}
	}
	names := recordNames{sd: sd, records: unrowed}
	for _, row := range slices.Sorted(maps.Keys(idx.exists)) {
		if row >= len(sd.tableIDs) {
			continue
		}
		t, ok := sd.schema.TypeByID(sd.tableIDs[row])
		if !ok {
			continue
		}
		var required []string
		for rel := range t.AllAssociations() {
			if !rel.IsOptional() {
				required = append(required, rel.Name())
			}
		}
		if len(required) == 0 {
			continue
		}
		for _, key := range slices.Sorted(maps.Keys(idx.exists[row])) {
			// A root whose key a check refused is judged by no record fact. An
			// address two roots share holds no record for either when it holds none.
			if !idx.exists[row][key] {
				continue
			}
			for _, relation := range required {
				if slot := (slotCoord{parentRow: row, parentKey: key, relation: relation}); held[slot]+missing[slot] == 0 &&
					!names.may(slot) {
					markSlot(&idx.refusedRoots, slotCoord{parentRow: row, parentKey: key})
					sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
						fmt.Sprintf("root %s[%s] holds no edge and no record under the required association %q, where graph.Add records an absent one",
							sd.refText(row), diag.ValueText(key), relation)).Build())
				}
			}
		}
	}
}

// recordNames answers for records whose source rows resolve to none. Each row and
// relation reads the records once, however many roots ask.
type recordNames struct {
	sd      *streamDecoder
	records []unresolvedWire
	keys    map[slotCoord]map[string]bool
}

// may reports whether one of the records could stand at slot: its source key is
// slot's address in the canonical form of slot's row, under slot's relation, or
// under any where slot leaves it empty.
func (n *recordNames) may(slot slotCoord) bool {
	at := slotCoord{parentRow: slot.parentRow, relation: slot.relation}
	keys, read := n.keys[at]
	if !read {
		keys = make(map[string]bool)
		for _, u := range n.records {
			if slot.relation == "" || u.Relation == slot.relation {
				keys[n.sd.canonicalWireKey(slot.parentRow, u.SourceKey)] = true
			}
		}
		if n.keys == nil {
			n.keys = make(map[slotCoord]map[string]bool)
		}
		n.keys[at] = keys
	}
	return keys[slot.parentKey]
}

// unresolvedReasons is the closed set graph.UnresolvedEdge documents. The
// two reasons naming a reference that never had a target carry no target key
// and no properties, so a record stating one and carrying either describes a
// state the graph model cannot hold.
var unresolvedReasons = map[string]bool{"target_missing": true, "absent": true, "empty": true}

// checkUnresolvedReason holds a record to the reasons graph.UnresolvedEdge
// documents, and a reason naming no target to carry no target key or edge
// properties, as graph.RebuildSnapshot does; Verify and Info then report what
// Load refuses. It reports whether the reason is a documented one.
func (sd *streamDecoder) checkUnresolvedReason(ui int, u unresolvedWire) bool {
	if u.Reason == "" {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states no reason; it states one of target_missing, absent or empty", ui)).Build())
		return false
	}
	if !unresolvedReasons[u.Reason] {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %s, which is not one of target_missing, absent or empty",
				ui, diag.QuoteValue(u.Reason))).Build())
		return false
	}
	if u.Reason == "target_missing" {
		return true
	}
	if len(u.TargetKey) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %q but carries a target key %s",
				ui, u.Reason, diag.ValueText(formatWireKey(u.TargetKey)))).Build())
	}
	if len(u.Properties) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("unresolved record %d states reason %q but carries edge properties",
				ui, u.Reason)).Build())
	}
	return true
}

// warnUnresolvedRequired reports an unresolved record under a required
// association at the revalidation severity. Whether it is required is read
// from the schema, never from the record's own field. Without the option the
// record is well-formed data (Snapshot.Unresolved), so no reader pays for it.
func (sd *streamDecoder) warnUnresolvedRequired(sourceRow int, u unresolvedWire) {
	rel, ok := sd.associationAt(sourceRow, u.Relation)
	if !ok || rel.IsOptional() {
		return
	}
	sd.collector.Collect(diag.NewIssue(sd.loadCfg.revalidateSeverity, diag.W_SNAPSHOT_UNRESOLVED_REQUIRED,
		fmt.Sprintf("required association %q of %s[%s] is unresolved (%s)",
			u.Relation, sd.refText(sourceRow), diag.ValueText(formatWireKey(u.SourceKey)), u.Reason)).
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
// type, at its declared target, with a target key of the target's arity whose
// components graph.ParseKey reads back. The first fact the record breaks is
// the one reported, in graph's order. A record whose reason names no target,
// or is not a documented one, owes no target key: checkUnresolvedReason judges
// what it carries. It returns the association, or nil where the source's type
// declares none or cannot be read.
func (sd *streamDecoder) checkAssociation(what string, sourceRow int, sourceKey []any, relation string,
	targetRow *int, targetKey []any, reason string, documented bool,
) *schema.Relation {
	namesTarget := documented && reason != "absent" && reason != "empty"
	// The document's name is cut until the schema declares the association.
	relationText := diag.QuoteValue(relation)
	scalarKey := func() {
		if i := unreadableWireComponent(targetKey); namesTarget && i >= 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("%s of %s under %s carries target key %s, whose component %d is not a scalar graph.ParseKey reads back",
					what, diag.ValueText(literalWireKey(sourceKey)), relationText, diag.ValueText(literalWireKey(targetKey)), i)).
				WithDetail(diag.DetailKeyRelationName, relation).
				Build())
		}
	}
	if sd.schema == nil || sourceRow < 0 || sourceRow >= len(sd.tableIDs) {
		scalarKey()
		return nil
	}
	t, ok := sd.schema.TypeByID(sd.tableIDs[sourceRow])
	if !ok {
		scalarKey()
		return nil
	}
	source := sd.refAt(sourceRow)
	at := fmt.Sprintf("%s of %s[%s] under ", what, sd.refText(sourceRow), diag.ValueText(formatWireKey(sourceKey)))
	rel, ok := t.Relation(relation)
	if !ok || !rel.IsAssociation() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			at+relationText+", which the type does not declare as an association").
			WithDetail(diag.DetailKeyTypeName, source).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
		return nil
	}
	relationText = strconv.Quote(relation)
	at += relationText
	if !sd.wireTypeMatches(targetRow, rel.TargetID()) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
			fmt.Sprintf("%s targets %s, which the association declares as %s",
				at, sd.wireTypeText(targetRow), sd.identRef(rel.TargetID()))).
			WithDetail(diag.DetailKeyTypeName, source).
			WithDetail(diag.DetailKeyRelationName, relation).
			WithDetail(diag.DetailKeyExpected, sd.identRef(rel.TargetID())).
			WithDetail(diag.DetailKeyGot, sd.wireTypeRef(targetRow)).
			Build())
		return rel
	}
	target, ok := sd.schema.TypeByID(rel.TargetID())
	if !ok || !namesTarget {
		return rel
	}
	if declared := keyArity(target); len(targetKey) != declared {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("%s carries a %d-part target key; the target declares %d", at, len(targetKey), declared)).
			WithDetail(diag.DetailKeyTypeName, source).
			WithDetail(diag.DetailKeyRelationName, relation).
			Build())
		return rel
	}
	scalarKey()
	return rel
}

// checkOnes refuses each (one) association holding more than one record,
// edges and unresolved records together: graph.Add stages at most one. An edge
// that names no root is no count, but beside another record it leaves two the
// validator cannot read as one, so its root is not revalidated.
func (sd *streamDecoder) checkOnes(idx *docIndex) {
	for at, n := range idx.danglingOnes {
		if n+idx.ones[at] > 1 {
			markSlot(&idx.refusedRoots, slotCoord{parentRow: at.slot.parentRow, parentKey: at.slot.parentKey})
		}
	}
	for _, at := range idx.order {
		if n, slot := idx.ones[at], at.slot; n > 1 {
			markSlot(&idx.refusedRoots, slotCoord{parentRow: slot.parentRow, parentKey: slot.parentKey})
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("(one) association %q of %s[%s] holds %d records",
					slot.relation, sd.refText(slot.parentRow), diag.ValueText(slot.parentKey), n)).
				WithDetail(diag.DetailKeyTypeName, sd.refAt(slot.parentRow)).
				WithDetail(diag.DetailKeyRelationName, slot.relation).
				Build())
		}
	}
}

// checkDuplicateSlot holds a composed duplicate record at row to the position
// graph.AddComposed records one at and graph.RebuildSnapshot derives its
// conflict from: slot is a composition its parent's type declares, the
// record's type is that composition's declared target, and the composition is
// not a keyless (many) one, which holds no conflict. It judges the position
// alone, whatever the slot holds. It returns the composition, nil on a
// schema-less read, and false where it refused the record.
func (sd *streamDecoder) checkDuplicateSlot(di, row int, rowOK bool, slot slotCoord) (*schema.Relation, bool) {
	if sd.schema == nil || slot.parentRow >= len(sd.tableIDs) {
		return nil, true
	}
	pt, ok := sd.schema.TypeByID(sd.tableIDs[slot.parentRow])
	if !ok {
		return nil, true
	}
	rel, ok := pt.Relation(slot.relation)
	if !ok || rel.Kind() != schema.RelationComposition {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("duplicate record %d names %s, which %s does not declare as a composition",
				di, diag.QuoteValue(slot.relation), sd.refText(slot.parentRow))).
			WithDetail(diag.DetailKeyTypeName, sd.refAt(slot.parentRow)).
			WithDetail(diag.DetailKeyRelationName, slot.relation).
			Build())
		return nil, false
	}
	if rowOK && row < len(sd.tableIDs) && sd.tableIDs[row] != rel.TargetID() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
			fmt.Sprintf("duplicate record %d is typed %s, which the composition %q declares as %s",
				di, sd.refText(row), slot.relation, sd.identRef(rel.TargetID()))).Build())
		return nil, false
	}
	if target, ok := sd.schema.TypeByID(rel.TargetID()); ok && rel.IsMany() && !target.HasPrimaryKey() {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("duplicate record %d is under the keyless (many) composition %q, where no child conflicts",
				di, slot.relation)).Build())
		return nil, false
	}
	return rel, true
}

// conflictInSlot returns the occupant the stated conflict address resolves to
// in the slot: a non-empty key selects among the occupants, an empty one
// addresses a sole occupant. The caller has refused an empty one that a keyed
// (many) duplicate with a key of its own states, so that arm reads a (one)
// slot, a keyed (many) slot whose duplicate holds no key, or any slot on a
// schema-less read. The arm is chosen by length, not nil-ness, because an
// empty JSON array decodes non-nil.
func (sd *streamDecoder) conflictInSlot(idx *docIndex, slot slotCoord, conflictRow int, rawKey []any, keyStr string) (slotChild, bool) {
	occupants := idx.slots[slot]
	if len(rawKey) > 0 {
		want := slotChild{row: conflictRow, key: keyStr}
		if idx.occupies(slot, want) {
			return want, true
		}
		return slotChild{}, false
	}
	if len(occupants) == 1 && occupants[0].row == conflictRow {
		return occupants[0], true
	}
	return slotChild{}, false
}

// validateEdgeRefs holds each edge's target to a root, and feeds the record
// facts and the (one) counts. A target a refused unit states is that unit's
// break, so the edge stays in both. An edge that names no root is refused: it
// stays held where it stands, and is no record beside another and no count. Of
// a sound root, an edge feeds that root's (one) counts, and only
// the holder's, ordinal 0, counts beside its records.
func (sd *streamDecoder) validateEdgeRefs(idx *docIndex) {
	for _, ref := range idx.refs {
		if idx.rootExists(ref.targetRow, ref.targetKey) || sd.refusedAddress(idx, ref.targetRow, ref.targetKey) {
			if ref.own == 0 {
				if idx.soundEdges == nil {
					idx.soundEdges = make(map[slotCoord]int)
				}
				idx.soundEdges[ref.slot]++
			}
			if ref.own >= 0 && ref.one {
				idx.countOne(oneCoord{slot: ref.slot, root: ref.own})
			}
			continue
		}
		if ref.own >= 0 && ref.one {
			if idx.danglingOnes == nil {
				idx.danglingOnes = make(map[oneCoord]int)
			}
			idx.danglingOnes[oneCoord{slot: ref.slot, root: ref.own}]++
		}
		sourceRef := sd.refAt(ref.slot.parentRow)
		targetRef := sd.refAt(ref.targetRow)
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
			fmt.Sprintf("edge in %s[%s] references %s[%s] which does not exist",
				sd.refText(ref.slot.parentRow), diag.ValueText(ref.slot.parentKey), sd.refText(ref.targetRow), diag.ValueText(ref.targetKey))).
			WithDetail(diag.DetailKeyTypeName, sourceRef).
			WithDetail(diag.DetailKeyPrimaryKey, ref.slot.parentKey).
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
	if _, err := sd.validateBody(ctx, groups, diags); err != nil {
		return nil, diagWire{}, false
	}
	if sd.collector.HasErrors() {
		return nil, diagWire{}, false
	}
	return groups, diags, true
}

// validateBody runs every structural check the format defines over a decoded
// body, the integrity hash last, then the revalidation the options ask for,
// and returns [streamDecoder.verifyIntegrity]'s status. Load, Verify and Info
// share it: the rules are one definition and only the gating policy differs,
// so a surface that summarises a document cannot report it clean while a
// surface that loads one refuses it.
func (sd *streamDecoder) validateBody(ctx context.Context, groups []instanceGroupWire, diags diagWire) (string, error) {
	idx, err := sd.walkInstances(ctx, groups)
	if err != nil {
		return "", err
	}
	sd.validateDiagnostics(diags, idx)
	sd.validateEdgeRefs(idx)
	sd.checkRecordFacts(diags, idx)
	idx.countPending()
	sd.checkOnes(idx)
	integrity := sd.verifyIntegrity()
	return integrity, sd.revalidate(ctx, diags.Unresolved, idx)
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
						TargetKey:  immutable.WrapKey(normalizeSlice(e.TargetKey)),
						Properties: immutable.WrapProperties(normalizeMap(e.Properties)),
					})
				}
			}
		}
	}

	dupParts := make([]graph.DuplicateParts, 0, len(diags.Duplicates))
	for _, dw := range diags.Duplicates {
		dp := graph.DuplicateParts{Instance: sd.instanceParts(*dw.Type, dw.Instance)}
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

// normalizeSlice applies NormalizeValue to a copy of each element in a slice.
// NormalizeValue rewrites a nested list or map in place, and a refusal reads the
// document's numbers after an address has normalized them.
func normalizeSlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = immutable.NormalizeValue(copyWireValue(v))
	}
	return result
}

func copyWireValue(v any) any {
	switch val := v.(type) {
	case []any:
		elems := make([]any, len(val))
		for i, elem := range val {
			elems[i] = copyWireValue(elem)
		}
		return elems
	case map[string]any:
		members := make(map[string]any, len(val))
		for k, elem := range val {
			members[k] = copyWireValue(elem)
		}
		return members
	default:
		return v
	}
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

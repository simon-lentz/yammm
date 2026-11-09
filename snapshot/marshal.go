package snapshot

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// Marshal serializes a Snapshot to the yammm snapshot persistence format
// (.ys). Output is deterministic by default — created_at is omitted unless
// [WithCreatedAt] or [WithCreatedAtFrom] opts in — so one snapshot and one
// option set always produce identical bytes. Marshal checks ctx.Err() at the
// start and once per type group during emission, returning Fatal
// E_CONTEXT_CANCELLED on cancellation.
//
// Every snapshot came from a validator or a document, so the wire has a form for
// every value it holds, and no tree in it is nested deeper than the reader
// accepts. Fatal E_INTERNAL marks only a broken invariant of the snapshot
// itself. Panics if snap is zero or ctx is nil.
//
// Three further refusals, each Error [diag.E_SNAPSHOT_MALFORMED]: an indent that is
// not JSON whitespace, which would produce bytes that are not JSON, and a metadata
// key or value, or an instance provenance's source name or raw path, that is not
// valid UTF-8, which encoding/json would write as U+FFFD.
func Marshal(ctx context.Context, snap graph.Snapshot, opts ...Option) ([]byte, diag.Result) {
	if snap.IsZero() {
		panic("snapshot.Marshal: zero Snapshot")
	}
	if ctx == nil {
		panic("snapshot.Marshal: nil context")
	}
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	cfg := applyOptions(opts)
	if bad, ok := nonWhitespaceIndent(cfg.indent); ok {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("indent must be JSON whitespace; it is written between JSON tokens and %q produces a document that is not JSON", bad)).
			Build())
		return nil, c.Result()
	}
	if err := invalidMetadata(cfg.metadata); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, "snapshot.Marshal: "+err.Error()).Build())
		return nil, c.Result()
	}
	s := snap.Schema()

	// Step 1: Compute schema structural hash.
	schemaHash := schema.StructuralHash(s)
	schemaSource := s.SourceID().String()

	// Step 2: Read the snapshot once, then serialize the payload sections —
	// identity lives in the types table, every other position is a row index.
	names := newSchemaNames(s)
	view := newWriterView(snap, names)
	tt := buildTypeTable(view, names)

	groups, err := marshalInstances(ctx, view, s, tt)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			c := diag.NewCollector(0)
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, ctxErr.Error()).Build())
			return nil, c.Result()
		}
		return nil, marshalFatalErr("instances", err)
	}

	diagsWire, err := marshalDiagnostics(view, s, tt)
	if err != nil {
		return nil, marshalFatalErr("diagnostics", err)
	}

	typesJSON, marshalErr := marshalSection(tt.entries, cfg.indent)
	if marshalErr != nil {
		return nil, marshalFatalErr("types", marshalErr)
	}
	instancesJSON, marshalErr := marshalSection(groups, cfg.indent)
	if marshalErr != nil {
		return nil, marshalFatalErr("instances", marshalErr)
	}
	diagJSON, marshalErr := marshalSection(diagsWire, cfg.indent)
	if marshalErr != nil {
		return nil, marshalFatalErr("diagnostics", marshalErr)
	}

	// Build header and assemble document.
	createdAt := cfg.createdAtText
	if createdAt == "" && !cfg.createdAt.IsZero() {
		createdAt = cfg.createdAt.UTC().Format(time.RFC3339)
	}

	features := []string{}
	var metadata map[string]string
	if len(cfg.metadata) > 0 {
		metadata = cfg.metadata
	}

	// A snapshot carrying no claim writes no attestation block: the wire's
	// pointer-with-omitempty already states absence, and substituting a
	// both-false claim would be the writer inventing content.
	var attWire *attestationWire
	if att := snap.Attestation(); att != nil {
		attWire = &attestationWire{Values: att.Values, Associations: att.Associations}
	}

	return assembleDocument(
		schemaHash, s.Name(), schemaSource, createdAt, features, metadata,
		attWire,
		typesJSON, instancesJSON, diagJSON,
		cfg.indent, currentVersion,
	)
}

// marshalSection serializes one body section, compact or indented.
func marshalSection(v any, indent string) ([]byte, error) {
	var data []byte
	var err error
	if indent != "" {
		data, err = json.MarshalIndent(v, indent, indent)
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return data, nil
}

// writerGroup is one types-table row's root instances, filed by each
// instance's own identity.
type writerGroup struct {
	id        schema.TypeID
	instances []graph.Instance
}

// writerView holds the groups and diagnostic records the table pass and the emit
// pass both read, in one order. Each pass reads an instance's edges from the
// snapshot itself.
type writerView struct {
	snap       graph.Snapshot
	groups     []writerGroup
	duplicates []graph.Duplicate
	unresolved []graph.UnresolvedEdge
}

// newWriterView reads the snapshot once. Instances are grouped by their own
// TypeID — the filing key is a container detail, and an instance whose group
// differs from its identity would otherwise be silently rebound on load —
// and a type the snapshot holds with no instances keeps an empty group.
func newWriterView(snap graph.Snapshot, names schemaNames) *writerView {
	// Types returns a fresh slice per call, so it is taken once.
	snapTypes := snap.Types()

	byID := make(map[schema.TypeID][]graph.Instance)
	order := make([]schema.TypeID, 0, len(snapTypes))

	addGroup := func(id schema.TypeID) {
		if _, ok := byID[id]; !ok {
			byID[id] = nil
			order = append(order, id)
		}
	}

	for _, typeID := range snapTypes {
		addGroup(typeID)
		for inst := range snap.InstancesOf(typeID) {
			id := inst.TypeID()
			addGroup(id)
			byID[id] = append(byID[id], inst)
		}
	}

	groups := make([]writerGroup, 0, len(order))
	for _, id := range order {
		insts := byID[id]
		slices.SortFunc(insts, func(a, b graph.Instance) int {
			return cmp.Compare(a.PrimaryKey().String(), b.PrimaryKey().String())
		})
		groups = append(groups, writerGroup{id: id, instances: insts})
	}
	// Ordered by the rendered identity, the same key buildTypeTable sorts by:
	// the instances section follows this order, so a group order derived from
	// the source path would move wire bytes when one schema text is loaded
	// from two directories.
	slices.SortFunc(groups, func(a, b writerGroup) int {
		ea, eb := names.entry(a.id), names.entry(b.id)
		if c := cmp.Compare(ea.Schema, eb.Schema); c != 0 {
			return c
		}
		return cmp.Compare(ea.Name, eb.Name)
	})

	return &writerView{
		groups:     groups,
		snap:       snap,
		duplicates: slices.Collect(snap.Duplicates()),
		unresolved: slices.Collect(snap.Unresolved()),
	}
}

// marshalInstances builds the instances section: one entry per group, in
// table-row order. A type denoted only as a reference target has no group
// and no entry.
func marshalInstances(ctx context.Context, view *writerView, s schema.Schema, tt *typeTable) ([]instanceGroupWire, error) {
	groups := make([]instanceGroupWire, 0, len(view.groups))
	for _, group := range view.groups {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("marshalling instances: %w", err)
		}
		row, err := tt.rowRef(group.id, "instance type")
		if err != nil {
			return nil, err
		}
		items := make([]instWire, 0, len(group.instances))
		for _, inst := range group.instances {
			w, err := marshalInstance(inst, view.snap, s, tt, false)
			if err != nil {
				return nil, err
			}
			items = append(items, w)
		}
		groups = append(groups, instanceGroupWire{Type: row, Items: items})
	}
	return groups, nil
}

// marshalInstance converts an instance to its wire form. carryType is false
// only where the position already denotes the type: a root instance (its
// group entry) and a duplicate's rejected instance (its record).
func marshalInstance(
	inst graph.Instance,
	snap graph.Snapshot,
	s schema.Schema,
	tt *typeTable,
	carryType bool,
) (instWire, error) {
	id := inst.TypeID()
	t, _ := s.TypeByID(id)
	w := instWire{
		Key:        inst.PrimaryKey().Clone(),
		Properties: wireProps(inst.Properties(), t),
	}

	if carryType {
		row, err := tt.rowRef(id, "instance type")
		if err != nil {
			return instWire{}, err
		}
		w.Type = row
	}

	var edgeMap map[string][]edgeWire
	for e := range snap.EdgesFrom(inst) {
		if edgeMap == nil {
			edgeMap = make(map[string][]edgeWire)
		}
		rel, _ := t.Relation(e.Relation())
		row, err := tt.rowRef(e.Target().TypeID(), "edge target")
		if err != nil {
			return instWire{}, err
		}
		ew := edgeWire{
			TargetType: row,
			TargetKey:  e.Target().PrimaryKey().Clone(),
			Properties: wireEdgeProps(e.Properties(), rel),
		}
		if ew.Properties == nil {
			ew.Properties = map[string]any{}
		}
		edgeMap[e.Relation()] = append(edgeMap[e.Relation()], ew)
	}
	w.Edges = edgeMap

	compRels := inst.ComposedRelations()
	if len(compRels) > 0 {
		compMap := make(map[string][]instWire, len(compRels))
		for _, relName := range compRels {
			childWires := make([]instWire, 0, inst.ComposedCount(relName))
			for child := range inst.Composed(relName) {
				cw, err := marshalInstance(child, snap, s, tt, true)
				if err != nil {
					return instWire{}, err
				}
				childWires = append(childWires, cw)
			}
			compMap[relName] = childWires
		}
		w.Composed = compMap
	}

	prov, err := marshalProvenance(inst)
	if err != nil {
		return instWire{}, err
	}
	w.Provenance = prov
	return w, nil
}

// marshalDiagnostics builds the diagnostics section. Every duplicate record
// states its conflict's address. Every constructor of a snapshot gives a
// duplicate a conflict and an unresolved record a source.
func marshalDiagnostics(view *writerView, s schema.Schema, tt *typeTable) (diagWire, error) {
	dupWires := make([]dupWire, 0, len(view.duplicates))
	for _, d := range view.duplicates {
		row, err := tt.rowRef(d.Instance().TypeID(), "duplicate type")
		if err != nil {
			return diagWire{}, err
		}
		inst, err := marshalInstance(d.Instance(), view.snap, s, tt, false)
		if err != nil {
			return diagWire{}, err
		}
		conflictRow, err := tt.rowRef(d.Conflict().TypeID(), "duplicate conflict type")
		if err != nil {
			return diagWire{}, err
		}
		dupWires = append(dupWires, dupWire{
			Type:     row,
			Key:      d.Instance().PrimaryKey().Clone(),
			Instance: inst,
			Conflict: &conflictWire{Type: conflictRow, Key: d.Conflict().PrimaryKey().Clone()},
		})
	}

	unresWires := make([]unresolvedWire, 0, len(view.unresolved))
	for _, u := range view.unresolved {
		sourceID := u.Source().TypeID()
		sourceRow, err := tt.rowRef(sourceID, "unresolved source type")
		if err != nil {
			return diagWire{}, err
		}
		targetRow, err := tt.rowRef(u.TargetType(), "unresolved target type")
		if err != nil {
			return diagWire{}, err
		}
		uw := unresolvedWire{
			SourceType: sourceRow,
			SourceKey:  u.Source().PrimaryKey().Clone(),
			Relation:   u.Relation(),
			TargetType: targetRow,
			Required:   u.Required(),
			Reason:     u.Reason(),
		}
		switch u.Reason() {
		case "absent", "empty":
			uw.TargetKey = nil
		default:
			key, err := parseTargetKey(u.TargetKey())
			if err != nil {
				return diagWire{}, fmt.Errorf("unresolved record %s[%s].%s: %w",
					tt.ref(sourceID), diag.CutValue(u.Source().PrimaryKey().String()), u.Relation(), err)
			}
			uw.TargetKey = key
			var rel schema.Relation
			if srcType, ok := s.TypeByID(sourceID); ok {
				rel, _ = srcType.Relation(u.Relation())
			}
			uw.Properties = wireEdgeProps(u.Properties(), rel)
		}
		unresWires = append(unresWires, uw)
	}

	return diagWire{Duplicates: dupWires, Unresolved: unresWires}, nil
}

// errInvalidProvenance marks a provenance text that is not valid UTF-8, which
// [Marshal] refuses as it refuses such metadata.
var errInvalidProvenance = errors.New("provenance text is not valid UTF-8")

// marshalProvenance converts an instance's provenance to its wire form,
// preferring the raw path a load kept, for a path that failed to parse or
// parsed to another spelling.
func marshalProvenance(inst graph.Instance) (*provenanceWire, error) {
	prov := inst.Provenance()
	if prov.IsZero() {
		return nil, nil //nolint:nilnil // an instance with no provenance writes none, which is no failure
	}
	// A recorded raw path is written back verbatim, INCLUDING an empty one.
	// Reading RawPath() alone could not tell a recorded "" from no record, so
	// a document that carried an empty path came back carrying "$" — and the
	// W_SNAPSHOT_PATH_FALLBACK warning that named the substitution was gone by
	// the second read.
	pathStr := prov.Path().String()
	if prov.HasRawPath() {
		pathStr = prov.RawPath()
	}
	if !utf8.ValidString(prov.SourceName()) || !utf8.ValidString(pathStr) {
		return nil, fmt.Errorf("%w: source name %s, path %s of %s[%s]", errInvalidProvenance,
			diag.QuoteValue(prov.SourceName()), diag.QuoteValue(pathStr), inst.TypeName(), diag.CutValue(inst.PrimaryKey().String()))
	}
	return &provenanceWire{SourceName: prov.SourceName(), Path: pathStr}, nil
}

// assembleDocument builds the full .ys document with integrity hash.
func assembleDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	attestation *attestationWire,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string, version int,
) ([]byte, diag.Result) {
	// Step 3-5: Build canonical-form document (integrity_hash = "").
	canonBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		attestation,
		"", // integrity_hash placeholder
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	// Step 6: Compute integrity hash.
	integrityHash := sha256Sum(canonBytes)

	// Step 7: Re-assemble with computed hash.
	finalBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		attestation,
		integrityHash,
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	return finalBytes, diag.OK()
}

// nonWhitespaceIndent reports the first rune in an indent that is not JSON
// whitespace, if any: space, tab, line feed and carriage return are the four
// RFC 8259 allows between tokens. The indent is emitted verbatim between JSON
// tokens, so anything else, a vertical tab or a no-break space among them,
// makes the output unparseable.
func nonWhitespaceIndent(indent string) (string, bool) {
	for _, r := range indent {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			return string(r), true
		}
	}
	return "", false
}

// buildDocument assembles a complete .ys document from pre-serialized sections.
func buildDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	attestation *attestationWire,
	integrityHash string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string, version int,
) []byte {
	hdr := marshalHeaderWire{
		SchemaName:    schemaName,
		SchemaSource:  schemaSource,
		SchemaHash:    schemaHash,
		IntegrityHash: integrityHash,
		Features:      features,
		CreatedAt:     createdAt,
		Metadata:      metadata,
		Attestation:   attestation,
	}

	// Build the header JSON with version and schema_hash_algorithm as
	// parameters, matching the headerWire field order exactly.
	headerJSON := buildHeaderBytes(hdr, indent, version, currentHashAlgoVersion)

	if indent != "" {
		return assembleIndented(headerJSON, typesJSON, instancesJSON, diagJSON, indent)
	}
	return assembleCompact(headerJSON, typesJSON, instancesJSON, diagJSON)
}

// buildHeaderBytes returns the JSON-encoded header object, compact or
// indented per the indent parameter. Version and hashAlgo are parameters
// because UpdateMetadata reuses its input's body verbatim, so writing the
// current constants there would relabel an older body.
func buildHeaderBytes(hdr marshalHeaderWire, indent string, version, hashAlgo int) []byte {
	if indent != "" {
		return buildHeaderIndented(hdr, indent, version, hashAlgo)
	}
	return buildHeaderCompact(hdr, version, hashAlgo)
}

// buildHeaderCompact produces the header object JSON in compact mode.
// Fields are written in headerWire declaration order.
func buildHeaderCompact(hdr marshalHeaderWire, version, hashAlgo int) []byte {
	var b strings.Builder
	b.WriteByte('{')

	// version
	b.WriteString(`"version":`)
	fmt.Fprintf(&b, "%d", version)

	// schema_name
	b.WriteString(`,"schema_name":`)
	writeJSONString(&b, hdr.SchemaName)

	// schema_source
	b.WriteString(`,"schema_source":`)
	writeJSONString(&b, hdr.SchemaSource)

	// schema_hash
	b.WriteString(`,"schema_hash":`)
	writeJSONString(&b, hdr.SchemaHash)

	// schema_hash_algorithm
	b.WriteString(`,"schema_hash_algorithm":`)
	fmt.Fprintf(&b, "%d", hashAlgo)

	// integrity_hash
	b.WriteString(`,"integrity_hash":`)
	writeJSONString(&b, hdr.IntegrityHash)

	// features
	b.WriteString(`,"features":`)
	featJSON, _ := json.Marshal(hdr.Features)
	b.Write(featJSON)

	// created_at (omitempty)
	if hdr.CreatedAt != "" {
		b.WriteString(`,"created_at":`)
		writeJSONString(&b, hdr.CreatedAt)
	}

	// metadata (omitempty)
	if len(hdr.Metadata) > 0 {
		b.WriteString(`,"metadata":`)
		metaJSON, _ := json.Marshal(hdr.Metadata)
		b.Write(metaJSON)
	}

	// attestation (omitempty on read; Marshal writes it when the snapshot
	// holds a claim, and UpdateMetadata preserves an input's absence rather
	// than inventing a claim)
	if hdr.Attestation != nil {
		b.WriteString(`,"attestation":`)
		attJSON, _ := json.Marshal(hdr.Attestation)
		b.Write(attJSON)
	}

	b.WriteByte('}')
	return []byte(b.String())
}

// buildHeaderIndented produces the header object JSON with indentation.
func buildHeaderIndented(hdr marshalHeaderWire, indent string, version, hashAlgo int) []byte {
	var b strings.Builder
	pfx := indent + indent // header fields are nested inside yammm_snapshot
	b.WriteString("{\n")

	// version
	b.WriteString(pfx)
	b.WriteString(`"version": `)
	fmt.Fprintf(&b, "%d", version)

	// schema_name
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_name": `)
	writeJSONString(&b, hdr.SchemaName)

	// schema_source
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_source": `)
	writeJSONString(&b, hdr.SchemaSource)

	// schema_hash
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_hash": `)
	writeJSONString(&b, hdr.SchemaHash)

	// schema_hash_algorithm
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"schema_hash_algorithm": `)
	fmt.Fprintf(&b, "%d", hashAlgo)

	// integrity_hash
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"integrity_hash": `)
	writeJSONString(&b, hdr.IntegrityHash)

	// features
	b.WriteString(",\n")
	b.WriteString(pfx)
	b.WriteString(`"features": `)
	featJSON, _ := json.Marshal(hdr.Features)
	b.Write(featJSON)

	// created_at (omitempty)
	if hdr.CreatedAt != "" {
		b.WriteString(",\n")
		b.WriteString(pfx)
		b.WriteString(`"created_at": `)
		writeJSONString(&b, hdr.CreatedAt)
	}

	// metadata (omitempty)
	if len(hdr.Metadata) > 0 {
		b.WriteString(",\n")
		b.WriteString(pfx)
		b.WriteString(`"metadata": `)
		metaJSON, _ := json.MarshalIndent(hdr.Metadata, pfx, indent)
		b.Write(metaJSON)
	}

	// attestation (omitempty; see the compact builder for the rule)
	if hdr.Attestation != nil {
		b.WriteString(",\n")
		b.WriteString(pfx)
		b.WriteString(`"attestation": `)
		attJSON, _ := json.MarshalIndent(hdr.Attestation, pfx, indent)
		b.Write(attJSON)
	}

	b.WriteString("\n")
	b.WriteString(indent)
	b.WriteByte('}')
	return []byte(b.String())
}

// assembleCompact builds the full document in compact mode.
func assembleCompact(headerJSON, typesJSON, instancesJSON, diagJSON []byte) []byte {
	var b []byte
	b = append(b, `{"yammm_snapshot":`...)
	b = append(b, headerJSON...)
	b = append(b, `,"types":`...)
	b = append(b, typesJSON...)
	b = append(b, `,"instances":`...)
	b = append(b, instancesJSON...)
	b = append(b, `,"diagnostics":`...)
	b = append(b, diagJSON...)
	b = append(b, '}')
	return b
}

// assembleIndented builds the full document with indentation.
func assembleIndented(headerJSON, typesJSON, instancesJSON, diagJSON []byte, indent string) []byte {
	var b []byte
	b = append(b, "{\n"...)
	b = append(b, indent...)
	b = append(b, `"yammm_snapshot": `...)
	b = append(b, headerJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"types": `...)
	b = append(b, typesJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"instances": `...)
	b = append(b, instancesJSON...)
	b = append(b, ",\n"...)
	b = append(b, indent...)
	b = append(b, `"diagnostics": `...)
	b = append(b, diagJSON...)
	b = append(b, '\n')
	b = append(b, '}')
	return b
}

// parseTargetKey parses an UnresolvedEdge.TargetKey string into []any.
// TargetKey is in Key.String() canonical form: e.g., `["c99"]` or `[1]`.
//
// The keyless forms "" and "[]" yield nil. Every other string the graph writes
// came from immutable.Key.String, whose inverse is [graph.ParseKey], so a parse
// failure is the writer's own invariant broken and is returned rather than
// turned into an empty key. [graph.ParseKey] owns the parsing, so the module
// holds one decoder rather than two that can drift.
func parseTargetKey(keyStr string) ([]any, error) {
	if keyStr == "" || keyStr == "[]" {
		return nil, nil
	}
	values, err := graph.ParseKey(keyStr)
	if err != nil {
		return nil, fmt.Errorf("target key %s: graph.ParseKey: %w", diag.CutValue(keyStr), err)
	}
	return values, nil
}

// writeJSONString writes a JSON-encoded string to a strings.Builder.
func writeJSONString(b *strings.Builder, s string) {
	encoded, _ := json.Marshal(s)
	b.Write(encoded)
}

// marshalFatalErr reports a marshal failure. A text [errInvalidProvenance] names
// is the caller's to correct; any other failure is a broken invariant of the
// snapshot: every value a snapshot holds is one the wire carries.
func marshalFatalErr(section string, err error) diag.Result {
	c := diag.NewCollector(0)
	if errors.Is(err, errInvalidProvenance) {
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, "snapshot.Marshal: "+err.Error()).Build())
		return c.Result()
	}
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
		fmt.Sprintf("snapshot.Marshal: failed to serialize %s: %v", section, err)).Build())
	return c.Result()
}

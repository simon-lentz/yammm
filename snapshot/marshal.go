package snapshot

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// Marshal serializes a Snapshot to the yammm snapshot persistence format
// (.ys). Output is deterministic by default — created_at is omitted unless
// [WithCreatedAt] opts in — so one snapshot and one option set always
// produce identical bytes. Marshal checks ctx.Err() at the start and once
// per type group during emission, returning Fatal E_CONTEXT_CANCELLED on
// cancellation; a caller-assembled state the writer cannot serialize —
// a type outside its table, an unresolved record with no source instance,
// or a target key [graph.ParseKey] cannot read — returns nil bytes with
// Fatal E_INTERNAL, and a snapshot built through [graph.Graph] or [Load]
// never triggers that path. A snapshot nested deeper
// than the reader accepts returns nil bytes with Error
// E_SNAPSHOT_DEPTH_EXCEEDED — the same code and severity the reader raises,
// so the bound reads identically from both sides. Panics if snap is nil.
//
// Two further refusals, both Error-severity [diag.E_SNAPSHOT_MALFORMED]: an
// indent that is not whitespace, which would produce bytes that are not JSON;
// and a type whose schema is outside the entry schema's import closure, which
// the document has no portable name for.
//
// A SUCCESSFUL Marshal can return Warning-severity issues. They report values
// the snapshot held that the wire cannot carry — an unresolved record's target
// key or edge properties under a reason that admits neither. A caller checking
// only [diag.Result.Err] sees none of them.
func Marshal(ctx context.Context, snap *graph.Snapshot, opts ...Option) ([]byte, diag.Result) {
	if snap == nil {
		panic("snapshot.Marshal: nil Snapshot")
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
			fmt.Sprintf("indent must be whitespace; it is written between JSON tokens and %q produces a document that is not JSON", bad)).
			Build())
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
	// Refused before a byte is written: a type the closure cannot name has no
	// portable rendering, and writing its source path instead would put a
	// machine-local path into the one field the re-key exists to keep free of
	// them — in a document that would otherwise look well-formed.
	if id, ok := names.requireDenotable(snap.Types()); !ok {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
			fmt.Sprintf("cannot write type %s: its schema is not in the entry schema's import closure, so the document has no portable name for it", id)).
			WithDetail(diag.DetailKeyTypeName, id.String()).
			Build())
		return nil, c.Result()
	}
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

	writerWarnings := diag.NewCollector(0)
	diagsWire, err := marshalDiagnostics(view, s, tt, writerWarnings)
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
	createdAt := ""
	if !cfg.createdAt.IsZero() {
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

	out, res := assembleDocument(
		schemaHash, s.Name(), schemaSource, createdAt, features, metadata,
		attWire,
		typesJSON, instancesJSON, diagJSON,
		cfg.indent, currentVersion,
	)
	// The writer's own warnings travel with the bytes: a value it could not
	// put on the wire is a fact about this document, and silence left the next
	// reader to find an absence nothing explained.
	if res.HasErrors() {
		return out, res
	}
	writerWarnings.Merge(res)
	return out, writerWarnings.Result()
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
	instances []*graph.Instance
}

// writerView is one read of the snapshot the writer emits from, so the table
// pass and the emit pass see the same data without re-walking the snapshot's
// defensive-copy accessors.
type writerView struct {
	groups     []writerGroup
	edges      map[*graph.Instance][]*graph.Edge
	duplicates []*graph.Duplicate
	unresolved []*graph.UnresolvedEdge
}

// newWriterView reads the snapshot once. Instances are grouped by their own
// TypeID — the filing key is a container detail, and an instance whose group
// differs from its identity would otherwise be silently rebound on load —
// and a type the snapshot holds with no instances keeps an empty group.
func newWriterView(snap *graph.Snapshot, names schemaNames) *writerView {
	// Types returns a fresh defensive copy per call, so it is taken once.
	snapTypes := snap.Types()

	byID := make(map[schema.TypeID][]*graph.Instance)
	order := make([]schema.TypeID, 0, len(snapTypes))
	edges := make(map[*graph.Instance][]*graph.Edge)

	addGroup := func(id schema.TypeID) {
		if _, ok := byID[id]; !ok {
			byID[id] = nil
			order = append(order, id)
		}
	}

	for _, typeID := range snapTypes {
		addGroup(typeID)
		for _, inst := range snap.InstancesOf(typeID) {
			id := inst.TypeID()
			addGroup(id)
			byID[id] = append(byID[id], inst)
			if es := snap.EdgesFrom(inst); len(es) > 0 {
				edges[inst] = es
			}
		}
	}

	groups := make([]writerGroup, 0, len(order))
	for _, id := range order {
		insts := byID[id]
		slices.SortFunc(insts, func(a, b *graph.Instance) int {
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
		edges:      edges,
		duplicates: snap.Duplicates(),
		unresolved: snap.Unresolved(),
	}
}

// marshalInstances builds the instances section: one entry per group, in
// table-row order. A type denoted only as a reference target has no group
// and no entry.
func marshalInstances(ctx context.Context, view *writerView, s *schema.Schema, tt *typeTable) ([]instanceGroupWire, error) {
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
			w, err := marshalInstance(inst, view.edges[inst], s, tt, false, 0)
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
	inst *graph.Instance,
	edges []*graph.Edge,
	s *schema.Schema,
	tt *typeTable,
	carryType bool,
	depth int,
) (instWire, error) {
	// The same bound the reader enforces, so Marshal never writes a document
	// Load and Verify refuse whole.
	if depth > maxComposedDepth {
		return instWire{}, &depthExceededError{
			depth: depth,
			ref:   tt.ref(inst.TypeID()),
			key:   inst.PrimaryKey().String(),
		}
	}
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

	if len(edges) > 0 {
		edgeMap := make(map[string][]edgeWire)
		for _, e := range edges {
			var rel *schema.Relation
			if t != nil {
				rel, _ = t.Relation(e.Relation())
			}
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
	}

	compRels := inst.ComposedRelations()
	if len(compRels) > 0 {
		compMap := make(map[string][]instWire, len(compRels))
		for _, relName := range compRels {
			children := inst.Composed(relName)
			childWires := make([]instWire, 0, len(children))
			for _, child := range children {
				cw, err := marshalInstance(child, nil, s, tt, true, depth+1)
				if err != nil {
					return instWire{}, err
				}
				childWires = append(childWires, cw)
			}
			compMap[relName] = childWires
		}
		w.Composed = compMap
	}

	w.Provenance = marshalProvenance(inst)
	return w, nil
}

// marshalDiagnostics builds the diagnostics section. Every duplicate record
// states its conflict's address; the graph API guarantees the invariants
// checked here, so a violation is a caller-assembled inconsistency.
func marshalDiagnostics(view *writerView, s *schema.Schema, tt *typeTable, diags *diag.Collector) (diagWire, error) {
	dupWires := make([]dupWire, 0, len(view.duplicates))
	for _, d := range view.duplicates {
		row, err := tt.rowRef(d.Instance.TypeID(), "duplicate type")
		if err != nil {
			return diagWire{}, err
		}
		inst, err := marshalInstance(d.Instance, nil, s, tt, false, 0)
		if err != nil {
			return diagWire{}, err
		}
		if d.Conflict == nil {
			return diagWire{}, fmt.Errorf("duplicate %s[%s] carries no conflict instance",
				d.Instance.TypeID(), d.Instance.PrimaryKey())
		}
		conflictRow, err := tt.rowRef(d.Conflict.TypeID(), "duplicate conflict type")
		if err != nil {
			return diagWire{}, err
		}
		conflict := &conflictWire{Type: conflictRow}
		if key := d.Conflict.PrimaryKey(); key.Len() > 0 {
			conflict.Key = key.Clone()
		}
		dw := dupWire{
			Type:     row,
			Key:      d.Instance.PrimaryKey().Clone(),
			Instance: inst,
			Conflict: conflict,
		}
		if d.Relation != "" {
			if d.Parent == nil {
				return diagWire{}, fmt.Errorf("duplicate %s[%s] carries relation %q with no parent",
					d.Instance.TypeID(), d.Instance.PrimaryKey(), d.Relation)
			}
			parentRow, err := tt.rowRef(d.Parent.TypeID(), "duplicate parent type")
			if err != nil {
				return diagWire{}, err
			}
			dw.ParentType = parentRow
			dw.ParentKey = d.Parent.PrimaryKey().Clone()
			dw.Relation = d.Relation
		}
		dupWires = append(dupWires, dw)
	}

	unresWires := make([]unresolvedWire, 0, len(view.unresolved))
	for _, u := range view.unresolved {
		if u.Source == nil {
			return diagWire{}, fmt.Errorf("unresolved edge record for relation %q carries no source instance", u.Relation)
		}
		sourceID := u.Source.TypeID()
		sourceRow, err := tt.rowRef(sourceID, "unresolved source type")
		if err != nil {
			return diagWire{}, err
		}
		targetRow, err := tt.rowRef(u.TargetType, "unresolved target type")
		if err != nil {
			return diagWire{}, err
		}
		uw := unresolvedWire{
			SourceType: sourceRow,
			SourceKey:  u.Source.PrimaryKey().Clone(),
			Relation:   u.Relation,
			TargetType: targetRow,
			Required:   u.Required,
			Reason:     u.Reason,
		}
		switch u.Reason {
		case "absent", "empty":
			// The reader refuses a document carrying either under these
			// reasons, so dropping them silently wrote a document that says
			// less than the record did. Report what is discarded.
			if u.TargetKey != "" && u.TargetKey != "[]" {
				diags.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_VALUE_DROPPED,
					fmt.Sprintf("unresolved record %s[%s].%s states reason %q and carries target key %s, which the wire cannot hold under that reason",
						tt.ref(sourceID), u.Source.PrimaryKey(), u.Relation, u.Reason, u.TargetKey)).
					WithDetail(diag.DetailKeyRelationName, u.Relation).
					Build())
			}
			if len(u.Properties().Clone()) > 0 {
				diags.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_VALUE_DROPPED,
					fmt.Sprintf("unresolved record %s[%s].%s states reason %q and carries edge properties, which the wire cannot hold under that reason",
						tt.ref(sourceID), u.Source.PrimaryKey(), u.Relation, u.Reason)).
					WithDetail(diag.DetailKeyRelationName, u.Relation).
					Build())
			}
			uw.TargetKey = nil
		default:
			key, err := parseTargetKey(u.TargetKey)
			if err != nil {
				return diagWire{}, fmt.Errorf("unresolved record %s[%s].%s: %w",
					tt.ref(sourceID), u.Source.PrimaryKey(), u.Relation, err)
			}
			uw.TargetKey = key
			var rel *schema.Relation
			if srcType, ok := s.TypeByID(sourceID); ok {
				rel, _ = srcType.Relation(u.Relation)
			}
			uw.Properties = wireEdgeProps(u.Properties(), rel)
		}
		unresWires = append(unresWires, uw)
	}

	return diagWire{Duplicates: dupWires, Unresolved: unresWires}, nil
}

// marshalProvenance converts an instance's provenance to its wire form,
// preferring the raw path a failed parse preserved.
func marshalProvenance(inst *graph.Instance) *provenanceWire {
	prov := inst.Provenance()
	if prov == nil {
		return nil
	}
	// A recorded raw path is written back verbatim, INCLUDING an empty one.
	// Reading RawPath() alone could not tell a recorded "" from no record, so
	// a document that carried an empty path came back carrying "$" — and the
	// E_SNAPSHOT_PATH_FALLBACK warning that named the substitution was gone by
	// the second read.
	pathStr := prov.Path().String()
	if prov.HasRawPath() {
		pathStr = prov.RawPath()
	}
	return &provenanceWire{SourceName: prov.SourceName(), Path: pathStr}
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

// nonWhitespaceIndent reports the first non-whitespace rune in an indent, if
// any. The indent is emitted verbatim between JSON tokens, so anything else
// makes the output unparseable — and the writer returned OK on it, leaving a
// caller to discover the corruption at the next read.
func nonWhitespaceIndent(indent string) (string, bool) {
	for _, r := range indent {
		if !unicode.IsSpace(r) {
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

	// attestation (omitempty on read; Marshal always sets it, and
	// UpdateMetadata preserves an input's absence rather than inventing
	// a claim)
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
		return nil, fmt.Errorf("target key %s: graph.ParseKey: %w", keyStr, err)
	}
	return values, nil
}

// writeJSONString writes a JSON-encoded string to a strings.Builder.
func writeJSONString(b *strings.Builder, s string) {
	encoded, _ := json.Marshal(s)
	b.Write(encoded)
}

// marshalFatalErr creates a diag.Result with a Fatal E_INTERNAL for marshal failures.
func marshalFatalErr(section string, err error) diag.Result {
	c := diag.NewCollector(0)
	// The composed-nesting bound is a property of the snapshot, not of the
	// writer, and the reader already names it. Reporting it as an internal
	// failure would leave a caller unable to tell the two apart.
	if de, ok := errors.AsType[*depthExceededError](err); ok {
		c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DEPTH_EXCEEDED, de.Error()).
			WithDetail(diag.DetailKeyDepth, strconv.Itoa(de.depth)).
			WithDetail(diag.DetailKeyTypeName, de.ref).
			Build())
		return c.Result()
	}
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
		fmt.Sprintf("snapshot.Marshal: failed to serialize %s: %v", section, err)).Build())
	return c.Result()
}

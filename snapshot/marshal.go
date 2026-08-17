package snapshot

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Marshal serializes a Snapshot to the yammm snapshot persistence format
// (.ys). Output is deterministic by default — created_at is omitted unless
// [WithCreatedAt] opts in — so one snapshot and one option set always
// produce identical bytes. Marshal checks ctx.Err() at the start and once
// per type group during emission, returning Fatal E_CONTEXT_CANCELLED on
// cancellation; a caller-assembled state the writer cannot serialize
// returns nil bytes with Fatal E_INTERNAL, and a snapshot built through
// [graph.Graph] or [Load] never triggers that path. Panics if snap is nil.
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
	s := snap.Schema()

	// Step 1: Compute schema structural hash.
	schemaHash := schema.StructuralHash(s)
	schemaSource := s.SourceID().String()

	// Step 2: Read the snapshot once, then serialize the payload sections —
	// identity lives in the types table, every other position is a row index.
	view := newWriterView(snap)
	tt := buildTypeTable(view)

	groups, err := marshalInstances(ctx, view, s, tt)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			c := diag.NewCollector(0)
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, ctxErr.Error()).Build())
			return nil, c.Result()
		}
		return nil, marshalFatalErr("instances", err)
	}

	diags, err := marshalDiagnostics(view, s, tt)
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
	diagJSON, marshalErr := marshalSection(diags, cfg.indent)
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

	return assembleDocument(
		schemaHash, s.Name(), schemaSource, createdAt, features, metadata,
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
func newWriterView(snap *graph.Snapshot) *writerView {
	byID := make(map[schema.TypeID][]*graph.Instance)
	order := make([]schema.TypeID, 0, len(snap.Types()))
	edges := make(map[*graph.Instance][]*graph.Edge)

	addGroup := func(id schema.TypeID) {
		if _, ok := byID[id]; !ok {
			byID[id] = nil
			order = append(order, id)
		}
	}

	for _, typeID := range snap.Types() {
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
	slices.SortFunc(groups, func(a, b writerGroup) int {
		return cmp.Compare(a.id.String(), b.id.String())
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
			w, err := marshalInstance(inst, view.edges[inst], s, tt, false)
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
				cw, err := marshalInstance(child, nil, s, tt, true)
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
func marshalDiagnostics(view *writerView, s *schema.Schema, tt *typeTable) (diagWire, error) {
	dupWires := make([]dupWire, 0, len(view.duplicates))
	for _, d := range view.duplicates {
		row, err := tt.rowRef(d.Instance.TypeID(), "duplicate type")
		if err != nil {
			return diagWire{}, err
		}
		inst, err := marshalInstance(d.Instance, nil, s, tt, false)
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
			uw.TargetKey = nil
		default:
			uw.TargetKey = parseTargetKey(u.TargetKey)
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
	pathStr := prov.RawPath()
	if pathStr == "" {
		pathStr = prov.Path().String()
	}
	return &provenanceWire{SourceName: prov.SourceName(), Path: pathStr}
}

// assembleDocument builds the full .ys document with integrity hash.
func assembleDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string, version int,
) ([]byte, diag.Result) {
	// Step 3-5: Build canonical-form document (integrity_hash = "").
	canonBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		"", // integrity_hash placeholder
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	// Step 6: Compute integrity hash.
	h := sha256.Sum256(canonBytes)
	integrityHash := fmt.Sprintf("sha256:%x", h)

	// Step 7: Re-assemble with computed hash.
	finalBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		integrityHash,
		typesJSON, instancesJSON, diagJSON,
		indent, version,
	)

	return finalBytes, diag.OK()
}

// buildDocument assembles a complete .ys document from pre-serialized sections.
func buildDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
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
func parseTargetKey(keyStr string) []any {
	if keyStr == "" || keyStr == "[]" {
		return nil
	}
	var result []any
	if err := json.Unmarshal([]byte(keyStr), &result); err != nil {
		return nil
	}
	// Normalize json.Number values.
	for i, v := range result {
		result[i] = immutable.NormalizeValue(v)
	}
	return result
}

// writeJSONString writes a JSON-encoded string to a strings.Builder.
func writeJSONString(b *strings.Builder, s string) {
	encoded, _ := json.Marshal(s)
	b.Write(encoded)
}

// marshalFatalErr creates a diag.Result with a Fatal E_INTERNAL for marshal failures.
func marshalFatalErr(section string, err error) diag.Result {
	c := diag.NewCollector(0)
	c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
		fmt.Sprintf("snapshot.Marshal: failed to serialize %s: %v", section, err)).Build())
	return c.Result()
}

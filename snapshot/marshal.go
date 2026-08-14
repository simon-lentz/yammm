package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Marshal serializes a Snapshot to the yammm snapshot persistence format (.ys).
//
// The output is deterministic by default: serializing the same snapshot with
// the same options always produces identical bytes, enabling content-addressable
// storage and diff-based comparison. The created_at header field is omitted by
// default; use [WithCreatedAt] to opt in to a timestamp.
//
// Context cancellation: Marshal checks ctx.Err() at the start and once per
// type during instance iteration. On cancellation, Marshal returns
// (nil, result) where result contains a Fatal-severity E_CONTEXT_CANCELLED
// diagnostic.
//
// Panics if snap is nil (programming error).
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

	// Step 2: Serialize payload sections.
	// Types array.
	// Two identities rendering one tag share a v2 array entry; no instance is
	// lost, because each carries the type_id that distinguishes it.
	types := []string{}

	// Instances map.
	instancesMap := make(map[string][]instWire, len(snap.Types()))
	var allEdgeParts []edgeCollected
	for _, typeID := range snap.Types() {
		if err := ctx.Err(); err != nil {
			c := diag.NewCollector(0)
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, c.Result()
		}
		typeName := schema.TagForm(s, typeID)
		if _, seen := instancesMap[typeName]; !seen {
			types = append(types, typeName)
		}
		instances := snap.InstancesOf(typeID)
		wires := make([]instWire, 0, len(instances))
		for _, inst := range instances {
			w := marshalInstance(inst, snap, s, schemaSource, typeName)
			// Collect edges from EdgesFrom for the global edge sort.
			edges := snap.EdgesFrom(inst)
			for _, e := range edges {
				allEdgeParts = append(allEdgeParts, edgeCollected{edge: e})
			}
			wires = append(wires, w)
		}
		instancesMap[typeName] = append(instancesMap[typeName], wires...)
	}
	_ = allEdgeParts // edges are serialized per-instance, not globally

	// Diagnostics section.
	dups := snap.Duplicates()
	dupWires := make([]dupWire, 0, len(dups))
	for _, d := range dups {
		dw := dupWire{
			Type:     d.Instance.TypeName(),
			Key:      d.Instance.PrimaryKey().Clone(),
			Instance: marshalDuplicateInstance(d.Instance, s, schemaSource),
		}
		dupWires = append(dupWires, dw)
	}

	unresolved := snap.Unresolved()
	unresolvedWires := make([]unresolvedWire, 0, len(unresolved))
	for _, u := range unresolved {
		uw := unresolvedWire{
			SourceType: u.Source.TypeName(),
			SourceKey:  u.Source.PrimaryKey().Clone(),
			Relation:   u.Relation,
			TargetType: schema.TagForm(s, u.TargetType),
			Required:   u.Required,
			Reason:     u.Reason,
		}
		// TargetKey: null for "absent"/"empty", parsed array for "target_missing".
		// Properties: populated only for "target_missing" — "absent" and
		// "empty" describe a missing-or-empty target reference that never
		// had a target to attach properties to. omitempty on the wire
		// keeps those entries compact (no empty {} emitted).
		switch u.Reason {
		case "absent", "empty":
			uw.TargetKey = nil
		default:
			uw.TargetKey = parseTargetKey(u.TargetKey)
			var rel *schema.Relation
			if srcType, ok := resolveWireType(s, u.Source.TypeName(), u.Source.TypeID()); ok {
				rel, _ = srcType.Relation(u.Relation)
			}
			uw.Properties = wireEdgeProps(u.Properties(), rel)
		}
		unresolvedWires = append(unresolvedWires, uw)
	}

	diags := diagWire{
		Duplicates: dupWires,
		Unresolved: unresolvedWires,
	}
	if diags.Duplicates == nil {
		diags.Duplicates = []dupWire{}
	}
	if diags.Unresolved == nil {
		diags.Unresolved = []unresolvedWire{}
	}

	// Step 2 continued: Serialize each section to JSON.
	var typesJSON, instancesJSON, diagJSON []byte
	var marshalErr error

	if cfg.indent != "" {
		typesJSON, marshalErr = json.MarshalIndent(types, "  ", cfg.indent) //nolint:errchkjson // []string is always safe
	} else {
		typesJSON, marshalErr = json.Marshal(types) //nolint:errchkjson // []string is always safe
	}
	if marshalErr != nil {
		return nil, marshalFatalErr("types", marshalErr)
	}

	if cfg.indent != "" {
		instancesJSON, marshalErr = json.MarshalIndent(instancesMap, "  ", cfg.indent)
	} else {
		instancesJSON, marshalErr = json.Marshal(instancesMap)
	}
	if marshalErr != nil {
		return nil, marshalFatalErr("instances", marshalErr)
	}

	if cfg.indent != "" {
		diagJSON, marshalErr = json.MarshalIndent(diags, "  ", cfg.indent)
	} else {
		diagJSON, marshalErr = json.Marshal(diags)
	}
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
		cfg.indent,
	)
}

// assembleDocument builds the full .ys document with integrity hash.
func assembleDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string,
) ([]byte, diag.Result) {
	// Step 3-5: Build canonical-form document (integrity_hash = "").
	canonBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		"", // integrity_hash placeholder
		typesJSON, instancesJSON, diagJSON,
		indent,
	)

	// Step 6: Compute integrity hash.
	h := sha256.Sum256(canonBytes)
	integrityHash := fmt.Sprintf("sha256:%x", h)

	// Step 7: Re-assemble with computed hash.
	finalBytes := buildDocument(
		schemaHash, schemaName, schemaSource, createdAt, features, metadata,
		integrityHash,
		typesJSON, instancesJSON, diagJSON,
		indent,
	)

	return finalBytes, diag.OK()
}

// buildDocument assembles a complete .ys document from pre-serialized sections.
func buildDocument(
	schemaHash, schemaName, schemaSource, createdAt string,
	features []string, metadata map[string]string,
	integrityHash string,
	typesJSON, instancesJSON, diagJSON []byte,
	indent string,
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
	// literal constants, matching the headerWire field order exactly.
	headerJSON := buildHeaderBytes(hdr, indent)

	if indent != "" {
		return assembleIndented(headerJSON, typesJSON, instancesJSON, diagJSON, indent)
	}
	return assembleCompact(headerJSON, typesJSON, instancesJSON, diagJSON)
}

// buildHeaderBytes returns the JSON-encoded header object, selecting
// between compact and indented form based on the indent parameter.
// Shared between buildDocument (the Marshal path) and UpdateMetadata
// (the metadata-rewrite fast path); both paths must produce byte-identical
// headers given the same marshalHeaderWire + indent inputs so the
// body-byte-range stability contract holds across both primitives.
func buildHeaderBytes(hdr marshalHeaderWire, indent string) []byte {
	if indent != "" {
		return buildHeaderIndented(hdr, indent)
	}
	return buildHeaderCompact(hdr)
}

// buildHeaderCompact produces the header object JSON in compact mode.
// Fields are written in headerWire declaration order with version and
// schema_hash_algorithm as literal constants.
func buildHeaderCompact(hdr marshalHeaderWire) []byte {
	var b strings.Builder
	b.WriteByte('{')

	// version (literal constant)
	b.WriteString(`"version":`)
	fmt.Fprintf(&b, "%d", currentVersion)

	// schema_name
	b.WriteString(`,"schema_name":`)
	writeJSONString(&b, hdr.SchemaName)

	// schema_source
	b.WriteString(`,"schema_source":`)
	writeJSONString(&b, hdr.SchemaSource)

	// schema_hash
	b.WriteString(`,"schema_hash":`)
	writeJSONString(&b, hdr.SchemaHash)

	// schema_hash_algorithm (literal constant)
	b.WriteString(`,"schema_hash_algorithm":`)
	fmt.Fprintf(&b, "%d", currentHashAlgoVersion)

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
func buildHeaderIndented(hdr marshalHeaderWire, indent string) []byte {
	var b strings.Builder
	pfx := indent + indent // header fields are nested inside yammm_snapshot
	b.WriteString("{\n")

	// version
	b.WriteString(pfx)
	b.WriteString(`"version": `)
	fmt.Fprintf(&b, "%d", currentVersion)

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
	fmt.Fprintf(&b, "%d", currentHashAlgoVersion)

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

// marshalInstance converts a graph.Instance to its wire representation.
func marshalInstance(inst *graph.Instance, snap *graph.Snapshot, s *schema.Schema, schemaSource, mapKey string) instWire {
	t, _ := resolveWireType(s, mapKey, inst.TypeID())
	w := instWire{
		Key:        inst.PrimaryKey().Clone(),
		Properties: wireProps(inst.Properties(), t),
	}

	// type_id: omit when recoverable from context.
	typeID := inst.TypeID()
	if !typeID.IsZero() && (typeID.Name() != mapKey || typeID.SchemaPath().String() != schemaSource) {
		w.TypeID = &typeIDWire{
			SchemaPath: typeID.SchemaPath().String(),
			Name:       typeID.Name(),
		}
	}

	// edges: group EdgesFrom by relation.
	edges := snap.EdgesFrom(inst)
	if len(edges) > 0 {
		edgeMap := make(map[string][]edgeWire)
		for _, e := range edges {
			var rel *schema.Relation
			if t != nil {
				rel, _ = t.Relation(e.Relation())
			}
			ew := edgeWire{
				TargetType: e.Target().TypeName(),
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

	// composed: recursive.
	compRels := inst.ComposedRelations()
	if len(compRels) > 0 {
		compMap := make(map[string][]instWire, len(compRels))
		for _, relName := range compRels {
			// The relation's target is what the decoder recovers when the wire
			// omits type_id; the child's own type may differ.
			var childType *schema.Type
			if t != nil {
				if rel, ok := t.Relation(relName); ok {
					childType, _ = s.TypeByID(rel.TargetID())
				}
			}
			children := inst.Composed(relName)
			childWires := make([]instWire, 0, len(children))
			for _, child := range children {
				cw := marshalComposedChild(child, s, childType)
				childWires = append(childWires, cw)
			}
			compMap[relName] = childWires
		}
		w.Composed = compMap
	}

	// provenance
	prov := inst.Provenance()
	if prov != nil {
		pathStr := prov.RawPath()
		if pathStr == "" {
			pathStr = prov.Path().String()
		}
		w.Provenance = &provenanceWire{
			SourceName: prov.SourceName(),
			Path:       pathStr,
		}
	}

	return w
}

// marshalComposedChild marshals a composed child under target, the parent
// relation's declared target type (nil when the parent never resolved).
// Properties and nested relations resolve against the child's own type, which
// is not always the target: encoding a child's values under another type's
// constraints corrupts them. Composed children never carry edges.
func marshalComposedChild(inst *graph.Instance, s *schema.Schema, target *schema.Type) instWire {
	own, _ := resolveWireType(s, inst.TypeName(), inst.TypeID())
	w := instWire{
		Key:        inst.PrimaryKey().Clone(),
		Properties: wireProps(inst.Properties(), own),
	}

	// type_id rides only when the decoder's recovery — the parent relation's
	// target — would land on a different type.
	typeID := inst.TypeID()
	if !typeID.IsZero() && (target == nil || target.ID() != typeID) {
		w.TypeID = &typeIDWire{
			SchemaPath: typeID.SchemaPath().String(),
			Name:       typeID.Name(),
		}
	}

	// composed: recursive.
	compRels := inst.ComposedRelations()
	if len(compRels) > 0 {
		compMap := make(map[string][]instWire, len(compRels))
		for _, relName := range compRels {
			var childType *schema.Type
			if own != nil {
				if rel, ok := own.Relation(relName); ok {
					childType, _ = s.TypeByID(rel.TargetID())
				}
			}
			children := inst.Composed(relName)
			childWires := make([]instWire, 0, len(children))
			for _, child := range children {
				cw := marshalComposedChild(child, s, childType)
				childWires = append(childWires, cw)
			}
			compMap[relName] = childWires
		}
		w.Composed = compMap
	}

	// provenance
	prov := inst.Provenance()
	if prov != nil {
		pathStr := prov.RawPath()
		if pathStr == "" {
			pathStr = prov.Path().String()
		}
		w.Provenance = &provenanceWire{
			SourceName: prov.SourceName(),
			Path:       pathStr,
		}
	}

	return w
}

// marshalDuplicateInstance marshals a duplicate record's instance.
// Duplicate instances have no edges and no composed children.
func marshalDuplicateInstance(inst *graph.Instance, s *schema.Schema, schemaSource string) instWire {
	t, _ := resolveWireType(s, inst.TypeName(), inst.TypeID())
	w := instWire{
		Key:        inst.PrimaryKey().Clone(),
		Properties: wireProps(inst.Properties(), t),
	}

	typeID := inst.TypeID()
	if !typeID.IsZero() && (typeID.Name() != inst.TypeName() || typeID.SchemaPath().String() != schemaSource) {
		w.TypeID = &typeIDWire{
			SchemaPath: typeID.SchemaPath().String(),
			Name:       typeID.Name(),
		}
	}

	prov := inst.Provenance()
	if prov != nil {
		pathStr := prov.RawPath()
		if pathStr == "" {
			pathStr = prov.Path().String()
		}
		w.Provenance = &provenanceWire{
			SourceName: prov.SourceName(),
			Path:       pathStr,
		}
	}

	return w
}

// edgeCollected is an internal type used during edge collection.
type edgeCollected struct {
	edge *graph.Edge
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

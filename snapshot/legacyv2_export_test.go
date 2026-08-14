package snapshot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// The legacy writer, kept only in the test binary.
//
// Production stopped writing wire v2 at yammm v0.12.0 while the read path
// accepts v2 documents indefinitely. Deriving legacy fixtures from the current
// writer is not possible — it emits v3 — so this is a faithful copy of the v2
// writer rather than a reimplementation: the type_id omission predicates below
// are the exact rules the v2 decoder inverts, and a paraphrase of them would
// test the paraphrase.

// legacyVersion is the wire version MarshalLegacyV2 stamps.
const legacyVersion = 2

// MarshalLegacyV2 serializes a snapshot in wire format 2.
func MarshalLegacyV2(ctx context.Context, snap *graph.Snapshot, opts ...Option) ([]byte, diag.Result) {
	_ = ctx
	cfg := applyOptions(opts)
	s := snap.Schema()

	schemaHash := schema.StructuralHash(s)
	schemaSource := s.SourceID().String()

	// Step 2: Serialize payload sections.
	// Types array.
	// Two identities rendering one tag share a v2 array entry; no instance is
	// lost, because each carries the type_id that distinguishes it.
	types := []string{}

	// Instances map.
	instancesMap := make(map[string][]instWire, len(snap.Types()))
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
			wires = append(wires, w)
		}
		instancesMap[typeName] = append(instancesMap[typeName], wires...)
	}

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

	var typesJSON, instancesJSON, diagJSON []byte
	var marshalErr error

	if cfg.indent != "" {
		typesJSON, marshalErr = json.MarshalIndent(types, "  ", cfg.indent)
	} else {
		typesJSON, marshalErr = json.Marshal(types)
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
		cfg.indent, legacyVersion,
	)
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

package snapshot

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// tableMissError reports a type identity the writer denotes at some position
// but the types table does not hold.
type tableMissError struct {
	id       schema.TypeID
	position string
}

func (e *tableMissError) Error() string {
	return fmt.Sprintf("type %s at %s position is absent from the types table", e.id, e.position)
}

// typeTable is the v3 types section: every type identity the document denotes,
// in TypeID order, with the row index each reference uses.
type typeTable struct {
	entries []typeTableEntry
	index   map[schema.TypeID]int
}

// indexOf returns the table row for id. A miss means the collection pass did
// not reach a position the writer is now emitting, which is an internal
// inconsistency rather than a property of the data.
func (tt *typeTable) indexOf(id schema.TypeID) (int, bool) {
	i, ok := tt.index[id]
	return i, ok
}

// writerTypeID is the identity a v3 writer files an instance under: its own,
// or the one its name resolves to when a caller assembled the instance without
// one. A v3 position denotes a type rather than naming it, so an instance that
// reaches the wire with neither cannot be written at all.
func writerTypeID(inst *graph.Instance, s *schema.Schema) schema.TypeID {
	if id := inst.TypeID(); !id.IsZero() {
		return id
	}
	if t, ok := resolveWireType(s, inst.TypeName(), schema.TypeID{}); ok {
		return t.ID()
	}
	return schema.TypeID{}
}

// buildTypeTable collects every type identity snap denotes — instance,
// composed child, edge target, duplicate, unresolved edge — and orders them by
// TypeID. An unresolved target has no instance, so the table can be wider than
// the set of types holding one.
func buildTypeTable(snap *graph.Snapshot, s *schema.Schema) *typeTable {
	seen := make(map[schema.TypeID]struct{})

	var collectInstance func(inst *graph.Instance)
	collectInstance = func(inst *graph.Instance) {
		if inst == nil {
			return
		}
		seen[writerTypeID(inst, s)] = struct{}{}
		for _, relName := range inst.ComposedRelations() {
			for _, child := range inst.Composed(relName) {
				collectInstance(child)
			}
		}
	}

	for _, typeID := range snap.Types() {
		seen[typeID] = struct{}{}
		for _, inst := range snap.InstancesOf(typeID) {
			collectInstance(inst)
			for _, e := range snap.EdgesFrom(inst) {
				seen[writerTypeID(e.Target(), s)] = struct{}{}
			}
		}
	}

	for _, d := range snap.Duplicates() {
		collectInstance(d.Instance)
		if d.Conflict != nil {
			seen[writerTypeID(d.Conflict, s)] = struct{}{}
		}
		if d.Parent != nil {
			seen[writerTypeID(d.Parent, s)] = struct{}{}
		}
	}

	for _, u := range snap.Unresolved() {
		if u.Source != nil {
			seen[writerTypeID(u.Source, s)] = struct{}{}
		}
		seen[u.TargetType] = struct{}{}
	}

	delete(seen, schema.TypeID{})

	ids := make([]schema.TypeID, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})

	tt := &typeTable{
		entries: make([]typeTableEntry, 0, len(ids)),
		index:   make(map[schema.TypeID]int, len(ids)),
	}
	for i, id := range ids {
		tt.entries = append(tt.entries, typeTableEntry{
			SchemaPath: id.SchemaPath().String(),
			Name:       id.Name(),
			Tag:        schema.TagForm(s, id),
		})
		tt.index[id] = i
	}
	return tt
}

// marshalInstancesV3 builds the instances section: one entry per table row,
// holding that type's root instances. A row whose type holds none — a type
// denoted only as an unresolved edge's target — carries an empty array, which
// is what makes the section's length equal the table's.
func marshalInstancesV3(ctx context.Context, snap *graph.Snapshot, s *schema.Schema, tt *typeTable) ([][]instWireV3, error) {
	sections := make([][]instWireV3, len(tt.entries))
	for i := range sections {
		sections[i] = []instWireV3{}
	}

	for _, typeID := range snap.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("marshalling instances: %w", err)
		}
		row, ok := tt.indexOf(typeID)
		if !ok {
			return nil, &tableMissError{id: typeID, position: "instance type"}
		}
		instances := snap.InstancesOf(typeID)
		wires := make([]instWireV3, 0, len(instances))
		for _, inst := range instances {
			w, err := marshalInstanceV3(inst, snap, s, tt, false)
			if err != nil {
				return nil, err
			}
			wires = append(wires, w)
		}
		sections[row] = wires
	}
	return sections, nil
}

// marshalInstanceV3 converts an instance to its v3 wire form. carryType is
// false only for a root instance, whose position in the instances section
// already denotes its type.
func marshalInstanceV3(
	inst *graph.Instance,
	snap *graph.Snapshot,
	s *schema.Schema,
	tt *typeTable,
	carryType bool,
) (instWireV3, error) {
	id := writerTypeID(inst, s)
	t, _ := s.TypeByID(id)
	w := instWireV3{
		Key:        inst.PrimaryKey().Clone(),
		Properties: wireProps(inst.Properties(), t),
	}

	if carryType {
		row, ok := tt.indexOf(id)
		if !ok {
			return instWireV3{}, &tableMissError{id: id, position: "instance type"}
		}
		w.Type = &row
	}

	if snap != nil {
		edges := snap.EdgesFrom(inst)
		if len(edges) > 0 {
			edgeMap := make(map[string][]edgeWireV3)
			for _, e := range edges {
				var rel *schema.Relation
				if t != nil {
					rel, _ = t.Relation(e.Relation())
				}
				targetID := writerTypeID(e.Target(), s)
				row, ok := tt.indexOf(targetID)
				if !ok {
					return instWireV3{}, &tableMissError{id: targetID, position: "edge target"}
				}
				ew := edgeWireV3{
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
	}

	compRels := inst.ComposedRelations()
	if len(compRels) > 0 {
		compMap := make(map[string][]instWireV3, len(compRels))
		for _, relName := range compRels {
			children := inst.Composed(relName)
			childWires := make([]instWireV3, 0, len(children))
			for _, child := range children {
				cw, err := marshalInstanceV3(child, nil, s, tt, true)
				if err != nil {
					return instWireV3{}, err
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

// marshalDiagnosticsV3 builds the v3 diagnostics section.
func marshalDiagnosticsV3(snap *graph.Snapshot, s *schema.Schema, tt *typeTable) (diagWireV3, error) {
	dups := snap.Duplicates()
	dupWires := make([]dupWireV3, 0, len(dups))
	for _, d := range dups {
		dupID := writerTypeID(d.Instance, s)
		row, ok := tt.indexOf(dupID)
		if !ok {
			return diagWireV3{}, &tableMissError{id: dupID, position: "duplicate type"}
		}
		inst, err := marshalInstanceV3(d.Instance, nil, s, tt, true)
		if err != nil {
			return diagWireV3{}, err
		}
		dw := dupWireV3{
			Type:     row,
			Key:      d.Instance.PrimaryKey().Clone(),
			Instance: inst,
		}
		if d.Relation != "" && d.Parent != nil {
			parentID := writerTypeID(d.Parent, s)
			parentRow, ok := tt.indexOf(parentID)
			if !ok {
				return diagWireV3{}, &tableMissError{id: parentID, position: "duplicate parent type"}
			}
			dw.ParentType = &parentRow
			dw.ParentKey = d.Parent.PrimaryKey().Clone()
			dw.Relation = d.Relation
		}
		dupWires = append(dupWires, dw)
	}

	unresolved := snap.Unresolved()
	unresWires := make([]unresolvedWireV3, 0, len(unresolved))
	for _, u := range unresolved {
		sourceID := writerTypeID(u.Source, s)
		sourceRow, ok := tt.indexOf(sourceID)
		if !ok {
			return diagWireV3{}, &tableMissError{id: sourceID, position: "unresolved source type"}
		}
		targetRow, ok := tt.indexOf(u.TargetType)
		if !ok {
			return diagWireV3{}, &tableMissError{id: u.TargetType, position: "unresolved target type"}
		}
		uw := unresolvedWireV3{
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

	return diagWireV3{Duplicates: dupWires, Unresolved: unresWires}, nil
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

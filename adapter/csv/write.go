package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// MarshalSnapshot serializes a graph snapshot to CSV, returning one byte slice
// per type. CSV is inherently single-type-per-file, so the output is a map
// from type name to CSV bytes.
//
// Returns [ErrNilSnapshot] if result is nil, and refuses a snapshot holding a
// composed child: see [refuseComposedChildren].
func (a *Adapter) MarshalSnapshot(
	ctx context.Context,
	result *graph.Snapshot,
) (map[string][]byte, error) {
	if result == nil {
		return nil, ErrNilSnapshot
	}
	if err := refuseComposedChildren(result); err != nil {
		return nil, err
	}
	output := make(map[string][]byte, len(result.Types()))

	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("csv marshal snapshot: %w", err)
		}

		typeName, ok := schema.AddressableTag(result.Schema(), typeID)
		if !ok {
			return nil, unnameableDenotedType(typeID)
		}
		schemaType, _ := result.Schema().TypeByID(typeID)
		instances := result.InstancesOf(typeID)

		var buf bytes.Buffer
		if err := a.writeSnapshotTypeTo(ctx, &buf, instances, result, schemaType); err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}
		output[typeName] = buf.Bytes()
	}

	return output, nil
}

// WriteSnapshot writes a graph snapshot to per-type writers. The writerFor
// function is called once per type to obtain the destination writer.
//
// Returns [ErrNilSnapshot] if result is nil, and refuses a snapshot holding a
// composed child before it requests any writer: see [refuseComposedChildren].
func (a *Adapter) WriteSnapshot(
	ctx context.Context,
	writerFor func(typeName string) (io.Writer, error),
	result *graph.Snapshot,
) error {
	if result == nil {
		return ErrNilSnapshot
	}
	if err := refuseComposedChildren(result); err != nil {
		return err
	}
	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("csv write snapshot: %w", err)
		}

		typeName, ok := schema.AddressableTag(result.Schema(), typeID)
		if !ok {
			return unnameableDenotedType(typeID)
		}
		w, err := writerFor(typeName)
		if err != nil {
			return fmt.Errorf("writer for type %q: %w", typeName, err)
		}

		schemaType, _ := result.Schema().TypeByID(typeID)
		instances := result.InstancesOf(typeID)

		if err := a.writeSnapshotTypeTo(ctx, w, instances, result, schemaType); err != nil {
			return fmt.Errorf("type %q: %w", typeName, err)
		}
	}

	return nil
}

// refuseComposedChildren returns an error naming the first instance, in type
// and instance order, that holds a composed child. A CSV row is flat, so the
// writer has no column for a child and would drop the subtree. It runs before
// any output is produced, so a refused export writes nothing. A composition
// with no children loses nothing and is not refused.
func refuseComposedChildren(snap *graph.Snapshot) error {
	for _, typeID := range snap.Types() {
		for _, inst := range snap.InstancesOf(typeID) {
			// ComposedRelations lists only relations holding a child.
			rels := inst.ComposedRelations()
			if len(rels) == 0 {
				continue
			}
			typeName, ok := schema.AddressableTag(snap.Schema(), typeID)
			if !ok {
				return unnameableDenotedType(typeID)
			}
			children := "composed children"
			if n := inst.ComposedCount(rels[0]); n == 1 {
				children = "a composed child"
			} else {
				children = strconv.Itoa(n) + " " + children
			}
			return fmt.Errorf("csv adapter: type %q instance %s: composition %q holds %s, which a CSV row has no column for, so the export would drop it",
				typeName, inst.PrimaryKey(), rels[0], children)
		}
	}
	return nil
}

// unnameableDenotedType reports a snapshot denoting a type the entry schema
// cannot name. Every constructor refuses such a snapshot, so reaching this is an
// invariant violation rather than a caller error.
func unnameableDenotedType(id schema.TypeID) error {
	return fmt.Errorf("csv adapter: snapshot denotes type %s, which the entry schema cannot name, so per-type output has no name for it; every constructor refuses such a snapshot, so this is an invariant violation", id)
}

// writeSnapshotTypeTo writes graph.Instance values from a snapshot as CSV rows.
// It uses snap.EdgesFrom for edge resolution instead of ValidInstance edge data.
func (a *Adapter) writeSnapshotTypeTo(
	ctx context.Context,
	w io.Writer,
	instances []*graph.Instance,
	snap *graph.Snapshot,
	schemaType *schema.Type,
) error {
	columns, err := buildColumnList(schemaType, snap.Schema())
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	writer.Comma = a.config.delimiter

	if err := writer.Write(columns); err != nil {
		return fmt.Errorf("csv write header: %w", err)
	}

	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			writer.Flush()
			return fmt.Errorf("csv write: %w", err)
		}

		row, err := a.instanceToRow(inst.Properties(), snapshotEdges(inst, snap), columns, schemaType, snap.Schema())
		if err != nil {
			// Flushed as the cancellation branch is, so a refused export still
			// parses as far as it goes.
			writer.Flush()
			return fmt.Errorf("instance %s: %w", inst.PrimaryKey(), err)
		}
		if err := writeRecord(writer, w, row); err != nil {
			return err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv flush: %w", err)
	}
	return nil
}

// writeRecord writes one row through writer, whose destination is w. A row
// whose only field is empty is written as a quoted empty field: [csv.Writer]
// writes it as a blank line, which [csv.Reader] skips, so the instance would
// vanish on the way back. A quoted empty field reads back as one empty field.
func writeRecord(writer *csv.Writer, w io.Writer, row []string) error {
	if len(row) != 1 || row[0] != "" {
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("csv write row: %w", err)
		}
		return nil
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv write row: %w", err)
	}
	if _, err := io.WriteString(w, `""`+"\n"); err != nil {
		return fmt.Errorf("csv write row: %w", err)
	}
	return nil
}

// snapshotEdges groups an instance's outgoing edges by relation name.
// EdgesFrom is O(1) per instance and returns sorted edges, so each group
// keeps the (targetType, targetKey) order.
func snapshotEdges(inst *graph.Instance, snap *graph.Snapshot) map[string][]*graph.Edge {
	allEdges := snap.EdgesFrom(inst)
	byRel := make(map[string][]*graph.Edge, len(allEdges))
	for _, e := range allEdges {
		byRel[e.Relation()] = append(byRel[e.Relation()], e)
	}
	return byRel
}

// buildColumnList determines the CSV column order from schema metadata:
// properties sorted alphabetically, then per association (sorted by field
// name) one column per FK component — <field>._target_<pk> — and one per
// edge property, <field>.<prop>. No declared name can contain a dot or a
// leading underscore, so the dotted grammar is unambiguous. The target
// type supplies the component names; an unresolvable target is an error,
// because the writer would otherwise emit columns its own parser cannot
// name.
func buildColumnList(schemaType *schema.Type, s *schema.Schema) ([]string, error) {
	if schemaType == nil {
		return nil, nil
	}

	// Collect property names.
	var propNames []string
	for _, prop := range schemaType.AllPropertiesSlice() {
		propNames = append(propNames, prop.Name())
	}
	slices.Sort(propNames)

	rels := slices.SortedFunc(schemaType.AllAssociations(), func(a, b *schema.Relation) int {
		return strings.Compare(a.FieldName(), b.FieldName())
	})
	var edgeCols []string
	for _, rel := range rels {
		target, ok := s.TypeByID(rel.TargetID())
		if !ok {
			return nil, fmt.Errorf("csv adapter: association %q: target type %s does not resolve, so its _target_ column names are unknowable",
				rel.Name(), rel.TargetID())
		}
		for _, pk := range target.PrimaryKeysSlice() {
			edgeCols = append(edgeCols, rel.FieldName()+"._target_"+pk.Name())
		}
		props := rel.PropertiesSlice()
		slices.SortFunc(props, func(a, b *schema.Property) int {
			return strings.Compare(a.Name(), b.Name())
		})
		for _, p := range props {
			edgeCols = append(edgeCols, rel.FieldName()+"."+p.Name())
		}
	}

	return append(propNames, edgeCols...), nil
}

// instanceToRow converts properties and edge data to a CSV row aligned
// with columns. Edge columns zip across the relation's targets on the list
// separator; an absent edge leaves every column of its group empty, which
// the parser reads as absent, never null. A null or missing property writes
// an empty cell, which the parser reads through the schema. A non-empty list
// whose cell renders empty is refused: its one element renders as "", and the
// list grammar writes [""] and [] alike.
func (a *Adapter) instanceToRow(
	props immutable.Properties,
	edgesByRel map[string][]*graph.Edge,
	columns []string,
	schemaType *schema.Type,
	s *schema.Schema,
) ([]string, error) {
	cells := make(map[string]string)
	if schemaType != nil {
		for rel := range schemaType.AllAssociations() {
			if err := a.relationCells(rel, s, edgesByRel[rel.Name()], cells); err != nil {
				return nil, err
			}
		}
	}

	row := make([]string, len(columns))
	for i, col := range columns {
		if val, ok := props.Get(col); ok {
			cell := a.valueToString(val, propertyConstraint(schemaType, col))
			if list, isList := val.Slice(); isList && list.Len() > 0 && cell == "" {
				return nil, fmt.Errorf("csv adapter: property %q: a list holding one empty element writes the cell an empty list writes, so the element would not survive the round trip", col)
			}
			row[i] = cell
			continue
		}
		row[i] = cells[col]
	}

	return row, nil
}

// relationCells renders one association's edge columns into cells: per FK
// component and per edge property, one segment per target, escaped and
// joined on the list separator. No edges means every cell stays "", the
// absent-group marker.
//
// An edge whose every cell renders empty is refused: one target whose key
// components are all "" and whose edge properties are all absent or "" writes
// the absent-group marker, so it would read back as no edge at all. Two or more
// targets always write a separator, so only a lone target can collide.
func (a *Adapter) relationCells(rel *schema.Relation, s *schema.Schema, edges []*graph.Edge, cells map[string]string) error {
	target, ok := s.TypeByID(rel.TargetID())
	if !ok {
		// buildColumnList already refused this shape; nothing to render.
		return nil
	}
	field := rel.FieldName()
	var group []string

	for i, pk := range target.PrimaryKeysSlice() {
		col := field + "._target_" + pk.Name()
		group = append(group, col)
		if len(edges) == 0 {
			cells[col] = ""
			continue
		}
		segs := make([]string, len(edges))
		for j, e := range edges {
			key := e.Target().PrimaryKey()
			if i < key.Len() {
				segs[j] = escapeListElem(scalarCell(canonicalOrRaw(key.Get(i).Unwrap(), pk.Constraint())), a.config.listSep)
			}
		}
		cells[col] = strings.Join(segs, a.config.listSep)
	}

	for _, p := range rel.PropertiesSlice() {
		col := field + "." + p.Name()
		group = append(group, col)
		if len(edges) == 0 {
			cells[col] = ""
			continue
		}
		segs := make([]string, len(edges))
		for j, e := range edges {
			if v, ok := e.Properties().Get(p.Name()); ok && !v.IsNil() {
				segs[j] = escapeListElem(scalarCell(canonicalOrRaw(v.Unwrap(), p.Constraint())), a.config.listSep)
			}
		}
		cells[col] = strings.Join(segs, a.config.listSep)
	}

	if len(edges) == 0 {
		return nil
	}
	for _, col := range group {
		if cells[col] != "" {
			return nil
		}
	}
	return fmt.Errorf("csv adapter: association %q: its target's key and every edge property render empty, so its columns would read back as no association", rel.Name())
}

// scalarCell renders one scalar value as cell text.
func scalarCell(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// propertyConstraint returns a declared property's constraint, or nil for a
// column the type does not declare.
func propertyConstraint(t *schema.Type, name string) schema.Constraint {
	if t == nil {
		return nil
	}
	p, ok := t.Property(name)
	if !ok {
		return nil
	}
	return p.Constraint()
}

// canonicalOrRaw renders raw in the form its constraint stores, and returns it
// untouched when the constraint cannot render it. An export has no diagnostic
// channel, so one malformed cell must not fail the whole file.
func canonicalOrRaw(raw any, c schema.Constraint) any {
	canonical, err := instance.CanonicalValue(raw, c)
	if err != nil {
		return raw
	}
	return canonical
}

// elementConstraint returns a list constraint's element constraint, or nil for
// any other constraint.
func elementConstraint(c schema.Constraint) schema.Constraint {
	if c == nil {
		return nil
	}
	lc, ok := schema.ResolveAlias(c).(schema.ListConstraint)
	if !ok {
		return nil
	}
	return lc.Element()
}

// valueToString renders an immutable.Value as a CSV cell string, in the form
// its constraint stores. Null renders as the empty cell.
func (a *Adapter) valueToString(val immutable.Value, c schema.Constraint) string {
	if val.IsNil() {
		return ""
	}

	// A collection renders elementwise, so the element constraint does the
	// work rather than the whole value.
	if s, ok := val.Slice(); ok {
		return a.sliceToString(s, elementConstraint(c))
	}

	return scalarCell(canonicalOrRaw(val.Unwrap(), c))
}

// sliceToString renders an immutable.Slice as a list-separated string. Each
// element renders under elem, so a List<Timestamp> canonicalizes at its
// elements and not only at its outer level. Elements escape through the
// shared helper, so a value containing the separator survives the split.
func (a *Adapter) sliceToString(s immutable.Slice, elem schema.Constraint) string {
	parts := make([]string, s.Len())
	for i, v := range s.Iter2() {
		if !v.IsNil() {
			parts[i] = escapeListElem(fmt.Sprint(canonicalOrRaw(v.Unwrap(), elem)), a.config.listSep)
		}
	}
	return strings.Join(parts, a.config.listSep)
}

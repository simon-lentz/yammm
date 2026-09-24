package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/adapter/internal/constraintof"
	"github.com/simon-lentz/yammm/adapter/internal/refusal"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// MarshalSnapshot serializes a graph snapshot to CSV, returning one byte slice
// per type. CSV is inherently single-type-per-file, so the output is a map
// from type name to CSV bytes.
//
// Returns [ErrNilSnapshot] if result is nil, refuses a setting the adapter
// cannot use with [ErrConfig], and refuses a snapshot holding a composed child:
// see [refuseComposedChildren].
func (a *Adapter) MarshalSnapshot(
	ctx context.Context,
	result *graph.Snapshot,
) (map[string][]byte, error) {
	if result == nil {
		return nil, ErrNilSnapshot
	}
	if err := a.configError(); err != nil {
		return nil, err
	}
	if err := refuseComposedChildren(result); err != nil {
		return nil, err
	}
	types := result.Types()
	output := make(map[string][]byte, len(types))

	for _, typeID := range types {
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
// Returns [ErrNilSnapshot] if result is nil, and refuses a setting the adapter
// cannot use, with [ErrConfig], and a snapshot holding a composed child before
// it requests any writer: see [refuseComposedChildren].
func (a *Adapter) WriteSnapshot(
	ctx context.Context,
	writerFor func(typeName string) (io.Writer, error),
	result *graph.Snapshot,
) error {
	if result == nil {
		return ErrNilSnapshot
	}
	if err := a.configError(); err != nil {
		return err
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
			return refusal.New(ErrUnrepresentable, "csv adapter: type %q instance %s: composition %q holds %s, which a CSV row has no column for, so the export would drop it",
				typeName, inst.PrimaryKey(), rels[0], children)
		}
	}
	return nil
}

// unnameableDenotedType reports a snapshot denoting a type the entry schema
// cannot name. No constructor builds one from data bound to this schema, so
// reaching this is a broken invariant or a snapshot imported from another
// schema against [graph.NewFromSnapshot]'s contract. It carries no refusal
// class: [ErrUnrepresentable] names data a caller can act on, and neither of
// those is that.
func unnameableDenotedType(id schema.TypeID) error {
	return fmt.Errorf("csv adapter: snapshot denotes type %s, which the entry schema cannot name, so per-type output has no name for it; no constructor builds such a snapshot from data bound to this schema, so an invariant is broken or the snapshot was imported from another schema, against graph.NewFromSnapshot's contract", id)
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
// name. Both [schema.Load] and [schema.NewBuilder] refuse a schema whose
// association target does not resolve, so no snapshot reaches that arm and it
// carries no refusal class.
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
			return nil, fmt.Errorf("csv adapter: association %q: target type %s does not resolve, so its _target_ column names are unknowable; every schema constructor refuses such a schema, so this is an invariant violation",
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
// an empty cell, which the parser reads through the schema. A cell holding a
// CR LF is refused.
func (a *Adapter) instanceToRow(
	props immutable.Properties,
	edgesByRel map[string][]*graph.Edge,
	columns []string,
	schemaType *schema.Type,
	s *schema.Schema,
) ([]string, error) {
	cells := make(map[string]string)
	if schemaType != nil {
		declared := 0
		for rel := range schemaType.AllAssociations() {
			edges := edgesByRel[rel.Name()]
			if len(edges) > 0 {
				declared++
			}
			if err := a.relationCells(rel, s, edges, cells); err != nil {
				return nil, err
			}
		}
		// Every column comes from a declared association, so an edge under any
		// other name has none and the row would be written without it.
		if declared != len(edgesByRel) {
			// In name order, so the relation a refusal names is the same on
			// every run.
			for _, relName := range slices.Sorted(maps.Keys(edgesByRel)) {
				if rel, ok := schemaType.Relation(relName); !ok || !rel.IsAssociation() {
					return nil, refusal.New(ErrUnrepresentable, "csv adapter: edge under %q, which type %s declares no association for, so it has no column",
						relName, schemaType.ID())
				}
			}
		}
	}

	row := make([]string, len(columns))
	for i, col := range columns {
		if val, ok := props.Get(col); ok {
			cell, err := a.valueToString(val, constraintof.Property(schemaType, col))
			if err != nil {
				return nil, fmt.Errorf("csv adapter: property %q: %w", col, err)
			}
			row[i] = cell
			continue
		}
		row[i] = cells[col]
	}
	for i, cell := range row {
		if strings.Contains(cell, "\r\n") {
			return nil, fmt.Errorf("csv adapter: column %q: %w", columns[i], errCRLF)
		}
	}

	return row, nil
}

// errCRLF refuses a cell whose text holds a CR LF: [encoding/csv]'s reader
// turns every CR LF into LF, inside a quoted field too. It carries
// [ErrUnrepresentable] and keeps its own text, so a caller matches either.
var errCRLF = refusal.New(ErrUnrepresentable, "its text holds a CR LF, which encoding/csv reads back as LF, so the value would not survive the round trip")

// relationCells renders one association's edge columns into cells: per FK
// component and per edge property, one segment per target, escaped and
// joined on the list separator. No edges leaves every cell of the group unset,
// which a row reads as "", the absent-group marker.
//
// An edge whose every cell renders empty is refused: one target whose key
// components are all "" and whose edge properties are all absent or "" writes
// the absent-group marker, so it would read back as no edge at all. Two or more
// targets always write a separator, so only a lone target can collide.
func (a *Adapter) relationCells(rel *schema.Relation, s *schema.Schema, edges []*graph.Edge, cells map[string]string) error {
	// No edges leaves every column of the group unset, which a row reads as "".
	if len(edges) == 0 {
		return nil
	}
	target, ok := s.TypeByID(rel.TargetID())
	if !ok {
		// buildColumnList already refused this shape; nothing to render.
		return nil
	}
	// A (one) association reads back as ONE object, and the validator refuses
	// the array two zipped targets make, so several edges have no shape.
	if !rel.IsMany() && len(edges) > 1 {
		return refusal.New(ErrUnrepresentable, "csv adapter: (one) association %q carries %d edges, and a (one) association reads back as one target",
			rel.Name(), len(edges))
	}
	field := rel.FieldName()
	var group []string

	pks := target.PrimaryKeysSlice()
	for _, e := range edges {
		// The group's columns are the DECLARED target's keys, so an edge that
		// points elsewhere would be written as a target of that type and read
		// back as one. graph.Add resolves every edge to the declared target;
		// only a rebuilt snapshot holds one that does not.
		if e.Target().TypeID() != rel.TargetID() {
			return refusal.New(ErrUnrepresentable, "csv adapter: association %q targets type %s; the association declares the target %s",
				rel.Name(), e.Target().TypeID(), rel.TargetID())
		}
		// A short key would write empty segments for its missing components,
		// which read back as a partial foreign key; a long one has no column.
		if key := e.Target().PrimaryKey(); key.Len() != len(pks) {
			return refusal.New(ErrUnrepresentable, "csv adapter: association %q target key has %d components; type %s declares %d",
				rel.Name(), key.Len(), e.Target().TypeID(), len(pks))
		}
	}
	for i, pk := range pks {
		col := field + "._target_" + pk.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			segs[j] = escapeListElem(scalarCell(canonicalOrRaw(e.Target().PrimaryKey().Get(i).Unwrap(), pk.Constraint())), a.config.listSep)
		}
		cells[col] = strings.Join(segs, a.config.listSep)
	}

	for _, p := range rel.PropertiesSlice() {
		col := field + "." + p.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			if v, ok := e.Properties().Get(p.Name()); ok && !v.IsNil() {
				segs[j] = escapeListElem(scalarCell(canonicalOrRaw(v.Unwrap(), p.Constraint())), a.config.listSep)
			}
		}
		cells[col] = strings.Join(segs, a.config.listSep)
	}

	for _, col := range group {
		if cells[col] != "" {
			return nil
		}
	}
	return refusal.New(ErrUnrepresentable, "csv adapter: association %q: its target's key and every edge property render empty, so its columns would read back as no association", rel.Name())
}

// scalarCell renders one scalar value as cell text. A float carries a decimal
// point whatever constraint holds it, as the JSON and .ys writers mark one: a
// whole float under an Integer is written 5.0, which the Integer column
// refuses on the way back, rather than 5, which it would read as the integer
// the snapshot never held. A cell carries no type, so a String column reads
// the same float back as the text 5.0.
func scalarCell(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	}
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Float32:
		return floatCell(rv.Float(), 32)
	case reflect.Float64:
		return floatCell(rv.Float(), 64)
	}
	return fmt.Sprint(v)
}

// floatCell writes f in plain decimal notation at bitSize, with ".0" appended
// to a finite whole value so the cell reads as a float.
func floatCell(f float64, bitSize int) string {
	s := strconv.FormatFloat(f, 'f', -1, bitSize)
	if !math.IsNaN(f) && !math.IsInf(f, 0) && !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
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

// errListOfOneEmptyElement refuses a list whose one element renders as "": the
// list grammar writes [""] and [] alike, so the list would read back as an
// empty list, or as null where it is an optional property's whole value. It
// carries [ErrUnrepresentable] and keeps its own text, so a caller matches
// either.
var errListOfOneEmptyElement = refusal.New(ErrUnrepresentable, "a list holding one empty element writes what an empty list writes, so the element would not survive the round trip")

// valueToString renders a value as cell text in the form its constraint
// stores, null as "". A collection renders each element by this same rule and
// escapes it, which inverts the parser's recursion at every depth.
func (a *Adapter) valueToString(val immutable.Value, c schema.Constraint) (string, error) {
	if val.IsNil() {
		return "", nil
	}
	s, ok := val.Slice()
	if !ok {
		return scalarCell(canonicalOrRaw(val.Unwrap(), c)), nil
	}
	elem := constraintof.Element(c)
	parts := make([]string, s.Len())
	for i, v := range s.Iter2() {
		text, err := a.valueToString(v, elem)
		if err != nil {
			return "", fmt.Errorf("list element %d: %w", i, err)
		}
		parts[i] = escapeListElem(text, a.config.listSep)
	}
	if len(parts) == 1 && parts[0] == "" {
		return "", errListOfOneEmptyElement
	}
	return strings.Join(parts, a.config.listSep), nil
}

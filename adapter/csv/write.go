package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/adapter/internal/constraintof"
	"github.com/simon-lentz/yammm/adapter/internal/refusal"
	"github.com/simon-lentz/yammm/diag"
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
// it requests any writer: see [refuseComposedChildren]. Every other refusal
// under the package doc's Empty Cells is found at its instance, after that
// type's writer was requested: the rows before it are flushed, so that
// destination parses as far as it goes, and no later type's writer is
// requested.
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
				typeName, diag.ValueText(inst.PrimaryKey().String()), rels[0], children)
		}
	}
	return nil
}

// unnameableDenotedType reports a snapshot denoting a type the entry schema
// cannot name. No constructor of a snapshot builds one, so reaching this is a
// broken invariant. It carries no refusal class:
// [ErrUnrepresentable] names data a caller can act on, and this is not that.
func unnameableDenotedType(id schema.TypeID) error {
	return fmt.Errorf("csv adapter: snapshot denotes type %s, which the entry schema cannot name, so per-type output has no name for it; no constructor builds such a snapshot, so an invariant is broken", id)
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
			return fmt.Errorf("instance %s: %w", diag.ValueText(inst.PrimaryKey().String()), err)
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
		// Every constructor holds an edge to an association the type declares,
		// so every edge has its group of columns.
		for rel := range schemaType.AllAssociations() {
			if err := a.relationCells(rel, s, edgesByRel[rel.Name()], cells); err != nil {
				return nil, err
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
	if len(edges) == 0 {
		return nil
	}
	target, ok := s.TypeByID(rel.TargetID())
	if !ok {
		// buildColumnList already refused this shape; nothing to render.
		return nil
	}
	// Every constructor holds an edge to its association's declared target,
	// and a (one) association to one record.
	field := rel.FieldName()
	var group []string

	pks := target.PrimaryKeysSlice()
	for i, pk := range pks {
		col := field + "._target_" + pk.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			raw := e.Target().PrimaryKey().Get(i).Unwrap()
			text, err := cellOf(raw, pk.Constraint())
			if err != nil {
				return fmt.Errorf("csv adapter: association %q: target key component %d, of type %T: %w", rel.Name(), i, raw, err)
			}
			segs[j] = escapeListElem(text, a.config.listSep)
		}
		cells[col] = strings.Join(segs, a.config.listSep)
	}

	for _, p := range rel.PropertiesSlice() {
		col := field + "." + p.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			if v, ok := e.Properties().Get(p.Name()); ok && !v.IsNil() {
				text, err := cellOf(v.Unwrap(), p.Constraint())
				if err != nil {
					return fmt.Errorf("csv adapter: association %q: edge property %q, of type %T: %w", rel.Name(), p.Name(), v.Unwrap(), err)
				}
				segs[j] = escapeListElem(text, a.config.listSep)
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

// cellText renders one scalar value as [scalarCell] renders it, or returns the
// refusal it owes: [errNoCellSpelling] for a value no cell spells,
// [errInvalidUTF8] for text that is not valid UTF-8, and [errUnreadNumber] for
// a float or a json.Number whose cell [floatFromCell] does not read. A Float
// column reads every number an Integer column reads, so a cell it refuses
// reads back as a number in no column.
func cellText(v any) (string, error) {
	text, unjudged, ok := scalarCell(v)
	if !ok {
		return "", errNoCellSpelling
	}
	if !utf8.ValidString(text) {
		return "", errInvalidUTF8
	}
	if unjudged {
		if _, err := floatFromCell(text); err != nil {
			return "", errUnreadNumber
		}
	}
	return text, nil
}

// scalarCell renders one value in a form [instance.ScalarOf] returns as cell
// text; every value reaches it from [cellOf], which reads it through ScalarOf
// first. A finite float
// carries a decimal point whatever constraint holds it, as the JSON and .ys
// writers mark one: a whole float under an Integer is written 5.0, which the
// Integer column refuses on the way back, rather than 5, which it would read
// as the integer the snapshot never held. A cell carries no type, so a String
// column reads the same float back as the text 5.0. unjudged reports a number whose text
// may be none the parser reads: a float, which can be a NaN or an infinity,
// and a json.Number, whose text is its cell; an integer's digits always read.
// ok is false for a value no cell spells: a map, an array, a struct or another
// value that is no scalar, which a bypass-built snapshot can hold.
func scalarCell(v any) (text string, unjudged, ok bool) {
	if v == nil {
		return "", false, true
	}
	switch x := v.(type) {
	case json.Number:
		return string(x), true, true
	case string:
		return x, false, true
	case bool:
		return strconv.FormatBool(x), false, true
	case int64:
		return strconv.FormatInt(x, 10), false, true
	case uint64:
		return strconv.FormatUint(x, 10), false, true
	case float32:
		return floatCell(float64(x), 32), true, true
	case float64:
		return floatCell(x, 64), true, true
	}
	return "", false, false
}

// floatCell writes f in plain decimal notation at bitSize, with ".0" appended
// to a finite whole value so the cell reads as a float. The parser reads a
// cell at 64 bits, so a float32 whose 32-bit text rounds back to another
// float32 there is written in its 64-bit form, which reads back as itself.
func floatCell(f float64, bitSize int) string {
	s := strconv.FormatFloat(f, 'f', -1, bitSize)
	if back, err := strconv.ParseFloat(s, 64); bitSize == 32 && err == nil && float32(back) != float32(f) {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	if !math.IsNaN(f) && !math.IsInf(f, 0) && !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// cellOf renders raw as cell text in the form its constraint stores, after
// reading it in the one form every writer writes ([instance.ScalarOf]), and
// renders that form when the constraint cannot: an export has no diagnostic
// channel, so one malformed cell must not fail the whole file. A value of no
// such form is [errNoCellSpelling]; every other refusal is [cellText]'s.
func cellOf(raw any, c schema.Constraint) (string, error) {
	held, ok := instance.ScalarOf(raw)
	if !ok {
		return "", errNoCellSpelling
	}
	if canonical, err := instance.CanonicalValue(held, c); err == nil {
		held = canonical
	}
	return cellText(held)
}

// errNullListElement refuses a null list element: the list grammar writes it
// as an empty element, which reads back as a value the list never held — ""
// for a List<String>, an empty list for a List<List<T>> — or is refused. It carries [ErrUnrepresentable] and keeps its own text.
var errNullListElement = refusal.New(ErrUnrepresentable, "a null list element has no spelling in a cell, so the element would not survive the round trip")

// errNoCellSpelling refuses a value the list grammar has no spelling for: one
// no column renders, such as a map, an array or a struct, a pointer to one, or
// a time or a UUID its column does not render. It carries [ErrUnrepresentable] and
// keeps its own text.
var errNoCellSpelling = refusal.New(ErrUnrepresentable, "a value no column renders, such as a map, an array, a struct, or a time or a UUID its column does not render, has no spelling in a cell, so the value would not survive the round trip")

// errInvalidUTF8 refuses a string that is not valid UTF-8: a cell would carry
// its bytes, and the validator refuses the row that reads them back. It carries
// [ErrUnrepresentable] and keeps its own text.
var errInvalidUTF8 = refusal.New(ErrUnrepresentable, "a string that is not valid UTF-8 reads back as a value the validator refuses, so the value would not survive the round trip")

// errUnreadNumber refuses a number whose cell no column of this adapter's
// parser reads as a number: a NaN, an infinity, and a json.Number that spells a
// number no finite float64 holds. It
// carries [ErrUnrepresentable] and keeps its own text.
var errUnreadNumber = refusal.New(ErrUnrepresentable, "a number whose cell this adapter reads back as a number in no column, so the value would not survive the round trip")

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
	if _, ok := val.Map(); ok {
		return "", fmt.Errorf("a map: %w", errNoCellSpelling)
	}
	s, ok := val.Slice()
	if !ok {
		raw := val.Unwrap()
		text, err := cellOf(raw, c)
		if err != nil {
			return "", fmt.Errorf("a value of type %T: %w", raw, err)
		}
		return text, nil
	}
	elem := constraintof.Element(c)
	parts := make([]string, s.Len())
	for i, v := range s.Iter2() {
		// IsNil reads a nil container; the one form reads a nil behind a
		// pointer.
		if held, ok := instance.ScalarOf(v.Unwrap()); v.IsNil() || (ok && held == nil) {
			return "", fmt.Errorf("list element %d: %w", i, errNullListElement)
		}
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

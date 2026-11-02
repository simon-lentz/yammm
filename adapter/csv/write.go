package csv

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

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
// Returns [ErrZeroSnapshot] if result is zero, refuses a setting the adapter
// cannot use with [ErrConfig], and refuses a snapshot holding a composed child:
// see [refuseComposedChildren].
func (a *Adapter) MarshalSnapshot(
	ctx context.Context,
	result graph.Snapshot,
) (map[string][]byte, error) {
	if result.IsZero() {
		return nil, ErrZeroSnapshot
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
		instances := slices.Collect(result.InstancesOf(typeID))

		data, err := a.snapshotTypeCSV(ctx, instances, result, schemaType)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}
		output[typeName] = data
	}

	return output, nil
}

// refuseComposedChildren returns an error naming the first instance, in type
// and instance order, that holds a composed child. A CSV row is flat, so the
// writer has no column for a child and would drop the subtree. It runs before
// any output is produced, so a refused export writes nothing. A composition
// with no children loses nothing and is not refused.
func refuseComposedChildren(snap graph.Snapshot) error {
	for _, typeID := range snap.Types() {
		for inst := range snap.InstancesOf(typeID) {
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

// snapshotTypeCSV renders graph.Instance values from a snapshot as CSV rows. It
// uses snap.EdgesFrom for edge resolution instead of ValidInstance edge data.
func (a *Adapter) snapshotTypeCSV(
	ctx context.Context,
	instances []graph.Instance,
	snap graph.Snapshot,
	schemaType schema.Type,
) ([]byte, error) {
	columns, err := buildColumnList(schemaType, snap.Schema())
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.Comma = a.config.delimiter

	writer.Write(columns) //nolint:errcheck,gosec // a bytes.Buffer never fails, and configError judged the delimiter

	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("csv write: %w", err)
		}

		row, err := a.instanceToRow(inst.Properties(), snapshotEdges(inst, snap), columns, schemaType, snap.Schema())
		if err != nil {
			return nil, fmt.Errorf("instance %s: %w", diag.ValueText(inst.PrimaryKey().String()), err)
		}
		// csv.Writer writes a row whose only field is empty as a blank line, which
		// csv.Reader skips, so the instance would vanish on the way back. A quoted
		// empty field reads back as one empty field.
		if len(row) == 1 && row[0] == "" {
			writer.Flush()
			buf.WriteString(`""` + "\n")
			continue
		}
		writer.Write(row) //nolint:errcheck,gosec // a bytes.Buffer never fails, and configError judged the delimiter
	}

	writer.Flush()
	return buf.Bytes(), nil
}

// snapshotEdges groups an instance's outgoing edges by relation name.
// EdgesFrom is O(1) per instance and returns sorted edges, so each group
// keeps the (targetType, targetKey) order.
func snapshotEdges(inst graph.Instance, snap graph.Snapshot) map[string][]graph.Edge {
	var byRel map[string][]graph.Edge
	for e := range snap.EdgesFrom(inst) {
		if byRel == nil {
			byRel = make(map[string][]graph.Edge)
		}
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
// name. A load refuses a schema whose association target does not resolve, so no snapshot reaches that arm and it
// carries no refusal class.
func buildColumnList(schemaType schema.Type, s schema.Schema) ([]string, error) {
	// Collect property names.
	var propNames []string
	for prop := range schemaType.AllProperties() {
		propNames = append(propNames, prop.Name())
	}
	slices.Sort(propNames)

	rels := slices.SortedFunc(schemaType.AllAssociations(), func(a, b schema.Relation) int {
		return strings.Compare(a.FieldName(), b.FieldName())
	})
	var edgeCols []string
	for _, rel := range rels {
		target, ok := s.TypeByID(rel.TargetID())
		if !ok {
			return nil, fmt.Errorf("csv adapter: association %q: target type %s does not resolve, so its _target_ column names are unknowable; every schema constructor refuses such a schema, so this is an invariant violation",
				rel.Name(), rel.TargetID())
		}
		for pk := range target.PrimaryKeys() {
			edgeCols = append(edgeCols, rel.FieldName()+"._target_"+pk.Name())
		}
		props := slices.Collect(rel.Properties())
		slices.SortFunc(props, func(a, b schema.Property) int {
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
	edgesByRel map[string][]graph.Edge,
	columns []string,
	schemaType schema.Type,
	s schema.Schema,
) ([]string, error) {
	cells := make(map[string]string)
	// Every constructor holds an edge to an association the type declares,
	// so every edge has its group of columns.
	for rel := range schemaType.AllAssociations() {
		if err := a.relationCells(rel, s, edgesByRel[rel.Name()], cells); err != nil {
			return nil, err
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
func (a *Adapter) relationCells(rel schema.Relation, s schema.Schema, edges []graph.Edge, cells map[string]string) error {
	if len(edges) == 0 {
		return nil
	}
	// buildColumnList resolved this target. Every constructor holds an edge to its
	// association's declared target, and a (one) association to one record.
	target, _ := s.TypeByID(rel.TargetID())
	field := rel.FieldName()
	var group []string

	pks := slices.Collect(target.PrimaryKeys())
	for i, pk := range pks {
		col := field + "._target_" + pk.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			text, _ := cellOf(e.Target().PrimaryKey().Get(i).Unwrap(), pk.Constraint()) //nolint:errcheck // a load refuses a key component that has no cell
			segs[j] = escapeListElem(text)
		}
		cells[col] = strings.Join(segs, listSeparator)
	}

	for p := range rel.Properties() {
		col := field + "." + p.Name()
		group = append(group, col)
		segs := make([]string, len(edges))
		for j, e := range edges {
			if v, ok := e.Properties().Get(p.Name()); ok {
				text, err := cellOf(v.Unwrap(), p.Constraint())
				if err != nil {
					return fmt.Errorf("csv adapter: association %q: edge property %q, of type %T: %w", rel.Name(), p.Name(), v.Unwrap(), err)
				}
				segs[j] = escapeListElem(text)
			}
		}
		cells[col] = strings.Join(segs, listSeparator)
	}

	for _, col := range group {
		if cells[col] != "" {
			return nil
		}
	}
	return refusal.New(ErrUnrepresentable, "csv adapter: association %q: its target's key and every edge property render empty, so its columns would read back as no association", rel.Name())
}

// floatCell writes f in plain decimal notation, with ".0" appended to a whole
// value so the cell reads as a float, as the JSON and .ys writers mark one: a
// whole float under an Integer is written 5.0, which the Integer column refuses on
// the way back, rather than 5, which it would read as the integer the snapshot
// never held. A cell carries no type, so a String column reads the same float
// back as the text 5.0.
func floatCell(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// cellOf renders raw as cell text in the form its constraint stores, and renders
// it as it stands when the constraint cannot: an export has no diagnostic
// channel, so one malformed cell must not fail the whole file. A list or a map is
// [errNoCellSpelling]. A json.Number whose cell [floatFromCell] does not read is
// [errUnreadNumber]: a Float column reads every number an Integer column reads,
// so a cell it refuses reads back as a number in no column.
func cellOf(raw any, c schema.Constraint) (string, error) {
	held := raw
	if canonical, err := instance.CanonicalValue(raw, c); err == nil {
		held = canonical
	}
	switch x := held.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return floatCell(x), nil
	case json.Number:
		if _, err := floatFromCell(string(x)); err != nil {
			return "", errUnreadNumber
		}
		return string(x), nil
	}
	return "", errNoCellSpelling
}

// errNullListElement refuses a null list element: the list grammar writes it
// as an empty element, which reads back as a value the list never held — ""
// for a List<String>, an empty list for a List<List<T>> — or is refused. It carries [ErrUnrepresentable] and keeps its own text.
var errNullListElement = refusal.New(ErrUnrepresentable, "a null list element has no spelling in a cell, so the element would not survive the round trip")

// errNoCellSpelling refuses a value the list grammar has no spelling for: a map
// anywhere, or a list at an edge property. [Adapter.valueToString] writes a list
// at a property as its list cell, whatever the constraint. It carries
// [ErrUnrepresentable] and keeps its own text.
var errNoCellSpelling = refusal.New(ErrUnrepresentable, "a map, or a list at an edge property, has no spelling in a cell, so the value would not survive the round trip")

// errUnreadNumber refuses a number whose cell no column of this adapter's parser
// reads as a number: a json.Number that spells a number no finite float64 holds.
// It carries [ErrUnrepresentable] and keeps its own text.
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
		if v.IsNil() {
			return "", fmt.Errorf("list element %d: %w", i, errNullListElement)
		}
		text, err := a.valueToString(v, elem)
		if err != nil {
			return "", fmt.Errorf("list element %d: %w", i, err)
		}
		parts[i] = escapeListElem(text)
	}
	if len(parts) == 1 && parts[0] == "" {
		return "", errListOfOneEmptyElement
	}
	return strings.Join(parts, listSeparator), nil
}

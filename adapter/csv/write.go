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

// WriteOption configures per-call CSV serialization behavior.
type WriteOption func(*writeConfig)

type writeConfig struct {
	includeHeader bool
	nullString    string
}

func defaultWriteConfig() writeConfig {
	return writeConfig{
		includeHeader: true,
		nullString:    "",
	}
}

// MarshalSnapshot serializes a graph snapshot to CSV, returning one byte slice
// per type. CSV is inherently single-type-per-file, so the output is a map
// from type name to CSV bytes.
//
// Returns [ErrNilSnapshot] if result is nil. When two types in the snapshot
// render the same output name, returns an error naming both identities — a
// name-keyed map cannot separate them.
func (a *Adapter) MarshalSnapshot(
	ctx context.Context,
	result *graph.Snapshot,
	opts ...WriteOption,
) (map[string][]byte, error) {
	if result == nil {
		return nil, ErrNilSnapshot
	}
	if err := renderedNameCollision(result); err != nil {
		return nil, err
	}

	output := make(map[string][]byte, len(result.Types()))

	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("csv marshal snapshot: %w", err)
		}

		typeName := schema.TagForm(result.Schema(), typeID)
		schemaType, _ := result.Schema().TypeByID(typeID)
		instances := result.InstancesOf(typeID)

		var buf bytes.Buffer
		if err := a.writeSnapshotTypeTo(ctx, &buf, instances, result, schemaType, opts...); err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}
		output[typeName] = buf.Bytes()
	}

	return output, nil
}

// WriteSnapshot writes a graph snapshot to per-type writers. The writerFor
// function is called once per type to obtain the destination writer.
//
// Returns [ErrNilSnapshot] if result is nil. When two types in the snapshot
// render the same output name, returns an error naming both identities before
// any writer is requested — two writers obtained under one name would target
// one destination.
func (a *Adapter) WriteSnapshot(
	ctx context.Context,
	writerFor func(typeName string) (io.Writer, error),
	result *graph.Snapshot,
	opts ...WriteOption,
) error {
	if result == nil {
		return ErrNilSnapshot
	}
	if err := renderedNameCollision(result); err != nil {
		return err
	}

	for _, typeID := range result.Types() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("csv write snapshot: %w", err)
		}

		typeName := schema.TagForm(result.Schema(), typeID)
		w, err := writerFor(typeName)
		if err != nil {
			return fmt.Errorf("writer for type %q: %w", typeName, err)
		}

		schemaType, _ := result.Schema().TypeByID(typeID)
		instances := result.InstancesOf(typeID)

		if err := a.writeSnapshotTypeTo(ctx, w, instances, result, schemaType, opts...); err != nil {
			return fmt.Errorf("type %q: %w", typeName, err)
		}
	}

	return nil
}

// renderedNameCollision reports an error when two type identities in the
// snapshot render one output name. The rendering is lossy where the snapshot
// is not, so the writer refuses rather than silently merging the pair.
func renderedNameCollision(snap *graph.Snapshot) error {
	s := snap.Schema()
	seen := make(map[string]schema.TypeID)
	for _, id := range snap.Types() {
		name := schema.TagForm(s, id)
		if first, ok := seen[name]; ok {
			return fmt.Errorf("csv adapter: type %s and type %s both render output name %q, so per-type CSV output cannot separate them",
				first, id, name)
		}
		seen[name] = id
	}
	return nil
}

// writeSnapshotTypeTo writes graph.Instance values from a snapshot as CSV rows.
// It uses snap.EdgesFrom for edge resolution instead of ValidInstance edge data.
func (a *Adapter) writeSnapshotTypeTo(
	ctx context.Context,
	w io.Writer,
	instances []*graph.Instance,
	snap *graph.Snapshot,
	schemaType *schema.Type,
	opts ...WriteOption,
) error {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	columns, err := buildColumnList(schemaType, snap.Schema())
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	writer.Comma = a.config.delimiter

	if cfg.includeHeader {
		if err := writer.Write(columns); err != nil {
			return fmt.Errorf("csv write header: %w", err)
		}
	}

	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			writer.Flush()
			return fmt.Errorf("csv write: %w", err)
		}

		row := a.instanceToRow(inst.Properties(), snapshotEdges(inst, snap), columns, schemaType, snap.Schema(), &cfg)
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("csv write row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv flush: %w", err)
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

	rels := slices.SortedFunc(schemaType.Associations(), func(a, b *schema.Relation) int {
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
// the parser reads as absent, never null.
func (a *Adapter) instanceToRow(
	props immutable.Properties,
	edgesByRel map[string][]*graph.Edge,
	columns []string,
	schemaType *schema.Type,
	s *schema.Schema,
	cfg *writeConfig,
) []string {
	cells := make(map[string]string)
	if schemaType != nil {
		for rel := range schemaType.Associations() {
			a.relationCells(rel, s, edgesByRel[rel.Name()], cells)
		}
	}

	row := make([]string, len(columns))
	for i, col := range columns {
		if val, ok := props.Get(col); ok {
			row[i] = a.valueToString(val, propertyConstraint(schemaType, col), cfg)
			continue
		}
		if cell, ok := cells[col]; ok {
			row[i] = cell
			continue
		}

		// Column not found: null.
		row[i] = cfg.nullString
	}

	return row
}

// relationCells renders one association's edge columns into cells: per FK
// component and per edge property, one segment per target, escaped and
// joined on the list separator. No edges means every cell stays "", the
// absent-group marker.
func (a *Adapter) relationCells(rel *schema.Relation, s *schema.Schema, edges []*graph.Edge, cells map[string]string) {
	target, ok := s.TypeByID(rel.TargetID())
	if !ok {
		// buildColumnList already refused this shape; nothing to render.
		return
	}
	field := rel.FieldName()

	for i, pk := range target.PrimaryKeysSlice() {
		col := field + "._target_" + pk.Name()
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
// its constraint stores.
func (a *Adapter) valueToString(
	val immutable.Value,
	c schema.Constraint,
	cfg *writeConfig,
) string {
	if val.IsNil() {
		return cfg.nullString
	}

	// A collection renders elementwise, so the element constraint does the
	// work rather than the whole value.
	if s, ok := val.Slice(); ok {
		return a.sliceToString(s, elementConstraint(c), cfg)
	}

	if v := canonicalOrRaw(val.Unwrap(), c); v != nil {
		return scalarCell(v)
	}
	return cfg.nullString
}

// sliceToString renders an immutable.Slice as a list-separated string. Each
// element renders under elem, so a List<Timestamp> canonicalizes at its
// elements and not only at its outer level. Elements escape through the
// shared helper, so a value containing the separator survives the split.
func (a *Adapter) sliceToString(s immutable.Slice, elem schema.Constraint, cfg *writeConfig) string {
	parts := make([]string, s.Len())
	for i, v := range s.Iter2() {
		if v.IsNil() {
			parts[i] = cfg.nullString
		} else {
			parts[i] = escapeListElem(fmt.Sprint(canonicalOrRaw(v.Unwrap(), elem)), a.config.listSep)
		}
	}
	return strings.Join(parts, a.config.listSep)
}

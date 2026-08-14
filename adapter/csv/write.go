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

// WithWriteHeader controls whether the output includes a header row.
// Default is true.
func WithWriteHeader(include bool) WriteOption {
	return func(c *writeConfig) {
		c.includeHeader = include
	}
}

// WithWriteNullString sets the string written for nil property values.
// Default is the empty string "".
func WithWriteNullString(s string) WriteOption {
	return func(c *writeConfig) {
		c.nullString = s
	}
}

// MarshalTyped serializes validated instances of a single type to CSV bytes.
//
// Column order is determined by [schema.Type.AllPropertiesSlice] (sorted
// alphabetically). FK columns from association relations are appended after
// properties.
//
// Compositions are silently omitted (CSV is a flat format).
func (a *Adapter) MarshalTyped(
	ctx context.Context,
	instances []*instance.ValidInstance,
	schemaType *schema.Type,
	opts ...WriteOption,
) ([]byte, error) {
	var buf bytes.Buffer
	_, err := a.writeTypedTo(ctx, &buf, instances, schemaType, opts...)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteTyped serializes validated instances of a single type to an [io.Writer].
//
// Returns the number of bytes written.
func (a *Adapter) WriteTyped(
	ctx context.Context,
	w io.Writer,
	instances []*instance.ValidInstance,
	schemaType *schema.Type,
	opts ...WriteOption,
) (int64, error) {
	return a.writeTypedTo(ctx, w, instances, schemaType, opts...)
}

// MarshalSnapshot serializes a graph snapshot to CSV, returning one byte slice
// per type. CSV is inherently single-type-per-file, so the output is a map
// from type name to CSV bytes.
//
// Returns [ErrNilSnapshot] if result is nil.
func (a *Adapter) MarshalSnapshot(
	ctx context.Context,
	result *graph.Snapshot,
	opts ...WriteOption,
) (map[string][]byte, error) {
	if result == nil {
		return nil, ErrNilSnapshot
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
// Returns [ErrNilSnapshot] if result is nil.
func (a *Adapter) WriteSnapshot(
	ctx context.Context,
	writerFor func(typeName string) (io.Writer, error),
	result *graph.Snapshot,
	opts ...WriteOption,
) error {
	if result == nil {
		return ErrNilSnapshot
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

// fkLookup resolves the foreign key targets for a given relation.
// Returns the target keys for the relation, or nil if none exist.
type fkLookup func(rel *schema.Relation) []immutable.Key

// writeTypedTo is the shared implementation for MarshalTyped and WriteTyped.
// It uses ValidInstance edge data for FK resolution.
func (a *Adapter) writeTypedTo(
	ctx context.Context,
	w io.Writer,
	instances []*instance.ValidInstance,
	schemaType *schema.Type,
	opts ...WriteOption,
) (int64, error) {
	cfg := defaultWriteConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	columns := buildColumnList(schemaType)

	cw := &countWriter{w: w}
	writer := csv.NewWriter(cw)
	writer.Comma = a.config.delimiter

	if cfg.includeHeader {
		if err := writer.Write(columns); err != nil {
			return cw.n, fmt.Errorf("csv write header: %w", err)
		}
	}

	for _, inst := range instances {
		if err := ctx.Err(); err != nil {
			writer.Flush()
			return cw.n, fmt.Errorf("csv write: %w", err)
		}

		// Build FK lookup from ValidInstance edge data.
		lookup := validInstanceFKLookup(inst)
		row := a.instanceToRow(inst.Properties(), lookup, columns, schemaType, &cfg)
		if err := writer.Write(row); err != nil {
			return cw.n, fmt.Errorf("csv write row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return cw.n, fmt.Errorf("csv flush: %w", err)
	}
	return cw.n, nil
}

// writeSnapshotTypeTo writes graph.Instance values from a snapshot as CSV rows.
// It uses snap.EdgesFrom for FK resolution instead of ValidInstance edge data.
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

	columns := buildColumnList(schemaType)

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

		// Build FK lookup from snapshot edge index.
		lookup := snapshotFKLookup(inst, snap)
		row := a.instanceToRow(inst.Properties(), lookup, columns, schemaType, &cfg)
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

// validInstanceFKLookup returns an fkLookup that resolves FKs from ValidInstance edge data.
func validInstanceFKLookup(inst *instance.ValidInstance) fkLookup {
	return func(rel *schema.Relation) []immutable.Key {
		edgeData, ok := inst.Edge(rel.Name())
		if !ok || edgeData.IsEmpty() {
			return nil
		}
		targets := edgeData.Targets()
		keys := make([]immutable.Key, len(targets))
		for i := range targets {
			keys[i] = targets[i].TargetKey()
		}
		return keys
	}
}

// snapshotFKLookup returns an fkLookup that resolves FKs from snapshot edge data.
// Edges are grouped by relation name on first access for efficiency.
func snapshotFKLookup(inst *graph.Instance, snap *graph.Snapshot) fkLookup {
	// Build the relation-to-keys map eagerly; EdgesFrom is O(1) per instance.
	allEdges := snap.EdgesFrom(inst)
	byRel := make(map[string][]immutable.Key, len(allEdges))
	for _, e := range allEdges {
		byRel[e.Relation()] = append(byRel[e.Relation()], e.Target().PrimaryKey())
	}
	return func(rel *schema.Relation) []immutable.Key {
		return byRel[rel.Name()]
	}
}

// buildColumnList determines the CSV column order from schema metadata.
// Properties are sorted alphabetically, then FK relation field names are appended.
func buildColumnList(schemaType *schema.Type) []string {
	if schemaType == nil {
		return nil
	}

	// Collect property names.
	var propNames []string
	for _, prop := range schemaType.AllPropertiesSlice() {
		propNames = append(propNames, prop.Name())
	}
	slices.Sort(propNames)

	// Collect FK relation field names (associations only).
	var fkNames []string
	for rel := range schemaType.Associations() {
		fkNames = append(fkNames, rel.FieldName())
	}
	slices.Sort(fkNames)

	return append(propNames, fkNames...)
}

// instanceToRow converts properties and FK data to a CSV row aligned with columns.
// Properties are accessed through props; FK columns are resolved via the fkFn closure.
func (a *Adapter) instanceToRow(
	props immutable.Properties,
	fkFn fkLookup,
	columns []string,
	schemaType *schema.Type,
	cfg *writeConfig,
) []string {
	row := make([]string, len(columns))

	for i, col := range columns {
		// Check if this column is a property.
		val, ok := props.Get(col)
		if ok {
			row[i] = a.valueToString(val, cfg)
			continue
		}

		// Check if this column is an FK relation field name.
		if schemaType != nil && fkFn != nil {
			fkVal := a.formatFKColumn(fkFn, col, schemaType)
			if fkVal != "" {
				row[i] = fkVal
				continue
			}
		}

		// Column not found: null.
		row[i] = cfg.nullString
	}

	return row
}

// formatFKColumn formats the FK value for a column that matches an association field name.
func (a *Adapter) formatFKColumn(
	fkFn fkLookup,
	fieldName string,
	schemaType *schema.Type,
) string {
	for rel := range schemaType.Associations() {
		if rel.FieldName() != fieldName {
			continue
		}

		keys := fkFn(rel)
		if len(keys) == 0 {
			return ""
		}

		// Format each key.
		parts := make([]string, len(keys))
		for i, key := range keys {
			parts[i] = formatKey(key, a.config.listSep)
		}

		// Single target: return the formatted key directly.
		if len(parts) == 1 {
			return parts[0]
		}
		// Multiple targets (one-to-many): join with list separator.
		return strings.Join(parts, a.config.listSep)
	}
	return ""
}

// formatKey renders a single immutable.Key as a string.
// Single-component keys use the bare value; composite keys join with the separator.
func formatKey(key immutable.Key, sep string) string {
	if key.Len() == 1 {
		if v := key.Get(0).Unwrap(); v != nil {
			return fmt.Sprint(v)
		}
		return ""
	}
	parts := make([]string, key.Len())
	for i := range key.Len() {
		parts[i] = fmt.Sprint(key.Get(i).Unwrap())
	}
	return strings.Join(parts, sep)
}

// valueToString renders an immutable.Value as a CSV cell string.
func (a *Adapter) valueToString(
	val immutable.Value,
	cfg *writeConfig,
) string {
	if val.IsNil() {
		return cfg.nullString
	}

	raw := val.Unwrap()

	switch v := raw.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return cfg.nullString
	default:
		// Slices: join with list separator.
		if s, ok := val.Slice(); ok {
			return a.sliceToString(s, cfg)
		}
		return fmt.Sprint(v)
	}
}

// sliceToString renders an immutable.Slice as a list-separated string.
func (a *Adapter) sliceToString(s immutable.Slice, cfg *writeConfig) string {
	parts := make([]string, s.Len())
	for i, v := range s.Iter2() {
		if v.IsNil() {
			parts[i] = cfg.nullString
		} else {
			parts[i] = fmt.Sprint(v.Unwrap())
		}
	}
	return strings.Join(parts, a.config.listSep)
}

// countWriter wraps an io.Writer and counts bytes written.
type countWriter struct {
	w io.Writer
	n int64
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err //nolint:wrapcheck // callers add context; wrapping here causes double-prefix chains
}

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

// fkLookup resolves the foreign key targets for a given relation.
// Returns the target keys for the relation, or nil if none exist.
type fkLookup func(rel *schema.Relation) []immutable.Key

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

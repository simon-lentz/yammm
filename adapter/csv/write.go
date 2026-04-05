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

	for _, typeName := range result.Types() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("csv marshal snapshot: %w", err)
		}

		schemaType, _ := result.Schema().Type(typeName)
		instances := result.InstancesOf(typeName)

		valids := make([]*instance.ValidInstance, 0, len(instances))
		for _, inst := range instances {
			valids = append(valids, toValidInstanceView(inst))
		}

		data, err := a.MarshalTyped(ctx, valids, schemaType, opts...)
		if err != nil {
			return nil, fmt.Errorf("type %q: %w", typeName, err)
		}
		output[typeName] = data
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

	for _, typeName := range result.Types() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("csv write snapshot: %w", err)
		}

		w, err := writerFor(typeName)
		if err != nil {
			return fmt.Errorf("writer for type %q: %w", typeName, err)
		}

		schemaType, _ := result.Schema().Type(typeName)
		instances := result.InstancesOf(typeName)

		valids := make([]*instance.ValidInstance, 0, len(instances))
		for _, inst := range instances {
			valids = append(valids, toValidInstanceView(inst))
		}

		if _, err := a.writeTypedTo(ctx, w, valids, schemaType, opts...); err != nil {
			return fmt.Errorf("type %q: %w", typeName, err)
		}
	}

	return nil
}

// writeTypedTo is the shared implementation for MarshalTyped and WriteTyped.
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

		row := a.instanceToRow(inst, columns, schemaType, &cfg)
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

// instanceToRow converts a ValidInstance to a CSV row aligned with columns.
func (a *Adapter) instanceToRow(
	inst *instance.ValidInstance,
	columns []string,
	schemaType *schema.Type,
	cfg *writeConfig,
) []string {
	row := make([]string, len(columns))

	for i, col := range columns {
		// Check if this column is a property.
		val, ok := inst.Property(col)
		if ok {
			row[i] = a.valueToString(val, cfg)
			continue
		}

		// Check if this column is an FK relation field name.
		if schemaType != nil {
			fkVal := a.lookupFKValue(inst, col, schemaType)
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

// lookupFKValue attempts to find a single-key FK value for a relation field name.
func (a *Adapter) lookupFKValue(
	inst *instance.ValidInstance,
	fieldName string,
	schemaType *schema.Type,
) string {
	// Find the relation by field name.
	for rel := range schemaType.Associations() {
		if rel.FieldName() != fieldName {
			continue
		}

		edgeData, ok := inst.Edge(rel.Name())
		if !ok || edgeData.IsEmpty() {
			return ""
		}

		// For CSV, serialize the first target's key as a simple value.
		target := edgeData.Targets()[0]
		key := target.TargetKey()
		if key.Len() == 1 {
			if v := key.Get(0).Unwrap(); v != nil {
				return fmt.Sprint(v)
			}
		}
		// Composite key: join components with the list separator.
		parts := make([]string, key.Len())
		for i := range key.Len() {
			parts[i] = fmt.Sprint(key.Get(i).Unwrap())
		}
		return strings.Join(parts, a.config.listSep)
	}

	return ""
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

// toValidInstanceView creates a ValidInstance view from a graph.Instance.
// This is a lightweight adapter for MarshalSnapshot which receives
// graph.Instance values from the snapshot but needs ValidInstance for
// the shared write path.
func toValidInstanceView(inst *graph.Instance) *instance.ValidInstance {
	return instance.NewValidInstance(
		inst.TypeName(),
		inst.TypeID(),
		inst.PrimaryKey(),
		inst.Properties(),
		nil, // no edge data from graph.Instance (edges are stored externally)
		nil, // no composition data
		inst.Provenance(),
	)
}

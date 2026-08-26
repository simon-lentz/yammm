package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// ParseTyped parses CSV data where all rows belong to a single type.
//
// The schemaType parameter drives type coercion from strings to typed
// Go values. If schemaType is nil, all values are kept as strings
// (no coercion).
//
// The first row defines column names.
func (a *Adapter) ParseTyped(
	ctx context.Context,
	source location.SourceID, //nolint:revive // reserved for future provenance tracking
	typeName string,
	r io.Reader,
	schemaType *schema.Type,
) ([]instance.RawInstance, diag.Result) {
	collector := diag.NewCollector(0)
	reader := csv.NewReader(stripBOM(r))
	reader.Comma = a.config.delimiter
	reader.LazyQuotes = true

	columns, startRow, err := a.readHeader(reader)
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("csv parse: %s", err)).Build())
		return nil, collector.Result()
	}

	var results []instance.RawInstance
	row := startRow
	for {
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("csv parse cancelled at row %d", row)).Build())
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("row %d: %s", row, err)).
				WithDetail(diag.DetailKeyTypeName, typeName).Build())
			row++
			continue
		}

		props := a.recordToProps(record, columns, schemaType, row, typeName, collector)
		results = append(results, instance.RawInstance{Properties: props})
		row++
	}

	return results, collector.Result()
}

// ParseWithTypeColumn parses CSV data where a designated column contains
// the type name for each row.
//
// The typeResolver function looks up a [*schema.Type] by name for coercion.
// It may return nil for unknown types, in which case values are kept as strings.
//
// Requires [WithTypeColumn] to be set. Returns [ErrNoTypeColumn] otherwise.
func (a *Adapter) ParseWithTypeColumn(
	ctx context.Context,
	source location.SourceID, //nolint:revive // reserved for future provenance tracking
	r io.Reader,
	typeResolver func(string) *schema.Type,
) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollector(0)

	if a.config.typeColumn == "" {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			ErrNoTypeColumn.Error()).Build())
		return nil, collector.Result()
	}

	reader := csv.NewReader(stripBOM(r))
	reader.Comma = a.config.delimiter
	reader.LazyQuotes = true

	columns, startRow, err := a.readHeader(reader)
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("csv parse: %s", err)).Build())
		return nil, collector.Result()
	}

	// Find type column index.
	typeColIdx := -1
	for i, col := range columns {
		if col == a.config.typeColumn {
			typeColIdx = i
			break
		}
	}
	if typeColIdx == -1 {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("type column %q not found in header", a.config.typeColumn)).Build())
		return nil, collector.Result()
	}

	results := make(map[string][]instance.RawInstance)
	row := startRow
	for {
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("csv parse cancelled at row %d", row)).Build())
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("row %d: %s", row, err)).Build())
			row++
			continue
		}

		if typeColIdx >= len(record) {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("row %d: too few columns for type column", row)).Build())
			row++
			continue
		}

		typeName := record[typeColIdx]
		if typeName == "" {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("row %d: empty type column", row)).Build())
			row++
			continue
		}

		schemaType := typeResolver(typeName)

		// Build columns excluding the type column.
		filteredCols := make([]string, 0, len(columns)-1)
		filteredVals := make([]string, 0, len(record)-1)
		for i, col := range columns {
			if i == typeColIdx {
				continue
			}
			filteredCols = append(filteredCols, col)
			if i < len(record) {
				filteredVals = append(filteredVals, record[i])
			}
		}

		props := a.recordToProps(filteredVals, filteredCols, schemaType, row, typeName, collector)
		results[typeName] = append(results[typeName], instance.RawInstance{Properties: props})
		row++
	}

	return results, collector.Result()
}

// readHeader reads the header row (if configured) and returns column names
// and the starting row number for data rows.
func (a *Adapter) readHeader(reader *csv.Reader) (columns []string, startRow int, err error) {
	if a.config.hasHeader {
		header, err := reader.Read()
		if err != nil {
			return nil, 0, fmt.Errorf("reading header: %w", err)
		}
		return header, 2, nil // data starts at row 2 (1-indexed, header is row 1)
	}
	return nil, 1, nil // no header; columns will be generated from indices
}

// recordToProps converts a CSV record to a property map with type
// coercion. Dotted edge columns (<field>._target_<pk>, <field>.<prop>)
// classify before the null mapping — an empty cell there means an absent
// group, never a null property — and assemble into the _target_ objects
// the validator accepts.
func (a *Adapter) recordToProps(
	record []string,
	columns []string,
	schemaType *schema.Type,
	row int,
	typeName string,
	collector *diag.Collector,
) map[string]any {
	props := make(map[string]any, len(record))

	var assocByField map[string]*schema.Relation
	var groups map[string]map[string]string // field -> suffix -> raw cell
	if schemaType != nil {
		assocByField = make(map[string]*schema.Relation)
		for rel := range schemaType.AllAssociations() {
			assocByField[rel.FieldName()] = rel
		}
	}

	for i, val := range record {
		var colName string
		if i < len(columns) && columns != nil {
			colName = columns[i]
		} else {
			colName = strconv.Itoa(i)
		}

		// Edge columns first: their empty cell is the absent-group marker.
		if schemaType != nil {
			if field, suffix, dotted := strings.Cut(colName, "."); dotted {
				rel, isAssoc := assocByField[field]
				if !isAssoc {
					collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
						fmt.Sprintf("row %d: dotted column %q does not match an association field of %q", row, colName, typeName)).
						WithDetail(diag.DetailKeyTypeName, typeName).Build())
					continue
				}
				if !strings.HasPrefix(suffix, "_target_") {
					if _, ok := rel.Property(suffix); !ok {
						collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
							fmt.Sprintf("row %d: column %q names neither a _target_ component nor an edge property of %q", row, colName, rel.Name())).
							WithDetail(diag.DetailKeyTypeName, typeName).Build())
						continue
					}
				}
				if groups == nil {
					groups = make(map[string]map[string]string)
				}
				if groups[field] == nil {
					groups[field] = make(map[string]string)
				}
				groups[field][suffix] = val
				continue
			}
		}

		// Null check.
		if val == a.config.nullValue {
			props[colName] = nil
			continue
		}

		// Coerce if schema type available.
		if schemaType != nil {
			prop, found := schemaType.Property(colName)
			if found {
				coerced, err := a.coerceStringValue(val, prop.Constraint())
				if err != nil {
					collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
						fmt.Sprintf("row %d, column %q: %s", row, colName, err)).
						WithDetail(diag.DetailKeyTypeName, typeName).Build())
					props[colName] = val // keep raw string on coercion failure
					continue
				}
				props[colName] = coerced
				continue
			}
		}

		// No schema or unknown property: keep as string.
		props[colName] = val
	}

	for field, cells := range groups {
		a.assembleEdgeGroup(field, cells, assocByField[field], row, typeName, collector, props)
	}

	return props
}

// assembleEdgeGroup zips one relation's edge cells into the validator's
// shape: each cell splits into one segment per target on the escaped list
// separator, segment counts must agree, and an all-empty group means the
// edge is absent. An empty segment means the optional edge property is
// absent on that target. Edge properties are scalars by language rule,
// which is why zipping is well-founded.
func (a *Adapter) assembleEdgeGroup(
	field string,
	cells map[string]string,
	rel *schema.Relation,
	row int,
	typeName string,
	collector *diag.Collector,
	props map[string]any,
) {
	n := -1
	segsBySuffix := make(map[string][]string, len(cells))
	for suffix, cell := range cells {
		if cell == "" {
			continue
		}
		segs := splitListElems(cell, a.config.listSep)
		if n == -1 {
			n = len(segs)
		} else if len(segs) != n {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("row %d: association %q columns disagree on target count (%d vs %d)", row, field, n, len(segs))).
				WithDetail(diag.DetailKeyTypeName, typeName).Build())
			return
		}
		segsBySuffix[suffix] = segs
	}
	if n == -1 {
		return // every cell empty: the edge is absent
	}

	var targetType *schema.Type
	if a.config.schema != nil {
		targetType, _ = a.config.schema.TypeByID(rel.TargetID())
	}

	targets := make([]any, n)
	for t := range n {
		obj := make(map[string]any, len(segsBySuffix))
		for suffix, segs := range segsBySuffix {
			seg := segs[t]
			if seg == "" {
				continue // absent on this target
			}
			var c schema.Constraint
			if pkName, isFK := strings.CutPrefix(suffix, "_target_"); isFK {
				if targetType != nil {
					if pk, ok := targetType.Property(pkName); ok {
						c = pk.Constraint()
					}
				}
			} else if p, ok := rel.Property(suffix); ok {
				c = p.Constraint()
			}
			if c == nil {
				// No constraint reachable (no WithSchema, or an unknown
				// component): the string survives and the validator rules.
				obj[suffix] = seg
				continue
			}
			coerced, err := a.coerceStringValue(seg, c)
			if err != nil {
				collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
					fmt.Sprintf("row %d, column %q.%s: %s", row, field, suffix, err)).
					WithDetail(diag.DetailKeyTypeName, typeName).Build())
				obj[suffix] = seg
				continue
			}
			obj[suffix] = coerced
		}
		targets[t] = obj
	}

	if !rel.IsMany() && n == 1 {
		props[field] = targets[0]
		return
	}
	props[field] = targets
}

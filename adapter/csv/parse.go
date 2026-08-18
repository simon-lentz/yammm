package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

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
// When WithHeader(true) (default), the first row defines column names.
// When WithHeader(false), columns are named by index ("0", "1", ...).
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

// recordToProps converts a CSV record to a property map with type coercion.
func (a *Adapter) recordToProps(
	record []string,
	columns []string,
	schemaType *schema.Type,
	row int,
	typeName string,
	collector *diag.Collector,
) map[string]any {
	props := make(map[string]any, len(record))

	for i, val := range record {
		var colName string
		if i < len(columns) && columns != nil {
			colName = columns[i]
		} else {
			colName = strconv.Itoa(i)
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

	return props
}

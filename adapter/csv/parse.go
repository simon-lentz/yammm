package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// ParseTyped parses CSV data where all rows belong to a single type.
//
// The schemaType parameter drives type coercion from strings to typed
// Go values. If schemaType is nil, all values are kept as strings
// (no coercion).
//
// The first row defines column names.
//
// Every instance carries a [location.Provenance] naming source, its path in the
// document ($.Entity[0]) and the span of its record's first field, and every
// row diagnostic carries that span. A record's line comes from the reader, so a
// quoted newline moves it as the file reads.
func (a *Adapter) ParseTyped(
	ctx context.Context,
	source location.SourceID,
	typeName string,
	r io.Reader,
	schemaType *schema.Type,
) ([]instance.RawInstance, diag.Result) {
	collector := diag.NewCollector(0)
	reader := csv.NewReader(stripBOM(r))
	reader.Comma = a.config.delimiter
	reader.LazyQuotes = true

	columns, err := a.readHeader(reader)
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("csv parse: %s", err)).
			WithSpan(headerSpan(source)).Build())
		return nil, collector.Result()
	}

	var results []instance.RawInstance
	var lastSpan location.Span
	for {
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("csv parse cancelled after %d records", len(results))).
				WithSpan(lastSpan).Build())
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("csv parse: %s", err)).
				WithSpan(parseErrorSpan(source, err)).
				WithDetail(diag.DetailKeyTypeName, typeName).Build())
			continue
		}

		lastSpan = recordSpan(source, reader, record)
		props := a.recordToProps(record, columns, schemaType, lastSpan, typeName, collector)
		results = append(results, instance.RawInstance{
			Properties: props,
			Provenance: location.NewProvenance(
				source.String(),
				path.Root().Key(typeName).Index(len(results)),
				lastSpan,
			),
		})
	}

	return results, collector.Result()
}

// recordSpan returns the point span at the start of the record the reader last
// read.
//
// The column is 1, not the reader's: [csv.Reader.FieldPos] counts columns in
// BYTES and [location.Position] counts them in runes. A record starts at
// column 1 under both, so the two agree here and nowhere else — this adapter
// parses an [io.Reader] and never holds the line it would need to convert one.
//
// A successful read always records a first field, so FieldPos cannot panic on
// index 0; the guard keeps that true if the reader's contract ever widens.
func recordSpan(source location.SourceID, reader *csv.Reader, record []string) location.Span {
	if len(record) == 0 {
		return location.Span{}
	}
	line, _ := reader.FieldPos(0)
	return location.Point(source, line, 1)
}

// parseErrorSpan locates a record the reader refused, at the start of the line
// the fault is on.
//
// [csv.Reader.FieldPos] is not the route: a fault in the first field leaves no
// recorded field and FieldPos panics on the index. That class is unreachable
// while LazyQuotes is on — it suppresses both quote faults, leaving a wrong
// field count as the one refusal, which is raised after the whole record parsed
// — so this reads the error, which answers for every class instead of one.
//
// The error's own column is dropped for the reason [recordSpan] states: it is a
// byte index and [location.Position] counts runes. A reader error that is not a
// parse error carries no position at all.
func parseErrorSpan(source location.SourceID, err error) location.Span {
	var parseErr *csv.ParseError
	if !errors.As(err, &parseErr) || parseErr.Line <= 0 {
		return location.Span{}
	}
	return location.Point(source, parseErr.Line, 1)
}

// headerSpan returns the point span of the header row, which is line 1.
func headerSpan(source location.SourceID) location.Span {
	return location.Point(source, 1, 1)
}

// ParseWithTypeColumn parses CSV data where a designated column contains
// the type name for each row.
//
// The typeResolver function looks up a [*schema.Type] by name for coercion.
// It may return nil for unknown types, in which case values are kept as strings.
//
// Requires [WithTypeColumn] to be set. Returns [ErrNoTypeColumn] otherwise.
//
// Every instance carries a [location.Provenance] naming source, its path under
// its own type ($.Entity[0]) and the span of its record's first field, as
// [Adapter.ParseTyped] records them.
func (a *Adapter) ParseWithTypeColumn(
	ctx context.Context,
	source location.SourceID,
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

	columns, err := a.readHeader(reader)
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("csv parse: %s", err)).
			WithSpan(headerSpan(source)).Build())
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
			fmt.Sprintf("type column %q not found in header", a.config.typeColumn)).
			WithSpan(headerSpan(source)).Build())
		return nil, collector.Result()
	}

	results := make(map[string][]instance.RawInstance)
	parsed := 0
	var lastSpan location.Span
	for {
		if err := ctx.Err(); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_CONTEXT_CANCELLED,
				fmt.Sprintf("csv parse cancelled after %d records", parsed)).
				WithSpan(lastSpan).Build())
			break
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("csv parse: %s", err)).
				WithSpan(parseErrorSpan(source, err)).Build())
			continue
		}

		lastSpan = recordSpan(source, reader, record)
		parsed++

		if typeColIdx >= len(record) {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				"too few columns for type column").
				WithSpan(lastSpan).Build())
			continue
		}

		typeName := record[typeColIdx]
		if typeName == "" {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				"empty type column").
				WithSpan(lastSpan).Build())
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

		props := a.recordToProps(filteredVals, filteredCols, schemaType, lastSpan, typeName, collector)
		results[typeName] = append(results[typeName], instance.RawInstance{
			Properties: props,
			Provenance: location.NewProvenance(
				source.String(),
				path.Root().Key(typeName).Index(len(results[typeName])),
				lastSpan,
			),
		})
	}

	return results, collector.Result()
}

// readHeader reads the header row and returns its column names.
func (a *Adapter) readHeader(reader *csv.Reader) (columns []string, err error) {
	if a.config.hasHeader {
		header, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		return header, nil
	}
	return nil, nil
}

// recordToProps converts a CSV record to a property map with type
// coercion. Dotted edge columns (<field>."_target_"<pk>, <field>.<prop>)
// classify before the null mapping — an empty cell there means an absent
// group, never a null property — and assemble into the "_target_" objects
// the validator accepts.
func (a *Adapter) recordToProps(
	record []string,
	columns []string,
	schemaType *schema.Type,
	span location.Span,
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
						fmt.Sprintf("dotted column %q does not match an association field of %q", colName, typeName)).
						WithSpan(span).
						WithDetail(diag.DetailKeyTypeName, typeName).Build())
					continue
				}
				if !strings.HasPrefix(suffix, "_target_") {
					if _, ok := rel.Property(suffix); !ok {
						collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
							fmt.Sprintf("column %q names neither a _target_ component nor an edge property of %q", colName, rel.Name())).
							WithSpan(span).
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
						fmt.Sprintf("column %q: %s", colName, err)).
						WithSpan(span).
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
		a.assembleEdgeGroup(field, cells, assocByField[field], span, typeName, collector, props)
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
	span location.Span,
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
				fmt.Sprintf("association %q columns disagree on target count (%d vs %d)", field, n, len(segs))).
				WithSpan(span).
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
					fmt.Sprintf("column %q.%s: %s", field, suffix, err)).
					WithSpan(span).
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

package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/simon-lentz/yammm/adapter/internal/typetag"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// ParseTyped parses CSV data where all rows belong to schemaType, which drives
// coercion; with a nil schemaType every value is kept as a string. The header's
// names resolve as the package documentation's "Column Mapping" states, and a
// fault is handled as its "Header and Records" states. Every instance carries a
// [location.Provenance]: source, its path ($.Entity[0]) and its record's span.
func (a *Adapter) ParseTyped(
	ctx context.Context,
	source location.SourceID,
	typeName string,
	r io.Reader,
	schemaType *schema.Type,
) ([]instance.RawInstance, diag.Result) {
	collector := diag.NewCollector(0)
	if a.listSepRefused(collector) {
		return nil, collector.Result()
	}
	reader := a.newReader(r)

	columns, _, ok := a.readHeader(reader, source, collector)
	if !ok {
		return nil, collector.Result()
	}
	typePlan := a.planColumns(columns, schemaType)

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
			if a.reportReadError(err, source, typeName, len(results), collector) {
				break
			}
			continue
		}

		lastSpan = recordSpan(source, reader, record)
		props := a.recordToProps(record, typePlan, lastSpan, typeName, collector)
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

// listSepRefused reports a list separator [listSepError] refuses as an Error
// and whether it did. The refusal names the configuration, not the input, so it
// carries no span.
func (a *Adapter) listSepRefused(collector *diag.Collector) bool {
	err := listSepError(a.config.listSep)
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE, err.Error()).Build())
	}
	return err != nil
}

// newReader returns the reader both entry points parse through. LazyQuotes
// stays off, so a malformed quoted field is a diagnostic and not a value the
// file never stated.
func (a *Adapter) newReader(r io.Reader) *csv.Reader {
	reader := csv.NewReader(stripBOM(r))
	reader.Comma = a.config.delimiter
	return reader
}

// reportReadError reports a record's read error and whether the parse stops.
// [encoding/csv] keeps no sticky error, so every later Read calls a failing
// [io.Reader] again: its failure stops the parse, Fatal as the diag package
// documents an I/O failure.
func (a *Adapter) reportReadError(err error, source location.SourceID, typeName string, records int, collector *diag.Collector) (stop bool) {
	if a.readFailure(err) {
		issue := diag.NewIssue(diag.Fatal, E_CSV_COERCE,
			fmt.Sprintf("csv read failed after %d records: %s", records, err))
		if typeName != "" {
			issue.WithDetail(diag.DetailKeyTypeName, typeName)
		}
		collector.Collect(issue.Build())
		return true
	}
	issue := diag.NewIssue(diag.Error, E_CSV_COERCE, fmt.Sprintf("csv parse: %s", err)).
		WithSpan(parseErrorSpan(source, err))
	if typeName != "" {
		issue.WithDetail(diag.DetailKeyTypeName, typeName)
	}
	collector.Collect(issue.Build())
	return false
}

// readFailure reports whether err came from the [io.Reader] rather than from
// the input's end, a fault in the input, or a refused delimiter. The end is
// [io.EOF] itself, as both loops test it: an error that only wraps it is the
// reader failing, and treating it as the end of a record would loop.
func (a *Adapter) readFailure(err error) bool {
	if err == io.EOF { //nolint:errorlint // only the bare io.EOF ends the input; a wrapped one is a failure
		return false
	}
	if _, ok := errors.AsType[*csv.ParseError](err); ok { //nolint:errcheck // type check only, value unused
		return false
	}
	return !delimiterRefused(a.config.delimiter)
}

// delimiterRefused reports whether [encoding/csv] refuses delim, asked of the
// package itself so this adapter never restates its rule.
func delimiterRefused(delim rune) bool {
	probe := csv.NewReader(strings.NewReader(""))
	probe.Comma = delim
	_, err := probe.Read()
	return !errors.Is(err, io.EOF)
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

// parseErrorSpan locates a refused record at the start of its fault's line,
// from the error: a quote fault in a first field leaves no recorded field, so
// [csv.Reader.FieldPos] would panic. The error's byte column is dropped, as in
// [recordSpan].
func parseErrorSpan(source location.SourceID, err error) location.Span {
	parseErr, ok := errors.AsType[*csv.ParseError](err)
	if !ok || parseErr.Line <= 0 {
		return location.Span{}
	}
	return location.Point(source, parseErr.Line, 1)
}

// ParseWithTypeColumn parses CSV data where a designated column contains
// the type name for each row.
//
// The typeResolver function looks up a [*schema.Type] by name for coercion.
// It may return nil for unknown types, in which case values are kept as strings.
// A value that is not a type name by the grammar draws E_INVALID_TYPE_TAG and
// its row is skipped. Requires [WithTypeColumn] to be set. Returns
// [ErrNoTypeColumn] otherwise. Faults and provenance are as [Adapter.ParseTyped].
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
	if a.listSepRefused(collector) {
		return nil, collector.Result()
	}

	reader := a.newReader(r)

	columns, header, ok := a.readHeader(reader, source, collector)
	if !ok {
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
			WithSpan(header).Build())
		return nil, collector.Result()
	}

	// The header fixes the other columns and each row type's plan over them.
	filteredCols := make([]string, 0, len(columns)-1)
	for i, col := range columns {
		if i != typeColIdx {
			filteredCols = append(filteredCols, col)
		}
	}
	plans := make(map[*schema.Type]*plan)

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
			if a.reportReadError(err, source, "", parsed, collector) {
				break
			}
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
		if err := typetag.Validate(typeName); err != nil {
			collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_TYPE_TAG,
				fmt.Sprintf("invalid type name %q in type column %q: %s", typeName, a.config.typeColumn, err)).
				WithSpan(lastSpan).
				WithDetail(diag.DetailKeyGot, typeName).
				WithDetail(diag.DetailKeyDetail, err.Error()).Build())
			continue
		}

		schemaType := typeResolver(typeName)
		typePlan, planned := plans[schemaType]
		if !planned {
			typePlan = a.planColumns(filteredCols, schemaType)
			plans[schemaType] = typePlan
		}

		filteredVals := make([]string, 0, len(record)-1)
		for i := range columns {
			if i == typeColIdx {
				continue
			}
			if i < len(record) {
				filteredVals = append(filteredVals, record[i])
			}
		}

		props := a.recordToProps(filteredVals, typePlan, lastSpan, typeName, collector)
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

// readHeader reads the header row and its line, refusing a column named twice
// or not at all. The reader fixes FieldsPerRecord from this row, so a later
// record of another length comes back with [csv.ErrFieldCount].
func (a *Adapter) readHeader(reader *csv.Reader, source location.SourceID, collector *diag.Collector) ([]string, location.Span, bool) {
	header, err := reader.Read()
	if err != nil {
		severity := diag.Error
		if a.readFailure(err) {
			severity = diag.Fatal
		}
		span := parseErrorSpan(source, err)
		if span.IsZero() {
			span = location.Point(source, 1, 1)
		}
		collector.Collect(diag.NewIssue(severity, E_CSV_COERCE,
			fmt.Sprintf("csv parse: reading header: %s", err)).
			WithSpan(span).Build())
		return nil, location.Span{}, false
	}

	// Blank lines before the header are skipped, so its line is the reader's.
	span := recordSpan(source, reader, header)
	ok := true
	seen := make(map[string]int, len(header))
	for i, name := range header {
		seen[name]++
		switch {
		case name == "":
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("header column %d has no name", i+1)).
				WithSpan(span).Build())
			ok = false
		case seen[name] == 2:
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("header names column %q more than once", name)).
				WithSpan(span).Build())
			ok = false
		}
	}
	if !ok {
		return nil, location.Span{}, false
	}
	return header, span, true
}

// recordToProps converts a CSV record to a property map, deciding each member's
// claim over the keys this row holds, as the package documentation's "Column
// Mapping" and "Empty Cells" state. The record is never longer than the plan:
// see [Adapter.readHeader].
func (a *Adapter) recordToProps(
	record []string,
	p *plan,
	span location.Span,
	typeName string,
	collector *diag.Collector,
) map[string]any {
	props := make(map[string]any, len(record))

	// What each undotted column's cell becomes in this row.
	resolved := make([]bool, len(record))
	passed := make([]bool, len(record))
	for _, c := range p.props {
		r, pass := c.resolve(func(i int) bool { return i == c.exact || record[i] != "" })
		if r >= 0 {
			resolved[r] = true
		}
		for _, i := range pass {
			passed[i] = true
		}
	}
	// Which spelling of each association this row's object carries: one whose
	// group writes an object, which is the key the validator sees.
	shapes := make([]groupShape, len(p.spellings))
	for s := range p.spellings {
		shapes[s] = a.shapeOf(p, &p.spellings[s], record)
	}
	spellingState := make([]claimState, len(p.spellings))
	for _, c := range p.relClaims {
		r, pass := c.resolve(func(s int) bool { return shapes[s].writes() })
		if r >= 0 {
			spellingState[r] = claimResolved // a group that writes nothing assembles to nothing
		}
		for _, s := range pass {
			spellingState[s] = claimPassed
		}
	}
	for s, sp := range p.spellings {
		if sp.rel == nil {
			spellingState[s] = claimPassed // names no association: the validator reports it
		}
	}

	for i, val := range record {
		col := &p.columns[i]
		switch col.kind {
		case columnString:
			props[col.name] = val

		case columnPlain:
			switch {
			case resolved[i]:
				a.writeProperty(props, col, val, span, typeName, collector)
			case val != "" && (passed[i] || col.prop == nil):
				props[col.name] = val // the validator reports the unknown or ambiguous field
			}

		}
	}

	for s := range p.spellings {
		if shapes[s].clash != "" {
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE, shapes[s].clash).
				WithSpan(span).
				WithDetail(diag.DetailKeyTypeName, typeName).Build())
		}
		if spellingState[s] == claimNone || !shapes[s].writes() {
			continue
		}
		if _, taken := props[p.spellings[s].field]; taken {
			// Two values for one key, as a repeated JSON member; the group is
			// the field's only valid form, so it is kept.
			collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
				fmt.Sprintf("column %[1]q and the dotted columns %[1]s.* both write the key %[1]q", p.spellings[s].field)).
				WithSpan(span).
				WithDetail(diag.DetailKeyTypeName, typeName).Build())
		}
		a.assembleEdgeGroup(p, &p.spellings[s], spellingState[s] == claimResolved, shapes[s], span, typeName, collector, props)
	}
	return props
}

// claimState is what one row does with a name: nothing, resolve it to its
// member, or pass it on for the validator to report.
type claimState uint8

const (
	claimNone claimState = iota
	claimResolved
	claimPassed
)

// groupShape is what one row's cells make of a spelling's group: the columns
// the object is built from, as positions in its cols, each one's segments, and
// the target count, -1 where every such cell is empty. clash is set where two
// cells disagree on the count, and then the group writes nothing.
type groupShape struct {
	cols  []int
	segs  [][]string
	n     int
	clash string
}

// writes reports whether the group writes an object into the row's properties.
func (g groupShape) writes() bool {
	return g.n > 0 && g.clash == ""
}

// shapeOf splits a spelling's cells on the escaped list separator. The columns
// refused under an exact spelling are left out, and the rest are read in
// header order, so a disagreement names one pair on every run.
func (a *Adapter) shapeOf(p *plan, sp *spelling, record []string) groupShape {
	g := groupShape{n: -1, segs: make([][]string, len(sp.cols))}
	first := ""
	for j, i := range sp.cols {
		g.cols = append(g.cols, j)
		val := record[i]
		if val == "" || g.clash != "" {
			continue
		}
		s := splitListElems(val, a.config.listSep)
		switch {
		case g.n == -1:
			g.n, first = len(s), p.columns[i].name
		case len(s) != g.n:
			g.clash = fmt.Sprintf("association %q columns disagree on target count: %q holds %d, %q holds %d",
				sp.field, first, g.n, p.columns[i].name, len(s))
		}
		g.segs[j] = s
	}
	for _, j := range g.cols {
		if g.segs[j] == nil && g.n > 0 {
			g.segs[j] = make([]string, g.n)
		}
	}
	return g
}

// writeProperty writes a resolved column's cell: its property's empty value,
// or the cell coerced, or its text where it does not coerce. Only an exact
// column reports that, since a strict validator reads no folded name.
func (a *Adapter) writeProperty(props map[string]any, col *column, val string, span location.Span, typeName string, collector *diag.Collector) {
	if val == "" {
		props[col.name] = a.emptyCell(col.prop)
		return
	}
	coerced, err := a.coerceStringValue(val, col.prop.Constraint())
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("column %q: %s", col.name, err)).
			WithSpan(span).
			WithDetail(diag.DetailKeyTypeName, typeName).Build())
		props[col.name] = val
		return
	}
	props[col.name] = coerced
}

// emptyCell decides what an empty cell holds for a declared property. The wire
// cannot: [csv.Writer] writes the empty string and a missing value as the same
// empty field, and [csv.Reader] reads a quoted empty field as a bare one.
//
// A required property's empty cell is the empty rendering of its kind when the
// kind has one — "" for a String, [] for a List — because a required property
// cannot be null. Where the kind has none (an Integer, a Date), it is nil, so
// the validator reports the property missing. An optional property's empty
// cell is nil, so an optional "" or [] reads back as null.
func (a *Adapter) emptyCell(p *schema.Property) any {
	if p.IsOptional() {
		return nil
	}
	v, err := a.coerceStringValue("", p.Constraint())
	if err != nil {
		return nil
	}
	return v
}

// assembleEdgeGroup writes the object of a group that writes one, one object
// per target on the escaped list separator, deciding each target's claims itself. An empty cell
// in a present group is an empty segment on every target. Zipping holds
// because edge properties are scalars by language rule.
func (a *Adapter) assembleEdgeGroup(
	p *plan,
	sp *spelling,
	resolvedSpelling bool,
	g groupShape,
	span location.Span,
	typeName string,
	collector *diag.Collector,
	props map[string]any,
) {
	cols, segs, n := g.cols, g.segs, g.n
	targets := make([]any, n)
	for t := range n {
		obj := make(map[string]any, len(cols))
		state := make([]claimState, len(sp.cols))
		if resolvedSpelling {
			for _, c := range sp.members {
				r, pass := c.resolve(func(j int) bool {
					return segs[j][t] != "" || j == c.exact && a.edgeEmptyValue(&p.columns[sp.cols[j]]) != nil
				})
				if r >= 0 {
					state[r] = claimResolved
				}
				for _, j := range pass {
					state[j] = claimPassed
				}
			}
		}
		for _, j := range cols {
			col := &p.columns[sp.cols[j]]
			seg := segs[j][t]
			if !resolvedSpelling || state[j] == claimPassed || !claimable(col.member) {
				if seg != "" {
					obj[col.suffix] = seg // no member, no constraint in reach, or ambiguous: the validator rules
				}
				continue
			}
			if state[j] != claimResolved {
				continue // shadowed or folded empty: absent on this target
			}
			if seg == "" {
				if v := a.edgeEmptyValue(col); v != nil {
					obj[col.suffix] = v
				}
				continue // an edge property is never null: absent on this target
			}
			member := col.eprop
			if col.member == memberKey {
				member = col.key
			}
			coerced, err := a.coerceStringValue(seg, member.Constraint())
			if err != nil {
				collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
					fmt.Sprintf("column %q.%s: %s", sp.field, col.suffix, err)).
					WithSpan(span).
					WithDetail(diag.DetailKeyTypeName, typeName).Build())
				obj[col.suffix] = seg
				continue
			}
			obj[col.suffix] = coerced
		}
		targets[t] = obj
	}

	if (sp.rel == nil || !sp.rel.IsMany()) && n == 1 {
		props[sp.field] = targets[0]
		return
	}
	props[sp.field] = targets
}

// claimable reports whether a dotted column's member takes part in its
// target's claims: a key the target declares, or an edge property.
func claimable(m edgeMember) bool {
	return m == memberKey || m == memberProperty
}

// edgeEmptyValue is what an empty segment holds for a resolved edge column, or
// nil where the member has no empty value and the key is absent: a key
// component's kind decides, and an edge property follows [Adapter.emptyCell].
func (a *Adapter) edgeEmptyValue(col *column) any {
	switch col.member {
	case memberKey:
		if v, err := a.coerceStringValue("", col.key.Constraint()); err == nil {
			return v
		}
	case memberProperty:
		return a.emptyCell(col.eprop)
	}
	return nil
}

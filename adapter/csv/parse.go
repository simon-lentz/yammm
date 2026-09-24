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
	collector := diag.NewCollectorUnlimited()
	if a.configRefused(collector) {
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
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
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

// configRefused reports a setting [Adapter.configError] refuses as an Error and
// whether it did. The refusal names the configuration, not the input, so it
// carries no span and takes [E_CSV_CONFIG].
func (a *Adapter) configRefused(collector *diag.Collector) bool {
	err := a.configError()
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_CONFIG, err.Error()).Build())
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
	if readFailure(err) {
		issue := diag.NewIssue(diag.Fatal, diag.E_ADAPTER_IO,
			fmt.Sprintf("csv read failed after %d records: %s", records, err))
		if typeName != "" {
			issue.WithDetail(diag.DetailKeyTypeName, typeName)
		}
		collector.Collect(issue.Build())
		return true
	}
	issue := diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, fmt.Sprintf("csv parse: %s", err)).
		WithSpan(parseErrorSpan(source, err))
	if typeName != "" {
		issue.WithDetail(diag.DetailKeyTypeName, typeName)
	}
	collector.Collect(issue.Build())
	return false
}

// readFailure reports whether err came from the [io.Reader] rather than from
// the input's end or a fault in the input. The end is [io.EOF] itself, as both
// loops test it: an error that only wraps it is the reader failing, and
// treating it as the end of a record would loop. A refused delimiter never
// arrives here: [Adapter.configError] refuses it before the first read.
func readFailure(err error) bool {
	if err == io.EOF { //nolint:errorlint // only the bare io.EOF ends the input; a wrapped one is a failure
		return false
	}
	if _, ok := errors.AsType[*csv.ParseError](err); ok { //nolint:errcheck // type check only, value unused
		return false
	}
	return true
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
// its row is skipped. Requires [WithTypeColumn] to be set; without it the
// result carries an Error [E_CSV_CONFIG] and no record is read. Faults and
// provenance are as [Adapter.ParseTyped].
func (a *Adapter) ParseWithTypeColumn(
	ctx context.Context,
	source location.SourceID,
	r io.Reader,
	typeResolver func(string) *schema.Type,
) (map[string][]instance.RawInstance, diag.Result) {
	collector := diag.NewCollectorUnlimited()

	if msg := typeColumnError(a.config.typeColumn); msg != "" {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_CONFIG, msg).Build())
		return nil, collector.Result()
	}
	if a.configRefused(collector) {
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
		collector.Collect(diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE,
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
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
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
			filteredVals = append(filteredVals, record[i])
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

// typeColumnError refuses a [WithTypeColumn] name no header can hold: none, or
// one holding a CR LF, which [encoding/csv] reads back as LF.
func typeColumnError(name string) string {
	switch {
	case name == "":
		return "csv adapter: ParseWithTypeColumn requires WithTypeColumn to be set"
	case strings.Contains(name, "\r\n"):
		return fmt.Sprintf("csv adapter: type column %q holds a CR LF, which encoding/csv reads back as LF, so no header holds it", name)
	}
	return ""
}

// readHeader reads the header row and its line, refusing a column named twice
// or not at all. The reader fixes FieldsPerRecord from this row, so a later
// record of another length comes back with [csv.ErrFieldCount].
func (a *Adapter) readHeader(reader *csv.Reader, source location.SourceID, collector *diag.Collector) ([]string, location.Span, bool) {
	header, err := reader.Read()
	if err != nil {
		severity, code := diag.Error, diag.E_ADAPTER_PARSE
		if readFailure(err) {
			severity, code = diag.Fatal, diag.E_ADAPTER_IO
		}
		span := parseErrorSpan(source, err)
		if span.IsZero() {
			span = location.Point(source, 1, 1)
		}
		collector.Collect(diag.NewIssue(severity, code,
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
			collector.Collect(diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE,
				fmt.Sprintf("header column %d has no name", i+1)).
				WithSpan(span).Build())
			ok = false
		case seen[name] == 2:
			collector.Collect(diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE,
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

// recordToProps converts a CSV record to the object the validator reads, as
// the package documentation's "Column Mapping" and "Empty Cells" state: the
// row's keys are decided from its cells, and [instance.ClaimKeys] decides
// which member each key claims, over every key alike. The record is never
// longer than the plan: see [Adapter.readHeader].
func (a *Adapter) recordToProps(
	record []string,
	p *plan,
	span location.Span,
	typeName string,
	collector *diag.Collector,
) map[string]any {
	props := make(map[string]any, len(record))
	if p.typ == nil {
		for i, val := range record {
			props[p.columns[i].name] = val
		}
		return props
	}
	report := func(msg string) {
		collector.Collect(diag.NewIssue(diag.Error, diag.E_ADAPTER_PARSE, msg).
			WithSpan(span).
			WithDetail(diag.DetailKeyTypeName, typeName).Build())
	}

	present := make([]bool, len(p.spellings))
	for s, sp := range p.spellings {
		for _, i := range sp.cols {
			present[s] = present[s] || record[i] != ""
		}
	}
	// A group whose targets disagree on their count writes nothing and so
	// claims nothing; dropping it can hand its member to another spelling,
	// whose group is then read as that member's, so the decision repeats.
	dropped := make([]bool, len(p.spellings))
	shapes := make([]groupShape, len(p.spellings))
	var claims instance.Claims
	mask := make([]byte, 0, len(p.columns)+len(p.spellings))
	for {
		mask = rowMask(p, record, present, dropped, mask)
		claims = a.nodeClaims(p, mask)
		changed := false
		for s := range p.spellings {
			sp := &p.spellings[s]
			if !present[s] || dropped[s] || !claimsAssociation(claims, sp) {
				continue
			}
			shapes[s] = a.shapeOf(p, sp, record)
			if shapes[s].clash != "" {
				report(shapes[s].clash)
				dropped[s], changed = true, true
			}
		}
		if !changed {
			break
		}
	}

	for s, sp := range p.spellings {
		if !present[s] || dropped[s] {
			continue
		}
		if sp.plain >= 0 && record[sp.plain] != "" {
			// Two values for one key, as a repeated JSON member. The group is
			// kept where the key claims an association, whose only valid form
			// it is, and the plain value everywhere else.
			report(fmt.Sprintf("column %[1]q and the dotted columns %[1]s.* both write the key %[1]q", sp.field))
			if !claimsAssociation(claims, &p.spellings[s]) {
				continue
			}
		}
		if claimsAssociation(claims, &p.spellings[s]) {
			props[sp.field] = a.assembleEdgeGroup(p, &p.spellings[s], shapes[s], span, typeName, collector, report)
			continue
		}
		obj := make(map[string]any, len(sp.cols))
		for _, i := range sp.cols {
			if record[i] != "" {
				obj[p.columns[i].suffix] = record[i] // no association in reach: the validator rules
			}
		}
		props[sp.field] = obj
	}

	for i, val := range record {
		col := &p.columns[i]
		if col.kind != columnPlain {
			continue
		}
		if _, written := props[col.name]; written {
			continue // the group wrote the key, or holds it beside an empty cell
		}
		if prop := claims.Property(col.name); prop != nil {
			a.writeProperty(props, col.name, prop, val, span, typeName, collector)
			continue
		}
		if val != "" {
			props[col.name] = val // the validator reports the unknown, shadowed or colliding field
		}
	}
	return props
}

// rowMask marks the keys a row's object holds before an empty folded column is
// placed: a byte per column, set for a filled plain column and for an empty
// one that spells a property exactly, which holds that property's empty value,
// then a byte per spelling, set for a group with a filled cell that no count
// clash dropped. Such a group writes its field's key, or loses it to the plain
// column of the same name; the key is held either way.
func rowMask(p *plan, record []string, present, dropped []bool, mask []byte) []byte {
	mask = mask[:0]
	for i, c := range p.columns {
		mask = append(mask, flag(c.kind == columnPlain && (record[i] != "" || c.exact != nil)))
	}
	for s := range p.spellings {
		mask = append(mask, flag(present[s] && !dropped[s]))
	}
	return mask
}

func flag(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// nodeClaims is [instance.ClaimKeys]'s decision over the keys mask marks and
// every empty folded column the row places. It is a function of the mask alone,
// so the plan keeps it: the rows of one file repeat few masks.
func (a *Adapter) nodeClaims(p *plan, mask []byte) instance.Claims {
	if c, ok := p.claimMemo[string(mask)]; ok {
		return c
	}
	keys := make([]string, 0, len(mask))
	for i, c := range p.columns {
		if mask[i] != 0 {
			keys = append(keys, c.name)
		}
	}
	for s, sp := range p.spellings {
		if mask[len(p.columns)+s] != 0 {
			keys = append(keys, sp.field)
		}
	}
	keys = append(keys, a.emptyFoldedColumns(p, mask, keys)...)
	c := instance.ClaimKeys(p.typ, keys, a.config.strict)
	p.claimMemo[string(mask)] = c
	return c
}

// emptyFoldedColumns is the empty plain columns that hold their property's
// empty value: a column that folds onto a property no key of the row folds
// onto, when it is the one such column in the header. Two such columns are
// both absent, as a JSON object that holds neither is. A folding column the
// mask leaves unmarked is an empty one.
func (a *Adapter) emptyFoldedColumns(p *plan, mask []byte, keys []string) []string {
	if a.config.strict {
		return nil
	}
	folded := make(map[string]bool, len(keys))
	for _, key := range keys {
		if lower, ok := instance.FoldKey(key); ok {
			folded[lower] = true
		}
	}
	candidates := make(map[*schema.Property][]string)
	var order []*schema.Property
	for i, c := range p.columns {
		if c.kind != columnPlain || c.folds == nil || mask[i] != 0 || folded[strings.ToLower(c.folds.Name())] {
			continue
		}
		if _, seen := candidates[c.folds]; !seen {
			order = append(order, c.folds)
		}
		candidates[c.folds] = append(candidates[c.folds], c.name)
	}
	var added []string
	for _, prop := range order {
		if names := candidates[prop]; len(names) == 1 {
			added = append(added, names[0])
		}
	}
	return added
}

// claimsAssociation reports whether the spelling's key claims the association
// the spelling names. A shadowed or colliding spelling claims nothing, so its
// group is text for the validator to report.
func claimsAssociation(claims instance.Claims, sp *spelling) bool {
	return sp.rel != nil && claims.Relation(sp.field) == sp.rel
}

// groupShape is what one row's cells make of an association's group: each
// position's segments, one per target, and the target count. The count is the
// deciding columns' (see [markDeciding]), and clash is set where two of them
// disagree. Any other column is zipped where its count agrees, taken whole as
// written where there is one target, and otherwise left out, in stray.
type groupShape struct {
	segs  [][]string
	n     int
	clash string
	stray []strayColumn
}

// strayColumn is a column naming no member whose segment count is not the
// group's target count.
type strayColumn struct {
	col, count int
}

// shapeOf splits a group's cells on the escaped list separator, in header
// order, so a disagreement names one pair on every run.
func (a *Adapter) shapeOf(p *plan, sp *spelling, record []string) groupShape {
	g := groupShape{n: -1, segs: make([][]string, len(sp.cols))}
	first := ""
	for j, i := range sp.cols {
		if record[i] == "" {
			continue
		}
		g.segs[j] = splitListElems(record[i], a.config.listSep)
		if !sp.members[j].decides {
			continue
		}
		switch n := len(g.segs[j]); {
		case g.n == -1:
			g.n, first = n, p.columns[i].name
		case n != g.n && g.clash == "":
			g.clash = fmt.Sprintf("association %q columns disagree on target count: %q holds %d, %q holds %d",
				sp.field, first, g.n, p.columns[i].name, n)
		}
	}
	if g.n == -1 {
		g.n = 1 // no deciding column holds text: one target carries it
	}
	for j, i := range sp.cols {
		if sp.members[j].decides || g.segs[j] == nil || len(g.segs[j]) == g.n {
			continue
		}
		if g.n == 1 {
			g.segs[j] = []string{record[i]}
			continue
		}
		g.stray = append(g.stray, strayColumn{col: i, count: len(g.segs[j])})
		g.segs[j] = nil
	}
	for j := range g.segs {
		if g.segs[j] == nil {
			g.segs[j] = make([]string, g.n)
		}
	}
	return g
}

// writeProperty writes a claimed column's cell: its property's empty value,
// or the cell coerced, or its text where it does not coerce.
func (a *Adapter) writeProperty(props map[string]any, name string, prop *schema.Property, val string, span location.Span, typeName string, collector *diag.Collector) {
	if val == "" {
		props[name] = a.emptyCell(prop)
		return
	}
	coerced, err := a.coerceStringValue(val, prop.Constraint())
	if err != nil {
		collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
			fmt.Sprintf("column %q: %s", name, err)).
			WithSpan(span).
			WithDetail(diag.DetailKeyTypeName, typeName).Build())
		props[name] = val
		return
	}
	props[name] = coerced
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

// assembleEdgeGroup builds the value of an association's group: one object per
// target, zipped on the escaped list separator, each deciding its own claims.
// An empty cell in a present group is an empty segment on every target.
// Zipping holds because edge properties are scalars by language rule.
func (a *Adapter) assembleEdgeGroup(
	p *plan,
	sp *spelling,
	g groupShape,
	span location.Span,
	typeName string,
	collector *diag.Collector,
	report func(string),
) any {
	for _, st := range g.stray {
		report(fmt.Sprintf("association %q column %q holds %d segments for %d targets and is left out",
			sp.field, p.columns[st.col].name, st.count, g.n))
	}
	targets := make([]any, g.n)
	for t := range g.n {
		targets[t] = a.edgeTarget(p, sp, g, t, span, typeName, collector)
	}
	if !sp.rel.IsMany() && g.n == 1 {
		return targets[0]
	}
	return targets
}

// edgeTarget builds one target's object. Its keys are its filled segments and
// the empty ones that hold their member's empty value; [instance.ClaimEdgeKeys]
// decides which member each claims.
func (a *Adapter) edgeTarget(p *plan, sp *spelling, g groupShape, t int, span location.Span, typeName string, collector *diag.Collector) map[string]any {
	mask := make([]byte, len(sp.cols))
	for j := range sp.cols {
		mask[j] = flag(g.segs[j][t] != "" || sp.members[j].exact && a.memberEmpty(sp.members[j]) != nil)
	}
	claims := a.edgeClaims(p, sp, mask)

	obj := make(map[string]any, len(sp.cols))
	for j, i := range sp.cols {
		suffix, seg := p.columns[i].suffix, g.segs[j][t]
		var member suffixMember
		if key := claims.Key(suffix); key != nil {
			member = suffixMember{member: memberKey, key: key}
		} else if eprop := claims.Property(suffix); eprop != nil {
			member = suffixMember{member: memberProperty, eprop: eprop}
		}
		switch {
		case member.member == memberNone:
			if seg != "" {
				obj[suffix] = seg // no member, or shadowed or colliding: the validator rules
			}
		case seg == "":
			if v := a.memberEmpty(member); v != nil {
				obj[suffix] = v
			}
		default:
			constraint := member.eprop
			if member.member == memberKey {
				constraint = member.key
			}
			coerced, err := a.coerceStringValue(seg, constraint.Constraint())
			if err != nil {
				collector.Collect(diag.NewIssue(diag.Error, E_CSV_COERCE,
					fmt.Sprintf("column %q.%s: %s", sp.field, suffix, err)).
					WithSpan(span).
					WithDetail(diag.DetailKeyTypeName, typeName).Build())
				obj[suffix] = seg
				continue
			}
			obj[suffix] = coerced
		}
	}
	return obj
}

// edgeClaims is [instance.ClaimEdgeKeys]'s decision over the suffixes mask
// marks and every empty folded suffix the target places, kept per spelling as
// [Adapter.nodeClaims] keeps a row's.
func (a *Adapter) edgeClaims(p *plan, sp *spelling, mask []byte) instance.EdgeClaims {
	if c, ok := sp.claimMemo[string(mask)]; ok {
		return c
	}
	keys := make([]string, 0, len(mask))
	for j, i := range sp.cols {
		if mask[j] != 0 {
			keys = append(keys, p.columns[i].suffix)
		}
	}
	keys = append(keys, a.emptyFoldedSuffixes(p, sp, mask, keys)...)
	c := instance.ClaimEdgeKeys(sp.rel, sp.target, keys, a.config.strict)
	sp.claimMemo[string(mask)] = c
	return c
}

// emptyFoldedSuffixes is the empty segments of one target that hold their
// member's empty value: a suffix that folds onto a member no key of the target
// folds onto, when it is the one such suffix in the group. A folding suffix
// the mask leaves unmarked is an empty one.
func (a *Adapter) emptyFoldedSuffixes(p *plan, sp *spelling, mask []byte, keys []string) []string {
	if a.config.strict {
		return nil
	}
	folded := make(map[string]bool, len(keys))
	for _, key := range keys {
		if lower, ok := instance.FoldKey(key); ok {
			folded[lower] = true
		}
	}
	candidates := make(map[string][]string)
	var order []string
	for j, i := range sp.cols {
		m := sp.members[j]
		if mask[j] != 0 || m.exact || a.memberEmpty(m) == nil {
			continue
		}
		name := m.name()
		if folded[name] {
			continue
		}
		if _, seen := candidates[name]; !seen {
			order = append(order, name)
		}
		candidates[name] = append(candidates[name], p.columns[i].suffix)
	}
	var added []string
	for _, name := range order {
		if suffixes := candidates[name]; len(suffixes) == 1 {
			added = append(added, suffixes[0])
		}
	}
	return added
}

// name is the lower-case field the member is read under.
func (m suffixMember) name() string {
	if m.member == memberKey {
		return strings.ToLower(keyPrefix + m.key.Name())
	}
	return strings.ToLower(m.eprop.Name())
}

// memberEmpty is what an empty segment holds for a member, or nil where it is
// absent: a key component's kind decides, and an edge property follows
// [Adapter.emptyCell], since an edge property is never null.
func (a *Adapter) memberEmpty(m suffixMember) any {
	switch m.member {
	case memberKey:
		if v, err := a.coerceStringValue("", m.key.Constraint()); err == nil {
			return v
		}
	case memberProperty:
		return a.emptyCell(m.eprop)
	}
	return nil
}

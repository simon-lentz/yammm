package snapshot

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// tableMissError reports a type identity the writer denotes at some position
// but the types table does not hold.
type tableMissError struct {
	id       schema.TypeID
	position string
}

func (e *tableMissError) Error() string {
	return fmt.Sprintf("type %s at %s position is absent from the types table", e.id, e.position)
}

// depthExceededError is the writer's half of the composed-nesting bound. It
// is typed so Marshal reports it under the code and severity the reader uses
// for the same condition, instead of as an internal failure the caller cannot
// distinguish from a corrupt writer state.
type depthExceededError struct {
	depth int
	ref   string
	key   string
}

func (e *depthExceededError) Error() string {
	return fmt.Sprintf("composed nesting depth %d exceeds limit %d at %s[%s]",
		e.depth, maxComposedDepth, e.ref, diag.ValueText(e.key))
}

// unwritableValueError names a property whose value the wire cannot carry: an
// instance's, or, where relation is set, an edge property of one of its
// association records. ref and key address a root, or a duplicate record's
// instance where duplicate is set; path names the composition hops below it.
type unwritableValueError struct {
	ref, key, relation, property string
	path                         []string
	duplicate                    bool
}

func (e *unwritableValueError) Error() string {
	where := fmt.Sprintf("%s[%s]", e.ref, diag.ValueText(e.key))
	if e.duplicate {
		where = "duplicate record " + where
	}
	if len(e.path) > 0 {
		where += "." + strings.Join(e.path, ".")
	}
	if e.relation != "" {
		where += fmt.Sprintf(" under %q", e.relation)
	}
	return fmt.Sprintf("%s: property %q holds a value the wire cannot carry: a non-finite float, a string that is not valid UTF-8, a json.Number that is no number literal, a time or a UUID its constraint does not render as text that reads back, or a value that is no scalar, list or string-keyed map",
		where, e.property)
}

// schemaNames maps each import-closure member's source identity to its
// declared name — the form the .ys wire states a type in. It is built once
// per Marshal because a closure walk per type identity would repeat the same
// answer for every row.
type schemaNames map[location.SourceID]string

// newSchemaNames indexes the entry schema's whole import closure. A name is
// unique across a closure: the loader registers every member and
// [schema.Registry.Register] refuses a second schema with an existing name.
func newSchemaNames(s *schema.Schema) schemaNames {
	if s == nil {
		return nil
	}
	names := make(schemaNames)
	for _, cs := range s.Closure() {
		names[cs.SourceID()] = cs.Name()
	}
	return names
}

// entry renders one type identity as the document states it: its schema's
// name in the closure, and its type name.
//
// A miss is NOT rendered as a source path. The whole point of keying by name is
// that a path does not travel, so falling back to one would put a
// machine-local path into the field that exists to keep it out — and the
// document would look well-formed.
//
// PRECONDITION: the closure names id. Every constructor of a snapshot refuses
// an identity the closure does not hold, so every identity a snapshot carries
// meets it.
func (n schemaNames) entry(id schema.TypeID) typeTableEntry {
	return typeTableEntry{Schema: n[id.SchemaPath()], Name: id.Name()}
}

// requireDenotable reports the first type identity the closure cannot name.
// Every constructor of a snapshot refuses such an identity, so this guards
// that invariant: a document that cannot state its own types is one no reader
// can bind.
func (n schemaNames) requireDenotable(ids []schema.TypeID) (schema.TypeID, bool) {
	for _, id := range ids {
		if _, ok := n[id.SchemaPath()]; !ok {
			return id, false
		}
	}
	return schema.TypeID{}, true
}

// typeTable is the types section under construction: every type identity the
// document denotes, ordered by (schema name, type name), with the row index
// each reference uses.
type typeTable struct {
	entries []typeTableEntry
	index   map[schema.TypeID]int
}

// ref renders id the way the document states it — "schema#name" — reading the
// row the table already holds. The writer's diagnostics use it so that the
// one code both halves share, [diag.E_SNAPSHOT_DEPTH_EXCEEDED], carries the
// same detail from the writer as [streamDecoder.refAt] gives from the reader.
// An identity absent from the table falls back to its own rendering.
func (tt *typeTable) ref(id schema.TypeID) string {
	if row, ok := tt.index[id]; ok {
		return TypeRef(tt.entries[row]).String()
	}
	return id.String()
}

// rowRef returns a fresh pointer to the table row for id, for the nullable
// row-index wire fields. A miss means the collection pass did not reach a
// position the writer is now emitting, which is an internal inconsistency
// rather than a property of the data.
func (tt *typeTable) rowRef(id schema.TypeID, position string) (*int, error) {
	row, ok := tt.index[id]
	if !ok {
		return nil, &tableMissError{id: id, position: position}
	}
	return &row, nil
}

// buildTypeTable collects every type identity the view denotes — instance,
// composed child, edge target, duplicate, conflict, parent, unresolved edge —
// and orders them by TypeID. An unresolved target has no instance, so the
// table can be wider than the set of types holding one.
func buildTypeTable(view *writerView, names schemaNames) *typeTable {
	seen := make(map[schema.TypeID]struct{})

	var collectInstance func(inst *graph.Instance)
	collectInstance = func(inst *graph.Instance) {
		if inst == nil {
			return
		}
		seen[inst.TypeID()] = struct{}{}
		for _, relName := range inst.ComposedRelations() {
			for _, child := range inst.Composed(relName) {
				collectInstance(child)
			}
		}
	}

	for _, group := range view.groups {
		seen[group.id] = struct{}{}
		for _, inst := range group.instances {
			collectInstance(inst)
			for _, e := range view.edges[inst] {
				seen[e.Target().TypeID()] = struct{}{}
			}
		}
	}

	for _, d := range view.duplicates {
		collectInstance(d.Instance())
		if d.Conflict() != nil {
			seen[d.Conflict().TypeID()] = struct{}{}
		}
		if d.Parent() != nil {
			seen[d.Parent().TypeID()] = struct{}{}
		}
	}

	for _, u := range view.unresolved {
		if u.Source() != nil {
			seen[u.Source().TypeID()] = struct{}{}
		}
		seen[u.TargetType()] = struct{}{}
	}

	// Ordered by the rendered row, not by TypeID: the sort key must be what
	// the document carries, or one schema text loaded from two directories
	// assigns different row indices to the same identities and the instances
	// section follows that order into different bytes.
	ids := slices.SortedFunc(maps.Keys(seen), func(a, b schema.TypeID) int {
		ea, eb := names.entry(a), names.entry(b)
		if c := cmp.Compare(ea.Schema, eb.Schema); c != 0 {
			return c
		}
		return cmp.Compare(ea.Name, eb.Name)
	})

	tt := &typeTable{
		entries: make([]typeTableEntry, 0, len(ids)),
		index:   make(map[schema.TypeID]int, len(ids)),
	}
	// Dedup is on TypeID while rows render (schema name, type name): two
	// identities cannot share a row, because a closure holds one schema per
	// name and a schema holds one type per name.
	for i, id := range ids {
		tt.entries = append(tt.entries, names.entry(id))
		tt.index[id] = i
	}
	return tt
}

// resolveTypeTable binds each table row to a schema type, strictly: a v3
// document was written by a writer that resolved every identity at write
// time, so a row the closure does not declare draws an Error and no name
// fallback runs. Two rows carrying one identity are malformed.
func (sd *streamDecoder) resolveTypeTable() {
	sd.tableIDs = make([]schema.TypeID, len(sd.typeTable))

	// firstRow[i] is the first row carrying row i's identity. A repeated row
	// is malformed once and binds as its first row does, so a row the closure
	// does not declare draws E_SNAPSHOT_UNKNOWN_TYPE once.
	firstRow := make([]int, len(sd.typeTable))
	seen := make(map[typeTableEntry]int, len(sd.typeTable))
	for i, e := range sd.typeTable {
		firstRow[i] = i
		if first, dup := seen[e]; dup {
			firstRow[i] = first
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("types table rows %d and %d carry one identity %s", first, i, sd.duplicateEntryText(e))).Build())
			continue
		}
		seen[e] = i
	}

	if sd.schema == nil {
		return
	}
	// The tag renders from the row alone, so it is derived once here rather
	// than per instance and per composed child during materialization.
	sd.tableTags = make([]string, len(sd.typeTable))
	for i, e := range sd.typeTable {
		if first := firstRow[i]; first != i {
			sd.tableIDs[i], sd.tableTags[i] = sd.tableIDs[first], sd.tableTags[first]
			continue
		}
		if t, ok := typeByWireID(sd.schema, e.Schema, e.Name); ok {
			sd.tableIDs[i] = t.ID()
			sd.tableTags[i] = schema.TagForm(sd.schema, t.ID())
			continue
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
			fmt.Sprintf("types table row %d names %s, which the import closure does not declare",
				i, sd.entryText(e))).
			WithDetail(diag.DetailKeyTypeName, TypeRef(e).String()).
			Build())
	}
}

// rowAt resolves a nullable row reference without reporting, and holds the
// one definition of what makes a reference valid. A caller running after
// validateBody uses it so the same defect is not reported twice.
func (sd *streamDecoder) rowAt(row *int) (int, bool) {
	if row == nil || *row < 0 || *row >= len(sd.typeTable) {
		return 0, false
	}
	return *row, true
}

// requireRow is rowAt with a diagnostic: nil and out-of-range each draw an
// Error naming the position, and neither ever binds to row 0. position runs
// only on the failure paths — the walk asks for a row per edge and per
// composed child, and discards the message whenever the reference resolves.
func (sd *streamDecoder) requireRow(row *int, position func() string) (int, bool) {
	if r, ok := sd.rowAt(row); ok {
		return r, true
	}
	if row == nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			position()+" carries no types-table row").Build())
		return 0, false
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
		fmt.Sprintf("%s references types table row %d, which holds %d rows",
			position(), *row, len(sd.typeTable))).Build())
	return 0, false
}

// refAt renders a table row as its identity for diagnostic messages.
func (sd *streamDecoder) refAt(row int) string {
	if row < 0 || row >= len(sd.typeTable) {
		return fmt.Sprintf("types[%d]", row)
	}
	return TypeRef(sd.typeTable[row]).String()
}

// refText is refAt for a message. A row the schema resolves names a declared
// type and is shown whole; any other row is the document's, so it is cut as
// [streamDecoder.entryText] cuts it.
func (sd *streamDecoder) refText(row int) string {
	if !sd.rowWritten(row) {
		return sd.refAt(row)
	}
	return sd.entryText(sd.typeTable[row])
}

// refQuoted is refText for a message that quotes the identity. A row cut by
// its parts quotes each part, so the type name survives the cut. A reader
// with a schema refuses an unresolved row before the walk, so a written row
// reaches it only with no schema, and no part is a name the closure holds.
func (sd *streamDecoder) refQuoted(row int) string {
	if !sd.rowWritten(row) {
		return strconv.Quote(sd.refAt(row))
	}
	e := sd.typeTable[row]
	if whole := TypeRef(e).String(); diag.QuoteValue(whole) == strconv.Quote(whole) {
		return strconv.Quote(whole)
	}
	return diag.QuoteValue(e.Schema) + "#" + diag.QuoteValue(e.Name)
}

// rowWritten reports whether row is a types-table row the schema does not
// resolve, so its identity is the document's text alone.
func (sd *streamDecoder) rowWritten(row int) bool {
	if row < 0 || row >= len(sd.typeTable) {
		return false
	}
	return row >= len(sd.tableIDs) || sd.tableIDs[row].IsZero()
}

// entryText renders a types-table row the schema does not resolve by its parts:
// the schema name whole when the import closure holds it, and otherwise each
// part cut as a value is, since a document can name a schema and a type of any
// length.
func (sd *streamDecoder) entryText(e typeTableEntry) string {
	schemaName := diag.ValueText(e.Schema)
	if sd.closureHolds(e.Schema) {
		schemaName = e.Schema
	}
	return schemaName + "#" + diag.ValueText(e.Name)
}

// closureHolds reports whether the import closure holds a schema named name.
func (sd *streamDecoder) closureHolds(name string) bool {
	if sd.schema == nil {
		return false
	}
	if sd.closureNames == nil {
		closure := sd.schema.Closure()
		sd.closureNames = make(map[string]struct{}, len(closure))
		for _, cs := range closure {
			sd.closureNames[cs.Name()] = struct{}{}
		}
	}
	_, ok := sd.closureNames[name]
	return ok
}

// duplicateEntryText renders a repeated row before the table is resolved: whole
// where the schema declares it, as refText renders a resolved row.
func (sd *streamDecoder) duplicateEntryText(e typeTableEntry) string {
	if sd.schema != nil {
		if _, ok := typeByWireID(sd.schema, e.Schema, e.Name); ok {
			return TypeRef(e).String()
		}
	}
	return sd.entryText(e)
}

// typeRefs returns the table as the schema-less display surface.
func (sd *streamDecoder) typeRefs() []TypeRef {
	refs := make([]TypeRef, len(sd.typeTable))
	for i, e := range sd.typeTable {
		refs[i] = TypeRef(e)
	}
	return refs
}

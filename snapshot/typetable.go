package snapshot

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

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
		e.depth, maxComposedDepth, e.ref, e.key)
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

// entry renders one type identity as the document states it, and reports
// whether the closure could name it.
//
// A miss is NOT rendered as a source path. The whole point of keying by name is
// that a path does not travel, so falling back to one would put a
// machine-local path into the field that exists to keep it out — and the
// document would look well-formed.
//
// PRECONDITION: requireDenotable has passed. [Marshal] runs it before a byte
// is written, so every identity reaching here is one the closure names.
func (n schemaNames) entry(id schema.TypeID) typeTableEntry {
	return typeTableEntry{Schema: n[id.SchemaPath()], Name: id.Name()}
}

// requireDenotable reports the first type identity the closure cannot name.
// An identity outside the closure reaches a snapshot only through
// caller-assembled parts — [graph.Graph.Add] refuses one — and a document
// that cannot state its own types is one no reader can bind.
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
		collectInstance(d.Instance)
		if d.Conflict != nil {
			seen[d.Conflict.TypeID()] = struct{}{}
		}
		if d.Parent != nil {
			seen[d.Parent.TypeID()] = struct{}{}
		}
	}

	for _, u := range view.unresolved {
		if u.Source != nil {
			seen[u.Source.TypeID()] = struct{}{}
		}
		seen[u.TargetType] = struct{}{}
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

	seen := make(map[typeTableEntry]int, len(sd.typeTable))
	for i, e := range sd.typeTable {
		if first, dup := seen[e]; dup {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
				fmt.Sprintf("types table rows %d and %d carry one identity %s", first, i, TypeRef(e))).Build())
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
		if t, ok := typeByWireID(sd.schema, e.Schema, e.Name); ok {
			sd.tableIDs[i] = t.ID()
			sd.tableTags[i] = schema.TagForm(sd.schema, t.ID())
			continue
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
			fmt.Sprintf("types table row %d names %s, which the import closure does not declare",
				i, TypeRef(e))).
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

// unknownTypeRows returns the rows s's import closure does not declare, in table
// order. It delegates to typeByWireID, so the exact-path match with no name
// fallback has one implementation and a caller cannot end up with a copy of it
// that drifts. A nil schema declares nothing, so every row comes back.
//
// It takes []TypeRef rather than a carrier so both Info surfaces can call it;
// only HeaderInfo exposes it today (see [HeaderInfo.UnknownTypes]).
func unknownTypeRows(rows []TypeRef, s *schema.Schema) []TypeRef {
	var unknown []TypeRef
	for _, row := range rows {
		if _, ok := typeByWireID(s, row.Schema, row.Name); !ok {
			unknown = append(unknown, row)
		}
	}
	return unknown
}

// typeRefs returns the table as the schema-less display surface.
func (sd *streamDecoder) typeRefs() []TypeRef {
	refs := make([]TypeRef, len(sd.typeTable))
	for i, e := range sd.typeTable {
		refs[i] = TypeRef(e)
	}
	return refs
}

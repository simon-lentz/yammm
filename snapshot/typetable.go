package snapshot

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
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
	id    schema.TypeID
	key   string
}

func (e *depthExceededError) Error() string {
	return fmt.Sprintf("composed nesting depth %d exceeds limit %d at %s[%s]",
		e.depth, maxComposedDepth, e.id, e.key)
}

// typeTable is the types section under construction: every type identity the
// document denotes, in TypeID order, with the row index each reference uses.
type typeTable struct {
	entries []typeTableEntry
	index   map[schema.TypeID]int
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
func buildTypeTable(view *writerView) *typeTable {
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

	ids := slices.SortedFunc(maps.Keys(seen), func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})

	tt := &typeTable{
		entries: make([]typeTableEntry, 0, len(ids)),
		index:   make(map[schema.TypeID]int, len(ids)),
	}
	// Dedup is on TypeID while rows render (SourceID.String(), Name): the two
	// agree only while location.ValidateSyntheticSourceID keeps String() injective.
	for i, id := range ids {
		tt.entries = append(tt.entries, typeTableEntry{
			SchemaPath: id.SchemaPath().String(),
			Name:       id.Name(),
		})
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
		if t, ok := typeByWireID(sd.schema, e.SchemaPath, e.Name); ok {
			sd.tableIDs[i] = t.ID()
			sd.tableTags[i] = schema.TagForm(sd.schema, t.ID())
			continue
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
			fmt.Sprintf("types table row %d names %s, which the import closure does not declare",
				i, TypeRef(e))).
			WithDetail(diag.DetailKeyTypeName, e.Name).
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

// typeRefs returns the table as the schema-less display surface.
func (sd *streamDecoder) typeRefs() []TypeRef {
	refs := make([]TypeRef, len(sd.typeTable))
	for i, e := range sd.typeTable {
		refs[i] = TypeRef(e)
	}
	return refs
}

package jschema

import (
	"fmt"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/schema"
)

// edgeRec carries what emission needs for one declared association: the
// relation (edge properties, field name) and its resolved target (primary-key
// fields for the _target_* block). The $defs key lives in defsTable.edges,
// keyed by relation pointer — an inherited association is the SAME pointer in
// the declaring type's own slice and in every subtype's AllAssociationsSlice,
// so subtypes reference the one entry their declaring owner gets.
type edgeRec struct {
	rel    *schema.Relation
	target *schema.Type
}

// defsTable holds the collision-free $defs keys for every entity that becomes
// (or may be referenced as) a $defs entry: types, datatypes, and EDGE_
// association entries, all sharing one $defs namespace. It also records the
// per-property DataType resolution (property pointer -> datatype $defs key)
// that schemaForProperty's dtRef callback consumes.
//
// Unlike gogen's nameTable there is no identifier casing: keys are raw schema
// names, unqualified where unique across the closure and
// "<schemaName>.<Name>"-qualified on collision. Distinct raw names never
// merge, so an unresolvable collision means literally identical names — a
// type and datatype sharing a name in one schema (the loader permits this),
// or same-named entities in two schemas that share a schema name.
type defsTable struct {
	taken     map[string]bool
	types     map[schema.TypeID]string
	dataTypes map[*schema.DataType]string
	dtProps   map[*schema.Property]string
	edges     map[*schema.Relation]string

	orderedSchemas   []*schema.Schema
	orderedTypes     []*schema.Type
	orderedDataTypes []*schema.DataType
	orderedEdges     []edgeRec
}

// buildDefsTable walks the closure (entry + transitively imported schemas)
// and assigns every type and datatype a collision-free $defs key, registers
// one EDGE_ entry per declared association, and resolves every
// DataType-typed property (type properties and association edge properties)
// in its declaring schema.
func buildDefsTable(s *schema.Schema) (*defsTable, error) {
	table := &defsTable{
		taken:     map[string]bool{},
		types:     map[schema.TypeID]string{},
		dataTypes: map[*schema.DataType]string{},
		dtProps:   map[*schema.Property]string{},
		edges:     map[*schema.Relation]string{},

		orderedSchemas: s.Closure(),
	}
	for _, sc := range table.orderedSchemas {
		table.orderedTypes = append(table.orderedTypes, sc.TypesSlice()...)
		table.orderedDataTypes = append(table.orderedDataTypes, sc.DataTypesSlice()...)
	}
	if err := table.assignNames(); err != nil {
		return nil, err
	}
	if err := table.registerEdges(); err != nil {
		return nil, err
	}
	if err := table.registerDataTypeProps(); err != nil {
		return nil, err
	}
	return table, nil
}

// assignNames resolves $defs keys for types and datatypes together (they can
// legally share a name, and both land in the one $defs namespace). Bare name
// when it has a single claimant across the closure; "<schemaName>.<Name>" for
// every claimant on collision; a qualified key that is still taken cannot be
// separated by qualification and is a hard error. Assignment iterates
// candidates in sorted order so keys are deterministic.
func (dt *defsTable) assignNames() error {
	type origin struct {
		kind       string // "type" | "datatype"
		schemaName string
		id         schema.TypeID
		d          *schema.DataType
	}
	byCandidate := map[string][]origin{}
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			byCandidate[t.Name()] = append(byCandidate[t.Name()], origin{kind: "type", schemaName: sc.Name(), id: t.ID()})
		}
		for _, d := range sc.DataTypesSlice() {
			byCandidate[d.Name()] = append(byCandidate[d.Name()], origin{kind: "datatype", schemaName: sc.Name(), d: d})
		}
	}

	assign := func(o origin, name string) {
		dt.taken[name] = true
		if o.kind == "type" {
			dt.types[o.id] = name
		} else {
			dt.dataTypes[o.d] = name
		}
	}

	for _, cand := range slices.Sorted(maps.Keys(byCandidate)) {
		origins := byCandidate[cand]
		if len(origins) == 1 && !dt.taken[cand] {
			assign(origins[0], cand)
			continue
		}
		for _, o := range origins {
			qualified := o.schemaName + "." + cand
			if dt.taken[qualified] {
				return fmt.Errorf(
					"jschema: $defs key collision that schema-qualification cannot resolve: %q (%s %q in schema %q); rename one entity",
					qualified, o.kind, cand, o.schemaName,
				)
			}
			assign(o, qualified)
		}
	}
	return nil
}

// registerEdges assigns every DECLARED association its
// "EDGE_<ownerKey>_<field>_<targetKey>" $defs key, built from the
// collision-resolved keys of the declaring (owner) type and the target,
// resolved in the DECLARING schema. Keys are unique among edges by
// construction (owner keys are unique per type, field names unique within a
// type) but share the $defs namespace with types and datatypes — and raw
// yammm type names may themselves start with "EDGE_", so a clash with an
// assigned name is possible and is a hard error.
func (dt *defsTable) registerEdges() error {
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			ownerKey, ok := dt.types[t.ID()]
			if !ok {
				return fmt.Errorf("jschema: no $defs key for type %q", t.Name())
			}
			for _, rel := range t.AssociationsSlice() { // OWN associations only
				target, ok := sc.ResolveType(rel.Target())
				if !ok {
					return fmt.Errorf("jschema: association %q target %q unresolved", rel.Name(), rel.Target().String())
				}
				targetKey, ok := dt.types[target.ID()]
				if !ok {
					return fmt.Errorf("jschema: association %q target type %q has no $defs key", rel.Name(), target.Name())
				}
				key := "EDGE_" + ownerKey + "_" + rel.FieldName() + "_" + targetKey
				if dt.taken[key] {
					return fmt.Errorf(
						"jschema: $defs key collision: generated edge key %q for association %q on type %q is already taken; rename the conflicting entity",
						key, rel.Name(), t.Name(),
					)
				}
				dt.taken[key] = true
				dt.edges[rel] = key
				dt.orderedEdges = append(dt.orderedEdges, edgeRec{rel: rel, target: target})
			}
		}
	}
	return nil
}

// registerDataTypeProps resolves the datatype $defs key for every
// DataType-typed member — type properties AND association edge properties —
// keyed by property pointer. Each is resolved in its DECLARING schema, so a
// property inherited from a cross-schema parent (whose DataTypeRef is
// relative to the parent's schema) maps to the right datatype. A
// list-of-DataType property carries its element's DataTypeRef and registers
// the same way. Members with no DataType reference (zero DataTypeRef) skip.
func (dt *defsTable) registerDataTypeProps() error {
	record := func(sc *schema.Schema, kind, owner string, p *schema.Property) error {
		ref := p.DataTypeRef()
		if ref.IsZero() {
			return nil
		}
		d, ok := sc.ResolveDataType(ref)
		if !ok {
			return fmt.Errorf("jschema: %s %q property %q references unresolved datatype %q", kind, owner, p.Name(), ref.String())
		}
		name, ok := dt.dataTypes[d]
		if !ok {
			return fmt.Errorf("jschema: no $defs key for datatype %q", d.Name())
		}
		dt.dtProps[p] = name
		return nil
	}
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			for _, p := range t.PropertiesSlice() { // OWN type properties
				if err := record(sc, "type", t.Name(), p); err != nil {
					return err
				}
			}
			for _, rel := range t.AssociationsSlice() { // OWN associations' edge properties
				for _, ep := range rel.PropertiesSlice() {
					if err := record(sc, "edge", rel.Name(), ep); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// defName returns the $defs key for a resolved type identity.
func (dt *defsTable) defName(id schema.TypeID) (string, bool) {
	n, ok := dt.types[id]
	return n, ok
}

// dataTypeDefName returns the $defs key for a datatype (pointer-keyed).
func (dt *defsTable) dataTypeDefName(d *schema.DataType) (string, bool) {
	n, ok := dt.dataTypes[d]
	return n, ok
}

// edgeDefName returns the EDGE_ $defs key for a declared (or inherited —
// same pointer) association.
func (dt *defsTable) edgeDefName(rel *schema.Relation) (string, bool) {
	n, ok := dt.edges[rel]
	return n, ok
}

// dtPropName reports the datatype $defs key for a DataType-typed property.
// Its signature satisfies schemaForProperty's dtRef callback.
func (dt *defsTable) dtPropName(p *schema.Property) (string, bool) {
	// Keyed by the DECLARED property (see the record loop above, which walks own
	// property slices). Origin() bridges to it from a merged view, where a property
	// whose annotations were unioned across ancestors is a synthesized copy that is
	// in no type's own slice.
	n, ok := dt.dtProps[p.Origin()]
	return n, ok
}

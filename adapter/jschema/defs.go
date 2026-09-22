package jschema

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

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
// that schemaForProperty's dtRef callback consumes, and the same resolution
// for a datatype whose own constraint lists another datatype.
//
// Unlike gogen's nameTable there is no identifier casing: keys are raw schema
// names, unqualified where unique across the closure and
// "<schemaName>.<Name>"-qualified on collision. A qualified key that is
// already taken — a type and a datatype sharing a name in one schema (the
// loader permits this), or same-named entities in two schemas that share a
// schema name — takes the first free numeric suffix, as an EDGE_ key does.
type defsTable struct {
	taken     map[string]bool
	types     map[schema.TypeID]string
	dataTypes map[*schema.DataType]string
	dtProps   map[*schema.Property]string
	dtInner   map[*schema.DataType]string
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
// and every datatype listing another datatype in its declaring schema.
func buildDefsTable(s *schema.Schema) (*defsTable, error) {
	table := &defsTable{
		taken:     map[string]bool{},
		types:     map[schema.TypeID]string{},
		dataTypes: map[*schema.DataType]string{},
		dtProps:   map[*schema.Property]string{},
		dtInner:   map[*schema.DataType]string{},
		edges:     map[*schema.Relation]string{},

		orderedSchemas: s.Closure(),
	}
	for _, sc := range table.orderedSchemas {
		table.orderedTypes = append(table.orderedTypes, sc.TypesSlice()...)
		table.orderedDataTypes = append(table.orderedDataTypes, sc.DataTypesSlice()...)
	}
	table.assignNames()
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
// every claimant on collision; a qualified key that is still taken — a type
// and a datatype of one schema sharing a name — takes the first free numeric
// suffix, types before datatypes in declaration order. Candidates are taken
// in sorted order so keys are deterministic.
func (dt *defsTable) assignNames() {
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
			assign(o, dt.reserve(o.schemaName+"."+cand))
		}
	}
}

// registerEdges assigns every DECLARED association its
// "EDGE_<ownerKey>_<field>_<targetKey>" $defs key, built from the
// collision-resolved keys of the declaring (owner) type and the target,
// resolved in the DECLARING schema. The key shares the $defs namespace with
// types, datatypes and other edges, and "_" joins parts that may themselves
// hold one: type EDGE_Car_owner_Person, or A's b_x association and A_b's x
// association to one target, spell the same key. Edges register after every
// type and datatype, so a declared name keeps its key and a taken edge key
// takes the first free numeric suffix, in closure and declaration order.
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
				dt.edges[rel] = dt.reserve("EDGE_" + ownerKey + "_" + rel.FieldName() + "_" + targetKey)
				dt.orderedEdges = append(dt.orderedEdges, edgeRec{rel: rel, target: target})
			}
		}
	}
	return nil
}

// reserve returns the first free key in "<base>", "<base>2", … and records it
// as taken.
func (dt *defsTable) reserve(base string) string {
	key := base
	for i := 2; dt.taken[key]; i++ {
		key = base + strconv.Itoa(i)
	}
	dt.taken[key] = true
	return key
}

// registerDataTypeProps resolves the datatype $defs key for every member
// whose constraint names a datatype, directly or as its innermost List
// element — type properties, association edge properties, and datatypes
// whose constraint lists another datatype. Each is resolved from the
// constraint in its DECLARING schema, so a property inherited from a
// cross-schema parent maps to the right datatype, and a Builder-built
// property, which carries no DataTypeRef, resolves too. Properties are keyed
// by pointer; members naming no datatype skip.
func (dt *defsTable) registerDataTypeProps() error {
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			for _, p := range t.PropertiesSlice() { // OWN type properties
				if err := dt.recordProperty(sc, "type", t.Name(), p); err != nil {
					return err
				}
			}
			for _, rel := range t.AssociationsSlice() { // OWN associations' edge properties
				for _, ep := range rel.PropertiesSlice() {
					if err := dt.recordProperty(sc, "edge", rel.Name(), ep); err != nil {
						return err
					}
				}
			}
		}
		for _, d := range sc.DataTypesSlice() {
			name, ok, err := dt.aliasKey(sc, d.Constraint())
			if err != nil {
				return fmt.Errorf("jschema: datatype %q: %w", d.Name(), err)
			}
			if ok {
				dt.dtInner[d] = name
			}
		}
	}
	return nil
}

func (dt *defsTable) recordProperty(sc *schema.Schema, kind, owner string, p *schema.Property) error {
	name, ok, err := dt.aliasKey(sc, p.Constraint())
	if err != nil {
		return fmt.Errorf("jschema: %s %q property %q: %w", kind, owner, p.Name(), err)
	}
	if ok {
		dt.dtProps[p] = name
	}
	return nil
}

// aliasKey returns the $defs key of the datatype c names, directly or as its
// innermost List element, resolving the name in sc, the schema that declares
// c. It reports false when c names no datatype.
func (dt *defsTable) aliasKey(sc *schema.Schema, c schema.Constraint) (string, bool, error) {
	_, ac, ok := aliasInLists(c)
	if !ok {
		return "", false, nil
	}
	d, ok := dataTypeNamed(sc, ac.DataTypeName())
	if !ok {
		return "", false, fmt.Errorf("references unresolved datatype %q", ac.DataTypeName())
	}
	name, ok := dt.dataTypes[d]
	if !ok {
		return "", false, fmt.Errorf("no $defs key for datatype %q", d.Name())
	}
	return name, true, nil
}

// dataTypeNamed resolves a datatype reference as an alias constraint spells
// it: "Name" in sc, or "alias.Name" in the schema sc imports as alias.
func dataTypeNamed(sc *schema.Schema, name string) (*schema.DataType, bool) {
	qualifier, local, qualified := strings.Cut(name, ".")
	if !qualified {
		return sc.DataType(name)
	}
	imp, ok := sc.ImportByAlias(qualifier)
	if !ok || imp.Schema() == nil {
		return nil, false
	}
	return imp.Schema().DataType(local)
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

// innerDataTypeName reports the $defs key of the datatype a datatype's own
// constraint lists.
func (dt *defsTable) innerDataTypeName(d *schema.DataType) (string, bool) {
	n, ok := dt.dtInner[d]
	return n, ok
}

// dtPropName reports the datatype $defs key for a DataType-typed property.
// Its signature satisfies schemaForProperty's dtRef callback.
func (dt *defsTable) dtPropName(p *schema.Property) (string, bool) {
	// Keyed by the DECLARED property (see registerDataTypeProps, which walks own
	// property slices). Origin() bridges to it from a merged view, where a property
	// whose annotations were unioned across ancestors is a synthesized copy that is
	// in no type's own slice.
	n, ok := dt.dtProps[p.Origin()]
	return n, ok
}

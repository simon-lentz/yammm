package jschema

import (
	"fmt"
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

// defsTable holds the $defs key of every type, datatype and EDGE_ entry, all
// in one namespace, and the datatype key each DataType-typed property and
// each datatype listing another datatype resolves to.
type defsTable struct {
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

// buildDefsTable walks the closure (entry + transitively imported schemas),
// keys every type, datatype and declared association, and resolves every
// DataType-typed property (type properties and association edge properties)
// and every datatype listing another datatype in its declaring schema.
func buildDefsTable(s *schema.Schema) (*defsTable, error) {
	table := &defsTable{
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
	if err := table.assignKeys(); err != nil {
		return nil, err
	}
	if err := table.registerDataTypeProps(); err != nil {
		return nil, err
	}
	return table, nil
}

// keyClaim is one entity's two spellings and where its assigned key is kept.
type keyClaim struct {
	bare, exact string
	assign      func(key string)
}

// assignKeys keys every type, datatype and declared association by the rule
// the package doc's "Names, $defs, and Imports" section states. Every bare
// spelling is counted before any key is assigned, so a key depends on the set
// of claims and never on their order.
func (dt *defsTable) assignKeys() error {
	var claims []keyClaim
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			id := t.ID()
			claims = append(claims, keyClaim{bare: t.Name(), exact: typeExactKey(sc.Name(), t.Name()), assign: func(key string) { dt.types[id] = key }})
		}
		for _, d := range sc.DataTypesSlice() {
			claims = append(claims, keyClaim{bare: d.Name(), exact: dataTypeExactKey(sc.Name(), d.Name()), assign: func(key string) { dt.dataTypes[d] = key }})
		}
	}
	for _, sc := range dt.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			for _, rel := range t.AssociationsSlice() { // OWN associations only
				target, ok := sc.ResolveType(rel.Target())
				if !ok {
					return fmt.Errorf("jschema: association %q target %q unresolved", rel.Name(), rel.Target().String())
				}
				claims = append(claims, keyClaim{
					bare:   "EDGE_" + t.Name() + "_" + rel.FieldName() + "_" + target.Name(),
					exact:  edgeExactKey(sc.Name(), t.Name(), rel.Name()),
					assign: func(key string) { dt.edges[rel] = key },
				})
				dt.orderedEdges = append(dt.orderedEdges, edgeRec{rel: rel, target: target})
			}
		}
	}
	claimants := map[string]int{}
	for _, c := range claims {
		claimants[c.bare]++
	}
	for _, c := range claims {
		if claimants[c.bare] == 1 {
			c.assign(c.bare)
		} else {
			c.assign(c.exact)
		}
	}
	return nil
}

// typeExactKey is a type's exact $defs key. The name after the last "." is an
// upper-case identifier, never "datatype" or "edge".
func typeExactKey(schemaName, name string) string {
	return schemaName + "." + name
}

// dataTypeExactKey is a datatype's exact $defs key.
func dataTypeExactKey(schemaName, name string) string {
	return schemaName + "." + name + ".datatype"
}

// edgeExactKey is a declared association's exact $defs key.
func edgeExactKey(schemaName, owner, relation string) string {
	return schemaName + "." + owner + "." + relation + ".edge"
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

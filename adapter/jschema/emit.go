package jschema

import (
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/schema"
)

// fkTargetPrefix prefixes each foreign-key field of an edge object: an edge
// carries one "_target_<pk_name>" field per primary-key component of the
// target type, matching the field names the instance layer requires when it
// validates edge objects.
const fkTargetPrefix = "_target_"

// buildDocument assembles the complete JSON Schema document for a schema and
// its import closure as an ordered value tree. Envelope member order: $schema,
// $id (only when configured), title, description, type, properties,
// additionalProperties, $defs.
func buildDocument(s *schema.Schema, table *defsTable, cfg config) (val, error) {
	pairs := []kv{{K: "$schema", V: scalar("https://json-schema.org/draft/2020-12/schema")}}
	if cfg.schemaID != "" {
		pairs = append(pairs, kv{K: "$id", V: scalar(cfg.schemaID)})
	}
	pairs = append(
		pairs,
		kv{K: "title", V: scalar(s.Name())},
		kv{K: "description", V: scalar(defaultDescription)},
		kv{K: "type", V: scalar("object")},
	)

	top, err := topLevelProperties(s, table)
	if err != nil {
		return val{}, err
	}
	pairs = append(
		pairs,
		kv{K: "properties", V: top},
		kv{K: "additionalProperties", V: scalar(false)},
	)

	defs, err := buildDefs(table)
	if err != nil {
		return val{}, err
	}
	pairs = append(pairs, kv{K: "$defs", V: defs})
	return object(pairs...), nil
}

// topLevelProperties emits one envelope key per concrete non-part type, keyed by
// its [schema.AddressableTag]: the bare name for an entry-schema type and the
// alias-qualified name for a directly imported one — the two forms the instance
// validator resolves as type tags. A type the entry schema reaches only through
// another import has no tag, so it appears in $defs only. Each key holds the array-of-instances
// shape the object envelope requires.
func topLevelProperties(s *schema.Schema, table *defsTable) (val, error) {
	var props []kv
	for _, sc := range table.orderedSchemas {
		for _, t := range sc.TypesSlice() {
			if t.IsAbstract() || t.IsPart() {
				continue
			}
			tag, ok := schema.AddressableTag(s, t.ID())
			if !ok {
				continue // transitively imported: $defs-only
			}
			key, ok := table.defName(t.ID())
			if !ok {
				return val{}, fmt.Errorf("jschema: no $defs key for type %q", t.Name())
			}
			props = append(props, kv{
				K: tag,
				V: object(kv{K: "type", V: scalar("array")}, kv{K: "items", V: refTo(key)}),
			})
		}
	}
	return object(props...), nil
}

// buildDefs emits the $defs block: non-abstract types first (closure order,
// declaration order within each schema), then one EDGE_ entry per declared
// association, then named DataTypes. Abstract types are skipped — an
// association target must be a concrete non-part type and a composition
// target a part type, so nothing can ever $ref an abstract type; its members
// reach the document flattened into each subtype.
func buildDefs(table *defsTable) (val, error) {
	var entries []kv

	for _, t := range table.orderedTypes {
		if t.IsAbstract() {
			continue
		}
		name, ok := table.defName(t.ID())
		if !ok {
			return val{}, fmt.Errorf("jschema: no $defs key for type %q", t.Name())
		}
		v, err := typeDef(t, table)
		if err != nil {
			return val{}, err
		}
		entries = append(entries, kv{K: name, V: v})
	}

	for _, er := range table.orderedEdges {
		key, ok := table.edgeDefName(er.rel)
		if !ok {
			return val{}, fmt.Errorf("jschema: association %q has no registered EDGE_ $defs key", er.rel.Name())
		}
		v, err := edgeDef(er, table)
		if err != nil {
			return val{}, err
		}
		entries = append(entries, kv{K: key, V: v})
	}

	for _, d := range table.orderedDataTypes {
		name, ok := table.dataTypeDefName(d)
		if !ok {
			return val{}, fmt.Errorf("jschema: no $defs key for datatype %q", d.Name())
		}
		v, err := dataTypeDef(d, table)
		if err != nil {
			return val{}, err
		}
		entries = append(entries, kv{K: name, V: v})
	}

	return object(entries...), nil
}

// typeDef emits one instance-object schema: flattened properties (own +
// inherited), then compositions, then associations, all keyed by their wire
// field name. Required properties and required compositions land in
// "required"; associations never do — their presence is enforced when the
// graph is assembled, not per file, so requiredness is stated in the
// generated description instead. Unknown instance fields are rejected by the
// instance layer, so additionalProperties false is faithful.
func typeDef(t *schema.Type, table *defsTable) (val, error) {
	pairs := []kv{{K: "type", V: scalar("object")}}
	if doc := t.Documentation(); doc != "" {
		pairs = append(pairs, kv{K: "description", V: scalar(doc)})
	}

	var props []kv
	var required []val
	for _, p := range t.AllPropertiesSlice() {
		frag, err := schemaForProperty(p, table.dtPropName)
		if err != nil {
			return val{}, fmt.Errorf("jschema: type %q: %w", t.Name(), err)
		}
		if doc := p.Documentation(); doc != "" {
			if frag, err = withDescription(frag, doc); err != nil {
				return val{}, fmt.Errorf("jschema: type %q property %q: %w", t.Name(), p.Name(), err)
			}
		}
		props = append(props, kv{K: p.Name(), V: frag})
		if p.IsRequired() {
			required = append(required, scalar(p.Name()))
		}
	}
	for _, rel := range t.AllCompositionsSlice() {
		frag, err := compositionFrag(rel, table)
		if err != nil {
			return val{}, fmt.Errorf("jschema: type %q: %w", t.Name(), err)
		}
		props = append(props, kv{K: rel.FieldName(), V: frag})
		if !rel.IsOptional() {
			required = append(required, scalar(rel.FieldName()))
		}
	}
	for _, rel := range t.AllAssociationsSlice() {
		frag, err := associationFrag(rel, table)
		if err != nil {
			return val{}, fmt.Errorf("jschema: type %q: %w", t.Name(), err)
		}
		props = append(props, kv{K: rel.FieldName(), V: frag})
	}

	pairs = append(pairs, kv{K: "properties", V: object(props...)})
	if len(required) > 0 {
		pairs = append(pairs, kv{K: "required", V: array(required...)})
	}
	pairs = append(pairs, kv{K: "additionalProperties", V: scalar(false)})
	return object(pairs...), nil
}

// compositionFrag emits a composition property: always an array of child
// instance objects regardless of multiplicity (the wire shape the instance
// layer expects), with minItems 1 when required (an absent or empty required
// composition is an instance-layer error) and maxItems 1 for to-one (a
// second child under a to-one composition is refused by instance validation,
// E_DUPLICATE_COMPOSED_PK). The child is named by its resolved
// identity, so a composition inherited from a cross-schema parent still
// references the correct part type.
func compositionFrag(rel *schema.Relation, table *defsTable) (val, error) {
	childKey, ok := table.defName(rel.TargetID())
	if !ok {
		return val{}, fmt.Errorf("jschema: composition %q target %q has no $defs key", rel.Name(), rel.TargetID().String())
	}
	pairs := []kv{
		{K: "type", V: scalar("array")},
		{K: "items", V: refTo(childKey)},
	}
	if !rel.IsOptional() {
		pairs = append(pairs, kv{K: "minItems", V: scalar(1)})
	}
	if !rel.IsMany() {
		pairs = append(pairs, kv{K: "maxItems", V: scalar(1)})
	}
	desc := fmt.Sprintf("Composition %s %s → %s.", rel.Name(), multiplicity(rel), childKey)
	if doc := rel.Documentation(); doc != "" {
		desc += " " + doc
	}
	pairs = append(pairs, kv{K: "description", V: scalar(desc)})
	return object(pairs...), nil
}

// associationFrag emits an association property: a single edge object for
// to-one, an array of edge objects for to-many. No requiredness or count
// keywords are emitted — association presence is enforced when the graph is
// assembled, not per file, and a per-file constraint would flag data files
// the instance layer validates cleanly — so the generated description
// carries the multiplicity and, for required associations, where enforcement
// happens. The EDGE_ key is shared by pointer with the declaring owner, so
// an inherited association references the owner's single entry.
func associationFrag(rel *schema.Relation, table *defsTable) (val, error) {
	edgeKey, ok := table.edgeDefName(rel)
	if !ok {
		return val{}, fmt.Errorf("jschema: association %q (owner %q) has no registered EDGE_ $defs key", rel.Name(), rel.Owner())
	}
	targetKey, ok := table.defName(rel.TargetID())
	if !ok {
		return val{}, fmt.Errorf("jschema: association %q target %q has no $defs key", rel.Name(), rel.TargetID().String())
	}
	desc := fmt.Sprintf("Association %s %s → %s.", rel.Name(), multiplicity(rel), targetKey)
	if !rel.IsOptional() {
		desc += " Presence is enforced when the graph is assembled, not per-file."
	}
	if doc := rel.Documentation(); doc != "" {
		desc += " " + doc
	}
	if rel.IsMany() {
		return object(
			kv{K: "type", V: scalar("array")},
			kv{K: "items", V: refTo(edgeKey)},
			kv{K: "description", V: scalar(desc)},
		), nil
	}
	return withDescription(refTo(edgeKey), desc)
}

// edgeDef emits one edge-object schema: the _target_* foreign-key block
// first (one field per primary-key component of the target, each validating
// against the target key's own constraint — a DataType-typed key keeps its
// $ref), then the association's edge properties. Every _target_* field is
// required, plus required edge properties; the target always has at least
// one primary key (schema completion rejects an association whose target has
// none), so "required" is never empty. Unknown edge fields are rejected by
// the instance layer, so additionalProperties false is faithful.
func edgeDef(er edgeRec, table *defsTable) (val, error) {
	var props []kv
	var required []val
	for _, pk := range er.target.PrimaryKeysSlice() {
		frag, err := schemaForProperty(pk, table.dtPropName)
		if err != nil {
			return val{}, fmt.Errorf("jschema: edge %q target %q: %w", er.rel.Name(), er.target.Name(), err)
		}
		name := fkTargetPrefix + pk.Name()
		props = append(props, kv{K: name, V: frag})
		required = append(required, scalar(name))
	}
	for _, ep := range er.rel.PropertiesSlice() {
		frag, err := schemaForProperty(ep, table.dtPropName)
		if err != nil {
			return val{}, fmt.Errorf("jschema: edge %q: %w", er.rel.Name(), err)
		}
		if doc := ep.Documentation(); doc != "" {
			if frag, err = withDescription(frag, doc); err != nil {
				return val{}, fmt.Errorf("jschema: edge %q property %q: %w", er.rel.Name(), ep.Name(), err)
			}
		}
		props = append(props, kv{K: ep.Name(), V: frag})
		if ep.IsRequired() {
			required = append(required, scalar(ep.Name()))
		}
	}
	return object(
		kv{K: "type", V: scalar("object")},
		kv{K: "properties", V: object(props...)},
		kv{K: "required", V: array(required...)},
		kv{K: "additionalProperties", V: scalar(false)},
	), nil
}

// dataTypeDef emits a named DataType's $defs entry: its constraint fragment,
// with a datatype its constraint lists kept as a $ref at any List depth
// (Codes = List<FipsCode> emits items $ref FipsCode), and the DataType's
// documentation as description.
func dataTypeDef(d *schema.DataType, table *defsTable) (val, error) {
	frag, err := dataTypeFragment(d, table)
	if err != nil {
		return val{}, fmt.Errorf("jschema: datatype %q: %w", d.Name(), err)
	}
	if doc := d.Documentation(); doc != "" {
		return withDescription(frag, doc)
	}
	return frag, nil
}

func dataTypeFragment(d *schema.DataType, table *defsTable) (val, error) {
	lists, ac, ok := aliasInLists(d.Constraint())
	if !ok {
		return schemaForConstraint(d.Constraint())
	}
	name, ok := table.innerDataTypeName(d)
	if !ok {
		return val{}, fmt.Errorf("no registered $defs key for the datatype it references (%s)", ac.DataTypeName())
	}
	return wrapInLists(lists, refTo(name)), nil
}

// withDescription attaches desc as the fragment's "description", MERGING with
// one the constraint mapper already produced rather than appending a second.
//
// A `Timestamp["<layout>"]` carries the source layout as a description, so a
// documented property of that type rendered two "description" keys. Every
// last-one-wins reader — including this package's own selfCheck, which decodes
// into map[string]any — then dropped the layout the section promises.
//
// The separator matches compositionFrag and associationFrag: generated text
// first, doc-comment appended after a single space. The result holds its own
// member slice, so v is unchanged. Only an object carries members; any other
// fragment is a generator bug, surfaced as an error.
func withDescription(v val, desc string) (val, error) {
	if v.kind != kindObject {
		return val{}, fmt.Errorf("jschema: description %q attached to a fragment that is not an object", desc)
	}
	members := slices.Clone(v.obj)
	for i, member := range members {
		if member.K != "description" {
			continue
		}
		if existing, ok := member.V.stringValue(); ok && existing != "" {
			desc = existing + " " + desc
		}
		members[i] = kv{K: "description", V: scalar(desc)}
		return object(members...), nil
	}
	return object(append(members, kv{K: "description", V: scalar(desc)})...), nil
}

// multiplicity renders a relation's forward multiplicity in its DSL source
// form: (_) optional-one, (one) required-one, (many) optional-many,
// (one:many) required-many.
func multiplicity(rel *schema.Relation) string {
	switch {
	case rel.IsMany() && rel.IsOptional():
		return "(many)"
	case rel.IsMany():
		return "(one:many)"
	case rel.IsOptional():
		return "(_)"
	default:
		return "(one)"
	}
}

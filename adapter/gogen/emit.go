package gogen

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// closureTypes returns every type across the schema closure (entry + imports),
// so emission is identical for single-file and imported schemas.
func (g *generator) closureTypes() []*schema.Type {
	var out []*schema.Type
	for _, sc := range g.schema.Closure() {
		out = append(out, sc.TypesSlice()...)
	}
	return out
}

// emitTypes writes one struct per type in the closure, deterministic order.
// Abstract and part types are emitted too (part types are referenced by
// compositions; abstract types document the schema even though inheritance is
// flattened, so no field embeds them).
func (g *generator) emitTypes() error {
	for _, t := range g.closureTypes() {
		if err := g.emitType(t); err != nil {
			return err
		}
	}
	return nil
}

// emitNamedTypes emits the temporal types, a named Go type for every DataType,
// and one for every inline-enum field, each enum with its value constants. An
// inline enum inherited by several types yields one named type per owner.
func (g *generator) emitNamedTypes() error {
	g.emitTemporalTypes()
	for _, sc := range g.schema.Closure() {
		for _, dt := range sc.DataTypesSlice() {
			name, ok := g.names.goDataType(dt)
			if !ok {
				return fmt.Errorf("gogen: no Go name for datatype %q", dt.Name())
			}
			// A temporal DataType is its own carrier: a defined type over the
			// Date struct would inherit no method, so it embeds time.Time itself.
			if layout := temporalLayout(dt.Constraint()); layout != "" {
				g.emitTemporalDecl(name, layout)
				continue
			}
			if isDefaultTimestamp(dt.Constraint()) {
				g.emitDefaultTimestampDecl(name)
				continue
			}
			if lc, isList := dt.Constraint().(schema.ListConstraint); isList {
				if ec, ok := inlineEnum(lc); ok {
					elem, ok := g.names.goDataTypeElement(dt)
					if !ok {
						return fmt.Errorf("gogen: no Go name for datatype %q", dt.Name())
					}
					fmt.Fprintf(g.buf, "type %s string\n\n", elem)
					g.emitEnumConsts(elem, ec.Values())
				}
			}
			base, err := g.dataTypeBase(sc, dt)
			if err != nil {
				return fmt.Errorf("gogen: datatype %q: %w", dt.Name(), err)
			}
			fmt.Fprintf(g.buf, "type %s %s\n\n", name, base)
			if resolved := schema.ResolveAlias(dt.Constraint()); resolved.Kind() == schema.KindEnum {
				ec, ok := resolved.(schema.EnumConstraint)
				if !ok {
					return fmt.Errorf("gogen: datatype %q has Enum kind without EnumConstraint", dt.Name())
				}
				g.emitEnumConsts(name, ec.Values())
			}
		}
	}
	for _, t := range g.closureTypes() {
		if err := g.emitInlineEnums(g.typeOwner(t), t.AllPropertiesSlice()); err != nil {
			return err
		}
	}
	for _, e := range g.edges {
		if err := g.emitInlineEnums(g.edgeOwner(e.rel), e.rel.PropertiesSlice()); err != nil {
			return err
		}
	}
	return nil
}

// emitInlineEnums emits the named type and value constants of every property
// in props that declares an enum inline, as its own constraint or as the
// innermost element of the Lists it nests.
func (g *generator) emitInlineEnums(owner fieldOwner, props []*schema.Property) error {
	for _, p := range props {
		ec, ok := inlineEnum(p.Constraint())
		if !ok {
			continue
		}
		name := g.names.goInlineEnum(owner.enum, p)
		fmt.Fprintf(g.buf, "type %s string\n\n", name)
		g.emitEnumConsts(name, ec.Values())
	}
	return nil
}

// inlineEnum returns the enum c declares inline: c itself, or the innermost
// element of the Lists it nests. A DataType reference at any depth is not
// inline, whatever it resolves to.
func inlineEnum(c schema.Constraint) (schema.EnumConstraint, bool) {
	for {
		if isAlias(c) {
			return schema.EnumConstraint{}, false
		}
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			ec, ok := c.(schema.EnumConstraint)
			return ec, ok
		}
		c = lc.Element()
	}
}

// typeOwner is the owner of a type's struct fields.
func (g *generator) typeOwner(t *schema.Type) fieldOwner {
	name, _ := g.names.goType(t.ID())
	return fieldOwner{label: "type " + strconv.Quote(t.Name()), enum: enumOwner{goName: name, key: "type\x00" + t.ID().String()}}
}

// edgeOwner is the owner of an association's EDGE_ struct fields.
func (g *generator) edgeOwner(rel *schema.Relation) fieldOwner {
	name := g.edgeNames[rel]
	return fieldOwner{label: "association " + strconv.Quote(rel.Name()), enum: enumOwner{goName: name, key: "edge\x00" + name}}
}

// fieldOwner is the struct a field is emitted into: a label for errors and
// the owner its inline enums are named for.
type fieldOwner struct {
	label string
	enum  enumOwner
}

// dataTypeBase returns the Go type a non-temporal DataType is defined over. A
// List whose innermost element names another DataType keeps that DataType's Go
// name at every depth, resolved in sc, the schema that declares dt.
func (g *generator) dataTypeBase(sc *schema.Schema, dt *schema.DataType) (string, error) {
	lc, ok := dt.Constraint().(schema.ListConstraint)
	if !ok {
		return g.goBaseType(dt.Constraint())
	}
	elemEnum := func() (string, error) {
		if g.collect != nil {
			return "", nil
		}
		name, ok := g.names.goDataTypeElement(dt)
		if !ok {
			return "", fmt.Errorf("gogen: no Go name for datatype %q", dt.Name())
		}
		return name, nil
	}
	return g.listType(lc, elemEnum, func(ac schema.AliasConstraint) (string, error) {
		qualifier, name, qualified := strings.Cut(ac.DataTypeName(), ".")
		if !qualified {
			qualifier, name = "", qualifier
		}
		target, ok := sc.ResolveDataType(schema.NewDataTypeRef(qualifier, name, location.Span{}))
		if !ok {
			return "", fmt.Errorf("gogen: list element names unresolved datatype %q", ac.DataTypeName())
		}
		goName, ok := g.names.goDataType(target)
		if !ok {
			return "", fmt.Errorf("gogen: no Go name for datatype %q", target.Name())
		}
		return goName, nil
	})
}

// listType renders a List. When its innermost element names a DataType, the
// result is one "[]" per level around elemName's answer; when it declares an
// enum inline, around elemEnum's, whose empty answer is the collect pass
// declining to reserve a name; otherwise it is goBaseType's rendering of the
// whole List.
func (g *generator) listType(lc schema.ListConstraint, elemEnum func() (string, error), elemName func(schema.AliasConstraint) (string, error)) (string, error) {
	depth := 1
	elem := lc.Element()
	for {
		inner, ok := elem.(schema.ListConstraint)
		if !ok {
			break
		}
		depth++
		elem = inner.Element()
	}
	var name string
	switch e := elem.(type) {
	case schema.AliasConstraint:
		n, err := elemName(e)
		if err != nil {
			return "", err
		}
		name = n
	case schema.EnumConstraint:
		n, err := elemEnum()
		if err != nil || n == "" {
			return "", err
		}
		name = n
	default:
		return g.goBaseType(lc)
	}
	return strings.Repeat("[]", depth) + name, nil
}

// emitEnumConsts emits one `const <EnumGoName><Value> <EnumGoName> = "<value>"`
// per value, in declaration order, each name reserved in the shared namespace
// so a clash with a sibling value or any other declaration takes a suffix.
func (g *generator) emitEnumConsts(enumGoName string, values []string) {
	g.buf.WriteString("const (\n")
	for _, v := range values {
		constName := g.names.reserve(enumGoName + g.names.ident(v))
		fmt.Fprintf(g.buf, "%s %s = %s\n", constName, enumGoName, strconv.Quote(v))
	}
	g.buf.WriteString(")\n\n")
}

// registerDataTypeFields records the DataType Go name of every type and edge
// property whose DataTypeRef is set, keyed by the property pointer and resolved
// in the declaring schema, since an inherited property's ref is relative to
// its parent's schema. A List property carries its innermost element's ref.
func (g *generator) registerDataTypeFields() error {
	record := func(sc *schema.Schema, kind, owner string, p *schema.Property) error {
		ref := p.DataTypeRef()
		if ref.IsZero() {
			return nil
		}
		dt, ok := sc.ResolveDataType(ref)
		if !ok {
			return fmt.Errorf("gogen: %s %q property %q references unresolved datatype %q", kind, owner, p.Name(), ref.String())
		}
		name, ok := g.names.goDataType(dt)
		if !ok {
			return fmt.Errorf("gogen: no Go name for datatype %q", dt.Name())
		}
		g.dtFieldNames[p] = name
		return nil
	}
	for _, sc := range g.schema.Closure() {
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

// registerEdges names every declared association's EDGE_ struct
// "EDGE_<Owner>_<edge>_<Target>", keyed by the relation pointer an inheriting
// type shares, and reserves the name in the shared namespace. The target
// resolves in the declaring schema, where the relation's ref is written.
func (g *generator) registerEdges() error {
	for _, sc := range g.schema.Closure() {
		for _, t := range sc.TypesSlice() {
			ownerName, ok := g.names.goType(t.ID())
			if !ok {
				return fmt.Errorf("gogen: no Go name for type %q", t.Name())
			}
			for _, rel := range t.AssociationsSlice() { // OWN associations only
				target, ok := sc.ResolveType(rel.Target())
				if !ok {
					return fmt.Errorf("gogen: association %q target %q unresolved", rel.Name(), rel.Target().String())
				}
				// The target is guaranteed to have a primary key: schema completion rejects
				// both a PK-less concrete type and an association whose target has no primary
				// key (diag.E_NO_PRIMARY_KEY), so an association never resolves to a PK-less
				// target here — the EDGE_ Where block always has at least one field.
				targetName, ok := g.names.goType(target.ID())
				if !ok {
					return fmt.Errorf("gogen: association %q target type %q has no Go name", rel.Name(), target.Name())
				}
				g.edgeNames[rel] = g.names.reserve("EDGE_" + ownerName + "_" + rel.FieldName() + "_" + targetName)
				g.edges = append(g.edges, edgeRec{rel: rel, target: target})
			}
		}
	}
	return nil
}

// emitComposition writes a composition's field. The child is named by
// rel.TargetID, which is absolute, because an inherited composition's syntactic
// ref is relative to its declaring schema.
func (g *generator) emitComposition(rel *schema.Relation, used map[string]bool) error {
	childName, ok := g.names.goType(rel.TargetID())
	if !ok {
		return fmt.Errorf("gogen: composition %q target %q has no Go name", rel.Name(), rel.TargetID().String())
	}
	field := g.names.field(rel.FieldName(), used)
	// Every composition is a slice, (one) included: the adapter/json parser
	// and writer exchange an array for every multiplicity, and a slice also
	// keeps a required-one composition cycle legal as a Go type.
	fmt.Fprintf(g.buf, "%s []*%s %s\n", field, childName, jsonTag(rel.FieldName(), relationOmit))
	return nil
}

// emitAssociation writes an association's field, which points at the EDGE_
// struct its declaring type emits.
func (g *generator) emitAssociation(rel *schema.Relation, used map[string]bool) error {
	edgeName, ok := g.edgeNames[rel]
	if !ok {
		return fmt.Errorf("gogen: association %q (owner %q) has no registered EDGE_ name", rel.Name(), rel.Owner())
	}
	field := g.names.field(rel.FieldName(), used)
	star := "*"
	if rel.IsMany() {
		star = "[]*"
	}
	// A pointer even for a required (one): a required-one relation cycle
	// rendered as a value type would be an illegal recursive Go type.
	fmt.Fprintf(g.buf, "%s %s%s %s\n", field, star, edgeName, jsonTag(rel.FieldName(), relationOmit))
	return nil
}

// emitGraph writes the Graph aggregate: one slice field per concrete type the
// entry schema can name, keyed by its [schema.AddressableTag], which no two such
// types share.
func (g *generator) emitGraph() error {
	g.buf.WriteString("type Graph struct {\n")
	for _, t := range g.closureTypes() {
		if t.IsAbstract() || t.IsPart() {
			continue
		}
		key, ok := schema.AddressableTag(g.schema, t.ID())
		if !ok {
			continue
		}
		name, ok := g.names.goType(t.ID())
		if !ok {
			return fmt.Errorf("gogen: no Go name for type %q", t.Name())
		}
		fmt.Fprintf(g.buf, "%s []*%s %s\n", name, name, jsonTag(key, "omitempty"))
	}
	g.buf.WriteString("}\n\n")
	return nil
}

// emitEdgeStructs writes one struct per declared association: the target's
// primary keys as flattened "_target_" fields, then the edge properties, the
// shape adapter/json exchanges.
func (g *generator) emitEdgeStructs() error {
	for _, e := range g.edges {
		fmt.Fprintf(g.buf, "type %s struct {\n", g.edgeNames[e.rel])

		used := map[string]bool{}
		// The key fields come first, so a later edge-property edit takes the
		// collision suffix and a key field's Go name never moves.
		for _, pk := range e.target.PrimaryKeysSlice() {
			typ, err := g.goFieldType(g.typeOwner(e.target), pk)
			if err != nil {
				return err
			}
			fmt.Fprintf(g.buf, "%s %s %s\n", g.names.field("Target"+g.names.ident(pk.Name()), used), typ, jsonTag("_target_"+pk.Name(), ""))
		}
		owner := g.edgeOwner(e.rel)
		for _, p := range e.rel.PropertiesSlice() {
			if err := g.emitField(owner, p, used); err != nil {
				return err
			}
		}
		g.buf.WriteString("}\n\n")
	}
	return nil
}

// emitSerializedModel embeds every source in the closure under its key, the
// SerializedSources/SerializedEntry pair and SchemaHash. It records the store,
// the entry key and the hash as emitted, which is what finish checks.
func (g *generator) emitSerializedModel() error {
	srcs := g.schema.Sources()
	ids := srcs.SourceIDs()
	if len(ids) == 0 {
		return errors.New("gogen: schema is not source-backed; Marshal requires a schema loaded via Load/LoadString/LoadSourcesWithEntry")
	}
	keys, err := g.embeddedKeys(ids)
	if err != nil {
		return err
	}
	g.entryKey = keys[g.schema.SourceID()]
	g.embedded = make(map[string][]byte, len(ids))

	g.buf.WriteString("// serializedSources holds every source in the import closure, keyed by\n")
	g.buf.WriteString("// the name the re-load looks it up by, as verbatim .yammm text. Read it\n")
	g.buf.WriteString("// through SerializedSources below.\n")
	g.buf.WriteString("var serializedSources = map[string]string{\n")
	for _, id := range ids { // SourceIDs() is sorted/deterministic
		content, ok := srcs.ContentBySource(id)
		if !ok {
			return fmt.Errorf("gogen: source %s content unavailable", id)
		}
		key := keys[id]
		g.embedded[key] = content
		fmt.Fprintf(g.buf, "%s: %s,\n", strconv.Quote(key), strconv.Quote(string(content)))
	}
	g.buf.WriteString("}\n\n")

	g.emitUniformSources(g.entryKey)

	g.schemaHash = schema.StructuralHash(g.schema)
	fmt.Fprintf(g.buf, "const SchemaHash = %q\n", g.schemaHash)
	return nil
}

// emitUniformSources writes the SerializedSources/SerializedEntry pair. The
// accessor copies the store on every call, so no caller can mutate it.
func (g *generator) emitUniformSources(entryKey string) {
	g.buf.WriteString("// SerializedEntry is the entry-point key into SerializedSources.\n")
	fmt.Fprintf(g.buf, "const SerializedEntry = %s\n\n", strconv.Quote(entryKey))

	g.buf.WriteString("// SerializedSources returns every source in the import closure, keyed by\n")
	g.buf.WriteString("// the name the re-load looks it up by. Re-load with:\n")
	g.buf.WriteString("//\n")
	g.buf.WriteString("//\tschema.LoadSourcesWithEntry(ctx, SerializedSources(), SerializedEntry, \"\",\n")
	g.buf.WriteString("//\t\tschema.WithSourcesOnly(true), schema.WithSyntheticRoot(" + strconv.Quote(recipeRoot) + "))\n")
	g.buf.WriteString("//\n")
	g.buf.WriteString("// The synthetic root keeps the loaded type identities stable: no working\n")
	g.buf.WriteString("// directory, checkout or mount point enters them. Any root of that form\n")
	g.buf.WriteString("// serves; generation verified this one.\n")
	g.buf.WriteString("func SerializedSources() map[string][]byte {\n")
	g.buf.WriteString("m := make(map[string][]byte, len(serializedSources))\n")
	g.buf.WriteString("for k, v := range serializedSources {\n")
	g.buf.WriteString("m[k] = []byte(v)\n")
	g.buf.WriteString("}\n")
	g.buf.WriteString("return m\n")
	g.buf.WriteString("}\n\n")
}

// embeddedKeys assigns every source its key; the package doc's Embedded Source
// section states the rule. A source under two keys, or two sources under one
// key, is an error naming both, since the re-load reads the store as one map.
func (g *generator) embeddedKeys(ids []location.SourceID) (map[location.SourceID]string, error) {
	keys := map[location.SourceID]string{}
	owners := map[string]location.SourceID{}
	via := map[location.SourceID]string{}
	assign := func(id location.SourceID, key, how string) error {
		if other, taken := owners[key]; taken {
			return fmt.Errorf("gogen: sources %s (%s) and %s (%s) both take the embedded key %q; an embedded model holds one source per key", other, via[other], id, how, key)
		}
		keys[id], owners[key], via[id] = key, id, how
		return nil
	}
	entry := g.schema.SourceID()
	k, err := g.keys.key(entry)
	if err != nil {
		return nil, err
	}
	if err := assign(entry, k, "the entry"); err != nil {
		return nil, err
	}
	for queue := []*schema.Schema{g.schema}; len(queue) > 0; queue = queue[1:] {
		sch := queue[0]
		for _, imp := range sch.ImportsSlice() {
			target := imp.ResolvedSourceID()
			ik, err := schema.SyntheticImportKey(keys[sch.SourceID()], imp.Path())
			if err != nil {
				return nil, fmt.Errorf("gogen: import %q in %s: %w", imp.Path(), sch.SourceID(), err)
			}
			how := fmt.Sprintf("imported as %q by %s", imp.Path(), sch.SourceID())
			if have, seen := keys[target]; seen {
				if have != ik {
					return nil, fmt.Errorf("gogen: source %s is %s under the key %q and %s under the key %q; an embedded model re-loads a source under one key, so import it by one path", target, via[target], have, how, ik)
				}
				continue
			}
			if err := assign(target, ik, how); err != nil {
				return nil, err
			}
			if imp.Schema() != nil {
				queue = append(queue, imp.Schema())
			}
		}
	}
	for _, id := range ids {
		if _, ok := keys[id]; ok {
			continue
		}
		k, err := g.keys.key(id)
		if err != nil {
			return nil, err
		}
		if err := assign(id, k, "under the root"); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// keyRoot is the root embedded keys are written against: a synthetic root, or
// a directory identity. Both are zero for a synthetic source loaded with no root.
type keyRoot struct {
	synthetic string
	dir       location.CanonicalPath
}

// resolveKeyRoot reads the root from s, or takes a file entry's directory when
// the load recorded none. A file root is recorded as a host path and is
// re-derived as an identity, since a host path's bytes can differ from it.
func resolveKeyRoot(s *schema.Schema) (keyRoot, error) {
	root := s.ModuleRoot()
	switch {
	case root != "" && location.ValidateSyntheticSourceID(root) == nil:
		return keyRoot{synthetic: root}, nil
	case root != "":
		cp, err := location.NewCanonicalPath(root)
		if err != nil {
			return keyRoot{}, fmt.Errorf("gogen: module root %q: %w", root, err)
		}
		return keyRoot{dir: cp}, nil
	}
	if cp, ok := s.SourceID().CanonicalPath(); ok {
		return keyRoot{dir: cp.Dir()}, nil
	}
	return keyRoot{}, nil
}

// key returns id's key under the root, or for a synthetic source loaded with
// no root, the only source such a load holds, its base name. Where no relative
// form exists key returns an error, since a key is never a machine path.
func (k keyRoot) key(id location.SourceID) (string, error) {
	switch {
	case k.synthetic != "":
		if key, ok := strings.CutPrefix(id.String(), k.synthetic+"/"); ok {
			return key, nil
		}
	case !k.dir.IsZero():
		if key, ok := id.Rel(k.dir); ok {
			return key, nil
		}
	case !id.IsFilePath():
		name := id.String()
		base := name[strings.LastIndexAny(name, `/\`)+1:]
		if base != "" && base != "." && base != ".." {
			return base, nil
		}
		return "", fmt.Errorf("gogen: source %q names no file, so it has no key; load it under a name ending in a file name", name)
	}
	return "", fmt.Errorf("gogen: source %s has no key relative to the schema's root; generated keys are never generation-machine paths", id)
}

func (g *generator) emitType(t *schema.Type) error {
	goName, ok := g.names.goType(t.ID())
	if !ok {
		return fmt.Errorf("gogen: no Go name for type %q", t.Name())
	}
	g.emitDoc(t.Documentation()) // schema doc-comment -> Go doc-comment, where present
	fmt.Fprintf(g.buf, "type %s struct {\n", goName)
	used := map[string]bool{} // properties and relations share one namespace
	owner := g.typeOwner(t)
	for _, p := range t.AllPropertiesSlice() {
		if err := g.emitField(owner, p, used); err != nil {
			return err
		}
	}
	for _, rel := range t.AllCompositionsSlice() {
		if err := g.emitComposition(rel, used); err != nil {
			return err
		}
	}
	for _, rel := range t.AllAssociationsSlice() {
		if err := g.emitAssociation(rel, used); err != nil {
			return err
		}
	}
	g.buf.WriteString("}\n\n")
	return nil
}

// emitField writes one property's struct field into owner's struct, whose
// field namespace is used.
func (g *generator) emitField(owner fieldOwner, p *schema.Property, used map[string]bool) error {
	typ, err := g.goFieldType(owner, p)
	if err != nil {
		return fmt.Errorf("%s property %q: %w", owner.label, p.Name(), err)
	}
	g.emitDoc(p.Documentation())
	fmt.Fprintf(g.buf, "%s %s %s\n", g.names.field(p.Name(), used), typ, jsonTag(p.Name(), propertyOmit(p)))
	return nil
}

// relationOmit is every relation field's json option. A relation is never
// written null, so an unset one is left out; the parser refuses a null, and a
// required relation's absence is refused where yammm checks presence.
const relationOmit = "omitempty"

// propertyOmit returns p's json option. An optional slice takes omitzero, so a
// present empty list, which the library keeps, is written back rather than
// dropped as omitempty would.
func propertyOmit(p *schema.Property) string {
	switch {
	case !p.IsOptional():
		return ""
	case isSliceKind(schema.ResolveAlias(p.Constraint()).Kind()):
		return "omitzero"
	default:
		return "omitempty"
	}
}

// goFieldType returns the Go type of p's field in owner's struct: a pointer for
// an optional non-slice, a named DataType under its own name at any List depth,
// and an inline enum under its own name at any List depth.
func (g *generator) goFieldType(owner fieldOwner, p *schema.Property) (string, error) {
	c := p.Constraint()
	resolved := schema.ResolveAlias(c)

	var typ string
	switch {
	case isAlias(c):
		// A property whose annotations merge across ancestors reaches emission
		// as a copy in no type's own slice, so the table is read by Origin().
		name, ok := g.dtFieldNames[p.Origin()]
		if !ok {
			dtName := "?"
			if ac, isA := c.(schema.AliasConstraint); isA {
				dtName = ac.DataTypeName()
			}
			return "", fmt.Errorf("gogen: no registered Go name for datatype property %q (%s)", p.Name(), dtName)
		}
		typ = name
	case resolved.Kind() == schema.KindEnum:
		if g.collect != nil {
			// An inline enum names no temporal type, and reserving its name here
			// would move it ahead of the layouts in the shared namespace.
			return "", nil
		}
		typ = g.names.goInlineEnum(owner.enum, p)
	case resolved.Kind() == schema.KindList:
		lc, ok := resolved.(schema.ListConstraint)
		if !ok {
			return "", errors.New("gogen: List kind without ListConstraint")
		}
		elemEnum := func() (string, error) {
			if g.collect != nil {
				// As the scalar arm above: the name is reserved after the
				// layouts, never during the collect pass.
				return "", nil
			}
			return g.names.goInlineEnum(owner.enum, p), nil
		}
		t, err := g.listType(lc, elemEnum, func(ac schema.AliasConstraint) (string, error) {
			// The parser records a List property's innermost element ref as
			// the property's own, so the table holds the element's name.
			if name, ok := g.dtFieldNames[p.Origin()]; ok {
				return name, nil
			}
			return "", fmt.Errorf("gogen: no registered Go name for list element datatype %q of property %q", ac.DataTypeName(), p.Name())
		})
		if err != nil {
			return "", err
		}
		typ = t
	default:
		base, err := g.goBaseType(c)
		if err != nil {
			return "", err
		}
		typ = base
	}

	if p.IsOptional() && !isSliceKind(resolved.Kind()) {
		return "*" + typ, nil
	}
	return typ, nil
}

// isAlias reports whether a constraint is a DataType reference (AliasConstraint).
func isAlias(c schema.Constraint) bool {
	_, ok := c.(schema.AliasConstraint)
	return ok
}

// jsonTag builds a json struct tag carrying the wire name and, when opt is
// set, that option.
func jsonTag(name, opt string) string {
	if opt != "" {
		name += "," + opt
	}
	return fmt.Sprintf("`json:%q`", name)
}

// emitDoc writes a yammm doc-comment as Go line comments, one per line. The
// indentation every continuation line shares is removed, since the doc-comment
// formatter reads an indented line as a code block; deeper indentation stays.
func (g *generator) emitDoc(doc string) {
	if doc == "" {
		return
	}
	for line := range strings.SplitSeq(dedentContinuation(doc), "\n") {
		fmt.Fprintf(g.buf, "// %s\n", strings.TrimRight(line, " \t"))
	}
}

// dedentContinuation removes from every line after the first the leading
// white space all of them that hold text share. A block comment's first line
// follows its opening delimiter, so it carries none of that indentation.
func dedentContinuation(doc string) string {
	lines := strings.Split(doc, "\n")
	prefix, found := "", false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !found {
			prefix, found = indent, true
		}
		for !strings.HasPrefix(indent, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.TrimPrefix(lines[i], prefix)
	}
	return strings.Join(lines, "\n")
}

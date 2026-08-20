package gogen

import (
	"errors"
	"fmt"
	"path/filepath"
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

// emitNamedTypes emits a named Go type for every DataType (type <Name> <base>, plus
// value constants when the DataType resolves to an Enum) and for every inline-enum
// property (type <Owner><Field> string + value constants). It runs before emitTypes
// so the named types precede the structs that reference them. An inline enum
// inherited by N concrete types yields N named enum types (keyed by owning type over
// AllPropertiesSlice) — the inline-enum analog of property flattening.
func (g *generator) emitNamedTypes() error {
	for _, sc := range g.schema.Closure() {
		for _, dt := range sc.DataTypesSlice() {
			name, ok := g.names.goDataType(dt)
			if !ok {
				return fmt.Errorf("gogen: no Go name for datatype %q", dt.Name())
			}
			base, err := goBaseType(dt.Constraint())
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
		for _, p := range t.AllPropertiesSlice() {
			c := p.Constraint()
			if isAlias(c) {
				continue
			}
			resolved := schema.ResolveAlias(c)
			if resolved.Kind() != schema.KindEnum {
				continue
			}
			ec, ok := resolved.(schema.EnumConstraint)
			if !ok {
				return fmt.Errorf("gogen: property %q has Enum kind without EnumConstraint", p.Name())
			}
			name := g.names.goInlineEnum(t, p, g.initialisms)
			fmt.Fprintf(g.buf, "type %s string\n\n", name)
			g.emitEnumConsts(name, ec.Values())
		}
	}
	return nil
}

// emitEnumConsts emits one `const <EnumGoName><Value> <EnumGoName> = "<value>"` per
// value, in declaration order. Each const name is reserved in the shared namespace,
// so a value that sanitizes to empty ("" -> "X" via goExportedIdent), collides with
// a sibling value, or collides with any other identifier is suffixed
// deterministically — never a duplicate or conflicting const.
func (g *generator) emitEnumConsts(enumGoName string, values []string) {
	g.buf.WriteString("const (\n")
	for _, v := range values {
		constName := g.names.reserve(enumGoName + goExportedIdent(v, g.initialisms))
		fmt.Fprintf(g.buf, "%s %s = %s\n", constName, enumGoName, strconv.Quote(v))
	}
	g.buf.WriteString(")\n\n")
}

// registerDataTypeFields is a pre-pass (run before emitTypes) that resolves the Go
// named-type for every DataType-typed field — type properties AND edge (association)
// properties — keyed by the property POINTER. Each is resolved in its DECLARING
// schema (sc), so a property inherited from a cross-schema parent (whose DataTypeRef
// is relative to the parent's schema, not the emitting type's) maps to the correct
// named DataType. Fields whose type is not a named DataType (primitive, inline enum,
// List of a primitive or inline enum) have a zero DataTypeRef and are skipped. A
// list-of-DataType property carries its *element's* DataTypeRef, so it is registered
// here too (goListType wraps the recorded name in "[]").
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

// registerEdges is a pre-pass (run before emitTypes) that assigns every DECLARED
// association an owner-qualified EDGE_ Go name —
// "EDGE_<OwnerGoName>_<edge>_<TargetGoName>" — using the table's collision-resolved
// Go names for the declaring (owner) type and the target. It walks each type's OWN
// associations (AssociationsSlice, NOT the All*Slice) and keys the name by the
// relation POINTER. Because an inherited association is the SAME pointer in the
// declaring type's own slice and in every subtype's AllAssociationsSlice, a child's
// field (emitted later via emitAssociation) references the exact EDGE_ struct the
// declaring parent emits — one struct per declared association, named for the owner.
// Names are unique by construction (owner Go names are unique per type; field names
// are unique within a type), so no dedup or shape-merge is needed. Resolving the
// target against the DECLARING schema sc (not the emitting type's schema) is what
// makes an association inherited from a cross-schema parent resolve correctly.
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
				g.edgeNames[rel] = "EDGE_" + ownerName + "_" + rel.FieldName() + "_" + targetName
				g.edges = append(g.edges, edgeRec{rel: rel, target: target})
			}
		}
	}
	return nil
}

// emitComposition writes an inline ownership field to the composed child struct.
// The child is named by its RESOLVED identity (rel.TargetID(), set for every
// relation during schema completion), NOT by re-resolving the syntactic ref against
// the emitting type's schema. This matters because ResolveType is local to the
// schema it is called on, while AllCompositionsSlice may carry a composition
// INHERITED from a cross-schema parent — whose target ref is relative to the
// parent's schema, not the emitting type's. TargetID is absolute, so it names the
// correct child regardless. The target type is in the closure, so the name table
// has it.
func (g *generator) emitComposition(rel *schema.Relation, used map[string]int) error {
	childName, ok := g.names.goType(rel.TargetID())
	if !ok {
		return fmt.Errorf("gogen: composition %q target %q has no Go name", rel.Name(), rel.TargetID().String())
	}
	field := goField(rel.FieldName(), used, g.initialisms)
	star := "*"
	if rel.IsMany() {
		star = "[]*"
	}
	// omitempty reflects requiredness (rel.IsOptional); the pointer/slice (star) is
	// always kept regardless of multiplicity, because a required-one composition cycle
	// rendered as a value type would be an illegal recursive Go type.
	fmt.Fprintf(g.buf, "%s %s%s %s\n", field, star, childName, jsonTag(rel.FieldName(), rel.IsOptional()))
	return nil
}

// emitAssociation writes the relation field referencing the association's
// owner-qualified EDGE_ struct. The EDGE_ Go name was assigned in registerEdges
// (keyed by the relation pointer, shared between the declaring type and every type
// that inherits the association), so a field on a child type points at the same
// EDGE_ struct the declaring parent emits.
func (g *generator) emitAssociation(rel *schema.Relation, used map[string]int) error {
	edgeName, ok := g.edgeNames[rel]
	if !ok {
		return fmt.Errorf("gogen: association %q (owner %q) has no registered EDGE_ name", rel.Name(), rel.Owner())
	}
	field := goField(rel.FieldName(), used, g.initialisms)
	star := "*"
	if rel.IsMany() {
		star = "[]*"
	}
	// omitempty reflects requiredness (rel.IsOptional); the pointer/slice is always
	// kept regardless of multiplicity, because a required-one relation cycle rendered
	// as a value type would be an illegal recursive Go type.
	fmt.Fprintf(g.buf, "%s %s%s %s\n", field, star, edgeName, jsonTag(rel.FieldName(), rel.IsOptional()))
	return nil
}

// emitGraph writes the Graph aggregate: one slice field per CONCRETE type across the
// closure (not abstract, not part), the Go field named through the table and its JSON
// key in lower_snake. A two-pass emit lets the lossy snake-cast fall back to the
// unique Go type name on collision (ABCDef and AbcDef both -> abc_def), so Graph keys
// stay unique by construction.
func (g *generator) emitGraph() error {
	type field struct{ goName, key string }
	var fields []field
	keyCount := map[string]int{}
	for _, t := range g.closureTypes() {
		if t.IsAbstract() || t.IsPart() {
			continue
		}
		name, ok := g.names.goType(t.ID())
		if !ok {
			return fmt.Errorf("gogen: no Go name for type %q", t.Name())
		}
		key := goSnakeKey(name)
		keyCount[key]++
		fields = append(fields, field{goName: name, key: key})
	}
	g.buf.WriteString("type Graph struct {\n")
	for _, f := range fields {
		key := f.key
		if keyCount[f.key] > 1 {
			key = f.goName // lossy snake-cast collided; the unique Go name keeps keys unique
		}
		fmt.Fprintf(g.buf, "%s []*%s %s\n", f.goName, f.goName, jsonTag(key, true))
	}
	g.buf.WriteString("}\n\n")
	return nil
}

// emitEdgeStructs writes one struct per declared association, using the
// owner-qualified EDGE_ name assigned in registerEdges. Edge-property and Where-block
// PK Go types come from goFieldType, which reads the DataType names
// registerDataTypeFields resolved in each member's declaring schema — so no "current
// schema" is needed here.
func (g *generator) emitEdgeStructs() error {
	for _, e := range g.edges {
		fmt.Fprintf(g.buf, "type %s struct {\n", g.edgeNames[e.rel])

		used := map[string]int{}      // edge struct's field namespace
		jsonKeys := map[string]bool{} // edge struct's JSON wire keys (property names, verbatim)
		for _, p := range e.rel.PropertiesSlice() {
			if err := g.emitField(nil, p, used); err != nil { // nil owner: edge props are scalar
				return err
			}
			jsonKeys[p.Name()] = true
		}

		// Where block: target PK fields rendered faithfully — a PK whose type is a
		// named DataType emits the named type, not its primitive (its Go name was
		// recorded by registerDataTypeFields in the target's declaring schema). PKs are
		// required and limited to String/UUID/Date/Timestamp (or DataTypes thereof), so
		// goFieldType yields the named DataType or the primitive — never a pointer,
		// list, or inline enum.
		//
		// The block's wire key is "where" unless an edge property already claims it (a
		// property literally named "where"): two fields sharing a JSON key make
		// encoding/json drop BOTH at marshal time, and go/types does not flag duplicate
		// struct tags. On a clash the key falls back to the block's unique Go field name
		// (always upper-case-leading, so it cannot re-collide with a property's
		// lower-case-leading wire key) — mirroring emitGraph's lossy-collision strategy.
		// The edge property's own "where" key is canonical and never altered.
		whereField := goField("Where", used, g.initialisms)
		whereKey := "where"
		if jsonKeys[whereKey] {
			whereKey = whereField
		}
		fmt.Fprintf(g.buf, "%s struct {\n", whereField)
		whereUsed := map[string]int{}
		for _, pk := range e.target.PrimaryKeysSlice() {
			typ, err := g.goFieldType(e.target, pk)
			if err != nil {
				return err
			}
			if strings.Contains(typ, "time.Time") {
				g.needsTime = true
			}
			fmt.Fprintf(g.buf, "%s %s %s\n", goField(pk.Name(), whereUsed, g.initialisms), typ, jsonTag(pk.Name(), false))
		}
		fmt.Fprintf(g.buf, "} %s\n}\n\n", jsonTag(whereKey, false))
	}
	return nil
}

// emitSerializedModel embeds every source in the closure plus the schema's
// structural hash: an unexported package-level map keyed by
// MODULE-ROOT-RELATIVE source path (via sourceKey against the schema's recorded
// ModuleRoot), and the SerializedSources/SerializedEntry pair a caller re-loads
// through. Relative keys (never absolute SourceID strings) keep the .go output
// reproducible across checkouts/CI.
//
// One shape whatever the source count. The store is unexported because it is
// the pair's backing rather than a surface of its own, so the identifiers a
// consumer sees do not vary with how many files a schema happens to span.
//
// This MUST stay paired with verifyRoundTrip: the keys and the entry are
// derived there the same way (every key routes through sourceKey with the same
// root and g.schema.SourceID()), so a change to the shape here needs the mirror
// change there.
func (g *generator) emitSerializedModel() error {
	srcs := g.schema.Sources()
	ids := srcs.SourceIDs()
	entry := g.schema.SourceID()
	root := g.schema.ModuleRoot()
	if len(ids) == 0 {
		return errors.New("gogen: schema is not source-backed")
	}

	g.buf.WriteString("// serializedSources holds every source in the import closure, keyed by\n")
	g.buf.WriteString("// module-root-relative path, as verbatim .yammm text. Read it through\n")
	g.buf.WriteString("// SerializedSources below.\n")
	g.buf.WriteString("var serializedSources = map[string]string{\n")
	for _, id := range ids { // SourceIDs() is sorted/deterministic
		content, ok := srcs.ContentBySource(id)
		if !ok {
			return fmt.Errorf("gogen: source %s content unavailable", id)
		}
		fmt.Fprintf(g.buf, "%s: %s,\n", strconv.Quote(sourceKey(root, entry, id)), strconv.Quote(string(content)))
	}
	g.buf.WriteString("}\n\n")

	g.emitUniformSources(sourceKey(root, entry, entry))

	fmt.Fprintf(g.buf, "const SchemaHash = %q\n", schema.StructuralHash(g.schema))
	return nil
}

// emitUniformSources writes the SerializedSources/SerializedEntry pair, the one
// re-load surface a generated package exposes. entryKey is the sourceKey of the
// entry schema.
//
// The returned map is built fresh per call rather than handing back the
// package-level store, which a caller could otherwise mutate; no generated
// package has a hot path. Only builtin types appear, because generated output
// must stay stdlib-free (typeCheck resolves nothing but "time").
func (g *generator) emitUniformSources(entryKey string) {
	g.buf.WriteString("// SerializedEntry is the entry-point key into SerializedSources.\n")
	fmt.Fprintf(g.buf, "const SerializedEntry = %s\n\n", strconv.Quote(entryKey))

	g.buf.WriteString("// SerializedSources returns every source in the import closure, keyed by\n")
	g.buf.WriteString("// module-root-relative path. Re-load with:\n")
	g.buf.WriteString("//\n")
	g.buf.WriteString("//\tschema.LoadSourcesWithEntry(ctx, SerializedSources(), SerializedEntry, \"\",\n")
	g.buf.WriteString("//\t\tschema.WithSourcesOnly(), schema.WithSyntheticRoot(\"embedded://your-app\"))\n")
	g.buf.WriteString("//\n")
	g.buf.WriteString("// The synthetic root is what keeps the loaded type identities stable. Passing\n")
	g.buf.WriteString("// module root \".\" instead also re-loads, but \".\" canonicalizes against the\n")
	g.buf.WriteString("// process working directory, which then lands inside every TypeID.\n")
	g.buf.WriteString("func SerializedSources() map[string][]byte {\n")
	g.buf.WriteString("m := make(map[string][]byte, len(serializedSources))\n")
	g.buf.WriteString("for k, v := range serializedSources {\n")
	g.buf.WriteString("m[k] = []byte(v)\n")
	g.buf.WriteString("}\n")
	g.buf.WriteString("return m\n")
	g.buf.WriteString("}\n\n")
}

// sourceKey returns id's path relative to moduleRoot (the schema's recorded
// ModuleRoot — the root the load actually resolved module-style imports
// against), forward-slashed — so the embedded source keys are
// machine-independent AND match the import statements inside the sources:
// a module-style `import "a/b/x"` re-resolves to the key "a/b/x.yammm" on
// re-load, hitting the pre-registered source with no filesystem fallback.
// An absolute generation-machine path baked into the output would make the
// .go.golden files non-reproducible across checkouts/CI. When moduleRoot is
// empty (a LoadString-backed schema; imports are impossible there), keys
// fall back to entry-directory-relative — for the single source that exists
// in that case, its own base name. A source outside the module root (a legal
// entry layout; imports are sandboxed but the entry is not) yields a
// "../"-prefixed key, which still registers and re-loads consistently —
// merely an unusual-looking key. Falls back to the absolute id only when
// relativization is impossible (e.g. a different Windows volume) — still
// re-loadable on the generation machine, merely non-portable in that rare
// case, which verifyRoundTrip would surface.
func sourceKey(moduleRoot string, entry, id location.SourceID) string {
	root := moduleRoot
	if root == "" {
		root = filepath.Dir(entry.String())
	}
	if rel, err := filepath.Rel(root, id.String()); err == nil {
		return filepath.ToSlash(rel)
	}
	return id.String()
}

func (g *generator) emitType(t *schema.Type) error {
	goName, ok := g.names.goType(t.ID())
	if !ok {
		return fmt.Errorf("gogen: no Go name for type %q", t.Name())
	}
	g.emitDoc(t.Documentation()) // schema doc-comment -> Go doc-comment, where present
	fmt.Fprintf(g.buf, "type %s struct {\n", goName)
	used := map[string]int{} // one field namespace per struct; props + relations share it
	for _, p := range t.AllPropertiesSlice() {
		if err := g.emitField(t, p, used); err != nil {
			return err
		}
	}
	// Relation fields (compositions then associations) share the same used map, so a
	// relation field cannot collide with a property field.
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

// emitField writes one struct field: <GoName> <GoType> `json:"<wire name>[,omitempty]"`.
// used is the owning struct's field namespace, so goField can disambiguate the
// separator/digit-boundary merge residuals the loader does not catch.
func (g *generator) emitField(owner *schema.Type, p *schema.Property, used map[string]int) error {
	typ, err := g.goFieldType(owner, p)
	if err != nil {
		return fmt.Errorf("type %q property %q: %w", ownerName(owner), p.Name(), err)
	}
	if strings.Contains(typ, "time.Time") {
		g.needsTime = true
	}
	g.emitDoc(p.Documentation()) // property / edge-property doc-comment -> Go field doc-comment, where present
	fmt.Fprintf(g.buf, "%s %s %s\n", goField(p.Name(), used, g.initialisms), typ, jsonTag(p.Name(), p.IsOptional()))
	return nil
}

// goFieldType returns the Go type expression for a property's field, applying the
// optional-pointer rule (optional non-slice scalars become pointers; slices stay
// nilable) and rendering named Enum/DataType types faithfully. A named DataType
// field maps to its Go name (resolved in the property's declaring schema by
// registerDataTypeFields, keyed by the property pointer); an inline enum maps to
// its synthesized per-owner type; a List of a named DataType keeps the named
// element ([]FipsCode, not []string).
func (g *generator) goFieldType(owner *schema.Type, p *schema.Property) (string, error) {
	c := p.Constraint()
	resolved := schema.ResolveAlias(c)

	var typ string
	switch {
	case isAlias(c):
		// The DataType's Go name was resolved in the property's DECLARING schema by
		// registerDataTypeFields (keyed by the declared property pointer), so a
		// property inherited from a cross-schema parent still maps to the right named
		// type. Origin() bridges the two views: a property whose annotations were
		// merged across ancestors reaches emission as a synthesized copy that is not
		// in any type's own slice, so keying by the raw pointer would miss it.
		name, ok := g.dtFieldNames[p.Origin()]
		if !ok {
			dtName := "?"
			if ac, isA := c.(schema.AliasConstraint); isA {
				dtName = ac.DataTypeName()
			}
			return "", fmt.Errorf("gogen: no registered Go name for datatype property %q (%s)", p.Name(), dtName)
		}
		typ = name
	case owner != nil && resolved.Kind() == schema.KindEnum:
		typ = g.names.goInlineEnum(owner, p, g.initialisms) // <OwnerType><Field>, memoized + reserved
	case resolved.Kind() == schema.KindList:
		// Faithful list: List<DataType> renders []<Name>, not []<primitive>.
		lc, ok := resolved.(schema.ListConstraint)
		if !ok {
			return "", errors.New("gogen: List kind without ListConstraint")
		}
		t, err := g.goListType(p, lc)
		if err != nil {
			return "", err
		}
		typ = t
	default:
		base, err := goBaseType(c)
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

// goListType renders a List property's Go type, keeping a named DataType element
// (List<FipsCode> -> []FipsCode) instead of degrading to []string. For a
// list-of-datatype property the parser records the *element's* DataTypeRef as the
// property's DataTypeRef, so registerDataTypeFields resolved and recorded the
// element's DataType Go name (in the property's declaring schema, keyed by the
// property pointer). Primitive elements (List<String>) and inline-(anonymous-)enum
// elements carry no such entry and fall back to goBaseType — named element enums are
// deferred. A property whose whole type is a named List DataType (type Codes =
// List<String>) is handled by the isAlias branch in goFieldType, not here.
//
// The lookup keys by Origin() for the same reason goFieldType's isAlias branch
// does: a property whose annotations were merged across ancestors reaches
// emission as a synthesized copy that is in no type's own slice, so the raw
// pointer misses the table and the field degrades to []string while the same
// property on its ancestors emits []Name.
func (g *generator) goListType(p *schema.Property, lc schema.ListConstraint) (string, error) {
	if isAlias(lc.Element()) {
		if name, ok := g.dtFieldNames[p.Origin()]; ok {
			return "[]" + name, nil
		}
	}
	return goBaseType(lc)
}

// isAlias reports whether a constraint is a DataType reference (AliasConstraint).
func isAlias(c schema.Constraint) bool {
	_, ok := c.(schema.AliasConstraint)
	return ok
}

// jsonTag builds a json struct tag, preserving the wire name and adding
// ,omitempty for optional fields.
func jsonTag(name string, optional bool) string {
	if optional {
		return fmt.Sprintf("`json:%q`", name+",omitempty")
	}
	return fmt.Sprintf("`json:%q`", name)
}

// ownerName returns a type's name, or "<edge>" for the nil owner used when
// emitting EDGE_ property fields.
func ownerName(t *schema.Type) string {
	if t == nil {
		return "<edge>"
	}
	return t.Name()
}

// emitDoc writes a yammm doc-comment as Go line-comments directly above a
// declaration. An empty doc emits nothing; a multi-line doc becomes one "// "
// line per source line. Generated files are lint-excluded, so the golint "comment
// must start with the name" rule does not apply — the schema's wording is carried
// through verbatim.
func (g *generator) emitDoc(doc string) {
	if doc == "" {
		return
	}
	for line := range strings.SplitSeq(doc, "\n") {
		fmt.Fprintf(g.buf, "// %s\n", strings.TrimRight(line, " \t"))
	}
}

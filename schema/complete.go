package schema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/ident"
	"github.com/simon-lentz/yammm/location"
)

// completionRegistry provides lookup for cross-schema type resolution.
// This interface is implemented by schema.Registry.
type completionRegistry interface {
	// LookupBySourceID returns the schema at the given source ID, if loaded.
	LookupBySourceID(id location.SourceID) (*Schema, bool)
}

// resolvedImportMap maps import aliases to their resolved SourceIDs.
// Passed from the loader to enable cross-schema type resolution during completion.
type resolvedImportMap map[string]location.SourceID

// completeModel transforms a parsed AST model into a completed Schema.
//
// The completion process:
//  1. Creates the Schema and indexes types/datatypes
//  2. Validates imports against alias rules
//  3. Detects inheritance cycles
//  4. Linearizes inheritance (DFS, keep-first)
//  5. Merges inherited properties and relations
//  6. Detects collisions (case-insensitive, normalized names)
//  7. Validates relation targets
//
// Errors are collected in the provided collector. Returns nil if completion
// fails with fatal errors. The registry is optional; when nil, cross-schema
// references are deferred. The resolvedImports map provides pre-resolved
// import alias to SourceID mappings from the loader.
func completeModel(
	m *model,
	sourceID location.SourceID,
	collector *diag.Collector,
	registry completionRegistry,
	resolvedImports resolvedImportMap,
) *Schema {
	if m == nil {
		collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "no model to complete").Build())
		return nil
	}

	c := &completer{
		model:           m,
		sourceID:        sourceID,
		collector:       collector,
		registry:        registry,
		resolvedImports: resolvedImports,
		typeIndex:       make(map[string]*Type),
		dataIndex:       make(map[string]*DataType),
	}

	return c.complete()
}

// completer holds state during schema completion.
type completer struct {
	model           *model
	sourceID        location.SourceID
	collector       *diag.Collector
	registry        completionRegistry
	resolvedImports resolvedImportMap
	schema          *Schema
	typeIndex       map[string]*Type
	dataIndex       map[string]*DataType
}

func (c *completer) complete() *Schema {
	// Create the schema shell
	c.schema = newSchema(
		c.model.Name,
		c.sourceID,
		c.model.Span,
		c.model.Documentation,
	)

	// Phase 1: Index types and datatypes
	if !c.indexTypes() {
		return nil
	}
	if !c.indexDataTypes() {
		return nil
	}

	// Phase 2: Validate and index imports
	if !c.indexImports() {
		return nil
	}

	// Phase 3: Resolve alias constraints
	if !c.resolveAliasConstraints() {
		return nil
	}

	// Phase 3b: Validate relation edge properties (must be after alias resolution)
	if !c.validateRelationProperties() {
		return nil
	}

	// Phase 4: Detect inheritance cycles
	if !c.detectCycles() {
		return nil
	}

	// Phase 5: Complete each type (linearize, merge, validate)
	if !c.completeTypes() {
		return nil
	}

	// Phase 5b: Concrete types must declare or inherit a primary key. Collected
	// (not aborted here) so collision/relation/invariant diagnostics still surface
	// in the same pass; the final HasErrors gate below ends completion.
	c.validatePrimaryKeys()

	// Phase 6: Detect collisions
	if !c.detectCollisions() {
		return nil
	}

	// Phase 7: Validate relation targets
	if !c.validateRelationTargets() {
		return nil
	}

	// Phase 7b: Validate invariant expressions (static property checking)
	if !c.validateInvariantExpressions() {
		return nil
	}

	// Final check for any errors collected during completion phases
	// (e.g., from merge operations that don't abort but collect diagnostics)
	if c.collector.HasErrors() {
		return nil
	}

	// Phase 8: Seal all types and relations to prevent post-completion mutation
	for _, t := range c.schema.TypesSlice() {
		t.seal()
		// Seal all relations on this type
		for rel := range t.AllAssociations() {
			rel.seal()
		}
		for rel := range t.AllCompositions() {
			rel.seal()
		}
	}

	// Seal all data types for consistency with type/relation sealing
	for _, dt := range c.schema.DataTypesSlice() {
		dt.seal()
	}

	return c.schema
}

// indexTypes creates Type objects and indexes them by name.
func (c *completer) indexTypes() bool {
	types := make([]*Type, 0, len(c.model.Types))

	for _, td := range c.model.Types {
		if td == nil {
			continue
		}

		if existing, ok := c.typeIndex[td.Name]; ok {
			c.errorf(td.Span, diag.E_DUPLICATE_TYPE,
				"type %q is defined multiple times; first defined at %s",
				td.Name, existing.Span().Start)
			return false
		}

		t := newType(
			td.Name,
			c.sourceID,
			td.Span,
			td.Documentation,
			td.IsAbstract,
			td.IsPart,
		)

		// Set precise name span for go-to-definition accuracy
		if !td.NameSpan.IsZero() {
			t.setNameSpan(td.NameSpan)
		}

		// Convert declared inherits
		inherits := make([]TypeRef, 0, len(td.Inherits))
		for _, ref := range td.Inherits {
			if ref != nil {
				inherits = append(inherits, NewTypeRef(ref.Qualifier, ref.Name, ref.Span))
			}
		}
		t.setInherits(inherits)

		// Convert and set declared properties
		props := c.convertProperties(td.Properties, td.Name)
		t.setProperties(props)

		// Convert and set declared relations (split into associations/compositions)
		assocs, comps := c.convertRelations(td.Relations, td.Name)
		t.setAssociations(assocs)
		t.setCompositions(comps)

		// Convert and set invariants
		invariants := c.convertInvariants(td.Invariants)
		t.setInvariants(invariants)

		c.typeIndex[td.Name] = t
		types = append(types, t)
	}

	c.schema.setTypes(types)
	return true
}

// indexDataTypes creates DataType objects and indexes them by name.
func (c *completer) indexDataTypes() bool {
	dataTypes := make([]*DataType, 0, len(c.model.DataTypes))

	for _, dd := range c.model.DataTypes {
		if dd == nil {
			continue
		}

		if existing, ok := c.dataIndex[dd.Name]; ok {
			c.errorf(dd.Span, diag.E_DUPLICATE_TYPE,
				"datatype %q is defined multiple times; first defined at %s",
				dd.Name, existing.Span().Start)
			return false
		}

		dt := newDataType(
			dd.Name,
			dd.Constraint,
			dd.Span,
			dd.Documentation,
		)

		c.dataIndex[dd.Name] = dt
		dataTypes = append(dataTypes, dt)
	}

	c.schema.setDataTypes(dataTypes)
	return true
}

// indexImports validates and indexes import declarations.
func (c *completer) indexImports() bool {
	imports := make([]*Import, 0, len(c.model.Imports))
	aliasIndex := make(map[string]*importDecl)
	seenSourceIDs := make(map[location.SourceID]*importDecl)

	for _, id := range c.model.Imports {
		if id == nil {
			continue
		}

		// Validate alias is a valid identifier
		if !isValidAlias(id.Alias) {
			c.errorf(id.Span, diag.E_INVALID_ALIAS,
				"derived alias %q is not a valid identifier (aliases must start with a letter); use 'as <alias>' to provide a valid alias",
				id.Alias)
			return false
		}

		// Validate alias is not a reserved keyword
		if isReservedKeyword(id.Alias) {
			c.errorf(id.Span, diag.E_INVALID_ALIAS,
				"import alias %q is a reserved keyword; use 'as <alias>' to provide a different alias",
				id.Alias)
			return false
		}

		// Check for duplicate alias
		if existing, ok := aliasIndex[id.Alias]; ok {
			c.errorf(id.Span, diag.E_DUPLICATE_IMPORT,
				"alias %q already used for import %q; use explicit 'as' to disambiguate\n    existing: import %q\n    new:      import %q",
				id.Alias, existing.Path, existing.Path, id.Path)
			return false
		}

		// Check for alias collision with local type
		if _, ok := c.typeIndex[id.Alias]; ok {
			c.errorf(id.Span, diag.E_IMPORT_ALIAS_COLLISION,
				"import alias %q collides with local type name", id.Alias)
			return false
		}

		aliasIndex[id.Alias] = id

		// Resolve SourceID from pre-resolved imports or defer resolution
		var resolvedSourceID location.SourceID
		if c.resolvedImports != nil {
			// ResolvedImports was provided - verify alias has a resolution
			var ok bool
			resolvedSourceID, ok = c.resolvedImports[id.Alias]
			if !ok || resolvedSourceID.IsZero() {
				c.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
					fmt.Sprintf("import alias %q has no resolved SourceID; ensure loader provides all import resolutions", id.Alias)).
					WithSpan(id.Span).
					WithDetail(diag.DetailKeyAlias, id.Alias).
					WithDetail(diag.DetailKeyImportPath, id.Path).Build())
				return false
			}
			// Check for duplicate SourceID
			if existing, ok := seenSourceIDs[resolvedSourceID]; ok {
				c.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
					fmt.Sprintf("schema %q imported multiple times", resolvedSourceID.String())).
					WithSpan(id.Span).
					WithDetail(diag.DetailKeyImportPath, resolvedSourceID.String()).
					WithDetail(diag.DetailKeyFirstAlias, existing.Alias).
					WithDetail(diag.DetailKeyFirstLine, strconv.Itoa(existing.Span.Start.Line)).
					WithDetail(diag.DetailKeyDuplicateAlias, id.Alias).
					WithDetail(diag.DetailKeyDuplicateLine, strconv.Itoa(id.Span.Start.Line)).
					WithRelated(location.RelatedInfo{
						Span:    existing.Span,
						Message: fmt.Sprintf("first imported here as %q", existing.Alias),
					}).Build())
				return false
			}
			seenSourceIDs[resolvedSourceID] = id
		}
		// If resolvedImports is nil, leave resolvedSourceID as zero for deferred resolution
		imp := newImport(id.Path, id.Alias, resolvedSourceID, id.Span)
		imports = append(imports, imp)
	}

	c.schema.setImports(imports)
	return true
}

// convertProperties converts propertyDecl to Property.
// Detects duplicate property declarations within the same type.
func (c *completer) convertProperties(decls []*propertyDecl, ownerType string) []*Property {
	props := make([]*Property, 0, len(decls))
	seen := make(map[string]*propertyDecl) // Track first occurrence for related info

	for _, pd := range decls {
		if pd == nil {
			continue
		}

		// Check for duplicate property within same type
		if existing, ok := seen[pd.Name]; ok {
			c.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_PROPERTY,
				fmt.Sprintf("property %q is defined multiple times in type %q", pd.Name, ownerType)).
				WithSpan(pd.Span).
				WithRelated(location.RelatedInfo{
					Span:    existing.Span,
					Message: "first defined here",
				}).Build())
			continue // Skip duplicate
		}
		seen[pd.Name] = pd

		scope := TypeScope(NewTypeRef("", ownerType, pd.Span))
		p := newProperty(
			pd.Name,
			pd.Span,
			pd.Documentation,
			pd.Constraint,
			pd.DataTypeRef,
			pd.Optional,
			pd.IsPrimaryKey,
			scope,
		)
		props = append(props, p)
	}

	return props
}

// convertRelations converts relationDecl to Relation, splitting by kind.
// Detects duplicate relation declarations within the same type.
func (c *completer) convertRelations(decls []*relationDecl, ownerType string) (assocs, comps []*Relation) {
	seen := make(map[string]*relationDecl) // Track first occurrence by raw name

	for _, rd := range decls {
		if rd == nil {
			continue
		}

		// Check for duplicate relation within same type (by raw name)
		if existing, ok := seen[rd.Name]; ok {
			c.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_RELATION,
				fmt.Sprintf("relation %q is defined multiple times in type %q", rd.Name, ownerType)).
				WithSpan(rd.Span).
				WithRelated(location.RelatedInfo{
					Span:    existing.Span,
					Message: "first defined here",
				}).Build())
			continue // Skip duplicate
		}
		seen[rd.Name] = rd

		var target TypeRef
		if rd.Target != nil {
			target = NewTypeRef(rd.Target.Qualifier, rd.Target.Name, rd.Target.Span)
		}

		// Compute field name using lower_snake normalization
		fieldName := ident.ToLowerSnake(rd.Name)

		// Convert edge properties (associations only)
		var props []*Property
		if rd.Kind == RelationAssociation && len(rd.Properties) > 0 {
			props = make([]*Property, 0, len(rd.Properties))
			for _, pd := range rd.Properties {
				if pd == nil {
					continue
				}
				scope := RelationScope(rd.Name)
				p := newProperty(
					pd.Name,
					pd.Span,
					pd.Documentation,
					pd.Constraint,
					pd.DataTypeRef,
					pd.Optional,
					pd.IsPrimaryKey,
					scope,
				)
				props = append(props, p)
			}
		}

		// Map relationDecl kind to RelationKind
		var kind RelationKind
		switch rd.Kind {
		case RelationAssociation:
			kind = RelationAssociation
		case RelationComposition:
			kind = RelationComposition
		}

		r := newRelation(
			kind,
			rd.Name,
			fieldName,
			target,
			TypeID{}, // Resolved during completion
			rd.Span,
			rd.Documentation,
			rd.Optional,
			rd.Many,
			rd.Backref,
			rd.ReverseOptional,
			rd.ReverseMany,
			ownerType,
			props,
		)

		if kind == RelationAssociation {
			assocs = append(assocs, r)
		} else {
			comps = append(comps, r)
		}
	}

	return assocs, comps
}

// convertInvariants converts invariantDecl to Invariant.
func (c *completer) convertInvariants(decls []*invariantDecl) []*Invariant {
	invs := make([]*Invariant, 0, len(decls))

	for _, id := range decls {
		if id == nil {
			continue
		}

		inv := newInvariant(id.Name, id.Expr, id.Span, id.Documentation)
		invs = append(invs, inv)
	}

	return invs
}

// resolveAliasConstraints resolves all AliasConstraint references in properties and datatypes.
// Returns false if any unresolvable aliases are found.
func (c *completer) resolveAliasConstraints() bool {
	ok := true

	// First, resolve DataType constraints (they may reference each other)
	for _, dt := range c.schema.DataTypesSlice() {
		if alias, isAlias := dt.Constraint().(AliasConstraint); isAlias && !alias.IsResolved() {
			resolved, success := c.resolveAliasChain(alias.DataTypeName(), dt.Span(), make(map[string]bool))
			if !success {
				ok = false
				continue
			}
			dt.setConstraint(resolved)
		}
	}

	// Resolve List element aliases in DataTypes
	for _, dt := range c.schema.DataTypesSlice() {
		if _, isList := dt.Constraint().(ListConstraint); isList {
			resolved, success := c.resolveListElementAliases(dt.Constraint(), dt.Span())
			if !success {
				ok = false
				continue
			}
			dt.setConstraint(resolved)
		}
	}

	// Then, resolve property constraints on all types
	for _, t := range c.schema.TypesSlice() {
		for _, p := range t.PropertiesSlice() {
			if alias, isAlias := p.Constraint().(AliasConstraint); isAlias && !alias.IsResolved() {
				resolved, success := c.resolveAliasChain(alias.DataTypeName(), p.Span(), make(map[string]bool))
				if !success {
					ok = false
					continue
				}
				p.setConstraint(resolved)
			}
		}
	}

	// Resolve List element aliases in Properties
	for _, t := range c.schema.TypesSlice() {
		for _, p := range t.PropertiesSlice() {
			if _, isList := p.Constraint().(ListConstraint); isList {
				resolved, success := c.resolveListElementAliases(p.Constraint(), p.Span())
				if !success {
					ok = false
					continue
				}
				p.setConstraint(resolved)
			}
		}
	}

	return ok
}

// resolveAliasChain resolves a datatype name to its underlying constraint.
// The visited map tracks seen names for cycle detection.
// Returns the resolved AliasConstraint and success status.
// If the datatype is not found, returns the original unresolved alias (not an error).
// This allows for forward references and type-as-property patterns.
func (c *completer) resolveAliasChain(dataTypeName string, span location.Span, visited map[string]bool) (Constraint, bool) {
	// Cycle detection
	if visited[dataTypeName] {
		c.errorf(span, diag.E_INVALID_CONSTRAINT,
			"alias constraint %q forms a cycle", dataTypeName)
		return nil, false
	}
	visited[dataTypeName] = true

	// Parse qualified name
	qualifier, name := parseQualifiedName(dataTypeName)

	// Lookup DataType
	var dt *DataType
	var found bool
	if qualifier == "" {
		// Local datatype
		dt, found = c.dataIndex[name]
	} else {
		// Cross-schema reference
		if c.registry == nil {
			// Without registry, we cannot resolve cross-schema refs
			// Return unresolved alias; will be validated at link time
			return NewAliasConstraint(dataTypeName, nil), true
		}
		sourceID, ok := c.resolvedImports[qualifier]
		if !ok {
			// Unknown import alias - leave unresolved
			// This might be a type reference pattern (e.g., b.Middle where Middle is a type)
			return NewAliasConstraint(dataTypeName, nil), true
		}
		importedSchema, ok := c.registry.LookupBySourceID(sourceID)
		if !ok {
			// Imported schema not found - leave unresolved
			return NewAliasConstraint(dataTypeName, nil), true
		}
		dt, found = importedSchema.DataType(name)
	}

	if !found {
		// Datatype not found - leave unresolved
		// This might be a type reference pattern or forward reference
		return NewAliasConstraint(dataTypeName, nil), true
	}

	underlying := dt.Constraint()

	// If underlying is an unresolved alias, resolve it first
	if alias, isAlias := underlying.(AliasConstraint); isAlias && !alias.IsResolved() {
		resolved, ok := c.resolveAliasChain(alias.DataTypeName(), dt.Span(), visited)
		if !ok {
			return nil, false
		}
		return NewAliasConstraint(dataTypeName, resolved), true
	}

	// Otherwise, return new alias with underlying as resolved
	return NewAliasConstraint(dataTypeName, underlying), true
}

// resolveListElementAliases recursively resolves alias constraints inside
// ListConstraint elements. Returns the constraint with aliases resolved,
// or the original if no resolution was needed.
func (c *completer) resolveListElementAliases(constraint Constraint, span location.Span) (Constraint, bool) {
	lc, ok := constraint.(ListConstraint)
	if !ok {
		return constraint, true
	}

	elem := lc.Element()

	// Recurse into nested lists first
	resolved, ok := c.resolveListElementAliases(elem, span)
	if !ok {
		return constraint, false
	}
	elem = resolved

	// Resolve alias if element is an alias
	if alias, isAlias := elem.(AliasConstraint); isAlias && !alias.IsResolved() {
		resolvedAlias, success := c.resolveAliasChain(alias.DataTypeName(), span, make(map[string]bool))
		if !success {
			return constraint, false
		}
		elem = resolvedAlias
	}

	// Rebuild with resolved element
	minLen, hasMin := lc.MinLen()
	maxLen, hasMax := lc.MaxLen()

	switch {
	case hasMin && hasMax:
		return ListLenBetween(elem, minLen, maxLen), true
	case hasMin:
		return ListMinLen(elem, minLen), true
	case hasMax:
		return ListMaxLen(elem, maxLen), true
	default:
		return NewListConstraint(elem), true
	}
}

// parseQualifiedName splits a qualified name into qualifier and local name.
// For "foo.Bar", returns ("foo", "Bar"). For "Bar", returns ("", "Bar").
func parseQualifiedName(name string) (qualifier, localName string) {
	before, after, ok := strings.Cut(name, ".")
	if !ok {
		return "", name
	}
	return before, after
}

// validateRelationProperties validates edge properties on all relations.
// Specifically checks that relation properties do not use Vector types (per spec).
// Returns false if any validation errors are found.
func (c *completer) validateRelationProperties() bool {
	ok := true

	for _, t := range c.schema.TypesSlice() {
		for rel := range t.Associations() {
			for _, p := range rel.PropertiesSlice() {
				if isVectorConstraint(p.Constraint()) {
					c.errorf(p.Span(), diag.E_INVALID_CONSTRAINT,
						"relationship property %q cannot use Vector type", p.Name())
					ok = false
				}
				if isListConstraint(p.Constraint()) {
					c.errorf(p.Span(), diag.E_LIST_ON_EDGE,
						"relationship property %q cannot use List type", p.Name())
					ok = false
				}
			}
		}
	}

	return ok
}

// isVectorConstraint checks if a constraint is or resolves to a Vector type.
// Unwraps alias constraints to check the underlying type.
func isVectorConstraint(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	for {
		if constraint.Kind() == KindVector {
			return true
		}
		alias, ok := constraint.(AliasConstraint)
		if !ok || alias.Resolved() == nil {
			return false
		}
		constraint = alias.Resolved()
	}
}

// isListConstraint checks if a constraint is or resolves to a List type.
// Unwraps alias constraints to check the underlying type.
func isListConstraint(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	for {
		if constraint.Kind() == KindList {
			return true
		}
		alias, ok := constraint.(AliasConstraint)
		if !ok || alias.Resolved() == nil {
			return false
		}
		constraint = alias.Resolved()
	}
}

// isPrimaryKeyAllowed returns true if the constraint's underlying type is
// permitted as a primary key. Only String, UUID, Date, and Timestamp are
// allowed. Aliases are unwrapped to check the resolved type.
func isPrimaryKeyAllowed(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	// Unwrap aliases to the resolved type.
	for {
		alias, ok := constraint.(AliasConstraint)
		if !ok || alias.Resolved() == nil {
			break
		}
		constraint = alias.Resolved()
	}
	//exhaustive:enforce
	switch constraint.Kind() {
	case KindString, KindUUID, KindDate, KindTimestamp:
		return true
	case KindInteger, KindFloat, KindBoolean, KindEnum,
		KindPattern, KindVector, KindList, KindAlias:
		return false
	default:
		return false
	}
}

// validatePrimaryKeys enforces that every concrete (non-abstract, non-part) type
// declares or inherits at least one primary key. A node needs identity to be added to
// a graph (see Graph.Add / E_GRAPH_MISSING_PK) or referenced by an association;
// enforcing it at load fails fast for every consumer (gogen, the neo4j adapter, the
// LSP), not only graph construction. Abstract types (not instantiable) and part types
// (embedded, no independent identity — PK-less parts are a supported composition
// feature) are exempt. Errors are collected; the caller's final error gate aborts.
func (c *completer) validatePrimaryKeys() {
	for _, t := range c.schema.TypesSlice() {
		if t.IsAbstract() || t.IsPart() || t.HasPrimaryKey() {
			continue
		}
		// Skip a type whose supertype chain has an unresolved link (a deferred
		// cross-schema reference when the registry lacks the import, or an unknown type
		// already reported elsewhere): its primary key may be inherited from an ancestor
		// not yet visible, and the unresolved reference already carries its own diagnostic.
		if c.hasUnresolvedSupertype(t) {
			continue
		}
		c.errorf(t.Span(), diag.E_NO_PRIMARY_KEY,
			"concrete type %q has no primary key; declare a 'primary' property or inherit one from a parent type",
			t.Name())
	}
}

// hasUnresolvedSupertype reports whether t, or any type in its declared-inheritance
// closure, names an `extends` supertype that does not resolve at this point in
// completion — a deferred cross-schema reference when the registry lacks the import, or
// an unknown type already reported elsewhere. The walk is transitive, not just over t's
// direct supertypes: a primary key may be inherited through a local parent that itself
// extends an unresolved root, so the presence check must skip the whole chain, not only
// types that declare the unresolved reference directly. Recursion follows local
// supertypes only — a resolved cross-schema supertype is already linearized and its
// members merged, so it is a leaf here. Inheritance cycles are rejected earlier
// (detectCycles), so visited bounds the walk rather than guarding correctness.
func (c *completer) hasUnresolvedSupertype(t *Type) bool {
	visited := map[string]bool{t.Name(): true}
	queue := []*Type{t}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for ref := range cur.Inherits() {
			super := c.resolveTypeRef(ref)
			if super == nil {
				return true
			}
			// Recurse only into local supertypes; a resolved cross-schema supertype is
			// a fully-merged leaf. A qualified ref that resolved cannot be local.
			if ref.Qualifier() == "" && !visited[ref.Name()] {
				visited[ref.Name()] = true
				queue = append(queue, super)
			}
		}
	}
	return false
}

// errorf reports an error at the given span.
func (c *completer) errorf(span location.Span, code diag.Code, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	issue := diag.NewIssue(diag.Error, code, msg)
	if !span.IsZero() {
		issue = issue.WithSpan(span)
	}
	c.collector.Collect(issue.Build())
}

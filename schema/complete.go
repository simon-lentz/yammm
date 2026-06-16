package schema

import (
	"fmt"
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

// importResolution is one import alias's resolution outcome, handed from the
// loader (or the Builder) to completion. A deferred entry — the import was
// declared but its load failed or was structurally rejected — carries no
// SourceID; references through the alias defer to the root-cause diagnostic. A
// resolved entry carries the imported schema's SourceID. The explicit deferred
// flag replaces an earlier in-band convention (a zero SourceID meaning
// "deferred"), so the state is read, not inferred.
type importResolution struct {
	deferred bool
	sourceID location.SourceID
}

// resolvedImportMap maps an import alias to its resolution. An alias is present
// iff its declaration was seen; an absent alias was never resolved (a loader
// bug on the Load path, or a no-imports / no-registry context elsewhere). See
// [completer.classifyQualifier], the single interpreter of this map.
type resolvedImportMap map[string]importResolution

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
		// The collector may be shared across a whole multi-schema load, so
		// this completion is judged on the errors IT contributes, not on
		// errors collected before it began (a sibling import's failure must
		// not nil an unrelated clean schema).
		gate: newErrorGate(collector),
	}

	return c.complete()
}

// errorGate snapshots a collector's Fatal+Error count so a later check reports
// whether the guarded operation contributed any error. When a collector is
// shared across nested schema loads, "any error" ([diag.Collector.HasErrors])
// conflates a schema's own failures with a sibling's; the gate judges each
// schema by the errors IT contributed. tripped is the single load-bearing
// comparison, so a nested caller cannot reintroduce the conflation by forgetting
// to snapshot.
type errorGate struct {
	collector *diag.Collector
	entry     int
}

func newErrorGate(c *diag.Collector) errorGate {
	return errorGate{collector: c, entry: c.ErrorCount()}
}

// tripped reports whether any Fatal+Error issue was collected since the gate was
// taken.
func (g errorGate) tripped() bool {
	return g.collector.ErrorCount() > g.entry
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
	gate            errorGate // judges this completion by the errors it contributes
}

func (c *completer) complete() *Schema {
	// Create the schema shell
	c.schema = newSchema(
		c.model.Name,
		c.sourceID,
		c.model.Span,
		c.model.Documentation,
	)

	// Phases run in dependency order. Each phase collects every independent
	// finding it can and skips the entities it has already reported
	// (keep-first indexing, deferred references, poisoned-chain skips), so
	// one phase's failures do not hide another phase's findings. The final
	// error gate — not per-phase aborts — decides whether a schema is
	// produced. The one exception is detectCycles, whose gate guards a
	// genuine data dependency.

	// Phase 1: Index types and datatypes
	c.indexTypes()
	c.indexDataTypes()

	// Phase 2: Validate and index imports
	c.indexImports()

	// Phase 3: Resolve alias constraints
	c.resolveAliasConstraints()

	// Phase 3b: Validate relation edge properties (must be after alias resolution)
	c.validateRelationProperties()

	// Phase 4: Detect inheritance cycles. This gate stays: linearization
	// requires cycle-free input. completeTypes' DFS would terminate on a
	// cycle (early-mark plus seen-dedup), but the merged member sets it
	// produced would be garbage, and every downstream phase that reads
	// merged members would generate noise from them.
	if !c.detectCycles() {
		return nil
	}

	// Phase 5: Complete each type (linearize, merge, validate)
	c.completeTypes()

	// Phase 5b: Concrete types must declare or inherit a primary key. Collected
	// (not aborted here) so collision/relation/invariant diagnostics still surface
	// in the same pass; the final error gate below ends completion.
	c.validatePrimaryKeys()

	// Phase 6: Detect collisions
	c.detectCollisions()

	// Phase 7: Validate relation targets
	c.validateRelationTargets()

	// Phase 7b: Validate invariant expressions (static property checking)
	c.validateInvariantExpressions()

	// Final check for any errors THIS completion collected — its own error
	// delta, not the shared collector's total: errors collected before this
	// completion began belong to other schemas in the same load.
	if c.gate.tripped() {
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

// indexTypes creates Type objects and indexes them by name. A duplicate
// declaration is reported and skipped — the first declaration is the one
// kept and indexed — so every duplicate is diagnosed in one pass and
// downstream phases see one coherent Type per name.
func (c *completer) indexTypes() {
	types := make([]*Type, 0, len(c.model.Types))

	for _, td := range c.model.Types {
		if td == nil {
			continue
		}

		if existing, dup := c.typeIndex[td.Name]; dup {
			c.errorf(td.Span, diag.E_DUPLICATE_TYPE,
				"type %q is defined multiple times; first defined at %s",
				td.Name, existing.Span().Start)
			continue
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
}

// indexDataTypes creates DataType objects and indexes them by name. A
// duplicate declaration is reported and skipped, keep-first, like
// indexTypes.
func (c *completer) indexDataTypes() {
	dataTypes := make([]*DataType, 0, len(c.model.DataTypes))

	for _, dd := range c.model.DataTypes {
		if dd == nil {
			continue
		}

		if existing, dup := c.dataIndex[dd.Name]; dup {
			c.errorf(dd.Span, diag.E_DUPLICATE_TYPE,
				"datatype %q is defined multiple times; first defined at %s",
				dd.Name, existing.Span().Start)
			continue
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
}

// indexImports validates and indexes import declarations. Declaration-level
// import validation is owned here: this phase is shared by both front doors
// (parsed sources and the Builder), so checks live in one place. Every
// declaration is validated and one bad declaration does not hide another. A
// later declaration of a duplicated alias is dropped keep-first (no Import),
// so references resolve against the kept first binding. A declaration rejected
// for an invalid or reserved alias, or for colliding with a local
// type/datatype name, is marked deferred instead (see deferRejectedAlias), so
// references through its alias defer to the rejection's single root-cause
// diagnostic rather than silently resolving against the import the loader did
// resolve, or re-blaming E_UNKNOWN_TYPE at each reference site.
func (c *completer) indexImports() {
	imports := make([]*Import, 0, len(c.model.Imports))
	aliasIndex := make(map[string]*importDecl)

	// deferRejectedAlias marks a rejected declaration's alias as deferred,
	// matching how a failed import is represented. The inert zero-SourceID
	// Import makes ImportByAlias-based resolvers (extends clauses, relation
	// targets via referenceDeferred) recognise the deferral, and zeroing the
	// resolution-map entry makes datatype-reference resolution (resolveAliasChain
	// reads that map directly) defer too — otherwise the two reference kinds
	// diverge: datatype refs silently resolve against the import the loader
	// resolved before this phase rejected the declaration, while type refs
	// re-blame E_UNKNOWN_TYPE on top of the already-reported rejection.
	deferRejectedAlias := func(id *importDecl) {
		imports = append(imports, newImport(id.Path, id.Alias, location.SourceID{}, id.Span))
		if c.resolvedImports != nil {
			c.resolvedImports[id.Alias] = importResolution{deferred: true}
		}
	}

	for _, id := range c.model.Imports {
		if id == nil {
			continue
		}

		// Duplicate alias (keep-first): the first declaration of an alias owns
		// the slot — even one later rejected as invalid, reserved, or colliding
		// — so a repeat is reported once here as E_DUPLICATE_IMPORT and stays
		// inert. The slot is claimed before the per-declaration validation
		// below; otherwise a repeated *rejected* alias would re-fire its own
		// rejection at every occurrence instead (two imports whose alias
		// collides with a local name would each draw E_IMPORT_ALIAS_COLLISION
		// and never the duplicate report).
		if existing, dup := aliasIndex[id.Alias]; dup {
			c.errorf(id.Span, diag.E_DUPLICATE_IMPORT,
				"alias %q already used for import %q; use explicit 'as' to disambiguate\n    existing: import %q\n    new:      import %q",
				id.Alias, existing.Path, existing.Path, id.Path)
			continue
		}
		aliasIndex[id.Alias] = id

		// Validate alias is a valid identifier. Parse-path aliases always
		// pass — derived aliases are normalized by deriveAliasFromPath and
		// explicit aliases are letter-start by grammar — so this rejects
		// Builder-supplied strings.
		if !isValidAlias(id.Alias) {
			c.errorf(id.Span, diag.E_INVALID_ALIAS,
				"derived alias %q is not a valid identifier (aliases must start with a letter); use 'as <alias>' to provide a valid alias",
				id.Alias)
			deferRejectedAlias(id)
			continue
		}

		// Validate alias is not a reserved keyword. The parser drops such
		// declarations before they reach a model, so this too is live for
		// the Builder.
		if isReservedKeyword(id.Alias) {
			c.errorf(id.Span, diag.E_INVALID_ALIAS,
				"import alias %q is a reserved keyword; use 'as <alias>' to provide a different alias",
				id.Alias)
			deferRejectedAlias(id)
			continue
		}

		// Check for alias collision with local type or datatype names.
		// Both indexes are consulted: a qualified reference's qualifier is
		// ambiguous against either kind of local declaration.
		if _, collides := c.typeIndex[id.Alias]; collides {
			c.errorf(id.Span, diag.E_IMPORT_ALIAS_COLLISION,
				"import alias %q collides with local type name", id.Alias)
			deferRejectedAlias(id)
			continue
		}
		if _, collides := c.dataIndex[id.Alias]; collides {
			c.errorf(id.Span, diag.E_IMPORT_ALIAS_COLLISION,
				"import alias %q collides with local datatype name", id.Alias)
			deferRejectedAlias(id)
			continue
		}

		// Resolve SourceID from pre-resolved imports or defer resolution.
		// Duplicate resolved SourceIDs are the resolver's concern (the
		// loader's validateResolvedImports, the Builder's resolveImports
		// dedup) — by the time a resolution map reaches completion it is
		// already deduplicated.
		var resolvedSourceID location.SourceID
		if c.resolvedImports != nil {
			resolved, present := c.resolvedImports[id.Alias]
			switch {
			case !present:
				// Key-absent: the loader never resolved this alias at all —
				// a loader bug, not a user error. Reported and skipped.
				c.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
					fmt.Sprintf("import alias %q has no resolved SourceID; ensure loader provides all import resolutions", id.Alias)).
					WithSpan(id.Span).
					WithDetail(diag.DetailKeyAlias, id.Alias).
					WithDetail(diag.DetailKeyImportPath, id.Path).Build())
				continue
			case resolved.deferred:
				// The loader saw this alias and already reported why it has no
				// resolution (failed load or structural rejection). Construct
				// the Import deferred — zero SourceID, no additional diagnostic
				// — so references through the alias are recognizably deferred
				// downstream.
			default:
				resolvedSourceID = resolved.sourceID
			}
		}
		// If resolvedImports is nil, leave resolvedSourceID as zero for deferred resolution
		imp := newImport(id.Path, id.Alias, resolvedSourceID, id.Span)
		imports = append(imports, imp)
	}

	c.schema.setImports(imports)
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

// resolveAliasConstraints resolves all AliasConstraint references in
// properties and datatypes. Unresolvable chains (cycles) are reported per
// chain and the affected constraint left unresolved; resolution continues
// with the remaining constraints.
func (c *completer) resolveAliasConstraints() {
	// First, resolve DataType constraints (they may reference each other)
	for _, dt := range c.schema.DataTypesSlice() {
		if alias, isAlias := dt.Constraint().(AliasConstraint); isAlias && !alias.IsResolved() {
			resolved, success := c.resolveAliasChain(alias.DataTypeName(), dt.Span(), make(map[string]bool))
			if !success {
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
					continue
				}
				p.setConstraint(resolved)
			}
		}
	}
}

// aliasResolution classifies an import qualifier against the resolution map —
// the single source of truth for alias state during completion. It is the one
// interpreter of that map's three-state sentinel, so the deferral and
// resolution sites cannot disagree about an alias the way a Schema.ImportByAlias
// reader and a resolvedImports reader once could (the divergence that let a
// completion-rejected alias resolve silently on one path while re-blaming
// E_UNKNOWN_TYPE on another). Callers layer their own registry policy on top.
type aliasResolution int

const (
	// aliasUnattempted: no resolution map was supplied (a Builder schema
	// without imports, or a deferred-completion bridge). classifyQualifier is
	// map-only, so this stays distinct from aliasAbsent at classification time;
	// but every completion consumer reaches the classification only with a
	// registry present (each guards on c.registry first), where a nil map can
	// only mean a zero-import schema — so a qualified reference names no import
	// and is treated exactly like aliasAbsent: a genuine unknown, not a
	// deferral.
	aliasUnattempted aliasResolution = iota
	// aliasAbsent: the map was supplied but names no such import — a genuine
	// unknown reference (a user typo).
	aliasAbsent
	// aliasDeferred: the import is declared but unresolved (failed load or
	// rejected declaration); its root cause already carries a diagnostic.
	aliasDeferred
	// aliasResolved: the import is declared and resolved to a SourceID.
	aliasResolved
)

// classifyQualifier interprets the resolution map for one qualifier. It reads
// only the map, never the registry: callers decide their own registry policy
// (resolution looks the SourceID up; the primary-key check rejects a key whose
// type cannot be resolved under genuine deferral).
func (c *completer) classifyQualifier(qualifier string) (aliasResolution, location.SourceID) {
	if c.resolvedImports == nil {
		return aliasUnattempted, location.SourceID{}
	}
	res, declared := c.resolvedImports[qualifier]
	switch {
	case !declared:
		return aliasAbsent, location.SourceID{}
	case res.deferred:
		return aliasDeferred, location.SourceID{}
	default:
		return aliasResolved, res.sourceID
	}
}

// referenceDeferred reports whether a qualified reference whose target did not
// resolve should be skipped silently rather than reported as unknown. Two cases
// defer: no registry is available to resolve cross-schema refs (so the
// reference is validated at link time), or the qualifier names a declared
// import whose load failed (whose failure already carries the root-cause
// diagnostic). An unqualified reference, and a qualifier that names no declared
// import while a registry IS present (including a nil resolution map — a Builder
// or bridge with zero imports), is NOT deferred: it is a genuine unknown.
//
// This is the single deferral predicate for the resolveTypeRef-based reference
// sites (extends clauses, relation and composition targets), folding the
// registry check in so no site re-derives it — and so a future site cannot get
// the registry/nil-map interaction wrong, as one once did.
func (c *completer) referenceDeferred(qualifier string) bool {
	if qualifier == "" {
		return false
	}
	if c.registry == nil {
		return true
	}
	state, _ := c.classifyQualifier(qualifier)
	return state == aliasDeferred
}

// primaryKeyTypeDeferred reports whether a primary property's constraint
// bottoms out in an alias whose unresolvability is already diagnosed in
// this pass. For those shapes the primary-key type check is skipped:
// rejecting the key for its type would re-blame a reported root cause.
//
// A primary's type must be resolvable for identity, so an unresolved terminal
// is normally rejected. It is deferred only when alias resolution already
// reported the reference: an unqualified terminal (an undeclared name is
// E_UNKNOWN_TYPE; a declared name can sit unresolved only on a reported cycle),
// or any qualified terminal once a registry is present — with a registry, alias
// resolution reports every unresolvable qualified shape (an absent qualifier, a
// declared-but-failed import, a resolved import lacking the datatype, or a
// resolved import whose schema is unregistered). Without a registry a
// cross-schema ref defers silently to link time, so nothing reported it.
//
// This is the deliberately-separate companion to [completer.referenceDeferred]:
// that predicate decides whether a *reference* is reported; this one decides
// whether the primary-key *type check* runs on top of it. The "registry present
// ⇒ defer any qualified terminal" rule is correct only because
// [completer.resolveAliasChain] reports every unresolvable qualified datatype
// shape when a registry is present. The two must stay in lockstep: a qualified
// shape that resolveAliasChain ever stops reporting must also stop being
// deferred here, or the primary key's unresolvable type would go unreported —
// the exact swallow the surrounding diagnostic-completeness work exists to
// prevent. The reachable shapes are pinned by the qualified-primary-key tests in
// schema/diagnostic_completeness_test.go.
func (c *completer) primaryKeyTypeDeferred(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	terminal, isAlias := ResolveAlias(constraint).(AliasConstraint)
	if !isAlias || terminal.IsResolved() {
		return false
	}
	qualifier, _ := parseQualifiedName(terminal.DataTypeName())
	if qualifier == "" {
		// An unqualified unresolved terminal was reported by alias resolution
		// (E_UNKNOWN_TYPE for an undeclared name, the cycle diagnostic for a
		// cyclic chain), so the type check defers.
		return true
	}
	// A qualified terminal still unresolved after alias resolution: deferred iff
	// a registry was available to resolve and report it. (A resolved datatype
	// would have left the terminal resolved, tripping the early return above.)
	return c.registry != nil
}

// reportUnknownAlias emits an E_UNKNOWN_TYPE for an unresolvable datatype
// reference unless suppress is set. The chain recursion in resolveAliasChain —
// resolving a found datatype's own unresolved underlying — suppresses it: that
// datatype's own resolution already blamed the unknown terminal, so re-walking
// it through a referencing property or datatype must not re-report the same
// name. Direct references each report independently.
func (c *completer) reportUnknownAlias(suppress bool, span location.Span, format string, args ...any) {
	if suppress {
		return
	}
	c.errorf(span, diag.E_UNKNOWN_TYPE, format, args...)
}

// resolveAliasChain resolves a datatype name to its underlying constraint.
// The visited map tracks seen names for cycle detection. Returns the resolved
// AliasConstraint and success status.
//
// A name that cannot name a datatype is an error (E_UNKNOWN_TYPE): an
// undeclared local name, a qualifier that names no declared import, or a
// resolved import that lacks the datatype. The reference is left unresolved
// silently only when resolution is genuinely deferred — no registry to resolve
// a cross-schema ref, a declared-but-failed import whose failure was already
// reported, or an import whose schema is absent from the registry.
//
// suppressUnknown gates the E_UNKNOWN_TYPE reports (not the cycle report): the
// self-recursion that resolves a found datatype's own unresolved underlying
// sets it, because that datatype's own resolution already blamed the unknown
// terminal — re-walking the chain through a referencing property or datatype
// must not re-report the same name.
func (c *completer) resolveAliasChain(dataTypeName string, span location.Span, visited map[string]bool) (Constraint, bool) {
	// The self-recursion below (resolving a found datatype's own underlying)
	// inherits a non-empty visited map; every external caller passes a fresh
	// empty one. That distinction IS the suppress signal — re-walking a declared
	// datatype's chain must not re-report an unknown its own resolution already
	// blamed, while a direct reference reports independently.
	suppressUnknown := len(visited) > 0

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
		// Local datatype. The lookup needs no registry, so an undeclared
		// name is an error in every resolution context: a property's type
		// must be a built-in or a declared (possibly imported) datatype.
		dt, found = c.dataIndex[name]
		if !found {
			c.reportUnknownAlias(suppressUnknown, span,
				"unknown type %q in datatype reference; a property type must be a built-in or a declared datatype",
				dataTypeName)
			return nil, false
		}
	} else {
		// Cross-schema reference. Defer silently in exactly the cases the
		// resolveTypeRef sites do — [completer.referenceDeferred] is the shared
		// predicate (no registry to resolve against, or a declared-but-failed
		// import whose failure already carries the root cause), so datatype
		// references and extends/relation references cannot diverge on the
		// registry/nil-map interaction the way they once did.
		if c.referenceDeferred(qualifier) {
			return NewAliasConstraint(dataTypeName, nil), true
		}
		// Not deferred, and a registry is present: the qualifier either names no
		// import (a genuine unknown) or resolves to one.
		state, sourceID := c.classifyQualifier(qualifier)
		if state != aliasResolved {
			// aliasAbsent, or aliasUnattempted (a nil resolution map — reached
			// here only with a registry present, i.e. a Builder schema or bridge
			// with zero imports): the qualifier names no declared import.
			c.reportUnknownAlias(suppressUnknown, span,
				"unknown type %q in datatype reference: no import declared with alias %q",
				dataTypeName, qualifier)
			return nil, false
		}
		importedSchema, ok := c.registry.LookupBySourceID(sourceID)
		if !ok {
			// The import resolved to a SourceID but its schema is absent from the
			// registry — an internal inconsistency (every resolved import is
			// registered on both the Load and Builder paths). Report it as unknown
			// rather than leaving the reference silently unresolved, matching how
			// resolveTypeRef surfaces the same state for relation/extends targets.
			c.reportUnknownAlias(suppressUnknown, span,
				"unknown type %q in datatype reference: schema imported as %q is not registered",
				dataTypeName, qualifier)
			return nil, false
		}
		dt, found = importedSchema.DataType(name)
		if !found {
			c.reportUnknownAlias(suppressUnknown, span,
				"unknown type %q in datatype reference: datatype %q does not exist in the schema imported as %q",
				dataTypeName, name, qualifier)
			return nil, false
		}
	}

	underlying := dt.Constraint()

	// If underlying is an unresolved alias, resolve it first. Passing the
	// inherited (non-empty) visited map suppresses its E_UNKNOWN_TYPE: dt is a
	// declared datatype, so dt's own resolution already blamed any unknown
	// terminal in its chain — re-blaming it through this reference would report
	// the same name twice.
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
// Specifically checks that relation properties do not use Vector or List
// types (per spec). Findings are per-property diagnostics; the constraint
// objects themselves are unchanged by the check.
func (c *completer) validateRelationProperties() {
	for _, t := range c.schema.TypesSlice() {
		for rel := range t.Associations() {
			for _, p := range rel.PropertiesSlice() {
				if isVectorConstraint(p.Constraint()) {
					c.errorf(p.Span(), diag.E_INVALID_CONSTRAINT,
						"relationship property %q cannot use Vector type", p.Name())
				}
				if isListConstraint(p.Constraint()) {
					c.errorf(p.Span(), diag.E_LIST_ON_EDGE,
						"relationship property %q cannot use List type", p.Name())
				}
			}
		}
	}
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
		// A type whose merged property set carries a declared `primary` flag
		// never draws absence-blame: an empty extracted key slice despite a
		// declared primary means key extraction rejected the declaration for
		// its type (E_INVALID_PRIMARY_KEY_TYPE), and stacking
		// E_NO_PRIMARY_KEY on top would re-report the same root cause.
		if hasDeclaredPrimary(t) {
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

// hasDeclaredPrimary reports whether any merged property carries the
// primary flag, independent of whether key extraction accepted it into
// the type's primary keys.
func hasDeclaredPrimary(t *Type) bool {
	for _, p := range t.AllPropertiesSlice() {
		if p.IsPrimaryKey() {
			return true
		}
	}
	return false
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

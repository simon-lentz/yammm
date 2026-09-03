package schema

import (
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
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
//
// "Deferred" has one source that flows one way, not parallel representations
// that must be kept in agreement by hand: the loader folds each importBinding
// into this map (a failed binding becomes a deferred entry; see loadSource),
// [completer.indexImports] then builds each schema Import from the map (a
// deferred entry yields a zero-ResolvedSourceID Import that the loader's wiring
// loop leaves schema-less), and completion reads only the map — via
// [completer.classifyQualifier], the single interpreter named on
// [resolvedImportMap], through which every qualifier-resolving site routes.
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
// Completion runs a fixed sequence of phases: it indexes the declared types and
// datatypes, validates and indexes imports, resolves datatype-alias constraints,
// and linearizes inheritance to merge inherited members; it then validates
// primary keys, relation edge properties, name collisions, relation targets, and
// invariant expressions. Inheritance-cycle detection is the one hard gate — it
// aborts completion, because the downstream phases cannot run on a cyclic graph;
// every other phase collects its diagnostics and continues, so one schema reports
// every independent error in a single pass.
//
// Errors are collected in the provided collector. completeModel returns the
// completed *Schema, or nil if this completion contributed any Fatal or Error
// issue — a per-completion error gate, so when the collector is shared across a
// multi-schema load each schema is judged by its own error delta. The registry
// is optional, but a qualified reference resolves or is reported either way —
// only a declared-but-failed import defers. The resolvedImports map carries
// each import alias's resolution (or deferral) from the loader.
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
		gate:                    newErrorGate(collector),
		unresolvedSupertypeMemo: make(map[*Type]bool),
		diagnosedAnnotations:    make(map[*Annotation]bool),
		conflictedProperties:    make(map[*Property]bool),
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
//
// INVARIANT: the gate is an entry/exit delta on one shared collector, so it is
// correct only while accumulation into that collector is serial — the guarded
// operation and the nested loads it triggers are the sole writers between
// newErrorGate and tripped. A sibling writing errors to the same collector
// concurrently within that window would be miscounted against this gate. The
// loader is serial today (loadImports walks imports in sequence, one collector
// per Load); parallelizing import loading would break this assumption, and the
// fix would then be per-schema sub-collectors merged upward
// ([diag.Collector.Merge] already folds counts and truncation losslessly for
// exactly that), not a delta on a shared collector.
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

	// unresolvedSupertypeMemo caches [completer.hasUnresolvedSupertype] per type:
	// the inheritance graph and resolution state are fixed across the phases that
	// query it (validatePrimaryKeys, validateInvariantExpressions,
	// validateAnnotations), so a type's answer cannot change between calls.
	unresolvedSupertypeMemo map[*Type]bool

	// staticMembers caches [completer.membersOf] per type: the merged members
	// keyed as an invariant expression writes them.
	staticMembers map[*Type]staticMembers

	// pendingShadowed holds the annotations own re-declarations dropped, queued
	// during linearization and emitted by [completer.flushShadowedAnnotations]
	// once validation has established which of them are usable.
	pendingShadowed []shadowedAnnotation

	// diagnosedAnnotations marks the annotations that drew a diagnostic of their
	// own during validation, so the shadow warning does not advise re-stating an
	// annotation this same load rejects.
	diagnosedAnnotations map[*Annotation]bool

	// conflictedProperties marks own declarations that drew E_PROPERTY_CONFLICT,
	// so the shadow warning does not advise editing a declaration the load has
	// already rejected outright.
	conflictedProperties map[*Property]bool
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

	// Phase 3c: Bind every own relation to its target's identity. This runs
	// before inheritance merges so Relation.Equal compares identities, and a
	// relation that shadows an inherited one under the same name collides
	// when the targets differ.
	c.resolveRelationTargets()

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

	// Phase 7: Validate relation targets against the rules that need the
	// merged view — a composition reaches a concrete part, an association a
	// concrete non-part, and a part type carries no association.
	c.validateRelationTargets()

	// Phase 7b: Validate invariant expressions (static property checking)
	c.validateInvariantExpressions()

	// Phase 7c: Validate annotations (name, placement, args, target eligibility).
	// Runs after linearization so property-reference arguments can resolve
	// against inherited properties.
	c.validateAnnotations()

	// Phase 7d: Emit the shadowed-annotation warnings linearization queued. It
	// must follow 7c: the warning tells the user to re-state the annotation, which
	// is wrong advice for one this same load rejects, and only 7c knows which
	// those are.
	c.flushShadowedAnnotations()

	// Final check for any errors THIS completion collected — its own error
	// delta, not the shared collector's total: errors collected before this
	// completion began belong to other schemas in the same load.
	if c.gate.tripped() {
		return nil
	}

	// Phase 8: Seal this schema's own declarations.
	c.sealOwnDeclarations()

	return c.schema
}

// sealOwnDeclarations closes this schema's own declarations to mutation.
// Relations come from the own sets: a merged set carries an ancestor's
// [Relation] pointers, already sealed by their owning schema, and re-sealing
// them races two loads sharing a [Registry] ([TestSeal_SharedRegistryConcurrentLoads]).
func (c *completer) sealOwnDeclarations() {
	for _, t := range c.schema.types {
		t.seal()
		for rel := range t.Associations() {
			rel.seal()
		}
		for rel := range t.Compositions() {
			rel.seal()
		}
	}

	for _, dt := range c.schema.dataTypes {
		dt.seal()
	}
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
		invariants := c.convertInvariants(td.Name, td.Invariants)
		t.setInvariants(invariants)

		// Convert and set type-level annotations (@@name members)
		t.setAnnotations(convertAnnotations(td.Annotations))

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

	// deferRejectedAlias records a rejected declaration's alias as deferred in
	// the resolution map — the single deferral signal both reference kinds read:
	// datatype references via resolveAliasChain, and extends/relation references
	// via referenceDeferred (through classifyQualifier). A reference through the
	// alias then defers to the rejection's root-cause diagnostic instead of
	// re-blaming E_UNKNOWN_TYPE; and on the Load path — where the loader may
	// already have resolved this alias before this phase rejected it — the
	// deferred entry overwrites that resolution, so a datatype reference cannot
	// silently resolve against the rejected import. The nil check skips contexts
	// with no resolution map (a completeModel bridge, or a Builder with no
	// imports), where there is nothing to record or defer against.
	deferRejectedAlias := func(id *importDecl) {
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
				// Key-absent: the resolution map reached completion without an
				// entry for this alias — a bug in whichever front door built
				// it, not a user error, which is why it is E_INTERNAL rather
				// than a member of the import-resolution family. Neither door
				// can reach it: the loader binds or fails every alias before
				// completion, and the Builder discards its resolution map
				// whenever a path fails. Reported and skipped.
				c.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
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
			convertAnnotations(pd.Annotations),
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

		fieldName := strings.ToLower(rd.Name)

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
					nil, // relation edge properties take no annotations
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

// convertInvariants converts invariantDecl to Invariant. An invariant's
// message is its identity, so a message declared twice on one type is a
// duplicate: the first declaration is kept and the second reported.
func (c *completer) convertInvariants(typeName string, decls []*invariantDecl) []*Invariant {
	invs := make([]*Invariant, 0, len(decls))
	first := make(map[string]*invariantDecl, len(decls))

	for _, id := range decls {
		if id == nil {
			continue
		}
		if prior, dup := first[id.Name]; dup {
			c.errorfRelated(id.Span, diag.E_DUPLICATE_INVARIANT,
				[]location.RelatedInfo{{Span: prior.Span, Message: "first declared here"}},
				"type %q declares invariant %q twice", typeName, id.Name)
			continue
		}
		first[id.Name] = id
		invs = append(invs, newInvariant(id.Name, id.Expr, id.Span, id.Documentation))
	}

	return invs
}

// convertAnnotations turns parsed annotationDecls into model Annotations,
// mirroring convertInvariants. Argument semantic kinds start ArgUnvalidated;
// validateAnnotations stamps them after linearization, before sealing. Used for
// both type-level annotations (in indexTypes) and property-trailing ones (in
// convertProperties), so property and type annotations share one conversion.
func convertAnnotations(decls []*annotationDecl) []*Annotation {
	if len(decls) == 0 {
		return nil
	}
	anns := make([]*Annotation, 0, len(decls))
	for _, d := range decls {
		if d == nil {
			continue
		}
		var args []AnnotationArg
		if len(d.Args) > 0 {
			args = make([]AnnotationArg, 0, len(d.Args))
			for _, a := range d.Args {
				args = append(args, AnnotationArg{
					text:      a.Text,
					tokenKind: a.Token,
					kind:      ArgUnvalidated,
					span:      a.Span,
					raw:       a.Raw,
				})
			}
		}
		ann := newAnnotation(d.Name, args, d.Documentation, d.Span)
		ann.setDetachedFrom(d.DetachedFromLine)
		anns = append(anns, ann)
	}
	return anns
}

// resolveAliasConstraints resolves all AliasConstraint references in
// properties and datatypes. Unresolvable chains (cycles) are reported per
// chain and the affected constraint left unresolved; resolution continues
// with the remaining constraints.
func (c *completer) resolveAliasConstraints() {
	// First, resolve DataType constraints (they may reference each other)
	for _, dt := range c.schema.dataTypes {
		if alias, isAlias := dt.Constraint().(AliasConstraint); isAlias && !alias.IsResolved() {
			resolved, success := c.resolveAliasChain(alias.DataTypeName(), dt.Span(), make(map[string]bool))
			if !success {
				continue
			}
			dt.setConstraint(resolved)
		}
	}

	// Resolve List element aliases in DataTypes
	for _, dt := range c.schema.dataTypes {
		if _, isList := dt.Constraint().(ListConstraint); isList {
			resolved, success := c.resolveListElementAliases(dt.Constraint(), dt.Span())
			if !success {
				continue
			}
			dt.setConstraint(resolved)
		}
	}

	// Then, resolve property constraints on all types
	for _, t := range c.schema.types {
		for p := range t.Properties() {
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
	for _, t := range c.schema.types {
		for p := range t.Properties() {
			if _, isList := p.Constraint().(ListConstraint); isList {
				resolved, success := c.resolveListElementAliases(p.Constraint(), p.Span())
				if !success {
					continue
				}
				p.setConstraint(resolved)
			}
		}
	}

	// Resolve relation edge-property constraints. Associations are the only
	// relation kind that carries a property block (compositions have none by
	// grammar), and their properties resolve exactly like type properties:
	// against this schema's own datatype index and import bindings, so a
	// declaring-schema-relative reference keeps declaring-schema semantics on
	// inherited relations (subtypes share the declaring type's *Relation).
	// Resolution must precede validateRelationProperties, whose Vector/List
	// bans unwrap aliases via Resolved().
	for _, t := range c.schema.types {
		for rel := range t.Associations() {
			for p := range rel.Properties() {
				if alias, isAlias := p.Constraint().(AliasConstraint); isAlias && !alias.IsResolved() {
					resolved, success := c.resolveAliasChain(alias.DataTypeName(), p.Span(), make(map[string]bool))
					if !success {
						continue
					}
					p.setConstraint(resolved)
				}
			}
			// List-shaped edge properties are rejected by the List-on-edge ban
			// before a schema is produced, so resolved elements are observable
			// only in diagnostics; the pass keeps every property the completer
			// owns in the same state on exit from resolveAliasConstraints.
			for p := range rel.Properties() {
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
	// aliasAbsent: the qualifier names no declared import — a genuine unknown
	// reference (a user typo). This folds in the no-map case (a Builder schema
	// or deferred-completion bridge with zero imports): a completion consumer
	// reaches classification only with a registry present, where a nil map can
	// only mean a zero-import schema, so any qualified reference names no import
	// — exactly aliasAbsent, not a deferral.
	aliasAbsent aliasResolution = iota
	// aliasDeferred: the import is declared but unresolved (failed load or
	// rejected declaration); its root cause already carries a diagnostic.
	aliasDeferred
	// aliasResolved: the import is declared and resolved to a SourceID.
	aliasResolved
)

// classifyQualifier interprets the resolution map for one qualifier. It reads
// only the map, never the registry: its callers ([completer.referenceDeferred]
// and [completer.resolveAliasChain]) layer their own registry policy on top.
func (c *completer) classifyQualifier(qualifier string) (aliasResolution, location.SourceID) {
	if c.resolvedImports == nil {
		// No resolution map means zero declared imports (with or without a
		// registry), so any qualifier names no import — absent.
		return aliasAbsent, location.SourceID{}
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

// referenceDeferred reports whether a qualified reference's failure to resolve
// stays silent: only a declared-but-failed import defers, its root cause being
// already reported. An undeclared qualifier is unknown regardless of registry
// presence — no link step ever comes, so deferring would hide a dangling ref.
func (c *completer) referenceDeferred(qualifier string) bool {
	if qualifier == "" {
		return false
	}
	state, _ := c.classifyQualifier(qualifier)
	return state == aliasDeferred
}

// primaryKeyTypeDeferred reports whether a primary property's primary-key TYPE
// check should be skipped because its constraint bottoms out in an unresolved
// alias. Such a terminal's type is unknowable here, so validating it against the
// allowed key types (String, UUID, Date, Timestamp) is meaningless — and its
// unresolvability is already accounted for by alias resolution (phase 3,
// [completer.resolveAliasConstraints]), which runs before this check:
//
//   - reported — an undeclared local name, or a qualifier naming no declared
//     import, draws E_UNKNOWN_TYPE; a cyclic chain draws the cycle diagnostic;
//   - silently deferred — a declared-but-failed import, whose failure is the
//     root cause and already carries its own diagnostic.
//
// Either way the root cause is owned elsewhere, so the type check defers rather
// than stacking a (usually misleading) E_INVALID_PRIMARY_KEY_TYPE on top of a
// reported error. A resolved terminal (a builtin, or an alias resolved to one)
// is checked normally.
//
// Keying off the terminal's resolved state alone — not a re-derived
// qualifier/registry prediction — is what keeps this in step with
// [completer.resolveAliasChain]: every unresolved terminal it leaves behind was
// reported or deferred there, and a resolved one is the only shape this checks.
//
// This couples to [completer.resolveAliasConstraints] leaving a deferred
// terminal as an unresolved AliasConstraint. A change
// that instead resolved such terminals to a placeholder would silently flip this
// predicate to false and resurface a mis-attributed E_INVALID_PRIMARY_KEY_TYPE,
// so the two must move together.
func (c *completer) primaryKeyTypeDeferred(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	terminal, isAlias := ResolveAlias(constraint).(AliasConstraint)
	return isAlias && !terminal.IsResolved()
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
// silently only for a declared-but-failed import, whose failure was already
// reported.
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
		// Cross-schema reference: reads the tri-state directly (it needs the
		// SourceID) but defers on the same one condition [completer.referenceDeferred] does.
		state, sourceID := c.classifyQualifier(qualifier)
		switch state {
		case aliasDeferred:
			// A declared import whose load/declaration failed; its failure
			// already carries the root cause, so defer rather than re-blame.
			return NewAliasConstraint(dataTypeName, nil), true
		case aliasAbsent:
			// The qualifier names no declared import: a genuine unknown, with
			// or without a registry — no link step will ever resolve it.
			c.reportUnknownAlias(suppressUnknown, span,
				"unknown type %q in datatype reference: no import declared with alias %q",
				dataTypeName, qualifier)
			return nil, false
		}
		// aliasResolved: the import resolved to sourceID. The registry is
		// non-nil here by construction — [Builder.resolveImports] returns a nil
		// resolution map whenever it is absent, and a nil map classifies every
		// qualifier absent, so this branch is unreachable without one.
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
	for _, t := range c.schema.types {
		for rel := range t.Associations() {
			for p := range rel.Properties() {
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
	for _, t := range c.schema.types {
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
//
// It short-circuits on the first primary rather than counting: the answer is a
// bool, and a type with a hundred merged properties should not walk them all to
// learn that its first one is a key. The @index redundancy rule, which once
// shared a counting helper with this, now reads the EXTRACTED keys
// ([Type.PrimaryKeys]) instead — a stricter question that this gate deliberately
// does not ask, since a primary rejected for its type is still a declared one.
func hasDeclaredPrimary(t *Type) bool {
	for p := range t.AllProperties() {
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
// members merged, so it is a leaf here.
//
// Every type reached is memoized, not just the entry: the recursion routes back through
// this method, so each supertype's answer is recorded as it is determined and a chain is
// walked once across all queries rather than re-walked per query. The three phases that
// call this (validatePrimaryKeys, validateInvariantExpressions, and validateAnnotations)
// run after inheritance and resolution are fixed, so a type's answer cannot change
// between calls.
//
// detectCycles rejects inheritance cycles before this runs, so the graph is acyclic; the
// memo is seeded false before recursing purely so the walk still terminates should that
// ever cease to hold, then overwritten with the real answer.
func (c *completer) hasUnresolvedSupertype(t *Type) bool {
	if cached, ok := c.unresolvedSupertypeMemo[t]; ok {
		return cached
	}
	c.unresolvedSupertypeMemo[t] = false // cycle-termination guard; overwritten below

	result := false
	for ref := range t.Inherits() {
		super := c.resolveTypeRef(ref)
		if super == nil {
			result = true
			break
		}
		// Recurse only into local supertypes; a resolved cross-schema supertype is a
		// fully-merged leaf. A qualified ref that resolved cannot be local.
		if ref.Qualifier() == "" && c.hasUnresolvedSupertype(super) {
			result = true
			break
		}
	}
	c.unresolvedSupertypeMemo[t] = result
	return result
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

// errorfRelated is [completer.errorf] for a diagnostic that points at other
// declarations as well as its own. Every completer site that emits related
// locations routes through here so the span policy is decided once: a related
// entry without a usable location is dropped rather than rendered.
func (c *completer) errorfRelated(
	span location.Span, code diag.Code, related []location.RelatedInfo, format string, args ...any,
) {
	msg := fmt.Sprintf(format, args...)
	issue := diag.NewIssue(diag.Error, code, msg)
	if !span.IsZero() {
		issue = issue.WithSpan(span)
	}
	if located := locatedRelated(related); len(located) > 0 {
		issue = issue.WithRelated(located...)
	}
	c.collector.Collect(issue.Build())
}

// locatedRelated returns the related entries that carry a usable location.
//
// A span-less entry is worse than no entry: the text renderer writes its
// "note:" line and then suppresses the location, so the reader gets a bare
// label pointing nowhere — and one per member, so a clash whose sides hold
// several declarations renders as a stack of identical, information-free
// lines. The LSP drops such entries outright, so the two surfaces disagree
// about what the diagnostic even contains. Schemas built through
// [Builder] carry no spans, which is where this arises.
func locatedRelated(related []location.RelatedInfo) []location.RelatedInfo {
	located := related[:0:0]
	for _, r := range related {
		if !r.Span.IsZero() {
			located = append(located, r)
		}
	}
	return located
}

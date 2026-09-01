package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/trace"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Graph builds an in-memory data structure from validated instances.
//
// Graph is safe for concurrent use from multiple goroutines. Multiple
// callers may invoke [Graph.Add] and [Graph.AddComposed] concurrently;
// the graph handles forward references and duplicate detection atomically.
//
// All operations accept a [context.Context] for cancellation. Cancellation
// does not corrupt internal state; partial results may be inspected.
type Graph struct {
	schema *schema.Schema
	config graphConfig
	mu     sync.RWMutex

	// closureSchemas is every schema in the bound schema's import closure, and
	// ownedSchemas the subset this graph accepts roots from. Both are fixed at
	// construction: the schema is immutable and its imports are wired before it
	// is observable.
	closureSchemas map[location.SourceID]bool
	ownedSchemas   map[location.SourceID]bool

	// instances indexes instances by TypeID, then by PK string.
	instances map[schema.TypeID]map[string]*Instance

	// edges holds all resolved association edges.
	edges []*Edge

	// pending holds unresolved forward references.
	// Key: pendingKey{targetTypeID, targetKey}
	// Multiple sources can reference the same target, so we store a slice.
	pending map[pendingKey][]*pendingEdge

	// duplicates holds duplicate PK records.
	duplicates []*Duplicate

	// collector accumulates diagnostics.
	collector *diag.Collector

	// attestValues accumulates the Values attestation: it stays true only
	// while every installed root and composed child arrived validated.
	// A seeded graph starts from the loaded header's claim. Guarded by mu.
	attestValues bool
}

// pendingKey identifies a pending edge by its target.
type pendingKey struct {
	targetTypeID schema.TypeID
	targetKey    string
}

// pendingEdge holds data for an unresolved forward reference.
type pendingEdge struct {
	source       *Instance
	relation     string
	jsonField    string // normalized JSON field name (lower_snake form)
	targetType   schema.TypeID
	targetKey    string
	properties   immutable.Properties
	isRequired   bool
	reasonDetail string // "absent", "empty", or ""
}

// New creates a new Graph bound to the given schema.
//
// Panics if schema is nil (programmer error). A nil schema is never valid
// as there is no way to validate instances without a schema.
func New(s *schema.Schema, opts ...Option) *Graph {
	if s == nil {
		panic("graph.New: nil schema")
	}

	cfg := graphConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	closure := make(map[location.SourceID]bool)
	for _, dep := range s.Closure() {
		closure[dep.SourceID()] = true
	}
	owned := map[location.SourceID]bool{s.SourceID(): true}
	for imp := range s.Imports() {
		if dep := imp.Schema(); dep != nil {
			owned[dep.SourceID()] = true
		}
	}

	return &Graph{
		schema:         s,
		config:         cfg,
		closureSchemas: closure,
		ownedSchemas:   owned,
		instances:      make(map[schema.TypeID]map[string]*Instance),
		pending:        make(map[pendingKey][]*pendingEdge),
		collector:      diag.NewCollector(0), // unlimited
		attestValues:   true,
	}
}

// Add adds a validated instance to the graph.
//
// Add indexes the instance by its TypeID and primary key, creates edges
// for associations, extracts composed children, and resolves any pending
// forward references that target this instance.
//
// Return semantics: check [diag.Result.OK] for success. A non-OK result
// contains diagnostic issues and the graph is unchanged — every rejection is
// decided before the instance installs. The package doc's Error Handling
// section enumerates the codes Add can emit.
//
// Panics if g is nil, inst is nil, or inst's schema does not match the graph's schema.
func (g *Graph) Add(ctx context.Context, inst *instance.ValidInstance) diag.Result {
	// Programmer errors: a caller cannot recover from any of these.
	if g == nil {
		panic("graph.Add: nil *Graph receiver")
	}

	if inst == nil {
		panic("graph.Add: nil ValidInstance")
	}

	if ctx == nil {
		panic("graph.Add: nil context")
	}

	opCollector := diag.NewCollector(0)

	// Opened before the context check so a cancelled Add is still traced.
	op := trace.Begin(
		ctx, g.config.logger, "yammm.graph.add",
		slog.String("type", inst.TypeName()),
		slog.String("pk", inst.PrimaryKey().String()),
	)
	defer func() { op.End(opCollector.Result().Err()) }()

	if err := ctx.Err(); err != nil {
		return g.reject(opCollector, diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.Add cancelled: "+err.Error()).Build())
	}

	typeID := inst.TypeID()

	// Schema mismatch check — programmer error
	if !g.isKnownSchema(typeID.SchemaPath()) {
		panic("graph.Add: instance schema does not match graph schema")
	}

	typ, ok := g.schema.TypeByID(typeID)
	if !ok || !g.ownsType(typeID) {
		msg := fmt.Sprintf("type %q not found in schema", inst.TypeName())
		builder := diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND, msg).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName())
		if pk := inst.PrimaryKey(); pk.Len() > 0 {
			builder = builder.WithDetail(diag.DetailKeyPrimaryKey, pk.String())
		}
		if strings.Contains(inst.TypeName(), ".") {
			builder = builder.WithHint("if this type is from a transitively imported schema, add a direct import to access it")
			builder = builder.WithDetail(diag.DetailKeyTypeSchema, typeID.SchemaPath().String())
		}
		return g.reject(opCollector, builder.Build())
	}

	if !typ.HasPrimaryKey() {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_MISSING_PK,
			fmt.Sprintf("type %q has no primary key; cannot add to graph", inst.TypeName())).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build())
	}

	if typ.IsPart() {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("part type %q cannot be added directly; use AddComposed", inst.TypeName())).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build())
	}

	if typ.IsAbstract() {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_ABSTRACT_TYPE,
			fmt.Sprintf("abstract type %q cannot be instantiated in the graph", inst.TypeName())).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build())
	}

	// An empty key installs under the literal "[]", and a key disagreeing with
	// a present key property is a forged address.
	if err := checkInstanceKey(typ, inst); err != nil {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
			fmt.Sprintf("instance of type %q: %s", inst.TypeName(), err)).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).
			WithDetail(diag.DetailKeyPrimaryKey, inst.PrimaryKey().String()).Build())
	}

	// One walk checks the instance and builds the tree to install. Every
	// rejection is decided here, before the first mutation, so a non-OK Add
	// leaves no trace of the record it refused.
	b := newInstanceBuilder(g, opCollector)
	var staged []stagedEdge
	graphInst := b.build(typ, inst, &staged)
	// Merged whatever the severity: the walks this replaced collected into
	// both collectors at every site, and gating on HasErrors would drop the
	// first sub-Error issue the check ever produces from Snapshot.Diagnostics().
	g.collector.Merge(opCollector.Result())
	if opCollector.HasErrors() {
		return opCollector.Result()
	}

	typeName := graphInst.TypeName()
	pkString := inst.PrimaryKey().String()

	g.mu.Lock()
	defer g.mu.Unlock()

	typeInstances := g.instances[typeID]
	if typeInstances != nil {
		if existing, found := typeInstances[pkString]; found {
			// Duplicate.Instance is documented as carrying no composed
			// children, so the record gets its own childless instance.
			rejected := newInstance(typeName, typeID, inst.PrimaryKey(), inst.Properties(), inst.Provenance(), inst.Validated())
			diagBuilder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %q", pkString, typeName)).
				WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyPrimaryKey, pkString)
			if prov := inst.Provenance(); prov != nil {
				diagBuilder = diagBuilder.WithSpan(prov.Span())
			}
			dup := newDuplicate(rejected, existing, nil, "", diagBuilder.Build())
			g.duplicates = append(g.duplicates, dup)
			trace.Warn(
				ctx, g.config.logger, "duplicate primary key",
				slog.String("type", typeName),
				slog.String("pk", pkString),
			)
			return g.reject(opCollector, dup.Diagnostic)
		}
	} else {
		g.instances[typeID] = make(map[string]*Instance)
	}

	// Commit. The rejected-duplicate path above never reaches this: a rejected
	// payload is outside the Values attestation.
	g.attestValues = g.attestValues && b.attested
	g.instances[typeID][pkString] = graphInst

	for _, se := range staged {
		if se.reason == "" {
			if targetInst := g.findInstance(se.targetType, se.targetKey); targetInst != nil {
				g.edges = append(g.edges, newEdge(se.relation, graphInst, targetInst, se.properties))
				trace.Debug(
					ctx, g.config.logger, "edge resolved",
					slog.String("relation", se.relation),
					slog.String("source_type", typeName),
					slog.String("source_pk", pkString),
					slog.String("target_type", se.targetTypeName),
					slog.String("target_pk", se.targetKey),
				)
				continue
			}
			trace.Debug(
				ctx, g.config.logger, "forward reference created",
				slog.String("relation", se.relation),
				slog.String("source_type", typeName),
				slog.String("source_pk", pkString),
				slog.String("target_type", se.targetTypeName),
				slog.String("target_pk", se.targetKey),
			)
		}
		pk := pendingKey{targetTypeID: se.targetType, targetKey: se.targetKey}
		g.pending[pk] = append(g.pending[pk], &pendingEdge{
			source:       graphInst,
			relation:     se.relation,
			jsonField:    se.jsonField,
			targetType:   se.targetType,
			targetKey:    se.targetKey,
			properties:   se.properties,
			isRequired:   se.isRequired,
			reasonDetail: se.reason, // Check renders the empty form as "target_missing".
		})
	}

	// Resolve every pending edge that targets this instance.
	pk := pendingKey{targetTypeID: typeID, targetKey: pkString}
	if pendingList, ok := g.pending[pk]; ok {
		for _, pend := range pendingList {
			g.edges = append(g.edges, newEdge(pend.relation, pend.source, graphInst, pend.properties))
		}
		if len(pendingList) > 0 {
			trace.Debug(
				ctx, g.config.logger, "pending edges resolved",
				slog.String("target_type", typeName),
				slog.String("target_pk", pkString),
				slog.Int("count", len(pendingList)),
			)
		}
		delete(g.pending, pk)
	}

	return opCollector.Result()
}

// AddComposed adds a composed child to an existing parent in the graph.
//
// This is an escape hatch for streaming scenarios where composed children
// arrive after the parent was added. For most use cases, compositions are
// automatically extracted during [Graph.Add].
//
// # Parameters
//
//   - parentType: the parent's type identity, as [Snapshot.Types] and
//     [Instance.TypeID] carry it. A rendered name cannot denote a type exactly
//     — see the package doc's Type Identity and Type Names section.
//   - parentKey: the parent's primary key in canonical string form, as returned by
//     [FormatKey]. For example, FormatKey("alice") returns `["alice"]`.
//   - relationName: the composition relation name as declared in the schema
//   - child: the validated child instance to attach
//
// # Limitation: Top-Level Parents Only
//
// AddComposed can only attach children to parents that exist in the top-level
// instances map (those added via [Graph.Add]). It cannot attach grandchildren
// to a composed child. To build nested compositions, either:
//   - Include nested children inline in the parent's [instance.ValidInstance], or
//   - Stream children only to top-level parents
//
// Return semantics: check [diag.Result.OK] for success. A non-OK result
// contains diagnostic issues and the child is not attached. The package doc's
// Error Handling section enumerates the codes AddComposed can emit.
func (g *Graph) AddComposed(
	ctx context.Context,
	parentType schema.TypeID,
	parentKey, relationName string,
	child *instance.ValidInstance,
) diag.Result {
	// Programmer errors: a caller cannot recover from any of these.
	if g == nil {
		panic("graph.AddComposed: nil *Graph receiver")
	}

	if child == nil {
		panic("graph.AddComposed: nil ValidInstance")
	}

	if ctx == nil {
		panic("graph.AddComposed: nil context")
	}

	opCollector := diag.NewCollector(0)

	// Opened before the context check so a cancelled AddComposed is still traced.
	op := trace.Begin(
		ctx, g.config.logger, "yammm.graph.add_composed",
		slog.String("parent_type", parentType.String()),
		slog.String("parent_pk", parentKey),
		slog.String("relation", relationName),
		slog.String("child_type", child.TypeName()),
	)
	defer func() { op.End(opCollector.Result().Err()) }()

	if err := ctx.Err(); err != nil {
		return g.reject(opCollector, diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.AddComposed cancelled: "+err.Error()).Build())
	}

	// Schema mismatch check — programmer error
	if !g.isKnownSchema(child.TypeID().SchemaPath()) {
		panic("graph.AddComposed: instance schema does not match graph schema")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// The identity resolves before the instance does, so a wrong type and a
	// wrong key stay distinguishable. TypeByID reports false for the zero
	// TypeID, which is how a caller that forgot to set one is told.
	typ, ok := g.schema.TypeByID(parentType)
	if !ok {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND,
			fmt.Sprintf("parent type %s not found in schema", parentType)).
			WithDetail(diag.DetailKeyTypeName, g.instanceTagForm(parentType)).
			WithDetail(diag.DetailKeyTypeSchema, parentType.SchemaPath().String()).Build())
	}

	parentName := g.instanceTagForm(parentType)

	parentInst := g.findInstance(parentType, parentKey)
	if parentInst == nil {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_PARENT_NOT_FOUND,
			fmt.Sprintf("parent instance %s[%s] not found", parentName, parentKey)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyTypeSchema, parentType.SchemaPath().String()).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).Build())
	}

	rel, ok := typ.Relation(relationName)
	if !ok || rel.Kind() != schema.RelationComposition {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("relation %q on type %q is not a composition", relationName, parentName)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).
			WithDetail(diag.DetailKeyRelationName, relationName).Build())
	}

	if child.TypeID() != rel.TargetID() {
		got, want, ident := g.describeTypePair(child.TypeID(), rel.TargetID())
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("child type %s does not match relation target %s",
				quoteTypeName(got, ident), quoteTypeName(want, ident))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).
			WithDetail(diag.DetailKeyRelationName, relationName).
			WithDetail(diag.DetailKeyExpected, want).
			WithDetail(diag.DetailKeyGot, got).Build())
	}

	// Identity, not name — see [instanceBuilder.child].
	childTyp, ok := g.schema.TypeByID(child.TypeID())
	if !ok {
		return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND,
			fmt.Sprintf("child type %q not found in schema", child.TypeName())).
			WithDetail(diag.DetailKeyTypeName, child.TypeName()).Build())
	}

	// The child's own key and subtree run the same rules Add applies to a root
	// and the builder applies to an inline child, and the tree it returns is
	// what attaches on success.
	if childTyp.HasPrimaryKey() {
		if err := checkInstanceKey(childTyp, child); err != nil {
			return g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
				fmt.Sprintf("composed child of type %q: %s", child.TypeName(), err)).
				WithDetail(diag.DetailKeyTypeName, child.TypeName()).
				WithDetail(diag.DetailKeyRelationName, relationName).
				WithDetail(diag.DetailKeyPrimaryKey, child.PrimaryKey().String()).Build())
		}
	}
	b := newInstanceBuilder(g, opCollector)
	builtChild := b.build(childTyp, child, nil)
	g.collector.Merge(opCollector.Result())
	if opCollector.HasErrors() {
		return opCollector.Result()
	}

	isMany := rel.IsMany()

	if !isMany {
		if parentInst.HasComposed(relationName) {
			childInst := newInstance(g.instanceTagForm(child.TypeID()), child.TypeID(),
				child.PrimaryKey(), child.Properties(), child.Provenance(), child.Validated())

			var conflictInst *Instance
			if existing := parentInst.composed[relationName]; len(existing) > 0 {
				conflictInst = existing[0]
			}

			builder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("composition %q already has a child", relationName)).
				WithDetail(diag.DetailKeyTypeName, parentName).
				WithDetail(diag.DetailKeyRelationName, relationName).
				WithDetail(diag.DetailKeyJSONField, rel.FieldName())
			// The key must name the occupant that EXISTS, not the rejected
			// child, which never attaches. Whether there is a key at all is a
			// question for the SCHEMA: a part type declaring none has no key to
			// report however an instance was constructed, and the detail is
			// then absent rather than carrying a stand-in.
			if conflictInst != nil && childTyp.HasPrimaryKey() {
				builder = builder.WithDetail(diag.DetailKeyPrimaryKey, conflictInst.PrimaryKey().String())
			}
			issue := builder.Build()

			g.duplicates = append(g.duplicates, newDuplicate(childInst, conflictInst, parentInst, relationName, issue))
			trace.Warn(
				ctx, g.config.logger, "duplicate composed child",
				slog.String("parent_type", parentName),
				slog.String("parent_pk", parentKey),
				slog.String("relation", relationName),
			)
			return g.reject(opCollector, issue)
		}
	} else if childTyp.HasPrimaryKey() {
		childPKString := child.PrimaryKey().String()
		for _, existing := range parentInst.composed[relationName] {
			if existing.PrimaryKey().String() != childPKString {
				continue
			}
			childInst := newInstance(g.instanceTagForm(child.TypeID()), child.TypeID(),
				child.PrimaryKey(), child.Properties(), child.Provenance(), child.Validated())

			issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				"duplicate composed child primary key "+childPKString).
				WithDetail(diag.DetailKeyTypeName, parentName).
				WithDetail(diag.DetailKeyRelationName, relationName).
				WithDetail(diag.DetailKeyJSONField, rel.FieldName()).
				WithDetail(diag.DetailKeyPrimaryKey, childPKString).
				Build()

			g.duplicates = append(g.duplicates, newDuplicate(childInst, existing, parentInst, relationName, issue))
			trace.Warn(
				ctx, g.config.logger, "duplicate composed child",
				slog.String("parent_type", parentName),
				slog.String("parent_pk", parentKey),
				slog.String("relation", relationName),
				slog.String("child_pk", childPKString),
			)
			return g.reject(opCollector, issue)
		}
	}
	// (many) without PK: always append (positional identity)

	g.attestValues = g.attestValues && b.attested
	parentInst.addComposed(relationName, builtChild)

	return opCollector.Result()
}

// Check validates graph completeness.
//
// Check verifies that all required associations have resolved targets.
// Optional associations may remain unresolved without error.
//
// Return semantics: check [diag.Result.OK] for success. A non-OK result
// contains diagnostic issues. A Fatal issue indicates context cancellation.
//
// Error codes that may appear in result:
//   - E_UNRESOLVED_REQUIRED: Required association target not in graph
//   - E_CONTEXT_CANCELLED: Context was cancelled
func (g *Graph) Check(ctx context.Context) diag.Result {
	if g == nil {
		panic("graph.Check: nil *Graph receiver")
	}

	if ctx == nil {
		panic("graph.Check: nil context")
	}

	// Check does NOT merge into g.collector, which is what makes it
	// idempotent: multiple calls return identical results.
	opCollector := diag.NewCollector(0)

	// Opened before the context check so a cancelled Check is still traced.
	op := trace.Begin(ctx, g.config.logger, "yammm.graph.check")
	defer func() { op.End(opCollector.Result().Err()) }()

	if err := ctx.Err(); err != nil {
		opCollector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.Check cancelled: "+err.Error()).Build())
		return opCollector.Result()
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	unresolvedCount := 0
	for _, pendingList := range g.pending {
		for _, pend := range pendingList {
			if !pend.isRequired {
				continue
			}
			unresolvedCount++

			var reason string
			var reasonToken string
			switch pend.reasonDetail {
			case "absent":
				reason = "association field is absent"
				reasonToken = "absent"
			case "empty":
				reason = "association array is empty"
				reasonToken = "empty"
			default:
				reason = "target instance not found"
				reasonToken = "target_missing"
			}

			builder := diag.NewIssue(diag.Error, diag.E_UNRESOLVED_REQUIRED,
				fmt.Sprintf("required association %q is unresolved: %s", pend.relation, reason)).
				WithDetail(diag.DetailKeyTypeName, pend.source.TypeName()).
				WithDetail(diag.DetailKeyPrimaryKey, pend.source.PrimaryKey().String()).
				WithDetail(diag.DetailKeyRelationName, pend.relation).
				WithDetail(diag.DetailKeyJSONField, pend.jsonField).
				WithDetail(diag.DetailKeyReason, reasonToken)

			// The "absent" and "empty" reasons carry no target to name.
			if reasonToken == "target_missing" {
				builder = builder.WithDetail(diag.DetailKeyTargetType, schema.TagForm(g.schema, pend.targetType))
				if pend.targetKey != "" {
					builder = builder.WithDetail(diag.DetailKeyTargetPK, pend.targetKey)
				}
			}

			if prov := pend.source.Provenance(); prov != nil {
				builder = builder.WithSpan(prov.Span())
			}

			opCollector.Collect(builder.Build())

			trace.Warn(
				ctx, g.config.logger, "unresolved required association",
				slog.String("source_type", pend.source.TypeName()),
				slog.String("source_pk", pend.source.PrimaryKey().String()),
				slog.String("relation", pend.relation),
				slog.String("target_type", schema.TagForm(g.schema, pend.targetType)),
				slog.String("reason", reasonToken),
			)
		}
	}

	if unresolvedCount > 0 {
		trace.Debug(
			ctx, g.config.logger, "check completed with unresolved",
			slog.Int("unresolved_count", unresolvedCount),
		)
	}

	return opCollector.Result()
}

// Snapshot creates a point-in-time snapshot of the graph.
//
// The returned [Snapshot] is immutable and independent of subsequent
// graph modifications. All slice accessors on Snapshot return sorted data.
//
// Snapshot acquires a read lock; concurrent Add/AddComposed calls will
// block until Snapshot completes.
func (g *Graph) Snapshot() *Snapshot {
	if g == nil {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Clone map tracks original->cloned instance mappings for the entire snapshot.
	// This ensures all references within the Snapshot point to cloned instances,
	// making the snapshot truly independent of future graph mutations.
	cloneMap := make(map[*Instance]*Instance)

	// Collect and sort type identities
	types := make([]schema.TypeID, 0, len(g.instances))
	for typeID := range g.instances {
		types = append(types, typeID)
	}

	// Build instances map with deep-cloned instances per type
	instances := make(map[schema.TypeID][]*Instance, len(g.instances))
	instanceIndex := make(map[schema.TypeID]map[string]*Instance, len(g.instances))

	for typeID, typeInstances := range g.instances {
		// Clone and collect instances
		insts := make([]*Instance, 0, len(typeInstances))
		for _, inst := range typeInstances {
			cloned := cloneInstance(inst, cloneMap)
			insts = append(insts, cloned)
		}

		instances[typeID] = insts

		// Build index from cloned instances
		idx := make(map[string]*Instance, len(insts))
		for _, inst := range insts {
			idx[inst.PrimaryKey().String()] = inst
		}
		instanceIndex[typeID] = idx
	}

	// Rebuild edges with cloned source/target references.
	// Defensive: clone on-demand if an instance is not already in cloneMap
	// (handles potential future cases where edges reference non-root instances).
	edges := make([]*Edge, len(g.edges))
	for i, e := range g.edges {
		clonedSource := cloneMap[e.source]
		if clonedSource == nil {
			clonedSource = cloneInstance(e.source, cloneMap)
		}
		clonedTarget := cloneMap[e.target]
		if clonedTarget == nil {
			clonedTarget = cloneInstance(e.target, cloneMap)
		}
		edges[i] = newEdge(e.relation, clonedSource, clonedTarget, e.properties)
	}

	// Rebuild duplicates with cloned instance references.
	// Defensive: clone on-demand if instances are not already in cloneMap.
	duplicates := make([]*Duplicate, len(g.duplicates))
	for i, d := range g.duplicates {
		// The rejected instance may not be in the graph's instances map,
		// so clone it separately if not already in cloneMap
		clonedInstance := cloneMap[d.Instance]
		if clonedInstance == nil {
			clonedInstance = cloneInstance(d.Instance, cloneMap)
		}
		// The conflict instance should be in instances map, but apply same
		// defensive pattern for consistency and future resilience
		clonedConflict := cloneMap[d.Conflict]
		if clonedConflict == nil {
			clonedConflict = cloneInstance(d.Conflict, cloneMap)
		}
		var clonedParent *Instance
		if d.Parent != nil {
			clonedParent = cloneMap[d.Parent]
			if clonedParent == nil {
				clonedParent = cloneInstance(d.Parent, cloneMap)
			}
		}
		duplicates[i] = newDuplicate(clonedInstance, clonedConflict, clonedParent, d.Relation, d.Diagnostic)
	}

	// Rebuild unresolved edges with cloned source references.
	// Defensive: clone on-demand if source is not already in cloneMap.
	totalPending := 0
	for _, pendingList := range g.pending {
		totalPending += len(pendingList)
	}
	unresolved := make([]*UnresolvedEdge, 0, totalPending)
	for _, pendingList := range g.pending {
		for _, pend := range pendingList {
			clonedSource := cloneMap[pend.source]
			if clonedSource == nil {
				clonedSource = cloneInstance(pend.source, cloneMap)
			}
			// Determine reason token
			reason := pend.reasonDetail
			if reason == "" {
				reason = "target_missing"
			}
			unresolved = append(unresolved, newUnresolvedEdge(
				clonedSource, pend.relation, pend.targetType, pend.targetKey,
				pend.isRequired, reason, pend.properties,
			))
		}
	}

	// The Values attestation is graph-level state, never an instance walk:
	// a seeded graph's imported clones all report Validated() == false
	// while the accumulator carries the loaded header's claim.
	hasInstance := false
	for _, typeInstances := range g.instances {
		if len(typeInstances) > 0 {
			hasInstance = true
			break
		}
	}
	requiredUnresolved := false
	for _, pendingList := range g.pending {
		for _, pend := range pendingList {
			if pend.isRequired {
				requiredUnresolved = true
				break
			}
		}
		if requiredUnresolved {
			break
		}
	}
	att := Attestation{
		Values:       hasInstance && g.attestValues,
		Associations: !requiredUnresolved,
	}

	return newSnapshot(g.schema, types, instances, instanceIndex, edges, duplicates, unresolved, g.collector.Result(), att)
}

// isKnownSchema reports whether schemaPath is anywhere in the bound schema's
// import closure. An instance from outside it is a programmer error.
func (g *Graph) isKnownSchema(schemaPath location.SourceID) bool {
	return schemaPath == g.schema.SourceID() || g.closureSchemas[schemaPath]
}

// ownsType reports whether this graph accepts a root instance of id's type: the
// bound schema declares it, or directly imports the schema that does. This is a
// policy, not a resolution rule — [schema.Schema.TypeByID] resolves an identity
// anywhere in the closure, and a composed child uses that reach.
func (g *Graph) ownsType(id schema.TypeID) bool {
	path := id.SchemaPath()
	return path == g.schema.SourceID() || g.ownedSchemas[path]
}

// describeTypePair renders two type identities so a reader can tell them apart.
// Tag forms collide whenever a type has no alias to qualify with, and a message
// reading `X does not match X` is worse than none.
func (g *Graph) describeTypePair(got, want schema.TypeID) (gotName, wantName string, collide bool) {
	gotName, wantName = g.instanceTagForm(got), g.instanceTagForm(want)
	if gotName == wantName {
		return got.String(), want.String(), true
	}
	return gotName, wantName, false
}

// quoteTypeName renders a type name for a message. A tag form is quoted the way
// every other name in these diagnostics is; a full identity already reads as
// one token and quoting it only adds noise.
func quoteTypeName(name string, isIdentity bool) string {
	if isIdentity {
		return name
	}
	return strconv.Quote(name)
}

// instanceTagForm computes the canonical instance tag form for a TypeID.
func (g *Graph) instanceTagForm(id schema.TypeID) string {
	return schema.TagForm(g.schema, id)
}

// findInstance looks up an instance by TypeID and key.
func (g *Graph) findInstance(typeID schema.TypeID, key string) *Instance {
	if typeInstances := g.instances[typeID]; typeInstances != nil {
		return typeInstances[key]
	}
	return nil
}

// iterEdges iterates over edges from a ValidInstance.
func (g *Graph) iterEdges(inst *instance.ValidInstance) map[string]*instance.ValidEdgeData {
	return maps.Collect(inst.Edges())
}

// checkInstanceKey rejects an empty key, a key whose arity disagrees with the
// type's declared primary keys, and a component that disagrees with the
// instance's own present key property. An absent property is not a mismatch.
func checkInstanceKey(typ *schema.Type, inst *instance.ValidInstance) error {
	key := inst.PrimaryKey()
	if key.Len() == 0 {
		return errors.New("primary key is empty")
	}
	declared := 0
	for range typ.PrimaryKeys() {
		declared++
	}
	if key.Len() != declared {
		return fmt.Errorf("primary key has %d components; type declares %d", key.Len(), declared)
	}
	i := 0
	for pk := range typ.PrimaryKeys() {
		val, ok := inst.Property(pk.Name())
		if ok && !keyComponentAgrees(key.Get(i), val) {
			return fmt.Errorf("primary key component %d disagrees with property %q (%s vs %s)",
				i, pk.Name(), renderKeyComponent(key.Get(i)), renderKeyComponent(val))
		}
		i++
	}
	return nil
}

// keyComponentAgrees reports whether a key component and its property hold the
// same value. Identical scalar kinds settle it without allocating, which is
// what keeps the check affordable once per composed child rather than once per
// root; anything else falls back to the canonical rendering, the form that
// decides key equality on the wire.
func keyComponentAgrees(component, property immutable.Value) bool {
	x, y := component.Unwrap(), property.Unwrap()
	switch xv := x.(type) {
	case string:
		if yv, ok := y.(string); ok {
			return xv == yv
		}
	case int64:
		if yv, ok := y.(int64); ok {
			return xv == yv
		}
	case bool:
		if yv, ok := y.(bool); ok {
			return xv == yv
		}
	}
	// float64 and nil are deliberately absent. Go equality disagrees with the
	// canonical rendering on both: -0.0 == 0.0 while WrapKey renders [-0] and
	// [0], and a typed nil is not == to an untyped one while both render
	// [null]. The rendering decides key identity, so it decides here.
	return renderKeyComponent(component) == renderKeyComponent(property)
}

// renderKeyComponent renders one value in the canonical key form.
func renderKeyComponent(v immutable.Value) string {
	return immutable.WrapKey([]any{v.Unwrap()}).String()
}

// reject records issue in the per-call collector and in the graph's cumulative
// one, and returns the per-call result. [diag.Collector] synchronizes itself,
// so the graph's own lock is not needed for the second write.
func (g *Graph) reject(opCollector *diag.Collector, issue diag.Issue) diag.Result {
	opCollector.Collect(issue)
	g.collector.Collect(issue)
	return opCollector.Result()
}

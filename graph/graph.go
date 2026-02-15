package graph

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
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
	targetType   string // instance tag form
	targetKey    string
	properties   immutable.Properties
	isRequired   bool
	reasonDetail string // "absent", "empty", or ""
}

// New creates a new Graph bound to the given schema.
//
// Panics if schema is nil (programmer error). A nil schema is never valid
// as there is no way to validate instances without a schema.
func New(s *schema.Schema, opts ...GraphOption) *Graph {
	if s == nil {
		panic("graph.New: nil schema")
	}

	cfg := graphConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Graph{
		schema:    s,
		config:    cfg,
		instances: make(map[schema.TypeID]map[string]*Instance),
		pending:   make(map[pendingKey][]*pendingEdge),
		collector: diag.NewCollector(0), // unlimited
	}
}

// Add adds a validated instance to the graph.
//
// Add indexes the instance by its TypeID and primary key, creates edges
// for associations, extracts composed children, and resolves any pending
// forward references that target this instance.
//
// Return semantics:
//   - (result, nil): Operation completed. Check result.OK() for success.
//   - (empty, error): Internal failure (nil receiver, nil instance, schema mismatch)
//     or context cancellation.
//
// Error codes that may appear in result:
//   - E_GRAPH_TYPE_NOT_FOUND: Instance type not in schema
//   - E_GRAPH_MISSING_PK: Type has no primary key
//   - E_DUPLICATE_PK: Primary key already exists for this type
func (g *Graph) Add(ctx context.Context, inst *instance.ValidInstance) (diag.Result, error) {
	// Nil receiver check
	if g == nil {
		return diag.OK(), ErrNilGraph
	}

	// Nil instance check
	if inst == nil {
		return diag.OK(), ErrNilInstance
	}

	// Nil context check
	if ctx == nil {
		panic("graph.Add: nil context")
	}

	// Per-operation collector for this Add call only
	opCollector := diag.NewCollector(0)

	// Operation boundary logging - must come before context check so
	// cancellations are traced (consistency with walk package pattern)
	op := trace.Begin(ctx, g.config.logger, "yammm.graph.add",
		slog.String("type", inst.TypeName()),
		slog.String("pk", inst.PrimaryKey().String()),
	)
	var retErr error
	defer func() { op.End(retErr) }()

	// Context cancellation check
	if err := ctx.Err(); err != nil {
		retErr = err
		return diag.OK(), retErr
	}

	// Resolve type
	typeID := inst.TypeID()

	// Schema mismatch check: verify instance was validated against this graph's
	// schema or one of its imports (programmer error detection)
	if !g.isKnownSchema(typeID.SchemaPath()) {
		retErr = ErrSchemaMismatch
		return diag.OK(), retErr
	}

	typ, ok := g.lookupType(typeID)
	if !ok {
		msg := fmt.Sprintf("type %q not found in schema", inst.TypeName())
		builder := diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND, msg).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName())
		// Add pk detail when available
		if pk := inst.PrimaryKey(); pk.Len() > 0 {
			builder = builder.WithDetail(diag.DetailKeyPrimaryKey, pk.String())
		}
		// Detect alias-qualified name (suggests imported type from potentially transitive import)
		if strings.Contains(inst.TypeName(), ".") {
			builder = builder.WithHint("if this type is from a transitively imported schema, add a direct import to access it")
			// Add type_schema detail (the schema path from the type ID)
			builder = builder.WithDetail(diag.DetailKeyTypeSchema, typeID.SchemaPath().String())
		}
		issue := builder.Build()
		opCollector.Collect(issue)
		g.mu.Lock()
		g.collector.Collect(issue)
		g.mu.Unlock()
		return opCollector.Result(), nil
	}

	// Check type has primary key
	if !typ.HasPrimaryKey() {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_MISSING_PK,
			fmt.Sprintf("type %q has no primary key; cannot add to graph", inst.TypeName())).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build()
		opCollector.Collect(issue)
		g.mu.Lock()
		g.collector.Collect(issue)
		g.mu.Unlock()
		return opCollector.Result(), nil
	}

	// Check part types cannot be added directly
	if typ.IsPart() {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("part type %q cannot be added directly; use AddComposed", inst.TypeName())).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build()
		opCollector.Collect(issue)
		g.mu.Lock()
		g.collector.Collect(issue)
		g.mu.Unlock()
		return opCollector.Result(), nil
	}

	// Compute instance tag form and primary key
	typeName := g.instanceTagForm(typeID)
	pkString := inst.PrimaryKey().String()

	g.mu.Lock()
	defer g.mu.Unlock()

	// Check for duplicate PK
	typeInstances := g.instances[typeID]
	if typeInstances != nil {
		if existing, found := typeInstances[pkString]; found {
			// Duplicate detected
			graphInst := newInstance(typeName, typeID, inst.PrimaryKey(), inst.Properties(), inst.Provenance())
			diagBuilder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %q", pkString, typeName)).
				WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyPrimaryKey, pkString)
			// Attach span from provenance if available
			if prov := inst.Provenance(); prov != nil {
				diagBuilder = diagBuilder.WithSpan(prov.Span())
			}
			dup := newDuplicate(graphInst, existing, diagBuilder.Build())
			g.duplicates = append(g.duplicates, dup)
			opCollector.Collect(dup.Diagnostic)
			g.collector.Collect(dup.Diagnostic)
			trace.Warn(ctx, g.config.logger, "duplicate primary key",
				slog.String("type", typeName),
				slog.String("pk", pkString),
			)
			return opCollector.Result(), nil
		}
	} else {
		g.instances[typeID] = make(map[string]*Instance)
	}

	// Create Instance
	graphInst := newInstance(typeName, typeID, inst.PrimaryKey(), inst.Properties(), inst.Provenance())

	// Add to instances map
	g.instances[typeID][pkString] = graphInst

	// Process associations - create edges
	// Capture edge data for both iteration and absent-field detection
	edgeDataMap := g.iterEdges(inst)
	for relationName, edgeData := range edgeDataMap {
		rel, ok := typ.Relation(relationName)
		if !ok || rel.Kind() != schema.RelationAssociation {
			continue
		}

		targetTypeID := rel.TargetID()
		targetTypeName := g.instanceTagForm(targetTypeID)
		isRequired := !rel.IsOptional()

		for target := range edgeData.TargetsIter() {
			targetKey := target.TargetKey().String()

			// Try to resolve target
			if targetInst := g.findInstance(targetTypeID, targetKey); targetInst != nil {
				// Create resolved edge
				edge := newEdge(relationName, graphInst, targetInst, target.Properties())
				g.edges = append(g.edges, edge)
				trace.Debug(ctx, g.config.logger, "edge resolved",
					slog.String("relation", relationName),
					slog.String("source_type", typeName),
					slog.String("source_pk", pkString),
					slog.String("target_type", targetTypeName),
					slog.String("target_pk", targetKey),
				)
			} else {
				// Create pending edge (forward reference)
				pk := pendingKey{targetTypeID: targetTypeID, targetKey: targetKey}
				g.pending[pk] = append(g.pending[pk], &pendingEdge{
					source:       graphInst,
					relation:     relationName,
					jsonField:    rel.FieldName(),
					targetType:   targetTypeName,
					targetKey:    targetKey,
					properties:   target.Properties(),
					isRequired:   isRequired,
					reasonDetail: "", // will be "target_missing" in Check
				})
				trace.Debug(ctx, g.config.logger, "forward reference created",
					slog.String("relation", relationName),
					slog.String("source_type", typeName),
					slog.String("source_pk", pkString),
					slog.String("target_type", targetTypeName),
					slog.String("target_pk", targetKey),
				)
			}
		}

		// Check for empty required edge
		if isRequired && edgeData.IsEmpty() {
			pk := pendingKey{targetTypeID: targetTypeID, targetKey: ""}
			g.pending[pk] = append(g.pending[pk], &pendingEdge{
				source:       graphInst,
				relation:     relationName,
				jsonField:    rel.FieldName(),
				targetType:   targetTypeName,
				targetKey:    "",
				isRequired:   true,
				reasonDetail: "empty",
			})
		}
	}

	// Track absent required associations
	for rel := range typ.AllAssociations() {
		if rel.IsOptional() {
			continue
		}
		relationName := rel.Name()
		// Check if relation was processed (has edge data)
		if _, processed := edgeDataMap[relationName]; processed {
			continue // Handled above (has targets or is empty)
		}
		// Required association field is absent
		targetTypeID := rel.TargetID()
		targetTypeName := g.instanceTagForm(targetTypeID)
		pk := pendingKey{targetTypeID: targetTypeID, targetKey: ""}
		g.pending[pk] = append(g.pending[pk], &pendingEdge{
			source:       graphInst,
			relation:     relationName,
			jsonField:    rel.FieldName(),
			targetType:   targetTypeName,
			targetKey:    "",
			isRequired:   true,
			reasonDetail: "absent",
		})
	}

	// Resolve ALL pending edges that target this instance
	pk := pendingKey{targetTypeID: typeID, targetKey: pkString}
	if pendingList, ok := g.pending[pk]; ok {
		for _, pend := range pendingList {
			edge := newEdge(pend.relation, pend.source, graphInst, pend.properties)
			g.edges = append(g.edges, edge)
		}
		if len(pendingList) > 0 {
			trace.Debug(ctx, g.config.logger, "pending edges resolved",
				slog.String("target_type", typeName),
				slog.String("target_pk", pkString),
				slog.Int("count", len(pendingList)),
			)
		}
		delete(g.pending, pk)
	}

	// Extract and attach composed children
	g.extractCompositions(inst, graphInst, opCollector)

	return opCollector.Result(), nil
}

// AddComposed adds a composed child to an existing parent in the graph.
//
// This is an escape hatch for streaming scenarios where composed children
// arrive after the parent was added. For most use cases, compositions are
// automatically extracted during [Graph.Add].
//
// # Parameters
//
//   - parentType: the type name in instance tag form (e.g., "Person" or "c.Entity")
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
// Return semantics:
//   - (result, nil): Operation completed. Check result.OK() for success.
//   - (empty, error): Internal failure or context cancellation.
//
// Error codes that may appear in result:
//   - E_GRAPH_TYPE_NOT_FOUND: Parent type not found
//   - E_GRAPH_PARENT_NOT_FOUND: Parent instance not found (may occur if parentKey
//     format doesn't match [FormatKey] output)
//   - E_GRAPH_INVALID_COMPOSITION: Relation not found or not a composition
//   - E_DUPLICATE_COMPOSED_PK: Child with same PK already exists (for PK'd children)
func (g *Graph) AddComposed(
	ctx context.Context,
	parentType, parentKey, relationName string,
	child *instance.ValidInstance,
) (diag.Result, error) {
	// Nil receiver check
	if g == nil {
		return diag.OK(), ErrNilGraph
	}

	// Nil child check
	if child == nil {
		return diag.OK(), ErrNilChild
	}

	// Nil context check
	if ctx == nil {
		panic("graph.AddComposed: nil context")
	}

	// Per-operation collector for this AddComposed call only
	opCollector := diag.NewCollector(0)

	// Operation boundary logging - must come before context check so
	// cancellations are traced (consistency with walk package pattern)
	op := trace.Begin(ctx, g.config.logger, "yammm.graph.add_composed",
		slog.String("parent_type", parentType),
		slog.String("parent_pk", parentKey),
		slog.String("relation", relationName),
		slog.String("child_type", child.TypeName()),
	)
	var retErr error
	defer func() { op.End(retErr) }()

	// Context cancellation check
	if err := ctx.Err(); err != nil {
		retErr = err
		return diag.OK(), retErr
	}

	// Schema mismatch check: verify child was validated against this graph's
	// schema or one of its imports (programmer error detection)
	if !g.isKnownSchema(child.TypeID().SchemaPath()) {
		retErr = ErrSchemaMismatch
		return diag.OK(), retErr
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Resolve parent type
	parentTypeID, ok := g.resolveTypeName(parentType)
	if !ok {
		msg := fmt.Sprintf("parent type %q not found", parentType)
		builder := diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND, msg).
			WithDetail(diag.DetailKeyTypeName, parentType)
		// Detect alias-qualified name (suggests imported type from potentially transitive import)
		if strings.Contains(parentType, ".") {
			builder = builder.WithHint("if this type is from a transitively imported schema, add a direct import to access it")
		}
		issue := builder.Build()
		opCollector.Collect(issue)
		g.collector.Collect(issue)
		return opCollector.Result(), nil
	}

	// Find parent instance
	parentInst := g.findInstance(parentTypeID, parentKey)
	if parentInst == nil {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_PARENT_NOT_FOUND,
			fmt.Sprintf("parent instance %s[%s] not found", parentType, parentKey)).
			WithDetail(diag.DetailKeyTypeName, parentType).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).Build()
		opCollector.Collect(issue)
		g.collector.Collect(issue)
		return opCollector.Result(), nil
	}

	// Lookup parent type and relation
	typ, ok := g.lookupType(parentTypeID)
	if !ok {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND,
			fmt.Sprintf("parent type %q not found", parentType)).
			WithDetail(diag.DetailKeyTypeName, parentType).Build()
		opCollector.Collect(issue)
		g.collector.Collect(issue)
		return opCollector.Result(), nil
	}

	rel, ok := typ.Relation(relationName)
	if !ok || rel.Kind() != schema.RelationComposition {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("relation %q on type %q is not a composition", relationName, parentType)).
			WithDetail(diag.DetailKeyTypeName, parentType).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).
			WithDetail(diag.DetailKeyRelationName, relationName).Build()
		opCollector.Collect(issue)
		g.collector.Collect(issue)
		return opCollector.Result(), nil
	}

	// Validate child type matches relation target
	if child.TypeID() != rel.TargetID() {
		issue := diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("child type %q does not match relation target %q", child.TypeName(), g.instanceTagForm(rel.TargetID()))).
			WithDetail(diag.DetailKeyTypeName, parentType).
			WithDetail(diag.DetailKeyPrimaryKey, parentKey).
			WithDetail(diag.DetailKeyRelationName, relationName).
			WithDetail(diag.DetailKeyExpected, g.instanceTagForm(rel.TargetID())).
			WithDetail(diag.DetailKeyGot, child.TypeName()).Build()
		opCollector.Collect(issue)
		g.collector.Collect(issue)
		return opCollector.Result(), nil
	}

	// Check for duplicates per
	isMany := rel.IsMany()
	childTyp, _ := g.lookupType(child.TypeID())
	hasPK := childTyp != nil && childTyp.HasPrimaryKey()

	if !isMany {
		// (one) cardinality: any child exists is a duplicate
		if parentInst.HasComposed(relationName) {
			// Create child Instance for duplicate record
			childTypeName := g.instanceTagForm(child.TypeID())
			childInst := newInstance(childTypeName, child.TypeID(), child.PrimaryKey(), child.Properties(), child.Provenance())

			// Find existing child as conflict
			var conflictInst *Instance
			if existing := parentInst.composed[relationName]; len(existing) > 0 {
				conflictInst = existing[0]
			}

			builder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("composition %q already has a child", relationName)).
				WithDetail(diag.DetailKeyTypeName, parentType).
				WithDetail(diag.DetailKeyRelationName, relationName).
				WithDetail(diag.DetailKeyJsonField, rel.FieldName())
			if composedPK, err := FormatComposedKey(keyToValues(parentInst.PrimaryKey()), relationName, nil); err == nil {
				builder = builder.WithDetail(diag.DetailKeyPrimaryKey, composedPK)
			}
			issue := builder.Build()

			// Record duplicate
			dup := newDuplicate(childInst, conflictInst, issue)
			g.duplicates = append(g.duplicates, dup)

			opCollector.Collect(issue)
			g.collector.Collect(issue)
			trace.Warn(ctx, g.config.logger, "duplicate composed child",
				slog.String("parent_type", parentType),
				slog.String("parent_pk", parentKey),
				slog.String("relation", relationName),
			)
			return opCollector.Result(), nil
		}
	} else if hasPK {
		// (many) with PK: check for duplicate PK among siblings
		childPKString := child.PrimaryKey().String()
		for _, existing := range parentInst.composed[relationName] {
			if existing.PrimaryKey().String() == childPKString {
				// Create child Instance for duplicate record
				childTypeName := g.instanceTagForm(child.TypeID())
				childInst := newInstance(childTypeName, child.TypeID(), child.PrimaryKey(), child.Properties(), child.Provenance())

				childKeyValues := keyToValues(child.PrimaryKey())
				builder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
					"duplicate composed child primary key "+childPKString).
					WithDetail(diag.DetailKeyTypeName, parentType).
					WithDetail(diag.DetailKeyRelationName, relationName).
					WithDetail(diag.DetailKeyJsonField, rel.FieldName())
				if composedPK, err := FormatComposedKey(keyToValues(parentInst.PrimaryKey()), relationName, childKeyValues); err == nil {
					builder = builder.WithDetail(diag.DetailKeyPrimaryKey, composedPK)
				}
				issue := builder.Build()

				// Record duplicate (existing is the conflict)
				dup := newDuplicate(childInst, existing, issue)
				g.duplicates = append(g.duplicates, dup)

				opCollector.Collect(issue)
				g.collector.Collect(issue)
				trace.Warn(ctx, g.config.logger, "duplicate composed child",
					slog.String("parent_type", parentType),
					slog.String("parent_pk", parentKey),
					slog.String("relation", relationName),
					slog.String("child_pk", childPKString),
				)
				return opCollector.Result(), nil
			}
		}
	}
	// (many) without PK: always append (positional identity)

	// Create child Instance and attach
	childTypeName := g.instanceTagForm(child.TypeID())
	childInst := newInstance(childTypeName, child.TypeID(), child.PrimaryKey(), child.Properties(), child.Provenance())
	g.extractCompositions(child, childInst, opCollector) // Extract nested compositions from streamed child
	parentInst.addComposed(relationName, childInst)

	return opCollector.Result(), nil
}

// Check validates graph completeness.
//
// Check verifies that all required associations have resolved targets.
// Optional associations may remain unresolved without error.
//
// Return semantics:
//   - (result, nil): Check completed. Check result.OK() for success.
//   - (empty, error): Internal failure or context cancellation.
//
// Error codes that may appear in result:
//   - E_UNRESOLVED_REQUIRED: Required association target not in graph
func (g *Graph) Check(ctx context.Context) (diag.Result, error) {
	if g == nil {
		return diag.OK(), ErrNilGraph
	}

	// Nil context check
	if ctx == nil {
		panic("graph.Check: nil context")
	}

	// Per-operation collector for this Check call only.
	// Unlike Add/AddComposed, Check does NOT merge into g.collector,
	// making it idempotent: multiple calls return identical results.
	opCollector := diag.NewCollector(0)

	// Operation boundary logging - must come before context check so
	// cancellations are traced (consistency with walk package pattern)
	op := trace.Begin(ctx, g.config.logger, "yammm.graph.check")
	var retErr error
	defer func() { op.End(retErr) }()

	// Context cancellation check
	if err := ctx.Err(); err != nil {
		retErr = err
		return diag.OK(), retErr
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Check pending edges for required associations
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
				WithDetail(diag.DetailKeyJsonField, pend.jsonField).
				WithDetail(diag.DetailKeyReason, reasonToken)

			// Add target_type and target_pk only for target_missing case
			if reasonToken == "target_missing" {
				builder = builder.WithDetail(diag.DetailKeyTargetType, pend.targetType)
				if pend.targetKey != "" {
					builder = builder.WithDetail(diag.DetailKeyTargetPK, pend.targetKey)
				}
			}

			// Attach span from source instance provenance if available
			if prov := pend.source.Provenance(); prov != nil {
				builder = builder.WithSpan(prov.Span())
			}

			issue := builder.Build()
			opCollector.Collect(issue)

			trace.Warn(ctx, g.config.logger, "unresolved required association",
				slog.String("source_type", pend.source.TypeName()),
				slog.String("source_pk", pend.source.PrimaryKey().String()),
				slog.String("relation", pend.relation),
				slog.String("target_type", pend.targetType),
				slog.String("reason", reasonToken),
			)
		}
	}

	if unresolvedCount > 0 {
		trace.Debug(ctx, g.config.logger, "check completed with unresolved",
			slog.Int("unresolved_count", unresolvedCount),
		)
	}

	return opCollector.Result(), nil
}

// Snapshot creates a point-in-time snapshot of the graph.
//
// The returned [Result] is immutable and independent of subsequent
// graph modifications. All slice accessors on Result return sorted data.
//
// Snapshot acquires a read lock; concurrent Add/AddComposed calls will
// block until Snapshot completes.
func (g *Graph) Snapshot() *Result {
	if g == nil {
		return nil
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Clone map tracks original->cloned instance mappings for the entire snapshot.
	// This ensures all references within the Result point to cloned instances,
	// making the snapshot truly independent of future graph mutations.
	cloneMap := make(map[*Instance]*Instance)

	// Collect and sort type names
	types := make([]string, 0, len(g.instances))
	for typeID := range g.instances {
		types = append(types, g.instanceTagForm(typeID))
	}
	slices.Sort(types)

	// Build instances map with deep-cloned instances per type
	instances := make(map[string][]*Instance, len(g.instances))
	instanceIndex := make(map[string]map[string]*Instance, len(g.instances))

	for typeID, typeInstances := range g.instances {
		typeName := g.instanceTagForm(typeID)

		// Clone and collect instances
		insts := make([]*Instance, 0, len(typeInstances))
		for _, inst := range typeInstances {
			cloned := cloneInstance(inst, cloneMap)
			insts = append(insts, cloned)
		}

		// Sort by PK string
		slices.SortFunc(insts, func(a, b *Instance) int {
			return cmp.Compare(a.PrimaryKey().String(), b.PrimaryKey().String())
		})

		instances[typeName] = insts

		// Build index from cloned instances
		idx := make(map[string]*Instance, len(insts))
		for _, inst := range insts {
			idx[inst.PrimaryKey().String()] = inst
		}
		instanceIndex[typeName] = idx
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
	slices.SortFunc(edges, func(a, b *Edge) int {
		if c := cmp.Compare(a.Source().TypeName(), b.Source().TypeName()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Source().PrimaryKey().String(), b.Source().PrimaryKey().String()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Relation(), b.Relation()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Target().TypeName(), b.Target().TypeName()); c != 0 {
			return c
		}
		return cmp.Compare(a.Target().PrimaryKey().String(), b.Target().PrimaryKey().String())
	})

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
		duplicates[i] = newDuplicate(clonedInstance, clonedConflict, d.Diagnostic)
	}
	slices.SortFunc(duplicates, func(a, b *Duplicate) int {
		if c := cmp.Compare(a.Instance.TypeName(), b.Instance.TypeName()); c != 0 {
			return c
		}
		return cmp.Compare(a.Instance.PrimaryKey().String(), b.Instance.PrimaryKey().String())
	})

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
				pend.isRequired, reason))
		}
	}
	slices.SortFunc(unresolved, func(a, b *UnresolvedEdge) int {
		if c := cmp.Compare(a.Source.TypeName(), b.Source.TypeName()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Source.PrimaryKey().String(), b.Source.PrimaryKey().String()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Relation, b.Relation); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TargetType, b.TargetType); c != 0 {
			return c
		}
		return cmp.Compare(a.TargetKey, b.TargetKey)
	})

	return newResult(g.schema, types, instances, instanceIndex, edges, duplicates, unresolved, g.collector.Result())
}

// isKnownSchema checks if the given schema path is the graph's schema or one of its imports.
// Used for schema mismatch detection in Add/AddComposed.
func (g *Graph) isKnownSchema(schemaPath location.SourceID) bool {
	// Check if it's the graph's own schema
	if schemaPath == g.schema.SourceID() {
		return true
	}

	// Check imported schemas (including transitive imports)
	// We use a visited set to avoid cycles
	visited := make(map[location.SourceID]bool)
	return checkSchemaImports(g.schema, schemaPath, visited)
}

// checkSchemaImports recursively checks if schemaPath is in the import tree of s.
func checkSchemaImports(s *schema.Schema, schemaPath location.SourceID, visited map[location.SourceID]bool) bool {
	for imp := range s.Imports() {
		impSchema := imp.Schema()
		if impSchema == nil {
			continue
		}

		impPath := impSchema.SourceID()
		if impPath == schemaPath {
			return true
		}

		// Avoid cycles
		if visited[impPath] {
			continue
		}
		visited[impPath] = true

		// Recursively check transitive imports
		if checkSchemaImports(impSchema, schemaPath, visited) {
			return true
		}
	}

	return false
}

// lookupType looks up a Type by TypeID.
func (g *Graph) lookupType(id schema.TypeID) (*schema.Type, bool) {
	// Check local types
	if id.SchemaPath() == g.schema.SourceID() {
		return g.schema.Type(id.Name())
	}

	// Check imported schemas
	for imp := range g.schema.Imports() {
		if imp.Schema() != nil && imp.Schema().SourceID() == id.SchemaPath() {
			return imp.Schema().Type(id.Name())
		}
	}

	return nil, false
}

// resolveTypeName resolves a type name string to TypeID.
func (g *Graph) resolveTypeName(typeName string) (schema.TypeID, bool) {
	// Check for alias-qualified name
	if before, after, ok := strings.Cut(typeName, "."); ok {
		alias := before
		name := after
		imp, ok := g.schema.ImportByAlias(alias)
		if !ok || imp.Schema() == nil {
			return schema.TypeID{}, false
		}
		typ, ok := imp.Schema().Type(name)
		if !ok {
			return schema.TypeID{}, false
		}
		return typ.ID(), true
	}

	// Unqualified name: local type only
	typ, ok := g.schema.Type(typeName)
	if !ok {
		return schema.TypeID{}, false
	}
	return typ.ID(), true
}

// instanceTagForm computes the canonical instance tag form for a TypeID.
func (g *Graph) instanceTagForm(id schema.TypeID) string {
	// Local type: unqualified
	if id.SchemaPath() == g.schema.SourceID() {
		return id.Name()
	}

	// Imported type: alias-qualified
	alias := g.schema.FindImportAlias(id.SchemaPath())
	if alias != "" {
		return alias + "." + id.Name()
	}

	// Fallback (shouldn't happen for valid schemas)
	return id.Name()
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

// extractCompositions extracts composed children from a ValidInstance.
//
// Handles both slice and bare *ValidInstance shapes for defensive robustness.
// Enforces (one) cardinality for inline compositions.
//
// The opCollector receives per-operation diagnostics; g.collector receives
// cumulative diagnostics for Snapshot().Diagnostics().
func (g *Graph) extractCompositions(valid *instance.ValidInstance, graphInst *Instance, opCollector *diag.Collector) {
	// Get the type definition to check cardinality
	typ, ok := g.lookupType(valid.TypeID())
	if !ok {
		return // Shouldn't happen with validated instances
	}

	for relationName, composedValue := range valid.Compositions() {
		// Get relation to check cardinality
		rel, hasRel := typ.Relation(relationName)
		isMany := !hasRel || rel.IsMany() // Default to many if unknown

		unwrapped := composedValue.Unwrap()

		// Handle slice shape (normal case from validator)
		if slice, ok := unwrapped.(immutable.Slice); ok {
			// Cardinality check for (one) relations
			if !isMany && slice.Len() > 1 {
				parentKeyValues := keyToValues(graphInst.PrimaryKey())
				builder := diag.NewIssue(
					diag.Error,
					diag.E_DUPLICATE_COMPOSED_PK,
					fmt.Sprintf("composition %q: (one) cardinality violated, got %d children", relationName, slice.Len()),
				).WithDetail(diag.DetailKeyTypeName, graphInst.TypeName()).
					WithDetail(diag.DetailKeyRelationName, relationName)
				// Add json_field detail if relation is known
				if hasRel {
					builder = builder.WithDetail(diag.DetailKeyJsonField, rel.FieldName())
				}
				if composedPK, err := FormatComposedKey(parentKeyValues, relationName, nil); err == nil {
					builder = builder.WithDetail(diag.DetailKeyPrimaryKey, composedPK)
				}
				issue := builder.Build()
				opCollector.Collect(issue)
				g.collector.Collect(issue)
			}

			// Note: For (many) compositions with primary keys, duplicate checking
			// is NOT performed here because inline compositions arrive from
			// ValidInstance objects that have already been validated by
			// instance.Validate(). For streamed children, AddComposed performs
			// the equivalent check.
			count := 0
			for i := range slice.Len() {
				val := slice.Get(i)
				childValid, ok := val.Unwrap().(*instance.ValidInstance)
				if !ok {
					continue
				}
				g.attachComposedChild(childValid, graphInst, relationName, opCollector)
				count++
				// For (one) relations, only attach first child
				if !isMany && count >= 1 {
					break
				}
			}
			continue
		}

		// Handle bare *ValidInstance shape (defensive)
		if childValid, ok := unwrapped.(*instance.ValidInstance); ok {
			g.attachComposedChild(childValid, graphInst, relationName, opCollector)
		}
		// Skip nil/zero values (absent optional compositions)
	}
}

// attachComposedChild creates and attaches a single composed child.
func (g *Graph) attachComposedChild(childValid *instance.ValidInstance, graphInst *Instance, relationName string, opCollector *diag.Collector) {
	childTypeName := g.instanceTagForm(childValid.TypeID())
	childInst := newInstance(childTypeName, childValid.TypeID(),
		childValid.PrimaryKey(), childValid.Properties(), childValid.Provenance())

	// Recursively extract nested compositions
	g.extractCompositions(childValid, childInst, opCollector)

	graphInst.addComposed(relationName, childInst)
}

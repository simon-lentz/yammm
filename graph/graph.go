package graph

import (
	"context"
	"fmt"
	"sync"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Graph builds an in-memory data structure from validated instances.
//
// Graph is safe for concurrent use from multiple goroutines. Multiple
// callers may invoke [Graph.Add] and [Graph.AddOrMerge] concurrently; the graph
// handles forward references, duplicate detection and merges atomically.
//
// Add, AddOrMerge and Check accept a [context.Context] for cancellation.
// Cancellation does not corrupt internal state; partial results may be inspected.
type Graph struct {
	schema schema.Schema
	mu     sync.RWMutex

	// canon rewrites a [Snapshot]'s received addresses into the form a
	// validator stores.
	canon *canonicalizer

	// instances indexes instances by TypeID, then by PK string.
	instances map[schema.TypeID]map[string]Instance

	// edges holds all resolved association edges.
	edges []Edge

	// pending holds the unresolved records, filed by target type and key.
	pending map[pendingKey]map[*pendingEdge]struct{}

	// duplicates holds duplicate PK records.
	duplicates []Duplicate

	// bySource indexes edges and pending records by source and association,
	// for [Graph.AddOrMerge]. It is nil until the first call to reach the merge
	// rules, at a held key with no composed children, builds it.
	bySource map[recordSlot]*heldRecords

	// attestValues is the Values attestation: true for a new graph, whose every
	// record comes from a validator, and a seeded graph's loaded header's claim.
	attestValues bool

	// merged holds each root a merge grew whose type declares an invariant,
	// which [Graph.Check] judges, and judges the validator and name each such
	// root's type is judged by. judge is the validator over the graph's schema,
	// built at the first merge that needs it.
	merged map[Instance]struct{}
	judges map[schema.TypeID]judgeTarget
	judge  *instance.Validator
}

// pendingKey identifies a pending edge by its target.
type pendingKey struct {
	targetTypeID schema.TypeID
	targetKey    string
}

// pendingEdge holds data for an unresolved forward reference.
type pendingEdge struct {
	source       Instance
	relation     string
	jsonField    string // JSON field name: the lower-case relation name
	targetType   schema.TypeID
	targetKey    string
	properties   immutable.Properties
	isRequired   bool
	reasonDetail UnresolvedReason
}

// New creates a new Graph bound to the given schema.
//
// Panics if schema is zero (programmer error). A zero schema is never valid
// as there is no way to validate instances without a schema.
func New(s schema.Schema) *Graph {
	if s.IsZero() {
		panic("graph.New: zero Schema")
	}

	return &Graph{
		schema:       s,
		canon:        newCanonicalizer(s),
		instances:    make(map[schema.TypeID]map[string]Instance),
		pending:      make(map[pendingKey]map[*pendingEdge]struct{}),
		attestValues: true,
	}
}

// Add adds a validated instance to the graph.
//
// Add indexes the instance by its TypeID and primary key, creates edges
// for associations, extracts composed children, and resolves any pending
// forward references that target this instance.
//
// A refusal returns a *[diag.ResultError] and installs nothing, since every
// refusal is decided before the install; the graph keeps a duplicate's record.
// The package doc's Error Handling section enumerates the codes Add can emit.
// Add panics if g or ctx is nil, inst is zero, or this graph's schema does not
// hold inst's type under its identity: a validator of another load built it,
// even one over the same text, or the type is outside that schema's import
// closure.
func (g *Graph) Add(ctx context.Context, inst instance.ValidInstance) error {
	_, err := g.add(ctx, inst, "graph.Add", false)
	return err
}

// add is [Graph.Add], and with merge set [Graph.AddOrMerge]. name is the
// calling method's, which its panics and its cancellation message carry. The
// graph draws no issue below failure severity, so its calls take no warnings
// callback.
func (g *Graph) add(ctx context.Context, inst instance.ValidInstance, name string, merge bool) (merged bool, err error) {
	// Programmer errors: a caller cannot recover from any of these.
	if g == nil {
		panic(name + ": nil *Graph receiver")
	}

	if inst.IsZero() {
		panic(name + ": zero ValidInstance")
	}

	if ctx == nil {
		panic(name + ": nil context")
	}

	typ := inst.Type()
	typeID := typ.ID()

	// Which load validated the instance is no fact of the data, so another load's
	// handle is a programmer error, as a zero instance is. It panics before the
	// cancellation check, so a cancelled context does not hide it.
	if held, ok := g.schema.TypeByID(typeID); !ok || held != typ {
		panic(name + ": instance was validated under another schema load, or its type is outside this schema's import closure")
	}

	opCollector := diag.NewCollector(0)

	if err := ctx.Err(); err != nil {
		opCollector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			name+" cancelled: "+err.Error()).Build())
		return false, opCollector.Err()
	}

	// A validator refuses a part type at a root, so only a child read out of
	// [instance.ValidInstance.Composed] arrives here.
	if !typ.HasPrimaryKey() {
		opCollector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_MISSING_PK,
			fmt.Sprintf("type %s has no primary key; cannot add to graph", diag.QuoteValue(g.tagForm(typeID)))).
			WithDetail(diag.DetailKeyTypeName, g.tagForm(typeID)).Build())
		return false, opCollector.Err()
	}

	if typ.IsPart() {
		opCollector.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("part type %s cannot be added directly; add it in its parent's composition", diag.QuoteValue(g.tagForm(typeID)))).
			WithDetail(diag.DetailKeyTypeName, g.tagForm(typeID)).Build())
		return false, opCollector.Err()
	}

	var staged []stagedEdge
	graphInst := g.build(typ, inst, &staged)

	typeName := graphInst.TypeName()
	pkString := graphInst.PrimaryKey().String()

	g.mu.Lock()
	defer g.mu.Unlock()

	typeInstances := g.instances[typeID]
	if typeInstances != nil {
		if existing, found := typeInstances[pkString]; found {
			if merge && len(graphInst.composed) == 0 {
				plan, refused := g.planMerge(typ, existing, staged)
				if len(refused) > 0 {
					for _, issue := range refused {
						opCollector.Collect(issue)
					}
					return false, opCollector.Err()
				}
				g.commitMerge(existing, plan)
				if len(plan) > 0 {
					g.noteMerged(typ, existing)
				}
				return true, nil
			}
			// Duplicate.Instance is documented as carrying no composed
			// children, so the record gets its own childless instance.
			rejected := newInstance(typeName, typeID, graphInst.PrimaryKey(), graphInst.Properties(), inst.Provenance(), true)
			diagBuilder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %q", diag.CutValue(pkString), typeName)).
				WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyPrimaryKey, pkString).
				WithSpan(inst.Provenance().Span())
			if span := existing.Provenance().Span(); !span.IsZero() {
				diagBuilder.WithRelated(diag.RelatedInfo{Span: span, Message: "first declared here"})
			}
			g.duplicates = append(g.duplicates, newDuplicate(rejected, existing))
			opCollector.Collect(diagBuilder.Build())
			return false, opCollector.Err()
		}
	} else {
		g.instances[typeID] = make(map[string]Instance)
	}

	g.instances[typeID][pkString] = graphInst

	for _, se := range staged {
		// An "absent" or "empty" record's key is the empty text, which addresses no
		// instance.
		if targetInst := g.findInstance(se.targetType, se.targetKey); !targetInst.IsZero() {
			g.installEdge(newEdge(se.relation, graphInst, targetInst, se.properties))
			continue
		}
		g.installPendingEdge(&pendingEdge{
			source:       graphInst,
			relation:     se.relation,
			jsonField:    se.jsonField,
			targetType:   se.targetType,
			targetKey:    se.targetKey,
			properties:   se.properties,
			isRequired:   se.isRequired,
			reasonDetail: se.reason,
		})
	}

	// Resolve every pending edge that targets this instance.
	g.resolvePending(typeID, pkString, graphInst)

	return false, nil
}

// Check validates graph completeness, and judges each root a merge grew whose
// type carries an invariant.
//
// Check verifies that all required associations have resolved targets.
// Optional associations may remain unresolved without error. It also runs each
// root [Graph.AddOrMerge] grew, whose type declares or inherits an invariant, through a
// validator, as the snapshot reader's revalidation does.
//
// A failing graph returns a *[diag.ResultError] holding one
// E_UNRESOLVED_REQUIRED for each target of a required association that is not
// in the graph, one for each required association whose field is absent or
// whose array is empty, and each failure a grown root draws, such as
// E_INVARIANT_FAIL, carrying the root's key; a cancelled ctx returns a Fatal
// E_CONTEXT_CANCELLED. Each call validates each grown root again, and a root
// seeded by [NewFromSnapshot] is judged only once a merge in this graph grows it.
func (g *Graph) Check(ctx context.Context) error {
	if g == nil {
		panic("graph.Check: nil *Graph receiver")
	}

	if ctx == nil {
		panic("graph.Check: nil context")
	}

	opCollector := diag.NewCollector(0)

	if err := ctx.Err(); err != nil {
		opCollector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.Check cancelled: "+err.Error()).Build())
		return opCollector.Err()
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.judgeMerged(ctx, opCollector) {
		return opCollector.Err()
	}

	for _, set := range g.pending {
		for pend := range set {
			if !pend.isRequired {
				continue
			}
			var reason string
			switch pend.reasonDetail {
			case ReasonAbsent:
				reason = "association field is absent"
			case ReasonEmpty:
				reason = "association array is empty"
			default:
				reason = "target instance not found"
			}

			builder := diag.NewIssue(diag.Error, diag.E_UNRESOLVED_REQUIRED,
				fmt.Sprintf("required association %q is unresolved: %s", pend.relation, reason)).
				WithDetail(diag.DetailKeyTypeName, pend.source.TypeName()).
				WithDetail(diag.DetailKeyPrimaryKey, pend.source.PrimaryKey().String()).
				WithDetail(diag.DetailKeyRelationName, pend.relation).
				WithDetail(diag.DetailKeyJSONField, pend.jsonField).
				WithDetail(diag.DetailKeyReason, pend.reasonDetail.String())

			// The absent and empty reasons carry no target to name.
			if pend.reasonDetail == ReasonTargetMissing {
				builder = builder.WithDetail(diag.DetailKeyTargetType, schema.TagForm(g.schema, pend.targetType)).
					WithDetail(diag.DetailKeyTargetPK, pend.targetKey)
			}

			opCollector.Collect(builder.WithSpan(pend.source.Provenance().Span()).Build())

		}
	}

	return opCollector.Err()
}

// Snapshot creates a point-in-time snapshot of the graph.
//
// The returned [Snapshot] is independent of subsequent graph modifications,
// and no method of it changes it. Every collection accessor on Snapshot answers in sorted order.
//
// Snapshot acquires a read lock. Until Snapshot completes, a concurrent Add
// blocks at its commit.
func (g *Graph) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{}
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	// Clone map tracks original->cloned instance mappings for the entire snapshot.
	// This ensures all references within the Snapshot point to cloned instances,
	// making the snapshot truly independent of future graph mutations.
	cloneMap := make(map[Instance]Instance)

	// Collect and sort type identities
	types := make([]schema.TypeID, 0, len(g.instances))
	for typeID := range g.instances {
		types = append(types, typeID)
	}

	// Build instances map with deep-cloned instances per type
	instances := make(map[schema.TypeID][]Instance, len(g.instances))
	instanceIndex := make(map[schema.TypeID]map[string]Instance, len(g.instances))

	for typeID, typeInstances := range g.instances {
		// Clone and collect instances
		insts := make([]Instance, 0, len(typeInstances))
		for _, inst := range typeInstances {
			cloned := cloneInstance(inst, cloneMap)
			insts = append(insts, cloned)
		}

		instances[typeID] = insts

		// Build index from cloned instances
		idx := make(map[string]Instance, len(insts))
		for _, inst := range insts {
			idx[inst.PrimaryKey().String()] = inst
		}
		instanceIndex[typeID] = idx
	}

	// Every record below names a root, which the walk above cloned: an edge's two
	// ends, a pending record's source, and a duplicate's conflict.
	edges := make([]Edge, len(g.edges))
	for i, e := range g.edges {
		edges[i] = newEdge(e.relation, cloneMap[e.source], cloneMap[e.target], e.properties)
	}

	duplicates := make([]Duplicate, len(g.duplicates))
	for i, d := range g.duplicates {
		// A rejected instance is in no tree the walk above reached.
		duplicates[i] = newDuplicate(cloneInstance(d.instance, cloneMap), cloneMap[d.conflict])
	}

	totalPending := 0
	for _, set := range g.pending {
		totalPending += len(set)
	}
	unresolved := make([]UnresolvedEdge, 0, totalPending)
	for _, set := range g.pending {
		for pend := range set {
			unresolved = append(unresolved, newUnresolvedEdge(
				cloneMap[pend.source], pend.relation, pend.targetType, pend.targetKey,
				pend.isRequired, pend.reasonDetail, pend.properties,
			))
		}
	}

	// The Values attestation is graph-level state, never an instance walk:
	// a graph seeded from a loaded snapshot holds clones that report
	// Validated() == false while the accumulator carries the header's claim.
	hasInstance := false
	for _, typeInstances := range g.instances {
		if len(typeInstances) > 0 {
			hasInstance = true
			break
		}
	}
	requiredUnresolved := false
	for _, set := range g.pending {
		for pend := range set {
			if pend.isRequired {
				requiredUnresolved = true
				break
			}
		}
		if requiredUnresolved {
			break
		}
	}
	// A built snapshot always makes a claim; only a loaded document whose
	// header states none can carry none.
	att := &Attestation{
		Values:       hasInstance && g.attestValues,
		Associations: !requiredUnresolved,
	}

	return newSnapshot(g.schema, g.canon, types, instances, instanceIndex, edges, duplicates, unresolved, att)
}

// tagForm renders id under the graph's schema, as every name the graph
// returns and every graph diagnostic does.
func (g *Graph) tagForm(id schema.TypeID) string {
	return schema.TagForm(g.schema, id)
}

// findInstance looks up an instance by TypeID and key.
func (g *Graph) findInstance(typeID schema.TypeID, key string) Instance {
	if typeInstances := g.instances[typeID]; typeInstances != nil {
		return typeInstances[key]
	}
	return Instance{}
}

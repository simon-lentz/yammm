package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// Graph builds an in-memory data structure from validated instances.
//
// Graph is safe for concurrent use from multiple goroutines. Multiple
// callers may invoke [Graph.Add] concurrently; the graph handles forward
// references and duplicate detection atomically.
//
// Add and Check accept a [context.Context] for cancellation. Cancellation does
// not corrupt internal state; partial results may be inspected.
type Graph struct {
	schema schema.Schema
	mu     sync.RWMutex

	// canon rewrites what the import brings in, and a [Snapshot]'s received
	// addresses, into the form a validator stores.
	canon *canonicalizer

	// instances indexes instances by TypeID, then by PK string.
	instances map[schema.TypeID]map[string]Instance

	// edges holds all resolved association edges.
	edges []Edge

	// pending holds unresolved forward references.
	// Key: pendingKey{targetTypeID, targetKey}
	// Multiple sources can reference the same target, so we store a slice.
	pending map[pendingKey][]*pendingEdge

	// duplicates holds duplicate PK records.
	duplicates []Duplicate

	// bySource indexes edges and pending records by source and association,
	// for [BatchAssembler.AddValidOrMerge]. It is nil until the first call to
	// reach the merge rules, at a held key with no composed children, builds it.
	bySource map[recordSlot]*heldRecords

	// collector accumulates diagnostics.
	collector *diag.Collector

	// attestValues is the Values attestation: true for a new graph, whose every
	// record comes from a validator, and a seeded graph's loaded header's claim.
	attestValues bool
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
	reasonDetail string // "absent", "empty", or ""
}

// New creates a new Graph bound to the given schema.
//
// Panics if schema is zero (programmer error). A zero schema is never valid
// as there is no way to validate instances without a schema.
func New(s schema.Schema) *Graph {
	if s.IsZero() {
		panic("graph.New: zero schema")
	}

	return &Graph{
		schema:       s,
		canon:        newCanonicalizer(s),
		instances:    make(map[schema.TypeID]map[string]Instance),
		pending:      make(map[pendingKey][]*pendingEdge),
		collector:    diag.NewCollector(0), // unlimited
		attestValues: true,
	}
}

// Add adds a validated instance to the graph.
//
// Add indexes the instance by its TypeID and primary key, creates edges
// for associations, extracts composed children, and resolves any pending
// forward references that target this instance.
//
// Return semantics: check [diag.Result.OK] for success. A non-OK result
// contains diagnostic issues and no instance installs — every rejection is
// decided before the install. The graph keeps the issues and a duplicate's record. The package doc's Error Handling
// section enumerates the codes Add can emit.
//
// Panics if g or ctx is nil, inst is zero, or this graph's schema does not hold
// inst's type under its identity: a validator of another load built it, even one
// over the same text, or the type is outside that schema's import closure.
func (g *Graph) Add(ctx context.Context, inst instance.ValidInstance) diag.Result {
	_, res := g.add(ctx, inst, false)
	return res
}

// add is [Graph.Add], and with merge set the merge
// [BatchAssembler.AddValidOrMerge] states.
func (g *Graph) add(ctx context.Context, inst instance.ValidInstance, merge bool) (merged bool, res diag.Result) {
	// Programmer errors: a caller cannot recover from any of these.
	if g == nil {
		panic("graph.Add: nil *Graph receiver")
	}

	if inst.IsZero() {
		panic("graph.Add: zero ValidInstance")
	}

	if ctx == nil {
		panic("graph.Add: nil context")
	}

	opCollector := diag.NewCollector(0)

	if err := ctx.Err(); err != nil {
		return false, g.reject(opCollector, diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.Add cancelled: "+err.Error()).Build())
	}

	typ := inst.Type()
	typeID := typ.ID()

	// Which load validated the instance is no fact of the data, so another load's
	// handle is a programmer error, as a zero instance is.
	if held, ok := g.schema.TypeByID(typeID); !ok || held != typ {
		panic("graph.Add: instance was validated under another schema load, or its type is outside this schema's import closure")
	}

	if !schema.Addressable(g.schema, typeID) {
		builder := diag.NewIssue(diag.Error, diag.E_GRAPH_TYPE_NOT_FOUND,
			fmt.Sprintf("type %s not found in schema", diag.QuoteValue(inst.TypeName()))).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).
			WithDetail(diag.DetailKeyTypeSchema, typeID.SchemaPath().String()).
			WithHint("if this type is from a transitively imported schema, add a direct import to access it")
		if pk := inst.PrimaryKey(); pk.Len() > 0 {
			builder = builder.WithDetail(diag.DetailKeyPrimaryKey, pk.String())
		}
		return false, g.reject(opCollector, builder.Build())
	}

	// A validator refuses a part type at a root, so only a child read out of
	// [instance.ValidInstance.Compositions] arrives here.
	if !typ.HasPrimaryKey() {
		return false, g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_MISSING_PK,
			fmt.Sprintf("type %s has no primary key; cannot add to graph", diag.QuoteValue(inst.TypeName()))).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build())
	}

	if typ.IsPart() {
		return false, g.reject(opCollector, diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("part type %s cannot be added directly; add it in its parent's composition", diag.QuoteValue(inst.TypeName()))).
			WithDetail(diag.DetailKeyTypeName, inst.TypeName()).Build())
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
						res = g.reject(opCollector, issue)
					}
					return false, res
				}
				g.commitMerge(existing, plan)
				return true, opCollector.Result()
			}
			// Duplicate.Instance is documented as carrying no composed
			// children, so the record gets its own childless instance.
			rejected := newInstance(typeName, typeID, graphInst.PrimaryKey(), graphInst.Properties(), inst.Provenance(), true)
			diagBuilder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK,
				fmt.Sprintf("duplicate primary key %s for type %q", diag.ValueText(pkString), typeName)).
				WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyPrimaryKey, pkString).
				WithSpan(inst.Provenance().Span())
			g.duplicates = append(g.duplicates, newDuplicate(rejected, existing))
			return false, g.reject(opCollector, diagBuilder.Build())
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
			reasonDetail: se.reason, // Check renders the empty form as "target_missing".
		})
	}

	// Resolve every pending edge that targets this instance.
	g.resolvePending(typeID, pkString, graphInst)

	return false, opCollector.Result()
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
//   - E_UNRESOLVED_REQUIRED: A required association's target is not in the graph,
//     or its field is absent, or its array is empty
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

	if err := ctx.Err(); err != nil {
		opCollector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
			"graph.Check cancelled: "+err.Error()).Build())
		return opCollector.Result()
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, pendingList := range g.pending {
		for _, pend := range pendingList {
			if !pend.isRequired {
				continue
			}
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
				builder = builder.WithDetail(diag.DetailKeyTargetType, schema.TagForm(g.schema, pend.targetType)).
					WithDetail(diag.DetailKeyTargetPK, pend.targetKey)
			}

			opCollector.Collect(builder.WithSpan(pend.source.Provenance().Span()).Build())

		}
	}

	return opCollector.Result()
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
	for _, pendingList := range g.pending {
		totalPending += len(pendingList)
	}
	unresolved := make([]UnresolvedEdge, 0, totalPending)
	for _, pendingList := range g.pending {
		for _, pend := range pendingList {
			// Determine reason token
			reason := pend.reasonDetail
			if reason == "" {
				reason = "target_missing"
			}
			unresolved = append(unresolved, newUnresolvedEdge(
				cloneMap[pend.source], pend.relation, pend.targetType, pend.targetKey,
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
	// A built snapshot always makes a claim; only a loaded document can carry
	// none, and only because it was written before the claim existed.
	att := &Attestation{
		Values:       hasInstance && g.attestValues,
		Associations: !requiredUnresolved,
	}

	return newSnapshot(g.schema, g.canon, types, instances, instanceIndex, edges, duplicates, unresolved, g.collector.Result(), att)
}

// identityText renders a type identity for a message: whole where s resolves
// it. Otherwise the caller wrote it, so its name is cut as a value is, and so is
// its schema path unless a schema of the closure carries that path.
func identityText(s schema.Schema, id schema.TypeID) string {
	if _, ok := s.TypeByID(id); ok {
		return id.String()
	}
	name := diag.ValueText(id.Name())
	path := id.SchemaPath()
	if path.IsZero() {
		return name
	}
	for cs := range s.Closure() {
		if cs.SourceID() == path {
			return path.String() + ":" + name
		}
	}
	return diag.ValueText(path.String()) + ":" + name
}

// instanceTagForm computes the canonical instance tag form for a TypeID.
func (g *Graph) instanceTagForm(id schema.TypeID) string {
	return schema.TagForm(g.schema, id)
}

// findInstance looks up an instance by TypeID and key.
func (g *Graph) findInstance(typeID schema.TypeID, key string) Instance {
	if typeInstances := g.instances[typeID]; typeInstances != nil {
		return typeInstances[key]
	}
	return Instance{}
}

// checkKey rejects an empty key, a key whose arity disagrees with the type's
// declared primary keys, a key property that is absent or holds none
// ([value.Absent]), and a component that disagrees with its key property. A key
// states what the instance's own properties state, so a key property the
// instance does not hold leaves the key unstated. Every component reads back
// through [ParseKey]: a validator stores only strings in a key, and the snapshot
// reader refuses any other component that does not.
func checkKey(typ schema.Type, key immutable.Key, property func(string) (immutable.Value, bool), canon *canonicalizer) error {
	if key.Len() == 0 {
		return errors.New("primary key is empty")
	}
	if declared := keyArity(typ); key.Len() != declared {
		return fmt.Errorf("primary key has %d components; type declares %d", key.Len(), declared)
	}
	i := 0
	for pk := range typ.PrimaryKeys() {
		val, ok := property(pk.Name())
		if !ok || value.Absent(val.Unwrap()) {
			return fmt.Errorf("primary key property %q is absent or null", pk.Name())
		}
		if !keyComponentAgrees(key.Get(i), val, pk.Constraint(), canon) {
			return fmt.Errorf("primary key component %d disagrees with property %q (%s vs %s)",
				i, pk.Name(), diag.ValueText(renderKeyComponent(key.Get(i))), diag.ValueText(renderKeyComponent(val)))
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
//
// Both sides are first put in the form the constraint stores, so two spellings
// of one Timestamp agree: the graph holds one spelling per value, and refusing
// the pair here would refuse a record the rebuild path accepts.
func keyComponentAgrees(component, property immutable.Value, c schema.Constraint, canon *canonicalizer) bool {
	// One scalar stored twice is one value in every form, so the common case
	// renders nothing; any other pair reaches the canonical form.
	if sameScalar(component.Unwrap(), property.Unwrap()) {
		return true
	}
	x, y := canon.canonicalOrRaw(component, c), canon.canonicalOrRaw(property, c)
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
	return renderKeyValue(x) == renderKeyValue(y)
}

// sameScalar reports whether x and y are one string, int64 or bool. It never
// reports a float or a nil equal, for the reasons keyComponentAgrees states.
func sameScalar(x, y any) bool {
	switch xv := x.(type) {
	case string:
		yv, ok := y.(string)
		return ok && xv == yv
	case int64:
		yv, ok := y.(int64)
		return ok && xv == yv
	case bool:
		yv, ok := y.(bool)
		return ok && xv == yv
	}
	return false
}

// renderKeyComponent renders one value in the canonical key form. Diagnostics
// name the spelling the caller wrote, so they render the value as it arrived.
func renderKeyComponent(v immutable.Value) string {
	return renderKeyValue(v.Unwrap())
}

// renderKeyValue renders one unwrapped value in the canonical key form.
func renderKeyValue(v any) string {
	return immutable.WrapKey([]any{v}).String()
}

// reject records issue in the per-call collector and in the graph's cumulative
// one, and returns the per-call result. [diag.Collector] synchronizes itself,
// so the graph's own lock is not needed for the second write.
func (g *Graph) reject(opCollector *diag.Collector, issue diag.Issue) diag.Result {
	opCollector.Collect(issue)
	g.collector.Collect(issue)
	return opCollector.Result()
}

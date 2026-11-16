package graph

// NewFromSnapshot creates a Graph over snap's own schema, holding snap's
// contents, ready for [Graph.Add] calls that resolve against them. Every
// constructor of a [Snapshot] holds it to the package doc's "Structural facts"
// under its schema, so the import refuses nothing and rewrites nothing.
//
// Panics if snap is zero (programmer error).
func NewFromSnapshot(snap Snapshot) *Graph {
	if snap.IsZero() {
		panic("graph.NewFromSnapshot: zero Snapshot")
	}
	g := New(snap.schema)
	g.importSnapshot(snap)
	return g
}

// importSnapshot installs snap's records into g, a fresh Graph of snap's
// schema, as clones, bypassing Add: no duplicate detection, no extraction.
func (g *Graph) importSnapshot(snap Snapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	cloneMap := make(map[Instance]Instance)
	imported := 0
	for _, typeID := range snap.Types() {
		if g.instances[typeID] == nil {
			g.instances[typeID] = make(map[string]Instance)
		}
		for inst := range snap.InstancesOf(typeID) {
			cloned := cloneInstance(inst, cloneMap)
			g.instances[typeID][cloned.PrimaryKey().String()] = cloned
			imported++
		}
	}

	// Importing no instance imports no unproven data, and a snapshot with no
	// claim makes the graph's Values claim false, as a false claim does.
	if imported > 0 {
		att := snap.Attestation()
		g.attestValues = g.attestValues && att != nil && att.Values
	}

	for edge := range snap.Edges() {
		g.installEdge(newEdge(edge.Relation(), cloneMap[edge.Source()], cloneMap[edge.Target()], edge.Properties()))
	}
	for _, unres := range snap.unresolved {
		g.installPending(cloneMap[unres.source], unres)
	}
	for dup := range snap.Duplicates() {
		// The conflict is a root, which the loop above cloned.
		g.duplicates = append(g.duplicates, newDuplicate(cloneInstance(dup.instance, cloneMap), cloneMap[dup.conflict]))
	}
}

// installPending installs u, an unresolved record of source, as [Graph.Add]
// stages it, with whether the association is required read from the schema.
func (g *Graph) installPending(source Instance, u UnresolvedEdge) {
	typ, _ := g.schema.TypeByID(source.typeID)
	rel, _ := typ.Relation(u.relation)
	reason := u.reason
	// Graph.Snapshot renders the empty reason Add stores as "target_missing".
	if reason == "target_missing" {
		reason = ""
	}
	g.installPendingEdge(&pendingEdge{
		source:       source,
		relation:     u.relation,
		jsonField:    rel.FieldName(),
		targetType:   rel.TargetID(),
		targetKey:    u.targetKey,
		properties:   u.properties,
		isRequired:   !rel.IsOptional(),
		reasonDetail: reason,
	})
}

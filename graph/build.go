package graph

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// stagedEdge is one association record resolved against the schema but not yet
// against the graph, which only the commit phase may read. reason is empty for
// a target-bearing reference, or "absent" / "empty" for the two shapes that
// name no target.
type stagedEdge struct {
	relation   string
	jsonField  string
	targetType schema.TypeID
	targetKey  string
	properties immutable.Properties
	isRequired bool
	reason     string
}

// build returns the Instance tree of inst, a record [Graph.add] holds to be of
// a type this graph's load declares. Its validator walked that same type, so
// every member, multiplicity and key it holds is one the type states, and every
// value is in the form the type stores. stage receives the root's association
// records: a load refuses a part type that carries an association, so a child
// stages none.
func (g *Graph) build(typ schema.Type, inst instance.ValidInstance, stage *[]stagedEdge) Instance {
	id := typ.ID()
	graphInst := newInstance(g.tagForm(id), id, inst.PrimaryKey(), inst.Properties(), inst.Provenance(), true)

	for rel := range typ.AllAssociations() {
		stageRelation(rel, inst, stage)
	}

	for relationName, composedValue := range inst.Compositions() {
		children, _ := composedValue.Slice()
		for i := range children.Len() {
			child := children.Get(i).Unwrap().(instance.ValidInstance) //nolint:errcheck // Compositions states the element type.
			graphInst.addComposed(relationName, g.build(child.Type(), child, stage))
		}
	}

	return graphInst
}

// stageRelation records what the commit phase resolves for one association of a
// root: each target it names, or the "empty" or "absent" record a required
// association draws where it names none.
func stageRelation(rel schema.Relation, inst instance.ValidInstance, stage *[]stagedEdge) {
	isRequired := !rel.IsOptional()
	edgeData, present := inst.Edge(rel.Name())
	for target := range edgeData.Targets() {
		*stage = append(*stage, stagedEdge{
			relation:   rel.Name(),
			jsonField:  rel.FieldName(),
			targetType: rel.TargetID(),
			targetKey:  target.TargetKey().String(),
			properties: target.Properties(),
			isRequired: isRequired,
		})
	}
	if !isRequired || !edgeData.IsEmpty() {
		return
	}
	reason := "absent"
	if present {
		reason = "empty"
	}
	*stage = append(*stage, stagedEdge{
		relation:   rel.Name(),
		jsonField:  rel.FieldName(),
		targetType: rel.TargetID(),
		isRequired: true,
		reason:     reason,
	})
}

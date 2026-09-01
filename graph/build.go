package graph

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// instanceBuilder walks a [instance.ValidInstance] ONCE: it reports every
// structural violation and assembles the [Instance] tree a clean walk installs.
// Nothing it builds touches the graph, so a caller that finds errors discards
// the tree and the graph is unchanged.
//
// The two-walk shape it replaced — check, then attach — kept the same four
// predicates in two places, and the attaching walk skipped in silence exactly
// what the checking walk rejected. One traversal makes the invariant structural
// rather than a discipline two functions have to keep.
type instanceBuilder struct {
	g *Graph
	c *diag.Collector

	// attested folds [instance.ValidInstance.Validated] over the root and every
	// descendant. The commit phase applies it under the graph's lock.
	attested bool
}

// stagedEdge is one association record resolved against the schema but not yet
// against the graph, which only the commit phase may read. reason is empty for
// a target-bearing reference, or "absent" / "empty" for the two shapes that
// name no target.
type stagedEdge struct {
	relation   string
	jsonField  string
	targetType schema.TypeID
	// targetTypeName is the rendered target, computed once per relation
	// OUTSIDE the graph's lock — the commit phase only reads it.
	targetTypeName string
	targetKey      string
	properties     immutable.Properties
	isRequired     bool
	reason         string
}

func newInstanceBuilder(g *Graph, c *diag.Collector) *instanceBuilder {
	return &instanceBuilder{g: g, c: c, attested: true}
}

// build walks inst and returns the Instance to install. stage receives the
// instance's association records, and is nil for a composed child: a child's
// edges are checked like any other, but no path installs them — the package
// doc's Composed Children section states that gap.
func (b *instanceBuilder) build(typ *schema.Type, inst *instance.ValidInstance, stage *[]stagedEdge) *Instance {
	b.attested = b.attested && inst.Validated()
	graphInst := newInstance(b.g.instanceTagForm(inst.TypeID()), inst.TypeID(),
		inst.PrimaryKey(), inst.Properties(), inst.Provenance(), inst.Validated())

	// Diagnostics name the graph's canonical tag form, never the instance's
	// self-declared TypeName: a consumer keying on type_name must get a name
	// that round-trips through Snapshot.Types.
	typeName := graphInst.TypeName()

	b.edges(typ, inst, typeName, stage)

	for relationName, composedValue := range inst.Compositions() {
		rel, ok := typ.Relation(relationName)
		if !ok {
			b.c.Collect(undeclaredRelation(typeName, relationName,
				fmt.Sprintf("type %q declares no composition %q", typeName, relationName)))
			continue
		}
		if rel.Kind() != schema.RelationComposition {
			b.c.Collect(undeclaredRelation(typeName, relationName,
				fmt.Sprintf("relation %q on type %q is declared as an association, not a composition",
					relationName, typeName)))
			continue
		}
		b.slot(graphInst, typeName, relationName, rel, composedValue)
	}

	return graphInst
}

// edges checks an instance's association data and, when stage is non-nil,
// records what the commit phase will resolve against the graph.
//
// The map form is materialized only for a staging walk, which needs it for the
// absent-required pass. A composed child's edges are checked but never
// installed, so its walk reads the iterator and allocates nothing.
func (b *instanceBuilder) edges(typ *schema.Type, inst *instance.ValidInstance, typeName string, stage *[]stagedEdge) {
	if stage == nil {
		for relationName, edgeData := range inst.Edges() {
			b.checkEdgeRelation(typ, typeName, relationName, edgeData)
		}
		return
	}

	declared := b.g.iterEdges(inst)
	for relationName, edgeData := range declared {
		rel, ok := b.checkEdgeRelation(typ, typeName, relationName, edgeData)
		if !ok {
			continue
		}
		isRequired := !rel.IsOptional()
		targetName := b.g.instanceTagForm(rel.TargetID())
		for target := range edgeData.TargetsIter() {
			*stage = append(*stage, stagedEdge{
				relation:       relationName,
				jsonField:      rel.FieldName(),
				targetType:     rel.TargetID(),
				targetTypeName: targetName,
				targetKey:      target.TargetKey().String(),
				properties:     target.Properties(),
				isRequired:     isRequired,
			})
		}
		if isRequired && edgeData.IsEmpty() {
			*stage = append(*stage, stagedEdge{
				relation:       relationName,
				jsonField:      rel.FieldName(),
				targetType:     rel.TargetID(),
				targetTypeName: targetName,
				isRequired:     true,
				reason:         "empty",
			})
		}
	}

	for rel := range typ.AllAssociations() {
		if rel.IsOptional() {
			continue
		}
		if _, present := declared[rel.Name()]; present {
			continue
		}
		*stage = append(*stage, stagedEdge{
			relation:       rel.Name(),
			jsonField:      rel.FieldName(),
			targetType:     rel.TargetID(),
			targetTypeName: b.g.instanceTagForm(rel.TargetID()),
			isRequired:     true,
			reason:         "absent",
		})
	}
}

// checkEdgeRelation validates one association slot: the name must be declared
// as an association, and a (one) slot may carry at most one target. It reports
// the relation when the slot is sound enough to install.
func (b *instanceBuilder) checkEdgeRelation(
	typ *schema.Type,
	typeName string,
	relationName string,
	edgeData *instance.ValidEdgeData,
) (*schema.Relation, bool) {
	rel, ok := typ.Relation(relationName)
	if !ok {
		b.c.Collect(undeclaredRelation(typeName, relationName,
			fmt.Sprintf("type %q declares no association %q", typeName, relationName)))
		return nil, false
	}
	if rel.Kind() != schema.RelationAssociation {
		b.c.Collect(undeclaredRelation(typeName, relationName,
			fmt.Sprintf("relation %q on type %q is declared as a composition, not an association",
				relationName, typeName)))
		return nil, false
	}
	if rel.IsMany() {
		return rel, true
	}
	targets := 0
	for range edgeData.TargetsIter() {
		targets++
	}
	if targets > 1 {
		b.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
			fmt.Sprintf("association %q on type %q is (one) and carries %d targets",
				relationName, typeName, targets)).
			WithDetail(diag.DetailKeyTypeName, typeName).
			WithDetail(diag.DetailKeyRelationName, relationName).Build())
		return nil, false
	}
	return rel, true
}

// slot checks one composition slot — its cardinality and the shape of what
// arrived in it — and builds every child it admits.
func (b *instanceBuilder) slot(
	graphParent *Instance,
	parentName string,
	relationName string,
	rel *schema.Relation,
	composedValue immutable.Value,
) {
	switch v := composedValue.Unwrap().(type) {
	case immutable.Slice:
		if !rel.IsMany() && v.Len() > 1 {
			b.c.Collect(b.g.composedOverflowIssue(parentName, relationName, rel, v))
		}
		seen := make(map[string]int, v.Len())
		for i := range v.Len() {
			child, ok := v.Get(i).Unwrap().(*instance.ValidInstance)
			if !ok || child == nil {
				b.c.Collect(nonInstanceChild(parentName, relationName, rel, i))
				continue
			}
			b.child(graphParent, parentName, relationName, rel, child, i, seen)
		}
	case *instance.ValidInstance:
		if v == nil {
			b.c.Collect(nonInstanceChild(parentName, relationName, rel, 0))
			return
		}
		b.child(graphParent, parentName, relationName, rel, v, 0, nil)
	case nil:
		// An absent optional composition arrives as a nil value.
	default:
		b.c.Collect(nonInstanceChild(parentName, relationName, rel, 0))
	}
}

// child checks one composed child against its relation target and, if it holds,
// builds it and attaches it to the parent under construction. seen accumulates
// the sibling keys already met, and is nil for a slot that cannot hold siblings.
func (b *instanceBuilder) child(
	graphParent *Instance,
	parentName string,
	relationName string,
	rel *schema.Relation,
	child *instance.ValidInstance,
	index int,
	seen map[string]int,
) {
	if child.TypeID() != rel.TargetID() {
		got, want, ident := b.g.describeTypePair(child.TypeID(), rel.TargetID())
		b.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("child type %s does not match relation target %s",
				quoteTypeName(got, ident), quoteTypeName(want, ident))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relationName).
			WithDetail(diag.DetailKeyExpected, want).
			WithDetail(diag.DetailKeyGot, got).Build())
		return
	}

	// A composed child is addressed by identity, so it resolves across the
	// whole import closure — where [Graph.ownsType] states the narrower policy
	// [Graph.Add] applies to a root.
	childTyp, ok := b.g.schema.TypeByID(child.TypeID())
	if !ok {
		b.c.Collect(unresolvableChildType(child))
		return
	}

	if childTyp.HasPrimaryKey() {
		if err := checkInstanceKey(childTyp, child); err != nil {
			b.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
				fmt.Sprintf("composed child of type %q: %s", child.TypeName(), err)).
				WithDetail(diag.DetailKeyTypeName, child.TypeName()).
				WithDetail(diag.DetailKeyRelationName, relationName).
				WithDetail(diag.DetailKeyPrimaryKey, child.PrimaryKey().String()).Build())
			return
		}
		if seen != nil && rel.IsMany() {
			keyStr := child.PrimaryKey().String()
			if first, dup := seen[keyStr]; dup {
				b.c.Collect(b.g.siblingDuplicateIssue(parentName, relationName, rel, child, first, index))
			} else {
				seen[keyStr] = index
			}
		}
	}

	graphParent.addComposed(relationName, b.build(childTyp, child, nil))
}

// unresolvableChildType reports a composed child whose identity is not in the
// schema's import closure.
//
// No public constructor reaches it: the caller's identity must already equal
// the relation's target, and a relation's target is resolved by the schema
// layer at load, so it is always in the closure. Reaching this means the
// closure and the relation disagree, which is why it is Fatal E_INTERNAL and
// not a graph-category error — and why no test drives it. Dropping the subtree
// in silence was the alternative, and that is the regression it exists to
// prevent.
func unresolvableChildType(child *instance.ValidInstance) diag.Issue {
	return diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
		fmt.Sprintf("composed child type %s is not in the schema's import closure", child.TypeID())).
		WithDetail(diag.DetailKeyTypeName, child.TypeName()).
		WithDetail(diag.DetailKeyTypeSchema, child.TypeID().SchemaPath().String()).Build()
}

// composedOverflowIssue reports a (one) composition slot carrying several
// children. The address it names is the second child's, which is the one the
// slot cannot hold.
func (g *Graph) composedOverflowIssue(
	parentName string,
	relationName string,
	rel *schema.Relation,
	children immutable.Slice,
) diag.Issue {
	builder := diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
		fmt.Sprintf("composition %q: (one) cardinality violated, got %d children", relationName, children.Len())).
		WithDetail(diag.DetailKeyTypeName, parentName).
		WithDetail(diag.DetailKeyRelationName, relationName).
		WithDetail(diag.DetailKeyJSONField, rel.FieldName())

	// No primary_key detail: the whole record is refused, so not one of these
	// children attaches and no composed key is assigned to any of them. A
	// detail naming an address the writers never create is worse than none.
	return builder.Build()
}

// siblingDuplicateIssue reports two children of one (many) slot sharing a
// primary key. The streamed path in [Graph.AddComposed] enforces the same rule
// against the children already attached.
func (g *Graph) siblingDuplicateIssue(
	parentName string,
	relationName string,
	rel *schema.Relation,
	child *instance.ValidInstance,
	firstIndex, index int,
) diag.Issue {
	return diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
		fmt.Sprintf("duplicate composed child primary key %s at indices %d and %d",
			child.PrimaryKey().String(), firstIndex, index)).
		WithDetail(diag.DetailKeyTypeName, parentName).
		WithDetail(diag.DetailKeyRelationName, relationName).
		WithDetail(diag.DetailKeyJSONField, rel.FieldName()).
		WithDetail(diag.DetailKeyPrimaryKey, child.PrimaryKey().String()).
		Build()
}

// undeclaredRelation builds the diagnostic for instance data filed under a
// name the type does not declare in the slot it arrived in.
func undeclaredRelation(typeName, relationName, msg string) diag.Issue {
	return diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION, msg).
		WithDetail(diag.DetailKeyTypeName, typeName).
		WithDetail(diag.DetailKeyRelationName, relationName).Build()
}

// nonInstanceChild builds the diagnostic for a composition slot holding
// something that is not an instance — a typed nil among them, which
// dereferenced rawly before this guard existed.
func nonInstanceChild(typeName, relationName string, rel *schema.Relation, index int) diag.Issue {
	return diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
		fmt.Sprintf("composition %q on type %q: child %d is not an instance",
			relationName, typeName, index)).
		WithDetail(diag.DetailKeyTypeName, typeName).
		WithDetail(diag.DetailKeyRelationName, relationName).
		WithDetail(diag.DetailKeyJSONField, rel.FieldName()).Build()
}

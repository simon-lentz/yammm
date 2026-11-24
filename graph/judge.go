package graph

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// judgeTarget is the validator a root's type is judged by and the name that
// validator resolves the type by.
type judgeTarget struct {
	v    *instance.Validator
	name string
}

// noteMerged records held, a root a merge grew, for [Graph.Check] to judge when
// its type declares an invariant: a validator judged each listing of the root
// alone, and no invariant has seen the records they hold together. Called
// under g.mu.
func (g *Graph) noteMerged(typ schema.Type, held Instance) {
	if !hasInvariant(typ) {
		return
	}
	if g.merged == nil {
		g.merged = make(map[Instance]struct{})
		g.judges = make(map[schema.TypeID]judgeTarget)
	}
	if _, ok := g.judges[typ.ID()]; !ok {
		g.judges[typ.ID()] = g.judgeFor(typ)
	}
	g.merged[held] = struct{}{}
}

func hasInvariant(typ schema.Type) bool {
	for range typ.AllInvariants() {
		return true
	}
	return false
}

// judgeFor returns the validator that judges a root of t, and the name it
// resolves t by, as the snapshot reader's revalidation chooses them. A type the
// graph's schema can name is judged under that name by a validator over that
// schema. Any other is judged under its declared name by a validator over its
// declaring schema: the graph's schema resolves no name to it. Like the
// reader's, the validator caps no root's issues. Called under g.mu.
func (g *Graph) judgeFor(t schema.Type) judgeTarget {
	id := t.ID()
	if tag, ok := schema.AddressableTag(g.schema, id); ok {
		if g.judge == nil {
			g.judge = instance.NewValidator(g.schema, instance.WithIssueLimit(diag.NoLimit))
		}
		return judgeTarget{v: g.judge, name: tag}
	}
	for s := range g.schema.Closure() {
		if s.SourceID() == id.SchemaPath() {
			return judgeTarget{v: instance.NewValidator(s, instance.WithIssueLimit(diag.NoLimit)), name: id.Name()}
		}
	}
	// TypeByID found t in the closure, so its declaring schema is there.
	panic("graph: no schema of the closure declares " + g.tagForm(id))
}

// judgeMerged runs each merged root's held form through its type's validator,
// in (type name, key) order, and adds what it finds to c. It reports false when
// ctx is cancelled, having added the cancellation. Called under g.mu's read lock.
func (g *Graph) judgeMerged(ctx context.Context, c *diag.Collector) bool {
	if len(g.merged) == 0 {
		return true
	}
	roots := slices.SortedFunc(maps.Keys(g.merged), func(a, b Instance) int {
		return cmp.Or(cmp.Compare(a.typeName, b.typeName), cmp.Compare(a.primaryKey.String(), b.primaryKey.String()))
	})
	for _, root := range roots {
		typ, _ := g.schema.TypeByID(root.typeID)
		target := g.judges[root.typeID]
		_, err := target.v.ValidateOne(ctx, target.name, instance.RawInstance{
			Properties: g.heldForm(typ, root),
			Provenance: root.provenance,
		})
		if ctxErr := ctx.Err(); ctxErr != nil {
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED,
				"graph.Check cancelled: "+ctxErr.Error()).Build())
			return false
		}
		if err == nil {
			continue
		}
		typeName, key := root.typeName, root.primaryKey.String()
		re, ok := errors.AsType[*diag.ResultError](err)
		if !ok {
			c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("merged root %s[%s]: %s", typeName, diag.CutValue(key), err.Error())).
				WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyPrimaryKey, key).Build())
			continue
		}
		c.MergeFunc(re.Result, func(issue diag.Issue) diag.Issue {
			return mergedRootIssue(issue, typeName, key)
		})
	}
	return true
}

// mergedRootIssue returns issue naming the grown root it was drawn on: its
// type-name and key details are the root's, the type under the graph's
// rendering. The message stays the validator's, which for E_INVARIANT_FAIL is
// the invariant's own message.
func mergedRootIssue(issue diag.Issue, typeName, key string) diag.Issue {
	b := diag.NewIssue(issue.Severity(), issue.Code(), issue.Message())
	for _, d := range issue.Details() {
		if d.Key != diag.DetailKeyTypeName && d.Key != diag.DetailKeyPrimaryKey {
			b = b.WithDetails(d)
		}
	}
	b = b.WithDetail(diag.DetailKeyTypeName, typeName).
		WithDetail(diag.DetailKeyPrimaryKey, key).
		WithRelated(issue.Related()...)
	if issue.HasSpan() {
		b = b.WithSpan(issue.Span())
	}
	if issue.Hint() != "" {
		b = b.WithHint(issue.Hint())
	}
	if issue.SourceName() != "" || issue.Path() != "" {
		b = b.WithPath(issue.SourceName(), issue.Path())
	}
	return b.Build()
}

// heldForm returns inst's raw form as the snapshot reader rebuilds a root it
// revalidates: its properties; each association's edges, then its
// "target_missing" records, as _target_-keyed objects; and its composed children
// as arrays of their own held forms. An edge's key is a [value.Resolved], since a
// held target stands at it; a "target_missing" record's key is judged as held,
// which a graph seeded from an unrevalidated document may hold unjudged.
func (g *Graph) heldForm(typ schema.Type, inst Instance) map[string]any {
	props := inst.properties.Clone()
	if props == nil {
		props = make(map[string]any)
	}
	for rel := range typ.AllAssociations() {
		h := g.bySource[recordSlot{inst, rel.Name()}]
		if h.targets() == 0 {
			continue
		}
		target, _ := g.schema.TypeByID(rel.TargetID())
		pks := slices.Collect(target.PrimaryKeys())
		listing := make([]any, 0, h.targets())
		for _, key := range slices.Sorted(maps.Keys(h.edges)) {
			e := h.edges[key]
			listing = append(listing, targetObject(pks, resolved(e.target.primaryKey.Clone()), e.properties.Clone()))
		}
		for _, key := range slices.Sorted(maps.Keys(h.pending)) {
			p := h.pending[key]
			components, err := ParseKey(p.targetKey)
			if err != nil {
				panic("graph: a held target key does not parse: " + err.Error())
			}
			listing = append(listing, targetObject(pks, components, p.properties.Clone()))
		}
		if rel.IsMany() {
			props[rel.FieldName()] = listing
		} else {
			props[rel.FieldName()] = listing[0]
		}
	}
	for rel := range typ.AllCompositions() {
		kids := inst.composed[rel.Name()]
		if len(kids) == 0 {
			continue
		}
		childType, _ := g.schema.TypeByID(rel.TargetID())
		arr := make([]any, 0, len(kids))
		for _, kid := range kids {
			arr = append(arr, g.heldForm(childType, kid))
		}
		props[rel.FieldName()] = arr
	}
	return props
}

// targetObject returns one listing of an association: obj, a fresh map or nil,
// with each key component under its key property's name and the target prefix
// the validator reads a target key by.
func targetObject(pks []schema.Property, components []any, obj map[string]any) map[string]any {
	if obj == nil {
		obj = make(map[string]any, len(pks))
	}
	for i, comp := range components {
		obj["_target_"+pks[i].Name()] = comp
	}
	return obj
}

// resolved returns components, each a [value.Resolved] the validator takes as
// judged.
func resolved(components []any) []any {
	for i, comp := range components {
		components[i] = value.Resolved{Value: comp}
	}
	return components
}

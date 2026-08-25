package neo4j

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// composedKeyProp is the property carrying a part node's identity. Sibling
// uniqueness is not a Neo4j constraint, so a part's declared primary key
// cannot key its node; this composed path string can. No declared property
// can begin with an underscore, so the name cannot collide.
const composedKeyProp = "_composed_key"

// compositionClosure returns the transitive composition relation names and
// part-type identities reachable from rootType, both sorted. The visited
// set bounds a composition cycle.
func compositionClosure(s *schema.Schema, rootType *schema.Type) (relNames []string, partIDs []schema.TypeID) {
	visited := make(map[schema.TypeID]bool)
	relSeen := make(map[string]bool)

	var walk func(t *schema.Type)
	walk = func(t *schema.Type) {
		for _, rel := range t.AllCompositionsSlice() {
			if !relSeen[rel.Name()] {
				relSeen[rel.Name()] = true
				relNames = append(relNames, rel.Name())
			}
			id := rel.TargetID()
			if visited[id] {
				continue
			}
			visited[id] = true
			partIDs = append(partIDs, id)
			if target, ok := s.TypeByID(id); ok {
				walk(target)
			}
		}
	}
	walk(rootType)

	slices.Sort(relNames)
	slices.SortFunc(partIDs, func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})
	return relNames, partIDs
}

// buildCompositionReplaceQuery renders the subtree delete for one root
// label. The quantified path pattern anchors EVERY hop: the relationship
// must carry one of the closure's composition names, and the node it lands
// on must carry one of the closure's part labels. That anchoring is what
// makes the union safe — an association can neither be declared by a part
// nor target one (schema/collision.go's validateAssociationTargets), so no
// association edge ever lands on a part label, and the path cannot escape
// the subtree whatever names other types reuse. Every part at depth k is
// the endpoint of the k-hop path, so one statement deletes the whole
// subtree with nothing orphaned.
func buildCompositionReplaceQuery(rootLabel string, rootKeys []string, relNames, partLabels []string) string {
	var b strings.Builder
	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MATCH (p:")
	b.WriteString(rootLabel)
	b.WriteString(" {")
	for i, name := range rootKeys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.%s%s", name, batchKeyParamPrefix, name)
	}
	b.WriteString("})\n")
	fmt.Fprintf(&b, "MATCH (p) (()-[:%s]->(:%s)){1,} (c)\n",
		strings.Join(relNames, "|"), strings.Join(partLabels, "|"))
	b.WriteString("DETACH DELETE c")
	return b.String()
}

// buildCompositionCreateQuery renders one create statement for a
// (parent, relation, part) group. A depth-1 parent is a root, matched on
// its primary keys; a deeper parent is itself a part, matched on its own
// composed key. SET c = row.props assigns the whole property map, composed
// key included — never +=, because a part is created fresh after the
// replace phase deleted its predecessor.
func buildCompositionCreateQuery(parentLabel string, parentKeys []string, relName, partLabel string, parentByCK bool) string {
	var b strings.Builder
	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MATCH (p:")
	b.WriteString(parentLabel)
	b.WriteString(" {")
	if parentByCK {
		fmt.Fprintf(&b, "%s: row.parent_ck", composedKeyProp)
	} else {
		for i, name := range parentKeys {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s: row.%s%s", name, batchKeyParamPrefix, name)
		}
	}
	b.WriteString("})\n")
	fmt.Fprintf(&b, "CREATE (p)-[:%s]->(c:%s)\n", relName, partLabel)
	b.WriteString("SET c = row.props")
	return b.String()
}

// composedGroupKey identifies one create statement's group.
type composedGroupKey struct {
	depth       int
	parentLabel string
	relName     string
	partLabel   string
	parentByCK  bool
}

// createGroup accumulates one group's rows.
type createGroup struct {
	key  composedGroupKey
	rows []map[string]any
}

// composedQueries builds the CompositionReplace and CompositionCreate
// phases. Replace rows come from the SCHEMA, not from the instances'
// children: every instance of a root type whose composition closure is
// non-empty emits a replace row, so a write with zero children still
// deletes stale ones. Create groups run parent-first (depth ascending).
func composedQueries(result *graph.Snapshot, shapes *GraphShape, cfg *writeConfig) ([]*BatchNodeQuery, error) {
	s := result.Schema()
	var replace []*BatchNodeQuery

	groups := make(map[composedGroupKey]*createGroup)

	for _, typeID := range result.Types() {
		rootType, ok := s.TypeByID(typeID)
		if !ok {
			continue
		}
		relNames, partIDs := compositionClosure(s, rootType)
		if len(relNames) == 0 {
			continue
		}

		rootShape, ok := shapes.Types[typeID]
		if !ok {
			return nil, fmt.Errorf("no shape for type %s", typeID)
		}
		partLabels := make([]string, len(partIDs))
		for i, id := range partIDs {
			partShape, ok := shapes.Types[id]
			if !ok {
				return nil, fmt.Errorf("no shape for part type %s (composed under %s)", id, typeID)
			}
			partLabels[i] = partShape.Label
		}

		instances := result.InstancesOf(typeID)
		var replaceRows []map[string]any
		for _, inst := range instances {
			keyProps, err := extractKeyProps(inst.Properties(), &rootShape)
			if err != nil {
				return nil, fmt.Errorf("type %s: %w", typeID, err)
			}
			row := make(map[string]any, len(keyProps))
			for k, v := range keyProps {
				row[batchKeyParamPrefix+k] = v
			}
			replaceRows = append(replaceRows, row)

			// Descend the instance tree collecting create rows.
			if err := collectCreateRows(s, shapes, &rootShape, rootType, inst, keyProps, groups); err != nil {
				return nil, err
			}
		}

		stmt := buildCompositionReplaceQuery(rootShape.Label, rootShape.PrimaryKeys, relNames, partLabels)
		for _, chunk := range chunkSlice(replaceRows, cfg.nodeChunkSize) {
			replace = append(replace, &BatchNodeQuery{
				Statement: stmt,
				Params:    map[string]any{"rows": chunk},
				Kind:      CompositionReplace,
			})
		}
	}

	// Deterministic create order: depth ascending (a parent exists before
	// its children), then by group identity.
	keys := make([]composedGroupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b composedGroupKey) int {
		if v := cmp.Compare(a.depth, b.depth); v != 0 {
			return v
		}
		if v := cmp.Compare(a.parentLabel, b.parentLabel); v != 0 {
			return v
		}
		if v := cmp.Compare(a.relName, b.relName); v != 0 {
			return v
		}
		return cmp.Compare(a.partLabel, b.partLabel)
	})

	var creates []*BatchNodeQuery
	for _, k := range keys {
		g := groups[k]
		var parentKeys []string
		if !k.parentByCK {
			// Depth-1 groups share the root type's key names; they rode in
			// on the rows, so recover them from the group's parent shape by
			// label. The row keys are already coerced.
			pk, ok := g.rows[0]["_parent_keys"].([]string)
			if !ok {
				return nil, fmt.Errorf("depth-1 create group %s/%s lost its parent key names", k.parentLabel, k.relName)
			}
			parentKeys = pk
		}
		stmt := buildCompositionCreateQuery(k.parentLabel, parentKeys, k.relName, k.partLabel, k.parentByCK)
		for _, chunk := range chunkSlice(g.rows, cfg.nodeChunkSize) {
			// The bookkeeping entry never reaches the driver.
			for _, row := range chunk {
				delete(row, "_parent_keys")
			}
			creates = append(creates, &BatchNodeQuery{
				Statement: stmt,
				Params:    map[string]any{"rows": chunk},
				Kind:      CompositionCreate,
			})
		}
	}

	return append(replace, creates...), nil
}

// collectCreateRows walks one root instance's composition tree, appending a
// create row per child into its (depth, parent, relation, part) group.
func collectCreateRows(
	s *schema.Schema,
	shapes *GraphShape,
	rootShape *NodeShape,
	rootType *schema.Type,
	root *graph.Instance,
	rootKeyProps map[string]any,
	groups map[composedGroupKey]*createGroup,
) error {
	rootKeyValues := root.PrimaryKey().Clone()

	var walk func(parent *graph.Instance, parentType *schema.Type, parentCK string, depth int) error
	walk = func(parent *graph.Instance, parentType *schema.Type, parentCK string, depth int) error {
		for _, relName := range parent.ComposedRelations() {
			rel, ok := parentType.Relation(relName)
			if !ok || rel.Kind() != schema.RelationComposition {
				return fmt.Errorf("instance of %s carries composed children under %q, which the schema does not declare as a composition", parentType.Name(), relName)
			}
			partType, ok := s.TypeByID(rel.TargetID())
			if !ok {
				return fmt.Errorf("composition %q: target %s not in schema", relName, rel.TargetID())
			}
			partShape, ok := shapes.Types[rel.TargetID()]
			if !ok {
				return fmt.Errorf("no shape for part type %s", rel.TargetID())
			}

			children := parent.Composed(relName)
			for i, child := range children {
				// A keyed child's composed key carries its own key values;
				// a keyless child's is positional — documented as NOT a
				// stable identity across writes, which replace semantics
				// make safe.
				var keyOrIndex any
				if partType.HasPrimaryKey() && child.PrimaryKey().Len() > 0 {
					keyOrIndex = child.PrimaryKey().Clone()
				} else {
					keyOrIndex = i
				}
				parentKeyValues := rootKeyValues
				if depth > 1 {
					parentKeyValues = []any{parentCK}
				}
				ck, err := graph.FormatComposedKey(parentKeyValues, relName, keyOrIndex)
				if err != nil {
					return fmt.Errorf("composition %q child %d: %w", relName, i, err)
				}

				props, err := propsToParamMap(child.Properties(), partType)
				if err != nil {
					return fmt.Errorf("part %s: %w", partShape.Label, err)
				}
				props[composedKeyProp] = ck

				row := map[string]any{"props": props}
				key := composedGroupKey{
					depth:     depth,
					relName:   relName,
					partLabel: partShape.Label,
				}
				if depth == 1 {
					key.parentLabel = rootShape.Label
					for k, v := range rootKeyProps {
						row[batchKeyParamPrefix+k] = v
					}
					row["_parent_keys"] = rootShape.PrimaryKeys
				} else {
					key.parentLabel = shapes.Types[parent.TypeID()].Label
					key.parentByCK = true
					row["parent_ck"] = parentCK
				}

				g := groups[key]
				if g == nil {
					g = &createGroup{key: key}
					groups[key] = g
				}
				g.rows = append(g.rows, row)

				if err := walk(child, partType, ck, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root, rootType, "", 1)
}

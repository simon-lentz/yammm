package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// existenceV3 tracks which root instances a v3 document holds, keyed by table
// row rather than by name: the table is the document's own denotation, so a
// reference validates against it without re-deriving a name.
type existenceV3 map[int]map[string]bool

// composedCoord locates a composed child by its parent's coordinates. Composed
// children never enter the instances section, so a reference to one — today
// only a duplicate record's — resolves through this index instead.
type composedCoord struct {
	parentType int
	parentKey  string
	relation   string
	childKey   string
}

// edgeRefV3 is a lightweight edge reference for structural validation.
type edgeRefV3 struct {
	sourceType int
	sourceKey  string
	targetType int
	targetKey  string
}

// isV3 reports whether the decoded header names a version carrying the types
// table. Every fork in this package turns on it.
func (sd *streamDecoder) isV3() bool {
	return sd.header.Version >= 3
}

// typeTags renders the document's types as the names it uses for them, for the
// public Info and HeaderOnly surfaces. A v3 table carries each tag verbatim.
func (sd *streamDecoder) typeTags() []string {
	if !sd.isV3() {
		return sd.types
	}
	tags := make([]string, len(sd.typeTable))
	for i, e := range sd.typeTable {
		tags[i] = e.Tag
	}
	return tags
}

// resolveTypeTable binds each table row to a schema type. A row whose schema
// path no longer resolves falls back to its name, which is the shape a document
// takes after its schema moved; a row that resolves to nothing is reported and
// left zero.
func (sd *streamDecoder) resolveTypeTable() {
	sd.tableIDs = make([]schema.TypeID, len(sd.typeTable))
	if sd.schema == nil {
		return
	}
	for i, e := range sd.typeTable {
		wire := &typeIDWire{SchemaPath: e.SchemaPath, Name: e.Name}
		if t, ok := typeByWireID(sd.schema, wire); ok {
			sd.tableIDs[i] = t.ID()
			continue
		}
		if t, ok := resolveWireType(sd.schema, e.Name, schema.TypeID{}); ok {
			sd.tableIDs[i] = t.ID()
			continue
		}
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_UNKNOWN_TYPE,
			fmt.Sprintf("types table entry %d names %q in schema %q, which the import closure does not declare",
				i, e.Name, e.SchemaPath)).
			WithDetail(diag.DetailKeyTypeName, e.Name).
			Build())
	}
}

// typeIDAt returns the identity bound to a table row, reporting an index the
// table does not hold.
func (sd *streamDecoder) typeIDAt(row int, position string) (schema.TypeID, bool) {
	if row < 0 || row >= len(sd.tableIDs) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("%s references types table row %d, which holds %d rows",
				position, row, len(sd.tableIDs))).Build())
		return schema.TypeID{}, false
	}
	id := sd.tableIDs[row]
	if id.IsZero() {
		return schema.TypeID{}, false
	}
	return id, true
}

// tagAt renders a table row as the name the document uses for it, for
// diagnostic messages.
func (sd *streamDecoder) tagAt(row int) string {
	if row < 0 || row >= len(sd.typeTable) {
		return "types[" + strconv.Itoa(row) + "]"
	}
	return sd.typeTable[row].Tag
}

// decodeSectionsV3 decodes the instances and diagnostics sections of a v3
// document by re-scanning the raw data.
func (sd *streamDecoder) decodeSectionsV3() ([][]instWireV3, diagWireV3, error) {
	dec := json.NewDecoder(bytes.NewReader(sd.data))
	dec.UseNumber()

	if _, err := dec.Token(); err != nil {
		return nil, diagWireV3{}, fmt.Errorf("expected opening brace: %w", err)
	}

	var instances [][]instWireV3
	var diags diagWireV3
	diagsFound := false

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, diagWireV3{}, fmt.Errorf("expected key token: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			continue
		}

		switch key {
		case "instances":
			if err := dec.Decode(&instances); err != nil {
				return nil, diagWireV3{}, fmt.Errorf("failed to decode instances: %w", err)
			}
		case "diagnostics":
			if err := dec.Decode(&diags); err != nil {
				return nil, diagWireV3{}, fmt.Errorf("failed to decode diagnostics: %w", err)
			}
			diagsFound = true
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, diagWireV3{}, fmt.Errorf("failed to skip key %q: %w", key, err)
			}
		}
	}

	if instances == nil {
		instances = [][]instWireV3{}
	}
	if !diagsFound {
		diags = diagWireV3{Duplicates: []dupWireV3{}, Unresolved: []unresolvedWireV3{}}
	}

	return instances, diags, nil
}

// checkSectionLength verifies the instances section is parallel to the types
// table. The two sections denote the same rows, so the consistency check that a
// name-keyed section needed is a length comparison here.
func (sd *streamDecoder) checkSectionLength(sections [][]instWireV3) {
	if len(sections) == len(sd.typeTable) {
		return
	}
	sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_TYPE_MISMATCH,
		fmt.Sprintf("instances section holds %d entries but the types table holds %d",
			len(sections), len(sd.typeTable))).Build())
}

// processInstanceV3 validates one instance and calls emit on success. row is
// the instance's own table row.
func (sd *streamDecoder) processInstanceV3(
	row int,
	inst instWireV3,
	depth int,
	parent *composedCoord,
	composed map[composedCoord]bool,
	emit func(row int, inst instWireV3),
) {
	if depth > maxComposedDepth {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DEPTH_EXCEEDED,
			fmt.Sprintf("composed nesting depth %d exceeds limit %d", depth, maxComposedDepth)).
			WithDetail(diag.DetailKeyDepth, strconv.Itoa(depth)).
			WithDetail(diag.DetailKeyTypeName, sd.tagAt(row)).
			Build())
		return
	}

	if depth > 0 && len(inst.Edges) > 0 {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_INVALID_COMPOSED,
			fmt.Sprintf("composed child %s has edges (composed children must not carry edges)", sd.tagAt(row))).
			WithDetail(diag.DetailKeyTypeName, sd.tagAt(row)).
			Build())
	}

	if inst.Provenance != nil && inst.Provenance.Path != "" {
		if _, err := path.Parse(inst.Provenance.Path); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Warning, diag.E_SNAPSHOT_PATH_FALLBACK,
				fmt.Sprintf("provenance path %q could not be parsed, falling back to root path", inst.Provenance.Path)).
				WithDetail(diag.DetailKeyOriginalPath, inst.Provenance.Path).
				WithDetail(diag.DetailKeyTypeName, sd.tagAt(row)).
				Build())
		}
	}

	if parent != nil && composed != nil {
		composed[*parent] = true
	}

	for relName, children := range inst.Composed {
		for _, child := range children {
			childRow, ok := sd.composedChildRow(child, relName, row)
			if !ok {
				continue
			}
			coord := composedCoord{
				parentType: row,
				parentKey:  formatWireKey(inst.Key),
				relation:   relName,
				childKey:   formatWireKey(child.Key),
			}
			// Composed children are materialized through their parent, so they
			// are validated here but never emitted as root instances.
			sd.processInstanceV3(childRow, child, depth+1, &coord, composed, nil)
		}
	}

	if emit != nil {
		emit(row, inst)
	}
}

// composedChildRow reads a composed child's declared table row. A v3 writer
// emits one at every non-root position, so its absence is a malformed document
// rather than a value to recover from context.
func (sd *streamDecoder) composedChildRow(child instWireV3, relName string, parentRow int) (int, bool) {
	if child.Type == nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("composed child under %s.%s carries no type index", sd.tagAt(parentRow), relName)).
			WithDetail(diag.DetailKeyRelationName, relName).
			Build())
		return 0, false
	}
	if *child.Type < 0 || *child.Type >= len(sd.typeTable) {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED,
			fmt.Sprintf("composed child under %s.%s references types table row %d, which holds %d rows",
				sd.tagAt(parentRow), relName, *child.Type, len(sd.typeTable))).
			WithDetail(diag.DetailKeyRelationName, relName).
			Build())
		return 0, false
	}
	return *child.Type, true
}

// validateInstancesV3 performs the lightweight validation Verify needs.
func (sd *streamDecoder) validateInstancesV3(
	ctx context.Context,
	sections [][]instWireV3,
) (existenceV3, map[composedCoord]bool, []edgeRefV3, error) {
	exists := make(existenceV3)
	composed := make(map[composedCoord]bool)
	var refs []edgeRefV3

	sd.checkSectionLength(sections)

	for row, insts := range sections {
		if err := ctx.Err(); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, nil, nil, fmt.Errorf("context cancelled: %w", err)
		}
		if exists[row] == nil {
			exists[row] = make(map[string]bool, len(insts))
		}
		for _, inst := range insts {
			sd.processInstanceV3(row, inst, 0, nil, composed, func(emitRow int, emitInst instWireV3) {
				keyStr := formatWireKey(emitInst.Key)
				if exists[emitRow] == nil {
					exists[emitRow] = make(map[string]bool)
				}
				exists[emitRow][keyStr] = true
				for _, targets := range emitInst.Edges {
					for _, et := range targets {
						refs = append(refs, edgeRefV3{
							sourceType: emitRow,
							sourceKey:  keyStr,
							targetType: et.TargetType,
							targetKey:  formatWireKey(et.TargetKey),
						})
					}
				}
			})
		}
	}

	return exists, composed, refs, nil
}

// decodeInstancesV3 performs the full materialization Load needs.
func (sd *streamDecoder) decodeInstancesV3(
	ctx context.Context,
	sections [][]instWireV3,
) (existenceV3, map[composedCoord]bool, map[schema.TypeID][]graph.InstanceParts, []graph.EdgeParts, []edgeRefV3, error) {
	exists := make(existenceV3)
	composed := make(map[composedCoord]bool)
	parts := make(map[schema.TypeID][]graph.InstanceParts, len(sections))
	var edgeParts []graph.EdgeParts
	var refs []edgeRefV3

	sd.checkSectionLength(sections)

	for row, insts := range sections {
		if err := ctx.Err(); err != nil {
			sd.collector.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
			return nil, nil, nil, nil, nil, fmt.Errorf("context cancelled: %w", err)
		}
		if exists[row] == nil {
			exists[row] = make(map[string]bool, len(insts))
		}
		for _, inst := range insts {
			sd.processInstanceV3(row, inst, 0, nil, composed, func(emitRow int, emitInst instWireV3) {
				typeID, ok := sd.typeIDAt(emitRow, "instance")
				if !ok {
					return
				}
				ip := sd.wireToInstancePartsV3(emitRow, typeID, emitInst)
				parts[typeID] = append(parts[typeID], ip)

				keyStr := ip.PrimaryKey.String()
				if exists[emitRow] == nil {
					exists[emitRow] = make(map[string]bool)
				}
				exists[emitRow][keyStr] = true

				for relName, targets := range emitInst.Edges {
					for _, et := range targets {
						refs = append(refs, edgeRefV3{
							sourceType: emitRow,
							sourceKey:  keyStr,
							targetType: et.TargetType,
							targetKey:  formatWireKey(et.TargetKey),
						})
						targetID, ok := sd.typeIDAt(et.TargetType, "edge target")
						if !ok {
							continue
						}
						edgeParts = append(edgeParts, graph.EdgeParts{
							Relation:   relName,
							SourceType: typeID,
							SourceKey:  ip.PrimaryKey,
							TargetType: targetID,
							TargetKey:  immutable.WrapKey(normalizeSlice(et.TargetKey)),
							Properties: immutable.WrapProperties(normalizeMap(et.Properties)),
						})
					}
				}
			})
		}
	}

	return exists, composed, parts, edgeParts, refs, nil
}

// countInstancesV3 token-scans the instances section for Info. Counts
// accumulate per tag rather than overwrite, because two table rows can render
// the same name.
func (sd *streamDecoder) countInstancesV3(sections [][]instWireV3) (map[string]int, int) {
	counts := make(map[string]int, len(sections))
	totalEdges := 0

	for row, insts := range sections {
		counts[sd.tagAt(row)] += len(insts)
		for _, inst := range insts {
			for _, targets := range inst.Edges {
				totalEdges += len(targets)
			}
		}
	}

	return counts, totalEdges
}

// wireToInstancePartsV3 converts a wire instance to InstanceParts.
func (sd *streamDecoder) wireToInstancePartsV3(
	row int,
	typeID schema.TypeID,
	inst instWireV3,
) graph.InstanceParts {
	ip := graph.InstanceParts{
		TypeName:   sd.tagAt(row),
		TypeID:     typeID,
		PrimaryKey: immutable.WrapKey(normalizeSlice(inst.Key)),
		Properties: immutable.WrapProperties(normalizeMap(inst.Properties)),
	}

	if len(inst.Composed) > 0 {
		ip.Composed = make(map[string][]graph.InstanceParts, len(inst.Composed))
		for relName, children := range inst.Composed {
			childParts := make([]graph.InstanceParts, 0, len(children))
			for _, child := range children {
				childRow, ok := sd.composedChildRow(child, relName, row)
				if !ok {
					continue
				}
				childID, ok := sd.typeIDAt(childRow, "composed child")
				if !ok {
					continue
				}
				childParts = append(childParts, sd.wireToInstancePartsV3(childRow, childID, child))
			}
			ip.Composed[relName] = childParts
		}
	}

	if inst.Provenance != nil {
		parsedPath, parseErr := path.Parse(inst.Provenance.Path)
		if parseErr != nil {
			parsedPath = path.Root()
		}
		prov := location.NewProvenance(inst.Provenance.SourceName, parsedPath, location.Span{})
		if parseErr != nil {
			prov = prov.WithRawPath(inst.Provenance.Path)
		}
		ip.Provenance = prov
	}

	return ip
}

// validateDiagnosticsV3 validates duplicate and unresolved records.
func (sd *streamDecoder) validateDiagnosticsV3(
	diags diagWireV3,
	exists existenceV3,
	composed map[composedCoord]bool,
) {
	for _, dup := range diags.Duplicates {
		keyStr := formatWireKey(dup.Key)
		tag := sd.tagAt(dup.Type)

		if !sd.duplicateConflictExists(dup, keyStr, exists, composed) {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
				fmt.Sprintf("duplicate conflict for %s[%s] references non-existent instance", tag, keyStr)).
				WithDetail(diag.DetailKeyTypeName, tag).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}

		if len(dup.Instance.Composed) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have composed children", tag, keyStr)).
				WithDetail(diag.DetailKeyTypeName, tag).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}

		if len(dup.Instance.Edges) > 0 {
			sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_EDGES_ON_DUPLICATE,
				fmt.Sprintf("duplicate instance %s[%s] must not have edges", tag, keyStr)).
				WithDetail(diag.DetailKeyTypeName, tag).
				WithDetail(diag.DetailKeyPrimaryKey, keyStr).
				Build())
		}
	}
}

// duplicateConflictExists reports whether the instance a duplicate collided
// with is present. A composed-child duplicate carries its parent's coordinates
// because the child it names is not in the instances section.
func (sd *streamDecoder) duplicateConflictExists(
	dup dupWireV3,
	keyStr string,
	exists existenceV3,
	composed map[composedCoord]bool,
) bool {
	if dup.Relation == "" || dup.ParentType == nil {
		return exists[dup.Type][keyStr]
	}
	return composed[composedCoord{
		parentType: *dup.ParentType,
		parentKey:  formatWireKey(dup.ParentKey),
		relation:   dup.Relation,
		childKey:   keyStr,
	}]
}

// validateEdgeRefsV3 checks that all edge target references resolve.
func (sd *streamDecoder) validateEdgeRefsV3(refs []edgeRefV3, exists existenceV3) {
	for _, ref := range refs {
		if exists[ref.targetType][ref.targetKey] {
			continue
		}
		sourceTag := sd.tagAt(ref.sourceType)
		targetTag := sd.tagAt(ref.targetType)
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_DANGLING_REFERENCE,
			fmt.Sprintf("edge in %s[%s] references %s[%s] which does not exist",
				sourceTag, ref.sourceKey, targetTag, ref.targetKey)).
			WithDetail(diag.DetailKeyTypeName, sourceTag).
			WithDetail(diag.DetailKeyPrimaryKey, ref.sourceKey).
			WithDetail(diag.DetailKeyTargetType, targetTag).
			WithDetail(diag.DetailKeyTargetPK, ref.targetKey).
			WithHint("ensure the target instance is included in the snapshot").
			Build())
	}
}

package snapshot

import (
	"cmp"
	"context"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// loadV3 materializes a v3 document. Every type reference is a table row, so
// nothing here re-derives an identity from a name.
func loadV3(ctx context.Context, sd *streamDecoder, s *schema.Schema) (*graph.Snapshot, diag.Result) {
	sections, diags, err := sd.decodeSectionsV3()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}

	exists, composed, instParts, edgeParts, refs, err := sd.decodeInstancesV3(ctx, sections)
	if err != nil {
		return nil, sd.collector.Result()
	}

	sd.validateDiagnosticsV3(diags, exists, composed)
	sd.verifyIntegrity()
	sd.validateEdgeRefsV3(refs, exists)

	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	dupParts := make([]graph.DuplicateParts, 0, len(diags.Duplicates))
	for _, dw := range diags.Duplicates {
		typeID, ok := sd.typeIDAt(dw.Type, "duplicate")
		if !ok {
			continue
		}
		dp := graph.DuplicateParts{
			Type:     typeID,
			Key:      immutable.WrapKey(normalizeSlice(dw.Key)),
			Instance: sd.wireToInstancePartsV3(dw.Type, typeID, dw.Instance),
		}
		if dw.Relation != "" && dw.ParentType != nil {
			parentID, ok := sd.typeIDAt(*dw.ParentType, "duplicate parent")
			if !ok {
				continue
			}
			dp.ParentType = parentID
			dp.ParentKey = immutable.WrapKey(normalizeSlice(dw.ParentKey))
			dp.Relation = dw.Relation
		}
		dupParts = append(dupParts, dp)
	}

	unresParts := make([]graph.UnresolvedParts, 0, len(diags.Unresolved))
	for _, uw := range diags.Unresolved {
		sourceID, ok := sd.typeIDAt(uw.SourceType, "unresolved source")
		if !ok {
			continue
		}
		targetID, ok := sd.typeIDAt(uw.TargetType, "unresolved target")
		if !ok {
			continue
		}
		up := graph.UnresolvedParts{
			SourceType: sourceID,
			SourceKey:  immutable.WrapKey(normalizeSlice(uw.SourceKey)),
			Relation:   uw.Relation,
			TargetType: targetID,
			Required:   uw.Required,
			Reason:     uw.Reason,
			Properties: immutable.WrapProperties(normalizeMap(uw.Properties)),
		}
		if uw.TargetKey != nil {
			up.TargetKey = immutable.WrapKey(normalizeSlice(uw.TargetKey))
		}
		unresParts = append(unresParts, up)
	}

	typeIDs := slices.SortedFunc(maps.Keys(instParts), func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})

	parts := graph.SnapshotParts{
		Types:      typeIDs,
		Instances:  instParts,
		Edges:      edgeParts,
		Duplicates: dupParts,
		Unresolved: unresParts,
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		sd.collector.Merge(result)
		return nil, sd.collector.Result()
	}

	return snap, sd.collector.Result()
}

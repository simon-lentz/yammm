package snapshot

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Load deserializes a previously-saved snapshot from the yammm snapshot
// persistence format (.ys).
//
// Load follows the library's standard diagnostic pattern: it returns
// (T, diag.Result) where Result carries structured issues with stable codes.
// On success, the Result is OK (or contains only warnings). On failure,
// the Result has Error-severity issues and the snapshot is nil.
//
// The schema parameter must be the same schema (or a compatible schema)
// used when the data was originally validated. Load verifies compatibility
// using [schema.StructuralHash].
//
// Load does not re-validate instance data. The persisted snapshot is
// assumed to contain valid data. However, Load performs structural
// validation of the .ys format itself.
//
// The returned Snapshot's Schema() method returns the schema provided to
// Load, not the schema used at original construction time.
//
// The returned Snapshot's Diagnostics() method returns diag.OK() —
// construction diagnostics are transient and not persisted.
//
// Panics if s is nil (programming error).
func Load(ctx context.Context, data []byte, s *schema.Schema, opts ...LoadOption) (*graph.Snapshot, diag.Result) {
	if s == nil {
		panic("snapshot.Load: nil Schema")
	}
	if err := ctx.Err(); err != nil {
		c := diag.NewCollector(0)
		c.Collect(diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error()).Build())
		return nil, c.Result()
	}

	cfg := applyLoadOptions(opts)
	sd := newStreamDecoder(data, s, cfg)

	// Steps 1-3: Decode and validate header, types, schema hash.
	if err := sd.decodeHeader(); err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	// Decode remaining sections (instances + diagnostics).
	instances, diags, err := sd.decodeSections()
	if err != nil {
		sd.collector.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, err.Error()).Build())
		return nil, sd.collector.Result()
	}

	// Step 4: Decode instances (full materialization — decodeInstances).
	exists, instParts, edgeParts, err := sd.decodeInstances(ctx, instances)
	if err != nil {
		return nil, sd.collector.Result()
	}

	// Step 5: Validate diagnostics section.
	sd.validateDiagnostics(diags, exists)

	// Step 6: Integrity verification.
	sd.verifyIntegrity()

	// Step 7: Structural validation — edge references.
	refs := make([]edgeRef, 0, len(edgeParts))
	for _, ep := range edgeParts {
		refs = append(refs, edgeRef{
			sourceType: ep.SourceType,
			sourceKey:  ep.SourceKey.String(),
			targetType: ep.TargetType,
			targetKey:  ep.TargetKey.String(),
		})
	}
	sd.validateEdgeRefs(refs, exists)

	// If any errors, return nil snapshot.
	if sd.collector.HasErrors() {
		return nil, sd.collector.Result()
	}

	// Step 8: Assemble SnapshotParts and delegate to RebuildSnapshot.
	dupParts := make([]graph.DuplicateParts, 0, len(diags.Duplicates))
	for _, dw := range diags.Duplicates {
		var dupTypeID schema.TypeID
		if st, ok := s.Type(dw.Type); ok {
			dupTypeID = st.ID()
		}
		dp := graph.DuplicateParts{
			Type:     dw.Type,
			Key:      immutable.WrapKey(normalizeSlice(dw.Key)),
			Instance: sd.wireToInstanceParts(dw.Type, dw.Instance, dupTypeID),
		}
		dupParts = append(dupParts, dp)
	}

	unresParts := make([]graph.UnresolvedParts, 0, len(diags.Unresolved))
	for _, uw := range diags.Unresolved {
		up := graph.UnresolvedParts{
			SourceType: uw.SourceType,
			SourceKey:  immutable.WrapKey(normalizeSlice(uw.SourceKey)),
			Relation:   uw.Relation,
			TargetType: uw.TargetType,
			Required:   uw.Required,
			Reason:     uw.Reason,
		}
		if uw.TargetKey != nil {
			up.TargetKey = immutable.WrapKey(normalizeSlice(uw.TargetKey))
		}
		unresParts = append(unresParts, up)
	}

	parts := graph.SnapshotParts{
		Types:      sd.types,
		Instances:  instParts,
		Edges:      edgeParts,
		Duplicates: dupParts,
		Unresolved: unresParts,
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		// Merge RebuildSnapshot diagnostics (Fatal E_INTERNAL) into our collector.
		sd.collector.Merge(result)
		return nil, sd.collector.Result()
	}

	return snap, sd.collector.Result()
}

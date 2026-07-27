package neo4j

// KeyMutability selects the SET-clause shape emitted by
// [BuildNodeMergeQuery] and [BuildBatchNodeMergeQuery]. The enum replaces
// an earlier trailing bool parameter so call sites read as
// "this is the immutable-key variant" rather than "the third bool is true
// here"; it matches the always-on RETURN design choice on the relationship
// builders — neither API carries a flag parameter that conditionally
// changes output shape.
//
// Layering. KeyMutability is a per-call query-shape selector. The
// effective immutable-key set — [WithImmutableKeys] unioned with a
// type's derived @writeOnce keys — carries the *property-name filter*
// that feeds the `update_props` parameter at write time; the two are
// complementary, not overlapping. [Adapter.NodeQueryFor] and
// [Adapter.BatchNodeQueries] derive the enum from whether that
// effective set is non-empty (per type, for the batch path) so the
// pair stays consistent at the public-wrapper layer. Direct
// [BuildNodeMergeQuery] /
// [BuildBatchNodeMergeQuery] callers are responsible for supplying the
// matching parameter map themselves: [ImmutableKeys] requires the
// caller to pass both `$props` and `$update_props`; [MutableKeys]
// requires only `$props`.
type KeyMutability int

const (
	// MutableKeys declares primary-key fields and properties can be
	// rewritten on subsequent MERGE calls. Generates a single
	// `SET n += $props` (or `SET n += row.props` for the batch form).
	MutableKeys KeyMutability = iota

	// ImmutableKeys declares primary-key fields and every property in the
	// EFFECTIVE immutable set — the keys passed to [WithImmutableKeys]
	// unioned with the type's schema-derived @writeOnce keys (see
	// [ImmutableKeysFor] and [NodeShape.ImmutableKeys]) — are set once at
	// node creation and must not be rewritten on MATCH. A caller building
	// `$update_props` from the [WithImmutableKeys] list alone rewrites every
	// @writeOnce property on each MERGE, silently defeating the guarantee
	// the annotation exists to provide. Generates the split form:
	// `ON CREATE SET n += $props` + `ON MATCH SET n += $update_props`
	// (or the `row.props` / `row.update_props` batch variants).
	//
	// Callers using this shape must provide `$update_props` (or
	// `row.update_props` on each batch row) in their parameter map
	// or Neo4j will raise `Parameter $update_props was not provided`
	// at execute time.
	ImmutableKeys
)

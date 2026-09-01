package neo4j

// keyMutability selects the SET-clause shape emitted by the node-merge
// templates.
//
// Unexported: no exported function in the default build accepts or returns one,
// so an exported selector would be surface a caller cannot reach. The
// server-backed harness gets it through the build-tagged alias in
// integration_export.go.
//
// Layering. keyMutability is a per-call query-shape selector. A type's
// derived @writeOnce key set carries the *property-name filter* that feeds
// the `update_props` parameter at write time; the two are complementary, not
// overlapping. [Adapter.BatchNodeQueries] derives the enum per type from
// whether that set is non-empty, so the pair stays consistent at the
// public-wrapper layer. [immutableKeys] requires both `$props` and
// `$update_props` in the parameter map; [mutableKeys] requires only
// `$props`.
type keyMutability int

const (
	// mutableKeys declares primary-key fields and properties can be
	// rewritten on subsequent MERGE calls. Generates a single
	// `SET n += $props` (or `SET n += row.props` for the batch form).
	mutableKeys keyMutability = iota

	// immutableKeys declares primary-key fields and every property in the
	// type's schema-derived @writeOnce set (see [ImmutableKeysFor] and
	// [NodeShape.ImmutableKeys]) are set once at node creation and must not
	// be rewritten on MATCH. A caller building `$update_props` from any
	// narrower list rewrites the @writeOnce properties it omits on each
	// MERGE, silently defeating the guarantee the annotation exists to
	// provide. Generates the split form:
	// `ON CREATE SET n += $props` + `ON MATCH SET n += $update_props`
	// (or the `row.props` / `row.update_props` batch variants).
	//
	// Callers using this shape must provide `$update_props`, or
	// `row.update_props` on each batch row. The two omissions fail
	// differently, measured on 5.26: a missing top-level parameter is
	// `Expected parameter(s): update_props`, while a row missing the key is
	// `Expected Property(...update_props) to be a map, but it was NO_VALUE`.
	// The row case surfaces only once the node EXISTS, because it is the ON
	// MATCH branch that reads the key — a first write of a batch passes and
	// the same batch replayed fails.
	immutableKeys
)

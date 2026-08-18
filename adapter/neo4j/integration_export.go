//go:build neo4j_integration

package neo4j

// The server-backed integration tests live in a separate package and drive the
// raw Cypher templates against a real Neo4j; this build exports the builders
// to them without widening the normal build's API surface.
var (
	BuildNodeMergeQuery      = buildNodeMergeQuery
	BuildBatchNodeMergeQuery = buildBatchNodeMergeQuery
)

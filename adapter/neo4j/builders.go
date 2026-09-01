package neo4j

import (
	"fmt"
	"strings"
)

// batchKeyParamPrefix namespaces MERGE-key entries away from the `props` and
// `update_props` entries that share their map. Both builders and both parameter
// assemblers spell it through this constant, so the two halves of each wire
// contract cannot drift apart.
const batchKeyParamPrefix = "key_"

// The single-relationship endpoint-key prefixes. The builder and its parameter
// assembler spell them through these constants for the same reason
// [batchKeyParamPrefix] exists: a template reading a key the assembler never
// writes yields a MATCH against null, which merges nothing and reports no error.
//
// The single and batch contracts genuinely differ — `$from_key_<name>` against
// `row.from_<name>` — because they are separate published parameter shapes, not
// because one of them is stale. Nothing in an edge row can collide with an
// endpoint key (a row holds only the two key sets and `rel_props`), so the batch
// form has no reason to carry the extra segment. These two stay unexported: the
// single-relationship template is internal, so nothing outside assembles its
// params.
const (
	relFromKeyParamPrefix = "from_key_"
	relToKeyParamPrefix   = "to_key_"
)

// The row-key prefixes of [BuildBatchRelationshipMergeQuery]'s `$rows`
// parameter. Each row carries the source endpoint's key properties under
// RelFromRowPrefix and the target's under RelToRowPrefix:
//
//	{rows: [{from_<name>: v, to_<name>: v, [rel_props: map]}, ...]}
//
// They are exported because that shape is the builder's contract in all but
// name: a consumer holding the template has to assemble rows that match it, and
// a prefix it spells as a literal is a prefix nothing keeps in step. A
// disagreement is silent — the MATCH binds null, merges nothing, and reports no
// error.
const (
	RelFromRowPrefix = "from_"
	RelToRowPrefix   = "to_"
)

// buildBatchNodeMergeQuery returns the UNWIND-batched variant of
// the single-node form removed with the class-D cut. Parameter shape:
//
//	{rows: [{key_<key_1>: v, props: map, [update_props: map]}, ...]}
//
// The `update_props` entry per row is required when keys == [immutableKeys]
// and ignored when keys == [MutableKeys].
//
// Merge keys carry the same `key_` prefix the single-node builder gives its
// parameters ([batchKeyParamPrefix]), so a row's key entries occupy a namespace
// disjoint from `props` and `update_props`. That matters because `props` and
// `update_props` are themselves legal DSL property names: without the prefix a
// primary key so named would collide with the property map in the same row.
//
// Node batch merges do not emit a RETURN clause
// for the rationale.
func buildBatchNodeMergeQuery(label string, keyNames []string, keys keyMutability) string {
	var b strings.Builder
	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MERGE (n:")
	b.WriteString(label)
	b.WriteString(" {")
	for i, name := range keyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.%s%s", name, batchKeyParamPrefix, name)
	}
	b.WriteString("})")

	if keys == immutableKeys {
		b.WriteString("\nON CREATE SET n += row.props")
		b.WriteString("\nON MATCH SET n += row.update_props")
	} else {
		b.WriteString("\nSET n += row.props")
	}
	return b.String()
}

// buildRelationshipMergeQuery returns the Cypher template for a single
// relationship MERGE between nodes identified by their (label, primary-key)
// shapes. If hasProps is true, the generated query ends with
// `SET r += $rel_props` so callers can pass edge properties.
//
// The generated query always ends with `RETURN count(*) AS matched_rows`.
// Consumers implementing silent-failure detection on generated MERGEs
// (e.g., a link-engine safety net) sum the column across calls;
// consumers that don't care ignore it. Making the clause always-on keeps
// the signature free of a trailing `hasReturnCount` bool and the emitted
// output free of a flag-controlled shape branch.
//
// Return-value semantics (matched_rows). The returned column reflects
// this call's MERGE match count only: 0 if no MERGE executed (structural
// no-op — the silent-failure condition), 1 if the relationship exists
// after the call. Consumers issuing multiple calls (e.g., a loop over
// distinct (from, to) pairs) are responsible for summing the per-call
// values themselves. For UNWIND-batched workloads, prefer
// [BuildBatchRelationshipMergeQuery] and apply the same summing guidance
// across chunk transactions.
func buildRelationshipMergeQuery(
	fromLabel string, fromKeyNames []string,
	relType string,
	toLabel string, toKeyNames []string,
	hasProps bool,
) string {
	var b strings.Builder

	b.WriteString("MATCH (from:")
	b.WriteString(fromLabel)
	b.WriteString(" {")
	for i, name := range fromKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: $%s%s", name, relFromKeyParamPrefix, name)
	}
	b.WriteString("})\n")

	b.WriteString("MATCH (to:")
	b.WriteString(toLabel)
	b.WriteString(" {")
	for i, name := range toKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: $%s%s", name, relToKeyParamPrefix, name)
	}
	fmt.Fprintf(&b, "})\nMERGE (from)-[r:%s]->(to)", relType)

	if hasProps {
		b.WriteString("\nSET r += $rel_props")
	}
	b.WriteString("\nRETURN count(*) AS matched_rows")
	return b.String()
}

// BuildBatchRelationshipMergeQuery returns the Cypher template for an
// UNWIND-batched relationship MERGE between nodes identified by their
// (label, primary-key) shapes. If hasProps is true, the generated query ends
// with `SET r += row.rel_props` so callers can pass per-edge properties.
// Parameter shape:
//
//	{rows: [{from_<name>: v, to_<name>: v, [rel_props: map]}, ...]}
//
// The generated query always ends with `RETURN count(*) AS matched_rows`.
// Consumers implementing silent-failure detection on generated MERGEs sum the
// column; consumers that do not care ignore it. Making the clause always-on
// keeps the signature free of a trailing bool and the emitted output free of a
// flag-controlled shape branch.
//
// Return-value semantics (matched_rows). Neo4j's transactional semantics
// mean each chunk either fully commits (all matched_rows reflected in
// the returned value) or fully rolls back (no matched_rows, driver
// error surfaces to the caller). The returned column reflects the
// MERGE match count within this chunk's transaction only — it is not
// a session-spanning aggregate.
//
// Consumers that chunk a batch across multiple transactions (the common
// case — large batches are split to respect transaction-size limits) are
// responsible for summing matched_rows across chunk results. A single
// chunk's value reflects that chunk's commit only. The library returns
// a template; cross-chunk aggregation is the consumer's.
//
// The silent-failure detection pattern that motivates the always-on
// RETURN — "sum matched_rows across chunks; assert sum > 0 whenever at
// least one edge was submitted" — works correctly only when the
// consumer sums across chunks. A consumer that reads a single chunk's
// matched_rows as authoritative will false-positive "rule silently
// no-op'd" any time the first chunk legitimately has zero matches even
// though later chunks have nonzero matches.
func BuildBatchRelationshipMergeQuery(
	fromLabel string, fromKeyNames []string,
	relType string,
	toLabel string, toKeyNames []string,
	hasProps bool,
) string {
	var b strings.Builder

	b.WriteString("UNWIND $rows AS row\n")
	b.WriteString("MATCH (from:")
	b.WriteString(fromLabel)
	b.WriteString(" {")
	for i, name := range fromKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.%s%s", name, RelFromRowPrefix, name)
	}
	b.WriteString("})\n")

	b.WriteString("MATCH (to:")
	b.WriteString(toLabel)
	b.WriteString(" {")
	for i, name := range toKeyNames {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: row.%s%s", name, RelToRowPrefix, name)
	}
	fmt.Fprintf(&b, "})\nMERGE (from)-[r:%s]->(to)", relType)

	if hasProps {
		b.WriteString("\nSET r += row.rel_props")
	}
	b.WriteString("\nRETURN count(*) AS matched_rows")
	return b.String()
}

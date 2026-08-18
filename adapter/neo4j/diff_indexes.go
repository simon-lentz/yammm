package neo4j

import (
	"fmt"
	"strings"
)

// IndexDiffResult is the five-way classification of desired vs actual indexes.
// Anything not in Match is actionable; Unverified is neither actionable nor a
// clean bill of health, so a caller deciding "is this database in sync?" must
// consult it as well as Drift, Create, and Drop.
type IndexDiffResult struct {
	// Match contains indexes that exist in both schema and database with
	// identical semantic definitions.
	Match []IndexMatch

	// Drift contains indexes present in the database that do not serve what the
	// schema declares, and that a CREATE cannot fix. Four producers: a vector
	// index whose dimension or similarity differs; an index holding a desired
	// index's name under a different definition; one in a state that serves no
	// queries (FAILED); and a desired index whose name is held by a kind the DSL
	// cannot express. Each needs a drop before the declaration can be realised.
	Drift []IndexDrift

	// Create contains indexes defined in the schema that are absent from the
	// database.
	Create []Index

	// Drop contains schema-owned indexes in the database that have no
	// corresponding declaration in the schema AND that the schema could have
	// declared. A kind the DSL has no vocabulary for (TEXT, POINT), a
	// multi-label FULLTEXT index, a relationship index, and a constraint's
	// backing index are all left out even on an owned label: none is something
	// a schema edit could account for, so reporting them would be drift that
	// never clears. See [RemoteIndex.Declarable].
	Drop []RemoteIndex

	// Unverified contains indexes that exist in both by semantic key but whose
	// full definition could not be compared because the database did not report
	// the configuration needed. They are neither confirmed in sync nor confirmed
	// drifted; folding them into Match would report an unchecked index as
	// verified.
	Unverified []IndexUnverified

	// Excluded counts the remote indexes that entered no bucket at all, because
	// the comparison had nothing to say about them: the schema owns no label they
	// carry, or they are of a shape it cannot declare (TEXT, POINT, a
	// multi-label FULLTEXT, a relationship index, a constraint's backing
	// index). See [ConstraintDiffResult.Excluded], which carries the same
	// meaning and the same reason for existing.
	Excluded int
}

// IndexUnverified pairs a desired index with an actual index about which no
// in-sync claim can be made — its configuration could not be read, or it is
// still populating and does not serve queries yet. Reason says which.
type IndexUnverified struct {
	Desired Index
	Actual  RemoteIndex
	Reason  string // Human-readable description of what could not be verified
}

// IndexMatch pairs a desired index with its matching actual index.
type IndexMatch struct {
	Desired Index
	Actual  RemoteIndex
}

// IndexDrift pairs a desired index with an actual index that has the same
// semantic key but a different definition.
type IndexDrift struct {
	Desired Index
	Actual  RemoteIndex
	Reason  string // Human-readable drift description
}

// DiffIndexes performs a semantic diff between desired schema indexes and
// actual database indexes, classifying each into one of five categories.
//
// Only indexes on labels the schema owns are considered — see
// [Adapter.OwnedLabels], which the caller builds once and passes in. Desired
// indexes are matched to remote ones in three phases: name and identity
// together, then identity alone, then name alone; see [pairObjects] for why that
// order matters.
//
// Composite property order is significant, a deliberate divergence from
// [Adapter.DiffConstraints], which sorts properties: SHOW INDEXES reports
// properties in index-definition order, so the order-sensitive identity is
// sound. A same-set/different-order remote index is therefore a distinct index —
// Create + Drop when its name differs too, and Drift when it holds the desired
// index's name.
//
// A schema-owned remote index with no declaration is reported as a Drop — the
// drift the index feature exists to surface. Drops are reported, never applied.
//
// Index and constraint names share ONE namespace. A constraint backed by an
// index appears in SHOW INDEXES under the constraint's name and is seen here
// automatically; a constraint with NO backing index — NOT NULL and TYPE — never
// appears there at all, so a caller that introspected constraints passes them as
// alsoBlocking and a desired index whose name one of them holds is reported as
// drift naming the holder. Omitting them is safe but weaker: such an index then
// reports as a Create that IF NOT EXISTS turns into a silent no-op on every run.
func (a *Adapter) DiffIndexes(
	desired []Index,
	actual []RemoteIndex,
	owned *OwnedLabels,
	alsoBlocking ...RemoteConstraint,
) *IndexDiffResult {
	result := &IndexDiffResult{}

	// Blockers are collected from EVERY remote object, before ownership or
	// declarability filtering, because the conditions under which a CREATE
	// silently does nothing are not scoped the way ownership is:
	//
	//   - Index names are global. An index on a label this schema does not own,
	//     of a kind it cannot declare, still holds its name against every CREATE
	//     in the database — and a constraint's backing index holds the
	//     constraint's name, so constraint and index names share one namespace.
	//   - A (label, properties, kind) already served by an existing index is not
	//     created again under a second name, which is how a constraint's backing
	//     index absorbs a declared @@index over a primary key.
	byName := make(map[string]RemoteIndex, len(actual))
	byDefinition := make(map[string]RemoteIndex, len(actual))
	for _, ri := range actual {
		if ri.Name != "" {
			if _, taken := byName[ri.Name]; !taken {
				byName[ri.Name] = ri
			}
		}
		// Names are global, but definitions are not: node and relationship
		// indexes occupy separate namespaces, so a relationship index whose type
		// happens to be spelled like a node label does not stop the server from
		// creating that node index. Only node indexes can serve a node
		// definition. Type is upper-cased to meet [desiredIndexKey]'s canonical
		// spelling, which is what this map is looked up by.
		if ri.EntityType != "" && !strings.EqualFold(ri.EntityType, "NODE") {
			continue
		}
		// A multi-label node index serves no single-label declaration either:
		// the server creates the declaration beside it, so keying it here would
		// report a create the server actually performs as blocked. Its NAME
		// still blocks, through byName above. FULLTEXT is the only kind the
		// server creates multi-label today; the guard is on the count so that
		// stays true by construction rather than by enumeration.
		if len(ri.LabelsOrTypes) > 1 {
			continue
		}
		def := identityKey(append(
			[]string{firstLabel(ri.LabelsOrTypes), strings.ToUpper(ri.Type)}, ri.Properties...,
		)...)
		if _, taken := byDefinition[def]; !taken {
			byDefinition[def] = ri
		}
	}
	// A constraint with no backing index — NOT NULL and TYPE — appears in no
	// SHOW INDEXES row, so its name reaches this diff only through alsoBlocking.
	// It is folded into byName as a nameless-Type RemoteIndex: Type is what
	// [describeIndexBlocker] renders, and leaving it empty is what tells a
	// consumer the holder is a constraint rather than an index.
	blockingConstraints := make(map[string]RemoteConstraint, len(alsoBlocking))
	for _, rc := range alsoBlocking {
		if rc.Name == "" {
			continue
		}
		if _, taken := byName[rc.Name]; taken {
			continue
		}
		if _, taken := blockingConstraints[rc.Name]; !taken {
			blockingConstraints[rc.Name] = rc
		}
	}

	var ownedRemotes []scoped[RemoteIndex]
	for _, ri := range actual {
		label, isOwned := owned.ownedLabel(ri.LabelsOrTypes)
		if !isOwned || !ri.Declarable() {
			result.Excluded++
			continue
		}
		ownedRemotes = append(ownedRemotes, scopedObject(ri, label))
	}

	pairs, unmatched, orphans := pairObjects(desired, ownedRemotes, identityFuncs[Index, scoped[RemoteIndex]]{
		desiredName: func(d Index) string { return d.Name },
		actualName:  func(o scoped[RemoteIndex]) string { return o.obj.Name },
		desiredKey:  desiredIndexKey,
		actualKey:   remoteIndexKey,
	})

	for _, p := range pairs {
		d, ri := p.Desired, p.Actual.obj

		// Only a name-only pair can have disagreeing identities; the other two
		// phases match on identity by construction. The database already holds
		// the name, and every emitted statement carries IF NOT EXISTS, so a
		// CREATE would silently do nothing — the index must be dropped and
		// recreated.
		if p.byName {
			result.Drift = append(result.Drift, IndexDrift{
				Desired: d,
				Actual:  ri,
				Reason: fmt.Sprintf("index %q exists with a different definition: schema %s, database %s",
					ri.Name, describeIndexShape(d.Properties, indexKindToRemoteType(d.Kind)),
					describeIndexShape(ri.Properties, ri.Type)),
			})
			continue
		}

		// Drift before unverifiability, throughout: a fact the database
		// disclosed that disagrees with the declaration is definite and
		// actionable, and outranks anything the database would not disclose or
		// has not finished doing. A wrong vector dimension is wrong whether or
		// not the index has finished populating.
		if reason, drifted := vectorDrift(d, ri); drifted {
			result.Drift = append(result.Drift, IndexDrift{
				Desired: d,
				Actual:  ri,
				Reason:  reason,
			})
			continue
		}

		// An index that exists but does not serve queries is not in sync,
		// however exactly its definition matches.
		if reason, actionable := indexStateProblem(ri); reason != "" {
			result.append(actionable, d, ri, reason)
			continue
		}

		if reason, unverifiable := vectorConfigUnreadable(d, ri); unverifiable {
			result.Unverified = append(result.Unverified, IndexUnverified{
				Desired: d,
				Actual:  ri,
				Reason:  reason,
			})
			continue
		}

		result.Match = append(result.Match, IndexMatch{
			Desired: d,
			Actual:  ri,
		})
	}

	// Names pairing already handed to a declaration. A remote object here still
	// holds its name against every other CREATE, so it still blocks — but the
	// message must say the holder is this schema's own realisation of a different
	// declaration, or the report reads as if one object were both in sync and to
	// be dropped for no stated reason.
	claimed := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		if n := p.Actual.obj.Name; n != "" {
			claimed[n] = struct{}{}
		}
	}

	// A desired index the pairing did not realise is only a Create if the server
	// would actually create it. Where something already blocks the statement, the
	// CREATE is a silent no-op and reporting it produces a plan that repeats
	// unchanged forever, so it is drift naming what to remove first.
	for _, d := range unmatched {
		key := desiredIndexKey(d)

		// A definition already served by a constraint's backing index SATISFIES
		// the declaration rather than blocking it: the index exists and does the
		// work, it merely carries the constraint's name. Declaring @@index over a
		// primary key is legal in the DSL, so this is reachable from a valid
		// schema — and no action could clear it, since dropping the backing index
		// means dropping the primary key. Reporting drift here would make a
		// fully-applied schema diff dirty forever.
		//
		// The state check is the same one every paired index goes through: an
		// index that exists but does not serve queries is not in sync however
		// exactly it fits the declaration, and a backing index is no exception.
		if served, ok := byDefinition[key]; ok && served.OwningConstraint != "" {
			if reason, actionable := indexStateProblem(served); reason != "" {
				result.append(actionable, d, served, reason)
				continue
			}
			result.Match = append(result.Match, IndexMatch{Desired: d, Actual: served})
			continue
		}
		if blocker, blocked := indexCreateBlocker(d, key, byName, byDefinition); blocked {
			result.Drift = append(result.Drift, IndexDrift{
				Desired: d,
				Actual:  blocker,
				Reason:  describeIndexBlocker(d, blocker, claimed),
			})
			continue
		}
		// Only after the index namespace comes up empty: a constraint holding the
		// name is the rarer case, and a real index holding it is the more precise
		// finding.
		if rc, taken := blockingConstraints[d.Name]; taken && d.Name != "" {
			result.Drift = append(result.Drift, IndexDrift{
				Desired: d,
				Actual:  RemoteIndex{Name: rc.Name, LabelsOrTypes: rc.LabelsOrTypes, Properties: rc.Properties},
				Reason: fmt.Sprintf("name %q is already held by a %s constraint on %s; index and constraint names share one namespace, so drop or rename it before this index can be created",
					rc.Name, rc.Type, describeLabels(rc.LabelsOrTypes)),
			})
			continue
		}
		result.Create = append(result.Create, d)
	}
	for _, o := range orphans {
		result.Drop = append(result.Drop, o.obj)
	}

	return result
}

// indexStateProblem reports why a remote index does not serve queries, and
// whether that is actionable. FAILED is terminal — drop and recreate — so it is
// drift; POPULATING resolves on its own, so it is unverified rather than a
// change to apply. An empty reason means the index is serving.
func indexStateProblem(ri RemoteIndex) (reason string, actionable bool) {
	if ri.IsOnline() {
		return "", false
	}
	if strings.EqualFold(ri.State, "POPULATING") {
		return "index is still populating; it does not serve queries yet", false
	}
	return fmt.Sprintf("index %q is in state %s and does not serve queries; drop and recreate it",
		ri.Name, ri.State), true
}

// append files one desired/actual pair under Drift or Unverified, so the two
// call sites that classify an index state cannot disagree about which bucket a
// state belongs in.
func (r *IndexDiffResult) append(actionable bool, d Index, ri RemoteIndex, reason string) {
	if actionable {
		r.Drift = append(r.Drift, IndexDrift{Desired: d, Actual: ri, Reason: reason})
		return
	}
	r.Unverified = append(r.Unverified, IndexUnverified{Desired: d, Actual: ri, Reason: reason})
}

// describeIndexShape renders an index's members and kind for a drift message.
func describeIndexShape(props []string, remoteType string) string {
	return remoteType + "(" + strings.Join(props, ", ") + ")"
}

// desiredIndexKey builds the semantic identity of a desired Index. Properties
// are NOT sorted: composite index order is significant, so a reordered
// composite is a distinct index.
func desiredIndexKey(idx Index) string {
	return identityKey(append(
		[]string{idx.Label, indexKindToRemoteType(idx.Kind)}, idx.Properties...,
	)...)
}

// remoteIndexKey builds the semantic identity of a remote index from the label
// that made it schema-owned, preserving the reported property order (SHOW
// INDEXES reports properties in index-definition order). Skips the property
// sort so composite order remains significant — mirroring [desiredIndexKey],
// not [remoteSemanticKey].
//
// Type is upper-cased because [RemoteIndex.Declarable] admits it case-insensitively
// while [desiredIndexKey] embeds the canonical upper-case [indexKindToRemoteType].
// Without the fold here an index the declarability gate accepts could never pair
// by identity, so a remote realising a declared index would read as an unrelated
// object to drop.
func remoteIndexKey(o scoped[RemoteIndex]) string {
	return identityKey(append(
		[]string{o.label, strings.ToUpper(o.obj.Type)}, o.obj.Properties...,
	)...)
}

// unknownRemoteIndexType is what [indexKindToRemoteType] returns for a value
// outside the enum. [TestIndexKind_EnumerationIsComplete] sweeps a numeric range
// looking for values that map to something other than this, which is how a kind
// missing from [allIndexKinds] is caught.
const unknownRemoteIndexType = "UNKNOWN"

// indexKindToRemoteType maps an IndexKind to the corresponding Neo4j remote
// index type string. A kind missing a case here falls through to
// [unknownRemoteIndexType], which matches no remote index and makes every index
// of that kind invisible to the diff. //exhaustive:enforce makes that a CI
// failure rather than a silent omission — the repo's linter config leaves
// default-signifies-exhaustive at false, so the default arm does not satisfy it.
func indexKindToRemoteType(kind IndexKind) string {
	//exhaustive:enforce
	switch kind {
	case IndexRange:
		return "RANGE"
	case IndexVector:
		return "VECTOR"
	case IndexFulltext:
		return "FULLTEXT"
	default:
		return unknownRemoteIndexType
	}
}

// DeclarableIndexType reports whether a remote index type string names a kind
// the DSL can declare — RANGE, VECTOR, or FULLTEXT, case-insensitively.
//
// It is the kind half of [RemoteIndex.Declarable], for a consumer that
// classifies by kind alone and enforces its own rules on ownership, label
// count, and entity type. An empty type is not declarable: nothing identifies
// what kind an unreported one is, and guessing would classify it against a
// desired index of some other kind.
func DeclarableIndexType(remoteType string) bool {
	for _, k := range allIndexKinds {
		if strings.EqualFold(remoteType, indexKindToRemoteType(k)) {
			return true
		}
	}
	return false
}

// Declarable reports whether a remote index is one a schema could have
// declared, and therefore one [Adapter.DiffIndexes] owns.
//
// Four rules, all of which must hold.
//
// The kind must be one the DSL has vocabulary for — see [DeclarableIndexType].
// SHOW INDEXES reports every non-LOOKUP, non-constraint-backed index on a
// schema-owned label, including TEXT, POINT, and the Neo4j 4.x-historical
// BTREE, and including relationship indexes, which the adapter never emits.
// Classifying one of those as an undeclared drop would report drift that no
// schema edit could resolve, leaving the all-or-nothing constraints-only mode
// as the only escape. An unreported kind is deliberately not declarable, for
// the reason [DeclarableIndexType] gives; such an object still blocks names,
// because blockers are collected before this filter, so it is not invisible,
// only unclassified.
//
// The index must own at most one label. A multi-label node index — FULLTEXT is
// the only kind the server creates that way (`FOR (n:A|B)`) — is not
// declarable: an annotation is per-type, so a declaration always targets
// exactly one label, and a multi-label object is a different index the server
// creates a single-label declaration beside. The guard is on the label count,
// not the kind, so a future multi-label-capable kind cannot silently escape it.
//
// The index must not back a constraint. A backing index is created and dropped
// with its constraint, so reporting one as an undeclared orphan advises
// dropping the index behind a primary key. [IntrospectIndexesQuery] yields
// those rows deliberately — they are blockers the diff must see — so this check
// is the only thing keeping them out of matches and drops.
//
// The entity type must be NODE or unreported. An unreported one is treated as
// NODE: the field is absent only where a server or fixture does not report it,
// and every index the adapter emits is a node index. The reverse — treating an
// unreadable entityType as not-a-node-index — would silently drop real drift on
// any server whose driver shape this package does not recognise, which is the
// worse failure: an admitted relationship index is at most a spurious drop
// line, a dropped node index is undetected drift.
//
// Both string comparisons fold case. The columns are canonically upper-case,
// but a consumer assembling records from its own SHOW INDEXES mapping may
// normalise them, and a case difference must not make a genuinely declarable
// index invisible to matching and dropping.
//
// The constraint-side counterpart is a method on [Adapter] rather than on the
// remote value, because edition gating is part of its answer. The two are not
// parallel by design.
func (ri RemoteIndex) Declarable() bool {
	if ri.OwningConstraint != "" {
		return false
	}
	if len(ri.LabelsOrTypes) > 1 {
		return false
	}
	if ri.EntityType != "" && !strings.EqualFold(ri.EntityType, "NODE") {
		return false
	}
	return DeclarableIndexType(ri.Type)
}

// vectorConfigUnreadable reports whether a matched VECTOR index cannot be
// compared because the database reported no readable vector configuration — an
// older server that omits options, or a driver shape the options parser does not
// recognise.
//
// The semantic key matches on label, properties, and type alone, so a remote
// index of the wrong dimension matches a desired one by key; without this the
// pair would land in Match and report an unchecked index as in sync. It is a
// distinct outcome from drift (nothing was proven different) and from a failed
// introspection (there, nothing was compared at all).
// It names WHICH setting could not be read. A single fixed message claiming the
// database reported no configuration at all sends an operator looking for a
// server that omits options entirely, when in fact one setting was readable and
// only the other could not be compared.
//
// Callers must run [vectorDrift] first: a readable setting that disagrees is a
// definite, actionable finding and outranks the other setting being
// unreadable, so "we could not check everything" is only reported once
// everything checkable has agreed.
func vectorConfigUnreadable(d Index, ri RemoteIndex) (string, bool) {
	if d.Kind != IndexVector {
		return "", false
	}
	_, dimOK := ri.VectorDimensions()
	_, simOK := ri.VectorSimilarity()
	switch {
	case dimOK && simOK:
		return "", false
	case dimOK:
		return "database reported no vector similarity function; it was not compared", true
	case simOK:
		return "database reported no vector dimension; it was not compared", true
	default:
		return "database reported no vector configuration; dimension and similarity not compared", true
	}
}

// vectorDrift reports whether a matched VECTOR index differs in a configuration
// setting the database actually disclosed.
//
// Each setting is compared independently and only when it is readable:
// "unverifiable" is not "different", so an absent setting claims no drift, but
// it must not suppress a comparison of the setting sitting next to it either. A
// remote index with a wrong dimension and no reported similarity is drifted on
// the evidence available, not merely unverified.
func vectorDrift(d Index, ri RemoteIndex) (string, bool) {
	if d.Kind != IndexVector {
		return "", false
	}
	if remoteDim, ok := ri.VectorDimensions(); ok && d.VectorDimensions != remoteDim {
		return fmt.Sprintf("vector dimension mismatch: schema %d, database %d",
			d.VectorDimensions, remoteDim), true
	}
	// Case-insensitive: the schema side is lowercase (validation forces it) while
	// Neo4j reports the similarity function uppercased ("COSINE"), so a
	// case-sensitive compare would flag every in-sync vector index as drift. The
	// two valid functions ("cosine", "euclidean") differ beyond case, so
	// EqualFold cannot conflate them.
	if remoteSim, ok := ri.VectorSimilarity(); ok && !strings.EqualFold(d.VectorSimilarity, remoteSim) {
		return fmt.Sprintf("vector similarity mismatch: schema %s, database %s",
			d.VectorSimilarity, remoteSim), true
	}
	return "", false
}

// firstLabel returns the label a remote object's DEFINITION is keyed by. Only
// single-label rows are keyed — [Adapter.DiffIndexes] skips a multi-label row
// before computing its definition key, because a multi-label index serves no
// single-label declaration — so the "first" label is the row's only label; the
// function merely stays total for the empty slice a hand-built record can
// carry. Use [describeLabels] wherever a message names the object.
func firstLabel(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	return labels[0]
}

// describeLabels renders every label a remote object carries, in Neo4j's own
// multi-label index syntax, so a message telling an operator which object to
// drop names the whole thing. A FULLTEXT node index created
// `FOR (n:Other|Doc)` reports both, and naming only the first sends the operator
// looking under a label the object does not exclusively belong to.
func describeLabels(labels []string) string {
	return strings.Join(labels, "|")
}

// indexCreateBlocker reports the remote index, if any, that would make the
// emitted CREATE for d silently do nothing.
//
// Two independent conditions, both observed against a live server:
//
//   - Its NAME is already held. Index names are global — across labels, across
//     kinds, and shared with constraint names via their backing indexes — so a
//     CREATE under a taken name is skipped whatever else differs.
//   - Its DEFINITION is already served by another PLAIN index. An index over the
//     same label, properties, and kind is not created a second time under a
//     different name.
//
// A definition served by a constraint's BACKING index does not reach here: the
// caller treats that as satisfying the declaration, because no action could
// clear it. The caller passes the desired index's identity key, which it has
// already computed for that test.
func indexCreateBlocker(d Index, key string, byName, byDefinition map[string]RemoteIndex) (RemoteIndex, bool) {
	if blocker, taken := byName[d.Name]; taken && d.Name != "" {
		return blocker, true
	}
	if blocker, served := byDefinition[key]; served {
		return blocker, true
	}
	return RemoteIndex{}, false
}

// describeIndexBlocker explains which of the two blocking conditions applies and
// what has to be removed, so the operator has an action rather than a repeat.
//
// claimed holds the names pairing already gave to a declaration. A blocker in
// that set is this schema's own realisation of some OTHER declaration that
// happens to carry this one's generated name; dropping it is still the right
// move, because the next apply re-creates it under its own name, but the report
// otherwise shows one object as both matched and to-be-dropped with nothing
// connecting the two lines.
func describeIndexBlocker(d Index, blocker RemoteIndex, claimed map[string]struct{}) string {
	// Guarded on a non-empty desired name: DiffIndexes is public API and a
	// caller may build Index values with no name at all, in which case an
	// equally nameless blocker is a DEFINITION block, not a name collision.
	if d.Name != "" && blocker.Name == d.Name {
		if blocker.OwningConstraint != "" {
			return fmt.Sprintf("index name %q is held by the index backing constraint %q; drop that constraint or rename, or this index can never be created",
				blocker.Name, blocker.OwningConstraint)
		}
		if _, isOurs := claimed[blocker.Name]; isOurs {
			return fmt.Sprintf("index name %q is already held by a %s index on %s that realises another declaration in this schema; drop it — the next apply recreates it under its own name",
				blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes))
		}
		return fmt.Sprintf("index name %q is already held by a %s index on %s; drop it before this index can be created",
			blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes))
	}
	return fmt.Sprintf("this definition is already served by index %q; drop it before this index can be created", blocker.Name)
}

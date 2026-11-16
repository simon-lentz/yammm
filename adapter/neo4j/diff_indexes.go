package neo4j

import (
	"cmp"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
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

	// Drift contains the desired indexes a CREATE alone cannot realise, in the
	// desired list's order, each with every database object whose drop its
	// create needs. The package doc's Index Diffing section lists what makes a
	// declaration drift.
	Drift []IndexDrift

	// Create contains indexes defined in the schema that nothing realises once
	// the plan's drops run, and that nothing blocks. An index served by the
	// backing index of a constraint the plan drops is one.
	Create []Index

	// Drop contains schema-owned indexes in the database that have no
	// corresponding declaration in the schema AND that the schema could have
	// declared. A kind the DSL has no vocabulary for (TEXT, POINT), a
	// multi-label FULLTEXT index, a relationship index, and a constraint's
	// backing index are all left out even on an owned label: none is something
	// a schema edit could account for, so reporting them would be drift that
	// never clears. See [RemoteIndex.Declarable]. An index a drift on either
	// side names among its Blockers is not also a Drop. A caller that applies
	// Drops drops them before any create.
	Drop []RemoteIndex

	// Unverified contains indexes that exist in both by semantic key but whose
	// full definition could not be compared because the database did not report
	// the configuration needed. They are neither confirmed in sync nor confirmed
	// drifted; folding them into Match would report an unchecked index as
	// verified.
	Unverified []IndexUnverified

	// Excluded counts the remote indexes the plan leaves alone: no Match or
	// Unverified Actual, no Drop, and no row whose drop target a drift on either
	// side names or a Drop removes. Each is on a label the schema does not own,
	// or [RemoteIndex.Declarable] refuses it. [ConstraintDiffResult.Excluded] gives the reason the count exists.
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

// IndexDrift pairs a desired index with every database object standing in its
// way.
type IndexDrift struct {
	Desired Index

	// Blockers holds every database object to drop before Desired can be
	// created, at least one. A blocker can be unowned or undeclarable.
	Blockers Blockers

	// Reason has one clause per blocker, the realisation's first where the drift
	// pairs one.
	Reason string
}

// indexSide is the index half of a plan while [Adapter.Diff] settles it. actual
// is sorted, orphans are the owned, declarable rows no declaration took, and
// unmatched the declarations pairing left without a row.
type indexSide struct {
	actual    []RemoteIndex
	plans     []indexPlan
	unmatched []int
	orphans   []scoped[RemoteIndex]
	claimed   []string
}

// pairIndexes pairs desired indexes with the owned remote indexes
// [RemoteIndex.Declarable] accepts, in three phases: name and identity
// together, then identity alone, then name alone; see [pairObjects].
//
// Composite property order is significant, a deliberate divergence from the
// constraint side, which sorts properties to pair them: SHOW INDEXES reports
// properties in index-definition order, so the order-sensitive identity is
// sound. A same-set, different-order remote index is a distinct index.
func pairIndexes(desired []Index, actual []RemoteIndex, owned *OwnedLabels, t targets) indexSide {
	side := indexSide{actual: actual}
	var ownedRemotes []scoped[RemoteIndex]
	for at, ri := range actual {
		if label, isOwned := owned.ownedLabel(ri.LabelsOrTypes); isOwned && ri.Declarable() {
			ownedRemotes = append(ownedRemotes, scoped[RemoteIndex]{obj: ri, label: label, at: at})
		}
	}

	positions := make([]int, len(desired))
	for i := range positions {
		positions[i] = i
	}
	pairs, unmatched, orphans := pairObjects(positions, ownedRemotes, identityFuncs[int, scoped[RemoteIndex]]{
		desiredName: func(i int) string { return desired[i].Name },
		actualName:  func(o scoped[RemoteIndex]) string { return o.obj.Name },
		desiredKey:  func(i int) string { return desiredIndexKey(desired[i]) },
		actualKey:   remoteIndexKey,
	})
	side.unmatched, side.orphans = unmatched, orphans

	side.plans = make([]indexPlan, len(desired))
	for i, d := range desired {
		side.plans[i] = indexPlan{desired: d, state: planCreate}
	}
	for _, p := range pairs {
		d, ri := desired[p.Desired], p.Actual.obj
		if ri.Name != "" {
			side.claimed = append(side.claimed, ri.Name)
		}
		cp := &side.plans[p.Desired]
		cp.realisation, cp.at, cp.realised, cp.state = &ri, p.Actual.at, t.index(ri), planMatch

		// Only a name-only pair can have disagreeing identities; the other two
		// phases match on identity by construction. The database already holds
		// the name, and every emitted statement carries IF NOT EXISTS, so a
		// CREATE would silently do nothing.
		if p.byName {
			cp.state = planDrift
			cp.ownCause = fmt.Sprintf("index %q exists with a different definition: schema %s, database %s",
				ri.Name, describeIndexShape(d.Properties, indexKindToRemoteType(d.Kind)),
				describeIndexShape(ri.Properties, ri.Type))
			continue
		}

		// Drift before unverifiability, throughout: a fact the database
		// disclosed that disagrees with the declaration is definite and
		// actionable, and outranks anything the database would not disclose or
		// has not finished doing.
		if reason, drifted := vectorDrift(d, ri); drifted {
			cp.state, cp.ownCause = planDrift, reason
			continue
		}
		if classifyIndexState(cp, ri) {
			continue
		}
		if reason, unverifiable := vectorConfigUnreadable(d, ri); unverifiable {
			cp.state, cp.unverifiedReason = planUnverified, reason
		}
	}
	return side
}

// serveFromBackingIndexes gives each unmatched declaration a constraint's
// backing index that serves its definition, as its realisation. Such an index
// satisfies the declaration rather than blocking it: the server accepts the
// declaration's create beside it as a no-op. Its state is checked as a paired
// index's is.
func (side indexSide) serveFromBackingIndexes(f indexBlockerFinder, t targets) {
	for _, i := range side.unmatched {
		cp := &side.plans[i]
		if at, served := f.backingFor(desiredIndexKey(cp.desired)); served {
			ri := side.actual[at]
			cp.realisation, cp.at, cp.realised, cp.served, cp.state = &ri, at, t.index(ri), true, planMatch
			classifyIndexState(cp, ri)
		}
	}
}

// result files every settled plan and accounts for every remote row: a row is a
// Match or Unverified Actual, a Drop, a row whose target removed holds, or
// counted in Excluded. An orphan a drift names is no Drop.
func (side indexSide) result(blocking, removed map[string]struct{}, t targets) IndexDiffResult {
	var r IndexDiffResult
	accounted := make([]bool, len(side.actual))
	for _, cp := range side.plans {
		switch cp.state {
		case planMatch:
			r.Match = append(r.Match, IndexMatch{Desired: cp.desired, Actual: *cp.realisation})
			accounted[cp.at] = true
		case planUnverified:
			r.Unverified = append(r.Unverified, IndexUnverified{Desired: cp.desired, Actual: *cp.realisation, Reason: cp.unverifiedReason})
			accounted[cp.at] = true
		case planCreate:
			r.Create = append(r.Create, cp.desired)
		case planDrift:
			drift := IndexDrift{Desired: cp.desired}
			clauses := make([]string, 0, len(cp.blockers))
			for _, b := range cp.blockers {
				b.addTo(&drift.Blockers)
				clauses = append(clauses, b.why)
			}
			drift.Reason = strings.Join(clauses, "; ")
			r.Drift = append(r.Drift, drift)
		}
	}
	for _, o := range side.orphans {
		if _, isBlocker := blocking[t.index(o.obj)]; !isBlocker {
			r.Drop = append(r.Drop, o.obj)
			accounted[o.at] = true
		}
	}
	for at, ri := range side.actual {
		if _, gone := removed[t.index(ri)]; !accounted[at] && !gone {
			r.Excluded++
		}
	}
	return r
}

type indexPlan = plan[Index, RemoteIndex]

// classifyIndexState files a plan whose realisation does not serve queries: a
// FAILED index is drift and a POPULATING one unverified. It reports whether it
// filed the plan.
func classifyIndexState(cp *indexPlan, ri RemoteIndex) bool {
	reason, actionable := indexStateProblem(ri)
	switch {
	case reason == "":
		return false
	case actionable:
		cp.state, cp.ownCause = planDrift, reason
	default:
		cp.state, cp.unverifiedReason = planUnverified, reason
	}
	return true
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
// declared, and therefore one [Adapter.Diff] can pair or drop when the
// schema owns its label.
//
// Four rules, all of which must hold.
//
// The kind must be one the DSL has vocabulary for — see [DeclarableIndexType].
// [IntrospectIndexesQuery] reports every index, including TEXT, POINT, LOOKUP,
// the Neo4j 4.x-historical BTREE, relationship indexes, which the adapter never
// emits, and constraints' backing indexes.
// Classifying one of those as an undeclared drop would report drift that no
// schema edit could resolve, leaving the all-or-nothing constraints-only mode
// as the only escape. An unreported kind is deliberately not declarable, for
// the reason [DeclarableIndexType] gives. Such an object still blocks names,
// because blockers are collected from every row whatever this filter says, so
// it is not invisible, only unclassified.
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
// keeps them out of pairing and drops. One can serve the definition of a
// declaration that pairing left without a row. While the plan keeps its
// constraint, it is that declaration's Actual, or its drift's first blocker
// where it is FAILED. Where the plan drops the constraint of an online or
// POPULATING one, the declaration is a Create unless another blocker makes it
// a Drift.
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
// single-label rows are keyed — [Adapter.Diff] skips a multi-label row
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
	if len(labels) == 0 {
		return "no label"
	}
	return strings.Join(labels, "|")
}

// indexBlockerFinder indexes every remote object that can block a desired
// index's create, by the name or the definition it blocks.
//
// Two conditions, both observed against a live server:
//
//   - Its NAME is already held. Index names are global — across labels, across
//     kinds, and shared with constraint names — so a CREATE under a taken name
//     is skipped whatever else differs.
//   - Its DEFINITION is already served by another PLAIN index. An index over the
//     same label, properties, and kind is not created a second time under a
//     different name.
//
// A definition served by a constraint's BACKING index satisfies the
// declaration instead; see backingFor.
type indexBlockerFinder struct {
	actual []RemoteIndex
	byName map[string][]int
	// byDefinition holds positions in actual, since a name does not tell one
	// row from another: a caller's rows may share one or have none.
	byDefinition map[string][]int
	// constraintByName reaches a NOT NULL or TYPE constraint, which no SHOW
	// INDEXES row carries; a key constraint's backing index row is named first
	// and shares its drop target.
	constraintByName map[string][]RemoteConstraint
	claimed          map[string]struct{}
	targets          targets
}

func newIndexBlockerFinder(actual []RemoteIndex, constraints []RemoteConstraint, claimed map[string]struct{}, t targets) indexBlockerFinder {
	f := indexBlockerFinder{
		actual:           actual,
		byName:           make(map[string][]int),
		byDefinition:     make(map[string][]int),
		constraintByName: make(map[string][]RemoteConstraint),
		claimed:          claimed,
		targets:          t,
	}
	for i, ri := range actual {
		if ri.Name != "" {
			f.byName[ri.Name] = append(f.byName[ri.Name], i)
		}
		// Node and relationship indexes occupy separate definition namespaces,
		// and a multi-label node index serves no single-label declaration: the
		// server creates the declaration beside either. Their NAMES still block.
		if ri.EntityType != "" && !strings.EqualFold(ri.EntityType, "NODE") {
			continue
		}
		if len(ri.LabelsOrTypes) > 1 {
			continue
		}
		def := identityKey(append(
			[]string{firstLabel(ri.LabelsOrTypes), strings.ToUpper(ri.Type)}, ri.Properties...,
		)...)
		f.byDefinition[def] = append(f.byDefinition[def], i)
	}
	for _, rc := range constraints {
		if rc.Name != "" {
			f.constraintByName[rc.Name] = append(f.constraintByName[rc.Name], rc)
		}
	}
	return f
}

// backingFor returns the position of a constraint's backing index that serves
// the definition key, if one does.
func (f indexBlockerFinder) backingFor(key string) (int, bool) {
	for _, at := range f.byDefinition[key] {
		if f.actual[at].OwningConstraint != "" {
			return at, true
		}
	}
	return 0, false
}

// blockers returns every remote object that keeps d's create from taking
// effect, each with the clause that explains it. A backing index serving d's
// definition is no blocker; see backingFor.
func (f indexBlockerFinder) blockers(d Index) []blocker {
	var out []blocker
	if d.Name != "" {
		for _, at := range f.byName[d.Name] {
			out = append(out, blocker{index: &f.actual[at], target: f.targets.index(f.actual[at]), why: describeIndexBlocker(d, f.actual[at], f.claimed)})
		}
	}
	for _, at := range f.byDefinition[desiredIndexKey(d)] {
		if ri := &f.actual[at]; ri.OwningConstraint == "" {
			out = append(out, blocker{index: ri, target: f.targets.index(*ri), why: describeIndexBlocker(d, *ri, f.claimed)})
		}
	}
	if d.Name != "" {
		for _, rc := range f.constraintByName[d.Name] {
			out = append(out, blocker{
				constraint: &rc,
				target:     f.targets.constraint(rc),
				why: fmt.Sprintf("name %q is already held by a %s constraint on %s; index and constraint names share one namespace, so drop or rename it before this index can be created",
					rc.Name, rc.Type, describeLabels(rc.LabelsOrTypes)),
			})
		}
	}
	return out
}

// remoteIndexRecordKey identifies one remote index record.
func remoteIndexRecordKey(ri RemoteIndex) string {
	return identityKey(append(append(
		[]string{ri.Name, ri.Type, ri.EntityType, ri.State, ri.OwningConstraint, strconv.Itoa(len(ri.LabelsOrTypes))},
		ri.LabelsOrTypes...), ri.Properties...)...)
}

// compareRemoteIndexes is the order [Adapter.Diff] reads indexes in, so no
// result depends on the order the server returned rows in.
func compareRemoteIndexes(x, y RemoteIndex) int {
	return cmp.Or(
		cmp.Compare(x.Name, y.Name),
		cmp.Compare(x.Type, y.Type),
		cmp.Compare(x.EntityType, y.EntityType),
		slices.Compare(x.LabelsOrTypes, y.LabelsOrTypes),
		slices.Compare(x.Properties, y.Properties),
		cmp.Compare(x.State, y.State),
		cmp.Compare(x.OwningConstraint, y.OwningConstraint),
		compareValues(reflect.ValueOf(x.Options), reflect.ValueOf(y.Options)),
	)
}

// compareValues orders two valid values totally: by type, then by value. The
// printed form is not enough, since int64(3) and float64(3) both print as 3.
// A nil map, slice, pointer or interface comes first, a map orders by its
// entries in key order, a float by its bits after its value, and a func,
// channel or unsafe pointer by its printed form. Two distinct types that print
// one name and one package order by their values.
func compareValues(x, y reflect.Value) int {
	if c := cmp.Compare(x.Type().String(), y.Type().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(x.Type().PkgPath(), y.Type().PkgPath()); c != 0 {
		return c
	}
	if c := cmp.Compare(x.Kind(), y.Kind()); c != 0 {
		return c
	}
	switch x.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface:
		if c := cmp.Compare(boolRank(!x.IsNil()), boolRank(!y.IsNil())); c != 0 || x.IsNil() {
			return c
		}
	}
	switch x.Kind() {
	case reflect.Bool:
		return cmp.Compare(boolRank(x.Bool()), boolRank(y.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cmp.Compare(x.Int(), y.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return cmp.Compare(x.Uint(), y.Uint())
	case reflect.Float32, reflect.Float64:
		return compareFloats(x.Float(), y.Float())
	case reflect.Complex64, reflect.Complex128:
		return cmp.Or(compareFloats(real(x.Complex()), real(y.Complex())), compareFloats(imag(x.Complex()), imag(y.Complex())))
	case reflect.String:
		return cmp.Compare(x.String(), y.String())
	case reflect.Pointer, reflect.Interface:
		return compareValues(x.Elem(), y.Elem())
	case reflect.Slice, reflect.Array:
		for i := range min(x.Len(), y.Len()) {
			if c := compareValues(x.Index(i), y.Index(i)); c != 0 {
				return c
			}
		}
		return cmp.Compare(x.Len(), y.Len())
	case reflect.Map:
		return slices.CompareFunc(sortedEntries(x), sortedEntries(y), compareEntries)
	case reflect.Struct:
		for i := range x.NumField() {
			if c := compareValues(x.Field(i), y.Field(i)); c != 0 {
				return c
			}
		}
		return 0
	}
	return cmp.Compare(fmt.Sprint(x), fmt.Sprint(y))
}

// compareFloats orders NaN first, then by value, then by bits, so -0 and +0
// and two NaN payloads are told apart.
func compareFloats(x, y float64) int {
	return cmp.Or(cmp.Compare(x, y), cmp.Compare(math.Float64bits(x), math.Float64bits(y)))
}

// entry is one map entry. A map can hold two NaN keys, so entries with equal
// keys order by value.
type entry struct{ key, value reflect.Value }

func compareEntries(a, b entry) int {
	return cmp.Or(compareValues(a.key, b.key), compareValues(a.value, b.value))
}

func sortedEntries(m reflect.Value) []entry {
	entries := make([]entry, 0, m.Len())
	for it := m.MapRange(); it.Next(); {
		entries = append(entries, entry{it.Key(), it.Value()})
	}
	slices.SortFunc(entries, compareEntries)
	return entries
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// describeIndexBlocker says why blocker keeps d's create from taking effect and
// what to drop. A name in claimed is another declaration's realisation, whose
// own drift creates it again under its own name.
func describeIndexBlocker(d Index, blocker RemoteIndex, claimed map[string]struct{}) string {
	// Guarded on a non-empty desired name: Diff is public API and a
	// caller may build Index values with no name at all, in which case an
	// equally nameless blocker is a DEFINITION block, not a name collision.
	if d.Name != "" && blocker.Name == d.Name {
		if blocker.OwningConstraint != "" {
			return fmt.Sprintf("index name %q is held by the index backing constraint %q; drop that constraint or rename, or this index can never be created",
				blocker.Name, blocker.OwningConstraint)
		}
		if _, isOurs := claimed[blocker.Name]; isOurs {
			return fmt.Sprintf("index name %q is already held by a %s index on %s that realises another declaration in this schema; drop it, and that declaration is created again under its own name",
				blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes))
		}
		return fmt.Sprintf("index name %q is already held by a %s index on %s; drop it before this index can be created",
			blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes))
	}
	return fmt.Sprintf("this definition is already served by index %q; drop it before this index can be created", blocker.Name)
}

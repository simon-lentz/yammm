package neo4j

import (
	"context"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// OwnedLabels is the set of Neo4j labels one schema owns: every named,
// non-abstract member type's label, a superset of the labels an adapter emits for it.
// Construct via [Adapter.OwnedLabels]; the zero value and a nil pointer own
// nothing.
//
// [Adapter.Diff] takes this rather than a schema name because ownership
// cannot be recovered from a label string. [Adapter.Label] composes a label from
// a caller-configurable prefix and separator around a schema name and a type
// name, which is not an invertible function: for any rule that tries to read a
// schema back out of a label there is a configuration, or a sibling schema name,
// that satisfies the rule without belonging to the schema. Membership answers
// the question directly instead.
type OwnedLabels struct {
	labels map[string]struct{}
}

// OwnedLabels returns every named, non-abstract type's label across the whole
// IMPORT CLOSURE of s, under the adapter's current configuration: the labels this
// adapter emits for s, and a label it refuses to emit (below).
//
// The closure, not the entry schema alone: an imported type this schema writes
// nodes for gets a label, and it is emitted constraints and indexes, so it is
// the schema's to own. The emit set and the own set reach their types through
// one walk, which is what keeps the desired and owned sides widening together,
// so a newly-owned object is also newly desired and no spurious DROP arises. A
// later change must not split them. They differ only by the labels the
// emitters refuse (below).
//
// The set is every such type's label, and ownership checks no identifier.
// [Adapter.BatchNodeQueries], [Adapter.BatchEdgeQueries],
// [Adapter.ConstraintsStructured] and [Adapter.IndexesStructured] each REFUSE the whole schema with
// [E_NEO4J_INVALID_IDENTIFIER] when a type's label is not a valid Neo4j
// identifier, emitting nothing (see [SanitizeIdentifier]); this set still
// names such a label, because a database may hold objects on it from an
// earlier configuration and they are the schema's to report. The set is
// therefore not a count of what will be emitted.
//
// It is derived from the schema in hand, which fixes what a diff can see. An
// object left behind by a type that has since been DELETED or RENAMED sits on a
// label no current type produces, so it is outside this set and is never a
// Drop. Unless a drift names it as a blocker, its half's Excluded counts it. That
// is a property of deriving ownership from the schema rather than a gap to be
// closed by matching labels against a namespace prefix: [Adapter.Label] composes
// a label from a caller-configurable prefix and separator around a schema name
// and a type name, so for any prefix rule there is a sibling schema name or a
// separator setting that satisfies it without belonging to the schema — and a
// false positive there is a plan telling an operator to drop another
// application's constraint.
func (a *Adapter) OwnedLabels(ctx context.Context, s schema.Schema) *OwnedLabels {
	owned := &OwnedLabels{labels: make(map[string]struct{})}
	for _, label := range a.labeledTypes(ctx, s) {
		owned.labels[label] = struct{}{}
	}
	return owned
}

// Contains reports whether label is one this schema emits.
func (o *OwnedLabels) Contains(label string) bool {
	if o == nil {
		return false
	}
	_, ok := o.labels[label]
	return ok
}

// LabelOf returns the first label of a remote object that this schema owns, and
// whether the object is owned at all.
//
// Ownership and identity are answered together, from the same label, so an
// object carrying several labels can never be admitted under one label and then
// keyed by another. It is exported because the diff hands back bare
// [RemoteIndex] and [RemoteConstraint] values, whose LabelsOrTypes a caller
// reporting on a Drop must resolve the same way the diff did — picking the
// first label instead names one the schema may not own. A drift's Blockers can
// be unowned, and LabelOf reports false for one.
func (o *OwnedLabels) LabelOf(labels []string) (string, bool) {
	return o.ownedLabel(labels)
}

func (o *OwnedLabels) ownedLabel(labels []string) (string, bool) {
	for _, label := range labels {
		if o.Contains(label) {
			return label, true
		}
	}
	return "", false
}

// scoped carries a remote object together with the schema-owned label that
// admitted it, so identity, drift messages, and the drop list all read the same
// label ownership was decided by. at is the object's position in the sorted
// remote rows.
type scoped[A any] struct {
	obj   A
	label string
	at    int
}

// identityKey encodes segments into a string no other segment list can produce,
// by writing each segment's byte length and a colon ahead of it.
//
// The segments include remote property names and labels, which are
// database-supplied and unvalidated — Neo4j accepts any property key when it is
// backticked — so a separator join is not injective: one property named "a,b"
// would encode identically to the two-property composite (a, b).
func identityKey(segments ...string) string {
	var b strings.Builder
	n := 0
	for _, s := range segments {
		n += len(s) + 3
	}
	b.Grow(n)
	for _, s := range segments {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(s)
	}
	return b.String()
}

// pair is one desired object matched to the remote object realising it.
// byName records that the two were matched by name alone, which is the only way
// a pair's identities can differ.
type pair[D, A any] struct {
	Desired D
	Actual  A
	byName  bool
}

// identityFuncs supplies pairObjects' projections. An empty name skips the name
// phases, and nil exact projections skip the exact phase.
type identityFuncs[D, A any] struct {
	desiredName  func(D) string
	actualName   func(A) string
	desiredKey   func(D) string
	actualKey    func(A) string
	desiredExact func(D) string
	actualExact  func(A) string
}

// pairObjects matches each desired object with at most one remote object in
// four phases, strongest evidence first: name and identity, the exact
// definition, identity alone, name alone. Within a phase the first remaining
// candidate in actual's order wins, so [Adapter.Diff] sorts actual first.
func pairObjects[D, A any](desired []D, actual []A, f identityFuncs[D, A]) (pairs []pair[D, A], unmatched []D, orphans []A) {
	claimed := make([]bool, len(actual))
	matchedTo := make([]int, len(desired))
	byName := make([]bool, len(desired))
	for i := range matchedTo {
		matchedTo[i] = -1
	}

	// index groups the unclaimed remote objects under a projection, in actual's
	// order within each group.
	index := func(key func(A) string) map[string][]int {
		m := make(map[string][]int, len(actual))
		for i, a := range actual {
			if claimed[i] {
				continue
			}
			if k := key(a); k != "" {
				m[k] = append(m[k], i)
			}
		}
		return m
	}
	// claim gives each still-unmatched desired object the first candidate under
	// its key that is unclaimed and acceptable.
	//
	// The scan restarts at the front of the bucket for every desired object.
	// Carrying a per-key cursor instead would skip past a candidate an earlier
	// desired object REJECTED — in the name-and-identity phase, rejection is the
	// common case — so a later desired object could be denied the very candidate
	// that is its exact name-and-identity realisation. `claimed` already stops a
	// candidate being taken twice, which is all the cursor was needed for.
	claim := func(candidates map[string][]int, key func(D) string, accept func(di, ai int) bool, viaName bool) {
		for di := range desired {
			if matchedTo[di] >= 0 {
				continue
			}
			k := key(desired[di])
			if k == "" {
				continue
			}
			for _, ai := range candidates[k] {
				if claimed[ai] || !accept(di, ai) {
					continue
				}
				claimed[ai] = true
				matchedTo[di] = ai
				byName[di] = viaName
				break
			}
		}
	}

	sameIdentity := func(di, ai int) bool { return f.desiredKey(desired[di]) == f.actualKey(actual[ai]) }
	always := func(int, int) bool { return true }

	claim(index(f.actualName), f.desiredName, sameIdentity, false)
	if f.desiredExact != nil && f.actualExact != nil {
		claim(index(f.actualExact), f.desiredExact, always, false)
	}
	claim(index(f.actualKey), f.desiredKey, always, false)
	claim(index(f.actualName), f.desiredName, always, true)

	for di, d := range desired {
		if ai := matchedTo[di]; ai >= 0 {
			pairs = append(pairs, pair[D, A]{Desired: d, Actual: actual[ai], byName: byName[di]})
			continue
		}
		unmatched = append(unmatched, d)
	}
	for i, a := range actual {
		if !claimed[i] {
			orphans = append(orphans, a)
		}
	}
	return pairs, unmatched, orphans
}

type planState int

const (
	planMatch planState = iota
	planUnverified
	planCreate
	planDrift
)

// plan is one declaration's classification while [Adapter.Diff] settles it.
// realisation is the remote row pairing gave it, nil when there is none; at is
// that row's position and realised its drop target. served marks an index
// realised by a constraint's backing index rather than by its own.
type plan[D, A any] struct {
	desired          D
	realisation      *A
	at               int
	realised         string
	served           bool
	state            planState
	ownCause         string
	unverifiedReason string
	blockers         []blocker
}

// blocker is one remote object a create needs dropped, the clause that says
// why, and its drop target. Exactly one of constraint and index is set.
type blocker struct {
	constraint *RemoteConstraint
	index      *RemoteIndex
	why        string
	target     string
}

func (b blocker) addTo(bs *Blockers) {
	if b.constraint != nil {
		bs.Constraints = append(bs.Constraints, *b.constraint)
		return
	}
	bs.Indexes = append(bs.Indexes, *b.index)
}

// targets gives each remote row its drop target: the object one DROP removes. A
// backing index's target is its constraint's, the first by that name in the
// sorted rows, or the name alone when no row carries it.
type targets struct {
	constraintByName map[string]string
}

func newTargets(constraints []RemoteConstraint) targets {
	t := targets{constraintByName: make(map[string]string)}
	for _, rc := range constraints {
		if _, seen := t.constraintByName[rc.Name]; rc.Name != "" && !seen {
			t.constraintByName[rc.Name] = t.constraint(rc)
		}
	}
	return t
}

func (targets) constraint(rc RemoteConstraint) string {
	return "c" + remoteConstraintKey(rc)
}

func (t targets) index(ri RemoteIndex) string {
	if ri.OwningConstraint == "" {
		return "i" + remoteIndexRecordKey(ri)
	}
	if target, ok := t.constraintByName[ri.OwningConstraint]; ok {
		return target
	}
	return "n" + ri.OwningConstraint
}

// settlement is the fixed point of one plan over both sides. A drop target names
// what one DROP removes: a constraint and its backing index share one. waiting
// holds the plans each target realises, and blocking the targets of every
// drift's blockers.
type settlement struct {
	blocking map[string]struct{}
	waiting  map[string][]func()
	queue    []func()
}

func newSettlement() *settlement {
	return &settlement{blocking: make(map[string]struct{}), waiting: make(map[string][]func())}
}

// settleRules is how one side's plans settle: find reports a declaration's blockers,
// moved the clause for a realisation another drift drops, and own carries a
// realisation as a blocker.
type settleRules[D, A any] struct {
	find  func(D) []blocker
	moved func(A) string
	own   func(*A) blocker
}

// enqueue adds plans to s. A Match or Unverified plan waits until its
// realisation's target is removed; every other plan settles when s runs.
func enqueue[D, A any](s *settlement, plans []plan[D, A], rules settleRules[D, A]) {
	for i := range plans {
		p := &plans[i]
		run := func() { settlePlan(s, p, rules) }
		if p.state == planMatch || p.state == planUnverified {
			s.waiting[p.realised] = append(s.waiting[p.realised], run)
			continue
		}
		s.queue = append(s.queue, run)
	}
}

// settlePlan gives p every blocker find reports, each target once, and moves it
// to Drift when there is one. A Match or Unverified plan reaching here lost its
// realisation: its own row moves it to Drift, that row first under the clause
// moved gives; a backing index it was served by leaves it a Create.
func settlePlan[D, A any](s *settlement, p *plan[D, A], rules settleRules[D, A]) {
	if p.state == planMatch || p.state == planUnverified {
		if p.served {
			p.state, p.realisation = planCreate, nil
		} else {
			p.state, p.ownCause = planDrift, rules.moved(*p.realisation)
		}
	}
	seen := make(map[string]struct{})
	if p.state == planDrift && p.realisation != nil {
		own := rules.own(p.realisation)
		own.why, own.target = p.ownCause, p.realised
		p.blockers = append(p.blockers, own)
		seen[p.realised] = struct{}{}
	}
	for _, b := range rules.find(p.desired) {
		if _, dup := seen[b.target]; dup {
			continue
		}
		seen[b.target] = struct{}{}
		p.blockers = append(p.blockers, b)
	}
	if len(p.blockers) > 0 {
		p.state = planDrift
	}
	for _, b := range p.blockers {
		s.blocking[b.target] = struct{}{}
		s.remove(b.target)
	}
}

// remove wakes the plans that target realised, once: the plan drops target.
func (s *settlement) remove(target string) {
	s.queue = append(s.queue, s.waiting[target]...)
	delete(s.waiting, target)
}

// run settles every queued plan; each plan settles once.
func (s *settlement) run() {
	for len(s.queue) > 0 {
		next := s.queue[0]
		s.queue = s.queue[1:]
		next()
	}
}

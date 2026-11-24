package neo4j

// scoped carries a remote object together with the schema-owned label that
// admitted it, so identity, drift messages, and the drop list all read the same
// label ownership was decided by. at is the object's position in the sorted
// remote rows.
type scoped[A any] struct {
	obj   A
	label string
	at    int
}

// pair is one desired object matched to the remote object realising it.
// byName records that the two were matched by name alone, which is the only way
// a pair's identities can differ.
type pair[D, A any] struct {
	Desired D
	Actual  A
	byName  bool
}

// identityFuncs supplies pairObjects' projections. Nil exact projections skip
// the exact phase.
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
// candidate in actual's order wins, so [Diff] sorts actual first.
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
			if !claimed[i] {
				m[key(a)] = append(m[key(a)], i)
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
			for _, ai := range candidates[key(desired[di])] {
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

// plan is one declaration's classification while [Diff] settles it.
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

// constraintTarget and indexTarget give a remote row its drop target, the
// object one DROP removes, by its kind and its name, which [Diff] holds unique
// within a kind. A backing index's target is its constraint's.
func constraintTarget(rc RemoteConstraint) string {
	return "c" + rc.Name
}

func indexTarget(ri RemoteIndex) string {
	if ri.OwningConstraint != "" {
		return "c" + ri.OwningConstraint
	}
	return "i" + ri.Name
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

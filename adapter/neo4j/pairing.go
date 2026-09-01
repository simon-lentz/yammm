package neo4j

import (
	"context"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// OwnedLabels is the exact set of Neo4j labels an adapter emits for one schema.
// Construct via [Adapter.OwnedLabels]; the zero value and a nil pointer own
// nothing.
//
// The diff entry points take this rather than a schema name because ownership
// cannot be recovered from a label string. [Adapter.Label] composes a label from
// a caller-configurable prefix and separator around two sanitized free-form
// names, which is not an invertible function: for any rule that tries to read a
// schema back out of a label there is a configuration, or a sibling schema name,
// that satisfies the rule without belonging to the schema. Membership answers
// the question directly instead.
type OwnedLabels struct {
	labels map[string]struct{}
}

// OwnedLabels returns the labels this adapter emits for s — every named,
// non-abstract type's label across the whole IMPORT CLOSURE, under the
// adapter's current configuration.
//
// The closure, not the entry schema alone: an imported type this schema writes
// nodes for gets a label, and it is emitted constraints and indexes, so it is
// the schema's to own. The emit set and the own set are ONE set by construction
// — both reach their types through the same walk — which is what keeps the
// desired and owned sides widening together, so a newly-owned object is also
// newly desired and no spurious DROP arises. A later change must not split
// them.
//
// The set is every such type's label — a SUPERSET of what the emitters
// produce. [Adapter.ConstraintsStructured], [Adapter.IndexesStructured]
// and [Adapter.ShapeForSchema] additionally skip a type whose label is not a
// valid Neo4j identifier, and emit nothing for it; ownership deliberately does
// not, because a database may still hold objects on that label from an earlier
// configuration and they are the schema's to report. The set is therefore not a
// count of what will be emitted.
//
// It is derived from the schema in hand, which fixes what a diff can see. An
// object left behind by a type that has since been DELETED or RENAMED sits on a
// label no current type produces, so it is outside this set and the diff
// classifies it into no bucket; [ConstraintDiffResult.Excluded] counts it. That
// is a property of deriving ownership from the schema rather than a gap to be
// closed by matching labels against a namespace prefix: [Adapter.Label] composes
// a label from a caller-configurable prefix and separator around two sanitized
// free-form names, so for any prefix rule there is a sibling schema name or a
// separator setting that satisfies it without belonging to the schema — and a
// false positive there is a plan telling an operator to drop another
// application's constraint.
//
// In UNSCOPED mode (an empty schema name) [Adapter.Label] returns the bare
// sanitized type name, so the set holds bare labels such as `Person`. That is
// exact — those are the labels this schema writes to — but it means an unrelated
// application's objects on the same bare label are the schema's to report,
// because the two are genuinely sharing the label. Scope a schema with a name,
// or a prefix, to keep the namespaces apart.
func (a *Adapter) OwnedLabels(ctx context.Context, s *schema.Schema) *OwnedLabels {
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

// LabelOf returns the one label of a remote object that this schema owns, and
// whether the object is owned at all.
//
// Ownership and identity are answered together, from the same label, so an
// object carrying several labels can never be admitted under one label and then
// keyed by another. It is exported because the diff hands back bare
// [RemoteIndex] and [RemoteConstraint] values, whose LabelsOrTypes a caller
// reporting on a drop or a drift must resolve the same way the diff did —
// picking the first label instead names one the schema may not own.
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
// label ownership was decided by.
type scoped[A any] struct {
	obj   A
	label string
}

func scopedObject[A any](obj A, label string) scoped[A] {
	return scoped[A]{obj: obj, label: label}
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

// identityFuncs supplies the four projections pairObjects needs: a name and a
// semantic identity for each side. A name may be empty — constraint names are
// suppressed under [WithNamedConstraints](false) — in which case that object
// does not participate in either name phase.
type identityFuncs[D, A any] struct {
	desiredName func(D) string
	actualName  func(A) string
	desiredKey  func(D) string
	actualKey   func(A) string
}

// pairObjects matches each desired object with at most one remote object,
// returning the pairs in desired order, the desired objects nothing realised,
// and the remote objects nothing claimed — the last in introspection order.
//
// Three phases, strongest evidence first:
//
//  1. name AND identity agree — the object this adapter emitted, still as it
//     emitted it;
//  2. identity alone — the declaration is realised by an object created under
//     some other name (an operator's ad-hoc index, or one emitted before a
//     naming change);
//  3. name alone — the name is taken by an object that is no longer what the
//     declaration asks for, which surfaces as drift.
//
// Ordering the phases this way is what keeps a name from outranking an exact
// realisation. A remote object whose name matches but whose definition does not
// is weaker evidence than one whose definition matches exactly; claiming by name
// first lets the misnamed object consume the declaration and leaves the exact
// one to be reported as an orphan to drop.
//
// Within a phase, pairing is positional over the candidates that remain, which
// is only reachable when several objects are indistinguishable under that
// phase's evidence — where no preference exists to get wrong.
func pairObjects[D, A any](desired []D, actual []A, f identityFuncs[D, A]) (pairs []pair[D, A], unmatched []D, orphans []A) {
	claimed := make([]bool, len(actual))
	matchedTo := make([]int, len(desired))
	byName := make([]bool, len(desired))
	for i := range matchedTo {
		matchedTo[i] = -1
	}

	// index groups the unclaimed remote objects under a projection, preserving
	// introspection order within each group.
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

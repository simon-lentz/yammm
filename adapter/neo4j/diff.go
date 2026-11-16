package neo4j

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ConstraintDiffResult is the five-way classification of desired vs actual
// constraints. Anything not in Match is actionable; Unverified is neither
// actionable nor a clean bill of health, so a caller deciding "is this database
// in sync?" must consult it as well as Drift, Create, and Drop.
type ConstraintDiffResult struct {
	// Match contains constraints that exist in both schema and database with
	// identical semantic definitions.
	Match []ConstraintMatch

	// Drift contains the desired constraints a CREATE alone cannot realise, in
	// the desired list's order, each with every database object whose drop its
	// create needs. [DiffResult] says how a plan is applied, and the package
	// doc's Applying a Plan section states each create's data precondition.
	Drift []ConstraintDrift

	// Create contains constraints defined in the schema that are absent from
	// the database and that nothing blocks. A declaration whose create
	// something blocks is a Drift.
	Create []Constraint

	// Drop contains schema-owned constraints in the database that have no
	// corresponding definition in the schema AND that the schema could have
	// declared. A relationship constraint (whose LabelsOrTypes holds a
	// relationship type, not a label) and a node constraint kind the DSL has no
	// vocabulary for are both left out even on an owned label: neither is
	// something a schema edit could account for, so reporting them would be drift
	// that never clears. This mirrors [IndexDiffResult.Drop]. See
	// [declarableRemoteConstraint]. A constraint a drift on either side names
	// among its Blockers is not also a Drop. A caller that applies Drops drops
	// them before any create.
	Drop []RemoteConstraint

	// Unverified contains constraints that exist in both by identity but whose
	// full definition could not be compared because the database did not report
	// what was needed. They are neither confirmed in sync nor confirmed drifted;
	// folding them into Match would report an unchecked constraint as verified.
	Unverified []ConstraintUnverified

	// Excluded counts the remote constraints the plan leaves alone: no Match or
	// Unverified Actual, no Drop, and no row whose drop target a drift on either
	// side names or a Drop removes. Each is on a label the schema does not own,
	// or of a kind this configuration cannot declare.
	//
	// It exists because the five buckets are a statement about what WAS compared,
	// and their being empty is not the same as the database being accounted for.
	// A type deleted or renamed since the last apply is the case that matters: its
	// constraints keep enforcing on a label no type declares any more, so they
	// are never a Drop: they land here, unless one holds a declared name and a
	// drift names it as a blocker.
	// Ownership is derived from the schema, so this is inherent, not a gap to be
	// closed by guessing at labels — see [OwnedLabels].
	//
	// A caller reporting "in sync" should say how many objects were left out of
	// that claim. It is deliberately not drift: in a database shared with other
	// applications a non-zero count is the normal state.
	Excluded int
}

// ConstraintUnverified pairs a desired constraint with an actual constraint
// whose definition could not be read, so no in-sync claim is made about it.
type ConstraintUnverified struct {
	Desired Constraint
	Actual  RemoteConstraint
	Reason  string // Human-readable description of what could not be verified
}

// ConstraintMatch pairs a desired constraint with its matching actual constraint.
type ConstraintMatch struct {
	Desired Constraint
	Actual  RemoteConstraint
}

// ConstraintDrift pairs a desired constraint with every database object standing
// in its way.
type ConstraintDrift struct {
	Desired Constraint

	// Blockers holds every database object to drop before Desired can be
	// created, at least one. A blocker can be unowned or undeclarable.
	Blockers Blockers

	// Reason has one clause per blocker, the realisation's first where the drift
	// pairs one. A NODE KEY that a UNIQUE constraint over its key blocks adds
	// a clause on the data the NODE KEY requires.
	Reason string
}

// Blockers lists the database objects that one drift's create needs dropped,
// each drop target once: a constraint and its backing index appear once. Drop a
// constraint with [DropConstraintStatement]. Drop an index with
// [DropIndexStatement], or by dropping its OwningConstraint where it has one.
type Blockers struct {
	Constraints []RemoteConstraint
	Indexes     []RemoteIndex
}

// DiffResult is the plan that takes a database's constraints and indexes to
// what a schema declares. Apply it whole: drop every drift's Blockers on both
// sides and every Drop the caller applies, each drop target once, then create
// every constraint Create and constraint drift's Desired, then every index
// Create and index drift's Desired.
type DiffResult struct {
	Constraints ConstraintDiffResult
	Indexes     IndexDiffResult
}

// Diff compares the desired constraints and indexes with the database's and
// returns one plan over both, which converges in one apply. Index and
// constraint names share one namespace and a constraint's drop removes its
// backing index, so either side's drop can remove what the other side matched;
// one fixed point settles both. See the package doc's Applying a Plan section.
func (a *Adapter) Diff(
	desiredConstraints []Constraint,
	desiredIndexes []Index,
	actualConstraints []RemoteConstraint,
	actualIndexes []RemoteIndex,
	owned *OwnedLabels,
) *DiffResult {
	actualConstraints = slices.Clone(actualConstraints)
	slices.SortFunc(actualConstraints, compareRemoteConstraints)
	actualIndexes = slices.Clone(actualIndexes)
	slices.SortFunc(actualIndexes, compareRemoteIndexes)
	t := newTargets(actualConstraints)

	cs := a.pairConstraints(desiredConstraints, actualConstraints, owned, t)
	is := pairIndexes(desiredIndexes, actualIndexes, owned, t)
	claimed := make(map[string]struct{})
	for _, name := range append(cs.claimed, is.claimed...) {
		claimed[name] = struct{}{}
	}
	cf := newConstraintBlockerFinder(desiredConstraints, actualConstraints, actualIndexes, claimed, t)
	xf := newIndexBlockerFinder(actualIndexes, actualConstraints, claimed, t)
	is.serveFromBackingIndexes(xf, t)

	s := newSettlement()
	enqueue(s, cs.plans, settleRules[Constraint, RemoteConstraint]{
		find: cf.blockers,
		moved: func(rc RemoteConstraint) string {
			return describeConstraint(rc) + " realises this declaration and the plan drops it; this constraint is created again under its own name"
		},
		own: func(rc *RemoteConstraint) blocker { return blocker{constraint: rc} },
	})
	enqueue(s, is.plans, settleRules[Index, RemoteIndex]{
		find: xf.blockers,
		moved: func(ri RemoteIndex) string {
			held := fmt.Sprintf("index %q", ri.Name)
			if ri.Name == "" {
				held = "the unnamed index"
			}
			return held + " realises this declaration and the plan drops it; this index is created again under its own name"
		},
		own: func(ri *RemoteIndex) blocker { return blocker{index: ri} },
	})
	// A Drop removes its row too once applied, so an index served by the backing
	// index of a constraint the plan drops is created again.
	for _, o := range cs.orphans {
		s.remove(t.constraint(o.obj))
	}
	for _, o := range is.orphans {
		s.remove(t.index(o.obj))
	}
	s.run()

	// A row is accounted for when its drop target is a blocker's or a Drop's, so
	// a backing index leaves Excluded with its constraint. No other row shares
	// an index Drop's target.
	removed := maps.Clone(s.blocking)
	for _, o := range cs.orphans {
		removed[t.constraint(o.obj)] = struct{}{}
	}
	return &DiffResult{Constraints: cs.result(s.blocking, removed, t), Indexes: is.result(s.blocking, removed, t)}
}

// constraintSide is the constraint half of a plan while [Adapter.Diff] settles
// it. actual is sorted, and orphans are the owned, declarable rows no
// declaration took.
type constraintSide struct {
	actual  []RemoteConstraint
	plans   []constraintPlan
	orphans []scoped[RemoteConstraint]
	claimed []string
}

// pairConstraints pairs desired constraints with the owned, declarable remote
// ones in phases: name and identity together, then the exact definition, then
// identity alone, then name alone; see [pairObjects]. Constraint names are
// absent under [WithNamedConstraints](false), and every pairing then falls
// through to identity.
//
// Property order is not significant for pairing by identity: a constraint's
// members are a set, so desiredSemanticKey and remoteSemanticKey sort them,
// where an index's properties are a sequence. It is significant for blocking:
// constraintDefinitionKey states the measured server rule, that a plain RANGE
// index, or a UNIQUE or NODE KEY constraint of the other kind, blocks a
// key-backed constraint only in the same property order.
func (a *Adapter) pairConstraints(desired []Constraint, actual []RemoteConstraint, owned *OwnedLabels, t targets) constraintSide {
	side := constraintSide{actual: actual}
	var ownedRemotes []scoped[RemoteConstraint]
	for at, rc := range actual {
		if label, isOwned := owned.ownedLabel(rc.LabelsOrTypes); isOwned && a.declarableRemoteConstraint(rc) {
			ownedRemotes = append(ownedRemotes, scoped[RemoteConstraint]{obj: rc, label: label, at: at})
		}
	}

	positions := make([]int, len(desired))
	for i := range positions {
		positions[i] = i
	}
	pairs, _, orphans := pairObjects(positions, ownedRemotes, identityFuncs[int, scoped[RemoteConstraint]]{
		desiredName:  func(i int) string { return desired[i].Name },
		actualName:   func(o scoped[RemoteConstraint]) string { return o.obj.Name },
		desiredKey:   func(i int) string { return desiredSemanticKey(desired[i]) },
		actualKey:    remoteSemanticKey,
		desiredExact: func(i int) string { return desiredDefinitionKey(desired[i]) },
		actualExact:  remoteDefinitionKey,
	})
	side.orphans = orphans

	side.plans = make([]constraintPlan, len(desired))
	for i, d := range desired {
		side.plans[i] = constraintPlan{desired: d, state: planCreate}
	}
	for _, p := range pairs {
		d, rc := desired[p.Desired], p.Actual.obj
		if rc.Name != "" {
			side.claimed = append(side.claimed, rc.Name)
		}
		cp := &side.plans[p.Desired]
		cp.realisation, cp.at, cp.realised, cp.state = &rc, p.Actual.at, t.constraint(rc), planMatch
		switch {
		// Only a name-only pair can have disagreeing identities; the other phases
		// match on identity by construction. The database already holds the name
		// and every emitted statement carries IF NOT EXISTS, so re-creating it
		// would silently do nothing.
		case p.byName:
			cp.state = planDrift
			cp.ownCause = fmt.Sprintf("constraint %q exists with a different definition: schema %s(%s), database %s(%s)",
				rc.Name, constraintKindToRemoteType(d.Kind), strings.Join(d.Properties, ", "),
				rc.Type, strings.Join(rc.Properties, ", "))

		// A TYPE constraint whose remote property type the server did not report
		// cannot be compared; folding it into Match would report an unchecked
		// constraint as verified. A caller's own query reaches this, never
		// [IntrospectConstraintsQuery].
		case d.Kind == ConstraintType && d.TypeExpr != "" && rc.PropertyType == "":
			cp.state = planUnverified
			cp.unverifiedReason = "database reported no property type; the type expression was not compared"

		// EqualFold: the column and the emitted expression are canonically
		// upper-case, and no two Neo4j type expressions differ only by case.
		case d.Kind == ConstraintType && d.TypeExpr != "" && !strings.EqualFold(d.TypeExpr, rc.PropertyType):
			cp.state = planDrift
			cp.ownCause = fmt.Sprintf("property type mismatch: schema %s, database %s", d.TypeExpr, rc.PropertyType)
		}
	}
	return side
}

// result files every settled plan and accounts for every remote row: a row is a
// Match or Unverified Actual, a Drop, a row whose target removed holds, or
// counted in Excluded. An orphan a drift names is no Drop.
func (side constraintSide) result(blocking, removed map[string]struct{}, t targets) ConstraintDiffResult {
	var r ConstraintDiffResult
	accounted := make([]bool, len(side.actual))
	for _, cp := range side.plans {
		switch cp.state {
		case planMatch:
			r.Match = append(r.Match, ConstraintMatch{Desired: cp.desired, Actual: *cp.realisation})
			accounted[cp.at] = true
		case planUnverified:
			r.Unverified = append(r.Unverified, ConstraintUnverified{Desired: cp.desired, Actual: *cp.realisation, Reason: cp.unverifiedReason})
			accounted[cp.at] = true
		case planCreate:
			r.Create = append(r.Create, cp.desired)
		case planDrift:
			r.Drift = append(r.Drift, constraintDrift(cp))
		}
	}
	for _, o := range side.orphans {
		if _, isBlocker := blocking[t.constraint(o.obj)]; !isBlocker {
			r.Drop = append(r.Drop, o.obj)
			accounted[o.at] = true
		}
	}
	for at, rc := range side.actual {
		if _, gone := removed[t.constraint(rc)]; !accounted[at] && !gone {
			r.Excluded++
		}
	}
	return r
}

type constraintPlan = plan[Constraint, RemoteConstraint]

func constraintDrift(cp constraintPlan) ConstraintDrift {
	drift := ConstraintDrift{Desired: cp.desired}
	var clauses []string
	existence := ""
	for _, b := range cp.blockers {
		b.addTo(&drift.Blockers)
		clauses = append(clauses, b.why)
		if existence == "" && b.constraint != nil {
			existence = nodeKeyExistence(cp.desired, *b.constraint)
		}
	}
	drift.Reason = strings.Join(clauses, "; ") + existence
	return drift
}

// constraintBlockerFinder indexes every remote object that can block a desired
// constraint's create, by the name or the definition it blocks.
type constraintBlockerFinder struct {
	byName       map[string][]RemoteConstraint
	indexByName  map[string][]RemoteIndex
	rangeByDef   map[string][]RemoteIndex
	keyKindByDef map[string][]RemoteConstraint
	// claimed holds the names pairing gave a declaration; see
	// [describeIndexBlocker] for why such a holder gets another message.
	claimed map[string]struct{}
	targets targets
}

func newConstraintBlockerFinder(desired []Constraint, actual []RemoteConstraint, indexes []RemoteIndex, claimed map[string]struct{}, t targets) constraintBlockerFinder {
	f := constraintBlockerFinder{
		byName:       make(map[string][]RemoteConstraint),
		indexByName:  make(map[string][]RemoteIndex),
		rangeByDef:   make(map[string][]RemoteIndex),
		keyKindByDef: make(map[string][]RemoteConstraint),
		claimed:      claimed,
		targets:      t,
	}
	// A key-kind holder blocks only a declaration on its own label, so a row is
	// indexed as one only on a label a key-backed declaration names.
	keyLabels := make(map[string]struct{})
	for _, d := range desired {
		if constraintHasBackingIndex(d.Kind) {
			keyLabels[d.Label] = struct{}{}
		}
	}
	// Names are global, so a constraint on an unowned label, or of a kind this
	// configuration cannot declare, still holds its name against every create.
	for _, rc := range actual {
		if rc.Name != "" {
			f.byName[rc.Name] = append(f.byName[rc.Name], rc)
		}
		// The server refuses a UNIQUE beside a NODE KEY, and the reverse, over the
		// same label and properties in the same order, whether pairing gave the
		// holder to a declaration, left it an orphan, or excluded it.
		if len(rc.LabelsOrTypes) != 1 || (rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE")) {
			continue
		}
		if _, named := keyLabels[rc.LabelsOrTypes[0]]; !named {
			continue
		}
		if kind := canonicalRemoteConstraintType(rc.Type); kind == constraintKindToRemoteType(ConstraintUnique) || kind == constraintKindToRemoteType(ConstraintNodeKey) {
			def := constraintDefinitionKey(rc.LabelsOrTypes[0], rc.Properties)
			f.keyKindByDef[def] = append(f.keyKindByDef[def], rc)
		}
	}
	for _, ri := range indexes {
		// A constraint's backing index carries the constraint's name, and its drop
		// target is the constraint's, so the constraint named first stands for both.
		if ri.Name != "" {
			f.indexByName[ri.Name] = append(f.indexByName[ri.Name], ri)
		}
		// Only a plain RANGE node index over the same single label blocks a
		// key-backed constraint, by the server refusing a constraint whose backing
		// index an equivalent index provides. Measured on 5.26 Enterprise, 2026.05
		// Enterprise and 2026.05 Community: TEXT, POINT, FULLTEXT, VECTOR and any
		// relationship index are accepted beside it, and docs/SPEC.md blesses
		// @fulltext on a sole primary key. An empty column means the record did
		// not come from [IntrospectIndexesQuery]; it is read the permissive way.
		if ri.OwningConstraint != "" || len(ri.LabelsOrTypes) != 1 {
			continue
		}
		if ri.EntityType != "" && !strings.EqualFold(ri.EntityType, "NODE") {
			continue
		}
		if ri.Type != "" && !strings.EqualFold(ri.Type, "RANGE") {
			continue
		}
		def := constraintDefinitionKey(ri.LabelsOrTypes[0], ri.Properties)
		f.rangeByDef[def] = append(f.rangeByDef[def], ri)
	}
	return f
}

// blockers returns every remote object other than d's realisation that keeps
// d's create from taking effect, each with the clause that explains it.
func (f constraintBlockerFinder) blockers(d Constraint) []blocker {
	var out []blocker
	if d.Name != "" {
		for _, rc := range f.byName[d.Name] {
			suffix := "; drop it before this constraint can be created"
			if _, isOurs := f.claimed[rc.Name]; isOurs {
				suffix = " that realises another declaration in this schema; drop it, and that declaration is created again under its own name"
			}
			out = append(out, blocker{constraint: &rc, target: f.targets.constraint(rc), why: fmt.Sprintf("constraint name %q is already held by a %s constraint on %s%s",
				rc.Name, rc.Type, describeLabels(rc.LabelsOrTypes), suffix)})
		}
		for _, ri := range f.indexByName[d.Name] {
			out = append(out, blocker{index: &ri, target: f.targets.index(ri), why: fmt.Sprintf("name %q is already held by a %s index on %s; index and constraint names share one namespace, so drop or rename it before this constraint can be created",
				ri.Name, ri.Type, describeLabels(ri.LabelsOrTypes))})
		}
	}
	if !constraintHasBackingIndex(d.Kind) {
		return out
	}
	def := constraintDefinitionKey(d.Label, d.Properties)
	for _, ri := range f.rangeByDef[def] {
		out = append(out, blocker{index: &ri, target: f.targets.index(ri), why: fmt.Sprintf("a %s index %q already serves %s; the server refuses a constraint whose backing index an equivalent index provides, so drop that index before this constraint can be created",
			ri.Type, ri.Name, describeLabels(ri.LabelsOrTypes))})
	}
	want := constraintKindToRemoteType(d.Kind)
	for _, rc := range f.keyKindByDef[def] {
		if canonicalRemoteConstraintType(rc.Type) == want {
			continue
		}
		out = append(out, blocker{constraint: &rc, target: f.targets.constraint(rc), why: fmt.Sprintf("%s already holds %s(%s); the server refuses a UNIQUE and a NODE KEY constraint over the same label and properties in the same order, so drop it before this constraint can be created",
			describeConstraint(rc), describeLabels(rc.LabelsOrTypes), strings.Join(rc.Properties, ", "))})
	}
	return out
}

// remoteConstraintKey identifies one remote constraint record.
func remoteConstraintKey(rc RemoteConstraint) string {
	return identityKey(append(append(
		[]string{rc.Name, rc.Type, rc.EntityType, rc.PropertyType, strconv.Itoa(len(rc.LabelsOrTypes))},
		rc.LabelsOrTypes...), rc.Properties...)...)
}

// compareRemoteConstraints is the order [Adapter.Diff] reads constraints in, so no result depends on the order the server returned rows in.
func compareRemoteConstraints(x, y RemoteConstraint) int {
	return cmp.Or(
		cmp.Compare(x.Name, y.Name),
		cmp.Compare(x.Type, y.Type),
		cmp.Compare(x.EntityType, y.EntityType),
		slices.Compare(x.LabelsOrTypes, y.LabelsOrTypes),
		slices.Compare(x.Properties, y.Properties),
		cmp.Compare(x.PropertyType, y.PropertyType),
		cmp.Compare(x.CreateStatement, y.CreateStatement),
	)
}

// desiredDefinitionKey is a desired constraint's identity with its properties
// in declared order, which [pairObjects]' exact phase pairs by.
func desiredDefinitionKey(c Constraint) string {
	return identityKey(append([]string{c.Label, constraintKindToRemoteType(c.Kind)}, c.Properties...)...)
}

// remoteDefinitionKey mirrors [desiredDefinitionKey] for a remote constraint,
// its properties in the server's order.
func remoteDefinitionKey(o scoped[RemoteConstraint]) string {
	return identityKey(append([]string{o.label, canonicalRemoteConstraintType(o.obj.Type)}, o.obj.Properties...)...)
}

// nodeKeyExistence is a drift Reason's clause for a NODE KEY whose drop step
// removes a UNIQUE on its label over members within its key, which made the key
// unique: the server refuses the NODE KEY while a node lacks a key property, and
// that refusal then leaves the key unenforced.
func nodeKeyExistence(d Constraint, actual RemoteConstraint) string {
	if d.Kind != ConstraintNodeKey || canonicalRemoteConstraintType(actual.Type) != constraintKindToRemoteType(ConstraintUnique) ||
		!slices.Equal(actual.LabelsOrTypes, []string{d.Label}) || len(actual.Properties) == 0 {
		return ""
	}
	for _, p := range actual.Properties {
		if !slices.Contains(d.Properties, p) {
			return ""
		}
	}
	return "; the NODE KEY also requires every node to hold each key property, and the server refuses it after the drop while a node lacks one, leaving the key without a uniqueness constraint"
}

// describeConstraint names a remote constraint for a drift message, its kind as
// the server reported it.
func describeConstraint(rc RemoteConstraint) string {
	if rc.Name == "" {
		return fmt.Sprintf("an unnamed %s constraint", rc.Type)
	}
	return fmt.Sprintf("a %s constraint %q", rc.Type, rc.Name)
}

// desiredSemanticKey builds the semantic identity of a desired Constraint. A
// constraint's members are a set, so they are sorted.
func desiredSemanticKey(c Constraint) string {
	props := slices.Sorted(slices.Values(c.Properties))
	return identityKey(append(
		[]string{c.Label, constraintKindToRemoteType(c.Kind)}, props...,
	)...)
}

// remoteSemanticKey builds the semantic identity of a remote constraint from
// the label that made it schema-owned, sorting members to mirror
// [desiredSemanticKey].
//
// Type goes through [canonicalRemoteConstraintType] because
// [declarableRemoteConstraint] admits it under the same fold while
// [desiredSemanticKey] embeds the canonical [constraintKindToRemoteType].
// Without the fold here a constraint the declarability gate accepts could never
// pair by identity, so a remote realising a declared constraint would read as an
// unrelated object to drop.
func remoteSemanticKey(o scoped[RemoteConstraint]) string {
	props := slices.Sorted(slices.Values(o.obj.Properties))
	return identityKey(append(
		[]string{o.label, canonicalRemoteConstraintType(o.obj.Type)}, props...,
	)...)
}

// canonicalRemoteConstraintType folds a server-reported constraint type onto the
// spelling [constraintKindToRemoteType] produces, upper-casing on the way.
//
// Neo4j renamed the node uniqueness constraint type between Cypher languages:
// CYPHER 5 reports UNIQUENESS, CYPHER 25 (a new 2026.05 database's default)
// NODE_PROPERTY_UNIQUENESS — aligning it with the NODE_PROPERTY_EXISTENCE and
// NODE_PROPERTY_TYPE spellings it has always used. Both name the same
// constraint and [IntrospectConstraintsQuery] names no language, so every
// comparison against a remote type must accept either. Comparing raw strings
// makes the uniqueness constraints a CYPHER 25 query reports undeclarable, and
// the whole diff then misreports: the actual side excludes
// them, so a named desired constraint finds its name taken and reports the
// schema's OWN constraint as drift, and an unnamed one plans a create of a
// constraint the database already holds.
//
// The fold is for COMPARISON only. [RemoteConstraint.Type] keeps exactly what
// the server reported, because drift messages echo it — naming a constraint kind
// the operator's database does not use would be its own confusion.
func canonicalRemoteConstraintType(remoteType string) string {
	upper := strings.ToUpper(remoteType)
	if canonical, aliased := remoteConstraintTypeAliases[upper]; aliased {
		return canonical
	}
	return upper
}

// remoteConstraintTypeAliases maps a non-canonical server spelling onto the one
// [constraintKindToRemoteType] emits. Keys are upper-case, since the map is
// consulted after upper-casing.
//
// Only NODE constraint spellings belong here. The relationship ones
// (RELATIONSHIP_UNIQUENESS and its siblings) are deliberately absent: folding
// one onto a node kind would make [Adapter.declarableRemoteConstraint] admit a
// relationship constraint, whose LabelsOrTypes holds a relationship TYPE rather
// than a label, so ownership would be decided against the wrong namespace
// entirely.
var remoteConstraintTypeAliases = map[string]string{
	"NODE_PROPERTY_UNIQUENESS": "UNIQUENESS",
}

// constraintKindToRemoteType maps a ConstraintKind to the corresponding
// Neo4j remote constraint type string. //exhaustive:enforce makes a newly added
// kind a CI failure rather than a silent fall-through to
// [unknownRemoteConstraintType], which matches no remote constraint and would
// make every constraint of that kind a perpetual Create paired with a
// perpetual Drop.
func constraintKindToRemoteType(kind ConstraintKind) string {
	//exhaustive:enforce
	switch kind {
	case ConstraintUnique:
		return "UNIQUENESS"
	case ConstraintNotNull:
		return "NODE_PROPERTY_EXISTENCE"
	case ConstraintType:
		return "NODE_PROPERTY_TYPE"
	case ConstraintNodeKey:
		return "NODE_KEY"
	default:
		return unknownRemoteConstraintType
	}
}

// unknownRemoteConstraintType is what [constraintKindToRemoteType] returns for
// a value outside the enum.
const unknownRemoteConstraintType = "UNKNOWN"

// declarableRemoteConstraint reports whether a remote constraint is one THIS
// ADAPTER could have emitted, and therefore one the diff owns.
//
// SHOW CONSTRAINTS reports relationship constraints and node constraint kinds
// the DSL has no vocabulary for (Neo4j 5.26's label-existence constraints, for
// one). Classifying any of those as an undeclared drop reports drift no schema
// edit could resolve. For a relationship constraint `LabelsOrTypes` holds the
// relationship TYPE rather than a label, so ownership is being decided against
// the wrong namespace entirely.
//
// Edition is part of the answer, which is why this is a method. Under
// [Community] the emitter produces UNIQUE alone, so a NOT NULL or TYPE
// constraint in the database is a kind this configuration cannot declare, and
// admitting it would make the desired side (filtered) and the actual side
// (unfiltered) disagree by construction: every one of them becomes an unmatched
// orphan, and the plan tells an operator to drop every required-property and
// property-type guarantee the database has.
//
// An unreported entityType is treated as NODE: the field is absent only where a
// server or fixture does not report it, and every constraint the adapter emits
// is a node constraint. Treating an unreadable entityType as not-a-node would
// silently drop real drift, which is the worse failure — an admitted
// relationship constraint is at most a spurious drop line, a dropped node
// constraint is drift nobody sees.
func (a *Adapter) declarableRemoteConstraint(rc RemoteConstraint) bool {
	if rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE") {
		return false
	}
	// The fold covers case (as [RemoteIndex.Declarable] does — the column is
	// canonically upper-case, but a case difference must not hide a declarable
	// constraint from matching and dropping) and both Cypher languages'
	// spellings of the uniqueness type. Both must be absorbed here and in
	// [remoteSemanticKey] identically, or a constraint admitted by one is
	// unpairable by the other.
	canonical := canonicalRemoteConstraintType(rc.Type)
	for _, k := range a.declarableConstraintKinds() {
		if canonical == constraintKindToRemoteType(k) {
			return true
		}
	}
	return false
}

// declarableConstraintKinds returns the kinds this adapter's configuration
// emits, which is what the diff can own. It mirrors the edition gating in
// [Adapter.ConstraintsStructured]; the two must agree, or the diff compares a
// filtered desired set against an unfiltered actual one.
func (a *Adapter) declarableConstraintKinds() []ConstraintKind {
	if a.config.edition == Community {
		return communityConstraintKinds
	}
	return allConstraintKinds
}

// allConstraintKinds lists every [ConstraintKind], for the code that must
// enumerate the enum rather than switch on one value — notably
// [Adapter.declarableRemoteConstraint]. A kind missing from this list is
// invisible to the diff: its remote constraints are reported neither as matched
// nor as a drop. [TestConstraintKind_EnumerationIsComplete] sweeps a numeric
// range wide enough to find kinds this list omits, wherever they sit.
var allConstraintKinds = []ConstraintKind{
	ConstraintUnique, ConstraintNotNull, ConstraintType, ConstraintNodeKey,
}

// communityConstraintKinds lists the kinds a Community server supports, which
// is the subset [Adapter.ConstraintsStructured] keeps under
// [WithEdition]([Community]).
var communityConstraintKinds = []ConstraintKind{ConstraintUnique}

// constraintDefinitionKey identifies a constraint by what it constrains rather
// than by its name, so an index or a constraint of the other key-backed kind
// holding the same thing under another name is recognisable as a blocker.
//
// The properties are NOT sorted, where [desiredSemanticKey] and
// [remoteSemanticKey] sort theirs. A constraint's members are a set, so
// constraint identity is order-free; but the server refuses a key-backed
// constraint only beside a plain RANGE index in the SAME property order —
// measured on 5.26 Enterprise, 2026.05 Enterprise and 2026.05 Community, for
// UNIQUE and NODE KEY alike, with SHOW INDEXES reporting declaration order —
// and only beside a constraint of the other key-backed kind in that order,
// measured on both Enterprise images. Sorting here would report a
// reversed-order object as a blocker the server never raises, and the plan
// would never converge.
func constraintDefinitionKey(label string, properties []string) string {
	return identityKey(append([]string{label}, properties...)...)
}

// constraintHasBackingIndex reports whether creating this kind creates an index
// the server refuses to duplicate. NOT NULL and TYPE constraints have none, so
// an equivalent index does not block them.
func constraintHasBackingIndex(k ConstraintKind) bool {
	return k == ConstraintUnique || k == ConstraintNodeKey
}

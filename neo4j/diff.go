package neo4j

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// ConstraintDiff is the four-way classification of desired vs actual
// constraints. Drift, Create and Drop are actionable, and a database is in sync
// with the schema's constraints when all three are empty.
type ConstraintDiff struct {
	// Match contains constraints that exist in both schema and database with
	// identical semantic definitions.
	Match []ConstraintMatch

	// Drift contains the desired constraints a CREATE alone cannot realise, in
	// the desired list's order, each with every database object whose drop its
	// create needs. [Plan] says how a plan is applied, and the package
	// doc's Applying a Plan section states each create's data precondition.
	Drift []ConstraintDrift

	// Create contains constraints defined in the schema that are absent from
	// the database and that nothing blocks. A declaration whose create
	// something blocks is a Drift.
	Create []cypher.Constraint

	// Drop contains schema-owned constraints in the database that have no
	// corresponding definition in the schema AND that the schema could have
	// declared. A relationship constraint (whose LabelsOrTypes holds a
	// relationship type, not a label) and a node constraint kind the DSL has no
	// vocabulary for are both left out even on an owned label: neither is
	// something a schema edit could account for, so reporting them would be drift
	// that never clears. This mirrors [IndexDiff.Drop]. See
	// [declarableRemoteConstraint]. A constraint a drift on either side names
	// among its Blockers is not also a Drop. A caller that applies Drops drops
	// them before any create.
	Drop []RemoteConstraint

	// Excluded counts the remote constraints the plan leaves alone: no Match
	// Actual, no Drop, and no row whose drop target a drift on either
	// side names or a Drop removes. Each is on a label the schema does not own,
	// or of a kind this configuration cannot declare.
	//
	// It exists because the four buckets are a statement about what WAS compared,
	// and their being empty is not the same as the database being accounted for.
	// A type deleted or renamed since the last apply is the case that matters: its
	// constraints keep enforcing on a label no type declares any more, so they
	// are never a Drop: they land here, unless one holds a declared name and a
	// drift names it as a blocker.
	// Ownership is derived from the schema, so this is inherent, not a gap to be
	// closed by guessing at labels — see [cypher.OwnedLabels].
	//
	// A caller reporting "in sync" should say how many objects were left out of
	// that claim. It is deliberately not drift: in a database shared with other
	// applications a non-zero count is the normal state.
	Excluded int
}

// ConstraintMatch pairs a desired constraint with its matching actual constraint.
type ConstraintMatch struct {
	Desired cypher.Constraint
	Actual  RemoteConstraint
}

// ConstraintDrift pairs a desired constraint with every database object standing
// in its way.
type ConstraintDrift struct {
	Desired cypher.Constraint

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
// constraint with [cypher.DropConstraintStatement]. Drop an index with
// [cypher.DropIndexStatement], or by dropping its OwningConstraint where it has one.
type Blockers struct {
	Constraints []RemoteConstraint
	Indexes     []RemoteIndex
}

// Plan takes a database's constraints and indexes to what a schema declares. Apply it whole: drop every drift's Blockers on both
// sides and every Drop the caller applies, each drop target once, then create
// every constraint Create and constraint drift's Desired, then every index
// Create and index drift's Desired.
type Plan struct {
	Constraints ConstraintDiff
	Indexes     IndexDiff
}

// Diff plans the database objects constraints and indexes, as SHOW
// CONSTRAINTS and SHOW INDEXES report them, against what s declares under c:
// the desired constraints and indexes and the owned labels all derive from
// (c, s). The plan converges in one apply; the package doc's Applying a Plan
// section says how. A refusal returns a *[diag.ResultError] holding an
// [E_NEO4J_INVALID_REMOTE_OBJECT] for each break in the remote rows that Diff
// cannot plan from, the constraint generation's failures, and the index
// generation's when the constraints' are none; the package doc's Diff Scope
// section states the order. Diff takes [WithWarnings], which receives the
// constraint generation's warnings, and refuses any other option with an error
// naming it.
func Diff(c cypher.Config, s schema.Schema, constraints []RemoteConstraint, indexes []RemoteIndex, opts ...Option) (Plan, error) {
	cfg := newConfig(opts)
	if err := cfg.refuse("Diff", optWarnings); err != nil {
		return Plan{}, err
	}
	collector := diag.NewCollector(diag.NoLimit)
	checkRemote(collector, constraints, indexes)
	desired, err := c.Constraints(s, cypher.WithWarnings(cfg.warnings))
	if err != nil {
		return Plan{}, withFailures(collector, err)
	}
	desiredIndexes, err := c.Indexes(s)
	if err != nil {
		return Plan{}, withFailures(collector, err)
	}
	if err := collector.Err(); err != nil {
		return Plan{}, err
	}
	return diff(c, desired, desiredIndexes, constraints, indexes, c.OwnedLabels(s)), nil
}

// withFailures returns the failures collector holds and those err holds as one
// error. An err that holds no [diag.ResultError] is returned as it is.
func withFailures(collector *diag.Collector, err error) error {
	re, ok := errors.AsType[*diag.ResultError](err)
	if !ok {
		return err
	}
	collector.Merge(re.Result)
	return collector.Err()
}

// checkRemote reports to collector each remote row a plan cannot be made from,
// one [E_NEO4J_INVALID_REMOTE_OBJECT] per break. A plan pairs, blocks and drops
// a row by its name, so each name must pick out one row of its kind; a
// constraint and the index backing it share theirs.
func checkRemote(collector *diag.Collector, constraints []RemoteConstraint, indexes []RemoteIndex) {
	checkNames(collector, "constraint", "constraints", constraints, func(rc RemoteConstraint) string { return rc.Name })
	checkNames(collector, "index", "indexes", indexes, func(ri RemoteIndex) string { return ri.Name })
	for i, rc := range constraints {
		if reportsNoPropertyType(rc) {
			collector.Collect(remoteIssue(fmt.Sprintf("remote constraint %d is a property type constraint that reports no property type", i)))
		}
	}
}

// checkNames reports each row with no name and each name two or more rows
// share, by the rows' positions.
func checkNames[R any](collector *diag.Collector, kind, kinds string, rows []R, name func(R) string) {
	positions := make(map[string][]int, len(rows))
	for i, r := range rows {
		n := name(r)
		if n == "" {
			collector.Collect(remoteIssue(fmt.Sprintf("remote %s %d has no name", kind, i)))
			continue
		}
		positions[n] = append(positions[n], i)
	}
	// Sorted, so the issues arrive in one order on every run.
	for _, n := range slices.Sorted(maps.Keys(positions)) {
		at := positions[n]
		if len(at) < 2 {
			continue
		}
		listed := make([]string, len(at))
		for i, p := range at {
			listed[i] = strconv.Itoa(p)
		}
		last := len(listed) - 1
		collector.Collect(remoteIssue(fmt.Sprintf("remote %s %s and %s share the name %q",
			kinds, strings.Join(listed[:last], ", "), listed[last], n)))
	}
}

func remoteIssue(message string) diag.Issue {
	return diag.NewIssue(diag.Error, E_NEO4J_INVALID_REMOTE_OBJECT, message).Build()
}

// reportsNoPropertyType reports whether rc is a property type constraint whose
// record carries no property type, so the type it enforces cannot be compared.
func reportsNoPropertyType(rc RemoteConstraint) bool {
	return strings.HasSuffix(strings.ToUpper(rc.Type), "_PROPERTY_TYPE") && rc.PropertyType == ""
}

// owner resolves a remote object's labels to the one its schema owns;
// [*cypher.OwnedLabels] is the owner a schema derives.
type owner interface {
	LabelOf(labels []string) (string, bool)
}

// diff compares the desired constraints and indexes with the database's and
// returns one plan over both. Index and constraint names share one namespace
// and a constraint's drop removes its backing index, so either side's drop can
// remove what the other side matched; one fixed point settles both.
func diff(
	c cypher.Config,
	desiredConstraints []cypher.Constraint,
	desiredIndexes []cypher.Index,
	actualConstraints []RemoteConstraint,
	actualIndexes []RemoteIndex,
	owned owner,
) Plan {
	// Names are unique within a kind, so ordering by name makes no result
	// depend on the order the rows came in.
	actualConstraints = slices.SortedFunc(slices.Values(actualConstraints), func(x, y RemoteConstraint) int { return cmp.Compare(x.Name, y.Name) })
	actualIndexes = slices.SortedFunc(slices.Values(actualIndexes), func(x, y RemoteIndex) int { return cmp.Compare(x.Name, y.Name) })

	cs := pairConstraints(c, desiredConstraints, actualConstraints, owned)
	is := pairIndexes(desiredIndexes, actualIndexes, owned)
	claimed := make(map[string]struct{})
	for _, name := range append(cs.claimed, is.claimed...) {
		claimed[name] = struct{}{}
	}
	cf := newConstraintBlockerFinder(desiredConstraints, actualConstraints, actualIndexes, claimed)
	xf := newIndexBlockerFinder(actualIndexes, actualConstraints, claimed)
	is.serveFromBackingIndexes(xf)

	st := newSettlement()
	enqueue(st, cs.plans, settleRules[cypher.Constraint, RemoteConstraint]{
		find: cf.blockers,
		moved: func(rc RemoteConstraint) string {
			return describeConstraint(rc) + " realises this declaration and the plan drops it; this constraint is created again under its own name"
		},
		own: func(rc *RemoteConstraint) blocker { return blocker{constraint: rc} },
	})
	enqueue(st, is.plans, settleRules[cypher.Index, RemoteIndex]{
		find: xf.blockers,
		moved: func(ri RemoteIndex) string {
			return fmt.Sprintf("index %q realises this declaration and the plan drops it; this index is created again under its own name", ri.Name)
		},
		own: func(ri *RemoteIndex) blocker { return blocker{index: ri} },
	})
	// A Drop removes its row too once applied, so an index served by the backing
	// index of a constraint the plan drops is created again.
	for _, o := range cs.orphans {
		st.remove(constraintTarget(o.obj))
	}
	for _, o := range is.orphans {
		st.remove(indexTarget(o.obj))
	}
	st.run()

	// A row is accounted for when its drop target is a blocker's or a Drop's, so
	// a backing index leaves Excluded with its constraint. No other row shares
	// an index Drop's target.
	removed := maps.Clone(st.blocking)
	for _, o := range cs.orphans {
		removed[constraintTarget(o.obj)] = struct{}{}
	}
	return Plan{Constraints: cs.result(st.blocking, removed), Indexes: is.result(st.blocking, removed)}
}

// constraintSide is the constraint half of a plan while [diff] settles
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
// identity alone, then name alone; see [pairObjects].
//
// Property order is not significant for pairing by identity: a constraint's
// members are a set, so desiredSemanticKey and remoteSemanticKey sort them,
// where an index's properties are a sequence. It is significant for blocking:
// constraintDefinitionKey states the measured server rule, that a plain RANGE
// index, or a UNIQUE or NODE KEY constraint of the other kind, blocks a
// key-backed constraint only in the same property order.
func pairConstraints(c cypher.Config, desired []cypher.Constraint, actual []RemoteConstraint, owned owner) constraintSide {
	side := constraintSide{actual: actual}
	var ownedRemotes []scoped[RemoteConstraint]
	for at, rc := range actual {
		if label, isOwned := owned.LabelOf(rc.LabelsOrTypes); isOwned && declarableRemoteConstraint(c, rc) {
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
		side.claimed = append(side.claimed, rc.Name)
		cp := &side.plans[p.Desired]
		cp.realisation, cp.at, cp.realised, cp.state = &rc, p.Actual.at, constraintTarget(rc), planMatch
		switch {
		// Only a name-only pair can have disagreeing identities; the other phases
		// match on identity by construction. The database already holds the name
		// and every emitted statement carries IF NOT EXISTS, so re-creating it
		// would silently do nothing.
		case p.byName:
			cp.state = planDrift
			cp.ownCause = fmt.Sprintf("constraint %q exists with a different definition: schema %s(%s), database %s(%s)",
				rc.Name, d.Kind, strings.Join(d.Properties, ", "),
				rc.Type, strings.Join(rc.Properties, ", "))

		// EqualFold: the column and the emitted expression are canonically
		// upper-case, and no two Neo4j type expressions differ only by case.
		case d.Kind == cypher.ConstraintType && !strings.EqualFold(d.TypeExpr, rc.PropertyType):
			cp.state = planDrift
			cp.ownCause = fmt.Sprintf("property type mismatch: schema %s, database %s", d.TypeExpr, rc.PropertyType)
		}
	}
	return side
}

// result files every settled plan and accounts for every remote row: a row is a
// Match Actual, a Drop, a row whose target removed holds, or
// counted in Excluded. An orphan a drift names is no Drop.
func (side constraintSide) result(blocking, removed map[string]struct{}) ConstraintDiff {
	var r ConstraintDiff
	accounted := make([]bool, len(side.actual))
	for _, cp := range side.plans {
		switch cp.state {
		case planMatch:
			r.Match = append(r.Match, ConstraintMatch{Desired: cp.desired, Actual: *cp.realisation})
			accounted[cp.at] = true
		case planCreate:
			r.Create = append(r.Create, cp.desired)
		case planDrift:
			r.Drift = append(r.Drift, constraintDrift(cp))
		}
	}
	for _, o := range side.orphans {
		if _, isBlocker := blocking[constraintTarget(o.obj)]; !isBlocker {
			r.Drop = append(r.Drop, o.obj)
			accounted[o.at] = true
		}
	}
	for at, rc := range side.actual {
		if _, gone := removed[constraintTarget(rc)]; !accounted[at] && !gone {
			r.Excluded++
		}
	}
	return r
}

type constraintPlan = plan[cypher.Constraint, RemoteConstraint]

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
	byName       map[string]RemoteConstraint
	indexByName  map[string]RemoteIndex
	rangeByDef   map[string][]RemoteIndex
	keyKindByDef map[string][]RemoteConstraint
	// claimed holds the names pairing gave a declaration; see
	// [describeIndexBlocker] for why such a holder gets another message.
	claimed map[string]struct{}
}

func newConstraintBlockerFinder(desired []cypher.Constraint, actual []RemoteConstraint, indexes []RemoteIndex, claimed map[string]struct{}) constraintBlockerFinder {
	f := constraintBlockerFinder{
		byName:       make(map[string]RemoteConstraint, len(actual)),
		indexByName:  make(map[string]RemoteIndex, len(indexes)),
		rangeByDef:   make(map[string][]RemoteIndex),
		keyKindByDef: make(map[string][]RemoteConstraint),
		claimed:      claimed,
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
		f.byName[rc.Name] = rc
		// The server refuses a UNIQUE beside a NODE KEY, and the reverse, over the
		// same label and properties in the same order, whether pairing gave the
		// holder to a declaration, left it an orphan, or excluded it.
		if len(rc.LabelsOrTypes) != 1 || (rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE")) {
			continue
		}
		if _, named := keyLabels[rc.LabelsOrTypes[0]]; !named {
			continue
		}
		if kind := canonicalRemoteConstraintType(rc.Type); kind == mapping.Uniqueness || kind == mapping.NodeKey {
			def := constraintDefinitionKey(rc.LabelsOrTypes[0], rc.Properties)
			f.keyKindByDef[def] = append(f.keyKindByDef[def], rc)
		}
	}
	for _, ri := range indexes {
		// A constraint's backing index carries the constraint's name, and its drop
		// target is the constraint's, so the constraint named first stands for both.
		f.indexByName[ri.Name] = ri
		// Only a plain RANGE node index over the same single label blocks a
		// key-backed constraint, by the server refusing a constraint whose backing
		// index an equivalent index provides. Measured on 5.26 Enterprise, 2026.05
		// Enterprise and 2026.05 Community: TEXT, POINT, FULLTEXT, VECTOR and any
		// relationship index are accepted beside it, and docs/SPEC.md blesses
		// @fulltext on a sole primary key. An empty column means the record did
		// not come from [ShowIndexes]; it is read the permissive way.
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
func (f constraintBlockerFinder) blockers(d cypher.Constraint) []blocker {
	var out []blocker
	if rc, held := f.byName[d.Name]; held {
		suffix := "; drop it before this constraint can be created"
		if _, isOurs := f.claimed[rc.Name]; isOurs {
			suffix = " that realises another declaration in this schema; drop it, and that declaration is created again under its own name"
		}
		out = append(out, blocker{constraint: &rc, target: constraintTarget(rc), why: fmt.Sprintf("constraint name %q is already held by a %s constraint on %s%s",
			rc.Name, rc.Type, describeLabels(rc.LabelsOrTypes), suffix)})
	}
	if ri, held := f.indexByName[d.Name]; held {
		out = append(out, blocker{index: &ri, target: indexTarget(ri), why: fmt.Sprintf("name %q is already held by a %s index on %s; index and constraint names share one namespace, so drop or rename it before this constraint can be created",
			ri.Name, ri.Type, describeLabels(ri.LabelsOrTypes))})
	}
	if !constraintHasBackingIndex(d.Kind) {
		return out
	}
	def := constraintDefinitionKey(d.Label, d.Properties)
	for _, ri := range f.rangeByDef[def] {
		out = append(out, blocker{index: &ri, target: indexTarget(ri), why: fmt.Sprintf("a %s index %q already serves %s; the server refuses a constraint whose backing index an equivalent index provides, so drop that index before this constraint can be created",
			ri.Type, ri.Name, describeLabels(ri.LabelsOrTypes))})
	}
	want := d.Kind.String()
	for _, rc := range f.keyKindByDef[def] {
		if canonicalRemoteConstraintType(rc.Type) == want {
			continue
		}
		out = append(out, blocker{constraint: &rc, target: constraintTarget(rc), why: fmt.Sprintf("%s already holds %s(%s); the server refuses a UNIQUE and a NODE KEY constraint over the same label and properties in the same order, so drop it before this constraint can be created",
			describeConstraint(rc), describeLabels(rc.LabelsOrTypes), strings.Join(rc.Properties, ", "))})
	}
	return out
}

// desiredDefinitionKey is a desired constraint's identity with its properties
// in declared order, which [pairObjects]' exact phase pairs by.
func desiredDefinitionKey(d cypher.Constraint) string {
	return mapping.Key(append([]string{d.Label, d.Kind.String()}, d.Properties...)...)
}

// remoteDefinitionKey mirrors [desiredDefinitionKey] for a remote constraint,
// its properties in the server's order.
func remoteDefinitionKey(o scoped[RemoteConstraint]) string {
	return mapping.Key(append([]string{o.label, canonicalRemoteConstraintType(o.obj.Type)}, o.obj.Properties...)...)
}

// nodeKeyExistence is a drift Reason's clause for a NODE KEY whose drop step
// removes a UNIQUE on its label over members within its key, which made the key
// unique: the server refuses the NODE KEY while a node lacks a key property, and
// that refusal then leaves the key unenforced.
func nodeKeyExistence(d cypher.Constraint, actual RemoteConstraint) string {
	if d.Kind != cypher.ConstraintNodeKey || canonicalRemoteConstraintType(actual.Type) != mapping.Uniqueness ||
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
	return fmt.Sprintf("a %s constraint %q", rc.Type, rc.Name)
}

// desiredSemanticKey builds the semantic identity of a desired constraint. A
// constraint's members are a set, so they are sorted.
func desiredSemanticKey(d cypher.Constraint) string {
	return mapping.ConstraintKey(d.Label, d.Kind.String(), d.Properties)
}

// remoteSemanticKey builds the semantic identity of a remote constraint from
// the label that made it schema-owned, sorting members to mirror
// [desiredSemanticKey].
//
// Type goes through [canonicalRemoteConstraintType] because
// [declarableRemoteConstraint] admits it under the same fold while
// [desiredSemanticKey] embeds the canonical remote type.
// Without the fold here a constraint the declarability gate accepts could never
// pair by identity, so a remote realising a declared constraint would read as an
// unrelated object to drop.
func remoteSemanticKey(o scoped[RemoteConstraint]) string {
	return mapping.ConstraintKey(o.label, canonicalRemoteConstraintType(o.obj.Type), o.obj.Properties)
}

// canonicalRemoteConstraintType folds a server-reported constraint type onto the
// spelling [cypher.ConstraintKind.String] produces, upper-casing on the way.
//
// Neo4j renamed the node uniqueness constraint type between Cypher languages:
// CYPHER 5 reports UNIQUENESS, CYPHER 25 (a new 2026.05 database's default)
// NODE_PROPERTY_UNIQUENESS — aligning it with the NODE_PROPERTY_EXISTENCE and
// NODE_PROPERTY_TYPE spellings it has always used. Both name the same
// constraint and [ShowConstraints] names no language, so every
// comparison against a remote type must accept either. Comparing raw strings
// makes the uniqueness constraints a CYPHER 25 query reports undeclarable, and
// the whole diff then misreports: the actual side excludes
// them, so a desired constraint finds its name taken and reports the schema's
// OWN constraint as drift.
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
// [cypher.ConstraintKind.String] emits. Keys are upper-case, since the map is
// consulted after upper-casing.
//
// Only NODE constraint spellings belong here. The relationship ones
// (RELATIONSHIP_UNIQUENESS and its siblings) are deliberately absent: folding
// one onto a node kind would make [declarableRemoteConstraint] admit a
// relationship constraint, whose LabelsOrTypes holds a relationship TYPE rather
// than a label, so ownership would be decided against the wrong namespace
// entirely.
var remoteConstraintTypeAliases = map[string]string{
	"NODE_PROPERTY_UNIQUENESS": "UNIQUENESS",
}

// declarableRemoteConstraint reports whether a remote constraint is one c
// could have emitted, and therefore one the diff owns.
//
// SHOW CONSTRAINTS reports relationship constraints and node constraint kinds
// the DSL has no vocabulary for (Neo4j 5.26's label-existence constraints, for
// one). Classifying any of those as an undeclared drop reports drift no schema
// edit could resolve. For a relationship constraint `LabelsOrTypes` holds the
// relationship TYPE rather than a label, so ownership is being decided against
// the wrong namespace entirely.
//
// Edition is part of the answer, which is why it takes c. Under
// [cypher.Community] the emitter produces UNIQUE alone, so a NOT NULL or TYPE
// constraint in the database is a kind this configuration cannot declare, and
// admitting it would make the desired side (filtered) and the actual side
// (unfiltered) disagree by construction: every one of them becomes an unmatched
// orphan, and the plan tells an operator to drop every required-property and
// property-type guarantee the database has.
//
// An unreported entityType is treated as NODE: the field reads empty where a
// server or fixture does not report it or reports it in a shape the parser
// cannot read, and every constraint the generator emits is a node constraint.
// Treating an unreadable entityType as not-a-node would silently drop real
// drift, which is the worse failure — an admitted relationship constraint is at
// most a spurious drop line, or a false match where its type name equals an
// owned label, while a dropped node constraint is drift nobody sees.
func declarableRemoteConstraint(c cypher.Config, rc RemoteConstraint) bool {
	if rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE") {
		return false
	}
	// The fold covers case (as [RemoteIndex.Declarable] does — the column is
	// canonically upper-case, but a case difference must not hide a declarable
	// constraint from matching and dropping) and both Cypher languages'
	// spellings of the uniqueness type. Both must be absorbed here and in
	// [remoteSemanticKey] identically, or a constraint admitted by one is
	// unpairable by the other.
	return slices.Contains(mapping.DeclarableConstraintTypes(c.Edition == cypher.Community), canonicalRemoteConstraintType(rc.Type))
}

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
	return mapping.Key(append([]string{label}, properties...)...)
}

// constraintHasBackingIndex reports whether creating this kind creates an index
// the server refuses to duplicate. NOT NULL and TYPE constraints have none, so
// an equivalent index does not block them.
func constraintHasBackingIndex(k cypher.ConstraintKind) bool {
	return k == cypher.ConstraintUnique || k == cypher.ConstraintNodeKey
}

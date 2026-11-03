package neo4j

import (
	"fmt"
	"slices"
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

	// Drift contains database objects, constraints or indexes, that do not enforce
	// what the schema declares or keep a declaration from being created, and that
	// a CREATE cannot fix. [ConstraintDrift.Actual]
	// names its five producers and how to tell an index from a constraint before
	// issuing a DROP. A drift whose Desired takes a name follows every drift that
	// drops by that name, save in a cycle of names: there the cycle's first drift
	// comes first, and its create finds the name still held. Applied in order, each
	// as a drop and a create, no drop removes what an earlier create made.
	Drift []ConstraintDrift

	// Create contains constraints defined in the schema that are absent from
	// the database and that nothing blocks. Where nothing realises a declaration,
	// one whose name or key another object holds, or whose definition a RANGE
	// index serves, is a Drift.
	Create []Constraint

	// Drop contains schema-owned constraints in the database that have no
	// corresponding definition in the schema AND that the schema could have
	// declared. A relationship constraint (whose LabelsOrTypes holds a
	// relationship type, not a label) and a node constraint kind the DSL has no
	// vocabulary for are both left out even on an owned label: neither is
	// something a schema edit could account for, so reporting them would be drift
	// that never clears. This mirrors [IndexDiffResult.Drop]. See
	// [declarableRemoteConstraint]. A constraint a drift names as its Actual is
	// not also a Drop.
	Drop []RemoteConstraint

	// Unverified contains constraints that exist in both by identity but whose
	// full definition could not be compared because the database did not report
	// what was needed. They are neither confirmed in sync nor confirmed drifted;
	// folding them into Match would report an unchecked constraint as verified.
	Unverified []ConstraintUnverified

	// Excluded counts the remote constraints the diff sets aside because the
	// schema owns no label they carry, or they are of a kind this configuration
	// cannot declare. One can still stand as a drift entry's Actual.
	//
	// It exists because the five buckets are a statement about what WAS compared,
	// and their being empty is not the same as the database being accounted for.
	// A type deleted or renamed since the last apply is the case that matters: its
	// constraints keep enforcing on a label no type declares any more, so nothing
	// in the current schema can name them and they land here rather than in Drop.
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

// ConstraintDrift pairs a desired constraint with the database object standing
// in its way.
type ConstraintDrift struct {
	Desired Constraint

	// Actual is the database object to remove before Desired can be realised, from
	// one of five producers: a TYPE constraint whose enforced type differs; a
	// constraint holding Desired's name under another definition; and, where
	// nothing realises Desired, what blocks its CREATE — the holder of its name, a
	// RANGE index serving its label and properties in its order, or a UNIQUE or
	// NODE KEY constraint of the other kind over them in that order. A blocker may be
	// unowned, undeclarable, or an index, since index and constraint names share
	// one namespace. An empty Type with a non-empty Name marks an index: remove it
	// with DROP INDEX, and read only its Name, EntityType, LabelsOrTypes and Properties.
	Actual RemoteConstraint

	Reason string // Human-readable drift description
}

// DiffConstraints performs a semantic diff between desired schema constraints
// and actual database constraints.
//
// Pairing considers only remote constraints on labels the schema owns — see
// [Adapter.OwnedLabels], which the caller builds once and passes in. An unowned
// remote constraint whose name an unmatched desired constraint holds is reported
// as drift with that constraint as Actual. Desired
// constraints are matched to remote ones in three phases: name and identity
// together, then identity alone, then name alone; see [pairObjects] for why that
// order matters. Constraint names are absent under [WithNamedConstraints](false),
// in which case every pairing falls through to identity.
//
// Property order is not significant for PAIRING: a constraint's members are a
// set, so desiredSemanticKey and remoteSemanticKey sort them, where
// [Adapter.DiffIndexes] compares an index's properties as a sequence. It IS
// significant for BLOCKING: constraintDefinitionKey states the measured server
// rule, that a plain RANGE index, or a UNIQUE or NODE KEY constraint of the
// other kind, blocks a key-backed constraint only in the same property order,
// so it does not sort and a blocker is matched in DECLARED order. A drift names
// one blocker, the first of its name's holder, a RANGE index over it and a
// constraint of the other key kind. While a second blocker stands, the server
// refuses the create, and the next diff names that blocker.
// `yammm neo4j diff` passes every index it read as alsoBlocking, so that path
// runs unless the index read fails.
func (a *Adapter) DiffConstraints(
	desired []Constraint,
	actual []RemoteConstraint,
	owned *OwnedLabels,
	alsoBlocking ...RemoteIndex,
) *ConstraintDiffResult {
	result := &ConstraintDiffResult{}

	// Constraint and index names share ONE namespace: a constraint's backing
	// index carries the constraint's name, and a plain index can hold a name a
	// CREATE CONSTRAINT wants. A caller that introspected indexes passes them as
	// alsoBlocking so a constraint whose name an index already holds is reported
	// as drift naming the holder, instead of a Create the server refuses on
	// every run. Omitting them is weaker: such a constraint then reports as that
	// Create.
	blockingIndexes := make(map[string]RemoteIndex, len(alsoBlocking))
	// A PLAIN index over the same label and properties blocks a key-backed
	// constraint too, under any name: the server refuses to create a constraint
	// whose backing index an equivalent index already provides. Only the name
	// case was tested, so such a constraint reported as a Create the server
	// declines on every run — a plan that never converges. The index side has
	// modelled this as a definition block all along; see [indexCreateBlocker].
	blockingDefinitions := make(map[string]RemoteIndex, len(alsoBlocking))
	for _, ri := range alsoBlocking {
		if ri.Name != "" {
			if _, taken := blockingIndexes[ri.Name]; !taken {
				blockingIndexes[ri.Name] = ri
			}
		}
		// A backing index is not an independent blocker: it exists because its
		// constraint does, and that constraint is matched or dropped on its own.
		// Multi-label rows are skipped for the reason the index side skips them.
		if ri.OwningConstraint != "" || len(ri.LabelsOrTypes) != 1 {
			continue
		}
		// Only a NODE index over the same label can serve a node constraint's
		// backing index, and only a RANGE one: a uniqueness constraint backs
		// itself with a range index, and the server refuses only a duplicate of
		// THAT. Measured on 5.26 Enterprise, 2026.05 Enterprise and 2026.05
		// Community -- RANGE is
		// refused ("a constraint cannot be created until the index is
		// dropped"), while TEXT, POINT, FULLTEXT, VECTOR and any relationship
		// index are accepted alongside it. Blocking on those reported permanent
		// drift for a schema the server would have taken, and docs/SPEC.md
		// blesses the colliding shape: @fulltext on a sole primary key is "a
		// distinct, legitimate object". An empty column means the record did
		// not come from [IntrospectIndexesQuery]; it is read the permissive way
		// the index side reads it.
		if ri.EntityType != "" && !strings.EqualFold(ri.EntityType, "NODE") {
			continue
		}
		if ri.Type != "" && !strings.EqualFold(ri.Type, "RANGE") {
			continue
		}
		def := constraintDefinitionKey(ri.LabelsOrTypes[0], ri.Properties)
		if _, taken := blockingDefinitions[def]; !taken {
			blockingDefinitions[def] = ri
		}
	}

	// Keep each owned remote constraint alongside the label that made it owned,
	// so its identity is built from the same label ownership was decided by.
	// Names are collected from EVERY remote constraint, before ownership or
	// declarability filtering: a constraint name is global, so one on a label
	// this schema does not own — or of a kind it cannot declare — still makes a
	// CREATE under that name a silent no-op. Mirrors [Adapter.DiffIndexes].
	byName := make(map[string]RemoteConstraint, len(actual))
	for _, rc := range actual {
		if rc.Name == "" {
			continue
		}
		if _, taken := byName[rc.Name]; !taken {
			byName[rc.Name] = rc
		}
	}

	var ownedRemotes []scoped[RemoteConstraint]
	// A key-kind holder blocks only a declaration on its own label, so an excluded
	// row is kept as a candidate only on a label a key-backed declaration names.
	keyLabels := make(map[string]struct{})
	for _, d := range desired {
		if constraintHasBackingIndex(d.Kind) {
			keyLabels[d.Label] = struct{}{}
		}
	}
	var excludedCandidates []RemoteConstraint
	for _, rc := range actual {
		label, isOwned := owned.ownedLabel(rc.LabelsOrTypes)
		if !isOwned || !a.declarableRemoteConstraint(rc) {
			result.Excluded++
			if len(rc.LabelsOrTypes) == 1 {
				if _, named := keyLabels[rc.LabelsOrTypes[0]]; named {
					excludedCandidates = append(excludedCandidates, rc)
				}
			}
			continue
		}
		ownedRemotes = append(ownedRemotes, scopedObject(rc, label))
	}

	pairs, unmatched, orphans := pairObjects(desired, ownedRemotes, identityFuncs[Constraint, scoped[RemoteConstraint]]{
		desiredName: func(d Constraint) string { return d.Name },
		actualName:  func(o scoped[RemoteConstraint]) string { return o.obj.Name },
		desiredKey:  desiredSemanticKey,
		actualKey:   remoteSemanticKey,
	})

	for _, p := range pairs {
		d, rc := p.Desired, p.Actual.obj

		// Only a name-only pair can have disagreeing identities; the other two
		// phases match on identity by construction. The database already holds
		// the name and every emitted statement carries IF NOT EXISTS, so
		// re-creating it would silently do nothing.
		if p.byName {
			result.Drift = append(result.Drift, ConstraintDrift{
				Desired: d,
				Actual:  rc,
				Reason: fmt.Sprintf("constraint %q exists with a different definition: schema %s(%s), database %s(%s)",
					rc.Name, constraintKindToRemoteType(d.Kind), strings.Join(d.Properties, ", "),
					rc.Type, strings.Join(rc.Properties, ", ")),
			})
			continue
		}

		// A TYPE constraint whose remote property type the server did not report
		// cannot be compared. Folding it into Match would report an unchecked
		// constraint as verified — the same false in-sync claim
		// [IndexDiffResult.Unverified] exists to prevent on the index side.
		//
		// Reachable when the records came from a query that does not yield
		// propertyType, i.e. a caller feeding [ParseRemoteConstraints] from its own
		// introspection rather than [IntrospectConstraintsQuery]. A server too old
		// to report the column is also too old to hold the constraint, so that
		// pairing does not arise: the desired constraint simply lands in Create.
		if d.Kind == ConstraintType && d.TypeExpr != "" && rc.PropertyType == "" {
			result.Unverified = append(result.Unverified, ConstraintUnverified{
				Desired: d,
				Actual:  rc,
				Reason:  "database reported no property type; the type expression was not compared",
			})
			continue
		}

		// Check for drift (type expression mismatch on TYPE constraints).
		//
		// EqualFold for the reason every other server-string comparison in this
		// package folds: the column is canonically upper-case, and so is the
		// emitted expression, but a case difference is not a difference in the
		// enforced type — reporting one as drift would tell an operator to
		// replace a constraint that already enforces exactly what the schema
		// declares. No two Neo4j type expressions differ only by case, so the
		// fold cannot conflate distinct types.
		if d.Kind == ConstraintType && d.TypeExpr != "" && rc.PropertyType != "" {
			if !strings.EqualFold(d.TypeExpr, rc.PropertyType) {
				result.Drift = append(result.Drift, ConstraintDrift{
					Desired: d,
					Actual:  rc,
					Reason:  fmt.Sprintf("property type mismatch: schema %s, database %s", d.TypeExpr, rc.PropertyType),
				})
				continue
			}
		}

		result.Match = append(result.Match, ConstraintMatch{
			Desired: d,
			Actual:  rc,
		})
	}

	// Names pairing already handed to a declaration; see [describeIndexBlocker]
	// for why a blocker in this set gets a different message.
	claimed := make(map[string]struct{}, len(pairs))
	for _, p := range pairs {
		if n := p.Actual.obj.Name; n != "" {
			claimed[n] = struct{}{}
		}
	}

	keyHolders := newKeyKindHolders(pairs, orphans, excludedCandidates)

	// As on the index side, a desired constraint whose name the database already
	// holds cannot be created: the statement carries IF NOT EXISTS and is
	// skipped, so reporting a Create yields a plan that repeats unchanged.
	for _, d := range unmatched {
		if blocker, taken := byName[d.Name]; taken && d.Name != "" {
			suffix := "; drop it before this constraint can be created"
			if _, isOurs := claimed[blocker.Name]; isOurs {
				suffix = " that realises another declaration in this schema; drop it — the next apply recreates it under its own name"
			}
			result.Drift = append(result.Drift, ConstraintDrift{
				Desired: d,
				Actual:  blocker,
				Reason: fmt.Sprintf("constraint name %q is already held by a %s constraint on %s%s",
					blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes), suffix),
			})
			continue
		}
		// The holder is an INDEX. Actual carries the fields an index and a
		// constraint share, and leaves Type empty — which is the documented
		// discriminator telling a consumer to issue DROP INDEX, not DROP
		// CONSTRAINT. See [ConstraintDrift.Actual].
		if blocker, taken := blockingIndexes[d.Name]; taken && d.Name != "" {
			result.Drift = append(result.Drift, ConstraintDrift{
				Desired: d,
				Actual: RemoteConstraint{
					Name:          blocker.Name,
					EntityType:    blocker.EntityType,
					LabelsOrTypes: blocker.LabelsOrTypes,
					Properties:    blocker.Properties,
				},
				Reason: fmt.Sprintf("name %q is already held by a %s index on %s; index and constraint names share one namespace, so drop or rename it before this constraint can be created",
					blocker.Name, blocker.Type, describeLabels(blocker.LabelsOrTypes)),
			})
			continue
		}
		if blocker, blocked := blockingDefinitions[constraintDefinitionKey(d.Label, d.Properties)]; blocked && constraintHasBackingIndex(d.Kind) {
			result.Drift = append(result.Drift, ConstraintDrift{
				Desired: d,
				Actual: RemoteConstraint{
					Name:          blocker.Name,
					EntityType:    blocker.EntityType,
					LabelsOrTypes: blocker.LabelsOrTypes,
					Properties:    blocker.Properties,
				},
				Reason: fmt.Sprintf("a %s index %q already serves %s; the server refuses a constraint whose backing index an equivalent index provides, so drop that index before this constraint can be created",
					blocker.Type, blocker.Name, describeLabels(blocker.LabelsOrTypes)),
			})
			continue
		}
		if blocker, found := keyHolders.blocking(d); found {
			result.Drift = append(result.Drift, ConstraintDrift{
				Desired: d,
				Actual:  blocker,
				Reason: fmt.Sprintf("%s already holds %s(%s); the server refuses a UNIQUE and a NODE KEY constraint over the same label and properties in the same order, so drop it before this constraint can be created",
					describeConstraint(blocker), describeLabels(blocker.LabelsOrTypes), strings.Join(blocker.Properties, ", ")),
			})
			continue
		}
		result.Create = append(result.Create, d)
	}
	for i, o := range orphans {
		if !keyHolders.heldOut[i] {
			result.Drop = append(result.Drop, o.obj)
		}
	}
	for i, d := range result.Drift {
		result.Drift[i].Reason += nodeKeyExistence(d.Desired, d.Actual)
	}
	result.Drift = orderDriftsForApply(result.Drift)

	return result
}

// orderDriftsForApply orders the drifts so that a drift whose Desired takes a
// name follows every drift that drops a constraint by that name. Otherwise the
// later drop removes the constraint the earlier create made, or the create runs
// while the name is still held and IF NOT EXISTS skips it. Among the drifts free
// to run, the earliest goes first; a cycle of names, which only operator-made
// names form, is broken at its earliest drift. The input is returned unchanged
// when no drift moves.
func orderDriftsForApply(drifts []ConstraintDrift) []ConstraintDrift {
	dropsOf := make(map[string][]int, len(drifts))
	createsOf := make(map[string][]int, len(drifts))
	for i, d := range drifts {
		if d.Actual.Name != "" {
			dropsOf[d.Actual.Name] = append(dropsOf[d.Actual.Name], i)
		}
		if d.Desired.Name != "" {
			createsOf[d.Desired.Name] = append(createsOf[d.Desired.Name], i)
		}
	}
	waits := make([]int, len(drifts))
	moved := false
	for i, d := range drifts {
		for _, j := range dropsOf[d.Desired.Name] {
			if j != i {
				waits[i]++
				moved = moved || j > i
			}
		}
	}
	if !moved {
		return drifts
	}
	var ready indexHeap
	for i := range drifts {
		if waits[i] == 0 {
			ready.push(i)
		}
	}
	out := make([]ConstraintDrift, 0, len(drifts))
	placed := make([]bool, len(drifts))
	unplaced := 0
	for len(out) < len(drifts) {
		next := -1
		for len(ready) > 0 && next < 0 {
			if i := ready.pop(); !placed[i] {
				next = i
			}
		}
		if next < 0 {
			for placed[unplaced] {
				unplaced++
			}
			next = unplaced
		}
		placed[next] = true
		out = append(out, drifts[next])
		// A placed drift can reach zero here only after a cycle placed it early;
		// pop skips it.
		for _, i := range createsOf[drifts[next].Actual.Name] {
			if waits[i]--; waits[i] == 0 {
				ready.push(i)
			}
		}
	}
	return out
}

// indexHeap is a min-heap of drift positions.
type indexHeap []int

func (h *indexHeap) push(i int) {
	*h = append(*h, i)
	for c := len(*h) - 1; c > 0; {
		p := (c - 1) / 2
		if (*h)[p] <= (*h)[c] {
			break
		}
		(*h)[p], (*h)[c] = (*h)[c], (*h)[p]
		c = p
	}
}

func (h *indexHeap) pop() int {
	old := *h
	top, n := old[0], len(old)-1
	old[0] = old[n]
	*h = old[:n]
	for p := 0; ; {
		c := 2*p + 1
		if c >= n {
			break
		}
		if c+1 < n && old[c+1] < old[c] {
			c++
		}
		if old[p] <= old[c] {
			break
		}
		old[p], old[c] = old[c], old[p]
		p = c
	}
	return top
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

// keyKindHolders indexes the remote UNIQUE and NODE KEY constraints on a
// key-backed declaration's label by label and members in their order. The server
// refuses beside one whether pairing gave it to another declaration, left it an
// orphan, or excluded it, as a NODE KEY a Community configuration cannot declare.
type keyKindHolders struct {
	byDefinition map[string][]keyKindHolder
	// heldOut marks each orphan, by position, that a drift's action removes.
	heldOut map[int]bool
}

type keyKindHolder struct {
	rc     RemoteConstraint
	kind   string
	orphan int // position in the orphans slice; -1 for a paired or excluded row
}

func newKeyKindHolders(pairs []pair[Constraint, scoped[RemoteConstraint]], orphans []scoped[RemoteConstraint], excluded []RemoteConstraint) keyKindHolders {
	h := keyKindHolders{byDefinition: make(map[string][]keyKindHolder), heldOut: make(map[int]bool)}
	add := func(rc RemoteConstraint, orphan int) {
		if rc.EntityType != "" && !strings.EqualFold(rc.EntityType, "NODE") {
			return
		}
		if len(rc.LabelsOrTypes) != 1 {
			return
		}
		kind := canonicalRemoteConstraintType(rc.Type)
		if kind != constraintKindToRemoteType(ConstraintUnique) && kind != constraintKindToRemoteType(ConstraintNodeKey) {
			return
		}
		def := constraintDefinitionKey(rc.LabelsOrTypes[0], rc.Properties)
		h.byDefinition[def] = append(h.byDefinition[def], keyKindHolder{rc: rc, kind: kind, orphan: orphan})
	}
	for _, p := range pairs {
		add(p.Actual.obj, -1)
	}
	for i, o := range orphans {
		add(o.obj, i)
	}
	for _, rc := range excluded {
		add(rc, -1)
	}
	return h
}

// blocking returns the first holder of the other key-backed kind over d's label
// and members in d's order, and holds it out of Drop when it is an orphan.
func (h keyKindHolders) blocking(d Constraint) (RemoteConstraint, bool) {
	if !constraintHasBackingIndex(d.Kind) {
		return RemoteConstraint{}, false
	}
	want := constraintKindToRemoteType(d.Kind)
	for _, holder := range h.byDefinition[constraintDefinitionKey(d.Label, d.Properties)] {
		if holder.kind == want {
			continue
		}
		if holder.orphan >= 0 {
			h.heldOut[holder.orphan] = true
		}
		return holder.rc, true
	}
	return RemoteConstraint{}, false
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
// Neo4j renamed the node uniqueness constraint type between server generations:
// 5.x reports UNIQUENESS, 2026.x reports NODE_PROPERTY_UNIQUENESS — aligning it
// with the NODE_PROPERTY_EXISTENCE and NODE_PROPERTY_TYPE spellings it has
// always used. Both name the same constraint and this adapter targets both
// generations, so every comparison against a remote type must accept either.
// Comparing raw strings makes a 2026.x server's uniqueness constraints
// undeclarable, and the whole diff then misreports: the actual side excludes
// them, the desired side finds its name taken and reports the schema's OWN
// constraint as drift to be dropped, and [Adapter.InferSchema] marks no primary
// key at all, scaffolding a schema that cannot load.
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
	// constraint from matching and dropping) and the cross-generation spelling
	// of the uniqueness type. Both must be absorbed here and in
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

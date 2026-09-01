package neo4j

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// TestConstraintsForSchema_Golden pins the complete default-option statement
// list per schema fixture: statement text, statement count, and emission
// order. Absences are load-bearing — the abstract_types and inheritance
// goldens contain no statements for their abstract types, and the per-fixture
// type mappings (aliases, enum/pattern, lists, UUID→STRING) are pinned in
// full rather than per-substring.
func TestConstraintsForSchema_Golden(t *testing.T) {
	t.Parallel()
	fixtures := []string{
		"abstract_types", "aliases", "basic", "composite_pk", "enum_pattern",
		"inheritance", "list_properties", "multiple_types", "part_types",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, fixture+".yammm")
			stmts, result := New().ConstraintsForSchema(context.Background(), s)
			if err := result.Err(); err != nil {
				t.Fatalf("ConstraintsForSchema(%s): %v", fixture, err)
			}
			yammmtest.Golden(t, "constraints_"+fixture, []byte(strings.Join(stmts, "\n")+"\n"))
		})
	}
}

func TestConstraints_NamedConstraints(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New() // Default: named=true.

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Every statement should contain a name before IF NOT EXISTS.
	for _, stmt := range stmts {
		prefix := "CREATE CONSTRAINT "
		after := strings.TrimPrefix(stmt, prefix)
		if strings.HasPrefix(after, "IF NOT EXISTS") {
			t.Errorf("named constraint missing name: %s", stmt)
		}
	}

	// Check specific names.
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_unique IF NOT EXISTS")
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_not_null IF NOT EXISTS")
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_type IF NOT EXISTS")
}

func TestConstraints_UnnamedConstraints(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNamedConstraints(false))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Every statement should have no name — "CREATE CONSTRAINT IF NOT EXISTS".
	for _, stmt := range stmts {
		if !strings.HasPrefix(stmt, "CREATE CONSTRAINT IF NOT EXISTS") {
			t.Errorf("unnamed constraint has unexpected format: %s", stmt)
		}
	}
}

func TestConstraints_NodeKey(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNodeKeyConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Should use NODE KEY instead of UNIQUE.
	assertContains(t, stmts, "REQUIRE n.id IS NODE KEY")
	assertNotContains(t, stmts, "IS UNIQUE")

	// PK property NOT NULL should be omitted (NODE KEY implies NOT NULL).
	assertNotContains(t, stmts, "REQUIRE n.id IS NOT NULL")

	// Non-PK required properties should still have NOT NULL.
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_NodeKeyComposite(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New(WithNodeKeyConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	assertContains(t, stmts, "REQUIRE (n.schema_id, n.record_id) IS NODE KEY")
	assertNotContains(t, stmts, "IS UNIQUE")

	// PK NOT NULL should be omitted.
	assertNotContains(t, stmts, "REQUIRE n.schema_id IS NOT NULL")
	assertNotContains(t, stmts, "REQUIRE n.record_id IS NOT NULL")

	// Non-PK required should remain.
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

// countCode returns how many issues in r carry code.
func countCode(r diag.Result, code diag.Code) int {
	n := 0
	for issue := range r.Issues() {
		if issue.Code() == code {
			n++
		}
	}
	return n
}

// TestConstraints_NodeKeyCommunity_DegradesToUnique pins the fix for a silent
// total loss of primary-key enforcement: NODE KEY is Enterprise-only, so under
// [Community] the emitter must fall back to the UNIQUE half rather than emit a
// kind the edition filter then discards. Before the fix this fixture produced
// ZERO constraints — the NODE KEY was filtered out and the PK's NOT NULL had
// already been skipped on the assumption NODE KEY covered it — so a Community
// user asking for STRONGER keys via WithNodeKeyConstraints got none at all.
func TestConstraints_NodeKeyCommunity_DegradesToUnique(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNodeKeyConstraints(true), WithEdition(Community))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// The PK is enforced, by the strongest kind Community supports.
	assertContains(t, stmts, "REQUIRE n.id IS UNIQUE")
	assertNotContains(t, stmts, "IS NODE KEY")

	// Exactly the UNIQUE constraint survives: NOT NULL and TYPE are
	// Enterprise-only and correctly dropped, matching plain Community.
	if len(stmts) != 1 {
		t.Errorf("expected exactly 1 constraint (UNIQUE), got %d: %v", len(stmts), stmts)
	}
}

// TestConstraints_NodeKeyCommunity_MatchesPlainCommunity states the invariant
// behind the degrade directly: on Community, asking for NODE KEY must produce
// exactly what not asking produces. WithNodeKeyConstraints selects how primary
// keys are ENCODED, and Community affords one encoding, so the flag cannot
// change the output there.
func TestConstraints_NodeKeyCommunity_MatchesPlainCommunity(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"basic", "composite_pk", "multiple_types", "inheritance"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, fixture+".yammm")

			withNodeKeys, r1 := New(WithNodeKeyConstraints(true), WithEdition(Community)).
				ConstraintsForSchema(context.Background(), s)
			if err := r1.Err(); err != nil {
				t.Fatalf("ConstraintsForSchema(node-keys): %v", err)
			}
			plain, r2 := New(WithEdition(Community)).
				ConstraintsForSchema(context.Background(), s)
			if err := r2.Err(); err != nil {
				t.Fatalf("ConstraintsForSchema(plain): %v", err)
			}

			if !slices.Equal(withNodeKeys, plain) {
				t.Errorf("Community output differs by node-keys flag:\n with: %v\n without: %v",
					withNodeKeys, plain)
			}
		})
	}
}

// TestConstraints_NodeKeyCommunity_Composite covers the tuple form, whose
// suffix is built on a separate branch from the single-property form.
func TestConstraints_NodeKeyCommunity_Composite(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New(WithNodeKeyConstraints(true), WithEdition(Community))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	assertContains(t, stmts, "REQUIRE (n.schema_id, n.record_id) IS UNIQUE")
	assertNotContains(t, stmts, "IS NODE KEY")
}

// TestConstraints_NodeKeyCommunity_WarnsOncePerCall pins both that the degrade
// is announced and that it is announced at configuration altitude. A per-type
// warning would scale with schema size and bury the one fact being reported;
// multiple_types exists precisely to catch that.
func TestConstraints_NodeKeyCommunity_WarnsOncePerCall(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	a := New(WithNodeKeyConstraints(true), WithEdition(Community))

	_, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// A warning, not an error: the caller still gets usable output.
	if result.HasErrors() {
		t.Errorf("degrade must not fail the call: %v", result)
	}
	if got := countCode(result, W_NEO4J_NODE_KEY_UNSUPPORTED); got != 1 {
		t.Errorf("expected exactly 1 %s, got %d", W_NEO4J_NODE_KEY_UNSUPPORTED, got)
	}

	// Severity is asserted, not assumed. Warning is the contract: high enough
	// that HasWarnings sees it and default renderers print it, low enough that it
	// is not a failure. Info would still render and still carry the code, so
	// every other assertion here passes at that severity too.
	for issue := range result.Issues() {
		if issue.Code() != W_NEO4J_NODE_KEY_UNSUPPORTED {
			continue
		}
		if issue.Severity() != diag.Warning {
			t.Errorf("%s severity = %v, want %v", issue.Code(), issue.Severity(), diag.Warning)
		}
	}
	if !result.HasWarnings() {
		t.Error("result carries no warning; the degrade is invisible to severity-gated callers")
	}
}

// TestConstraintsForType_CommunityNodeKeyKeepsPrimaryKeyNotNull asserts the
// coupling between the two emitters BEFORE the edition filter runs, which is the
// only place it is observable.
//
// notNullConstraints skips a primary key's NOT NULL whenever a NODE KEY is
// emitted to cover it. Post-filter that skip is invisible on Community — NOT
// NULL is Enterprise-only and would be dropped anyway — so an emitter that
// consulted the raw flag instead of the shared predicate would produce identical
// output today and pass every other test here. It would also be one filter
// change away from reinstating the original bug, where the primary key lost its
// uniqueness to the filter and its NOT NULL to a skip that assumed a NODE KEY
// that was never kept. Asserting pre-filter pins the invariant that actually
// matters: the skip happens only where the covering NODE KEY does.
func TestConstraintsForType_CommunityNodeKeyKeepsPrimaryKeyNotNull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNodeKeyConstraints(true), WithEdition(Community))

	collector := diag.NewCollector(0)
	var unfiltered []Constraint
	for typ, label := range a.emittableTypes(ctx, s, collector) {
		unfiltered = append(unfiltered, a.constraintsForType(ctx, typ, label, collector)...)
	}
	if unfiltered == nil {
		t.Fatal("no constraints emitted for basic.yammm")
	}

	var sawPKUnique, sawPKNotNull bool
	for _, c := range unfiltered {
		if !slices.Contains(c.Properties, "id") {
			continue
		}
		switch c.Kind {
		case ConstraintUnique:
			sawPKUnique = true
		case ConstraintNotNull:
			sawPKNotNull = true
		case ConstraintNodeKey:
			t.Errorf("Community emitted a NODE KEY pre-filter: %s", c.Statement)
		case ConstraintType:
		}
	}
	if !sawPKUnique {
		t.Error("no UNIQUE emitted for the primary key")
	}
	if !sawPKNotNull {
		t.Error("primary key's NOT NULL was skipped, but no NODE KEY was emitted to cover it")
	}
}

// TestConstraints_NodeKeyWarning_OnlyOnTheDegrade is the control: the warning
// must fire on the combination that degrades and nowhere else, or it becomes
// noise operators learn to ignore.
func TestConstraints_NodeKeyWarning_OnlyOnTheDegrade(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts []Option
	}{
		{"enterprise with node-keys", []Option{WithNodeKeyConstraints(true)}},
		{"community without node-keys", []Option{WithEdition(Community)}},
		{"enterprise without node-keys", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, "basic.yammm")

			_, result := New(tc.opts...).ConstraintsForSchema(context.Background(), s)
			if err := result.Err(); err != nil {
				t.Fatalf("ConstraintsForSchema failed: %v", err)
			}
			if result.HasCode(W_NEO4J_NODE_KEY_UNSUPPORTED) {
				t.Errorf("unexpected %s: %v", W_NEO4J_NODE_KEY_UNSUPPORTED, result)
			}
		})
	}
}

func TestConstraints_CommunityEdition(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithEdition(Community))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Only UNIQUE constraints should be present.
	for _, stmt := range stmts {
		if !strings.Contains(stmt, "IS UNIQUE") {
			t.Errorf("Community edition should only have UNIQUE, got: %s", stmt)
		}
	}
	if len(stmts) != 1 {
		t.Errorf("expected exactly 1 UNIQUE constraint, got %d", len(stmts))
	}
}

func TestConstraints_RequiredOnlyTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithRequiredOnlyTypeConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Required properties should have TYPE constraints.
	assertContains(t, stmts, "REQUIRE n.id IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.name IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.count IS :: INTEGER")
	assertContains(t, stmts, "REQUIRE n.active IS :: BOOLEAN")
	assertContains(t, stmts, "REQUIRE n.created_at IS :: ZONED DATETIME")

	// Optional properties should NOT have TYPE constraints.
	assertNotContains(t, stmts, "REQUIRE n.description IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.score IS :: FLOAT")
	assertNotContains(t, stmts, "REQUIRE n.birth_date IS :: DATE")
	assertNotContains(t, stmts, "REQUIRE n.ref IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.embedding IS ::")
}

// WithScalarTypeConstraints(false) suppresses EVERY property-type constraint,
// list-shaped and scalar alike.
//
// They are one kind of statement. A Vector and a List<Float> emit the same
// expression, so gating them apart meant the option suppressed the constraint
// for one declaration and not for an identical one. It is also the only switch
// that turns property-type constraints off, which a server older than Neo4j 5.9
// needs because they do not exist there.
//
// Mutation: moving listTypeConstraints back outside the gate turns this red.
func TestConstraints_TypeConstraintsDisabled(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	a := New(WithScalarTypeConstraints(false))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	for _, unwanted := range []string{
		"REQUIRE n.tags IS :: LIST<STRING NOT NULL>",
		"REQUIRE n.id IS :: STRING",
		"REQUIRE n.name IS :: STRING",
		"REQUIRE n.active IS :: BOOLEAN",
	} {
		assertNotContains(t, stmts, unwanted)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, "IS ::") {
			t.Errorf("a property-type constraint survived the gate: %q", stmt)
		}
	}

	// The gate is the only thing suppressed: keys and NOT NULL still emit.
	if len(stmts) == 0 {
		t.Error("every constraint was suppressed; the gate should reach type constraints alone")
	}
}

func TestConstraints_DeterministicOrder(t *testing.T) {
	t.Parallel()
	// Call-to-call determinism; the emission order itself is pinned by the
	// per-fixture goldens in TestConstraintsForSchema_Golden.
	s := loadSchema(t, "multiple_types.yammm")
	a := New()

	stmts1, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	stmts2, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if !slices.Equal(stmts1, stmts2) {
		t.Error("ConstraintsForSchema produced different output on second call")
	}
}

// TestConstraintsStructured_Golden pins the full structured form — Name,
// Kind, Label, Properties, TypeExpr, and the complete Statement — for the
// default options over the basic fixture.
func TestConstraintsStructured_Golden(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")

	constraints, result := New().ConstraintsStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsStructured failed: %v", err)
	}
	yammmtest.GoldenJSON(t, "constraints_structured_basic", constraints)
}

func assertContains(t *testing.T, stmts []string, substring string) {
	t.Helper()
	for _, stmt := range stmts {
		if strings.Contains(stmt, substring) {
			return
		}
	}
	t.Errorf("no statement contains %q\nstatements:\n%s", substring, strings.Join(stmts, "\n"))
}

func assertNotContains(t *testing.T, stmts []string, substring string) {
	t.Helper()
	for _, stmt := range stmts {
		if strings.Contains(stmt, substring) {
			t.Errorf("unexpected statement containing %q: %s", substring, stmt)
			return
		}
	}
}

// enterpriseKindCounts returns how many constraints of each kind the default
// (Enterprise) configuration emits for s, which is what Community must report
// as omitted for the kinds it cannot hold.
func enterpriseKindCounts(t *testing.T, s *schema.Schema) map[ConstraintKind]int {
	t.Helper()
	constraints, result := New().ConstraintsStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("Enterprise ConstraintsStructured: %v", err)
	}
	counts := map[ConstraintKind]int{}
	for _, c := range constraints {
		counts[c.Kind]++
	}
	return counts
}

// editionOmissionMessage returns the one W_NEO4J_EDITION_CONSTRAINT_OMITTED
// message in result, failing when there is not exactly one.
func editionOmissionMessage(t *testing.T, result diag.Result) string {
	t.Helper()
	if got := countCode(result, W_NEO4J_EDITION_CONSTRAINT_OMITTED); got != 1 {
		t.Fatalf("expected exactly 1 %s, got %d: %s", W_NEO4J_EDITION_CONSTRAINT_OMITTED, got, result)
	}
	for issue := range result.Issues() {
		if issue.Code() == W_NEO4J_EDITION_CONSTRAINT_OMITTED {
			if issue.Severity() != diag.Warning {
				t.Errorf("%s severity = %v, want %v", issue.Code(), issue.Severity(), diag.Warning)
			}
			return issue.Message()
		}
	}
	return ""
}

// TestConstraints_EditionOmission_WarnsOncePerCallWithCounts pins that
// Community reports what it dropped: once per call, with the count of each
// omitted kind, so an operator reading the script knows which guarantees
// the schema declares and the database will not hold.
func TestConstraints_EditionOmission_WarnsOncePerCallWithCounts(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	want := enterpriseKindCounts(t, s)
	if want[ConstraintNotNull] == 0 || want[ConstraintType] == 0 {
		t.Fatalf("fixture must drop both kinds under Community, Enterprise counts %v", want)
	}

	_, result := New(WithEdition(Community)).ConstraintsStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("omission must not fail the call: %v", result)
	}
	if !result.HasWarnings() {
		t.Error("result carries no warning; the omission is invisible to severity-gated callers")
	}
	msg := editionOmissionMessage(t, result)
	dropped := want[ConstraintNotNull] + want[ConstraintType]
	total := dropped + want[ConstraintUnique] + want[ConstraintNodeKey]
	for _, part := range []string{
		fmt.Sprintf("%d of %d", dropped, total),
		fmt.Sprintf("%d NOT NULL", want[ConstraintNotNull]),
		fmt.Sprintf("%d PROPERTY_TYPE", want[ConstraintType]),
	} {
		if !strings.Contains(msg, part) {
			t.Errorf("message %q does not carry %q", msg, part)
		}
	}
}

// TestConstraints_EditionOmission_OrderIsFixed pins that the kinds are listed
// in one order on every run, so two operators reading two runs see one
// message rather than a map-order shuffle.
func TestConstraints_EditionOmission_OrderIsFixed(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	a := New(WithEdition(Community))

	// Sixty-four calls: a two-key map iterates in the wrong order about one
	// time in eight, so one comparison would miss a map-ordered rendering.
	var first string
	for i := range 64 {
		_, result := a.ConstraintsStructured(context.Background(), s)
		msg := editionOmissionMessage(t, result)
		if i == 0 {
			first = msg
		}
		if msg != first {
			t.Fatalf("call %d rendered a different message:\n%s\n%s", i, first, msg)
		}
		if strings.Index(msg, "NOT NULL") > strings.Index(msg, "PROPERTY_TYPE") {
			t.Fatalf("kinds are not in declaration order: %s", msg)
		}
	}
}

// TestConstraints_EditionOmission_OnlyWhenSomethingIsDropped pins the two
// silent cases: Enterprise drops nothing, and a Community schema with no
// emittable type has nothing to drop. The second is the only Community call
// that drops nothing, since every concrete type carries a primary key whose
// NOT NULL Community cannot hold.
func TestConstraints_EditionOmission_OnlyWhenSomethingIsDropped(t *testing.T) {
	t.Parallel()
	t.Run("enterprise", func(t *testing.T) {
		t.Parallel()
		_, result := New().ConstraintsStructured(context.Background(), loadSchema(t, "multiple_types.yammm"))
		if result.HasCode(W_NEO4J_EDITION_CONSTRAINT_OMITTED) {
			t.Errorf("Enterprise reported an omission: %s", result)
		}
	})
	t.Run("community with nothing emittable", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadString(context.Background(),
			"schema \"empty\"\n\nabstract type Base {\n\tid String primary\n}\n", "empty.yammm")
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		constraints, result := New(WithEdition(Community)).ConstraintsStructured(context.Background(), s)
		if len(constraints) != 0 {
			t.Fatalf("fixture emitted %d constraints, want none", len(constraints))
		}
		if result.HasCode(W_NEO4J_EDITION_CONSTRAINT_OMITTED) {
			t.Errorf("a call that dropped nothing reported an omission: %s", result)
		}
	})
}

// TestConstraints_EditionOmission_CoexistsWithNodeKeyWarning pins that the
// two Community warnings are independent facts reported independently.
func TestConstraints_EditionOmission_CoexistsWithNodeKeyWarning(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	_, result := New(WithNodeKeyConstraints(true), WithEdition(Community)).
		ConstraintsStructured(context.Background(), s)
	for _, code := range []diag.Code{W_NEO4J_NODE_KEY_UNSUPPORTED, W_NEO4J_EDITION_CONSTRAINT_OMITTED} {
		if got := countCode(result, code); got != 1 {
			t.Errorf("expected exactly 1 %s, got %d", code, got)
		}
	}
}

// A Cypher reserved word as a property name is refused by the constraint
// emitters, not only by the index ones. All four emitter sites raise the same
// code, and none of them had a test.
//
// Mutation: dropping the ValidateIdentifier call in any emitter arm turns this
// red for that arm's property.
func TestConstraints_ReservedPropertyNameRefused(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_bad_ident.yammm")

	structured, result := New().ConstraintsStructured(context.Background(), s)
	if structured != nil {
		t.Errorf("a schema with a reserved property name emitted %d constraints; want none", len(structured))
	}
	var found bool
	for issue := range result.Issues() {
		if issue.Code() == E_NEO4J_INVALID_IDENTIFIER {
			found = true
			if !strings.Contains(issue.Message(), "match") {
				t.Errorf("issue does not name the offending property: %s", issue.Message())
			}
		}
	}
	if !found {
		t.Errorf("no E_NEO4J_INVALID_IDENTIFIER for property %q: %s", "match", result)
	}
}

// WithRequiredOnlyTypeConstraints(true) skips an OPTIONAL list property, the
// same way it skips an optional scalar. The list walk carries its own copy of
// that skip and no test reached it.
//
// Mutation: removing the requiredOnly check from listTypeConstraints turns this
// red.
func TestConstraints_RequiredOnlyTypes_SkipsOptionalList(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")

	stmts, result := New(WithRequiredOnlyTypeConstraints(true)).
		ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema: %v", err)
	}

	// tags, scores, ratios, flags, times and dates are all optional lists.
	for _, optional := range []string{"tags", "scores", "ratios", "flags", "times", "dates"} {
		assertNotContains(t, stmts, "REQUIRE n."+optional+" IS ::")
	}
	// A required scalar still gets one, so the option narrowed rather than
	// disabled.
	assertContains(t, stmts, "REQUIRE n.name IS :: STRING")
}

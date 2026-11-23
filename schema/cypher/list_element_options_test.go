package cypher

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
)

// constraintConfigs are the settings that decide which constraints are
// emitted. None of them decides what a schema may hold.
var constraintConfigs = []struct {
	name string
	c    Config
}{
	{"default", Config{}},
	{"no type constraints", Config{TypeConstraints: NoProperties}},
	{"required only", Config{TypeConstraints: RequiredProperties}},
	{"community", Config{Edition: Community}},
	{"community, no type constraints", Config{Edition: Community, TypeConstraints: NoProperties}},
	{"node keys, no type constraints", Config{NodeKeys: true, TypeConstraints: NoProperties}},
}

// unsupportedTypeIssues returns "type.property" for each E_NEO4J_UNSUPPORTED_TYPE
// Error in res, in order.
func unsupportedTypeIssues(res diag.Result) []string {
	var got []string
	for issue := range res.Errors() {
		if issue.Code() != E_NEO4J_UNSUPPORTED_TYPE {
			continue
		}
		var typeName, property string
		for _, d := range issue.Details() {
			switch d.Key {
			case diag.DetailKeyTypeName:
				typeName = d.Value
			case diag.DetailKeyPropertyName:
				property = d.Value
			}
		}
		got = append(got, typeName+"."+property)
	}
	return got
}

// docs/SPEC.md, "Export portability": a list whose element is itself a
// collection "has no equivalent in Neo4j", the constraint generator "rejects
// such a property with E_NEO4J_UNSUPPORTED_TYPE", and "it emits nothing for the
// whole schema". The sentence names no option, and no option gives Neo4j a
// nested collection type: each one decides what is emitted for a schema the
// generator accepts. Each row states the properties the sentence refuses, by
// the type whose label would carry them.
func TestConstraints_ANestedCollectionIsRefusedUnderEveryOption(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		source string
		want   string // the refused "type.property" pairs, space-separated
	}{
		{
			name:   "an optional list of lists",
			source: "type A {\n id String primary\n m List<List<Integer>>\n}\n",
			want:   "A.m",
		},
		{
			name:   "a required list of lists",
			source: "type A {\n id String primary\n m List<List<Integer>> required\n}\n",
			want:   "A.m",
		},
		{
			name:   "an optional list of vectors",
			source: "type A {\n id String primary\n v List<Vector[2]>\n}\n",
			want:   "A.v",
		},
		{
			name:   "an optional list of a list-typed alias",
			source: "type Codes = List<String>\ntype A {\n id String primary\n c List<Codes>\n}\n",
			want:   "A.c",
		},
		{
			name:   "an alias of a list of lists",
			source: "type Grid = List<List<Float>>\ntype A {\n id String primary\n g Grid\n}\n",
			want:   "A.g",
		},
		{
			name:   "three levels",
			source: "type A {\n id String primary\n d List<List<List<String>>>\n}\n",
			want:   "A.d",
		},
		{
			name:   "two properties of one type, beside a type that holds none",
			source: "type A {\n id String primary\n m List<List<Integer>>\n v List<Vector[2]>\n}\ntype B {\n id String primary\n n Integer required\n}\n",
			want:   "A.m A.v",
		},
		{
			name:   "on a part type",
			source: "type O {\n id String primary\n *-> PS (many) P\n}\npart type P {\n k String primary\n m List<List<Integer>>\n}\n",
			want:   "P.m",
		},
		{
			name:   "inherited from an abstract type, once for each type that extends it",
			source: "abstract type Q {\n id String primary\n m List<List<Integer>>\n}\ntype R extends Q {\n n Integer\n}\ntype S extends Q {\n}\n",
			want:   "R.m S.m",
		},
		{
			// The one exception: no node carries the property, so no pass reads it.
			name:   "on an abstract type nothing extends, which has no label",
			source: "abstract type Q {\n id String primary\n m List<List<Integer>>\n}\ntype R {\n id String primary\n n Integer required\n}\n",
			want:   "",
		},
		{
			name:   "no nested collection: a list, a vector and scalars",
			source: "type A {\n id String primary\n tags List<String>\n e Vector[3]\n n Integer required\n}\n",
			want:   "",
		},
	} {
		s := loadInline(t, "schema \"le\"\n"+c.source)
		for _, o := range constraintConfigs {
			t.Run(c.name+"/"+o.name, func(t *testing.T) {
				t.Parallel()
				constraints, res := diagtest.Capture(t, func(warn func(diag.Issue)) ([]Constraint, error) {
					return o.c.Constraints(s, WithWarnings(warn))
				})

				if got := strings.Join(unsupportedTypeIssues(res), " "); got != c.want {
					t.Errorf("Constraints refuses %q, want %q: %s", got, c.want, res)
				}
				if c.want != "" {
					if constraints != nil {
						t.Errorf("%d constraints beside the refusal, want none for the whole schema", len(constraints))
					}
					return
				}
				if diagtest.Err(res) != nil || len(constraints) == 0 {
					t.Errorf("a schema with nothing to refuse emits %d constraints: %s", len(constraints), res)
				}
			})
		}
	}
}

// What the option does decide is unmoved: with it off no property-type
// constraint is emitted, and the walk that judges a list's element checks no
// property name, since it writes no statement that holds one. `order` is a
// reserved word the default configuration refuses in a List property's
// constraint.
func TestConstraints_ScalarTypesOffEmitsNoTypeConstraintAndChecksNoListName(t *testing.T) {
	t.Parallel()
	s := loadInline(t, "schema \"le\"\ntype A {\n id String primary\n order List<String>\n tags List<String> required\n e Vector[3]\n n Integer\n}\n")

	if _, err := (Config{}).Constraints(s); !errors.Is(err, E_NEO4J_INVALID_IDENTIFIER) {
		t.Fatalf("the default configuration accepts the name, so the fixture asserts nothing: %s", err)
	}
	{
		constraints, err := Config{TypeConstraints: NoProperties}.Constraints(s)
		if err != nil {
			t.Fatalf("refused: %s", err)
		}
		var kinds []string
		for _, c := range constraints {
			if c.Kind == ConstraintType {
				t.Errorf("emitted a property-type constraint: %s", c.Statement)
			}
			kinds = append(kinds, c.Kind.String()+"("+strings.Join(c.Properties, ",")+")")
		}
		if got, want := strings.Join(kinds, " "), "UNIQUENESS(id) NODE_PROPERTY_EXISTENCE(id) NODE_PROPERTY_EXISTENCE(tags)"; got != want {
			t.Errorf("constraints = %s, want %s", got, want)
		}
	}
}

// Under RequiredProperties an optional list draws no TYPE
// constraint, so its name is not checked, as an optional scalar's is not; a
// required list's constraint still writes its name, which is checked.
func TestConstraints_RequiredOnlyChecksNoOptionalListName(t *testing.T) {
	t.Parallel()
	optional := loadInline(t, "schema \"le\"\ntype A {\n id String primary\n order List<String>\n tags List<String> required\n}\n")
	if _, err := (Config{}).Constraints(optional); !errors.Is(err, E_NEO4J_INVALID_IDENTIFIER) {
		t.Fatalf("the default configuration accepts the name, so the fixture asserts nothing: %s", err)
	}
	constraints, err := Config{TypeConstraints: RequiredProperties}.Constraints(optional)
	if err != nil {
		t.Fatalf("refused: %s", err)
	}
	var typed []string
	for _, c := range constraints {
		if c.Kind == ConstraintType {
			typed = append(typed, strings.Join(c.Properties, ","))
		}
	}
	slices.Sort(typed)
	if got := strings.Join(typed, " "); got != "id tags" {
		t.Errorf("property-type constraints on %s, want id tags", got)
	}

	required := loadInline(t, "schema \"le\"\ntype A {\n id String primary\n order List<String> required\n}\n")
	if _, err := (Config{TypeConstraints: RequiredProperties}).Constraints(required); !errors.Is(err, E_NEO4J_INVALID_IDENTIFIER) {
		t.Errorf("a required list's name was not checked: %s", err)
	}
}

// TestConstraints_CommunityChecksNoNameItDoesNotWrite pins that a property
// name is checked as an identifier only where a returned statement writes it:
// Community keeps UNIQUE alone, so a reserved name that only a NOT NULL or a
// property-type constraint would write is not refused.
func TestConstraints_CommunityChecksNoNameItDoesNotWrite(t *testing.T) {
	t.Parallel()
	s := loadInline(t, "schema \"le\"\ntype A {\n id String primary\n order List<String>\n match String required\n}\n")
	if _, err := (Config{}).Constraints(s); !errors.Is(err, E_NEO4J_INVALID_IDENTIFIER) {
		t.Fatalf("Enterprise accepts the names, so the fixture asserts nothing: %s", err)
	}
	constraints, err := Config{Edition: Community}.Constraints(s)
	if err != nil {
		t.Fatalf("Community refused a name no statement writes: %s", err)
	}
	if len(constraints) != 1 || constraints[0].Kind != ConstraintUnique || !slices.Equal(constraints[0].Properties, []string{"id"}) {
		t.Errorf("Community emitted %+v, want the UNIQUE on id alone", constraints)
	}

	key := loadInline(t, "schema \"le\"\ntype A {\n order String primary\n}\n")
	if _, err := (Config{Edition: Community}).Constraints(key); !errors.Is(err, E_NEO4J_INVALID_IDENTIFIER) {
		t.Errorf("Community did not check the name its UNIQUE writes: %s", err)
	}
}

// TestConstraints_ANameIsJudgedOnce pins that a reserved name the primary-key,
// NOT NULL and property-type statements all write draws one
// E_NEO4J_INVALID_IDENTIFIER.
func TestConstraints_ANameIsJudgedOnce(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"schema \"le\"\ntype A {\n id String primary\n match String required\n}\n",
		"schema \"le\"\ntype A {\n order String primary\n}\n",
		"schema \"le\"\ntype A {\n id String primary\n order List<String> required\n}\n",
	} {
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) ([]Constraint, error) {
			return Config{}.Constraints(loadInline(t, src), WithWarnings(warn))
		})
		n := 0
		for iss := range res.Issues() {
			if iss.Code() == E_NEO4J_INVALID_IDENTIFIER {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%q drew %d E_NEO4J_INVALID_IDENTIFIER, want 1:\n%s", src, n, res)
		}
	}
}

// TestConstraints_ANameIsJudgedOncePerDeclaredProperty pins the constraint
// pass to the index pass's subject: a reserved name two heirs inherit is one
// rename, so it draws one issue naming the type that declares it, and two
// types that each declare it draw two.
func TestConstraints_ANameIsJudgedOncePerDeclaredProperty(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		src  string
		want []string
	}{
		{
			"schema \"le\"\nabstract type B {\n match String required\n}\ntype A extends B {\n id String primary\n}\ntype C extends B {\n cid String primary\n}\n",
			[]string{`declared in B`},
		},
		{
			"schema \"le\"\ntype A {\n id String primary\n match String required\n}\ntype C {\n cid String primary\n match String required\n}\n",
			[]string{`declared in A`, `declared in C`},
		},
		{
			"schema \"le\"\nabstract type B {\n match String required\n}\nabstract type W {\n match String required @writeOnce\n}\ntype A extends B, W {\n id String primary\n}\ntype C extends B {\n cid String primary\n}\n",
			[]string{`declared in B`},
		},
	} {
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) ([]Constraint, error) {
			return Config{}.Constraints(loadInline(t, c.src), WithWarnings(warn))
		})
		var got []string
		for iss := range res.Issues() {
			if iss.Code() == E_NEO4J_INVALID_IDENTIFIER {
				got = append(got, iss.Message())
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%q drew %d E_NEO4J_INVALID_IDENTIFIER, want %d:\n%s", c.src, len(got), len(c.want), res)
			continue
		}
		slices.Sort(got)
		for i, w := range c.want {
			if !strings.Contains(got[i], w) {
				t.Errorf("issue %q does not name %q", got[i], w)
			}
		}
	}
}

// TestConstraints_CommunityCountsWhatARefusedNameWouldOmit pins that the
// edition warning counts every NOT NULL and property-type constraint the schema
// asks for, a refused primary key's included, as it does for a valid key.
func TestConstraints_CommunityCountsWhatARefusedNameWouldOmit(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"id", "order"} {
		s := loadInline(t, "schema \"le\"\ntype T {\n "+key+" String primary\n note String required\n}\n")
		_, res := diagtest.Capture(t, func(warn func(diag.Issue)) ([]Constraint, error) {
			return Config{Edition: Community}.Constraints(s, WithWarnings(warn))
		})
		found := false
		for iss := range res.Issues() {
			if iss.Code() == W_NEO4J_EDITION_CONSTRAINT_OMITTED {
				found = true
				if !strings.Contains(iss.Message(), "omitting 2 NOT NULL, 2 PROPERTY_TYPE") {
					t.Errorf("key %s: %s", key, iss.Message())
				}
			}
		}
		if !found {
			t.Errorf("key %s: no omission warning:\n%s", key, res)
		}
	}
}

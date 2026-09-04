package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

func hasIssue(res diag.Result, code diag.Code, fragment string) bool {
	for is := range res.Issues() {
		if is.Code() == code && strings.Contains(is.Message(), fragment) {
			return true
		}
	}
	return false
}

// An invariant's message is its identity and the text a failure reports, so
// an empty one is refused at load — by the one rule, whichever way the schema
// was built.
func TestLoad_RefusesAnEmptyInvariantMessage(t *testing.T) {
	t.Parallel()
	_, res := schema.LoadString(t.Context(), "schema \"s\"\ntype T {\n    id String primary\n    ! \"\" id != \"\"\n}\n", "s.yammm")
	if !hasIssue(res, diag.E_INVALID_INVARIANT, "message must not be empty") {
		t.Errorf("want E_INVALID_INVARIANT naming the empty message; got %v", res.Err())
	}
	_, res = schema.NewBuilder().WithName("s").
		AddType("T").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithInvariant("", expr.SExpr{expr.Op("!="), expr.SExpr{expr.Op("p"), expr.NewLiteral("id")}, expr.NewLiteral("")}, "").
		Done().Build()
	if !hasIssue(res, diag.E_INVALID_INVARIANT, "message must not be empty") {
		t.Errorf("Builder: want E_INVALID_INVARIANT naming the empty message; got %v", res.Err())
	}
}

// An invariant without an expression is refused at load by the same rule,
// and nothing downstream tolerates the state.
func TestBuild_RefusesANilInvariantExpression(t *testing.T) {
	t.Parallel()
	_, res := schema.NewBuilder().WithName("s").
		AddType("T").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithInvariant("m", nil, "").
		Done().Build()
	if !hasIssue(res, diag.E_INVALID_INVARIANT, "has no expression") {
		t.Errorf("want E_INVALID_INVARIANT naming the absent expression; got %v", res.Err())
	}
}

// Two edge properties on one relation whose names fold to one lowercase are
// a schema error, as two node properties are: the case-folded lookup is
// sound only when the schema guarantees uniqueness under fold.
func TestLoad_RefusesEdgePropertyCollisions(t *testing.T) {
	t.Parallel()
	base := "schema \"s\"\ntype Company {\n    id String primary\n}\ntype Person {\n    id String primary\n    --> WORKS_AT Company {\n%s    }\n}\n"
	for name, tc := range map[string]struct {
		props string
		code  diag.Code
		want  string
	}{
		"case fold":  {"        startDate Timestamp\n        startdate String\n", diag.E_CASE_COLLISION, "startdate"},
		"exact name": {"        title String\n        title String\n", diag.E_DUPLICATE_PROPERTY, "title"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadString(t.Context(), strings.Replace(base, "%s", tc.props, 1), "s.yammm")
			if !hasIssue(res, tc.code, tc.want) {
				t.Errorf("want %s naming %q; got %v", tc.code, tc.want, res.Err())
			}
		})
	}
	// Two properties that differ in more than case are fine.
	_, res := schema.LoadString(t.Context(), strings.Replace(base, "%s", "        startDate Timestamp\n        title String\n", 1), "s.yammm")
	if res.Err() != nil {
		t.Errorf("distinct edge properties refused: %v", res.Err())
	}
}

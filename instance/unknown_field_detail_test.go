package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// caseFoldSchema declares one property under the given name. A schema declaring
// "name" lets an exact input claim it and shadow a folded one; a schema
// declaring "fullName" lets two non-exact inputs fold onto it and collide.
func caseFoldSchema(t *testing.T, propName string) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "test"
type T {
    id String primary
    `+propName+` String
}`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("schema: %s", result)
	}
	return s
}

// issueFor returns the first issue carrying code, or nil.
func issueFor(res diag.Result, code diag.Code) *diag.Issue {
	for issue := range res.Issues() {
		if issue.Code() == code {
			return &issue
		}
	}
	return nil
}

func detailValue(issue *diag.Issue, key string) (string, bool) {
	for _, d := range issue.Details() {
		if d.Key == key {
			return d.Value, true
		}
	}
	return "", false
}

// E_UNKNOWN_FIELD says when the field case-folds onto a property an exact match
// already claimed. Both inputs plainly name the same schema property, so a bare
// "unknown field" leaves the operator with no way to see why one was taken and
// the other refused. The behaviour does not change — exact-match precedence is
// the documented rule — only the diagnostic gains the reason.
//
// Mutation: removing the exactMatchShadowing call turns this red; the issue
// carries only type and field.
func TestUnknownFieldDetail_NamesTheShadowingExactMatch(t *testing.T) {
	s := caseFoldSchema(t, "name")

	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{
		Properties: map[string]any{"id": "1", "name": "exact", "NAME": "folded"},
	})

	issue := issueFor(res, diag.E_UNKNOWN_FIELD)
	if issue == nil {
		t.Fatalf("no E_UNKNOWN_FIELD was reported: %s", res)
	}
	if reason, ok := detailValue(issue, diag.DetailKeyReason); !ok || reason != "case_fold_shadowed" {
		t.Errorf("reason detail = %q (present=%v), want %q", reason, ok, "case_fold_shadowed")
	}
	if prop, ok := detailValue(issue, diag.DetailKeyPropertyName); !ok || prop != "name" {
		t.Errorf("property detail = %q (present=%v), want %q", prop, ok, "name")
	}
}

// A field that folds onto nothing carries no reason. The detail must name a
// real cause, not appear on every unknown field.
func TestUnknownFieldDetail_AbsentWhenNothingShadows(t *testing.T) {
	s := caseFoldSchema(t, "name")

	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{
		Properties: map[string]any{"id": "1", "name": "exact", "unrelated": "x"},
	})

	issue := issueFor(res, diag.E_UNKNOWN_FIELD)
	if issue == nil {
		t.Fatalf("no E_UNKNOWN_FIELD was reported: %s", res)
	}
	if _, ok := detailValue(issue, diag.DetailKeyReason); ok {
		t.Error("a field that folds onto nothing carries a shadowing reason")
	}
}

// Two inputs folding onto a property with NO exact match is the collision case,
// which has its own code. The new detail must not appear there.
func TestUnknownFieldDetail_CollisionIsADifferentCase(t *testing.T) {
	s := caseFoldSchema(t, "fullName")

	_, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{
		Properties: map[string]any{"id": "1", "FullName": "a", "fullname": "b"},
	})

	if issueFor(res, diag.E_CASE_FOLD_COLLISION) == nil {
		t.Errorf("two inputs folding onto an unclaimed property should collide: %s", res)
	}
}

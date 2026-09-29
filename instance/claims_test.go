package instance_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

const claimsSchema = `schema "c"

type Company {
    id String primary
}

part type Pet {
    name String
}

type Person {
    id String primary
    name String
    aa String
    bb String
    --> WORKS_AT (one) Company {
        title String
        note String
        xx String
        yy String
    }
    *-> PETS (many) Pet
}
`

// ClaimKeys answers per key as the validator reads an object holding exactly
// those keys: an exact name claims its member, the one folded key claims an
// unclaimed member, and a shadowed, colliding or unknown key claims nothing.
func TestClaimKeys_DecidesEachKeyAsTheValidatorDoes(t *testing.T) {
	s := loadSrc(t, claimsSchema)
	person, _ := s.Type("Person")
	works, _ := person.RelationByField("works_at")
	pets, _ := person.RelationByField("pets")

	c := instance.ClaimKeys(person, []string{"id", "name", "NAME", "AA", "Aa", "BB", "Works_At", "PETS", "zzz"}, false)
	for key, want := range map[string]string{"id": "id", "name": "name", "BB": "bb"} {
		if p := c.Property(key); p == nil || p.Name() != want {
			t.Errorf("Property(%q) = %v, want %s", key, p, want)
		}
	}
	for _, key := range []string{"NAME", "AA", "Aa", "zzz"} {
		if p := c.Property(key); p != nil {
			t.Errorf("Property(%q) = %s, want nil: shadowed, colliding or unknown", key, p.Name())
		}
	}
	if c.Relation("Works_At") != works || c.Relation("PETS") != pets {
		t.Errorf("folded relation keys do not claim their relations")
	}

	strict := instance.ClaimKeys(person, []string{"id", "BB", "Works_At"}, true)
	if strict.Property("BB") != nil || strict.Relation("Works_At") != nil || strict.Property("id") == nil {
		t.Errorf("under strict names only the exact key claims")
	}
}

// ClaimEdgeKeys answers per key of one target object: a foreign-key field of
// the target, or an edge property. With no target no key claims a foreign-key
// field.
func TestClaimEdgeKeys_DecidesEachKeyAsTheValidatorDoes(t *testing.T) {
	s := loadSrc(t, claimsSchema)
	person, _ := s.Type("Person")
	company, _ := s.Type("Company")
	works, _ := person.RelationByField("works_at")
	keys := []string{"_target_id", "_TARGET_ID", "TITLE", "note", "zzz"}

	c := instance.ClaimEdgeKeys(works, company, keys, false)
	if k := c.Key("_target_id"); k == nil || k.Name() != "id" {
		t.Errorf("Key(_target_id) = %v, want id", k)
	}
	if c.Key("_TARGET_ID") != nil || c.Property("_TARGET_ID") != nil {
		t.Errorf("a key shadowed by an exact one claims something")
	}
	if p := c.Property("TITLE"); p == nil || p.Name() != "title" {
		t.Errorf("Property(TITLE) = %v, want title", p)
	}
	if c.Property("zzz") != nil || c.Key("zzz") != nil {
		t.Errorf("an unknown key claims something")
	}

	if instance.ClaimEdgeKeys(works, nil, keys, false).Key("_target_id") != nil {
		t.Errorf("with no target a key claims a foreign-key field")
	}
	if instance.ClaimEdgeKeys(works, company, keys, true).Property("TITLE") != nil {
		t.Errorf("under strict names a folded edge property claims")
	}
}

// A foreign-key key shadowed by an exact one is an unknown edge field that
// names the field it was shadowed by.
func TestEdgeTarget_AShadowedForeignKeyNamesItsField(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, claimsSchema))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_target_id": "c1", "_TARGET_ID": "c2"},
	}})
	is := mustIssue(t, res, instance.ErrUnknownEdgeField)
	if r := detailValues(is, diag.DetailKeyReason); len(r) != 1 || r[0] != "case_fold_shadowed" {
		t.Errorf("reason = %v, want case_fold_shadowed", r)
	}
	if p := detailValues(is, diag.DetailKeyPropertyName); len(p) != 1 || p[0] != "_target_id" {
		t.Errorf("property = %v, want _target_id", p)
	}
}

// An edge collision names the member it collides on: a foreign-key field, or
// an edge property with its property detail.
func TestEdgeTarget_ACollisionNamesItsMember(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, claimsSchema))
	_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{
		"id": "p", "works_at": map[string]any{"_TARGET_ID": "c1", "_Target_Id": "c2", "TITLE": "a", "Title": "b"},
	}})
	var fk, prop bool
	for is := range res.Issues() {
		if is.Code() != instance.ErrCaseFoldCollision {
			continue
		}
		switch {
		case strings.Contains(is.Message(), `foreign-key field "_target_id"`):
			fk = len(detailValues(is, diag.DetailKeyPropertyName)) == 0
		case strings.Contains(is.Message(), `edge property "title"`):
			p := detailValues(is, diag.DetailKeyPropertyName)
			prop = len(p) == 1 && p[0] == "title"
		}
	}
	if !fk || !prop {
		t.Errorf("want a foreign-key collision with no property detail and an edge-property one naming title: %s", res)
	}
}

// Collisions are collected in one order, so a limit keeps the same one: node
// properties by their first key, edge properties likewise, and an
// association's collision before a composition's.
func TestCollisions_ALimitKeepsTheFirstInOrder(t *testing.T) {
	v := instance.NewValidator(loadSrc(t, claimsSchema), instance.WithIssueLimit(1))
	for _, c := range []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"node properties", map[string]any{"id": "p", "BB": "1", "Bb": "2", "AA": "1", "Aa": "2"}, `"aa"`},
		{"edge properties", map[string]any{"id": "p", "works_at": map[string]any{"_target_id": "c", "YY": "1", "Yy": "2", "XX": "1", "Xx": "2"}}, `"xx"`},
		{"relations", map[string]any{"id": "p", "PETS": []any{}, "Pets": []any{}, "WORKS_AT": map[string]any{}, "Works_At": map[string]any{}}, "WORKS_AT"},
	} {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: c.props})
		is, ok := issueWithCode(res, instance.ErrCaseFoldCollision)
		if !ok || !strings.Contains(is.Message()+strings.Join(detailValues(is, diag.DetailKeyRelationName), ","), c.want) {
			t.Errorf("%s: kept %s, want the collision on %s", c.name, res, c.want)
		}
	}
}

// records keeps every log record's attributes.
type records struct {
	mu    sync.Mutex
	attrs []map[string]string
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }
func (r *records) Handle(_ context.Context, rec slog.Record) error {
	m := map[string]string{"msg": rec.Message}
	rec.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.String(); return true })
	r.mu.Lock()
	r.attrs = append(r.attrs, m)
	r.mu.Unlock()
	return nil
}
func (r *records) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *records) WithGroup(string) slog.Handler      { return r }

// A folded key's debug record names the input key and the property it resolved
// to.
func TestFoldedKey_TheDebugRecordNamesInputAndProperty(t *testing.T) {
	h := &records{}
	v := instance.NewValidator(loadSrc(t, claimsSchema), instance.WithLogger(slog.New(h)))
	if _, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "NAME": "n"}}); !res.OK() {
		t.Fatalf("validate: %s", res)
	}
	for _, a := range h.attrs {
		if a["msg"] == "property name normalized" && a["input"] == "NAME" && a["resolved"] == "name" {
			return
		}
	}
	t.Errorf("no record names input NAME and resolved name: %v", h.attrs)
}

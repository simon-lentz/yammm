package instance_test

import (
	"maps"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

const collidedSchema = `schema "cm"
type Company {
	cid String primary
	d Date primary
}
type Person {
	id String primary
	born Date required
	--> WORKS_AT (_:one) Company { since Integer required }
}
`

// A case-fold collision is a member's one issue, at a required property, a
// foreign-key field and a required edge property, as it is at a relation.
func TestValidateOne_ACollidedMemberDrawsTheCollisionAlone(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadSrc(t, collidedSchema))
	edge := map[string]any{"_target_cid": "c1", "_target_d": "2020-01-01", "since": 5}
	person := func(works map[string]any) map[string]any {
		obj := maps.Clone(edge)
		for k, v := range works {
			if v == nil {
				delete(obj, k)
				continue
			}
			obj[k] = v
		}
		return map[string]any{"id": "p1", "born": "2020-01-01", "works_at": obj}
	}
	for name, props := range map[string]map[string]any{
		"a required property":            {"id": "p1", "BORN": "2020-01-01", "Born": "2020-01-02", "works_at": edge},
		"a foreign-key field":            person(map[string]any{"_target_cid": nil, "_TARGET_CID": "c1", "_Target_Cid": "c2"}),
		"every field of a composite key": person(map[string]any{"_target_cid": nil, "_target_d": nil, "_TARGET_CID": "c1", "_Target_Cid": "c2", "_TARGET_D": "2020-01-01", "_Target_D": "2020-01-02"}),
		"a required edge property":       person(map[string]any{"since": nil, "SINCE": 5, "Since": 6}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			valid, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
			if valid != nil {
				t.Fatal("a collided member validated")
			}
			for issue := range res.Issues() {
				if issue.Code() != instance.ErrCaseFoldCollision {
					t.Errorf("drew %s beside the collision: %s", issue.Code(), res)
				}
			}
		})
	}
}

package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

const memberLookupSrc = `schema "m"

type Company {
    id String primary
}

part type Line {
    id String primary
}

abstract type Base {
    id String primary
    --> AUDITED_BY (one) Company
}

type Person extends Base {
    --> WORKS_AT (one) Company {
        title String
        startDate String
    }
    *-> LINES (many) Line
}
`

func loadMembers(t *testing.T) *schema.Type {
	t.Helper()
	s, res := schema.LoadString(t.Context(), memberLookupSrc, "m.yammm")
	if !res.OK() {
		t.Fatalf("load: %s", res)
	}
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatal("no Person")
	}
	return typ
}

// RelationByField is the exact lookup on the field name instance data carries
// a relation under — own and inherited — beside Relation, which indexes the
// DSL name.
func TestType_RelationByField(t *testing.T) {
	typ := loadMembers(t)
	for _, field := range []string{"works_at", "lines", "audited_by"} {
		r, ok := typ.RelationByField(field)
		if !ok || r.FieldName() != field {
			t.Errorf("RelationByField(%q) = %v, %v", field, r, ok)
		}
	}
	if _, ok := typ.RelationByField("WORKS_AT"); ok {
		t.Error("the field-name index is exact; the DSL spelling is Relation's")
	}
	if _, ok := typ.RelationByField("nope"); ok {
		t.Error("an unknown field resolved")
	}
}

// PropertyFold answers the case-folded lookup for an edge-property block
// without a scan per input key.
func TestRelation_PropertyFold(t *testing.T) {
	rel, ok := loadMembers(t).Relation("WORKS_AT")
	if !ok {
		t.Fatal("no WORKS_AT")
	}
	if p, ok := rel.PropertyFold("startdate"); !ok || p.Name() != "startDate" {
		t.Errorf("PropertyFold(startdate) = %v, %v", p, ok)
	}
	if p, ok := rel.PropertyFold("title"); !ok || p.Name() != "title" {
		t.Errorf("PropertyFold(title) = %v, %v", p, ok)
	}
	if _, ok := rel.PropertyFold("StartDate"); ok {
		t.Error("the argument is the lowercased key; the caller folds")
	}
	if _, ok := rel.PropertyFold("nope"); ok {
		t.Error("an unknown property resolved")
	}
}

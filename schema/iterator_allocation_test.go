package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

var rangedElements int

// Each row is a range written out as a caller writes it, since a helper's parameter
// would move the iterator to the heap. Every collection holds elements, because a copy
// of an empty slice allocates nothing.
func TestRecordIterators_ARangeAllocatesNothing(t *testing.T) {
	sources := map[string][]byte{
		"/virtual/f.yammm": []byte("schema \"f\"\ntype R { rid String primary }\n"),
		"/virtual/e.yammm": []byte(`schema "e"
import "./f" as a
type Name = String
type D { did String primary }
part type K { v String }
abstract type Base {
	b String
	@@index(b)
}
type X extends Base, a.R, Base {
	name Name required @index
	! "named" name != ""
	@@index(name)
	--> EMPLOYER (one) D { since Integer }
	*-> PARTS (many) K
}
type Y extends X { }
`),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "/virtual/e.yammm", "/virtual")
	if !res.OK() {
		t.Fatal(res)
	}
	x, ok := s.Type("X")
	if !ok {
		t.Fatal("no type X")
	}
	name, ok := x.Property("name")
	if !ok {
		t.Fatal("no property name")
	}
	employer, ok := x.Relation("EMPLOYER")
	if !ok {
		t.Fatal("no relation EMPLOYER")
	}

	for row, ranged := range map[string]func(){
		"Schema.Types": func() {
			n := 0
			for range s.Types() {
				n++
			}
			rangedElements = n
		},
		"Schema.DataTypes": func() {
			n := 0
			for range s.DataTypes() {
				n++
			}
			rangedElements = n
		},
		"Schema.Imports": func() {
			n := 0
			for range s.Imports() {
				n++
			}
			rangedElements = n
		},
		"Type.Properties": func() {
			n := 0
			for range x.Properties() {
				n++
			}
			rangedElements = n
		},
		"Type.AllProperties": func() {
			n := 0
			for range x.AllProperties() {
				n++
			}
			rangedElements = n
		},
		"Type.PrimaryKeys": func() {
			n := 0
			for range x.PrimaryKeys() {
				n++
			}
			rangedElements = n
		},
		"Type.Associations": func() {
			n := 0
			for range x.Associations() {
				n++
			}
			rangedElements = n
		},
		"Type.AllAssociations": func() {
			n := 0
			for range x.AllAssociations() {
				n++
			}
			rangedElements = n
		},
		"Type.Compositions": func() {
			n := 0
			for range x.Compositions() {
				n++
			}
			rangedElements = n
		},
		"Type.AllCompositions": func() {
			n := 0
			for range x.AllCompositions() {
				n++
			}
			rangedElements = n
		},
		"Type.Invariants": func() {
			n := 0
			for range x.Invariants() {
				n++
			}
			rangedElements = n
		},
		"Type.AllInvariants": func() {
			n := 0
			for range x.AllInvariants() {
				n++
			}
			rangedElements = n
		},
		"Type.Annotations": func() {
			n := 0
			for range x.Annotations() {
				n++
			}
			rangedElements = n
		},
		"Type.AllAnnotations": func() {
			n := 0
			for range x.AllAnnotations() {
				n++
			}
			rangedElements = n
		},
		"Type.Inherits": func() {
			n := 0
			for range x.Inherits() {
				n++
			}
			rangedElements = n
		},
		"Type.ExtendsClause": func() {
			n := 0
			for range x.ExtendsClause() {
				n++
			}
			rangedElements = n
		},
		"Type.SuperTypes": func() {
			n := 0
			for range x.SuperTypes() {
				n++
			}
			rangedElements = n
		},
		"Type.SubTypes": func() {
			n := 0
			for range x.SubTypes() {
				n++
			}
			rangedElements = n
		},
		"Property.Annotations": func() {
			n := 0
			for range name.Annotations() {
				n++
			}
			rangedElements = n
		},
		"Relation.Properties": func() {
			n := 0
			for range employer.Properties() {
				n++
			}
			rangedElements = n
		},
	} {
		if allocs := testing.AllocsPerRun(10, ranged); allocs != 0 {
			t.Errorf("a range over %s allocated %v times, want 0", row, allocs)
		}
		if rangedElements == 0 {
			t.Errorf("%s holds no element, so the row cannot see a copy", row)
		}
	}
}

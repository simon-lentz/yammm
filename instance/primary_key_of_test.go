package instance_test

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const primaryKeyOfSchema = `schema "pk"

abstract type Entity {
    id String primary
}

type Account {
    region String primary
    opened Date primary
    owner UUID primary
    label String required
}

type Site extends Entity {
    name String required
}

type Code {
    code String[1, 3] primary
}

part type Room {
    code String primary
}

type Floor {
    level String primary
    *-> ROOMS (many) Room
}
`

func loadPrimaryKeyOfSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), primaryKeyOfSchema, "pk.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

// TestPrimaryKeyOf_ReadsTheKeyOfARefusedInstance pins that the key is read
// whatever else the validator refuses about the instance, and that no key comes
// back where a component cannot be read or the type holds no root.
func TestPrimaryKeyOf_ReadsTheKeyOfARefusedInstance(t *testing.T) {
	t.Parallel()
	s := loadPrimaryKeyOfSchema(t)
	v := instance.NewValidator(s)
	owner := "8F14E45F-CEEA-467A-9575-1C2B5B5C3B7A"

	tests := []struct {
		name     string
		typeName string
		props    map[string]any
		want     string
	}{
		{"a missing required label", "Account", map[string]any{"region": "eu", "opened": "2024-01-05", "owner": owner}, `["eu","2024-01-05","8f14e45f-ceea-467a-9575-1c2b5b5c3b7a"]`},
		{"an unknown field", "Account", map[string]any{"region": "eu", "opened": "2024-01-05", "owner": owner, "label": "x", "nope": 1}, `["eu","2024-01-05","8f14e45f-ceea-467a-9575-1c2b5b5c3b7a"]`},
		{"a key under a folded spelling", "Account", map[string]any{"Region": "eu", "OPENED": "2024-01-05", "owner": owner}, `["eu","2024-01-05","8f14e45f-ceea-467a-9575-1c2b5b5c3b7a"]`},
		{"an inherited key", "Site", map[string]any{"id": "s1"}, `["s1"]`},
		{"an absent component", "Account", map[string]any{"region": "eu", "owner": owner}, ""},
		{"a null component", "Account", map[string]any{"region": "eu", "opened": nil, "owner": owner}, ""},
		{"a component its constraint refuses", "Account", map[string]any{"region": "eu", "opened": "seven", "owner": owner}, ""},
		{"a number at a Date component", "Account", map[string]any{"region": "eu", "opened": 7.0, "owner": owner}, ""},
		{"two keys folding onto one component", "Account", map[string]any{"region": "eu", "Opened": "2024-01-05", "OPENED": "2024-01-06", "owner": owner}, ""},
		{"a key its constraint's bounds admit", "Code", map[string]any{"code": "ab"}, `["ab"]`},
		{"a key shorter than its constraint's bounds", "Code", map[string]any{"code": ""}, ""},
		{"a key longer than its constraint's bounds", "Code", map[string]any{"code": "abcd"}, ""},
		{"an empty input key", "Site", map[string]any{"": "s1"}, ""},
		{"an abstract type", "Entity", map[string]any{"id": "e1"}, ""},
		{"a part type", "Room", map[string]any{"code": "r1"}, ""},
		{"an unknown type", "Nope", map[string]any{"id": "n1"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, key, ok := v.PrimaryKeyOf(tc.typeName, instance.RawInstance{Properties: tc.props})
			if tc.want == "" {
				if ok {
					t.Fatalf("PrimaryKeyOf read key %s, want none", key)
				}
				return
			}
			if !ok {
				t.Fatalf("PrimaryKeyOf read no key, want %s", tc.want)
			}
			if got := key.String(); got != tc.want {
				t.Errorf("key = %s, want %s", got, tc.want)
			}
			typ, _ := s.ResolveTypeName(tc.typeName)
			if id != typ.ID() {
				t.Errorf("type = %v, want %v", id, typ.ID())
			}
		})
	}
}

// TestPrimaryKeyOf_ExactUnderStrictNames pins that a strict validator reads a
// key component only under its exact name, as its Validate does.
func TestPrimaryKeyOf_ExactUnderStrictNames(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(loadPrimaryKeyOfSchema(t), instance.WithStrictPropertyNames(true))
	if _, key, ok := v.PrimaryKeyOf("Site", instance.RawInstance{Properties: map[string]any{"ID": "s1"}}); ok {
		t.Errorf("strict PrimaryKeyOf read %s under a folded spelling", key)
	}
	if _, _, ok := v.PrimaryKeyOf("Site", instance.RawInstance{Properties: map[string]any{"id": "s1"}}); !ok {
		t.Error("strict PrimaryKeyOf read no key under the exact name")
	}
}

// TestPrimaryKeyOf_AgreesWithValidate is the second implementation. Every
// instance here holds a valid label beside its key fields, so Validate refuses
// one only for how its key is spelled or valued: the helper reads a key exactly
// where Validate accepts, and reads the type and key Validate stores.
func TestPrimaryKeyOf_AgreesWithValidate(t *testing.T) {
	t.Parallel()
	s := loadPrimaryKeyOfSchema(t)
	for _, strict := range []bool{false, true} {
		v := instance.NewValidator(s, instance.WithStrictPropertyNames(strict))
		regions := []any{"eu", "", 3, nil}
		openings := []any{"2024-01-05", "2024-02-30", int64(1), nil}
		owners := []any{"8f14e45f-ceea-467a-9575-1c2b5b5c3b7a", "8F14E45F-CEEA-467A-9575-1C2B5B5C3B7A", "not-a-uuid"}
		spellings := []string{"opened", "Opened"}
		for _, region := range regions {
			for _, opened := range openings {
				for _, owner := range owners {
					for _, spelling := range spellings {
						props := map[string]any{"region": region, spelling: opened, "owner": owner, "label": "l"}
						raw := instance.RawInstance{Properties: props}
						name := fmt.Sprintf("strict=%v %v/%s=%v/%v", strict, region, spelling, opened, owner)
						valid, _ := v.ValidateOne(t.Context(), "Account", raw)
						id, key, ok := v.PrimaryKeyOf("Account", raw)
						switch {
						case ok != (valid != nil):
							t.Errorf("%s: PrimaryKeyOf read a key = %v, Validate accepted = %v", name, ok, valid != nil)
						case ok && (id != valid.TypeID() || key.String() != valid.PrimaryKey().String()):
							t.Errorf("%s: PrimaryKeyOf = %v %s, Validate stored %v %s", name, id, key, valid.TypeID(), valid.PrimaryKey())
						}
					}
				}
			}
		}
	}
}

package jschema

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// defSignatures maps every $defs key the generated document holds to what it
// denotes independent of any key: an object by its sorted property names,
// anything else by its JSON.
func defSignatures(t *testing.T, s *schema.Schema) map[string]string {
	t.Helper()
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sigs := make(map[string]string, len(doc.Defs))
	for key, raw := range doc.Defs {
		var def struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &def); err == nil && def.Properties != nil {
			sigs[key] = "object " + strings.Join(slices.Sorted(maps.Keys(def.Properties)), " ")
			continue
		}
		sigs[key] = string(raw)
	}
	return sigs
}

// defsCollisionSources gives every key family two claimants of one bare
// spelling: a type and a datatype, a type in two schemas, a type named as an
// EDGE_ key, and two associations whose parts join alike. Every object carries
// a property no other holds.
func defsCollisionSources() map[string]string {
	return map[string]string{
		"main.yammm": `schema "geo"

import "other.yammm" as other

type Region = String[2, 2]
type Code = Integer

type Region {
	region_id String primary
	r Region
}

type C {
	c_id String primary
}

type A {
	a_id String primary
	--> B_X (one) C {
		bx_note String
	}
}

type A_b {
	ab_id String primary
	--> X (one) C {
		x_note String
	}
}

type EDGE_A_b_x_C {
	e_id String primary
}

type Stamp {
	stamp_id String primary
	--> TO (one) other.Region
}
`,
		"other.yammm": `schema "other"

type Region {
	other_region_id String primary
}
`,
	}
}

// TestMarshal_DefsKeysDependOnTheClaimsNotTheirOrder pins that reordering the
// declarations, which keeps the schema's hash, keys every def alike.
func TestMarshal_DefsKeysDependOnTheClaimsNotTheirOrder(t *testing.T) {
	base := defsCollisionSources()
	s1 := loadMulti(t, base)
	blocks := strings.Split(strings.TrimSuffix(base["main.yammm"], "\n"), "\n\n")
	head, body := blocks[:2], blocks[2:]
	slices.Reverse(body)
	s2 := loadMulti(t, map[string]string{
		"main.yammm":  strings.Join(append(slices.Clone(head), body...), "\n\n") + "\n",
		"other.yammm": base["other.yammm"],
	})
	if h1, h2 := schema.StructuralHash(s1), schema.StructuralHash(s2); h1 != h2 {
		t.Fatalf("the reorder moved the hash: %s, %s", h1, h2)
	}
	first, second := defSignatures(t, s1), defSignatures(t, s2)
	if !maps.Equal(first, second) {
		t.Errorf("keys moved under a reorder:\n%v\nthen\n%v", first, second)
	}
	for _, want := range []string{
		"geo.Region", "geo.Region.datatype", "other.Region",
		"geo.A.B_X.edge", "geo.A_b.X.edge", "geo.EDGE_A_b_x_C",
		"Code", "C", "EDGE_Stamp_to_Region",
	} {
		if _, ok := first[want]; !ok {
			t.Errorf("no $defs key %s among %v", want, slices.Sorted(maps.Keys(first)))
		}
	}
}

// TestMarshal_ADeclarationAddedNeverRebindsADefsKey pins that for a schema and
// one that adds declarations to it, every key both documents hold denotes the
// same def in both.
func TestMarshal_ADeclarationAddedNeverRebindsADefsKey(t *testing.T) {
	for name, tc := range map[string]struct{ before, added string }{
		"a datatype whose qualified key a digit-ending type once took": {
			before: "type Region2 {\n\tr2_id String primary\n}\n\ntype Region {\n\tregion_id String primary\n}\n",
			added:  "type Region = String\n",
		},
		"an association spelled like an earlier one": {
			before: "type C {\n\tc_id String primary\n}\n\ntype A_b {\n\tab_id String primary\n\t--> X (one) C {\n\t\tx_note String\n\t}\n}\n",
			added:  "type A {\n\ta_id String primary\n\t--> B_X (one) C {\n\t\tbx_note String\n\t}\n}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			const other = "schema \"other\"\n\ntype Region2 {\n\tother_id String primary\n}\n"
			head := "schema \"geo\"\n\nimport \"other.yammm\" as other\n\n"
			before := defSignatures(t, loadMulti(t, map[string]string{"main.yammm": head + tc.before, "other.yammm": other}))
			after := defSignatures(t, loadMulti(t, map[string]string{"main.yammm": head + tc.added + "\n" + tc.before, "other.yammm": other}))
			for key, sig := range before {
				if got, ok := after[key]; ok && got != sig {
					t.Errorf("%s denotes %s, and %s once the declaration is added", key, sig, got)
				}
			}
		})
	}
}

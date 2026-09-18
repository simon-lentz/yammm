package cli

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The CSV parser decides an empty foreign-key segment by the association's
// target keys, which only the schema can give it. Both CLI entry points pass
// it: a union header whose two types share an association field name, and a
// composite key with one "" component, each parse and validate.
func TestLoadAndParseCSV_ReadsEmptyKeySegmentsThroughTheSchema(t *testing.T) {
	t.Parallel()
	const src = `schema "keys"

type X {
	id String primary
}

type Y {
	code String primary
}

type Pair {
	a String primary
	b String primary
}

type A {
	aid String primary
	--> REF (_) X
}

type B {
	bid String primary
	--> REF (_) Y
	--> PAIR (_) Pair
}
`
	s, res := schema.LoadString(t.Context(), src, "keys.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}

	union := writeDataFile(t, "union.csv", "kind,aid,bid,ref._target_id,ref._target_code,pair._target_a,pair._target_b\n"+
		"A,a1,,x1,,,\n"+
		"B,,b1,,y1,,x\n")
	parsed, pres, err := LoadAndParseCSV(t.Context(), union, "", "kind", s)
	if err != nil {
		t.Fatalf("type column: %v", err)
	}
	if !pres.OK() {
		t.Fatalf("type column parse: %s", pres)
	}
	if _, vres := ValidateInstances(t.Context(), s, parsed); !vres.OK() {
		t.Errorf("type column validate: %s", vres)
	}

	typed := writeDataFile(t, "b.csv", "bid,ref._target_code,pair._target_a,pair._target_b\nb1,y1,,x\n")
	parsed, pres, err = LoadAndParseCSV(t.Context(), typed, "B", "", s)
	if err != nil {
		t.Fatalf("typed: %v", err)
	}
	if !pres.OK() {
		t.Fatalf("typed parse: %s", pres)
	}
	pair, _ := parsed["B"][0].Properties["pair"].(map[string]any)
	if pair["_target_a"] != "" || pair["_target_b"] != "x" {
		t.Errorf("typed pair: got %#v, want _target_a \"\" beside _target_b x", parsed["B"][0].Properties["pair"])
	}
}

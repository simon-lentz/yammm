package jschema

import (
	"fmt"
	"strings"
	"testing"
)

// A $defs key embeds a schema name, which may hold any character; the $ref
// naming it must be a valid URI fragment that resolves to it.
func TestMarshal_RefToASchemaNameNeedingPercentEncodingResolves(t *testing.T) {
	for name, wantRef := range map[string]string{
		"a%b":   "#/$defs/a%25b.Region",
		"a%2Fb": "#/$defs/a%252Fb.Region",
		"a b":   "#/$defs/a%20b.Region",
		"a/b~c": "#/$defs/a~1b~0c.Region",
		"ü#x":   "#/$defs/%C3%BC%23x.Region",
		`a"b`:   "#/$defs/a%22b.Region",
	} {
		t.Run(name, func(t *testing.T) {
			s := loadMulti(t, map[string]string{
				"main.yammm": `schema "geo"

import "common.yammm" as common

type Region {
	id String primary
}

type County {
	fips String primary
	--> IN_REGION (one) common.Region
}
`,
				"common.yammm": fmt.Sprintf(`schema %q

type Region {
	code String primary
	name String required
}
`, name),
			})
			out, err := Marshal(s)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !strings.Contains(string(out), `"$ref": "`+wantRef+`"`) {
				t.Errorf("no $ref %s to the imported Region in:\n%s", wantRef, out)
			}
			compiled := compileEmitted(t, s)
			if err := validateEmitted(t, compiled, []byte(`{"common.Region": [{"code": "r", "name": "n"}]}`)); err != nil {
				t.Errorf("a valid imported Region fails: %v", err)
			}
			if validateEmitted(t, compiled, []byte(`{"common.Region": [{"code": "r"}]}`)) == nil {
				t.Error("an imported Region missing its required name passes, so the $ref resolves to the wrong def")
			}
		})
	}
}

func TestRefKey_InvertsRefTo(t *testing.T) {
	for _, key := range []string{"Person", "common.Region", "a/b.X", "odd~name.X", "a%b.X", "a%2Fb.X", "ü #?.X", "~1/~0"} {
		ref, ok := refTo(key).obj[0].V.stringValue()
		if !ok {
			t.Fatalf("refTo(%q) holds no string", key)
		}
		got, err := refKey(ref)
		if err != nil || got != key {
			t.Errorf("refKey(%q) = %q, %v; want %q", ref, got, err, key)
		}
	}
	if _, err := refKey("#/$defs/a%zzb"); err == nil {
		t.Error("refKey accepted a malformed percent escape")
	}
}

// Go randomises map iteration, so a walk in map order names a different
// broken reference from run to run.
func TestSelfCheck_ReportsOneDanglingRefOnEveryRun(t *testing.T) {
	var members []string
	for i := range 8 {
		members = append(members, fmt.Sprintf(`"p%d": {"$ref": "#/$defs/Ghost%d"}`, i, i))
	}
	doc := []byte(`{"properties": {` + strings.Join(members, ", ") + `}}`)
	want := `jschema: self-check: $ref "#/$defs/Ghost0" resolves to no emitted $defs entry`
	for range 200 {
		err := selfCheck(doc, map[string]bool{})
		if err == nil || err.Error() != want {
			t.Fatalf("selfCheck = %v, want %s", err, want)
		}
	}
}

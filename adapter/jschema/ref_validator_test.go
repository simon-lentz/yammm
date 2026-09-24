package jschema

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestMarshal_EveryRefResolvesUnderAValidator holds the self-check to a second
// implementation. An imported schema is named with each hard spelling, every
// byte written as a \xHH escape, and the entry schema declares a type of the
// same name, so the imported type's key carries the schema name. Either the
// load refuses the name, or Marshal's document compiles under a draft 2020-12
// validator whose own resolution of each $ref reaches the right definition.
func TestMarshal_EveryRefResolvesUnderAValidator(t *testing.T) {
	for _, name := range []string{"g\xff", "g\xff%", "a\xe2\x82", "\uFFFD", "g\uFFFD%", "a%b", "a%2Fb", "ü#x", "a/b~c", "a b"} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			var lit strings.Builder
			for i := range len(name) {
				fmt.Fprintf(&lit, `\x%02x`, name[i])
			}
			sources := map[string][]byte{
				"main.yammm": []byte("schema \"geo\"\n\nimport \"common.yammm\" as common\n\n" +
					"type Region {\n\tid String primary\n}\n\n" +
					"type County {\n\tfips String primary\n\t--> IN_REGION (one) common.Region\n}\n"),
				"common.yammm": []byte("schema \"" + lit.String() + "\"\n\n" +
					"type Region {\n\tcode String primary\n\tname String required\n}\n"),
			}
			s, res := schema.LoadSourcesWithEntry(context.Background(), sources, "main.yammm", ".", schema.WithSourcesOnly(true))
			if res.HasErrors() {
				return
			}
			compiled := compileEmitted(t, s)
			if err := validateEmitted(t, compiled, []byte(`{"common.Region": [{"code": "r", "name": "n"}]}`)); err != nil {
				t.Errorf("a valid imported Region fails: %v", err)
			}
			if validateEmitted(t, compiled, []byte(`{"common.Region": [{"code": "r"}]}`)) == nil {
				t.Error("an imported Region missing its required name passes, so the $ref reaches the wrong definition")
			}
			if err := validateEmitted(t, compiled, []byte(`{"County": [{"fips": "f", "in_region": {"_target_code": "r"}}]}`)); err != nil {
				t.Errorf("a County linking a Region fails, so the edge's $ref does not resolve: %v", err)
			}
			if validateEmitted(t, compiled, []byte(`{"County": [{"fips": "f", "in_region": {"_target_name": "r"}}]}`)) == nil {
				t.Error("an edge naming no key of Region passes, so the edge's $ref reaches the wrong definition")
			}
		})
	}
}

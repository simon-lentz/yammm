package jschema

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	yamjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// compileEmitted Marshals s and compiles the result with a real JSON Schema
// draft 2020-12 implementation. Compilation succeeding is itself an
// assertion: the emitted document conforms to the 2020-12 dialect, not just
// to json.Valid. Format assertions are enabled so the emitted advisory
// "format" keywords (uuid, date, date-time) become checkable — matching the
// constraints yammm itself enforces for those kinds.
func compileEmitted(t *testing.T, s *schema.Schema) *jsonschema.Schema {
	t.Helper()
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("emitted document does not decode: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("emitted.json", doc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	compiled, err := c.Compile("emitted.json")
	if err != nil {
		t.Fatalf("emitted document does not compile as draft 2020-12: %v", err)
	}
	return compiled
}

// validateEmitted validates a data file against the compiled emitted schema.
// The instance is decoded with the library's own UnmarshalJSON so numbers
// carry the representation its validator expects.
func validateEmitted(t *testing.T, compiled *jsonschema.Schema, data []byte) error {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("data file does not decode: %v", err)
	}
	return compiled.Validate(doc)
}

// yammmErrors runs a data file through the yammm ingestion path the emitted
// schema mirrors — ParseObject, then instance validation per type key — and
// returns every error, empty when the file is fully clean.
func yammmErrors(t *testing.T, s *schema.Schema, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	adapter, err := yamjson.New(nil)
	if err != nil {
		t.Fatalf("json.New: %v", err)
	}
	var errs []string
	raws, res := adapter.ParseObject(ctx, location.MustNewSourceID("test://jschema/contract.json"), data)
	if res.HasErrors() {
		errs = append(errs, res.Err().Error())
	}
	v := instance.NewValidator(s)
	for _, typeName := range slices.Sorted(maps.Keys(raws)) {
		if _, vres := v.Validate(ctx, typeName, raws[typeName]); vres.HasErrors() {
			errs = append(errs, vres.Err().Error())
		}
	}
	return errs
}

// readData reads a testdata sample file.
func readData(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

// TestContractAlignment is the trust anchor for the generator: for every
// corpus case, a valid data file must pass BOTH the yammm ingestion path and
// the freshly emitted schema, and an invalid data file (one violation per
// instance) must fail BOTH. A disagreement in either direction means the
// emitted schema describes a different language than yammm accepts.
func TestContractAlignment(t *testing.T) {
	cases := []string{
		"scalars",
		"named",
		"inline_enum",
		"relations",
		"edge_props",
		"composite_pk",
		"inheritance",
		"imports/main",
		"imports/collision_main",
		"patterns",
		"timestamp_formats",
		"docs",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			s := loadSchema(t, name)
			compiled := compileEmitted(t, s)

			t.Run("data", func(t *testing.T) {
				data := readData(t, name+".data.json")
				if errs := yammmErrors(t, s, data); len(errs) > 0 {
					t.Errorf("yammm rejected valid data:\n%s", strings.Join(errs, "\n"))
				}
				if err := validateEmitted(t, compiled, data); err != nil {
					t.Errorf("emitted schema rejected valid data:\n%v", err)
				}
			})

			t.Run("baddata", func(t *testing.T) {
				bad := readData(t, name+".baddata.json")
				if errs := yammmErrors(t, s, bad); len(errs) == 0 {
					t.Error("yammm accepted invalid data")
				}
				if err := validateEmitted(t, compiled, bad); err == nil {
					t.Error("emitted schema accepted invalid data")
				}
			})
		})
	}
}

// TestContractAsymmetry_CaseVariantPropertyNames pins an intended
// divergence: the instance layer matches property names case-insensitively
// by default, which JSON Schema cannot express, so the emitted schema
// targets canonical-name authoring. A case-variant file stays valid under
// yammm while the editor flags it toward the canonical spelling.
func TestContractAsymmetry_CaseVariantPropertyNames(t *testing.T) {
	s := loadSchema(t, "scalars")
	compiled := compileEmitted(t, s)
	data := []byte(`{"County":[{"fips":"12345","Name":"Alpha","population":1,"active":true}]}`)

	if errs := yammmErrors(t, s, data); len(errs) > 0 {
		t.Errorf("yammm must accept case-variant property names:\n%s", strings.Join(errs, "\n"))
	}
	if err := validateEmitted(t, compiled, data); err == nil {
		t.Error("emitted schema must flag case-variant property names (canonical-name authoring)")
	}
}

// TestContractAsymmetry_RequiredAssociationAbsent pins the deliberate
// two-sided ACCEPTANCE of an absent required association: the instance layer
// defers association presence to graph assembly, and the emitted schema
// mirrors that by never listing associations in "required" — a per-file
// requirement would flag batch-authored files whose references resolve
// across files.
func TestContractAsymmetry_RequiredAssociationAbsent(t *testing.T) {
	s := loadSchema(t, "relations")
	compiled := compileEmitted(t, s)
	data := []byte(`{"Car":[{"vin":"V9","wheels":[{"position":"FL"}]}]}`)

	if errs := yammmErrors(t, s, data); len(errs) > 0 {
		t.Errorf("yammm must accept an absent required association per-file:\n%s", strings.Join(errs, "\n"))
	}
	if err := validateEmitted(t, compiled, data); err != nil {
		t.Errorf("emitted schema must accept an absent required association per-file:\n%v", err)
	}
}

// TestContractAsymmetry_CustomTimestampFormat pins the direction of the
// custom-format degradation: a custom Timestamp layout is not expressible as
// a JSON Schema assertion, so the emitted schema accepts any string there
// while yammm enforces the layout. The editor under-flags; yammm still
// catches the error — degradation never runs the other way.
func TestContractAsymmetry_CustomTimestampFormat(t *testing.T) {
	s := loadSchema(t, "timestamp_formats")
	compiled := compileEmitted(t, s)
	data := []byte(`{"Reading":[{"id":"R9","taken_at":"2026-07-21T12:00:00Z","logged_at":"21/07/2026"}]}`)

	if errs := yammmErrors(t, s, data); len(errs) == 0 {
		t.Error("yammm must reject a value violating a custom Timestamp layout")
	}
	if err := validateEmitted(t, compiled, data); err != nil {
		t.Errorf("emitted schema must accept any string for a custom Timestamp layout:\n%v", err)
	}
}

// TestKnownDivergence_DataTypeEdgeProperty pins a current instance-layer
// limitation: schema completion resolves alias constraints for TYPE
// properties only, so a DataType-typed EDGE property keeps an unresolved
// AliasConstraint and the instance layer rejects every value for it
// ("unresolved alias constraint"), valid or not. The emitted schema
// implements the documented wire contract (the edge property validates
// against its DataType, here a $ref), so the two sides diverge on exactly
// this member. If this test starts failing on the yammm side, the
// instance-layer limitation has been fixed: restore a DataType-typed edge
// property to the edge_props sample-data pair so the alignment suite covers
// it again, and delete this pin.
func TestKnownDivergence_DataTypeEdgeProperty(t *testing.T) {
	s := loadSchema(t, "edge_props")
	compiled := compileEmitted(t, s)

	valid := []byte(`{"County":[{"fips":"06001","in_state":{"_target_code":"CA","since":1850,"code_tag":"CA"}}]}`)
	if errs := yammmErrors(t, s, valid); len(errs) == 0 {
		t.Error("instance layer accepted a DataType-typed edge property value; the alias-resolution limitation appears fixed — fold this case back into the alignment corpus")
	}
	if err := validateEmitted(t, compiled, valid); err != nil {
		t.Errorf("emitted schema must accept a valid DataType-typed edge property value:\n%v", err)
	}

	invalid := []byte(`{"County":[{"fips":"06001","in_state":{"_target_code":"CA","since":1850,"code_tag":"C"}}]}`)
	if err := validateEmitted(t, compiled, invalid); err == nil {
		t.Error("emitted schema must reject a DataType-constraint-violating edge property value")
	}
}

// TestKnownDivergence_ImportedTypeEdgeTarget pins a current instance-layer
// limitation: edge-target resolution uses the syntactic relation target ref
// against the ENTRY schema, so an association declared on an imported type —
// whose target ref is qualified by the imported schema's own alias
// ("base.Basin", meaningful only inside "common") — cannot resolve when
// instances of that type are validated from the entry file ("edge target
// type ... not found"). The resolved relation TargetID is absolute and would
// name the right type. The emitted schema resolves in the declaring schema,
// so its EDGE_ reference validates fine. If this test starts failing on the
// yammm side, the resolution limitation has been fixed: restore the
// "in_basin" edge to the imports/main sample-data pair and delete this pin.
func TestKnownDivergence_ImportedTypeEdgeTarget(t *testing.T) {
	s := loadSchema(t, "imports/main")
	compiled := compileEmitted(t, s)

	valid := []byte(`{"common.Region":[{"code":"R0001","name":"Alpha Region","in_basin":{"_target_id":"B1"}}]}`)
	if errs := yammmErrors(t, s, valid); len(errs) == 0 {
		t.Error("instance layer resolved an imported type's edge target; the resolution limitation appears fixed — fold this case back into the alignment corpus")
	}
	if err := validateEmitted(t, compiled, valid); err != nil {
		t.Errorf("emitted schema must accept a valid edge on an imported type:\n%v", err)
	}

	invalid := []byte(`{"common.Region":[{"code":"R0001","name":"Alpha Region","in_basin":{}}]}`)
	if err := validateEmitted(t, compiled, invalid); err == nil {
		t.Error("emitted schema must reject an edge object missing its foreign-key field")
	}
}

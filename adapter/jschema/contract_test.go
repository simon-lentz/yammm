package jschema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	yamjson "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
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
	var errs []string
	raws, res := yamjson.New().ParseObject(ctx, location.MustNewSourceID("test://jschema/contract.json"), data)
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

// instanceDoc is a single-instance projection of a corpus data file: one type
// key wrapping exactly one instance, with a label naming its origin.
type instanceDoc struct {
	label string
	data  []byte
}

// splitInstances projects a type-keyed array-of-instances document into one
// single-instance document per instance, preserving each instance's original
// bytes. Keys are visited in sorted order so labels are deterministic. It backs
// the per-instance baddata contract in TestContractAlignment: a whole-file
// check passes the moment ANY instance fails, so each instance is projected out
// and asserted on its own.
func splitInstances(t *testing.T, data []byte) []instanceDoc {
	t.Helper()
	var doc map[string][]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("data is not a type-keyed array-of-instances document: %v", err)
	}
	var docs []instanceDoc
	for _, key := range slices.Sorted(maps.Keys(doc)) {
		for i, inst := range doc[key] {
			single, err := json.Marshal(map[string][]json.RawMessage{key: {inst}})
			if err != nil {
				t.Fatalf("re-marshal %s[%d]: %v", key, i, err)
			}
			docs = append(docs, instanceDoc{label: fmt.Sprintf("%s_%d", key, i), data: single})
		}
	}
	return docs
}

// TestContractAlignment is the trust anchor for the generator: for every
// corpus case, a valid data file must pass BOTH the yammm ingestion path and
// the freshly emitted schema, and every instance of an invalid data file —
// each carrying one isolated violation — must fail BOTH on its own. A
// disagreement in either direction means the emitted schema describes a
// different language than yammm accepts. The baddata half runs per instance,
// not whole-file: a whole-file check passes as soon as ANY instance fails,
// which would let a regression that wrongly accepts one specific instance hide
// behind its siblings.
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
				docs := splitInstances(t, readData(t, name+".baddata.json"))
				if len(docs) == 0 {
					t.Fatal("baddata file has no instances to check")
				}
				for _, doc := range docs {
					t.Run(doc.label, func(t *testing.T) {
						if errs := yammmErrors(t, s, doc.data); len(errs) == 0 {
							t.Errorf("yammm accepted an invalid instance in isolation:\n%s", doc.data)
						}
						if err := validateEmitted(t, compiled, doc.data); err == nil {
							t.Errorf("emitted schema accepted an invalid instance in isolation:\n%s", doc.data)
						}
					})
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

// yammmStageCodes runs a data file through ParseObject, instance validation
// per type key, and graph assembly and Check — the stages every yammm data
// command runs — and returns the codes each stage reported, keyed by stage.
func yammmStageCodes(t *testing.T, s *schema.Schema, data []byte) map[string][]diag.Code {
	t.Helper()
	ctx := context.Background()
	codes := map[string][]diag.Code{}
	collect := func(stage string, r diag.Result) {
		for issue := range r.Errors() {
			codes[stage] = append(codes[stage], issue.Code())
		}
	}
	raws, res := yamjson.New().ParseObject(ctx, location.MustNewSourceID("test://jschema/contract.json"), data)
	collect("parse", res)
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range slices.Sorted(maps.Keys(raws)) {
		valids, vres := v.Validate(ctx, typeName, raws[typeName])
		collect("validate", vres)
		for _, vi := range valids {
			if vi != nil {
				collect("graph", g.Add(ctx, vi))
			}
		}
	}
	collect("graph", g.Check(ctx))
	return codes
}

// assertOnlyStageCode fails unless the one error yammm reports is want, from
// stage.
func assertOnlyStageCode(t *testing.T, got map[string][]diag.Code, stage string, want diag.Code) {
	t.Helper()
	if len(got) != 1 || !slices.Equal(got[stage], []diag.Code{want}) {
		t.Errorf("yammm must report %s alone, from %s; got %v", want, stage, got)
	}
}

// TestContractAsymmetry_EditorCannotCompareInstances pins the divergences the
// package doc's Fidelity Caveats list first: a check that compares
// instances, or evaluates an expression, has no JSON Schema form, so the
// editor accepts each file below and yammm refuses it at the stage named.
func TestContractAsymmetry_EditorCannotCompareInstances(t *testing.T) {
	const src = `schema "fleet"

type Person {
	id String primary
	lo Integer
	hi Integer
	! "lo_le_hi" lo <= hi
}

type Car {
	vin String primary
	--> OWNER (one) Person
	*-> WHEELS (many) Wheel
}

part type Wheel {
	position String primary
}

type Garage {
	id String primary
	--> CARS (one:many) Car
}

type Tree {
	id String primary
	*-> NODES (many) Node
}

part type Node {
	id String primary
	*-> KIDS (many) Node
}
`
	s, res := schema.LoadString(context.Background(), src, "test://fleet.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	compiled := compileEmitted(t, s)
	cases := []struct {
		name  string
		data  string
		stage string
		code  diag.Code
	}{
		{"duplicate primary key", `{"Person":[{"id":"p"},{"id":"p"}]}`, "graph", diag.E_DUPLICATE_PK},
		{"duplicate composed primary key", `{"Person":[{"id":"p"}],"Car":[{"vin":"v","owner":{"_target_id":"p"},"wheels":[{"position":"FL"},{"position":"FL"}]}]}`, "validate", diag.E_DUPLICATE_COMPOSED_PK},
		{"required association names no instance", `{"Car":[{"vin":"v","owner":{"_target_id":"nobody"}}]}`, "graph", diag.E_UNRESOLVED_REQUIRED},
		{"required association absent", `{"Car":[{"vin":"v"}]}`, "graph", diag.E_UNRESOLVED_REQUIRED},
		{"required to-many association empty", `{"Garage":[{"id":"g","cars":[]}]}`, "graph", diag.E_UNRESOLVED_REQUIRED},
		{"invariant", `{"Person":[{"id":"p","lo":3,"hi":2}]}`, "validate", diag.E_INVARIANT_FAIL},
		{"composition deeper than MaxComposedDepth", deepTree(instance.MaxComposedDepth + 1), "validate", diag.E_COMPOSITION_DEPTH_EXCEEDED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			if err := validateEmitted(t, compiled, data); err != nil {
				t.Errorf("emitted schema must accept the file:\n%v", err)
			}
			assertOnlyStageCode(t, yammmStageCodes(t, s, data), tc.stage, tc.code)
		})
	}
}

// TestContractAsymmetry_ValueAndNameReading pins the divergences in how one
// value or one name is read, each in the direction the package doc states.
func TestContractAsymmetry_ValueAndNameReading(t *testing.T) {
	scalars := loadSchema(t, "scalars")
	relations := loadSchema(t, "relations")
	const person = `"Person":[{"id":"0e4ac47a-8b6f-4bb9-b1e5-6a8f6c2f1a4d","name":"Avery"}]`

	t.Run("case-variant relation field and target key", func(t *testing.T) {
		data := []byte(`{` + person + `,"Car":[{"vin":"V1","wheels":[{"position":"FL"}],"Owner":{"_TARGET_ID":"0e4ac47a-8b6f-4bb9-b1e5-6a8f6c2f1a4d"}}]}`)
		if got := yammmStageCodes(t, relations, data); len(got) != 0 {
			t.Errorf("yammm must accept case-variant relation field names and _target_ keys; got %v", got)
		}
		if err := validateEmitted(t, compileEmitted(t, relations), data); err == nil {
			t.Error("emitted schema must flag a case-variant relation field name")
		}
	})

	scalarCases := []struct {
		name         string
		data         string
		editorAccept bool
		stage        string
		code         diag.Code
	}{
		{"null for an optional property", `{"County":[{"fips":"12345","name":"Alpha","population":1,"active":true,"density":null}]}`, false, "", diag.Code{}},
		{"repeated member name", `{"County":[{"fips":"12345","name":"Alpha","name":"Beta","population":1,"active":true}]}`, true, "parse", diag.E_ADAPTER_PARSE},
		{"repeated envelope key", `{"County":[{"fips":"12345","name":"Alpha","population":1,"active":true}],"County":[{"fips":"12346","name":"Beta","population":1,"active":true}]}`, true, "parse", diag.E_ADAPTER_PARSE},
	}
	compiled := compileEmitted(t, scalars)
	for _, tc := range scalarCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			err := validateEmitted(t, compiled, data)
			if tc.editorAccept && err != nil {
				t.Errorf("emitted schema must accept the file:\n%v", err)
			}
			if !tc.editorAccept && err == nil {
				t.Error("emitted schema must flag the file")
			}
			got := yammmStageCodes(t, scalars, data)
			if tc.stage == "" {
				if len(got) != 0 {
					t.Errorf("yammm must accept the file; got %v", got)
				}
				return
			}
			assertOnlyStageCode(t, got, tc.stage, tc.code)
		})
	}

	t.Run("null reads as absent", func(t *testing.T) {
		const data = `[{"fips":"12345","name":"Alpha","population":1,"active":true,"density":null}]`
		raws, res := yamjson.New().ParseObject(t.Context(), location.MustNewSourceID("test://jschema/null.json"), []byte(`{"County":`+data+`}`))
		if !res.OK() {
			t.Fatalf("parse: %v", res)
		}
		valids, vres := instance.NewValidator(scalars).Validate(t.Context(), "County", raws["County"])
		if !vres.OK() || len(valids) != 1 || valids[0] == nil {
			t.Fatalf("validate: %v", vres)
		}
		if _, present := valids[0].Property("density"); present {
			t.Error("a null optional property must read as absent")
		}
	})
}

// TestContract_AKeyTheEnvelopeDoesNotNameIsRefusedByBoth pins one key set on
// both sides: the envelope names no part or abstract type, and instance
// validation refuses either name before it answers an empty batch.
func TestContract_AKeyTheEnvelopeDoesNotNameIsRefusedByBoth(t *testing.T) {
	for _, fx := range []struct {
		fixture, key string
		code         diag.Code
	}{
		{"relations", "Wheel", diag.E_PART_TYPE_DIRECT},
		{"inheritance", "GeoEntity", diag.E_ABSTRACT_TYPE},
	} {
		s := loadSchema(t, fx.fixture)
		data := []byte(`{"` + fx.key + `":[]}`)
		assertOnlyStageCode(t, yammmStageCodes(t, s, data), "validate", fx.code)
		if err := validateEmitted(t, compileEmitted(t, s), data); err == nil {
			t.Errorf("%s: emitted schema must flag the key %s", fx.fixture, fx.key)
		}
	}
}

// TestContractAsymmetry_Numbers pins the number divergence in both directions.
// yammm's Integer is an integer literal an int64 holds and its Float any literal
// read to its nearest finite float64; JSON Schema's "integer" is a number with
// no fractional part whatever its spelling, and this validator reads a number
// exactly. So the editor accepts numbers yammm refuses, and flags a Float
// literal just past a bound that yammm, judging its nearest float64, accepts.
func TestContractAsymmetry_Numbers(t *testing.T) {
	s, res := schema.LoadString(context.Background(), `schema "n"

type N {
	id String primary
	i Integer
	f Float
	u Float[_, 1.0]
}
`, "test://n.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	compiled := compileEmitted(t, s)
	refusedByYammm := []struct{ name, data string }{
		{"integer 2^63", `{"N":[{"id":"a","i":9223372036854775808}]}`},
		{"integer 1e20", `{"N":[{"id":"a","i":100000000000000000000}]}`},
		{"whole decimal literal on an Integer", `{"N":[{"id":"a","i":5.0}]}`},
		{"whole exponent literal on an Integer", `{"N":[{"id":"a","i":1e2}]}`},
		{"float beyond float64", `{"N":[{"id":"a","f":1e400}]}`},
	}
	for _, tc := range refusedByYammm {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			if err := validateEmitted(t, compiled, data); err != nil {
				t.Errorf("emitted schema must accept the number:\n%v", err)
			}
			assertOnlyStageCode(t, yammmStageCodes(t, s, data), "validate", diag.E_TYPE_MISMATCH)
		})
	}
	t.Run("a Float just past its bound is its nearest float64 to yammm", func(t *testing.T) {
		data := []byte(`{"N":[{"id":"a","u":1.00000000000000001}]}`)
		if err := validateEmitted(t, compiled, data); err == nil {
			t.Error("an exact validator must flag a literal above the maximum")
		}
		if got := yammmStageCodes(t, s, data); len(got) != 0 {
			t.Errorf("yammm reads the literal as float64 1 and must accept it; got %v", got)
		}
	})
	t.Run("a fractional literal on an Integer is refused by both", func(t *testing.T) {
		data := []byte(`{"N":[{"id":"a","i":1.0000000000000000001}]}`)
		if err := validateEmitted(t, compiled, data); err == nil {
			t.Error("emitted schema must flag a fractional value on an Integer")
		}
		assertOnlyStageCode(t, yammmStageCodes(t, s, data), "validate", diag.E_TYPE_MISMATCH)
	})
}

// TestContractAsymmetry_Formats pins the format divergence in both
// validator modes: format keywords are annotations by default, and an
// asserting validator reads each format by its own grammar.
func TestContractAsymmetry_Formats(t *testing.T) {
	s := loadSchema(t, "scalars")
	const county = `"fips":"12345","name":"Alpha","population":1,"active":true`

	t.Run("annotation mode accepts a malformed UUID, Date or Timestamp", func(t *testing.T) {
		out, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("emitted.json", doc); err != nil {
			t.Fatalf("AddResource: %v", err)
		}
		compiled, err := c.Compile("emitted.json")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		for _, field := range []string{`"guid":"nope"`, `"founded":"2024-02-30"`, `"updated_at":"nope"`} {
			data := []byte(`{"County":[{` + county + `,` + field + `}]}`)
			if err := validateEmitted(t, compiled, data); err != nil {
				t.Errorf("%s: an editor that does not assert formats must accept the value:\n%v", field, err)
			}
			assertOnlyStageCode(t, yammmStageCodes(t, s, data), "validate", diag.E_CONSTRAINT_FAIL)
		}
	})

	t.Run("asserting mode accepts a date-time yammm refuses", func(t *testing.T) {
		for _, stamp := range []string{"2024-01-15t10:30:00z", "2024-12-31T23:59:60Z"} {
			data := []byte(`{"County":[{` + county + `,"updated_at":"` + stamp + `"}]}`)
			if err := validateEmitted(t, compileEmitted(t, s), data); err != nil {
				t.Errorf("%s: an asserting validator reads RFC 3339 and must accept it:\n%v", stamp, err)
			}
			assertOnlyStageCode(t, yammmStageCodes(t, s, data), "validate", diag.E_CONSTRAINT_FAIL)
		}
	})

	t.Run("asserting mode flags a UUID without hyphens", func(t *testing.T) {
		data := []byte(`{"County":[{` + county + `,"guid":"0e4ac47a8b6f4bb9b1e56a8f6c2f1a4d"}]}`)
		if err := validateEmitted(t, compileEmitted(t, s), data); err == nil {
			t.Error("an asserting validator must flag a UUID without hyphens")
		}
		if got := yammmStageCodes(t, s, data); len(got) != 0 {
			t.Errorf("yammm must accept a UUID without hyphens; got %v", got)
		}
	})
}

// deepTree returns a Tree document whose composed Node chain is depth levels
// deep, the root's own child at depth 1.
func deepTree(depth int) string {
	var b strings.Builder
	b.WriteString(`{"Tree":[{"id":"t","nodes":[`)
	for i := range depth {
		fmt.Fprintf(&b, `{"id":"n%d","kids":[`, i)
	}
	for range depth {
		b.WriteString(`]}`)
	}
	b.WriteString(`]}]}`)
	return b.String()
}

// A type and a datatype sharing one name in one schema generate, and data the
// schema accepts validates under the emitted document.
func TestMarshal_TypeAndDataTypeSharingANameGenerate(t *testing.T) {
	const src = `schema "geo"

type Region = String [2, 2]

type Region {
	id String primary
	code Region
}
`
	s := loadFixture(t, src, "test://type_dt_shared.yammm")
	compiled := compileEmitted(t, s)
	if err := validateEmitted(t, compiled, []byte(`{"Region":[{"id":"a","code":"NE"}]}`)); err != nil {
		t.Errorf("valid data refused: %v", err)
	}
	if err := validateEmitted(t, compiled, []byte(`{"Region":[{"id":"a","code":"NEW"}]}`)); err == nil {
		t.Error("a code of the wrong length validated: the property does not reference its datatype")
	}
}

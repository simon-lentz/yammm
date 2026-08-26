package jschema

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadSchema loads a testdata schema, failing on any diagnostic error.
func loadSchema(t *testing.T, name string) *schema.Schema {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name+".yammm"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	s, res := schema.Load(context.Background(), path)
	if res.HasErrors() {
		t.Fatalf("load %s: %v", name, res.Err())
	}
	return s
}

// decodeDoc parses a Marshal output document for structural assertions.
func decodeDoc(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("emitted document is not valid JSON: %v", err)
	}
	return doc
}

// docSection returns a named object member of an already-decoded document.
func docSection(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	sec, ok := doc[key].(map[string]any)
	if !ok {
		t.Fatalf("document has no object member %q", key)
	}
	return sec
}

// stringSlice converts a decoded JSON array of strings for comparison.
func stringSlice(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("expected string element, got %T", e)
		}
		out[i] = s
	}
	return out
}

// TestMarshal_Golden renders every corpus schema and compares byte-exact
// against its .json.golden. The corpus collectively covers the emitted
// document structure: envelope, per-type defs, EDGE_ defs, DataType defs,
// import flattening, name qualification, and description flow-through.
func TestMarshal_Golden(t *testing.T) {
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
		// Documented Timestamp["layout"] at all three call sites that can
		// carry a description already, plus an undocumented control.
		"documented_layouts",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			s := loadSchema(t, name)
			got, err := Marshal(s)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			yammmtest.Golden(t, name+".json", got)
		})
	}
}

// TestMarshal_Deterministic pins byte-identical output across runs on the
// same loaded schema — the property the golden corpus and consumer-side
// regenerate-and-diff flows rely on.
func TestMarshal_Deterministic(t *testing.T) {
	s := loadSchema(t, "relations")
	a, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	b, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Marshal output differs across runs on the same schema")
	}
}

// TestMarshal_Options pins the envelope configuration surface: the title
// defaults to the schema name, the description to the generated sentence,
// and "$id" is omitted entirely unless WithSchemaID is given.
func TestMarshal_Options(t *testing.T) {
	s := loadSchema(t, "scalars")

	t.Run("defaults", func(t *testing.T) {
		out, err := Marshal(s)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		doc := decodeDoc(t, out)
		if _, present := doc["$id"]; present {
			t.Error(`"$id" emitted without WithSchemaID`)
		}
		if got := doc["title"]; got != "geo" {
			t.Errorf("title = %v, want schema name %q", got, "geo")
		}
		if got := doc["description"]; got != defaultDescription {
			t.Errorf("description = %v, want the generated default", got)
		}
		if got := doc["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("$schema = %v", got)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		out, err := Marshal(
			s,
			WithSchemaID("https://example.com/geo.schema.json"),
		)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		doc := decodeDoc(t, out)
		if got := doc["$id"]; got != "https://example.com/geo.schema.json" {
			t.Errorf("$id = %v", got)
		}
	})
}

// TestMarshal_TopLevelAddressing pins the top-level key policy: entry-schema
// concrete types under their bare name, directly imported concrete types
// under their alias-qualified name (the only form the instance validator
// resolves for them), and transitively imported types reachable through
// $defs only.
func TestMarshal_TopLevelAddressing(t *testing.T) {
	s := loadSchema(t, "imports/main")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	props := docSection(t, doc, "properties")

	wantKeys := []string{"County", "common.Region"}
	gotKeys := slices.Sorted(func(yield func(string) bool) {
		for k := range props {
			if !yield(k) {
				return
			}
		}
	})
	slices.Sort(wantKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("top-level keys = %v, want %v", gotKeys, wantKeys)
	}

	defs := docSection(t, doc, "$defs")
	if _, ok := defs["Basin"]; !ok {
		t.Error(`transitively imported type "Basin" missing from $defs`)
	}
}

// TestMarshal_AbstractTypesNotEmitted pins the abstract-type emission rule:
// abstract types can never be $ref-referenced (associations require concrete
// non-part targets, compositions require part targets), so they get no $defs
// entry; their members reach the document flattened into each subtype, and
// an association they declare still yields one shared EDGE_ entry named for
// the declaring owner.
func TestMarshal_AbstractTypesNotEmitted(t *testing.T) {
	s := loadSchema(t, "inheritance")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	defs := docSection(t, doc, "$defs")

	if _, ok := defs["GeoEntity"]; ok {
		t.Error(`abstract type "GeoEntity" has a $defs entry`)
	}
	for _, want := range []string{"Region", "County", "City", "EDGE_GeoEntity_in_region_Region"} {
		if _, ok := defs[want]; !ok {
			t.Errorf("$defs missing %q", want)
		}
	}

	// Both subtypes inherit the abstract owner's association and must
	// reference its single EDGE_ entry.
	for _, typeName := range []string{"County", "City"} {
		td, ok := defs[typeName].(map[string]any)
		if !ok {
			t.Fatalf("$defs[%q] is not an object", typeName)
		}
		props, ok := td["properties"].(map[string]any)
		if !ok {
			t.Fatalf("$defs[%q] has no properties object", typeName)
		}
		assoc, ok := props["in_region"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no in_region association property", typeName)
		}
		if got := assoc["$ref"]; got != "#/$defs/EDGE_GeoEntity_in_region_Region" {
			t.Errorf("%s in_region $ref = %v", typeName, got)
		}
	}
}

// TestMarshal_RequiredContract pins the requiredness semantics: required
// properties and required compositions appear in "required" (a required
// composition also gets minItems 1; a to-one composition gets maxItems 1),
// while associations NEVER appear in "required" — their presence is enforced
// at graph assembly, not per file.
func TestMarshal_RequiredContract(t *testing.T) {
	s := loadSchema(t, "relations")
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	defs := docSection(t, doc, "$defs")

	car, ok := defs["Car"].(map[string]any)
	if !ok {
		t.Fatal(`$defs has no "Car" object`)
	}
	required := stringSlice(t, car["required"])
	if want := []string{"vin", "wheels"}; !slices.Equal(required, want) {
		t.Errorf("Car required = %v, want %v (owner association excluded)", required, want)
	}

	carProps, ok := car["properties"].(map[string]any)
	if !ok {
		t.Fatal("Car has no properties object")
	}
	wheels, ok := carProps["wheels"].(map[string]any)
	if !ok {
		t.Fatal("Car has no wheels composition property")
	}
	if got := wheels["minItems"]; got != float64(1) {
		t.Errorf("required composition wheels minItems = %v, want 1", got)
	}
	if _, present := wheels["maxItems"]; present {
		t.Error("(one:many) composition wheels must not carry maxItems")
	}

	person, ok := defs["Person"].(map[string]any)
	if !ok {
		t.Fatal(`$defs has no "Person" object`)
	}
	if got, want := stringSlice(t, person["required"]), []string{"id", "name"}; !slices.Equal(got, want) {
		t.Errorf("Person required = %v, want %v", got, want)
	}
}

// TestMarshal_ToOneCompositionMaxItems pins maxItems 1 on a (one)
// composition: the instance layer accepts any array, and graph assembly
// rejects a second child (duplicate composed primary key), so the schema
// mirrors real end-to-end enforcement.
func TestMarshal_ToOneCompositionMaxItems(t *testing.T) {
	s, res := schema.LoadString(context.Background(), `schema "fleet"

type Car {
	vin String primary
	*-> SPARE (one) Wheel
}

part type Wheel {
	serial String primary
}
`, "to_one_comp.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	doc := decodeDoc(t, out)
	defs := docSection(t, doc, "$defs")
	car, ok := defs["Car"].(map[string]any)
	if !ok {
		t.Fatal(`$defs has no "Car" object`)
	}
	props, ok := car["properties"].(map[string]any)
	if !ok {
		t.Fatal("Car has no properties object")
	}
	spare, ok := props["spare"].(map[string]any)
	if !ok {
		t.Fatal("Car has no spare composition property")
	}
	if got := spare["maxItems"]; got != float64(1) {
		t.Errorf("(one) composition maxItems = %v, want 1", got)
	}
	if got := spare["minItems"]; got != float64(1) {
		t.Errorf("required (one) composition minItems = %v, want 1", got)
	}
}

func TestSelfCheck(t *testing.T) {
	defs := map[string]bool{"Person": true, "a/b": true}

	t.Run("resolving_refs_pass", func(t *testing.T) {
		doc := []byte(`{"properties":{"p":{"$ref":"#/$defs/Person"}},"$defs":{"Person":{}}}`)
		if err := selfCheck(doc, defs); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("escaped_pointer_resolves", func(t *testing.T) {
		doc := []byte(`{"p":{"$ref":"#/$defs/a~1b"}}`)
		if err := selfCheck(doc, defs); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("unknown_ref_fails", func(t *testing.T) {
		doc := []byte(`{"p":{"$ref":"#/$defs/Ghost"}}`)
		if err := selfCheck(doc, defs); err == nil {
			t.Error("expected an error for a $ref with no $defs entry")
		}
	})

	t.Run("non_local_ref_fails", func(t *testing.T) {
		doc := []byte(`{"p":{"$ref":"https://example.com/x.json"}}`)
		if err := selfCheck(doc, defs); err == nil {
			t.Error("expected an error for a non-#/$defs/ $ref")
		}
	})

	t.Run("invalid_json_fails", func(t *testing.T) {
		if err := selfCheck([]byte(`{`), defs); err == nil {
			t.Error("expected an error for invalid JSON")
		}
	})
}

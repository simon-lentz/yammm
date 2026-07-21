package jschema

import (
	"bytes"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// normalize renders v and compacts it so comparisons are layout-independent.
func normalize(t *testing.T, v val) string {
	t.Helper()
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(renderToString(v))); err != nil {
		t.Fatalf("fragment is not valid JSON: %v\n%s", err, renderToString(v))
	}
	return compacted.String()
}

// normalizeWant compacts an expected-JSON literal.
func normalizeWant(t *testing.T, want string) string {
	t.Helper()
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, []byte(want)); err != nil {
		t.Fatalf("bad want literal: %v\n%s", err, want)
	}
	return compacted.String()
}

func mustPatterns(t *testing.T, exprs ...string) schema.PatternConstraint {
	t.Helper()
	compiled := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		compiled[i] = regexp.MustCompile(e)
	}
	return schema.NewPatternConstraint(compiled)
}

func TestSchemaForConstraint(t *testing.T) {
	cases := []struct {
		name string
		c    schema.Constraint
		want string
	}{
		{"string_unbounded", schema.NewStringConstraint(), `{"type": "string"}`},
		{"string_bounded", schema.StringLenBetween(1, 100), `{"type": "string", "minLength": 1, "maxLength": 100}`},
		{"string_min_only", schema.StringMinLen(1), `{"type": "string", "minLength": 1}`},
		{"integer_unbounded", schema.NewIntegerConstraint(), `{"type": "integer"}`},
		{"integer_bounded", schema.IntegerBetween(0, 150), `{"type": "integer", "minimum": 0, "maximum": 150}`},
		{"integer_max_only", schema.IntegerMax(9), `{"type": "integer", "maximum": 9}`},
		{"float_bounded", schema.FloatBetween(0.5, 99.5), `{"type": "number", "minimum": 0.5, "maximum": 99.5}`},
		{"boolean", schema.NewBooleanConstraint(), `{"type": "boolean"}`},
		{"enum", schema.NewEnumConstraint([]string{"red", "green"}), `{"type": "string", "enum": ["red", "green"]}`},
		{"pattern_single", mustPatterns(t, `^[A-Z]{2}$`), `{"type": "string", "pattern": "^[A-Z]{2}$"}`},
		{
			"pattern_multi_all_must_match",
			mustPatterns(t, `^[A-Z]`, `[0-9]$`),
			`{"type": "string", "allOf": [{"pattern": "^[A-Z]"}, {"pattern": "[0-9]$"}]}`,
		},
		{"uuid", schema.NewUUIDConstraint(), `{"type": "string", "format": "uuid"}`},
		{"date", schema.NewDateConstraint(), `{"type": "string", "format": "date"}`},
		{"timestamp_default", schema.NewTimestampConstraint(), `{"type": "string", "format": "date-time"}`},
		{
			"timestamp_custom_format_described_not_asserted",
			schema.NewTimestampConstraintFormatted("2006-01-02 15:04"),
			`{"type": "string", "description": "Timestamp[\"2006-01-02 15:04\"]"}`,
		},
		{
			"vector",
			schema.NewVectorConstraint(128),
			`{"type": "array", "items": {"type": "number"}, "minItems": 128, "maxItems": 128}`,
		},
		{
			"list_of_primitive_unbounded",
			schema.NewListConstraint(schema.NewStringConstraint()),
			`{"type": "array", "items": {"type": "string"}}`,
		},
		{
			"list_bounded",
			schema.ListLenBetween(schema.StringMinLen(1), 1, 10),
			`{"type": "array", "items": {"type": "string", "minLength": 1}, "minItems": 1, "maxItems": 10}`,
		},
		{
			// jschema renders inline enums in element position faithfully
			// (gogen degrades these to the primitive).
			"list_of_inline_enum_faithful",
			schema.NewListConstraint(schema.NewEnumConstraint([]string{"a", "b"})),
			`{"type": "array", "items": {"type": "string", "enum": ["a", "b"]}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schemaForConstraint(tc.c)
			if err != nil {
				t.Fatalf("schemaForConstraint: %v", err)
			}
			if g, w := normalize(t, got), normalizeWant(t, tc.want); g != w {
				t.Errorf("got  %s\nwant %s", g, w)
			}
		})
	}
}

func TestSchemaForConstraint_UnresolvedAliasIsError(t *testing.T) {
	if _, err := schemaForConstraint(schema.NewAliasConstraint("Broken", nil)); err == nil {
		t.Error("an unresolved alias reaching the mapper must be an error")
	}
}

const namedFixture = `schema "named"

type FipsCode = String [5, 5]

type County {
    fips FipsCode primary
    codes List <FipsCode>
    status Enum ["active", "merged"] required
    name String
}
`

// loadNamedType loads the alias fixture and returns the County type.
func loadNamedType(t *testing.T) *schema.Type {
	t.Helper()
	s, res := schema.LoadString(t.Context(), namedFixture, "test://named.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	typ, ok := s.Type("County")
	if !ok {
		t.Fatal("County type missing")
	}
	return typ
}

func prop(t *testing.T, typ *schema.Type, name string) *schema.Property {
	t.Helper()
	p, ok := typ.Property(name)
	if !ok {
		t.Fatalf("property %q missing", name)
	}
	return p
}

func TestSchemaForProperty(t *testing.T) {
	typ := loadNamedType(t)
	// Fake defs-table lookup: both DataType-referencing properties (direct
	// alias and list-of-alias) resolve to the FipsCode def.
	dtRef := func(p *schema.Property) (string, bool) {
		switch p.Name() {
		case "fips", "codes":
			return "FipsCode", true
		default:
			return "", false
		}
	}

	cases := []struct {
		propName string
		want     string
	}{
		{"fips", `{"$ref": "#/$defs/FipsCode"}`},
		{"codes", `{"type": "array", "items": {"$ref": "#/$defs/FipsCode"}}`},
		{"status", `{"type": "string", "enum": ["active", "merged"]}`},
		{"name", `{"type": "string"}`},
	}
	for _, tc := range cases {
		t.Run(tc.propName, func(t *testing.T) {
			got, err := schemaForProperty(prop(t, typ, tc.propName), dtRef)
			if err != nil {
				t.Fatalf("schemaForProperty: %v", err)
			}
			if g, w := normalize(t, got), normalizeWant(t, tc.want); g != w {
				t.Errorf("got  %s\nwant %s", g, w)
			}
		})
	}
}

func TestSchemaForProperty_UnregisteredAliasIsError(t *testing.T) {
	typ := loadNamedType(t)
	missing := func(*schema.Property) (string, bool) { return "", false }

	if _, err := schemaForProperty(prop(t, typ, "fips"), missing); err == nil {
		t.Error("alias property without a registered $defs key must be an error")
	}
	if _, err := schemaForProperty(prop(t, typ, "codes"), missing); err == nil {
		t.Error("list-of-alias property without a registered $defs key must be an error")
	}
}

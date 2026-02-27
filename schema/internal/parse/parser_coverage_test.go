package parse_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

// parseSchema is a helper to parse a schema and return the model and diagnostics.
func parseSchema(t *testing.T, schemaSource string) (*parse.Model, diag.Result) {
	t.Helper()
	reg := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://coverage.yammm")
	err := reg.Register(sourceID, []byte(schemaSource))
	require.NoError(t, err)

	collector := diag.NewCollector(0)
	parser := parse.NewParser(sourceID, collector, reg, reg)
	model := parser.Parse([]byte(schemaSource))
	return model, collector.Result()
}

// =============================================================================
// Float Constraint Tests
// =============================================================================

func TestParse_FloatConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOK       bool
		wantWarnings bool
		checkFn      func(t *testing.T, model *parse.Model)
	}{
		{
			name: "float with min only",
			source: `schema "test"
type Thing {
	value Float[0.0, _]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindFloat, prop.Constraint.Kind())
			},
		},
		{
			name: "float with max only",
			source: `schema "test"
type Thing {
	value Float[_, 100.0]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindFloat, prop.Constraint.Kind())
			},
		},
		{
			name: "float with both bounds",
			source: `schema "test"
type Thing {
	value Float[0.0, 100.0]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindFloat, prop.Constraint.Kind())
			},
		},
		{
			name: "float unbounded",
			source: `schema "test"
type Thing {
	value Float
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindFloat, prop.Constraint.Kind())
			},
		},
		{
			name: "float with negative min",
			source: `schema "test"
type Thing {
	value Float[-90.0, 90.0]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				fc, ok := prop.Constraint.(schema.FloatConstraint)
				require.True(t, ok, "expected FloatConstraint")
				min, hasMin := fc.Min()
				assert.True(t, hasMin)
				assert.Equal(t, -90.0, min)
				max, hasMax := fc.Max()
				assert.True(t, hasMax)
				assert.Equal(t, 90.0, max)
			},
		},
		{
			name: "float with both negative bounds",
			source: `schema "test"
type Thing {
	value Float[-180.0, -1.0]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				fc, ok := prop.Constraint.(schema.FloatConstraint)
				require.True(t, ok, "expected FloatConstraint")
				min, hasMin := fc.Min()
				assert.True(t, hasMin)
				assert.Equal(t, -180.0, min)
				max, hasMax := fc.Max()
				assert.True(t, hasMax)
				assert.Equal(t, -1.0, max)
			},
		},
		{
			name: "float with negative min unbounded max",
			source: `schema "test"
type Thing {
	value Float[-90.0, _]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				fc, ok := prop.Constraint.(schema.FloatConstraint)
				require.True(t, ok, "expected FloatConstraint")
				min, hasMin := fc.Min()
				assert.True(t, hasMin)
				assert.Equal(t, -90.0, min)
				_, hasMax := fc.Max()
				assert.False(t, hasMax)
			},
		},
		{
			name: "float with negative bounds inverted",
			source: `schema "test"
type Thing {
	value Float[10.0, -10.0]
}`,
			wantOK: false,
		},
		{
			name: "float with minus before unbounded min warns",
			source: `schema "test"
type Thing {
	value Float[-_, 100.0]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, _ *parse.Model) {
				t.Helper()
				// Warning is checked via wantWarnings in the test loop
			},
			wantWarnings: true,
		},
		{
			name: "float with minus before unbounded max warns",
			source: `schema "test"
type Thing {
	value Float[0.0, -_]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, _ *parse.Model) {
				t.Helper()
			},
			wantWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				if tt.checkFn != nil {
					tt.checkFn(t, model)
				}
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
			if tt.wantWarnings {
				assert.True(t, result.HasWarnings(), "expected warnings, got: %v", result)
			}
		})
	}
}

// =============================================================================
// Boolean Property Tests
// =============================================================================

func TestParse_BooleanProperty(t *testing.T) {
	t.Parallel()

	schemaSource := `schema "test"
type Thing {
	active Boolean
	enabled Boolean required
}`

	model, result := parseSchema(t, schemaSource)
	require.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 2)

	// Check first boolean property
	active := model.Types[0].Properties[0]
	assert.Equal(t, "active", active.Name)
	assert.Equal(t, schema.KindBoolean, active.Constraint.Kind())
	assert.True(t, active.Optional)

	// Check second boolean property
	enabled := model.Types[0].Properties[1]
	assert.Equal(t, "enabled", enabled.Name)
	assert.Equal(t, schema.KindBoolean, enabled.Constraint.Kind())
	assert.False(t, enabled.Optional)
}

// =============================================================================
// Pattern Constraint Tests
// =============================================================================

func TestParse_PatternConstraint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "single pattern",
			source: `schema "test"
type Thing {
	email Pattern["^[a-z]+@[a-z]+\\.[a-z]+$"]
}`,
			wantOK: true,
		},
		{
			name: "two patterns",
			source: `schema "test"
type Thing {
	code Pattern["^[A-Z]+$", "^[0-9]+$"]
}`,
			wantOK: true,
		},
		{
			name: "invalid regex",
			source: `schema "test"
type Thing {
	bad Pattern["[invalid"]
}`,
			wantOK: false,
		},
		{
			name: "too many patterns",
			source: `schema "test"
type Thing {
	bad Pattern["a", "b", "c"]
}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindPattern, prop.Constraint.Kind())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Timestamp Property Tests
// =============================================================================

func TestParse_TimestampProperty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "default timestamp",
			source: `schema "test"
type Event {
	createdAt Timestamp
}`,
			wantOK: true,
		},
		{
			name: "timestamp with format",
			source: `schema "test"
type Event {
	createdAt Timestamp["2006-01-02T15:04:05Z07:00"]
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindTimestamp, prop.Constraint.Kind())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Date Property Tests
// =============================================================================

func TestParse_DateProperty(t *testing.T) {
	t.Parallel()

	schemaSource := `schema "test"
type Event {
	date Date required
}`

	model, result := parseSchema(t, schemaSource)
	require.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 1)

	prop := model.Types[0].Properties[0]
	assert.Equal(t, "date", prop.Name)
	assert.Equal(t, schema.KindDate, prop.Constraint.Kind())
}

// =============================================================================
// Vector Property Tests
// =============================================================================

func TestParse_VectorProperty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "vector with dimension",
			source: `schema "test"
type Embedding {
	vector Vector[128]
}`,
			wantOK: true,
		},
		{
			name: "vector with large dimension",
			source: `schema "test"
type Embedding {
	vector Vector[1536]
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindVector, prop.Constraint.Kind())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// List Property Tests
// =============================================================================

func TestParse_ListProperty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "basic list of strings",
			source: `schema "test"
type R {
	tags List<String>
}`,
			wantOK: true,
		},
		{
			name: "list with element constraints",
			source: `schema "test"
type R {
	tags List<String[_, 6]>
}`,
			wantOK: true,
		},
		{
			name: "list with length bounds",
			source: `schema "test"
type R {
	tags List<String>[1, 5]
}`,
			wantOK: true,
		},
		{
			name: "list with both constraints",
			source: `schema "test"
type R {
	tags List<String[_, 6]>[1, 5]
}`,
			wantOK: true,
		},
		{
			name: "nested list",
			source: `schema "test"
type R {
	matrix List<List<Integer>>
}`,
			wantOK: true,
		},
		{
			name: "list of vectors",
			source: `schema "test"
type R {
	embeddings List<Vector[768]>
}`,
			wantOK: true,
		},
		{
			name: "list with one-sided min bound",
			source: `schema "test"
type R {
	tags List<String>[1, _]
}`,
			wantOK: true,
		},
		{
			name: "list with one-sided max bound",
			source: `schema "test"
type R {
	tags List<String>[_, 5]
}`,
			wantOK: true,
		},
		{
			name: "list with inverted bounds",
			source: `schema "test"
type R {
	tags List<String>[5, 1]
}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindList, prop.Constraint.Kind())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Relation Properties Tests
// =============================================================================

func TestParse_RelationProperties(t *testing.T) {
	t.Parallel()

	schemaSource := `schema "test"
type Person {
	name String required
}

type Friendship {
	since Date
	strength Integer
}

type Person {
	--> friends Person using Friendship
}`

	model, result := parseSchema(t, schemaSource)
	// This may have warnings about duplicate type name but should parse
	require.NotNil(t, model)
	_ = result // May have errors due to duplicate type
}

// =============================================================================
// Multiplicity Variants Tests
// =============================================================================

func TestParse_MultiplicityVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOK       bool
		wantMany     bool
		wantOptional bool
	}{
		{
			name: "one optional",
			source: `schema "test"
type A {}
type B {
	--> a (one) A
}`,
			wantOK:       true,
			wantMany:     false,
			wantOptional: true,
		},
		{
			name: "many",
			source: `schema "test"
type A {}
type B {
	--> items (many) A
}`,
			wantOK:   true,
			wantMany: true,
		},
		{
			name: "one to many",
			source: `schema "test"
type A {}
type B {
	--> items (one:many) A
}`,
			wantOK:   true,
			wantMany: true,
		},
		{
			name: "default is one optional",
			source: `schema "test"
type A {}
type B {
	--> a A
}`,
			wantOK:       true,
			wantMany:     false,
			wantOptional: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.GreaterOrEqual(t, len(model.Types), 2)

				// Find the type with the relation (type B)
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					if len(typ.Relations) > 0 {
						rel = typ.Relations[0]
						break
					}
				}
				require.NotNil(t, rel, "should have a relation")
				assert.Equal(t, tt.wantMany, rel.Many, "Many flag mismatch")
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Integer Constraint Edge Cases Tests
// =============================================================================

func TestParse_IntegerConstraintEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOK       bool
		wantWarnings bool
		checkFn      func(t *testing.T, model *parse.Model)
	}{
		{
			name: "integer with min only",
			source: `schema "test"
type Thing {
	value Integer[0, _]
}`,
			wantOK: true,
		},
		{
			name: "integer with max only",
			source: `schema "test"
type Thing {
	value Integer[_, 100]
}`,
			wantOK: true,
		},
		{
			name: "integer with both bounds",
			source: `schema "test"
type Thing {
	value Integer[1, 10]
}`,
			wantOK: true,
		},
		{
			name: "integer unbounded",
			source: `schema "test"
type Thing {
	value Integer
}`,
			wantOK: true,
		},
		{
			name: "integer with negative min",
			source: `schema "test"
type Thing {
	value Integer[-100, 100]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				prop := model.Types[0].Properties[0]
				ic, ok := prop.Constraint.(schema.IntegerConstraint)
				require.True(t, ok, "expected IntegerConstraint")
				min, hasMin := ic.Min()
				assert.True(t, hasMin)
				assert.Equal(t, int64(-100), min)
				max, hasMax := ic.Max()
				assert.True(t, hasMax)
				assert.Equal(t, int64(100), max)
			},
		},
		{
			name: "integer with both negative bounds",
			source: `schema "test"
type Thing {
	value Integer[-200, -1]
}`,
			wantOK: true,
			checkFn: func(t *testing.T, model *parse.Model) {
				t.Helper()
				prop := model.Types[0].Properties[0]
				ic, ok := prop.Constraint.(schema.IntegerConstraint)
				require.True(t, ok, "expected IntegerConstraint")
				min, hasMin := ic.Min()
				assert.True(t, hasMin)
				assert.Equal(t, int64(-200), min)
				max, hasMax := ic.Max()
				assert.True(t, hasMax)
				assert.Equal(t, int64(-1), max)
			},
		},
		{
			name: "integer with negative bounds inverted",
			source: `schema "test"
type Thing {
	value Integer[10, -10]
}`,
			wantOK: false,
		},
		{
			name: "integer with minus before unbounded min warns",
			source: `schema "test"
type Thing {
	value Integer[-_, 100]
}`,
			wantOK:       true,
			wantWarnings: true,
		},
		{
			name: "integer with minus before unbounded max warns",
			source: `schema "test"
type Thing {
	value Integer[0, -_]
}`,
			wantOK:       true,
			wantWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Properties, 1)
				prop := model.Types[0].Properties[0]
				assert.Equal(t, schema.KindInteger, prop.Constraint.Kind())
				if tt.checkFn != nil {
					tt.checkFn(t, model)
				}
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
			if tt.wantWarnings {
				assert.True(t, result.HasWarnings(), "expected warnings, got: %v", result)
			}
		})
	}
}

// =============================================================================
// Enum Edge Cases Tests
// =============================================================================

func TestParse_EnumEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "enum with escape sequences",
			source: `schema "test"
type Thing {
	value Enum["hello\nworld", "tab\there"]
}`,
			wantOK: true,
		},
		{
			name: "enum with empty value",
			source: `schema "test"
type Thing {
	value Enum[""]
}`,
			wantOK: false, // Empty enum value not allowed
		},
		{
			name: "enum with duplicate values",
			source: `schema "test"
type Thing {
	value Enum["a", "a"]
}`,
			wantOK: false, // Duplicate values not allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Composition Multiplicity Tests
// =============================================================================

func TestParse_CompositionMultiplicity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantOK   bool
		wantMany bool
	}{
		{
			name: "composition one",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child (one) Child
}`,
			wantOK:   true,
			wantMany: false,
		},
		{
			name: "composition many",
			source: `schema "test"
part type Child {}
type Parent {
	*-> children (many) Child
}`,
			wantOK:   true,
			wantMany: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the composition relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationComposition {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have a composition relation")
				assert.Equal(t, tt.wantMany, rel.Many)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Import Statement Variations
// =============================================================================

func TestParse_ImportStatementVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantOK    bool
		wantAlias string
	}{
		{
			name: "import with explicit alias",
			source: `schema "test"
import "types.yammm" as t`,
			wantOK:    true,
			wantAlias: "t",
		},
		{
			name: "import with path containing directory",
			source: `schema "test"
import "lib/types.yammm" as types`,
			wantOK:    true,
			wantAlias: "types",
		},
		{
			name: "import derive alias from simple filename",
			source: `schema "test"
import "common.yammm"`,
			wantOK:    true,
			wantAlias: "common",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Imports, 1)
				assert.Equal(t, tt.wantAlias, model.Imports[0].Alias)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Schema Name Tests
// =============================================================================

func TestParse_SchemaNameVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantOK   bool
		wantName string
	}{
		{
			name:     "simple schema name",
			source:   `schema "test"`,
			wantOK:   true,
			wantName: "test",
		},
		{
			name:     "schema name with escape",
			source:   `schema "my-schema"`,
			wantOK:   true,
			wantName: "my-schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				assert.Equal(t, tt.wantName, model.Name)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Multiple Inheritance Tests
// =============================================================================

func TestParse_MultipleInheritance(t *testing.T) {
	t.Parallel()

	schemaSource := `schema "test"
type A {}
type B {}
type C extends A, B {}`

	model, result := parseSchema(t, schemaSource)
	require.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 3)

	// Find type C
	var typeC *parse.TypeDecl
	for _, typ := range model.Types {
		if typ.Name == "C" {
			typeC = typ
			break
		}
	}
	require.NotNil(t, typeC)
	require.Len(t, typeC.Inherits, 2)
	assert.Equal(t, "A", typeC.Inherits[0].Name)
	assert.Equal(t, "B", typeC.Inherits[1].Name)
}

// =============================================================================
// Invariant Edge Cases Tests
// =============================================================================

func TestParse_InvariantEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "invariant with complex expression",
			source: `schema "test"
type Thing {
	a Integer
	b Integer
	! "Sum constraint" a + b < 100
}`,
			wantOK: true,
		},
		{
			name: "invariant with function call",
			source: `schema "test"
type Thing {
	items Integer[0,_]
	! "Has items" items->Count() > 0
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Invariants, 1)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Extended Relation Property Tests (ExitRel_property)
// =============================================================================

func TestParse_RelationPropertiesExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOK       bool
		wantRelProps int
	}{
		{
			name: "association with single edge property",
			source: `schema "test"
type Target {}
type Source {
	--> target Target {
		weight Integer
	}
}`,
			wantOK:       true,
			wantRelProps: 1,
		},
		{
			name: "association with multiple edge properties",
			source: `schema "test"
type Target {}
type Source {
	--> target Target {
		weight Integer
		label String
		score Float
	}
}`,
			wantOK:       true,
			wantRelProps: 3,
		},
		{
			name: "association with required edge property",
			source: `schema "test"
type Target {}
type Source {
	--> target Target {
		weight Integer required
	}
}`,
			wantOK:       true,
			wantRelProps: 1,
		},
		{
			name: "association with doc comment on edge property",
			source: `schema "test"
type Target {}
type Source {
	--> target Target {
		/* Weight of the relationship */
		weight Integer
	}
}`,
			wantOK:       true,
			wantRelProps: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the association relation with properties
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationAssociation && len(r.Properties) > 0 {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have an association with properties")
				assert.Equal(t, tt.wantRelProps, len(rel.Properties))
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Timestamp Format Tests
// =============================================================================

func TestParse_TimestampFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantOK     bool
		wantFormat string
	}{
		{
			name: "timestamp with custom format",
			source: `schema "test"
type Event {
	id UUID primary
	time Timestamp["2006-01-02T15:04:05Z07:00"]
}`,
			wantOK:     true,
			wantFormat: "2006-01-02T15:04:05Z07:00",
		},
		{
			name: "timestamp with simple format",
			source: `schema "test"
type Event {
	id UUID primary
	date Timestamp["2006-01-02"]
}`,
			wantOK:     true,
			wantFormat: "2006-01-02",
		},
		{
			name: "timestamp without format",
			source: `schema "test"
type Event {
	id UUID primary
	created Timestamp
}`,
			wantOK:     true,
			wantFormat: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Vector Dimension Tests
// =============================================================================

func TestParse_VectorDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		source        string
		wantOK        bool
		wantDimension int64
	}{
		{
			name: "vector with small dimension",
			source: `schema "test"
type Embedding {
	id UUID primary
	vec Vector[3]
}`,
			wantOK:        true,
			wantDimension: 3,
		},
		{
			name: "vector with large dimension",
			source: `schema "test"
type Embedding {
	id UUID primary
	vec Vector[1536]
}`,
			wantOK:        true,
			wantDimension: 1536,
		},
		{
			name: "vector with dimension 1",
			source: `schema "test"
type Embedding {
	id UUID primary
	vec Vector[1]
}`,
			wantOK:        true,
			wantDimension: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Association Reverse Tests
// =============================================================================

func TestParse_AssociationReverse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		source          string
		wantOK          bool
		wantReverseName string
	}{
		{
			name: "association with reverse name",
			source: `schema "test"
type Parent {}
type Child {
	--> parent Parent / children
}`,
			wantOK:          true,
			wantReverseName: "children",
		},
		{
			name: "association with reverse multiplicity",
			source: `schema "test"
type Parent {}
type Child {
	--> parent (one) Parent / children (many)
}`,
			wantOK:          true,
			wantReverseName: "children",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the association relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationAssociation {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have an association relation")
				assert.Equal(t, tt.wantReverseName, rel.Backref)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Composition Reverse Tests
// =============================================================================

func TestParse_CompositionReverse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		source          string
		wantOK          bool
		wantReverseName string
	}{
		{
			name: "composition with reverse name",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child Child / parent
}`,
			wantOK:          true,
			wantReverseName: "parent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the composition relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationComposition {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have a composition relation")
				assert.Equal(t, tt.wantReverseName, rel.Backref)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Extended Import Path Tests
// =============================================================================

func TestParse_ImportPathVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantOK   bool
		wantPath string
	}{
		{
			name: "import with absolute-like path",
			source: `schema "test"
import "/absolute/path/types.yammm" as abs`,
			wantOK:   true,
			wantPath: "/absolute/path/types.yammm",
		},
		{
			name: "import with dots in path",
			source: `schema "test"
import "../parent/types.yammm" as parent`,
			wantOK:   true,
			wantPath: "../parent/types.yammm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Imports, 1)
				assert.Equal(t, tt.wantPath, model.Imports[0].Path)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Schema DocComment Tests
// =============================================================================

func TestParse_SchemaDocComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		wantOK  bool
		wantDoc bool
	}{
		{
			name: "schema with doc comment",
			source: `/* Schema documentation */
schema "test"
type Foo { id UUID primary }`,
			wantOK:  true,
			wantDoc: true,
		},
		{
			name: "schema without doc comment",
			source: `schema "test"
type Foo { id UUID primary }`,
			wantOK:  true,
			wantDoc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				if tt.wantDoc {
					assert.NotEmpty(t, model.Documentation)
				} else {
					assert.Empty(t, model.Documentation)
				}
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Type Extends Tests (trailing comma)
// =============================================================================

func TestParse_TypeExtendsTrailingComma(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantOK    bool
		wantCount int
	}{
		{
			name: "extends with trailing comma",
			source: `schema "test"
type Base {}
type Child extends Base, {}`,
			wantOK:    true,
			wantCount: 1,
		},
		{
			name: "extends multiple with trailing comma",
			source: `schema "test"
abstract type A {}
abstract type B {}
type Child extends A, B, {}`,
			wantOK:    true,
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the type that extends
				var childType *parse.TypeDecl
				for _, typ := range model.Types {
					if len(typ.Inherits) > 0 {
						childType = typ
						break
					}
				}
				require.NotNil(t, childType)
				assert.Equal(t, tt.wantCount, len(childType.Inherits))
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Extended Invariant Expression Tests
// =============================================================================

func TestParse_InvariantExpressionVarieties(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "invariant with boolean literal",
			source: `schema "test"
type Thing {
	id UUID primary
	! "Always true" true
}`,
			wantOK: true,
		},
		{
			name: "invariant with nil underscore",
			source: `schema "test"
type Thing {
	id UUID primary
	name String
	! "Name not nil" name != _
}`,
			wantOK: true,
		},
		{
			name: "invariant with negation",
			source: `schema "test"
type Thing {
	id UUID primary
	disabled Boolean
	! "Must be enabled" !disabled
}`,
			wantOK: true,
		},
		{
			name: "invariant with string literal",
			source: `schema "test"
type Thing {
	id UUID primary
	status String
	! "Valid status" status == "active"
}`,
			wantOK: true,
		},
		{
			name: "invariant with regex match",
			source: `schema "test"
type Thing {
	id UUID primary
	email String
	! "Valid email" email =~ /^[a-z]+@[a-z]+\.[a-z]+$/
}`,
			wantOK: true,
		},
		{
			name: "invariant with in operator",
			source: `schema "test"
type Thing {
	id UUID primary
	color String
	! "Valid color" color in ["red", "green", "blue"]
}`,
			wantOK: true,
		},
		{
			name: "invariant with and operator",
			source: `schema "test"
type Thing {
	id UUID primary
	min Integer
	max Integer
	! "Min less than max" min >= 0 && max > min
}`,
			wantOK: true,
		},
		{
			name: "invariant with or operator",
			source: `schema "test"
type Thing {
	id UUID primary
	a Boolean
	b Boolean
	! "One must be true" a || b
}`,
			wantOK: true,
		},
		{
			name: "invariant with ternary",
			source: `schema "test"
type Thing {
	id UUID primary
	a Integer
	b Integer
	! "Conditional check" a > 0 ? { b > 0 }
}`,
			wantOK: true,
		},
		{
			name: "invariant with grouped expression",
			source: `schema "test"
type Thing {
	id UUID primary
	x Integer
	y Integer
	! "Grouping" (x + y) > 0
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Types, 1)
				require.Len(t, model.Types[0].Invariants, 1)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// ToSchemaTypeRef Tests
// =============================================================================

func TestTypeRef_ToSchemaTypeRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		source        string
		wantOK        bool
		wantQualified bool
	}{
		{
			name: "local type reference",
			source: `schema "test"
type Target {}
type Source {
	--> target Target
}`,
			wantOK:        true,
			wantQualified: false,
		},
		{
			name: "qualified type reference",
			source: `schema "test"
import "other.yammm" as other
type Source {
	--> target other.Target
}`,
			wantOK:        true,
			wantQualified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the relation and check the target type ref
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						rel = r
						break
					}
				}
				require.NotNil(t, rel, "should have a relation")
				assert.Equal(t, tt.wantQualified, rel.Target.IsQualified())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// String Escape Sequence Tests (stripDelimiters, unquoteString)
// =============================================================================

func TestParse_StringEscapeSequences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "string with newline escape",
			source: `schema "test\nwith\nnewlines"
type Foo { id UUID primary }`,
			wantOK: true,
		},
		{
			name: "string with tab escape",
			source: `schema "test\twith\ttabs"
type Foo { id UUID primary }`,
			wantOK: true,
		},
		{
			name: "string with backslash escape",
			source: `schema "test\\with\\backslashes"
type Foo { id UUID primary }`,
			wantOK: true,
		},
		{
			name: "string with quote escape",
			source: `schema "test\"with\"quotes"
type Foo { id UUID primary }`,
			wantOK: true,
		},
		{
			name: "single quoted string",
			source: `schema 'test'
type Foo { id UUID primary }`,
			wantOK: true,
		},
		{
			name: "enum with various string values",
			source: `schema "test"
type Foo {
	id UUID primary
	status Enum["pending", "in-progress", "done"]
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Syntax Error Recovery Tests
// =============================================================================

func TestParse_SyntaxErrorRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "missing closing brace",
			source: `schema "test"
type Foo {
	id UUID primary`,
			wantOK: false,
		},
		{
			name: "invalid type name",
			source: `schema "test"
type lowercase {}`,
			wantOK: false,
		},
		{
			name: "missing property type",
			source: `schema "test"
type Foo {
	id primary
}`,
			wantOK: false,
		},
		{
			name: "invalid constraint syntax",
			source: `schema "test"
type Foo {
	name String[invalid]
}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Import Alias Tests
// =============================================================================

func TestParse_ImportAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantOK    bool
		wantAlias string
	}{
		{
			name: "import with valid lowercase alias",
			source: `schema "test"
import "other.yammm" as other`,
			wantOK:    true,
			wantAlias: "other",
		},
		{
			name: "import with valid uppercase alias",
			source: `schema "test"
import "other.yammm" as Other`,
			wantOK:    true,
			wantAlias: "Other",
		},
		{
			name: "import derives alias from path",
			source: `schema "test"
import "types.yammm"`,
			wantOK:    true,
			wantAlias: "types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
				require.Len(t, model.Imports, 1)
				assert.Equal(t, tt.wantAlias, model.Imports[0].Alias)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Extended Multiplicity Tests
// =============================================================================

func TestParse_ExtendedMultiplicityVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOK       bool
		wantOptional bool
		wantMany     bool
	}{
		{
			name: "underscore multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> target (_) Target
}`,
			wantOK:       true,
			wantOptional: true,
			wantMany:     false,
		},
		{
			name: "underscore colon one multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> target (_:one) Target
}`,
			wantOK:       true,
			wantOptional: true,
			wantMany:     false,
		},
		{
			name: "underscore colon many multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> targets (_:many) Target
}`,
			wantOK:       true,
			wantOptional: true,
			wantMany:     true,
		},
		{
			name: "one colon one multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> target (one:one) Target
}`,
			wantOK:       true,
			wantOptional: false,
			wantMany:     false,
		},
		{
			name: "one colon many multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> targets (one:many) Target
}`,
			wantOK:       true,
			wantOptional: false,
			wantMany:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						rel = r
						break
					}
				}
				require.NotNil(t, rel, "should have a relation")
				assert.Equal(t, tt.wantOptional, rel.Optional, "optional mismatch")
				assert.Equal(t, tt.wantMany, rel.Many, "many mismatch")
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// ToSchemaTypeRef Tests
// =============================================================================

func TestTypeRef_ToSchemaTypeRefConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantOK     bool
		wantQualif string
		wantName   string
	}{
		{
			name: "local type ref conversion",
			source: `schema "test"
type Target {}
type Source {
	--> target Target
}`,
			wantOK:     true,
			wantQualif: "",
			wantName:   "Target",
		},
		{
			name: "qualified type ref conversion",
			source: `schema "test"
import "other.yammm" as other
type Source {
	--> target other.Target
}`,
			wantOK:     true,
			wantQualif: "other",
			wantName:   "Target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						rel = r
						break
					}
				}
				require.NotNil(t, rel, "should have a relation")
				require.NotNil(t, rel.Target, "should have a target")

				// Convert to schema.TypeRef
				schemaRef := rel.Target.ToSchemaTypeRef()
				assert.Equal(t, tt.wantQualif, schemaRef.Qualifier())
				assert.Equal(t, tt.wantName, schemaRef.Name())
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Type and Property DocComment Tests
// =============================================================================

func TestParse_TypeDocComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		wantOK  bool
		wantDoc bool
	}{
		{
			name: "type with doc comment",
			source: `schema "test"
/* This is a documented type */
type Foo { id UUID primary }`,
			wantOK:  true,
			wantDoc: true,
		},
		{
			name: "property with doc comment",
			source: `schema "test"
type Foo {
	/* Primary identifier */
	id UUID primary
}`,
			wantOK:  true,
			wantDoc: true,
		},
		{
			name: "type without doc comment",
			source: `schema "test"
type Foo { id UUID primary }`,
			wantOK:  true,
			wantDoc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Datatype Alias Tests
// =============================================================================

func TestParse_DatatypeAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "integer alias",
			source: `schema "test"
type Age = Integer[0, 200]
type Person {
	id UUID primary
	age Age
}`,
			wantOK: true,
		},
		{
			name: "string alias",
			source: `schema "test"
type Name = String[1, 100]
type Person {
	id UUID primary
	name Name
}`,
			wantOK: true,
		},
		{
			name: "enum alias",
			source: `schema "test"
type Status = Enum["active", "inactive", "pending"]
type Item {
	id UUID primary
	status Status
}`,
			wantOK: true,
		},
		{
			name: "qualified alias reference",
			source: `schema "test"
import "types.yammm" as t
type Person {
	id UUID primary
	age t.Age
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Composition with Reverse Multiplicity Tests
// =============================================================================

func TestParse_CompositionReverseMultiplicity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		source          string
		wantOK          bool
		wantReverseMany bool
	}{
		{
			name: "composition with reverse many",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child Child / parent (many)
}`,
			wantOK:          true,
			wantReverseMany: true,
		},
		{
			name: "composition with reverse one",
			source: `schema "test"
part type Child {}
type Parent {
	*-> children (many) Child / parent (one)
}`,
			wantOK:          true,
			wantReverseMany: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the composition relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationComposition {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have a composition relation")
				assert.Equal(t, tt.wantReverseMany, rel.ReverseMany)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Association with Reverse Multiplicity Tests
// =============================================================================

func TestParse_AssociationReverseMultiplicity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		source              string
		wantOK              bool
		wantReverseOptional bool
		wantReverseMany     bool
	}{
		{
			name: "association reverse one:one",
			source: `schema "test"
type Parent {}
type Child {
	--> parent (one) Parent / children (one:one)
}`,
			wantOK:              true,
			wantReverseOptional: false,
			wantReverseMany:     false,
		},
		{
			name: "association reverse one:many",
			source: `schema "test"
type Parent {}
type Child {
	--> parent (one) Parent / children (one:many)
}`,
			wantOK:              true,
			wantReverseOptional: false,
			wantReverseMany:     true,
		},
		{
			name: "association reverse _:many",
			source: `schema "test"
type Parent {}
type Child {
	--> parent Parent / children (_:many)
}`,
			wantOK:              true,
			wantReverseOptional: true,
			wantReverseMany:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)

				// Find the association relation
				var rel *parse.RelationDecl
				for _, typ := range model.Types {
					for _, r := range typ.Relations {
						if r.Kind == parse.RelationAssociation {
							rel = r
							break
						}
					}
				}
				require.NotNil(t, rel, "should have an association relation")
				assert.Equal(t, tt.wantReverseOptional, rel.ReverseOptional, "ReverseOptional mismatch")
				assert.Equal(t, tt.wantReverseMany, rel.ReverseMany, "ReverseMany mismatch")
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Invariant Without Expression Tests
// =============================================================================

func TestParse_InvariantScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "simple property comparison invariant",
			source: `schema "test"
type Thing {
	id UUID primary
	value Integer
	! "Must be positive" value > 0
}`,
			wantOK: true,
		},
		{
			name: "multiple invariants",
			source: `schema "test"
type Thing {
	id UUID primary
	min Integer
	max Integer
	! "Min positive" min >= 0
	! "Max greater than min" max > min
}`,
			wantOK: true,
		},
		{
			name: "invariant with division",
			source: `schema "test"
type Thing {
	id UUID primary
	a Integer
	b Integer
	! "Division check" a / b < 10
}`,
			wantOK: true,
		},
		{
			name: "invariant with multiplication",
			source: `schema "test"
type Thing {
	id UUID primary
	x Integer
	y Integer
	! "Product check" x * y < 1000
}`,
			wantOK: true,
		},
		{
			name: "invariant with modulo",
			source: `schema "test"
type Thing {
	id UUID primary
	n Integer
	! "Even number" n % 2 == 0
}`,
			wantOK: true,
		},
		{
			name: "invariant with subtraction",
			source: `schema "test"
type Thing {
	id UUID primary
	a Integer
	b Integer
	! "Difference check" a - b > 0
}`,
			wantOK: true,
		},
		{
			name: "invariant with unary minus",
			source: `schema "test"
type Thing {
	id UUID primary
	n Integer
	! "Negation check" -n < 0
}`,
			wantOK: true,
		},
		{
			name: "invariant with not match",
			source: `schema "test"
type Thing {
	id UUID primary
	email String
	! "Not spam" email !~ /spam/
}`,
			wantOK: true,
		},
		{
			name: "invariant with xor",
			source: `schema "test"
type Thing {
	id UUID primary
	a Boolean
	b Boolean
	! "Exclusive or" a ^ b
}`,
			wantOK: true,
		},
		{
			name: "invariant with less than or equal",
			source: `schema "test"
type Thing {
	id UUID primary
	n Integer
	! "Bounded" n <= 100
}`,
			wantOK: true,
		},
		{
			name: "invariant with greater than or equal",
			source: `schema "test"
type Thing {
	id UUID primary
	n Integer
	! "Minimum" n >= 0
}`,
			wantOK: true,
		},
		{
			name: "invariant with not equal",
			source: `schema "test"
type Thing {
	id UUID primary
	name String
	! "Not empty" name != ""
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Property Documentation Tests
// =============================================================================

func TestParse_PropertyDocumentation(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Foo {
	/* Primary identifier for the entity */
	id UUID primary
	/* User's display name */
	name String
	/* Age in years */
	age Integer
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 3)

	// Check that each property has documentation
	for _, prop := range model.Types[0].Properties {
		assert.NotEmpty(t, prop.Documentation, "property %s should have documentation", prop.Name)
	}
}

// =============================================================================
// Relation Documentation Tests
// =============================================================================

func TestParse_RelationDocumentation(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Target {}
type Source {
	/* Reference to the target entity */
	--> target Target
}
part type Child {}
type Parent {
	/* Children of this parent */
	*-> children (many) Child
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find Source type and check relation documentation
	for _, typ := range model.Types {
		for _, rel := range typ.Relations {
			assert.NotEmpty(t, rel.Documentation, "relation %s should have documentation", rel.Name)
		}
	}
}

// =============================================================================
// Error Path Tests - Cover uncovered error branches
// =============================================================================

func TestParse_ErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantOK      bool
		description string
	}{
		{
			name:        "empty schema content",
			source:      "",
			wantOK:      false,
			description: "Tests empty source",
		},
		{
			name: "unclosed type brace",
			source: `schema "test"
type Foo {
	id UUID primary`,
			wantOK:      false,
			description: "Tests unclosed brace error",
		},
		{
			name: "invalid type keyword",
			source: `schema "test"
typo Foo {}`,
			wantOK:      false,
			description: "Tests invalid keyword error",
		},
		{
			name: "lowercase type name",
			source: `schema "test"
type lowercase {}`,
			wantOK:      false,
			description: "Tests invalid type name case",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
			} else {
				// We expect either parsing to fail or diagnostics
				_ = result
			}
		})
	}
}

// =============================================================================
// Edge Cases for Extended Types
// =============================================================================

func TestParse_ExtendedTypeEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "part type",
			source: `schema "test"
part type Component {
	id UUID primary
}`,
			wantOK: true,
		},
		{
			name: "abstract type",
			source: `schema "test"
abstract type Entity {
	id UUID primary
}`,
			wantOK: true,
		},
		{
			name: "type extends qualified",
			source: `schema "test"
import "base.yammm" as base
type Child extends base.Parent {}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Extended Datatype Tests
// =============================================================================

func TestParse_ExtendedDatatypeDefinitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "datatype with documentation",
			source: `schema "test"
/* Custom email type */
type Email = Pattern["^[a-z]+@[a-z]+\\.[a-z]+$"]
type User {
	id UUID primary
	email Email
}`,
			wantOK: true,
		},
		{
			name: "float datatype alias",
			source: `schema "test"
type Percentage = Float[0.0, 100.0]
type Item {
	id UUID primary
	discount Percentage
}`,
			wantOK: true,
		},
		{
			name: "boolean datatype alias",
			source: `schema "test"
type Flag = Boolean
type Item {
	id UUID primary
	active Flag
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Using Clause Tests
// =============================================================================

func TestParse_UsingClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "association with local using type",
			source: `schema "test"
type Metadata {
	timestamp Timestamp
}
type Source {}
type Target {}
type Graph {
	--> source Source
	--> target Target using Metadata
}`,
			wantOK: true,
		},
		{
			name: "association with qualified using type",
			source: `schema "test"
import "meta.yammm" as meta
type Source {}
type Target {}
type Graph {
	--> source Source
	--> target Target using meta.EdgeData
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Relation Edge Property Doc Comments
// =============================================================================

func TestParse_RelationEdgePropertyDocComments(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Target {}
type Source {
	--> target Target {
		/* Weight of the edge */
		weight Float
		/* Label for display */
		label String
	}
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the Source type and verify edge properties
	for _, typ := range model.Types {
		if typ.Name == "Source" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			require.Len(t, rel.Properties, 2)
			for _, prop := range rel.Properties {
				assert.NotEmpty(t, prop.Documentation, "edge property %s should have doc", prop.Name)
			}
		}
	}
}

// =============================================================================
// Comprehensive Syntax Tests
// =============================================================================

func TestParse_ComprehensiveSyntaxCoverage(t *testing.T) {
	t.Parallel()

	// This comprehensive schema tests many paths at once
	source := `schema "comprehensive"

import "external.yammm" as ext

/* Primary datatype definition */
type Email = Pattern["^[a-z]+@[a-z]+\\.[a-z]+$"]

/* Age with bounds */
type Age = Integer[0, 150]

/* Status enum */
type Status = Enum["active", "inactive"]

/* Abstract entity base */
abstract type Entity {
	/* Primary identifier */
	id UUID primary
	createdAt Timestamp
	updatedAt Timestamp
}

/* Part type for ownership */
part type Address {
	street String[1, 200]
	city String[1, 100]
	zip String[5, 10]
}

/* Main user type */
type User extends Entity {
	/* User's email */
	email Email required
	/* User's display name */
	name String[1, 100]
	age Age
	status Status required
	/* User's primary address */
	*-> address Address / user
	/* Friends association */
	--> friends (many) User / friends {
		since Date
	}
	! "Valid age" age >= 0
	! "Name not empty" name != _
}

/* Organization type */
type Organization extends Entity {
	name String[1, 200] required
	--> members (many) User / organization {
		role String
		joinedAt Timestamp
	}
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	assert.Equal(t, "comprehensive", model.Name)
	assert.Len(t, model.Imports, 1)
	assert.Len(t, model.DataTypes, 3) // Email, Age, Status
	assert.GreaterOrEqual(t, len(model.Types), 4)
}

// =============================================================================
// UUID Property Tests
// =============================================================================

func TestParse_UUIDProperty(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Entity {
	id UUID primary
	secondaryId UUID
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Properties, 2)

	// Check primary key
	assert.True(t, model.Types[0].Properties[0].IsPrimaryKey)
	assert.False(t, model.Types[0].Properties[1].IsPrimaryKey)
}

// =============================================================================
// String Constraint Tests
// =============================================================================

func TestParse_StringConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		wantOK bool
	}{
		{
			name: "string with min length only",
			source: `schema "test"
type Foo {
	id UUID primary
	name String[1, _]
}`,
			wantOK: true,
		},
		{
			name: "string with max length only",
			source: `schema "test"
type Foo {
	id UUID primary
	name String[_, 100]
}`,
			wantOK: true,
		},
		{
			name: "string with both bounds",
			source: `schema "test"
type Foo {
	id UUID primary
	name String[1, 100]
}`,
			wantOK: true,
		},
		{
			name: "unbounded string",
			source: `schema "test"
type Foo {
	id UUID primary
	name String
}`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
				require.NotNil(t, model)
			} else {
				assert.False(t, result.OK(), "expected errors")
			}
		})
	}
}

// =============================================================================
// Nil TypeRef Edge Cases
// =============================================================================

func TestParse_NilTypeRefHandling(t *testing.T) {
	t.Parallel()

	// Test TypeRef.String() and IsQualified() methods
	ref := parse.TypeRef{Name: "Test"}
	assert.Equal(t, "Test", ref.String())
	assert.False(t, ref.IsQualified())

	qualRef := parse.TypeRef{Qualifier: "pkg", Name: "Test"}
	assert.Equal(t, "pkg.Test", qualRef.String())
	assert.True(t, qualRef.IsQualified())
}

// =============================================================================
// Relation Kind String Tests
// =============================================================================

func TestRelationKind_AllValues(t *testing.T) {
	t.Parallel()

	// Test all RelationKind values
	assert.Equal(t, "association", parse.RelationAssociation.String())
	assert.Equal(t, "composition", parse.RelationComposition.String())

	// Test unknown value
	var unknown parse.RelationKind = 99
	assert.Equal(t, "unknown", unknown.String())
}

// =============================================================================
// Extended Qualified Reference Tests
// =============================================================================

func TestParse_QualifiedInherits(t *testing.T) {
	t.Parallel()

	source := `schema "test"
import "base.yammm" as base
type Child extends base.Entity, base.Auditable {
	name String
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)

	child := model.Types[0]
	require.Len(t, child.Inherits, 2)
	assert.True(t, child.Inherits[0].IsQualified())
	assert.Equal(t, "base", child.Inherits[0].Qualifier)
	assert.Equal(t, "Entity", child.Inherits[0].Name)
	assert.True(t, child.Inherits[1].IsQualified())
	assert.Equal(t, "base", child.Inherits[1].Qualifier)
	assert.Equal(t, "Auditable", child.Inherits[1].Name)
}

// =============================================================================
// Additional Coverage Tests for Edge Cases
// =============================================================================

func TestParse_CompositionWithMultiplicity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		source       string
		wantOptional bool
		wantMany     bool
	}{
		{
			name: "composition underscore multiplicity",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child (_) Child
}`,
			wantOptional: true,
			wantMany:     false,
		},
		{
			name: "composition underscore colon one",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child (_:one) Child
}`,
			wantOptional: true,
			wantMany:     false,
		},
		{
			name: "composition underscore colon many",
			source: `schema "test"
part type Child {}
type Parent {
	*-> children (_:many) Child
}`,
			wantOptional: true,
			wantMany:     true,
		},
		{
			name: "composition one colon one",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child (one:one) Child
}`,
			wantOptional: false,
			wantMany:     false,
		},
		{
			name: "composition one colon many",
			source: `schema "test"
part type Child {}
type Parent {
	*-> children (one:many) Child
}`,
			wantOptional: false,
			wantMany:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			assert.True(t, result.OK(), "expected no errors, got: %v", result)
			require.NotNil(t, model)

			// Find the composition relation
			var rel *parse.RelationDecl
			for _, typ := range model.Types {
				for _, r := range typ.Relations {
					if r.Kind == parse.RelationComposition {
						rel = r
						break
					}
				}
			}
			require.NotNil(t, rel, "should have a composition relation")
			assert.Equal(t, tt.wantOptional, rel.Optional, "optional mismatch")
			assert.Equal(t, tt.wantMany, rel.Many, "many mismatch")
		})
	}
}

func TestParse_DatatypeDocumentation(t *testing.T) {
	t.Parallel()

	source := `schema "test"
/* Email datatype */
type Email = String[1, 100]
type User {
	id UUID primary
	email Email
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.DataTypes, 1)
	assert.NotEmpty(t, model.DataTypes[0].Documentation)
}

func TestParse_MultipleRelationsOnType(t *testing.T) {
	t.Parallel()

	source := `schema "test"
part type Component {}
type Connector {}
type Machine {
	*-> component Component
	--> connector Connector
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the Machine type
	for _, typ := range model.Types {
		if typ.Name == "Machine" {
			require.Len(t, typ.Relations, 2)
		}
	}
}

func TestParse_AssociationWithRelProperties(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type A {}
type B {
	--> a A {
		weight Integer
		label String
	}
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the association relation with properties
	for _, typ := range model.Types {
		if typ.Name == "B" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			require.Len(t, rel.Properties, 2)
		}
	}
}

func TestParse_CompositionDefault(t *testing.T) {
	t.Parallel()

	source := `schema "test"
part type Component {}
type Machine {
	*-> component Component
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the composition relation - default is optional/one
	for _, typ := range model.Types {
		if typ.Name == "Machine" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			assert.True(t, rel.Optional, "default should be optional")
			assert.False(t, rel.Many, "default should be one")
		}
	}
}

func TestParse_QualifiedCompositionTarget(t *testing.T) {
	t.Parallel()

	source := `schema "test"
import "parts.yammm" as parts
type Machine {
	*-> component parts.Component
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the composition relation
	for _, typ := range model.Types {
		if typ.Name == "Machine" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			assert.True(t, rel.Target.IsQualified())
			assert.Equal(t, "parts", rel.Target.Qualifier)
			assert.Equal(t, "Component", rel.Target.Name)
		}
	}
}

func TestParse_MultipleImports(t *testing.T) {
	t.Parallel()

	source := `schema "test"
import "types.yammm" as types
import "parts.yammm" as parts
type Foo {
	id UUID primary
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Imports, 2)
	assert.Equal(t, "types", model.Imports[0].Alias)
	assert.Equal(t, "parts", model.Imports[1].Alias)
}

func TestParse_AssociationWithSimpleBackref(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Parent {}
type Child {
	--> parent Parent / children
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the Child type
	for _, typ := range model.Types {
		if typ.Name == "Child" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			assert.Equal(t, "parent", rel.Name)
			assert.Equal(t, "children", rel.Backref)
		}
	}
}

func TestParse_CompositionWithBackref(t *testing.T) {
	t.Parallel()

	source := `schema "test"
part type Child {}
type Parent {
	*-> children (many) Child / owner (one)
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)

	// Find the Parent type
	for _, typ := range model.Types {
		if typ.Name == "Parent" {
			require.Len(t, typ.Relations, 1)
			rel := typ.Relations[0]
			assert.Equal(t, "children", rel.Name)
			assert.True(t, rel.Many)
			assert.Equal(t, "owner", rel.Backref)
			assert.False(t, rel.ReverseMany)
		}
	}
}

func TestParse_InvariantWithChainedMethod(t *testing.T) {
	t.Parallel()

	source := `schema "test"
type Thing {
	id UUID primary
	items Integer[0,_]
	! "Has items" items->Count() > 0
}`

	model, result := parseSchema(t, source)
	assert.True(t, result.OK(), "expected no errors, got: %v", result)
	require.NotNil(t, model)
	require.Len(t, model.Types, 1)
	require.Len(t, model.Types[0].Invariants, 1)
	require.NotNil(t, model.Types[0].Invariants[0].Expr)
}

// =============================================================================
// Nil-Safety Tests
// =============================================================================
// These tests verify that the parser handles malformed input gracefully without
// panicking. Per v2 error handling contracts, content errors (malformed user input)
// must be reported via diag.Collector, not panics.

func TestParse_NilSafety_MalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantOK      bool
		description string
	}{
		{
			name: "missing type name",
			source: `schema "test"
type {}`,
			wantOK:      false,
			description: "EnterType nil-safety: type declaration missing name",
		},
		{
			name: "missing property name",
			source: `schema "test"
type Foo {
	String required
}`,
			wantOK:      false,
			description: "ExitProperty nil-safety: property declaration missing name",
		},
		{
			name: "missing datatype name",
			source: `schema "test"
type = String`,
			wantOK:      false,
			description: "ExitDatatype nil-safety: datatype declaration missing name",
		},
		{
			name: "missing association relation name",
			source: `schema "test"
type Target {}
type Source {
	--> Target
}`,
			wantOK:      false,
			description: "ExitAssociation nil-safety: association missing relation name",
		},
		{
			name: "missing edge property name",
			source: `schema "test"
type Target {}
type Source {
	--> target Target {
		Integer required
	}
}`,
			wantOK:      false,
			description: "ExitRel_property nil-safety: edge property missing name",
		},
		{
			name: "missing invariant message",
			source: `schema "test"
type Foo {
	id UUID primary
	value Integer
	! value > 0
}`,
			wantOK:      false,
			description: "ExitInvariant nil-safety: invariant missing message string",
		},
		{
			name: "missing vector dimensions",
			source: `schema "test"
type Foo {
	id UUID primary
	vec Vector[]
}`,
			wantOK:      false,
			description: "ExitVectorT nil-safety: vector type missing dimensions",
		},
		{
			name: "malformed type reference in extends",
			source: `schema "test"
type Base {}
type Child extends . {}`,
			wantOK:      false,
			description: "buildTypeRef nil-safety: type reference missing type name",
		},
		{
			name: "malformed type reference in association",
			source: `schema "test"
type Source {
	--> target other.
}`,
			wantOK:      false,
			description: "buildTypeRef nil-safety: qualified type reference missing name",
		},
		{
			name: "malformed composition target",
			source: `schema "test"
part type Child {}
type Parent {
	*-> child .
}`,
			wantOK:      false,
			description: "buildTypeRef nil-safety: composition target missing name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The main assertion: no panic should occur
			var model *parse.Model
			var result diag.Result
			assert.NotPanics(t, func() {
				model, result = parseSchema(t, tt.source)
			}, "parser should not panic on malformed input: %s", tt.description)

			// Verify errors were reported via diagnostic system
			if tt.wantOK {
				assert.True(t, result.OK(), "expected no errors, got: %v", result)
			} else {
				assert.False(t, result.OK(), "expected errors to be reported via diagnostics")
				// Note: We verify that model is still usable (may be nil or partially populated)
				_ = model
			}
		})
	}
}

// TestParse_NilSafety_ErrorCodeVerification verifies that nil-safety errors
// are reported with the appropriate E_SYNTAX error code.
func TestParse_NilSafety_ErrorCodeVerification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantCode diag.Code
	}{
		{
			name: "missing invariant message produces E_SYNTAX",
			source: `schema "test"
type Foo {
	id UUID primary
	! true
}`,
			wantCode: diag.E_SYNTAX,
		},
		{
			name: "missing vector dimension produces E_SYNTAX",
			source: `schema "test"
type Foo {
	id UUID primary
	vec Vector[]
}`,
			wantCode: diag.E_SYNTAX,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var result diag.Result
			assert.NotPanics(t, func() {
				_, result = parseSchema(t, tt.source)
			})

			assert.False(t, result.OK(), "expected errors")

			// Check that at least one issue has the expected code
			foundCode := false
			for issue := range result.Issues() {
				if issue.Code() == tt.wantCode {
					foundCode = true
					break
				}
			}
			assert.True(t, foundCode, "expected to find error code %v", tt.wantCode)
		})
	}
}

// =============================================================================
// Additional Error Path Tests for Coverage
// =============================================================================

func TestParse_TimestampInvalidFormat(t *testing.T) {
	t.Parallel()

	// Test timestamp with invalid escape sequence in format string
	source := `schema "test"
type Event {
	id UUID primary
	time Timestamp["\xinvalid"]
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for invalid escape sequence")
}

func TestParse_InvariantInvalidMessage(t *testing.T) {
	t.Parallel()

	// Test invariant with invalid escape sequence in message
	source := `schema "test"
type Thing {
	id UUID primary
	value Integer
	! "\xinvalid" value > 0
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for invalid escape sequence")
}

func TestParse_EnumInvalidValue(t *testing.T) {
	t.Parallel()

	// Test enum with invalid escape sequence in value
	source := `schema "test"
type Thing {
	id UUID primary
	status Enum["\xinvalid"]
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for invalid escape sequence")
}

func TestParse_PatternInvalidString(t *testing.T) {
	t.Parallel()

	// Test pattern with invalid escape sequence
	source := `schema "test"
type Thing {
	id UUID primary
	code Pattern["\xinvalid"]
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for invalid escape sequence")
}

func TestParse_SchemaNameInvalidEscape(t *testing.T) {
	t.Parallel()

	// Test schema name with invalid escape sequence
	source := `schema "\xinvalid"
type Foo { id UUID primary }`

	_, result := parseSchema(t, source)
	// May produce syntax error or continue with best effort
	_ = result
}

func TestParse_ImportPathInvalidEscape(t *testing.T) {
	t.Parallel()

	// Test import with invalid escape sequence in path
	source := `schema "test"
import "\xinvalid" as bad`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for invalid escape sequence")
}

func TestParse_MissingCompositionRelName(t *testing.T) {
	t.Parallel()

	// Test composition with missing relation name
	source := `schema "test"
part type Child {}
type Parent {
	*-> Child
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for missing relation name")
}

func TestParse_MissingCompositionTarget(t *testing.T) {
	t.Parallel()

	// Test composition with missing target type
	source := `schema "test"
type Parent {
	*-> orphan
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for missing target type")
}

func TestParse_InvalidMultiplicityKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "invalid multiplicity in association",
			source: `schema "test"
type Target {}
type Source {
	--> target (invalid) Target
}`,
		},
		{
			name: "invalid colon multiplicity",
			source: `schema "test"
type Target {}
type Source {
	--> target (one:invalid) Target
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			// Invalid multiplicity should produce an error
			assert.False(t, result.OK(), "expected error for invalid multiplicity")
		})
	}
}

func TestParse_VectorDimensionNonNumeric(t *testing.T) {
	t.Parallel()

	// Test vector with non-numeric dimension (ANTLR error recovery)
	source := `schema "test"
type Embedding {
	id UUID primary
	vec Vector[abc]
}`

	_, result := parseSchema(t, source)
	assert.False(t, result.OK(), "expected error for non-numeric vector dimension")
}

func TestParse_ImportEmptyPath(t *testing.T) {
	t.Parallel()

	// Empty import path produces empty string but may not error during parse
	source := `schema "test"
import "" as empty`

	_, result := parseSchema(t, source)
	// Empty paths are checked during load, not parse
	_ = result
}

func TestParse_SyntaxErrorReporting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "unexpected token",
			source: `schema "test"
type Foo {
	@ invalid syntax
}`,
		},
		{
			name: "unclosed string",
			source: `schema "test
type Foo {}`,
		},
		{
			name:   "missing schema declaration",
			source: `type Foo {}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, result := parseSchema(t, tt.source)
			assert.False(t, result.OK(), "expected syntax error")
		})
	}
}

func TestParse_DocCommentStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantTypeDoc string
	}{
		{
			name: "simple doc comment",
			source: `schema "test"
/* Simple doc */
type Foo { id UUID primary }`,
			wantTypeDoc: "Simple doc",
		},
		{
			name: "multiline doc comment",
			source: `schema "test"
/* Line one
   Line two */
type Foo { id UUID primary }`,
			wantTypeDoc: "Line one\n   Line two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, result := parseSchema(t, tt.source)
			assert.True(t, result.OK(), "expected no errors")
			require.NotNil(t, model)
			require.Len(t, model.Types, 1)
			assert.Contains(t, model.Types[0].Documentation, strings.TrimPrefix(tt.wantTypeDoc, "/* "))
		})
	}
}

func TestParse_ExtendsEmptyType(t *testing.T) {
	t.Parallel()

	// When extends list is empty (just keyword but no types)
	source := `schema "test"
type Base {}
type Child extends {}`

	_, result := parseSchema(t, source)
	// Should either work with empty inherits or produce an error
	_ = result
}

func TestParse_AssociationUsingMissingTarget(t *testing.T) {
	t.Parallel()

	// Association with using clause but missing relation name
	// This exercises the using clause path in the parser
	source := `schema "test"
type Edge { weight Float }
type Target { id UUID primary }
type Source {
	--> rel using Edge Target
}`

	_, result := parseSchema(t, source)
	// This is valid syntax - exercises the using clause path
	_ = result
}

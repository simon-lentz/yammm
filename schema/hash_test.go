package schema_test

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

var zeroSpan = location.Span{}

func testSourceID(name string) location.SourceID {
	return location.MustNewSourceID("test://" + name)
}

// buildMinimalSchema creates a sealed schema with optional types and data types.
func buildMinimalSchema(
	name string,
	types []*schema.Type,
	dataTypes []*schema.DataType,
) *schema.Schema {
	sid := testSourceID(name)
	s := schema.TestNewSchema(name, sid, zeroSpan, "")
	if types != nil {
		schema.TestSetSchemaTypes(s, types)
	}
	if dataTypes != nil {
		schema.TestSetSchemaDataTypes(s, dataTypes)
	}
	for _, typ := range types {
		schema.TestSealType(typ)
	}
	for _, dt := range dataTypes {
		schema.TestSealDataType(dt)
	}
	schema.TestSealSchema(s)
	return s
}

// makeProp creates a property with the given name, constraint, and optional/pk flags.
func makeProp(name string, c schema.Constraint, optional, pk bool) *schema.Property {
	return schema.TestNewProperty(name, zeroSpan, "", c, schema.DataTypeRef{}, optional, pk, schema.DeclaringScope{}, nil)
}

// makeAssoc creates an association relation.
func makeAssoc(name, targetName string, optional, many bool, edgeProps []*schema.Property) *schema.Relation { //nolint:unparam // test helper - name varies across test files
	targetID := schema.NewTypeID(location.SourceID{}, targetName)
	return schema.TestNewRelation(
		schema.RelationAssociation,
		name,
		strings.ToLower(name),
		schema.LocalTypeRef(targetName, zeroSpan),
		targetID,
		zeroSpan,
		"",
		optional,
		many,
		"",
		edgeProps,
	)
}

// makeComp creates a composition relation.
func makeComp(name, targetName string, optional, many bool) *schema.Relation {
	targetID := schema.NewTypeID(location.SourceID{}, targetName)
	return schema.TestNewRelation(
		schema.RelationComposition,
		name,
		strings.ToLower(name),
		schema.LocalTypeRef(targetName, zeroSpan),
		targetID,
		zeroSpan,
		"",
		optional,
		many,
		"",
		nil,
	)
}

// buildTypeWithProps creates a type with AllProperties and PrimaryKeys set.
func buildTypeWithProps(name string, props []*schema.Property) *schema.Type {
	typ := schema.TestNewType(name, location.SourceID{}, zeroSpan, "", false, false)
	schema.TestSetTypeProperties(typ, props)
	schema.TestSetTypeAllProperties(typ, props)

	var pks []*schema.Property
	for _, p := range props {
		if p.IsPrimaryKey() {
			pks = append(pks, p)
		}
	}
	schema.TestSetTypePrimaryKeys(typ, pks)

	return typ
}

func TestStructuralHashVersion(t *testing.T) {
	assert.Equal(t, 4, schema.StructuralHashVersion)
}

func TestStructuralHash_NilPanics(t *testing.T) {
	assert.PanicsWithValue(
		t,
		"schema: StructuralHash called on nil *Schema",
		func() { schema.StructuralHash(nil) },
	)
}

func TestStructuralHash_Determinism(t *testing.T) {
	prop := makeProp("id", schema.NewUUIDConstraint(), false, true)
	typ := buildTypeWithProps("User", []*schema.Property{prop})
	s := buildMinimalSchema("app", []*schema.Type{typ}, nil)

	h1 := schema.StructuralHash(s)
	h2 := schema.StructuralHash(s)

	assert.Equal(t, h1, h2, "same schema must produce identical hashes")
	assert.True(t, strings.HasPrefix(h1, "sha256:"), "hash must have sha256: prefix")
	// SHA-256 hex is 64 chars.
	assert.Len(t, strings.TrimPrefix(h1, "sha256:"), 64)
}

func TestStructuralHash_EmptySchema(t *testing.T) {
	s := buildMinimalSchema("empty", nil, nil)

	h := schema.StructuralHash(s)
	assert.True(t, strings.HasPrefix(h, "sha256:"))
}

func TestStructuralHash_NameSensitivity(t *testing.T) {
	s1 := buildMinimalSchema("alpha", nil, nil)
	s2 := buildMinimalSchema("beta", nil, nil)

	assert.NotEqual(t, schema.StructuralHash(s1), schema.StructuralHash(s2),
		"schemas with different names must have different hashes")
}

func TestStructuralHash_PropertyConstraintSensitivity(t *testing.T) {
	propA := makeProp("name", schema.NewStringConstraint(), false, false)
	typA := buildTypeWithProps("Person", []*schema.Property{propA})
	sA := buildMinimalSchema("app", []*schema.Type{typA}, nil)

	propB := makeProp("name", schema.StringMinLen(1), false, false)
	typB := buildTypeWithProps("Person", []*schema.Property{propB})
	sB := buildMinimalSchema("app", []*schema.Type{typB}, nil)

	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB),
		"different property constraints must produce different hashes")
}

func TestStructuralHash_RequiredVsOptional(t *testing.T) {
	propReq := makeProp("email", schema.NewStringConstraint(), false, false) // required
	typReq := buildTypeWithProps("User", []*schema.Property{propReq})
	sReq := buildMinimalSchema("app", []*schema.Type{typReq}, nil)

	propOpt := makeProp("email", schema.NewStringConstraint(), true, false) // optional
	typOpt := buildTypeWithProps("User", []*schema.Property{propOpt})
	sOpt := buildMinimalSchema("app", []*schema.Type{typOpt}, nil)

	assert.NotEqual(t, schema.StructuralHash(sReq), schema.StructuralHash(sOpt),
		"required vs optional must produce different hashes")
}

func TestStructuralHash_AssociationTargetSensitivity(t *testing.T) {
	assocA := makeAssoc("WORKS_AT", "Company", false, false, nil)
	typA := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typA, []*schema.Relation{assocA})
	schema.TestSetTypeAllAssociations(typA, []*schema.Relation{assocA})
	sA := buildMinimalSchema("app", []*schema.Type{typA}, nil)

	assocB := makeAssoc("WORKS_AT", "Organization", false, false, nil)
	typB := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typB, []*schema.Relation{assocB})
	schema.TestSetTypeAllAssociations(typB, []*schema.Relation{assocB})
	sB := buildMinimalSchema("app", []*schema.Type{typB}, nil)

	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB),
		"different association targets must produce different hashes")
}

func TestStructuralHash_AssociationMultiplicitySensitivity(t *testing.T) {
	assocOne := makeAssoc("WORKS_AT", "Company", false, false, nil)
	typOne := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typOne, []*schema.Relation{assocOne})
	schema.TestSetTypeAllAssociations(typOne, []*schema.Relation{assocOne})
	sOne := buildMinimalSchema("app", []*schema.Type{typOne}, nil)

	assocMany := makeAssoc("WORKS_AT", "Company", false, true, nil)
	typMany := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typMany, []*schema.Relation{assocMany})
	schema.TestSetTypeAllAssociations(typMany, []*schema.Relation{assocMany})
	sMany := buildMinimalSchema("app", []*schema.Type{typMany}, nil)

	assert.NotEqual(t, schema.StructuralHash(sOne), schema.StructuralHash(sMany),
		"different association multiplicities must produce different hashes")
}

func TestStructuralHash_AssociationEdgeProperties(t *testing.T) {
	edgeProp := makeProp("since", schema.NewDateConstraint(), false, false)
	assocWith := makeAssoc("WORKS_AT", "Company", false, false, []*schema.Property{edgeProp})
	typWith := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typWith, []*schema.Relation{assocWith})
	schema.TestSetTypeAllAssociations(typWith, []*schema.Relation{assocWith})
	sWith := buildMinimalSchema("app", []*schema.Type{typWith}, nil)

	assocWithout := makeAssoc("WORKS_AT", "Company", false, false, nil)
	typWithout := buildTypeWithProps("Person", nil)
	schema.TestSetTypeAssociations(typWithout, []*schema.Relation{assocWithout})
	schema.TestSetTypeAllAssociations(typWithout, []*schema.Relation{assocWithout})
	sWithout := buildMinimalSchema("app", []*schema.Type{typWithout}, nil)

	assert.NotEqual(t, schema.StructuralHash(sWith), schema.StructuralHash(sWithout),
		"association with vs without edge properties must differ")
}

func TestStructuralHash_CompositionSensitivity(t *testing.T) {
	compA := makeComp("CONTAINS", "Wheel", false, true)
	typA := buildTypeWithProps("Car", nil)
	schema.TestSetTypeCompositions(typA, []*schema.Relation{compA})
	schema.TestSetTypeAllCompositions(typA, []*schema.Relation{compA})
	sA := buildMinimalSchema("app", []*schema.Type{typA}, nil)

	compB := makeComp("CONTAINS", "Engine", false, true)
	typB := buildTypeWithProps("Car", nil)
	schema.TestSetTypeCompositions(typB, []*schema.Relation{compB})
	schema.TestSetTypeAllCompositions(typB, []*schema.Relation{compB})
	sB := buildMinimalSchema("app", []*schema.Type{typB}, nil)

	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB),
		"different composition targets must produce different hashes")
}

// TestStructuralHash_ConstraintSensitivity drives every constraint-level
// hash distinction through one table: each row's two constraints land on
// the same property/type/schema scaffold and the hashes must differ
// (wantSame=false) or match (wantSame=true — sorted-set semantics).
func TestStructuralHash_ConstraintSensitivity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     schema.Constraint
		wantSame bool
	}{
		{"string unbounded vs min", schema.NewStringConstraint(), schema.StringMinLen(5), false},
		{"string unbounded vs max", schema.NewStringConstraint(), schema.StringMaxLen(100), false},
		{"string different min", schema.StringMinLen(1), schema.StringMinLen(2), false},
		{"string both bounds vs one", schema.StringLenBetween(1, 100), schema.StringMinLen(1), false},
		{"integer unbounded vs min", schema.NewIntegerConstraint(), schema.IntegerMin(0), false},
		{"integer different max", schema.IntegerMax(100), schema.IntegerMax(200), false},
		{"integer between vs single bound", schema.IntegerBetween(0, 100), schema.IntegerMin(0), false},
		{"float unbounded vs min", schema.NewFloatConstraint(), schema.FloatMin(0.0), false},
		{"float different min", schema.FloatMin(1.5), schema.FloatMin(2.5), false},
		{"float different max", schema.FloatMax(100.0), schema.FloatMax(200.0), false},
		{"enum different values", schema.NewEnumConstraint([]string{"ACTIVE", "INACTIVE"}), schema.NewEnumConstraint([]string{"ACTIVE", "DELETED"}), false},
		// Enum values are sorted before hashing, so order must not matter.
		{"enum order insensitive", schema.NewEnumConstraint([]string{"B", "A"}), schema.NewEnumConstraint([]string{"A", "B"}), true},
		{"enum different count", schema.NewEnumConstraint([]string{"A"}), schema.NewEnumConstraint([]string{"A", "B"}), false},
		{"pattern different patterns", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[A-Z]+$`)}), schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[0-9]+$`)}), false},
		// Pattern sets are sorted before hashing, so order must not matter.
		{
			"pattern order insensitive",
			schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[A-Z]`), regexp.MustCompile(`[0-9]$`)}),
			schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`[0-9]$`), regexp.MustCompile(`^[A-Z]`)}),
			true,
		},
		{"vector dimension", schema.NewVectorConstraint(128), schema.NewVectorConstraint(256), false},
		{"boolean vs string kind", schema.NewBooleanConstraint(), schema.NewStringConstraint(), false},
		{"timestamp default vs formatted", schema.NewTimestampConstraint(), schema.NewTimestampConstraintFormatted("2006-01-02T15:04:05Z07:00"), false},
		{"date vs uuid kind", schema.NewDateConstraint(), schema.NewUUIDConstraint(), false},
		{"list different element types", schema.NewListConstraint(schema.NewStringConstraint()), schema.NewListConstraint(schema.NewIntegerConstraint()), false},
		{"list unbounded vs bounded", schema.NewListConstraint(schema.NewStringConstraint()), schema.ListMinLen(schema.NewStringConstraint(), 1), false},
		{"list different max", schema.ListLenBetween(schema.NewStringConstraint(), 1, 10), schema.ListLenBetween(schema.NewStringConstraint(), 1, 20), false},
		{"list same bounds match", schema.ListLenBetween(schema.NewStringConstraint(), 1, 10), schema.ListLenBetween(schema.NewStringConstraint(), 1, 10), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			propA := makeProp("x", tt.a, false, false)
			propB := makeProp("x", tt.b, false, false)
			if tt.wantSame {
				assertConstraintsMatch(t, propA, propB)
			} else {
				assertConstraintsDiffer(t, propA, propB)
			}
		})
	}
}

func TestStructuralHash_DataTypeSensitivity(t *testing.T) {
	dtA := schema.TestNewDataType("Email", schema.StringMinLen(1), zeroSpan, "")
	sA := buildMinimalSchema("app", nil, []*schema.DataType{dtA})

	dtB := schema.TestNewDataType("Email", schema.StringMinLen(5), zeroSpan, "")
	sB := buildMinimalSchema("app", nil, []*schema.DataType{dtB})

	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB),
		"different data type constraints must produce different hashes")
}

func TestStructuralHash_DataTypeName(t *testing.T) {
	dtA := schema.TestNewDataType("Email", schema.NewStringConstraint(), zeroSpan, "")
	sA := buildMinimalSchema("app", nil, []*schema.DataType{dtA})

	dtB := schema.TestNewDataType("Phone", schema.NewStringConstraint(), zeroSpan, "")
	sB := buildMinimalSchema("app", nil, []*schema.DataType{dtB})

	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB),
		"different data type names must produce different hashes")
}

func TestStructuralHash_SuperTypeSensitivity(t *testing.T) {
	// Type with a super type reference.
	typ1 := buildTypeWithProps("Admin", nil)
	superRef := schema.NewResolvedTypeRef(
		schema.LocalTypeRef("User", zeroSpan),
		schema.NewTypeID(location.SourceID{}, "User"),
	)
	schema.TestSetTypeSuperTypes(typ1, []schema.ResolvedTypeRef{superRef})
	s1 := buildMinimalSchema("app", []*schema.Type{typ1}, nil)

	// Same type without super type.
	typ2 := buildTypeWithProps("Admin", nil)
	s2 := buildMinimalSchema("app", []*schema.Type{typ2}, nil)

	assert.NotEqual(t, schema.StructuralHash(s1), schema.StructuralHash(s2),
		"type with vs without supertypes must produce different hashes")
}

func TestStructuralHash_PrimaryKeySensitivity(t *testing.T) {
	propPK := makeProp("id", schema.NewUUIDConstraint(), false, true)
	typPK := buildTypeWithProps("User", []*schema.Property{propPK})
	sPK := buildMinimalSchema("app", []*schema.Type{typPK}, nil)

	propNonPK := makeProp("id", schema.NewUUIDConstraint(), false, false)
	typNonPK := buildTypeWithProps("User", []*schema.Property{propNonPK})
	sNonPK := buildMinimalSchema("app", []*schema.Type{typNonPK}, nil)

	assert.NotEqual(t, schema.StructuralHash(sPK), schema.StructuralHash(sNonPK),
		"PK vs non-PK property must produce different hashes")
}

func TestStructuralHash_AliasResolution(t *testing.T) {
	// Schema with a direct StringMinLen(1) constraint on a property.
	propDirect := makeProp("email", schema.StringMinLen(1), false, false)
	typDirect := buildTypeWithProps("User", []*schema.Property{propDirect})
	sDirect := buildMinimalSchema("app", []*schema.Type{typDirect}, nil)

	// Schema with an alias that resolves to StringMinLen(1).
	alias := schema.NewAliasConstraint("Email", schema.StringMinLen(1))
	propAlias := makeProp("email", alias, false, false)
	typAlias := buildTypeWithProps("User", []*schema.Property{propAlias})
	sAlias := buildMinimalSchema("app", []*schema.Type{typAlias}, nil)

	assert.Equal(t, schema.StructuralHash(sDirect), schema.StructuralHash(sAlias),
		"alias constraint must resolve to same hash as direct constraint")
}

func TestStructuralHash_TypeOrderingDeterministic(t *testing.T) {
	// Create schema with types added in one order.
	propA := makeProp("id", schema.NewUUIDConstraint(), false, true)
	propB := makeProp("name", schema.NewStringConstraint(), false, false)
	typA := buildTypeWithProps("Alpha", []*schema.Property{propA})
	typB := buildTypeWithProps("Beta", []*schema.Property{propB})

	// Schema.Types() returns lexicographic order regardless of insertion order.
	s1 := buildMinimalSchema("app", []*schema.Type{typB, typA}, nil)
	s2 := buildMinimalSchema("app", []*schema.Type{typA, typB}, nil)

	assert.Equal(t, schema.StructuralHash(s1), schema.StructuralHash(s2),
		"type insertion order must not affect hash")
}

func TestStructuralHash_Format(t *testing.T) {
	s := buildMinimalSchema("test", nil, nil)
	h := schema.StructuralHash(s)

	require.True(t, strings.HasPrefix(h, "sha256:"), "must start with sha256:")
	hex := strings.TrimPrefix(h, "sha256:")
	assert.Len(t, hex, 64, "SHA-256 hex digest must be 64 characters")
	for _, c := range hex {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hex digit must be lowercase hex: %c", c)
	}
}

func TestStructuralHash_ComprehensiveSchema(t *testing.T) {
	// Build a schema with types, properties, associations, compositions, data types.
	idProp := makeProp("id", schema.NewUUIDConstraint(), false, true)
	nameProp := makeProp("name", schema.StringLenBetween(1, 255), false, false)
	ageProp := makeProp("age", schema.IntegerBetween(0, 200), true, false)
	activeProp := makeProp("active", schema.NewBooleanConstraint(), false, false)

	personType := buildTypeWithProps("Person", []*schema.Property{idProp, nameProp, ageProp, activeProp})

	companyID := makeProp("id", schema.NewUUIDConstraint(), false, true)
	companyName := makeProp("name", schema.NewStringConstraint(), false, false)
	companyType := buildTypeWithProps("Company", []*schema.Property{companyID, companyName})

	edgeProp := makeProp("since", schema.NewDateConstraint(), false, false)
	worksAt := makeAssoc("WORKS_AT", "Company", false, false, []*schema.Property{edgeProp})
	schema.TestSetTypeAssociations(personType, []*schema.Relation{worksAt})
	schema.TestSetTypeAllAssociations(personType, []*schema.Relation{worksAt})

	addressProp := makeProp("street", schema.NewStringConstraint(), false, false)
	addressType := buildTypeWithProps("Address", []*schema.Property{addressProp})
	hasAddress := makeComp("HAS_ADDRESS", "Address", true, false)
	schema.TestSetTypeCompositions(personType, []*schema.Relation{hasAddress})
	schema.TestSetTypeAllCompositions(personType, []*schema.Relation{hasAddress})

	emailDT := schema.TestNewDataType("Email", schema.StringMinLen(1), zeroSpan, "")

	s := buildMinimalSchema(
		"hr",
		[]*schema.Type{personType, companyType, addressType},
		[]*schema.DataType{emailDT},
	)

	h1 := schema.StructuralHash(s)
	h2 := schema.StructuralHash(s)
	assert.Equal(t, h1, h2, "comprehensive schema must be deterministic")
	assert.True(t, strings.HasPrefix(h1, "sha256:"))
}

// assertConstraintsDiffer verifies two properties with different constraints
// produce different schema hashes.
func assertConstraintsDiffer(t *testing.T, propA, propB *schema.Property) {
	t.Helper()
	typA := buildTypeWithProps("T", []*schema.Property{propA})
	sA := buildMinimalSchema("s", []*schema.Type{typA}, nil)
	typB := buildTypeWithProps("T", []*schema.Property{propB})
	sB := buildMinimalSchema("s", []*schema.Type{typB}, nil)
	assert.NotEqual(t, schema.StructuralHash(sA), schema.StructuralHash(sB))
}

// assertConstraintsMatch verifies two properties with equivalent constraints
// produce the same schema hash.
func assertConstraintsMatch(t *testing.T, propA, propB *schema.Property) {
	t.Helper()
	typA := buildTypeWithProps("T", []*schema.Property{propA})
	sA := buildMinimalSchema("s", []*schema.Type{typA}, nil)
	typB := buildTypeWithProps("T", []*schema.Property{propB})
	sB := buildMinimalSchema("s", []*schema.Type{typB}, nil)
	assert.Equal(t, schema.StructuralHash(sA), schema.StructuralHash(sB))
}

// TestStructuralHash_ConstraintFieldCoverage uses reflection to enumerate all
// fields of each concrete constraint type and verifies that each field is covered
// by a hash stability test. This catches the case where a developer adds a new
// field to an existing constraint type without updating the hash function.
//
// The default panic on ConstraintKind guards against new *kinds*; this test
// guards against new *fields on existing kinds*.
func TestStructuralHash_ConstraintFieldCoverage(t *testing.T) {
	// Map of constraint type name → set of field names that are hashed.
	// Each entry must correspond to a field on the concrete struct.
	// "compiled" on PatternConstraint is derived from "patterns" and deliberately
	// not hashed separately — the hash uses Patterns() which returns the source strings.
	hashedFields := map[string]map[string]bool{
		"StringConstraint": {
			"minLen": true, // TestStructuralHash_ConstraintSensitivity ("string unbounded vs min")
			"maxLen": true, // TestStructuralHash_ConstraintSensitivity ("string unbounded vs max")
			"hasMin": true, // implied by minLen presence
			"hasMax": true, // implied by maxLen presence
		},
		"IntegerConstraint": {
			"min":    true, // TestStructuralHash_ConstraintSensitivity ("integer unbounded vs min")
			"max":    true, // TestStructuralHash_ConstraintSensitivity ("integer different max")
			"hasMin": true,
			"hasMax": true,
		},
		"FloatConstraint": {
			"min":    true, // TestStructuralHash_ConstraintSensitivity ("float unbounded vs min")
			"max":    true, // TestStructuralHash_ConstraintSensitivity ("float different max")
			"hasMin": true,
			"hasMax": true,
		},
		"BooleanConstraint": {}, // no fields
		"TimestampConstraint": {
			"format": true, // TestStructuralHash_ConstraintSensitivity ("timestamp default vs formatted")
		},
		"DateConstraint": {}, // no fields
		"UUIDConstraint": {}, // no fields
		"EnumConstraint": {
			"values": true, // TestStructuralHash_ConstraintSensitivity ("enum different values")
		},
		"PatternConstraint": {
			"patterns": true, // TestStructuralHash_ConstraintSensitivity ("pattern different patterns")
			"compiled": true, // derived from patterns, not hashed independently
		},
		"VectorConstraint": {
			"dimension": true, // TestStructuralHash_ConstraintSensitivity ("vector dimension")
		},
		"ListConstraint": {
			"element": true, // TestStructuralHash_ConstraintSensitivity ("list different element types")
			"minLen":  true, // TestStructuralHash_ConstraintSensitivity ("list unbounded vs bounded")
			"maxLen":  true, // TestStructuralHash_ConstraintSensitivity ("list different max")
			"hasMin":  true,
			"hasMax":  true,
		},
	}

	constraintTypes := []any{
		schema.StringConstraint{},
		schema.IntegerConstraint{},
		schema.FloatConstraint{},
		schema.BooleanConstraint{},
		schema.TimestampConstraint{},
		schema.DateConstraint{},
		schema.UUIDConstraint{},
		schema.EnumConstraint{},
		schema.PatternConstraint{},
		schema.VectorConstraint{},
		schema.ListConstraint{},
	}

	for _, c := range constraintTypes {
		rt := reflect.TypeOf(c)
		typeName := rt.Name()
		covered, ok := hashedFields[typeName]
		require.True(t, ok, "constraint type %s not in hashedFields map", typeName)

		for field := range rt.Fields() {
			_, isCovered := covered[field.Name]
			assert.True(t, isCovered,
				"constraint type %s has field %q (type %s) not listed in hashedFields coverage map — "+
					"if this field affects validation, add it to hashConstraint in schema/hash.go "+
					"and add a hash stability test",
				typeName, field.Name, field.Type)
		}

		// Also verify the coverage map doesn't list fields that don't exist
		for fieldName := range covered {
			found := false
			for field := range rt.Fields() {
				if field.Name == fieldName {
					found = true
					break
				}
			}
			assert.True(t, found,
				"hashedFields map lists %s.%s but no such field exists on the type",
				typeName, fieldName)
		}
	}
}

// TestStructuralHashVectors reads schema/testdata/hash_vectors.json and verifies
// that programmatically constructed schemas produce the expected hashes. These
// vectors serve as cross-language reference values for Python implementors.
func TestStructuralHashVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/hash_vectors.json")
	require.NoError(t, err, "hash_vectors.json must exist")

	var vectors []struct {
		Description string `json:"description"`
		SchemaName  string `json:"schema_name"`
		Expected    string `json:"expected_hash"`
	}
	require.NoError(t, json.Unmarshal(data, &vectors))
	require.NotEmpty(t, vectors, "hash_vectors.json must contain at least one vector")

	// Build the three reference schemas matching the vector descriptions
	schemas := map[string]*schema.Schema{
		"minimal":         buildVectorSchemaMinimal(),
		"relational":      buildVectorSchemaRelational(),
		"constraint_rich": buildVectorSchemaConstraintRich(),
	}

	for _, v := range vectors {
		t.Run(v.Description, func(t *testing.T) {
			s, ok := schemas[v.SchemaName]
			require.True(t, ok, "unknown schema_name %q in vectors", v.SchemaName)

			got := schema.StructuralHash(s)
			assert.Equal(t, v.Expected, got,
				"hash mismatch for %s — if the hash algorithm changed, "+
					"update the expected_hash in testdata/hash_vectors.json", v.SchemaName)
		})
	}
}

// buildVectorSchemaMinimal: single type, one string PK, no relations.
func buildVectorSchemaMinimal() *schema.Schema {
	idProp := makeProp("id", schema.NewStringConstraint(), false, true)
	typ := buildTypeWithProps("Item", []*schema.Property{idProp})
	return buildMinimalSchema("minimal", []*schema.Type{typ}, nil)
}

// buildVectorSchemaRelational: two types with association (+ edge props) and composition.
func buildVectorSchemaRelational() *schema.Schema {
	personID := makeProp("id", schema.NewStringConstraint(), false, true)
	personName := makeProp("name", schema.NewStringConstraint(), false, false)
	personType := buildTypeWithProps("Person", []*schema.Property{personID, personName})

	companyID := makeProp("id", schema.NewStringConstraint(), false, true)
	companyName := makeProp("name", schema.NewStringConstraint(), false, false)
	companyType := buildTypeWithProps("Company", []*schema.Property{companyID, companyName})

	edgeProp := makeProp("since", schema.NewDateConstraint(), false, false)
	worksAt := makeAssoc("WORKS_AT", "Company", false, false, []*schema.Property{edgeProp})
	schema.TestSetTypeAssociations(personType, []*schema.Relation{worksAt})
	schema.TestSetTypeAllAssociations(personType, []*schema.Relation{worksAt})

	addressStreet := makeProp("street", schema.NewStringConstraint(), false, false)
	addressType := buildTypeWithProps("Address", []*schema.Property{addressStreet})
	hasAddress := makeComp("HAS_ADDRESS", "Address", true, true)
	schema.TestSetTypeCompositions(personType, []*schema.Relation{hasAddress})
	schema.TestSetTypeAllCompositions(personType, []*schema.Relation{hasAddress})

	return buildMinimalSchema("relational",
		[]*schema.Type{personType, companyType, addressType}, nil)
}

// buildVectorSchemaConstraintRich: one type with properties exercising every constraint kind.
func buildVectorSchemaConstraintRich() *schema.Schema {
	props := []*schema.Property{
		makeProp("id", schema.NewStringConstraint(), false, true),
		makeProp("str_bounded", schema.StringLenBetween(1, 255), false, false),
		makeProp("int_bounded", schema.IntegerBetween(0, 1000), false, false),
		makeProp("flt_bounded", schema.FloatBetween(0.0, 100.0), false, false),
		makeProp("flag", schema.NewBooleanConstraint(), false, false),
		makeProp("ts", schema.NewTimestampConstraintFormatted("2006-01-02T15:04:05Z"), false, false),
		makeProp("dt", schema.NewDateConstraint(), false, false),
		makeProp("uid", schema.NewUUIDConstraint(), false, false),
		makeProp("status", schema.NewEnumConstraint([]string{"ACTIVE", "INACTIVE"}), false, false),
		makeProp("code", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[A-Z]{3}$`)}), false, false),
		makeProp("embedding", schema.NewVectorConstraint(128), false, false),
		makeProp("tags", schema.ListLenBetween(schema.NewStringConstraint(), 1, 10), false, false),
	}
	typ := buildTypeWithProps("Everything", props)
	return buildMinimalSchema("constraint_rich", []*schema.Type{typ}, nil)
}

// TestStructuralHash_DataTypeEdgeProperty pins the loaded-schema shape whose
// association declares a DataType-typed edge property: completion resolves the
// edge property's alias constraint, so hashing reaches a resolved terminal
// instead of the unrecognized-kind panic an unresolved alias produces. The
// hash must be deterministic and sensitive to the edge property's presence.
func TestStructuralHash_DataTypeEdgeProperty(t *testing.T) {
	load := func(edgeProps string) *schema.Schema {
		t.Helper()
		s, result := schema.LoadString(t.Context(), `schema "geo"

type StateCode = String [2, 2]

type State {
	code StateCode primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {`+edgeProps+`
	}
}
`, "test.yammm")
		require.False(t, result.HasErrors(), "load: %v", result.Err())
		return s
	}

	with := load("\n\t\tcode_tag StateCode")
	without := load("\n\t\tsince Integer")

	h1 := schema.StructuralHash(with)
	h2 := schema.StructuralHash(with)
	assert.Equal(t, h1, h2, "hash must be deterministic")
	assert.NotEqual(t, h1, schema.StructuralHash(without),
		"a DataType-typed edge property must contribute to the hash")
}

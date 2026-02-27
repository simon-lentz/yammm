package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// =============================================================================
// Primary modifier
// =============================================================================

// TestProperties_PrimaryImpliesRequired verifies that a property declared with
// the "primary" modifier is implicitly required — omitting it from instance data
// produces a validation failure.
// Source: SPEC.md, "Primary properties form part of the type's identity.
// They are implicitly required."
func TestProperties_PrimaryImpliesRequired(t *testing.T) {
	t.Parallel()

	data := "testdata/properties/data.json"
	v := loadSchema(t, "testdata/properties/primary_required.yammm")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Car")
		assertValid(t, v, "Car", records[0])
	})

	t.Run("missing_primary", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Car__missing_primary")
		// Primary implies required; the validator checks IsRequired() first,
		// so a missing primary property emits E_MISSING_REQUIRED.
		assertInvalid(t, v, "Car", records[0], diag.E_MISSING_REQUIRED)
	})
}

// =============================================================================
// Required modifier
// =============================================================================

// TestProperties_RequiredFieldEnforced verifies that a property declared with
// the "required" modifier must be present in every instance.
// Source: SPEC.md, "Required properties must be present in all instances."
func TestProperties_RequiredFieldEnforced(t *testing.T) {
	t.Parallel()

	data := "testdata/properties/data.json"
	v := loadSchema(t, "testdata/properties/required_field.yammm")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Person")
		assertValid(t, v, "Person", records[0])
	})

	t.Run("missing_required", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Person__missing_required")
		assertInvalid(t, v, "Person", records[0], diag.E_MISSING_REQUIRED)
	})
}

// =============================================================================
// Optional properties (no modifier)
// =============================================================================

// TestProperties_OptionalFieldsOmittable verifies that properties without
// modifiers are optional and may be omitted from instance data.
// Source: SPEC.md, "Properties without modifiers are optional and may be
// omitted from instance data."
func TestProperties_OptionalFieldsOmittable(t *testing.T) {
	t.Parallel()

	data := "testdata/properties/data.json"
	v := loadSchema(t, "testdata/properties/optional_field.yammm")

	// Config with only the primary key — all other fields omitted.
	records := loadTestData(t, data, "Config")
	assertValid(t, v, "Config", records[0])
}

// =============================================================================
// lc_keyword as property name
// =============================================================================

// TestProperties_LcKeywordAsPropertyName verifies that lowercase keyword forms
// (schema, type, required, primary, extends) are valid property names and
// that instances using them validate correctly.
// Source: SPEC.md, "PropertyName = LC_WORD | lc_keyword."
func TestProperties_LcKeywordAsPropertyName(t *testing.T) {
	t.Parallel()

	data := "testdata/properties/data.json"
	v := loadSchema(t, "testdata/properties/lc_keyword_property.yammm")

	records := loadTestData(t, data, "Meta")
	assertValid(t, v, "Meta", records[0])
}

// =============================================================================
// Relationship properties
// =============================================================================

// TestProperties_RelPropertyCompiles verifies that a schema declaring
// relationship properties (with required and optional modifiers) compiles
// cleanly.
// Source: SPEC.md, "Associations may have their own properties, declared
// within the relationship body."
func TestProperties_RelPropertyCompiles(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/properties/rel_properties.yammm")
}

// TestProperties_VectorInRelPropertyRejected verifies that Vector data types
// cannot be used in relationship properties — the schema fails compilation.
// Source: SPEC.md, "Relationship properties ... cannot use Vector data types."
func TestProperties_VectorInRelPropertyRejected(t *testing.T) {
	t.Parallel()
	result := loadSchemaExpectError(t, "testdata/properties/vector_in_rel.yammm")
	assertDiagHasCode(t, result, diag.E_INVALID_CONSTRAINT)
}

// =============================================================================
// Primary key type restrictions
// =============================================================================

// TestProperties_PrimaryKeyAllowedTypes verifies that the allowed PK types
// (String, UUID, Date, Timestamp) compile successfully as primary keys.
// Source: SPEC.md, "Primary Key Types" section.
func TestProperties_PrimaryKeyAllowedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
	}{
		{"String", `schema "PKString"
type R {
    id String primary
}`},
		{"UUID", `schema "PKUUID"
type R {
    id UUID primary
}`},
		{"Date", `schema "PKDate"
type R {
    day Date primary
}`},
		{"Timestamp", `schema "PKTimestamp"
type R {
    ts Timestamp primary
}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loadSchemaString(t, tt.schema, "pk_allowed_"+tt.name)
		})
	}
}

// TestProperties_PrimaryKeyBannedTypes verifies that disallowed types produce
// E_INVALID_PRIMARY_KEY_TYPE when used as primary keys.
// Source: SPEC.md, "Primary Key Types" section.
func TestProperties_PrimaryKeyBannedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
	}{
		{"Integer", `schema "PKInteger"
type R {
    id Integer primary
}`},
		{"Float", `schema "PKFloat"
type R {
    val Float primary
}`},
		{"Boolean", `schema "PKBoolean"
type R {
    flag Boolean primary
}`},
		{"Enum", `schema "PKEnum"
type R {
    status Enum("a", "b") primary
}`},
		{"Pattern", `schema "PKPattern"
type R {
    code Pattern("^[A-Z]+$") primary
}`},
		{"Vector", `schema "PKVector"
type R {
    emb Vector[3] primary
}`},
		{"List", `schema "PKList"
type R {
    tags List<String> primary
}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := loadSchemaStringExpectError(t, tt.schema, "pk_banned_"+tt.name)
			assertDiagHasCode(t, result, diag.E_INVALID_PRIMARY_KEY_TYPE)
		})
	}
}

// TestProperties_PrimaryKeyAliasToAllowedType verifies that a DataType alias
// resolving to an allowed PK type (e.g., String) is accepted.
// Source: SPEC.md, "Alias resolution: if a property uses a DataType alias,
// the resolved constraint is checked."
func TestProperties_PrimaryKeyAliasToAllowedType(t *testing.T) {
	t.Parallel()
	loadSchemaString(t, `schema "PKAlias"
type VIN = String[17, 17]
type Car {
    vin VIN primary
}`, "pk_alias_allowed")
}

// TestProperties_PrimaryKeyAliasToBannedType verifies that a DataType alias
// resolving to a banned PK type (e.g., Integer) is rejected.
func TestProperties_PrimaryKeyAliasToBannedType(t *testing.T) {
	t.Parallel()
	result := loadSchemaStringExpectError(t, `schema "PKAliasBanned"
type Count = Integer[0,]
type R {
    n Count primary
}`, "pk_alias_banned")
	assertDiagHasCode(t, result, diag.E_INVALID_PRIMARY_KEY_TYPE)
}

// TestProperties_CompositePrimaryKeyAllowed verifies that composite primary
// keys with multiple allowed types compile successfully.
func TestProperties_CompositePrimaryKeyAllowed(t *testing.T) {
	t.Parallel()
	loadSchemaString(t, `schema "PKComposite"
type Enrollment {
    region String primary
    studentId UUID primary
    day Date primary
}`, "pk_composite_allowed")
}

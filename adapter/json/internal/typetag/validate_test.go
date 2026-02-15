package typetag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_Unqualified(t *testing.T) {
	tests := []struct {
		name       string
		typeName   string
		wantDetail string // empty means valid
	}{
		// Valid cases
		{"simple", "Person", ""},
		{"with_underscore", "My_Type", ""},
		{"with_numbers", "Type123", ""},
		{"single_letter", "X", ""},
		{"complex", "MyComplexType_V2", ""},

		// Invalid syntax cases
		{"empty", "", DetailEmptyTag},
		{"lowercase_start", "person", DetailMustStartUpper},
		{"starts_with_number", "123Type", DetailMustStartUpper},
		{"starts_with_underscore", "_Type", DetailMustStartUpper},
		{"contains_hyphen", "My-Type", DetailInvalidChars},
		{"contains_space", "My Type", DetailInvalidChars},
		{"unicode_start", "Ütf8", DetailMustStartUpper},

		// Old DSL keywords are now valid (no longer reserved)
		{"old_reserved_type", "Type", ""},     // starts uppercase, valid
		{"old_reserved_schema", "Schema", ""}, // starts uppercase, valid

		// Datatype keywords are reserved
		{"datatype_integer", "Integer", DetailReservedDatatype},
		{"datatype_float", "Float", DetailReservedDatatype},
		{"datatype_boolean", "Boolean", DetailReservedDatatype},
		{"datatype_string", "String", DetailReservedDatatype},
		{"datatype_enum", "Enum", DetailReservedDatatype},
		{"datatype_pattern", "Pattern", DetailReservedDatatype},
		{"datatype_timestamp", "Timestamp", DetailReservedDatatype},
		{"datatype_date", "Date", DetailReservedDatatype},
		{"datatype_uuid", "UUID", DetailReservedDatatype},
		{"datatype_vector", "Vector", DetailReservedDatatype},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.typeName)
			if tt.wantDetail == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var tagErr *Error
				require.ErrorAs(t, err, &tagErr)
				assert.Equal(t, tt.typeName, tagErr.Tag)
				assert.Equal(t, tt.wantDetail, tagErr.Detail)
			}
		})
	}
}

func TestValidate_Qualified(t *testing.T) {
	tests := []struct {
		name       string
		typeName   string
		wantDetail string // empty means valid
	}{
		// Valid cases
		{"lowercase_alias", "common.Entity", ""},
		{"uppercase_alias", "Common.Entity", ""},
		{"complex_alias", "my_module.MyType", ""},
		{"single_char_alias", "c.Type", ""},
		{"underscore_in_both", "my_mod.My_Type", ""},

		// Structural errors
		{"empty_alias_leading_dot", ".Entity", DetailLeadingDot},
		{"empty_type_trailing_dot", "common.", DetailTrailingDot},
		{"multiple_dots", "a.b.c", DetailMultipleDots},
		{"triple_qualified", "a.b.C", DetailMultipleDots},
		{"dot_only", ".", DetailLeadingDot}, // "." has dot at position 0, so leading dot fires first

		// Alias errors
		{"number_start_alias", "123.Type", DetailAliasStartLetter},
		{"underscore_start_alias", "_mod.Type", DetailAliasStartLetter},
		{"hyphen_in_alias", "my-mod.Type", DetailAliasInvalidChars},

		// Type name errors
		{"lowercase_type", "common.entity", DetailMustStartUpper},
		{"number_start_type", "common.123Type", DetailMustStartUpper},
		{"hyphen_in_type", "common.My-Type", DetailInvalidChars},

		// Datatype keywords in qualified form
		{"datatype_integer_qualified", "alias.Integer", DetailReservedDatatype},
		{"datatype_string_qualified", "alias.String", DetailReservedDatatype},
		{"datatype_uuid_qualified", "common.UUID", DetailReservedDatatype},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.typeName)
			if tt.wantDetail == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var tagErr *Error
				require.ErrorAs(t, err, &tagErr)
				assert.Equal(t, tt.typeName, tagErr.Tag)
				assert.Equal(t, tt.wantDetail, tagErr.Detail)
			}
		})
	}
}

func TestIsValidUnqualified(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid_simple", "Person", true},
		{"valid_with_underscore", "My_Type", true},
		{"valid_with_numbers", "Type123", true},
		{"invalid_lowercase", "person", false},
		{"invalid_empty", "", false},
		{"invalid_number_start", "1Type", false},
		{"invalid_underscore_start", "_Type", false},
		{"invalid_hyphen", "My-Type", false},
		// Datatype keywords fail validation
		{"datatype_integer", "Integer", false},
		{"datatype_string", "String", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidUnqualified(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidQualified(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid_lowercase_alias", "common.Entity", true},
		{"valid_uppercase_alias", "Common.Entity", true},
		{"valid_mixed", "myMod.MyType", true},
		{"invalid_no_dot", "CommonEntity", false},
		{"invalid_empty_alias", ".Entity", false},
		{"invalid_empty_type", "common.", false},
		{"invalid_multiple_dots", "a.b.C", false},
		{"invalid_lowercase_type", "common.entity", false},
		// Datatype keywords fail validation
		{"datatype_qualified", "alias.Integer", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidQualified(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsDatatypeKeyword(t *testing.T) {
	datatypes := []string{
		"Integer", "Float", "Boolean", "String", "Enum",
		"Pattern", "Timestamp", "Date", "UUID", "Vector",
	}

	for _, dt := range datatypes {
		t.Run(dt, func(t *testing.T) {
			assert.True(t, IsDatatypeKeyword(dt), "%q should be a datatype keyword", dt)
		})
	}

	// Not datatype keywords
	notDatatypes := []string{
		"Person", "Entity", "foo", "integer", "string", // case sensitive
		"schema", "type", "import", "true", "false", // old DSL keywords
	}
	for _, name := range notDatatypes {
		t.Run("not_"+name, func(t *testing.T) {
			assert.False(t, IsDatatypeKeyword(name), "%q should not be a datatype keyword", name)
		})
	}
}

func TestError(t *testing.T) {
	err := &Error{Tag: "test", Detail: "test detail"}
	assert.Equal(t, "test detail", err.Error())
}

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
		{"boundary_chars", "Zz09_Aa", ""},
		{"boundary_first_upper", "Az", ""},

		// Invalid syntax cases
		{"empty", "", DetailEmptyTag},
		{"lowercase_start", "person", DetailMustStartUpper},
		{"starts_with_number", "123Type", DetailMustStartUpper},
		{"starts_with_underscore", "_Type", DetailMustStartUpper},
		{"contains_hyphen", "My-Type", DetailInvalidChars},
		{"contains_space", "My Type", DetailInvalidChars},
		{"unicode_start", "Ütf8", DetailMustStartUpper},
		{"invalid_utf8_start", "\xffType", DetailMustStartUpper},

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
		{"datatype_list", "List", DetailReservedDatatype},
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
		{"boundary_alias", "az09_.Zz09_A", ""},
		{"digit_in_alias", "mod2.Person", ""},
		{"digit_in_qualified_type", "common.Entity2", ""},

		// Structural errors
		{"empty_alias_leading_dot", ".Entity", DetailLeadingDot},
		{"empty_type_trailing_dot", "common.", DetailTrailingDot},
		{"multiple_dots", "a.b.c", DetailMultipleDots},
		{"triple_qualified", "a.b.C", DetailMultipleDots},
		{"adjacent_dots", "a..b", DetailMultipleDots},
		{"dot_after_type", "a.b.", DetailMultipleDots}, // the multi-dot scan wins over the trailing dot
		{"dot_only", ".", DetailLeadingDot},            // "." has dot at position 0, so leading dot fires first

		// Alias errors
		{"number_start_alias", "123.Type", DetailAliasStartLetter},
		{"underscore_start_alias", "_mod.Type", DetailAliasStartLetter},
		{"hyphen_in_alias", "my-mod.Type", DetailAliasInvalidChars},
		{"invalid_utf8_alias", "\xff.Type", DetailAliasStartLetter},

		// A reserved spelling is refused in the alias half, which admits
		// either case, where the type half refuses the datatype names alone.
		{"reserved_lc_alias", "type.Person", DetailAliasReserved},
		{"reserved_literal_alias", "true.Person", DetailAliasReserved},
		{"datatype_alias", "Integer.Person", DetailAliasReserved},
		{"list_alias", "List.Person", DetailAliasReserved},
		// Both halves are defective: the alias is the one reported.
		{"reserved_alias_and_bad_type", "type.entity", DetailAliasReserved},

		// Type name errors
		{"lowercase_type", "common.entity", DetailMustStartUpper},
		{"number_start_type", "common.123Type", DetailMustStartUpper},
		{"hyphen_in_type", "common.My-Type", DetailInvalidChars},
		{"invalid_utf8_type", "common.\xffType", DetailMustStartUpper},

		// Datatype keywords in qualified form
		{"datatype_integer_qualified", "alias.Integer", DetailReservedDatatype},
		{"datatype_string_qualified", "alias.String", DetailReservedDatatype},
		{"datatype_uuid_qualified", "common.UUID", DetailReservedDatatype},
		{"datatype_list_qualified", "common.List", DetailReservedDatatype},
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
				assert.Equal(t, tt.wantDetail, tagErr.Detail)
			}
		})
	}
}

func TestIsDatatypeKeyword(t *testing.T) {
	datatypes := []string{
		"Integer", "Float", "Boolean", "String", "Enum",
		"Pattern", "Timestamp", "Date", "UUID", "Vector", "List",
	}

	for _, dt := range datatypes {
		t.Run(dt, func(t *testing.T) {
			assert.True(t, isDatatypeKeyword(dt), "%q should be a datatype keyword", dt)
		})
	}

	// Not datatype keywords
	notDatatypes := []string{
		"Person", "Entity", "foo", "integer", "string", // case sensitive
		"schema", "type", "import", "true", "false", // old DSL keywords
	}
	for _, name := range notDatatypes {
		t.Run("not_"+name, func(t *testing.T) {
			assert.False(t, isDatatypeKeyword(name), "%q should not be a datatype keyword", name)
		})
	}
}

// TestDetailStrings pins the text of every canonical detail. Both parsers put
// it in an operator-facing message and in the issue's structured detail, so
// the text is shipped behaviour; every other assertion in this package
// compares a returned detail against the same constant and so cannot see a
// reword.
func TestDetailStrings(t *testing.T) {
	for _, tt := range []struct {
		got  string
		want string
	}{
		{DetailEmptyTag, "empty type tag"},
		{DetailMustStartUpper, "type name must start with uppercase letter"},
		{DetailInvalidChars, "type name contains invalid characters"},
		{DetailLeadingDot, "leading dot in qualified name"},
		{DetailTrailingDot, "trailing dot in qualified name"},
		{DetailMultipleDots, "multiple dots in qualified name"},
		{DetailReservedDatatype, "reserved datatype keyword"},
		{DetailAliasInvalidChars, "alias contains invalid characters"},
		{DetailAliasStartLetter, "alias must start with letter"},
		{DetailAliasReserved, "alias is a reserved keyword"},
	} {
		if tt.got != tt.want {
			t.Errorf("detail = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestError(t *testing.T) {
	err := &Error{Detail: "test detail"}
	assert.Equal(t, "test detail", err.Error())
}

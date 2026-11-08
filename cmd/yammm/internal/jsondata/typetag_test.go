package jsondata

import (
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// grammarVocabulary reads the parser's vocabulary from the names file its own
// test holds equal to the maps its lookahead groups are checked against.
func grammarVocabulary(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile("../../../../internal/parse/testdata/reserved_words.txt")
	if err != nil {
		t.Fatal(err)
	}
	words := map[string]bool{}
	for w := range strings.FieldsSeq(string(data)) {
		words[w] = true
	}
	return words
}

// TestReservedVocabularyMatchesTheGrammar holds the two reserved
// maps of [typeTagFault] equal to the parser's vocabulary, and holds the two maps to the split the
// grammar's LC_WORD and UC_WORD branches make. Both lists are hand-written, in
// different packages, and only this test couples them.
func TestReservedVocabularyMatchesTheGrammar(t *testing.T) {
	grammar := grammarVocabulary(t)

	for kw := range grammar {
		if !isReservedName(kw) {
			t.Errorf("the grammar refuses %q where either case is admitted; typeTagFault accepts it", kw)
		}
	}
	for kw := range datatypeKeywords {
		if !grammar[kw] {
			t.Errorf("typeTagFault refuses the datatype name %q; the grammar does not", kw)
		}
	}
	for kw := range reservedLC {
		if !grammar[kw] {
			t.Errorf("typeTagFault refuses the lowercase keyword %q; the grammar does not", kw)
		}
	}

	// The maps partition the vocabulary by case, and no behaviour can prove
	// it: the alias half looks a name up in the union, and the type half
	// refuses a lower-case name before it consults a vocabulary at all.
	for kw := range datatypeKeywords {
		if first, _ := utf8.DecodeRuneInString(kw); !isUpperASCII(first) {
			t.Errorf("datatypeKeywords holds %q, which is not a UC_WORD", kw)
		}
		if reservedLC[kw] {
			t.Errorf("%q is in both maps, which must be disjoint", kw)
		}
	}
	for kw := range reservedLC {
		if first, _ := utf8.DecodeRuneInString(kw); isUpperASCII(first) {
			t.Errorf("reservedLC holds %q, which is a UC_WORD", kw)
		}
	}
}

// TestTypeTagFault_RefusesEveryReservedSpelling drives both halves of a qualified
// tag against the grammar's whole vocabulary. A datatype name moved into the
// lowercase map fails here; the reverse cannot, because a lower-case name is
// refused before any vocabulary is read. The maps' own split is pinned by
// [TestReservedVocabularyMatchesTheGrammar].
func TestTypeTagFault_RefusesEveryReservedSpelling(t *testing.T) {
	for kw := range grammarVocabulary(t) {
		t.Run(kw, func(t *testing.T) {
			assertTagFault(t, kw+".Person", tagFaultAliasReserved)

			first, _ := utf8.DecodeRuneInString(kw)
			if isUpperASCII(first) {
				assertTagFault(t, kw, tagFaultReservedDatatype)
				assertTagFault(t, "mod."+kw, tagFaultReservedDatatype)
				return
			}
			// A lowercase keyword never reaches the type half's reserved
			// check: that half refuses a name not starting with an uppercase
			// ASCII letter before it looks the name up.
			assertTagFault(t, kw, tagFaultMustStartUpper)
			assertTagFault(t, "mod."+kw, tagFaultMustStartUpper)
		})
	}
}

func assertTagFault(t *testing.T, tag, want string) {
	t.Helper()
	if got := typeTagFault(tag); got != want {
		t.Errorf("typeTagFault(%q) = %q, want %q", tag, got, want)
	}
}

func TestTypeTagFault_Unqualified(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		wantFault string // empty means valid
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
		{"empty", "", tagFaultEmptyTag},
		{"lowercase_start", "person", tagFaultMustStartUpper},
		{"starts_with_number", "123Type", tagFaultMustStartUpper},
		{"starts_with_underscore", "_Type", tagFaultMustStartUpper},
		{"contains_hyphen", "My-Type", tagFaultInvalidChars},
		{"contains_space", "My Type", tagFaultInvalidChars},
		{"unicode_start", "Ütf8", tagFaultMustStartUpper},
		{"invalid_utf8_start", "\xffType", tagFaultMustStartUpper},

		// A lowercase keyword's capitalized spelling is an ordinary name.
		{"capitalized_keyword_type", "Type", ""},
		{"capitalized_keyword_schema", "Schema", ""},

		// Each boundary of the character classes.
		{"past_upper_low", "@x", tagFaultMustStartUpper},
		{"past_upper_high", "[x", tagFaultMustStartUpper},
		{"past_letter_upper_low", "A@", tagFaultInvalidChars},
		{"past_letter_upper_high", "A[", tagFaultInvalidChars},
		{"past_letter_lower_low", "A`", tagFaultInvalidChars},
		{"past_letter_lower_high", "A{", tagFaultInvalidChars},
		{"past_digit_low", "A/", tagFaultInvalidChars},
		{"past_digit_high", "A:", tagFaultInvalidChars},
		{"last_upper_past_first", "AZ", ""},
		{"non_ascii_past_first", "AÜ", tagFaultInvalidChars},
		{"bad_second_rune", "A-", tagFaultInvalidChars},
		{"bad_last_rune", "Ty-", tagFaultInvalidChars},

		// Datatype keywords are reserved
		{"datatype_integer", "Integer", tagFaultReservedDatatype},
		{"datatype_float", "Float", tagFaultReservedDatatype},
		{"datatype_boolean", "Boolean", tagFaultReservedDatatype},
		{"datatype_string", "String", tagFaultReservedDatatype},
		{"datatype_enum", "Enum", tagFaultReservedDatatype},
		{"datatype_pattern", "Pattern", tagFaultReservedDatatype},
		{"datatype_timestamp", "Timestamp", tagFaultReservedDatatype},
		{"datatype_date", "Date", tagFaultReservedDatatype},
		{"datatype_uuid", "UUID", tagFaultReservedDatatype},
		{"datatype_vector", "Vector", tagFaultReservedDatatype},
		{"datatype_list", "List", tagFaultReservedDatatype},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTagFault(t, tt.typeName, tt.wantFault)
		})
	}
}

func TestTypeTagFault_Qualified(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		wantFault string // empty means valid
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
		{"empty_alias_leading_dot", ".Entity", tagFaultLeadingDot},
		{"empty_type_trailing_dot", "common.", tagFaultTrailingDot},
		{"multiple_dots", "a.b.c", tagFaultMultipleDots},
		{"triple_qualified", "a.b.C", tagFaultMultipleDots},
		{"adjacent_dots", "a..b", tagFaultMultipleDots},
		{"dot_after_type", "a.b.", tagFaultMultipleDots},
		{"dot_only", ".", tagFaultLeadingDot},

		// Alias errors
		{"number_start_alias", "123.Type", tagFaultAliasStartLetter},
		{"underscore_start_alias", "_mod.Type", tagFaultAliasStartLetter},
		{"hyphen_in_alias", "my-mod.Type", tagFaultAliasInvalidChars},
		{"bad_alias_second_rune", "a-b.T", tagFaultAliasInvalidChars},
		{"bad_alias_last_rune", "ab-.T", tagFaultAliasInvalidChars},
		{"invalid_utf8_alias", "\xff.Type", tagFaultAliasStartLetter},

		// A reserved spelling is refused in the alias half, which admits
		// either case, where the type half refuses the datatype names alone.
		{"reserved_lc_alias", "type.Person", tagFaultAliasReserved},
		{"reserved_literal_alias", "true.Person", tagFaultAliasReserved},
		{"datatype_alias", "Integer.Person", tagFaultAliasReserved},
		{"list_alias", "List.Person", tagFaultAliasReserved},
		// Both halves are defective: the alias is the one reported.
		{"reserved_alias_and_bad_type", "type.entity", tagFaultAliasReserved},
		{"bad_alias_start_and_bad_type", "_mod.type", tagFaultAliasStartLetter},
		{"bad_alias_rune_and_bad_type", "my-mod.type", tagFaultAliasInvalidChars},

		// Type name errors
		{"lowercase_type", "common.entity", tagFaultMustStartUpper},
		{"number_start_type", "common.123Type", tagFaultMustStartUpper},
		{"hyphen_in_type", "common.My-Type", tagFaultInvalidChars},
		{"invalid_utf8_type", "common.\xffType", tagFaultMustStartUpper},

		// Datatype keywords in qualified form
		{"datatype_integer_qualified", "alias.Integer", tagFaultReservedDatatype},
		{"datatype_string_qualified", "alias.String", tagFaultReservedDatatype},
		{"datatype_uuid_qualified", "common.UUID", tagFaultReservedDatatype},
		{"datatype_list_qualified", "common.List", tagFaultReservedDatatype},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertTagFault(t, tt.typeName, tt.wantFault)
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
			if !isDatatypeKeyword(dt) {
				t.Errorf("%q should be a datatype keyword", dt)
			}
		})
	}

	// Not datatype keywords
	notDatatypes := []string{
		"Person", "Entity", "foo", "integer", "string", // case sensitive
		"schema", "type", "import", "true", "false", // the lowercase vocabulary
	}
	for _, name := range notDatatypes {
		t.Run("not_"+name, func(t *testing.T) {
			if isDatatypeKeyword(name) {
				t.Errorf("%q should not be a datatype keyword", name)
			}
		})
	}
}

// A refused tag's issue states the reason in its message and its detail, and the
// tag as what the parser got.
func TestParse_RefusedTagIssueStatesTheReason(t *testing.T) {
	_, result := Parse(t.Context(), location.NewSourceID("test://tag.json"), []byte(`{"person": []}`))
	var issue diag.Issue
	for found := range result.Errors() {
		issue = found
		break
	}
	if issue.Code() != diag.E_INVALID_TYPE_TAG {
		t.Fatalf("code %v, want E_INVALID_TYPE_TAG (%s)", issue.Code(), issue.Message())
	}
	if want := `invalid type name "person": ` + tagFaultMustStartUpper; issue.Message() != want {
		t.Errorf("message = %q, want %q", issue.Message(), want)
	}
	want := []diag.Detail{{Key: diag.DetailKeyGot, Value: "person"}, {Key: diag.DetailKeyDetail, Value: tagFaultMustStartUpper}}
	if !slices.Equal(issue.Details(), want) {
		t.Errorf("details = %v, want %v", issue.Details(), want)
	}
}

// TestTagFaultStrings pins the text of every reason. The parser puts it in an
// operator-facing message and in the issue's structured detail, so the text is
// shipped behaviour; every other assertion here compares a returned reason
// against the same constant and so cannot see a reword.
func TestTagFaultStrings(t *testing.T) {
	for _, tt := range []struct {
		got  string
		want string
	}{
		{tagFaultEmptyTag, "empty type tag"},
		{tagFaultMustStartUpper, "type name must start with uppercase letter"},
		{tagFaultInvalidChars, "type name contains invalid characters"},
		{tagFaultLeadingDot, "leading dot in qualified name"},
		{tagFaultTrailingDot, "trailing dot in qualified name"},
		{tagFaultMultipleDots, "multiple dots in qualified name"},
		{tagFaultReservedDatatype, "reserved datatype keyword"},
		{tagFaultAliasInvalidChars, "alias contains invalid characters"},
		{tagFaultAliasStartLetter, "alias must start with letter"},
		{tagFaultAliasReserved, "alias is a reserved keyword"},
	} {
		if tt.got != tt.want {
			t.Errorf("reason = %q, want %q", tt.got, tt.want)
		}
	}
}

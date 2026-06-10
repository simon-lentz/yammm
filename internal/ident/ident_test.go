package ident_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/ident"
)

// TestToLowerSnake_SpecExamples tests all examples from the architecture spec.
func TestToLowerSnake_SpecExamples(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "all-caps to lowercase", input: "WORKS_AT", want: "works_at"},
		{name: "simple all-caps", input: "KNOWS", want: "knows"},
		{name: "acronym boundary", input: "HTTPProxy", want: "http_proxy"},
		{name: "CamelCase split", input: "CreatedBy", want: "created_by"},
		{name: "trailing acronym", input: "UserID", want: "user_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToLowerSnake(tt.input); got != tt.want {
				t.Errorf("ToLowerSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToLowerSnake_EdgeCases tests additional edge cases beyond the spec.
func TestToLowerSnake_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "single lowercase", input: "a", want: "a"},
		{name: "single uppercase", input: "A", want: "a"},
		{name: "two-char acronym", input: "ID", want: "id"},
		{name: "leading acronym", input: "XMLParser", want: "xml_parser"},
		{name: "trailing acronym lc", input: "parseXML", want: "parse_xml"},
		{name: "digits and letters", input: "ABC123DEF", want: "abc_123_def"},
		{name: "acronym plus digit", input: "HTTP2Server", want: "http_2_server"},
		{name: "pre-snaked input", input: "ALREADY_SNAKE", want: "already_snake"},
		{name: "already lowercase snake", input: "already_snake", want: "already_snake"},
		{name: "leading underscores stripped", input: "__private", want: "private"},
		{name: "trailing underscores stripped", input: "trailing__", want: "trailing"},
		{name: "multiple underscores collapsed", input: "foo___bar", want: "foo_bar"},
		{name: "mixed case complex", input: "getHTTPResponseCode", want: "get_http_response_code"},
		{name: "single letter segments", input: "aBC", want: "a_bc"},
		{name: "consecutive digits", input: "foo123bar456", want: "foo_123_bar_456"},
		{name: "all digits", input: "123", want: "123"},
		{name: "all underscores", input: "___", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToLowerSnake(tt.input); got != tt.want {
				t.Errorf("ToLowerSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToLowerSnake_Unicode tests Unicode/rune support.
func TestToLowerSnake_Unicode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unicode with acronym", input: "ÅngströmID", want: "ångström_id"},
		{name: "unicode lowercase", input: "café", want: "café"},
		{name: "unicode uppercase", input: "CAFÉ", want: "café"},
		{name: "mixed unicode", input: "CaféOwner", want: "café_owner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToLowerSnake(tt.input); got != tt.want {
				t.Errorf("ToLowerSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToLowerSnake_Idempotent verifies the idempotency property.
func TestToLowerSnake_Idempotent(t *testing.T) {
	inputs := []string{
		"WORKS_AT",
		"HTTPProxy",
		"CreatedBy",
		"UserID",
		"already_snake",
		"MixedCASE_Identifier",
		"ABC123DEF",
		"ÅngströmID",
		"",
		"a",
		"A",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			first := ident.ToLowerSnake(input)
			if second := ident.ToLowerSnake(first); second != first {
				t.Errorf("ToLowerSnake not idempotent on %q: %q then %q", input, first, second)
			}
		})
	}
}

// TestToLowerSnake_Idempotent_Random tests idempotency with random inputs.
func TestToLowerSnake_Idempotent_Random(t *testing.T) {
	r := rand.New(rand.NewSource(99)) //nolint:gosec // deterministic pseudo-randomness is fine in tests
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")
	next := func() string {
		n := r.Intn(12) + 1
		var b strings.Builder
		b.Grow(n)
		for range n {
			b.WriteRune(alphabet[r.Intn(len(alphabet))])
		}
		return b.String()
	}

	for range 100 {
		src := next()
		first := ident.ToLowerSnake(src)
		if second := ident.ToLowerSnake(first); second != first {
			t.Errorf("ToLowerSnake not idempotent on random input %q: %q then %q", src, first, second)
		}
	}
}

// TestCapitalize tests the Capitalize function.
func TestCapitalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercase", input: "blah", want: "Blah"},
		{name: "empty", input: "", want: ""},
		{name: "snake to camel", input: "http_server", want: "HttpServer"},
		{name: "preserve acronym", input: "ID_number", want: "IDNumber"},
		{name: "unicode", input: "åäö", want: "Åäö"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.Capitalize(tt.input); got != tt.want {
				t.Errorf("Capitalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToUpperCamel tests the ToUpperCamel function.
func TestToUpperCamel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "complex", input: "St(range)___pCamelCase32_33Foo", want: "StRangePCamelCase32_33Foo"},
		{name: "snake", input: "foo_bar_baz", want: "FooBarBaz"},
		{name: "preserve acronym run", input: "HTTP_Server", want: "HTTPServer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamel(tt.input); got != tt.want {
				t.Errorf("ToUpperCamel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToUpperCamelInitialisms tests caller-supplied initialism-aware UpperCamel
// conversion: only segments present in the supplied set are upper-cased wholesale.
func TestToUpperCamelInitialisms(t *testing.T) {
	inits := map[string]bool{"id": true, "url": true, "http": true, "json": true}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain word", input: "color", want: "Color"},
		{name: "snake non-initialism", input: "first_name", want: "FirstName"},
		{name: "initialism alone", input: "id", want: "ID"},
		{name: "trailing initialism", input: "user_id", want: "UserID"},
		{name: "trailing initialism url", input: "base_url", want: "BaseURL"},
		{name: "leading initialism", input: "http_server", want: "HTTPServer"},
		{name: "embedded initialism", input: "json_payload", want: "JSONPayload"},
		{name: "word outside supplied set", input: "api_key", want: "ApiKey"},
		{name: "non-initialism word", input: "name", want: "Name"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamelInitialisms(tt.input, inits); got != tt.want {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToLowerCamel tests the ToLowerCamel function.
func TestToLowerCamel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "complex", input: "St(range)___pCamelCase32_33Foo", want: "stRangePCamelCase32_33Foo"},
		{name: "snake", input: "foo_bar_baz", want: "fooBarBaz"},
		{name: "HTTP acronym", input: "HTTPServer", want: "httpServer"},
		{name: "HTTP with underscore", input: "HTTP_Server", want: "httpServer"},
		{name: "ID acronym", input: "ID_number", want: "idNumber"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToLowerCamel(tt.input); got != tt.want {
				t.Errorf("ToLowerCamel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestCamelTransforms_NumericSegmentsSeparated tests numeric segment handling.
func TestCamelTransforms_NumericSegmentsSeparated(t *testing.T) {
	// Adjacent numeric segments should always be separated by "_" including 0 and 9.
	if got := ident.ToLowerCamel("foo 1 2 bar"); got != "foo1_2Bar" {
		t.Errorf("ToLowerCamel(\"foo 1 2 bar\") = %q, want \"foo1_2Bar\"", got)
	}
	if got := ident.ToLowerCamel("foo 0 0 bar"); got != "foo0_0Bar" {
		t.Errorf("ToLowerCamel(\"foo 0 0 bar\") = %q, want \"foo0_0Bar\"", got)
	}
	if got := ident.ToLowerCamel("foo 9 9 bar"); got != "foo9_9Bar" {
		t.Errorf("ToLowerCamel(\"foo 9 9 bar\") = %q, want \"foo9_9Bar\"", got)
	}

	if got := ident.ToUpperCamel("foo 1 2 bar"); got != "Foo1_2Bar" {
		t.Errorf("ToUpperCamel(\"foo 1 2 bar\") = %q, want \"Foo1_2Bar\"", got)
	}
	if got := ident.ToUpperCamel("foo 0 0 bar"); got != "Foo0_0Bar" {
		t.Errorf("ToUpperCamel(\"foo 0 0 bar\") = %q, want \"Foo0_0Bar\"", got)
	}
	if got := ident.ToUpperCamel("foo 9 9 bar"); got != "Foo9_9Bar" {
		t.Errorf("ToUpperCamel(\"foo 9 9 bar\") = %q, want \"Foo9_9Bar\"", got)
	}
}

// TestCamelTransforms_PreservesAcronymRuns tests acronym preservation.
func TestCamelTransforms_PreservesAcronymRuns(t *testing.T) {
	if got := ident.ToLowerCamel("HTTPServer"); got != "httpServer" {
		t.Errorf("ToLowerCamel(\"HTTPServer\") = %q, want \"httpServer\"", got)
	}
	if got := ident.ToLowerCamel("HTTP_Server"); got != "httpServer" {
		t.Errorf("ToLowerCamel(\"HTTP_Server\") = %q, want \"httpServer\"", got)
	}
	if got := ident.ToUpperCamel("ID_number"); got != "IDNumber" {
		t.Errorf("ToUpperCamel(\"ID_number\") = %q, want \"IDNumber\"", got)
	}
}

func TestToUpperCamel_EmptyString(t *testing.T) {
	if got := ident.ToUpperCamel(""); got != "" {
		t.Errorf("ToUpperCamel(%q) = %q, want %q", "", got, "")
	}
}

func TestToLowerCamel_EmptyString(t *testing.T) {
	if got := ident.ToLowerCamel(""); got != "" {
		t.Errorf("ToLowerCamel(%q) = %q, want %q", "", got, "")
	}
}

// TestCamelTransforms_LeadingDigits tests that identifiers starting with digits
// get prefixed with underscore to ensure valid Go identifiers.
func TestCamelTransforms_LeadingDigits(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantUpper string
		wantLower string
	}{
		{name: "digit prefix", input: "123name", wantUpper: "_123Name", wantLower: "_123Name"},
		{name: "digit only", input: "123", wantUpper: "_123", wantLower: "_123"},
		{name: "digit underscore word", input: "9_lives", wantUpper: "_9Lives", wantLower: "_9Lives"},
		{name: "leading zero", input: "0value", wantUpper: "_0Value", wantLower: "_0Value"},
		{name: "multiple leading digits", input: "42foo", wantUpper: "_42Foo", wantLower: "_42Foo"},
		{name: "already prefixed", input: "_123foo", wantUpper: "_123Foo", wantLower: "_123Foo"},
		{name: "letter start unaffected", input: "foo123", wantUpper: "Foo123", wantLower: "foo123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamel(tt.input); got != tt.wantUpper {
				t.Errorf("ToUpperCamel(%q) = %q, want %q", tt.input, got, tt.wantUpper)
			}
			if got := ident.ToLowerCamel(tt.input); got != tt.wantLower {
				t.Errorf("ToLowerCamel(%q) = %q, want %q", tt.input, got, tt.wantLower)
			}
		})
	}
}

// TestCamelTransforms_IdempotentOnOutput tests idempotency of camel transforms.
func TestCamelTransforms_IdempotentOnOutput(t *testing.T) {
	r := rand.New(rand.NewSource(99)) //nolint:gosec // deterministic pseudo-randomness is fine in tests
	alphabet := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_")
	next := func() string {
		n := r.Intn(8) + 1
		var b strings.Builder
		b.Grow(n)
		for range n {
			b.WriteRune(alphabet[r.Intn(len(alphabet))])
		}
		return b.String()
	}

	for range 64 {
		src := next()

		lower1 := ident.ToLowerCamel(src)
		if got := ident.ToLowerCamel(lower1); got != lower1 {
			t.Errorf("ToLowerCamel not idempotent on %q: %q then %q", src, lower1, got)
		}

		upper1 := ident.ToUpperCamel(src)
		if got := ident.ToUpperCamel(upper1); got != upper1 {
			t.Errorf("ToUpperCamel not idempotent on %q: %q then %q", src, upper1, got)
		}

		snake1 := ident.ToLowerSnake(src)
		if got := ident.ToLowerSnake(snake1); got != snake1 {
			t.Errorf("ToLowerSnake not idempotent on %q: %q then %q", src, snake1, got)
		}
	}
}

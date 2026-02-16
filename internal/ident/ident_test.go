package ident_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/ident"
	"github.com/stretchr/testify/assert"
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
			got := ident.ToLowerSnake(tt.input)
			assert.Equal(t, tt.want, got, "ToLowerSnake(%q)", tt.input)
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
			got := ident.ToLowerSnake(tt.input)
			assert.Equal(t, tt.want, got, "ToLowerSnake(%q)", tt.input)
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
			got := ident.ToLowerSnake(tt.input)
			assert.Equal(t, tt.want, got, "ToLowerSnake(%q)", tt.input)
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
			second := ident.ToLowerSnake(first)
			assert.Equal(t, first, second, "ToLowerSnake should be idempotent on %q", input)
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
		second := ident.ToLowerSnake(first)
		assert.Equal(t, first, second, "ToLowerSnake should be idempotent on random input %q", src)
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
			got := ident.Capitalize(tt.input)
			assert.Equal(t, tt.want, got, "Capitalize(%q)", tt.input)
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
			got := ident.ToUpperCamel(tt.input)
			assert.Equal(t, tt.want, got, "ToUpperCamel(%q)", tt.input)
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
			got := ident.ToLowerCamel(tt.input)
			assert.Equal(t, tt.want, got, "ToLowerCamel(%q)", tt.input)
		})
	}
}

// TestCamelTransforms_NumericSegmentsSeparated tests numeric segment handling.
func TestCamelTransforms_NumericSegmentsSeparated(t *testing.T) {
	// Adjacent numeric segments should always be separated by "_" including 0 and 9.
	assert.Equal(t, "foo1_2Bar", ident.ToLowerCamel("foo 1 2 bar"))
	assert.Equal(t, "foo0_0Bar", ident.ToLowerCamel("foo 0 0 bar"))
	assert.Equal(t, "foo9_9Bar", ident.ToLowerCamel("foo 9 9 bar"))

	assert.Equal(t, "Foo1_2Bar", ident.ToUpperCamel("foo 1 2 bar"))
	assert.Equal(t, "Foo0_0Bar", ident.ToUpperCamel("foo 0 0 bar"))
	assert.Equal(t, "Foo9_9Bar", ident.ToUpperCamel("foo 9 9 bar"))
}

// TestCamelTransforms_PreservesAcronymRuns tests acronym preservation.
func TestCamelTransforms_PreservesAcronymRuns(t *testing.T) {
	assert.Equal(t, "httpServer", ident.ToLowerCamel("HTTPServer"))
	assert.Equal(t, "httpServer", ident.ToLowerCamel("HTTP_Server"))
	assert.Equal(t, "IDNumber", ident.ToUpperCamel("ID_number"))
}

// D9: Missing empty string tests for ToUpperCamel and ToLowerCamel
func TestToUpperCamel_EmptyString(t *testing.T) {
	got := ident.ToUpperCamel("")
	assert.Equal(t, "", got, "ToUpperCamel(\"\") should return empty string")
}

func TestToLowerCamel_EmptyString(t *testing.T) {
	got := ident.ToLowerCamel("")
	assert.Equal(t, "", got, "ToLowerCamel(\"\") should return empty string")
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
			gotUpper := ident.ToUpperCamel(tt.input)
			assert.Equal(t, tt.wantUpper, gotUpper, "ToUpperCamel(%q)", tt.input)

			gotLower := ident.ToLowerCamel(tt.input)
			assert.Equal(t, tt.wantLower, gotLower, "ToLowerCamel(%q)", tt.input)
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
		assert.Equal(t, lower1, ident.ToLowerCamel(lower1))

		upper1 := ident.ToUpperCamel(src)
		assert.Equal(t, upper1, ident.ToUpperCamel(upper1))

		snake1 := ident.ToLowerSnake(src)
		assert.Equal(t, snake1, ident.ToLowerSnake(snake1))
	}
}

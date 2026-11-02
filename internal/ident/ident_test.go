package ident_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/ident"
)

func TestToUpperCamelInitialisms_NilSetCapitalizes(t *testing.T) {
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
			if got := ident.ToUpperCamelInitialisms(tt.input, nil); got != tt.want {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToUpperCamelInitialisms_NilSetSplitsSegments states the tokenizer over a nil set.
func TestToUpperCamelInitialisms_NilSetSplitsSegments(t *testing.T) {
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
			if got := ident.ToUpperCamelInitialisms(tt.input, nil); got != tt.want {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.want)
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

// TestCamelTransforms_NumericSegmentsSeparated tests numeric segment handling.
func TestCamelTransforms_NumericSegmentsSeparated(t *testing.T) {
	// Adjacent numeric segments should always be separated by "_" including 0 and 9.

	if got := ident.ToUpperCamelInitialisms("foo 1 2 bar", nil); got != "Foo1_2Bar" {
		t.Errorf("ToUpperCamelInitialisms(\"foo 1 2 bar\") = %q, want \"Foo1_2Bar\"", got)
	}
	if got := ident.ToUpperCamelInitialisms("foo 0 0 bar", nil); got != "Foo0_0Bar" {
		t.Errorf("ToUpperCamelInitialisms(\"foo 0 0 bar\") = %q, want \"Foo0_0Bar\"", got)
	}
	if got := ident.ToUpperCamelInitialisms("foo 9 9 bar", nil); got != "Foo9_9Bar" {
		t.Errorf("ToUpperCamelInitialisms(\"foo 9 9 bar\") = %q, want \"Foo9_9Bar\"", got)
	}
}

// TestCamelTransforms_PreservesAcronymRuns tests acronym preservation.
func TestCamelTransforms_PreservesAcronymRuns(t *testing.T) {
	if got := ident.ToUpperCamelInitialisms("ID_number", nil); got != "IDNumber" {
		t.Errorf("ToUpperCamelInitialisms(\"ID_number\") = %q, want \"IDNumber\"", got)
	}
}

func TestToUpperCamelInitialisms_EmptyString(t *testing.T) {
	if got := ident.ToUpperCamelInitialisms("", nil); got != "" {
		t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", "", got, "")
	}
}

// TestCamelTransforms_LeadingDigits tests that identifiers starting with digits
// get prefixed with underscore to ensure valid Go identifiers.
func TestCamelTransforms_LeadingDigits(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantUpper string
	}{
		{name: "digit prefix", input: "123name", wantUpper: "_123Name"},
		{name: "digit only", input: "123", wantUpper: "_123"},
		{name: "digit underscore word", input: "9_lives", wantUpper: "_9Lives"},
		{name: "leading zero", input: "0value", wantUpper: "_0Value"},
		{name: "multiple leading digits", input: "42foo", wantUpper: "_42Foo"},
		{name: "already prefixed", input: "_123foo", wantUpper: "_123Foo"},
		{name: "letter start unaffected", input: "foo123", wantUpper: "Foo123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamelInitialisms(tt.input, nil); got != tt.wantUpper {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.wantUpper)
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

		upper1 := ident.ToUpperCamelInitialisms(src, nil)
		if got := ident.ToUpperCamelInitialisms(upper1, nil); got != upper1 {
			t.Errorf("ToUpperCamelInitialisms not idempotent on %q: %q then %q", src, upper1, got)
		}
	}
}

// TestToUpperCamelInitialisms_SplitsAtCaseBreaks holds the two case boundaries
// through what they decide: which segment an initialism matches.
func TestToUpperCamelInitialisms_SplitsAtCaseBreaks(t *testing.T) {
	inits := map[string]bool{"id": true, "server": true}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lower to upper", input: "userId", want: "UserID"},
		{name: "an acronym run then a word", input: "URLId", want: "URLID"},
		{name: "a run keeps its last capital for the word", input: "someHTTPServer", want: "SomeHTTPSERVER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamelInitialisms(tt.input, inits); got != tt.want {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestToUpperCamelInitialisms_KeepsLettersWithNoCase holds that a letter with no
// case, or in title case, stays in the identifier and is no separator.
func TestToUpperCamelInitialisms_KeepsLettersWithNoCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "no case", input: "日本", want: "日本"},
		{name: "title case", input: "ǅa", want: "Ǆa"},
		{name: "no case after a word", input: "x日本", want: "X日本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ident.ToUpperCamelInitialisms(tt.input, nil); got != tt.want {
				t.Errorf("ToUpperCamelInitialisms(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

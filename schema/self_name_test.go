package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// self is bound to the instance in every invariant, at load and at
// evaluation alike, so neither the bare name nor $self reaches a property named
// self. The completer refuses it.
func TestPropertyNamedSelf_RefusedAtLoad(t *testing.T) {
	t.Parallel()
	const src = `schema "s"

type T {
    id String primary
    self String
}
`
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.Err() == nil {
		t.Fatal("a property named self loaded clean")
	}
	if !s.IsZero() {
		t.Error("a schema was produced beside the refusal")
	}
	is, ok := issueWithCode(res, diag.E_INVALID_NAME)
	if !ok {
		t.Fatalf("want E_INVALID_NAME; got %v", res.Err())
	}
	if !strings.Contains(is.Message(), "self") {
		t.Errorf("the message does not name self: %q", is.Message())
	}
	if is.Span().IsZero() {
		t.Error("the diagnostic carries no span")
	}
}

// Refused once, where it is declared: a subtype inherits the refusal, not a
// second diagnostic.
func TestPropertyNamedSelf_RefusedOnceWhereDeclared(t *testing.T) {
	t.Parallel()
	const src = `schema "s"

abstract type Base {
    id String primary
    self String
}

type A extends Base {
    x Integer
}

type B extends Base {
    y Integer
}
`
	_, res := schema.LoadString(t.Context(), src, "s.yammm")
	n := 0
	for is := range res.Issues() {
		if is.Code() == diag.E_INVALID_NAME {
			n++
		}
	}
	if n != 1 {
		t.Errorf("one declaration drew %d E_INVALID_NAME diagnostics, want 1: %v", n, res.Err())
	}
}

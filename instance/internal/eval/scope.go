package eval

import (
	"maps"
	"strings"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/expr"
)

// Scope provides variable bindings for expression evaluation.
//
// Scope is immutable; methods like WithVar return a new Scope with
// the additional binding. This enables safe concurrent evaluation
// and composable scope construction.
//
// # Scope Composition
//
// Scopes can be composed by chaining WithVar calls. There is no MergeScopes
// function because WithVar chaining is explicit about binding order and
// shadowing behavior:
//
//	// Create base scope
//	base := eval.PropertyScopeOf(immutable.WrapMap(props))
//
//	// Add variables from another source
//	combined := base.WithVar("x", 1).WithVar("y", 2)
//
//	// Merge bindings from a map
//	for name, val := range additionalBindings {
//	    combined = combined.WithVar(name, val)
//	}
//
// Later bindings shadow earlier ones with the same name.
type Scope interface {
	// Lookup returns the value bound to name, or (zero, false) if not found.
	// For property scopes, this looks up property values by name.
	// For variable scopes, this looks up bound variables.
	Lookup(name string) (immutable.Value, bool)

	// LookupFold returns the value a bare name reads: a variable of exactly
	// that name, then a property matched case-insensitively or a relation by
	// its name. Returns (zero, false) if not found.
	LookupFold(name string) (immutable.Value, bool)

	// WithVar returns a new Scope with the additional variable binding.
	// If the name is already bound, the new binding shadows the old one.
	WithVar(name string, value any) Scope
}

// PropertyScopeOf returns a Scope over a map that is already wrapped, with
// $self bound to it. Nothing is copied: the caller holds the map as a memo
// and hands the same one to every evaluation that reads it.
func PropertyScopeOf(m immutable.Map[string]) Scope {
	return &propertyScope{
		props: immutable.PropertiesOf(m),
		vars:  map[string]immutable.Value{expr.SelfVariable: immutable.Wrap(m)},
	}
}

// propertyScope is a scope backed by immutable.Properties.
type propertyScope struct {
	props immutable.Properties
	vars  map[string]immutable.Value
}

// Lookup resolves a variable's name: a variable of exactly that name, then a
// member by its declared name.
func (s *propertyScope) Lookup(name string) (immutable.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	return s.props.Get(name)
}

// LookupFold resolves a bare name the way docs/SPEC.md's scope chain states:
// a variable of exactly that name first, then a member by [readMember].
// Variable names never fold.
func (s *propertyScope) LookupFold(name string) (immutable.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	return readMember(s.props, name)
}

// readMember reads name from an instance's entries: a property by its name in
// any ASCII casing, a relation by its UPPER_SNAKE name alone. The entries key a
// relation by its name, and a property name starts lower case, so name
// upper-cased finds a relation's key and no property's; a read of it in another
// casing reads nothing, and the fold that follows reaches properties alone.
func readMember(props immutable.Properties, name string) (immutable.Value, bool) {
	if v, ok := props.Get(name); ok {
		return v, true
	}
	if key := toUpperASCII(name); key != name {
		if _, ok := props.Get(key); ok {
			return immutable.Value{}, false
		}
	}
	return props.GetFold(name)
}

// toUpperASCII maps the ASCII letters a-z to A-Z and leaves every other rune,
// as GetFold's ASCII-only fold does.
func toUpperASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

func (s *propertyScope) WithVar(name string, value any) Scope {
	newVars := make(map[string]immutable.Value, len(s.vars)+1)
	maps.Copy(newVars, s.vars)
	newVars[name] = immutable.Wrap(value)
	return &propertyScope{
		props: s.props,
		vars:  newVars,
	}
}

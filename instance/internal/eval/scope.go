package eval

import (
	"maps"
	"strings"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema/expr"
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
//	base := eval.PropertyScopeFromMap(props)
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
	// its name or its field name. Returns (zero, false) if not found.
	LookupFold(name string) (immutable.Value, bool)

	// WithVar returns a new Scope with the additional variable binding.
	// If the name is already bound, the new binding shadows the old one.
	WithVar(name string, value any) Scope
}

// EmptyScope returns an empty Scope with no bindings.
func EmptyScope() Scope {
	return &mapScope{
		vars: make(map[string]immutable.Value),
	}
}

// PropertyScopeFromMap returns a Scope backed by a raw property map, with
// $self bound to it. The map is wrapped ONCE, with WithClone so the caller's
// map is isolated: the property lookup reads the wrap as a Properties and
// $self reads it as a Map, sharing the entries. A second wrap for $self
// doubled every instance's scope-building allocations.
func PropertyScopeFromMap(props map[string]any) Scope {
	m := immutable.WrapMap(props, immutable.WithClone(true))
	return &propertyScope{
		props: immutable.PropertiesOf(m),
		vars:  map[string]immutable.Value{expr.SelfVariable: immutable.Wrap(m)},
	}
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

// mapScope is a simple variable-only scope.
type mapScope struct {
	vars map[string]immutable.Value
}

func (s *mapScope) Lookup(name string) (immutable.Value, bool) {
	v, ok := s.vars[name]
	return v, ok
}

// LookupFold on a variable-only scope is an exact lookup: variable names
// never fold, whichever spelling reaches them.
func (s *mapScope) LookupFold(name string) (immutable.Value, bool) {
	return s.Lookup(name)
}

func (s *mapScope) WithVar(name string, value any) Scope {
	newVars := make(map[string]immutable.Value, len(s.vars)+1)
	maps.Copy(newVars, s.vars)
	newVars[name] = immutable.Wrap(value)
	return &mapScope{vars: newVars}
}

// propertyScope is a scope backed by immutable.Properties.
type propertyScope struct {
	props immutable.Properties
	vars  map[string]immutable.Value
}

// Lookup resolves a variable's name: a variable of exactly that name, then a
// member spelled exactly, which reads a relation by its name, then a relation
// by its field name.
func (s *propertyScope) Lookup(name string) (immutable.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	if v, ok := s.props.Get(name); ok {
		return v, true
	}
	if key := toUpperASCII(name); key != name && name == toLowerASCII(key) {
		return s.props.Get(key)
	}
	return immutable.Value{}, false
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
// any ASCII casing, a relation by its UPPER_SNAKE name or its field name alone.
// The entries key a relation by its name, and a property name starts lower
// case, so name upper-cased finds a relation's key and no property's; a read
// of it in another casing reads nothing, and the fold that follows reaches
// properties alone.
func readMember(props immutable.Properties, name string) (immutable.Value, bool) {
	if v, ok := props.Get(name); ok {
		return v, true
	}
	if key := toUpperASCII(name); key != name {
		if v, ok := props.Get(key); ok {
			if name == toLowerASCII(key) {
				return v, true
			}
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

// toLowerASCII maps the ASCII letters A-Z to a-z.
func toLowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r - 'A' + 'a'
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

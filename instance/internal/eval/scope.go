package eval

import (
	"maps"

	"github.com/simon-lentz/yammm/immutable"
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

	// LookupFold returns the value bound to name using case-insensitive matching.
	// Returns (zero, false) if not found.
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
		vars:  map[string]immutable.Value{"self": immutable.Wrap(m)},
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

func (s *propertyScope) Lookup(name string) (immutable.Value, bool) {
	// Variables take precedence over properties
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	// Fall back to properties
	return s.props.Get(name)
}

// LookupFold resolves a bare name the way docs/SPEC.md's scope chain states:
// a variable of exactly that name first, then a property matched
// case-insensitively. Variable names never fold; property names always do.
func (s *propertyScope) LookupFold(name string) (immutable.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	return s.props.GetFold(name)
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

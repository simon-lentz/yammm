package eval

import (
	"maps"
	"strings"

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

	// WithSelf returns a new Scope with $self bound to the given value.
	// This is syntactic sugar for WithVar("self", value).
	WithSelf(self any) Scope
}

// EmptyScope returns an empty Scope with no bindings.
func EmptyScope() Scope {
	return &mapScope{
		vars: make(map[string]immutable.Value),
	}
}

// PropertyScope returns a Scope backed by the given immutable.Properties.
// Property lookups use the Properties' Get method.
func PropertyScope(props immutable.Properties) Scope {
	return &propertyScope{
		props: props,
		vars:  make(map[string]immutable.Value),
	}
}

// PropertyScopeFromMap returns a Scope backed by a raw property map.
// The map is wrapped using immutable.WrapPropertiesClone to ensure isolation.
func PropertyScopeFromMap(props map[string]any) Scope {
	return &propertyScope{
		props: immutable.WrapPropertiesClone(props),
		vars:  make(map[string]immutable.Value),
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

func (s *mapScope) LookupFold(name string) (immutable.Value, bool) {
	// First try exact match
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	// Fall back to case-insensitive search: pick alphabetically first key on collision
	// for deterministic behavior (matches immutable.Properties.GetFold behavior)
	lower := strings.ToLower(name)
	var matchKey string
	var matchVal immutable.Value
	for k, v := range s.vars {
		if strings.ToLower(k) == lower {
			if matchKey == "" || k < matchKey {
				matchKey = k
				matchVal = v
			}
		}
	}
	if matchKey != "" {
		return matchVal, true
	}
	return immutable.Value{}, false
}

func (s *mapScope) WithVar(name string, value any) Scope {
	newVars := make(map[string]immutable.Value, len(s.vars)+1)
	maps.Copy(newVars, s.vars)
	newVars[name] = immutable.Wrap(value)
	return &mapScope{vars: newVars}
}

func (s *mapScope) WithSelf(self any) Scope {
	return s.WithVar("self", self)
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

func (s *propertyScope) LookupFold(name string) (immutable.Value, bool) {
	// Variables take precedence (exact match first)
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	// Try case-insensitive variable search: pick alphabetically first key on collision
	// for deterministic behavior (matches immutable.Properties.GetFold behavior)
	lower := strings.ToLower(name)
	var matchKey string
	var matchVal immutable.Value
	for k, v := range s.vars {
		if strings.ToLower(k) == lower {
			if matchKey == "" || k < matchKey {
				matchKey = k
				matchVal = v
			}
		}
	}
	if matchKey != "" {
		return matchVal, true
	}
	// Fall back to properties (using GetFold for case-insensitive)
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

func (s *propertyScope) WithSelf(self any) Scope {
	return s.WithVar("self", self)
}

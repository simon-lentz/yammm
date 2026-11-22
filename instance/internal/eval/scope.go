package eval

import (
	"maps"

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
//	base := eval.PropertyScopeOf(immutable.WrapProperties(props))
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
	// Lookup returns the value name reads: a variable of exactly that name,
	// then a member by its declared name. Returns (zero, false) if not found.
	Lookup(name string) (immutable.Value, bool)

	// WithVar returns a new Scope with the additional variable binding.
	// If the name is already bound, the new binding shadows the old one.
	WithVar(name string, value any) Scope
}

// PropertyScopeOf returns a Scope over a map that is already wrapped, with
// $self bound to it. Nothing is copied: the caller holds the map as a memo
// and hands the same one to every evaluation that reads it.
func PropertyScopeOf(m immutable.Properties) Scope {
	return &propertyScope{
		props: m,
		vars:  map[string]immutable.Value{expr.SelfVariable: immutable.Wrap(m)},
	}
}

// propertyScope is a scope backed by immutable.Properties.
type propertyScope struct {
	props immutable.Properties
	vars  map[string]immutable.Value
}

// Lookup resolves a bare name or a $ variable: a variable of exactly that name,
// then a member by its declared name.
func (s *propertyScope) Lookup(name string) (immutable.Value, bool) {
	if v, ok := s.vars[name]; ok {
		return v, true
	}
	return s.props.Get(name)
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

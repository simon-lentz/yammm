package eval

import (
	"maps"

	"github.com/simon-lentz/yammm/immutable"
)

// EmptyScope returns an empty Scope with no bindings.
func EmptyScope() Scope {
	return &mapScope{
		vars: make(map[string]immutable.Value),
	}
}

// PropertyScopeFromMap returns a Scope backed by a raw property map, with $self
// bound to it. The wrap copies the caller's map.
func PropertyScopeFromMap(props map[string]any) Scope {
	return PropertyScopeOf(immutable.WrapProperties(props))
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

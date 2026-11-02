package expr

import (
	"slices"
	"strings"
)

// Builtins returns every builtin spec, ordered by name.
func Builtins() []BuiltinSpec {
	out := make([]BuiltinSpec, 0, len(builtinSpecs))
	for _, s := range builtinSpecs {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b BuiltinSpec) int { return strings.Compare(a.Name, b.Name) })
	return out
}

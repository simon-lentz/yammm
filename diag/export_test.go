package diag

import (
	"maps"
	"slices"
	"strings"
)

// AllCodes returns every registered code, sorted by its identifier.
func AllCodes() []Code {
	mu.Lock()
	defer mu.Unlock()
	return slices.SortedFunc(maps.Values(registry), func(a, b Code) int { return strings.Compare(a.value, b.value) })
}

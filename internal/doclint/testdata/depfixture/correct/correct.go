package correct

import (
	"strings"

	_ "depfixture/leaf"
)

// Trim is here so the standard-library import is used.
func Trim(s string) string { return strings.TrimSpace(s) }

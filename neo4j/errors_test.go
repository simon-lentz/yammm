package neo4j

import (
	"strings"
	"testing"
)

// Each sentinel the package exports names it in its text.
func TestErrors_NameThePackage(t *testing.T) {
	t.Parallel()

	for _, err := range []error{ErrUnsupportedListElem, ErrUnrepresentable, ErrZeroSnapshot} {
		if !strings.HasPrefix(err.Error(), "neo4j: ") {
			t.Errorf("%q does not name package neo4j", err)
		}
	}
}

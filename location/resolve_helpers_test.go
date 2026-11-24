package location

import (
	"path/filepath"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// diskSpelling is [yammmtest.DiskSpelling], which reads directory listings and
// shares no code with the resolver, failing the test on error.
func diskSpelling(t *testing.T, p string) string {
	t.Helper()
	spelled, err := yammmtest.DiskSpelling(p)
	if err != nil {
		t.Fatal(err)
	}
	return spelled
}

// identityForm writes a host path in the form an identity takes.
func identityForm(host string) string {
	return norm.NFC.String(filepath.ToSlash(host))
}

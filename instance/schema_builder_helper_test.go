package instance_test

import (
	"github.com/simon-lentz/yammm/instance"
)

// helperTripBogusProperty lives in this separate file so that the
// cross-file caller-locator test in schema_builder_test.go can assert the
// error points at THIS file, not the test-body file. The call to
// Property below is the offending frame runtime.Callers should resolve.
func helperTripBogusProperty(b *instance.SchemaBuilder) {
	b.Property("bogus_helper_field", 1)
}

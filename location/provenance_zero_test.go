package location_test

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestProvenance_TheZeroHandleAnswersAndWithRawPathMakesARecord(t *testing.T) {
	var zero location.Provenance
	if !zero.IsZero() {
		t.Error("the zero Provenance must report zero")
	}
	if got := zero.SourceName(); got != "" {
		t.Errorf("SourceName = %q, want empty", got)
	}
	if zero.HasRawPath() {
		t.Error("the zero Provenance must hold no raw path")
	}

	withPath := zero.WithRawPath("$.a")
	if withPath.IsZero() || !withPath.HasRawPath() {
		t.Errorf("WithRawPath: IsZero = %v, HasRawPath = %v, want a record with a path", withPath.IsZero(), withPath.HasRawPath())
	}
	if got := withPath.RawPath(); got != "$.a" {
		t.Errorf("RawPath = %q, want $.a", got)
	}
	if !zero.IsZero() {
		t.Error("WithRawPath must not write through the receiver")
	}
}

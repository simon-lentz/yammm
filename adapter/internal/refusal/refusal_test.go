package refusal_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/adapter/internal/refusal"
)

var (
	errClass = errors.New("class text that must not reach a message")
	errOther = errors.New("another class")
)

// The message is the site's text and nothing else, because the adapters ship
// message texts that documents quote.
func TestNew_MessageIsTheFormattedTextAlone(t *testing.T) {
	t.Parallel()
	err := refusal.New(errClass, "column %q: holds %d", "tags", 2)
	if got, want := err.Error(), `column "tags": holds 2`; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// The class is found directly and through a caller's own wrap, and no other
// class is.
func TestNew_ClassIsMatchedThroughWraps(t *testing.T) {
	t.Parallel()
	err := refusal.New(errClass, "refused")
	wrapped := fmt.Errorf("type %q: %w", "Order", err)

	for _, e := range []error{err, wrapped} {
		if !errors.Is(e, errClass) {
			t.Errorf("%v does not match its class", e)
		}
		if errors.Is(e, errOther) {
			t.Errorf("%v matches a class it was not given", e)
		}
	}
	if !errors.Is(wrapped, err) {
		t.Error("a wrapped refusal does not match the refusal itself")
	}
}

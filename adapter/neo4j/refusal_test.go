package neo4j

import (
	"errors"
	"fmt"
	"testing"
)

var errOther = errors.New("another class")

// The message is the site's text and nothing else, because the adapter ships
// message texts that documents quote.
func TestUnrepresentable_MessageIsTheFormattedTextAlone(t *testing.T) {
	t.Parallel()
	err := unrepresentable("column %q: holds %d", "tags", 2)
	if got, want := err.Error(), `column "tags": holds 2`; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// The class is found directly and through a caller's own wrap, and no other
// class is.
func TestUnrepresentable_ClassIsMatchedThroughWraps(t *testing.T) {
	t.Parallel()
	err := unrepresentable("refused")
	wrapped := fmt.Errorf("type %q: %w", "Order", err)
	for _, e := range []error{err, wrapped} {
		if !errors.Is(e, ErrUnrepresentable) {
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

func TestMarkUnrepresentable_KeepsTheMessageAndEveryClass(t *testing.T) {
	t.Parallel()
	inner := fmt.Errorf("property %q: %w", "m", errOther)
	err := markUnrepresentable(inner)
	if got, want := err.Error(), `property "m": another class`; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("type %q: %w", "Grid", err)
	for _, class := range []error{ErrUnrepresentable, errOther, inner} {
		if !errors.Is(wrapped, class) {
			t.Errorf("%v does not match %v", wrapped, class)
		}
	}
	if errors.Is(wrapped, errors.New(ErrUnrepresentable.Error())) {
		t.Errorf("%v matches a class it was not given", wrapped)
	}
}

func TestMarkUnrepresentable_NilAndAMarkedErrorAreReturnedAsTheyAre(t *testing.T) {
	t.Parallel()
	if err := markUnrepresentable(nil); err != nil {
		t.Errorf("markUnrepresentable(nil) = %v, want nil", err)
	}
	for _, err := range []error{
		unrepresentable("refused"),
		fmt.Errorf("type %q: %w", "Grid", markUnrepresentable(errOther)),
	} {
		if got := markUnrepresentable(err); got != err { //nolint:errorlint // the error itself must come back, not a wrapper of it
			t.Errorf("markUnrepresentable(%v) = %v, want the error itself", err, got)
		}
	}
}

// A marked refusal wraps the error it marks and its class, in that order, and a
// walk of its chain allocates nothing, errors.AsType's included.
func TestMarkUnrepresentable_WrapsTheMarkedErrorAndItsClassWithoutAllocating(t *testing.T) {
	inner := fmt.Errorf("property %q: %w", "m", errOther)
	err := markUnrepresentable(inner)
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("%T wraps no list of errors", err)
	}
	if got := multi.Unwrap(); len(got) != 2 || got[0] != inner || got[1] != ErrUnrepresentable { //nolint:errorlint // Unwrap must return the errors themselves
		t.Errorf("Unwrap = %v; want the marked error, then the class", got)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if !errors.Is(err, errOther) || !errors.Is(err, ErrUnrepresentable) {
			t.Fatal("a class is lost")
		}
		if _, ok := errors.AsType[*unrepresentableError](err); ok {
			t.Fatal("errors.AsType found a type the chain does not hold")
		}
	}); allocs != 0 {
		t.Errorf("a walk allocates %v; want 0", allocs)
	}
}

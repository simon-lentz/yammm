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

func TestMark_KeepsTheMessageAndEveryClass(t *testing.T) {
	t.Parallel()
	inner := fmt.Errorf("property %q: %w", "m", errOther)
	err := refusal.Mark(errClass, inner)
	if got, want := err.Error(), `property "m": another class`; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("type %q: %w", "Grid", err)
	for _, class := range []error{errClass, errOther, inner} {
		if !errors.Is(wrapped, class) {
			t.Errorf("%v does not match %v", wrapped, class)
		}
	}
	if errors.Is(wrapped, errors.New(errClass.Error())) {
		t.Errorf("%v matches a class it was not given", wrapped)
	}
}

func TestMark_NilAndAMarkedErrorAreReturnedAsTheyAre(t *testing.T) {
	t.Parallel()
	if err := refusal.Mark(errClass, nil); err != nil {
		t.Errorf("Mark(class, nil) = %v, want nil", err)
	}
	for _, err := range []error{
		refusal.New(errClass, "refused"),
		fmt.Errorf("type %q: %w", "Grid", refusal.Mark(errClass, errOther)),
	} {
		if got := refusal.Mark(errClass, err); got != err { //nolint:errorlint // Mark must return the error itself, not a wrapper of it
			t.Errorf("Mark(class, %v) = %v, want the error itself", err, got)
		}
	}
}

// A marked refusal wraps the error it marks and its class, in that order, and a
// walk of its chain allocates nothing, errors.AsType's included.
func TestMark_WrapsTheMarkedErrorAndItsClassWithoutAllocating(t *testing.T) {
	inner := fmt.Errorf("property %q: %w", "m", errOther)
	err := refusal.Mark(errClass, inner)
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("%T wraps no list of errors", err)
	}
	if got := multi.Unwrap(); len(got) != 2 || got[0] != inner || got[1] != errClass { //nolint:errorlint // Unwrap must return the errors themselves
		t.Errorf("Unwrap = %v; want the marked error, then the class", got)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if !errors.Is(err, errOther) || !errors.Is(err, errClass) {
			t.Fatal("a class is lost")
		}
		if _, ok := errors.AsType[*classError](err); ok {
			t.Fatal("errors.AsType found a type the chain does not hold")
		}
	}); allocs != 0 {
		t.Errorf("a walk allocates %v; want 0", allocs)
	}
}

// uncomparable is an error whose dynamic type == cannot compare.
type uncomparable struct{ s []int }

func (uncomparable) Error() string { return "uncomparable" }

// A class of an uncomparable type matches no target, as errors.Is answers
// such a target, and draws no panic.
func TestMark_AnUncomparableClassMatchesNothing(t *testing.T) {
	t.Parallel()
	err := refusal.Mark(uncomparable{s: []int{1}}, errOther)
	if errors.Is(err, uncomparable{s: []int{2}}) {
		t.Error("a marked refusal matches an uncomparable target")
	}
	if !errors.Is(err, errOther) {
		t.Error("a marked refusal lost the error it marks")
	}
}

// classError is a class of a concrete type a caller reads with errors.As.
type classError struct{ name string }

func (e *classError) Error() string { return e.name }

// errors.As finds a marked refusal's class, as it finds a refusal.New one's.
func TestMark_AsFindsTheClass(t *testing.T) {
	t.Parallel()
	class := &classError{name: "class"}
	for name, err := range map[string]error{
		"Mark": fmt.Errorf("type %q: %w", "Grid", refusal.Mark(class, errOther)),
		"New":  fmt.Errorf("type %q: %w", "Grid", refusal.New(class, "refused")),
	} {
		var got *classError
		if !errors.As(err, &got) || got != class {
			t.Errorf("%s: errors.As = %v; want the class", name, got)
		}
	}
}

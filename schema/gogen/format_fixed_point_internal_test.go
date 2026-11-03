package gogen

import (
	"errors"
	"strings"
	"testing"
)

// TestFormatToFixedPoint_FormatsUntilATextComesBackUnchanged pins the loop by
// the calls it makes, with formatters that settle after a known number of
// passes: one call more than the passes that change the text, the text of the
// last pass returned, and an error at the bound for one that never settles.
func TestFormatToFixedPoint_FormatsUntilATextComesBackUnchanged(t *testing.T) {
	t.Parallel()
	// settlesAfter returns a formatter that appends a byte on each of its
	// first n calls and then writes its input back, and its call count.
	settlesAfter := func(n int) (func([]byte) ([]byte, error), *int) {
		calls := 0
		return func(src []byte) ([]byte, error) {
			calls++
			if calls <= n {
				return append(append([]byte(nil), src...), '.'), nil
			}
			return append([]byte(nil), src...), nil
		}, &calls
	}
	for _, changes := range []int{0, 1, 2, maxFormatPasses - 1} {
		gofmt, calls := settlesAfter(changes)
		got, err := formatToFixedPoint([]byte("x"), gofmt)
		if err != nil {
			t.Fatalf("%d changing passes: %v", changes, err)
		}
		if want := "x" + strings.Repeat(".", changes); string(got) != want {
			t.Errorf("%d changing passes: the text is %q, want %q", changes, got, want)
		}
		if *calls != changes+1 {
			t.Errorf("%d changing passes: %d calls, want %d", changes, *calls, changes+1)
		}
	}

	gofmt, calls := settlesAfter(maxFormatPasses)
	if got, err := formatToFixedPoint([]byte("x"), gofmt); err == nil || got != nil ||
		!strings.Contains(err.Error(), "gofmt still rewrites the generated source") {
		t.Errorf("a formatter that changes the text on every pass: %q, %v; want no text and the bound's error", got, err)
	}
	if *calls != maxFormatPasses {
		t.Errorf("a formatter that changes the text on every pass: %d calls, want %d", *calls, maxFormatPasses)
	}

	failure := errors.New("no parse")
	if got, err := formatToFixedPoint([]byte("x"), func([]byte) ([]byte, error) { return nil, failure }); got != nil || !errors.Is(err, failure) ||
		!strings.HasPrefix(err.Error(), "gogen: generated source did not parse: ") {
		t.Errorf("a formatter that fails: %q, %v; want no text and its error", got, err)
	}
}

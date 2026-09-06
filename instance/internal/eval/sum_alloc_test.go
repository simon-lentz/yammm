package eval

import "testing"

// T5 (A-304): Sum classifies in two passes over the slice, not into two
// slices, so nothing it allocates grows with its input — the boxed result is
// the only allocation, and a small integer sum has none — while P3's rule
// holds: a list holding a float is float arithmetic, and the integer subtotal
// it would discard cannot overflow it.
func TestSum_AllocatesNothingPerElement(t *testing.T) {
	big := make([]any, 300)
	mixedBig := make([]any, 300)
	for i := range big {
		big[i] = int64(0) // a small sum boxes without allocating, so only a per-element cost could show
		mixedBig[i] = float64(i) + 0.5
	}
	// Boxed once here: converting a slice to any is the caller's allocation.
	var small, large, mixedSmall, mixedLarge any = []any{int64(1), int64(2), int64(3)}, big, []any{int64(1), 2.5, int64(3)}, mixedBig
	allocs := func(in any) float64 {
		return testing.AllocsPerRun(100, func() {
			if _, err := builtinSum(nil, in, nil, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	if a := allocs(small); a != 0 {
		t.Errorf("Sum over three small integers allocates %v, want 0", a)
	}
	if a, b := allocs(small), allocs(large); b != a {
		t.Errorf("Sum over 300 integers allocates %v against %v for three: an allocation grows with the input", b, a)
	}
	if a, b := allocs(mixedSmall), allocs(mixedLarge); b != a {
		t.Errorf("Sum over 300 floats allocates %v against %v for three: an allocation grows with the input", b, a)
	}
	if got, err := builtinSum(nil, mixedSmall, nil, nil, nil, nil); err != nil || got != 6.5 {
		t.Errorf("mixed sum = %v, %v; want 6.5", got, err)
	}
	huge := []any{int64(1 << 62), int64(1 << 62), 0.5}
	if got, err := builtinSum(nil, huge, nil, nil, nil, nil); err != nil {
		t.Errorf("a float list whose integer subtotal would overflow errored: %v", err)
	} else if got != float64(1<<62)*2+0.5 {
		t.Errorf("huge mixed sum = %v", got)
	}
	if _, err := builtinSum(nil, []any{int64(1 << 62), int64(1 << 62)}, nil, nil, nil, nil); err == nil {
		t.Error("an integer overflow summed silently")
	}
}

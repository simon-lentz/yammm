package value

import (
	"encoding/json"
	"math"
	"testing"
)

// TestWrittenNumber_MatchesEncodingJSON holds the text the stored-value order
// ranks to the text encoding/json writes for every carrier the snapshot writer
// passes to it, float32 at each magnitude included.
func TestWrittenNumber_MatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	type named float64
	vals := []any{
		int8(-8), int16(300), int32(-70000), int(1), uint8(255), uint16(1), uint32(7), uint(9), uintptr(3),
		int64(math.MinInt64), uint64(math.MaxUint64), named(1), named(1e21), named(1e-7),
	}
	for e := -45; e <= 38; e++ {
		p := float32(math.Pow(10, float64(e)))
		vals = append(vals, p, -p, p*3, p/7)
	}
	for _, v := range vals {
		if f, ok := v.(float32); ok && math.IsInf(float64(f), 0) {
			continue
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal(%#v): %v", v, err)
		}
		if got, ok := writtenNumber(v); !ok || got != string(want) {
			t.Errorf("writtenNumber(%#v) = %q, %v; want %q", v, got, ok, want)
		}
	}
	for _, v := range []any{math.NaN(), math.Inf(1), float32(math.Inf(-1))} {
		if got, ok := writtenNumber(v); ok {
			t.Errorf("writtenNumber(%v) = %q, true; want false, no snapshot writes it", v, got)
		}
	}
}

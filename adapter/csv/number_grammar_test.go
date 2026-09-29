package csv

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
)

// numberLiteral accepts exactly what encoding/json decodes as a number with
// nothing around it, over every string of up to six bytes the generator draws
// from the bytes a number literal and its near misses hold.
func TestNumberLiteral_AcceptsWhatTheDecoderReadsAsANumber(t *testing.T) {
	t.Parallel()
	const alphabet = "-+0123456789.eE x"
	rng := rand.New(rand.NewPCG(8259, 6)) //nolint:gosec // a fixed seed makes the class reproducible
	accepted := 0
	for range 200000 {
		var b strings.Builder
		for range 1 + rng.IntN(6) {
			b.WriteByte(alphabet[rng.IntN(len(alphabet))])
		}
		s := b.String()
		var n json.Number
		want := json.Unmarshal([]byte(s), &n) == nil && strings.TrimSpace(s) == s
		if got := numberLiteral(s); got != want {
			t.Errorf("numberLiteral(%q) = %v, want %v", s, got, want)
		}
		if want {
			accepted++
		}
	}
	if accepted < 1000 {
		t.Errorf("only %d literals drawn; the class does not reach the grammar", accepted)
	}
}

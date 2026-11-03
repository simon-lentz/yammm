package jschema

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// readDefsKey reads an exact $defs key back from the right, where only
// identifiers stand: a trailing "datatype" or "edge" names the kind.
func readDefsKey(key string) (kind string, parts []string, ok bool) {
	segs := strings.Split(key, ".")
	switch last := segs[len(segs)-1]; {
	case last == "datatype" && len(segs) >= 3:
		return "datatype", []string{strings.Join(segs[:len(segs)-2], "."), segs[len(segs)-2]}, true
	case last == "edge" && len(segs) >= 4:
		n := len(segs)
		return "edge", []string{strings.Join(segs[:n-3], "."), segs[n-3], segs[n-2]}, true
	case len(segs) >= 2:
		return "type", []string{strings.Join(segs[:len(segs)-1], "."), last}, true
	}
	return "", nil, false
}

// TestDefsKeys_AnExactKeyReadsBackToOneIdentity pins what the rule rests on:
// an exact key holds a "." no bare key holds, and reads back to one kind and
// one identity whatever its schema name holds, so no two entities share one.
func TestDefsKeys_AnExactKeyReadsBackToOneIdentity(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // a fixed seed makes the keys reproducible
	names := []string{"Region", "A_b", "EDGE_X", "B_X", "Datatype", "Edge"}
	pool := []rune("ab.X_%/ é")
	for range 5000 {
		var sb strings.Builder
		for range r.IntN(6) {
			sb.WriteRune(pool[r.IntN(len(pool))])
		}
		sc, n1, n2 := sb.String(), names[r.IntN(len(names))], names[r.IntN(len(names))]
		for _, tc := range []struct {
			key, kind string
			parts     []string
		}{
			{typeExactKey(sc, n1), "type", []string{sc, n1}},
			{dataTypeExactKey(sc, n1), "datatype", []string{sc, n1}},
			{edgeExactKey(sc, n1, strings.ToUpper(n2)), "edge", []string{sc, n1, strings.ToUpper(n2)}},
		} {
			kind, parts, ok := readDefsKey(tc.key)
			if !ok || kind != tc.kind || !slices.Equal(parts, tc.parts) {
				t.Fatalf("%q reads back as %s %q, want %s %q", tc.key, kind, parts, tc.kind, tc.parts)
			}
		}
		for _, bare := range []string{n1, "EDGE_" + n1 + "_" + strings.ToLower(n2) + "_" + n2} {
			if strings.Contains(bare, ".") {
				t.Fatalf("the bare key %q holds a dot", bare)
			}
		}
	}
}

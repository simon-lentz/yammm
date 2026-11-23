package schema_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// extendsChain is a schema whose types extend one another n deep.
func extendsChain(n int) string {
	var b strings.Builder
	b.WriteString("schema \"s\"\nabstract type T0 {\n\tid String primary\n\t! \"m\" id != \"\"\n}\n")
	for i := range n {
		fmt.Fprintf(&b, "abstract type T%d extends T%d {\n}\n", i+1, i)
	}
	fmt.Fprintf(&b, "type Leaf extends T%d {\n}\n", n)
	return b.String()
}

// TestLoad_ADeepExtendsChainCostsItsAncestry pins that a load allocates in
// proportion to the ancestry it records: each type holds its ancestors, so a
// chain n deep holds about n²/2 of them, and four times the depth may cost at
// most twice sixteen times the bytes. Rebuilding each supertype's subtypes at
// every subtype costs the cube. It runs alone, since it reads the process's
// allocation count.
func TestLoad_ADeepExtendsChainCostsItsAncestry(t *testing.T) {
	allocated := func(n int) uint64 {
		src := extendsChain(n)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		s, err := schema.LoadString(t.Context(), src, "s.yammm")
		runtime.ReadMemStats(&after)
		if s.IsZero() {
			t.Fatalf("depth %d: %v", n, err)
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	const depth, factor = 150, 4
	small, large := allocated(depth), allocated(depth*factor)
	t.Logf("depth %d allocates %d bytes; depth %d allocates %d", depth, small, depth*factor, large)
	if large > 2*factor*factor*small {
		t.Errorf("the depth grew %d times and the bytes %.1f times", factor, float64(large)/float64(small))
	}
}

package schema_test

import (
	"context"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Loading is safe from many goroutines at once. The parser values are built
// once and shared by every call, where the path they replaced built its own
// instances per parse; each call still gets its own loader and collector, so
// those shared values are the whole of the state under test.
//
// The t.Parallel() tests elsewhere run different tests concurrently, not the
// same load, so they do not cover this. Run under -race to mean anything.
func TestLoadString_ConcurrentCallsAreSafe(t *testing.T) {
	t.Parallel()

	sources := []string{
		"schema \"a\"\ntype Alpha {\n\tid String primary\n\tname String required\n}\n",
		"schema \"b\"\ntype Beta {\n\tid UUID primary\n\tscore Float[0.0, 1.0]\n\t! \"in range\" score >= 0.0\n}\n",
		"schema \"c\"\ntype Gamma {\n\tid String primary\n\tstate Enum[\"on\", \"off\"] @index\n\t@@index(id, state)\n}\n",
		// A source that fails, so the recovery and diagnostic paths run
		// concurrently too rather than only the success path.
		"schema \"d\"\ntype Delta {\n\tid String primary\n\tbroken Integer[0x10, 5]\n}\n",
	}

	const goroutines = 16
	const iterations = 25

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			src := sources[i%len(sources)]
			for range iterations {
				s, res := schema.LoadString(context.Background(), src, "concurrent.yammm")
				if s == nil && !res.HasErrors() {
					t.Errorf("nil schema with no error diagnostics for %q", src)
					return
				}
				if s != nil && res.HasErrors() {
					t.Errorf("non-nil schema alongside errors for %q: %v", src, res)
					return
				}
			}
		})
	}
	wg.Wait()
}

// Concurrent loads of the same source agree on what they produce. A race in the
// shared parsers could corrupt one call's tree without failing it, which the
// safety test above would not see.
func TestLoadString_ConcurrentCallsAgree(t *testing.T) {
	t.Parallel()

	const src = "schema \"agree\"\n" +
		"type Doc = String[1, 40]\n" +
		"type Alpha {\n\tid Doc primary\n\ttags List<String>\n\t--> peer (one) Beta\n}\n" +
		"type Beta {\n\tid String primary\n}\n"

	want, res := schema.LoadString(context.Background(), src, "agree.yammm")
	if want == nil {
		t.Fatalf("fixture does not load: %v", res)
	}
	wantHash := schema.StructuralHash(want)

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 25 {
				got, res := schema.LoadString(context.Background(), src, "agree.yammm")
				if got == nil {
					t.Errorf("concurrent load returned nil: %v", res)
					return
				}
				if h := schema.StructuralHash(got); h != wantHash {
					t.Errorf("concurrent load produced a different schema: %s != %s", h, wantHash)
					return
				}
			}
		})
	}
	wg.Wait()
}

package schema_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Phase 8 seals a completing schema's own declarations. A relation inherited
// across an `extends` that crosses a schema boundary is owned by the ancestor's
// schema and was sealed when that schema completed, so re-sealing it from the
// inheriting schema writes another schema's state.
//
// TestSeal_SharedRegistryConcurrentLoads is the regression anchor: it fails
// under -race whenever the sealing walk reaches a relation this schema does
// not own.
func TestSeal_SharedRegistryConcurrentLoads(t *testing.T) {
	t.Parallel()

	const base = `schema "base"

type Customer {
    id String primary
    name String
}

abstract type HasCustomer {
    id String primary
    --> CUSTOMER (one:many) Customer
}
`
	const first = `schema "first"

import "./base.yammm" as b

type OrderA extends b.HasCustomer {
    tag String
}
`
	const second = `schema "second"

import "./base.yammm" as b

type OrderB extends b.HasCustomer {
    note String
}
`

	dir := t.TempDir()
	for name, src := range map[string]string{
		"base.yammm":   base,
		"first.yammm":  first,
		"second.yammm": second,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// WithRegistry's godoc blesses sharing one registry across loads, so this
	// is a supported shape, not an abuse of the API.
	reg := schema.NewRegistry()
	ctx := t.Context()

	var wg sync.WaitGroup
	for _, entry := range []string{"first.yammm", "second.yammm"} {
		wg.Go(func() {
			_, res := schema.Load(ctx, filepath.Join(dir, entry),
				schema.WithModuleRoot(dir), schema.WithRegistry(reg))
			if res.Err() != nil {
				t.Errorf("load %s: %v", entry, res.Err())
			}
		})
	}
	wg.Wait()
}

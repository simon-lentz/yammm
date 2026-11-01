package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadClosureFixture loads a four-schema diamond: entry imports a and b,
// and both a and b import base.
func loadClosureFixture(t *testing.T) schema.Schema {
	t.Helper()
	sources := map[string][]byte{
		"entry.yammm": []byte(`schema "entry"

import "a.yammm" as a
import "b.yammm" as b

type Root {
	id String primary
	--> IN_A (one) a.Alpha
	--> IN_B (one) b.Beta
}
`),
		"a.yammm": []byte(`schema "a"

import "base.yammm" as base

type Alpha {
	id String primary
	--> GROUNDED (one) base.Ground
}
`),
		"b.yammm": []byte(`schema "b"

import "base.yammm" as base

type Beta {
	id String primary
	--> GROUNDED (one) base.Ground
}
`),
		"base.yammm": []byte(`schema "base"

type Ground {
	id String primary
}
`),
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load: %v", result.Err())
	}
	return s
}

func TestClosure_DiamondOrderAndDedup(t *testing.T) {
	s := loadClosureFixture(t)

	closure := slices.Collect(s.Closure())
	names := make([]string, 0, len(closure))
	for _, sc := range closure {
		names = append(names, sc.Name())
	}
	// Entry first, then direct imports in declaration order, then transitive
	// imports breadth-first; base appears exactly once despite two edges.
	want := []string{"entry", "a", "b", "base"}
	if !slices.Equal(names, want) {
		t.Errorf("closure order = %v, want %v", names, want)
	}
}

func TestClosure_NoImports(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "solo"

type Only {
	id String primary
}
`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("load: %v", result.Err())
	}

	closure := slices.Collect(s.Closure())
	if len(closure) != 1 {
		t.Fatalf("closure length = %d, want 1", len(closure))
	}
	if closure[0] != s {
		t.Error("an import-free closure must contain exactly the schema itself")
	}
}

func TestTypeByID(t *testing.T) {
	s := loadClosureFixture(t)

	t.Run("local type", func(t *testing.T) {
		root, ok := s.Type("Root")
		if !ok {
			t.Fatal("type Root missing")
		}
		got, ok := s.TypeByID(root.ID())
		if !ok {
			t.Fatal("TypeByID missed a local type")
		}
		if got != root {
			t.Error("TypeByID must return the identical Type")
		}
	})

	t.Run("transitively imported type", func(t *testing.T) {
		var base schema.Schema
		for sc := range s.Closure() {
			if sc.Name() == "base" {
				base = sc
			}
		}
		if base.IsZero() {
			t.Fatal("base schema missing from closure")
		}
		ground, ok := base.Type("Ground")
		if !ok {
			t.Fatal("type Ground missing")
		}

		got, ok := s.TypeByID(ground.ID())
		if !ok {
			t.Fatal("TypeByID missed a transitively imported type")
		}
		if got != ground {
			t.Error("TypeByID must return the identical Type")
		}
	})

	t.Run("zero TypeID misses", func(t *testing.T) {
		if _, ok := s.TypeByID(schema.TypeID{}); ok {
			t.Error("zero TypeID must miss")
		}
	})

	t.Run("unknown TypeID misses", func(t *testing.T) {
		if _, ok := s.TypeByID(schema.NewTypeID(sourceID(t, "nowhere.yammm"), "Ghost")); ok {
			t.Error("unknown TypeID must miss")
		}
	})
}

// Closure resolves its members before it returns, so a zero Schema fails at the
// call and never hands out an iterator that fails later.
func TestClosure_PanicsAtTheCallOnAZeroSchema(t *testing.T) {
	t.Parallel()
	yammmtest.AssertPanics(t, func() { _ = schema.Schema{}.Closure() })
}

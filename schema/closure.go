package schema

import (
	"slices"

	"github.com/simon-lentz/yammm/location"
)

// Closure returns this schema followed by every transitively imported schema:
// a breadth-first walk from the receiver over each schema's imports in
// declaration order, deduplicated by SourceID, so a schema reachable through
// multiple import paths appears exactly once, at its first-reached position.
// Imports without a resolved schema (deferred or failed) are skipped.
//
// The walk order is deterministic, making Closure suitable for driving
// generated output. The returned slice is a copy; callers may modify it
// freely.
//
// The closure is computed on first use and cached; Closure and TypeByID are
// safe for concurrent use, like all Schema accessors.
func (s *Schema) Closure() []*Schema {
	s.ensureClosure()
	return slices.Clone(s.closure)
}

// TypeByID returns the type with the given identity from anywhere in this
// schema's import closure (see [Schema.Closure]). It resolves the absolute
// type identities recorded during completion — [Relation.TargetID],
// [Type.ID] — without reference to any schema's local names or import
// aliases. A zero or unknown TypeID returns ok == false.
func (s *Schema) TypeByID(id TypeID) (*Type, bool) {
	s.ensureClosure()
	t, ok := s.typeByID[id]
	return t, ok
}

// ensureClosure computes and caches the import closure and its TypeID index.
// Computation is deferred to first use because import wiring
// ([Import.setSchema]) completes after each schema's own completion; by the
// time a Schema is observable outside the loader, wiring is done.
func (s *Schema) ensureClosure() {
	s.closureOnce.Do(func() {
		var closure []*Schema
		seen := map[location.SourceID]bool{}
		queue := []*Schema{s}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur == nil || seen[cur.SourceID()] {
				continue
			}
			seen[cur.SourceID()] = true
			closure = append(closure, cur)
			for _, imp := range cur.imports {
				if dep := imp.Schema(); dep != nil {
					queue = append(queue, dep)
				}
			}
		}

		typeByID := make(map[TypeID]*Type)
		for _, sc := range closure {
			for _, t := range sc.types {
				typeByID[t.ID()] = t
			}
		}

		s.closure = closure
		s.typeByID = typeByID
	})
}

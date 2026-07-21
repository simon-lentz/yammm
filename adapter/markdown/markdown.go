package markdown

import (
	"bytes"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// typeEntry records how one type in the closure is addressed in the emitted
// document: the display name used for its heading and in links to it (bare
// for entry-schema types, schema-qualified for imported ones), and the
// anchor that heading produces.
type typeEntry struct {
	typ     *schema.Type
	display string
	anchor  string
}

// generator accumulates the emitted document and the cross-referencing
// state the emitters and the self-check share: the closure-wide type index,
// the set of anchors emitted so far, and the source registry used to
// extract invariant declaration text.
type generator struct {
	buf     bytes.Buffer
	entry   *schema.Schema
	closure []*schema.Schema
	types   map[schema.TypeID]*typeEntry
	anchors map[string]bool
	sources *schema.Sources
}

// newGenerator builds the generator for a schema and its import closure.
// Entry-schema types display under their bare name; every imported type
// displays schema-qualified, which makes headings collision-proof across
// the closure.
func newGenerator(s *schema.Schema) *generator {
	g := &generator{
		entry:   s,
		closure: closureSchemas(s),
		types:   make(map[schema.TypeID]*typeEntry),
		anchors: make(map[string]bool),
		sources: s.Sources(),
	}
	for i, sch := range g.closure {
		for _, t := range sch.TypesSlice() {
			display := t.Name()
			if i > 0 {
				display = sch.Name() + "." + t.Name()
			}
			g.types[t.ID()] = &typeEntry{typ: t, display: display, anchor: slug(display)}
		}
	}
	return g
}

// resolveSuper finds the closure entry for a declared extends reference by
// matching it against the type's resolved supertype linearization. Returns
// false when the reference never resolved (a deferred import).
func (g *generator) resolveSuper(t *schema.Type, ref schema.TypeRef) (*typeEntry, bool) {
	for _, super := range t.SuperTypesSlice() {
		if super.Ref().String() != ref.String() {
			continue
		}
		if e, ok := g.types[super.ID()]; ok {
			return e, true
		}
	}
	return nil, false
}

// closureSchemas returns the schema plus its transitive import closure in
// deterministic breadth-first order, entry first, deduplicated by source.
func closureSchemas(s *schema.Schema) []*schema.Schema {
	var out []*schema.Schema
	seen := map[location.SourceID]bool{}
	queue := []*schema.Schema{s}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == nil || seen[cur.SourceID()] {
			continue
		}
		seen[cur.SourceID()] = true
		out = append(out, cur)
		for _, imp := range cur.ImportsSlice() {
			if dep := imp.Schema(); dep != nil {
				queue = append(queue, dep)
			}
		}
	}
	return out
}

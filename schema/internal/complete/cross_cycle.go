package complete

import (
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// crossVisitState tracks DFS progress for cross-schema cycle detection.
type crossVisitState int

const (
	crossUnvisited crossVisitState = iota
	crossVisiting
	crossVisited
)

// CrossSchemaRegistry provides type lookup across all loaded schemas.
type CrossSchemaRegistry interface {
	All() []*schema.Schema
	LookupType(id schema.TypeID) (*schema.Type, schema.LookupStatus)
}

// DetectCrossSchemaInheritanceCycles performs global inheritance cycle detection
// across all schemas in the registry. Returns any detected cycle issues.
//
// This function should be called after all schemas are loaded and registered,
// when full cross-schema visibility is available.
//
// The algorithm uses DFS with three states (unvisited, visiting, visited) to
// detect back-edges which indicate cycles. Diamond inheritance patterns are
// correctly handled as non-cycles.
func DetectCrossSchemaInheritanceCycles(registry CrossSchemaRegistry) []*diag.Issue {
	if registry == nil {
		return nil
	}

	state := make(map[schema.TypeID]crossVisitState)
	stack := make([]schema.TypeID, 0, 32)
	var issues []*diag.Issue

	var dfs func(id schema.TypeID) bool
	dfs = func(id schema.TypeID) bool {
		if state[id] == crossVisited {
			return true // Already fully processed, no cycle here
		}
		if state[id] == crossVisiting {
			// Cycle detected - this type is in the current DFS path
			cyclePath := buildCrossSchemaPath(registry, stack, id)
			t, _ := registry.LookupType(id)
			span := t.Span()
			issue := diag.NewIssue(diag.Error, diag.E_INHERIT_CYCLE,
				"cross-schema inheritance cycle detected: "+strings.Join(cyclePath, " -> ")).
				WithSpan(span).Build()
			issues = append(issues, &issue)
			return false
		}

		state[id] = crossVisiting
		stack = append(stack, id)

		// Use defer to ensure cleanup on ALL exit paths per DEV_WRITING_GO.md
		defer func() {
			state[id] = crossVisited
			stack = stack[:len(stack)-1]
		}()

		t, status := registry.LookupType(id)
		if !status.Found() {
			// Type not in registry; skip (already validated elsewhere)
			return true
		}

		for ref := range t.Inherits() {
			superID := resolveInheritToTypeID(registry, t, ref)
			if superID.IsZero() {
				continue // Unable to resolve; skip
			}
			// Continue traversing to collect all cycles rather than short-circuiting
			dfs(superID)
		}

		return true
	}

	// Visit all types in all schemas
	for _, s := range registry.All() {
		for _, t := range s.Types() {
			if state[t.ID()] == crossUnvisited {
				dfs(t.ID())
			}
		}
	}

	return issues
}

// resolveInheritToTypeID resolves a TypeRef from the Inherits() clause to a TypeID.
func resolveInheritToTypeID(registry CrossSchemaRegistry, ownerType *schema.Type, ref schema.TypeRef) schema.TypeID {
	if ref.Qualifier() == "" {
		// Local type reference
		return schema.NewTypeID(ownerType.SourceID(), ref.Name())
	}

	// Qualified reference - need to look up the import
	// Get the schema containing this type to find its imports
	for _, s := range registry.All() {
		if s.SourceID() == ownerType.SourceID() {
			imp, ok := s.ImportByAlias(ref.Qualifier())
			if !ok {
				return schema.TypeID{} // Import not found
			}
			return schema.NewTypeID(imp.ResolvedSourceID(), ref.Name())
		}
	}
	return schema.TypeID{}
}

// buildCrossSchemaPath creates a human-readable path for the cycle.
func buildCrossSchemaPath(registry CrossSchemaRegistry, stack []schema.TypeID, target schema.TypeID) []string {
	// Find where target appears in stack
	idx := -1
	for i, id := range stack {
		if id == target {
			idx = i
			break
		}
	}
	if idx == -1 {
		// Shouldn't happen, but handle gracefully
		result := make([]string, 0, len(stack)+1)
		for _, id := range stack {
			result = append(result, formatTypeID(registry, id))
		}
		result = append(result, formatTypeID(registry, target))
		return result
	}

	// Build cycle path from idx to end, plus target again
	result := make([]string, 0, len(stack)-idx+1)
	for _, id := range stack[idx:] {
		result = append(result, formatTypeID(registry, id))
	}
	result = append(result, formatTypeID(registry, target))
	return result
}

// formatTypeID returns a human-readable type identifier.
// For local types, returns just the name.
// For cross-schema types, returns "schemaName:TypeName".
func formatTypeID(registry CrossSchemaRegistry, id schema.TypeID) string {
	// Try to get a friendly schema name
	for _, s := range registry.All() {
		if s.SourceID() == id.SchemaPath() {
			return s.Name() + ":" + id.Name()
		}
	}
	return id.String()
}

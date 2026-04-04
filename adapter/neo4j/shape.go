package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// NodeShape describes the Neo4j representation of a single yammm type.
//
// Construct via [Adapter.ShapeForSchema]; do not create directly.
type NodeShape struct {
	Type           string   // Original yammm type name
	Label          string   // Sanitized Neo4j label (namespace + separator + type)
	PrimaryKeys    []string // Property names forming the uniqueness constraint
	RequiredFields []string // All required properties (including primary keys)
}

// GraphShape maps yammm type names to their Neo4j node representations.
//
// Construct via [Adapter.ShapeForSchema]; do not create directly.
type GraphShape struct {
	Types map[string]NodeShape
}

// ShapeForSchema converts a yammm schema into a [GraphShape] describing
// the Neo4j node structure for each non-abstract type.
//
// This is the metadata needed to ensure write-time label and key consistency.
// Consumers use [NodeShape.Label] for MERGE patterns and [NodeShape.PrimaryKeys]
// for MERGE key properties.
//
// If validation errors are found, returns (nil, result) where result contains
// [E_NEO4J_INVALID_IDENTIFIER] issues.
func (a *Adapter) ShapeForSchema(ctx context.Context, s *schema.Schema) (*GraphShape, diag.Result) {
	collector := diag.NewCollector(0)
	shape := &GraphShape{
		Types: make(map[string]NodeShape),
	}

	for _, t := range s.TypesSlice() {
		name := strings.TrimSpace(t.Name())
		if name == "" || t.IsAbstract() {
			continue
		}

		label := a.Label(ctx, s.Name(), name)
		if label == "" {
			continue
		}

		if err := ValidateIdentifier(label, fmt.Sprintf("type %q label", name)); err != nil {
			issue := diag.NewIssue(diag.Error, E_NEO4J_INVALID_IDENTIFIER,
				fmt.Sprintf("invalid label for type %q: %s", name, err)).
				WithDetail(diag.DetailKeyFormat, "neo4j").
				WithDetail(diag.DetailKeyTypeName, name).
				WithDetail(detailKeyLabel, label).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build()
			collector.Collect(issue)
			continue
		}

		// Extract primary keys.
		var primary []string
		for _, pk := range t.PrimaryKeysSlice() {
			if pkName := strings.TrimSpace(pk.Name()); pkName != "" {
				primary = append(primary, pkName)
			}
		}

		// Extract required fields (non-optional properties) with dedup.
		var required []string
		seen := make(map[string]struct{})
		for _, prop := range t.AllPropertiesSlice() {
			propName := strings.TrimSpace(prop.Name())
			if propName == "" {
				continue
			}
			if _, exists := seen[propName]; exists {
				continue
			}
			if prop.IsRequired() {
				seen[propName] = struct{}{}
				required = append(required, propName)
			}
		}

		// Merge primary keys into required (avoiding duplicates).
		for _, key := range primary {
			if _, exists := seen[key]; !exists {
				required = append(required, key)
			}
		}

		shape.Types[name] = NodeShape{
			Type:           name,
			Label:          label,
			PrimaryKeys:    primary,
			RequiredFields: required,
		}
	}

	result := collector.Result()
	if !result.OK() {
		return nil, result
	}
	return shape, result
}

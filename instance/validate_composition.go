package instance

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// pkPathFromInstance builds a PK-based path segment for a validated instance.
// Returns basePath.PK(...) if the child type has a primary key, otherwise basePath.Index(i).
// This is used for post-validation diagnostic paths where PK data is available.
func pkPathFromInstance(basePath path.Builder, child *ValidInstance, childType *schema.Type, index int) path.Builder {
	if !childType.HasPrimaryKey() {
		return basePath.Index(index)
	}

	pk := child.PrimaryKey()
	pks := childType.PrimaryKeysSlice()

	if pk.Len() == 0 || len(pks) == 0 {
		return basePath.Index(index)
	}

	fields := make([]path.PKField, 0, pk.Len())
	for i, prop := range pks {
		if i >= pk.Len() {
			break
		}
		fields = append(fields, path.PKField{
			Name:  prop.Name(),
			Value: pk.Get(i).Unwrap(),
		})
	}

	return basePath.PK(fields...)
}

// validateCompositions validates all composition relations for an instance.
// Returns a map of relation name -> composed children as immutable.Value.
func (v *Validator) validateCompositions(
	ctx context.Context,
	typ *schema.Type,
	props map[string]any,
	collector *diag.Collector,
	prov *Provenance,
) map[string]immutable.Value {
	composed := make(map[string]immutable.Value)

	for rel := range typ.AllCompositions() {
		if err := ctx.Err(); err != nil {
			return composed
		}

		// Get the raw composition value from properties.
		// Per architecture spec, use FieldName() (JSON field name) only,
		// not the schema relation Name() (DSL name).
		fieldName := rel.FieldName()
		rawValue, hasValue := props[fieldName]

		// Also check case-insensitive match on FieldName only, with collision detection
		if !hasValue && !v.cfg.strictPropertyNames {
			lower := strings.ToLower(fieldName)
			var candidates []string
			for k, val := range props {
				if strings.ToLower(k) == lower {
					candidates = append(candidates, k)
					rawValue = val
					hasValue = true
				}
			}
			if len(candidates) > 1 {
				// Collision: multiple input names fold to same composition field
				slices.Sort(candidates) // Deterministic error message
				issue := diag.NewIssue(
					diag.Error,
					ErrCaseFoldCollision,
					fmt.Sprintf("multiple input fields %v fold to composition field %q", candidates, fieldName),
				).WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyJsonField, fieldName)
				withProvenance(issue, prov, provenancePathBuilder(prov).String())
				collector.Collect(issue.Build())
				hasValue = false // Don't proceed with ambiguous match
			}
		}

		// Use schema relation name for path, not JSON field name
		basePath := provenancePathBuilder(prov).Key(rel.Name())

		// Validate the composition
		composedValue := v.validateComposition(ctx, rel, rawValue, hasValue, collector, prov, basePath)
		if !composedValue.IsNil() {
			composed[rel.Name()] = composedValue
		}
	}

	if len(composed) == 0 {
		return nil
	}
	return composed
}

// validateComposition validates a single composition relation.
func (v *Validator) validateComposition(
	ctx context.Context,
	rel *schema.Relation,
	rawValue any,
	hasValue bool,
	collector *diag.Collector,
	prov *Provenance,
	basePath path.Builder,
) immutable.Value {
	// Handle absent field - emit error for required compositions.
	// Unlike associations, composition presence is validated at instance layer.
	if !hasValue {
		if !rel.IsOptional() {
			issue := diag.NewIssue(
				diag.Error,
				ErrUnresolvedRequiredComposition,
				fmt.Sprintf("missing required composition %q", rel.Name()),
			).WithDetail(diag.DetailKeyReason, "absent").
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyJsonField, rel.FieldName())
			withProvenance(issue, prov, basePath.String())
			collector.Collect(issue.Build())
		}
		return immutable.Value{}
	}

	// Handle explicit null - always a shape error per spec.
	// Null is never valid for composition fields regardless of optionality.
	if rawValue == nil {
		issue := diag.NewIssue(
			diag.Error,
			ErrEdgeShapeMismatch,
			fmt.Sprintf("composition %q: null is not a valid composition value", rel.Name()),
		).WithExpectedGot("array", "null")
		withProvenance(issue, prov, basePath.String()).
			WithDetail(diag.DetailKeyJsonField, rel.FieldName())
		collector.Collect(issue.Build())
		return immutable.Value{}
	}

	// Compositions always expect an array (accept typed slices via reflection)
	arr, ok := toSliceOfAny(rawValue)
	if !ok {
		issue := diag.NewIssue(
			diag.Error,
			ErrEdgeShapeMismatch,
			fmt.Sprintf("composition %q: expected array, got %s", rel.Name(), kindOf(rawValue)),
		)
		withProvenance(issue, prov, basePath.String()).
			WithDetail(diag.DetailKeyJsonField, rel.FieldName())
		collector.Collect(issue.Build())
		return immutable.Value{}
	}

	// Empty array is valid for optional, error for required
	if len(arr) == 0 {
		if !rel.IsOptional() {
			issue := diag.NewIssue(
				diag.Error,
				ErrUnresolvedRequiredComposition,
				fmt.Sprintf("composition %q: required composition cannot be empty", rel.Name()),
			).WithDetail(diag.DetailKeyReason, "empty").
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyJsonField, rel.FieldName())
			withProvenance(issue, prov, basePath.String())
			collector.Collect(issue.Build())
			return immutable.Value{}
		}
		// Return empty slice wrapped
		return immutable.Wrap([]*ValidInstance{})
	}

	// Build raw instances for children
	childRaws := make([]RawInstance, 0, len(arr))
	for i, elem := range arr {
		// Accept typed maps via reflection for programmatic callers
		childObj, ok := toMapOfAny(elem)
		if !ok {
			issue := diag.NewIssue(
				diag.Error,
				ErrEdgeShapeMismatch,
				"composition child must be an object, got "+kindOf(elem),
			)
			withProvenance(issue, prov, basePath.Index(i).String()).
				WithDetail(diag.DetailKeyJsonField, rel.FieldName())
			collector.Collect(issue.Build())
			continue
		}

		// P1-3: Propagate parent's sourceName and span to children
		var childProv *Provenance
		if prov != nil {
			childProv = NewProvenance(prov.SourceName(), basePath.Index(i), prov.Span())
		} else {
			childProv = NewProvenance("", basePath.Index(i), location.Span{})
		}

		childRaws = append(childRaws, RawInstance{
			Properties: childObj,
			Provenance: childProv,
		})
	}

	// Recursively validate children
	validChildren, failures, err := v.ValidateForComposition(ctx, rel.Owner(), rel.Name(), childRaws)
	if err != nil {
		issue := diag.NewIssue(
			diag.Error,
			ErrEvalError,
			"composition validation error: "+err.Error(),
		).WithDetail(diag.DetailKeyRelationName, rel.Name())
		withProvenance(issue, prov, basePath.String())
		collector.Collect(issue.Build())
		return immutable.Value{}
	}

	// Collect failures into the parent collector with relation context.
	// Augment child issues with json_field detail for usability.
	relationDetails := diag.PathRelation(rel.Name(), rel.FieldName())
	for _, f := range failures {
		for issue := range f.Result.Issues() {
			augmented := diag.FromIssue(issue).
				WithDetails(relationDetails...).
				Build()
			collector.Collect(augmented)
		}
	}

	// Check for duplicate PKs among children - only for types that have PKs.
	// PK-less composed children use structural position (array index) for identity,
	// so no duplicate check is needed for them.
	if len(validChildren) > 0 {
		childType, found := v.schema.ResolveType(rel.Target())
		if found && childType.HasPrimaryKey() {
			seenPKs := make(map[string]int) // pk string -> first occurrence index
			for i, child := range validChildren {
				pkStr := child.PrimaryKey().String()
				if firstIdx, exists := seenPKs[pkStr]; exists {
					issue := diag.NewIssue(
						diag.Error,
						ErrDuplicateComposedPK,
						fmt.Sprintf("duplicate primary key in composed children at indices %d and %d", firstIdx, i),
					).WithDetail(diag.DetailKeyRelationName, rel.Name()).
						WithDetail(diag.DetailKeyJsonField, rel.FieldName()).
						WithDetail(diag.DetailKeyPrimaryKey, pkStr)
					withProvenance(issue, prov, pkPathFromInstance(basePath, child, childType, i).String())
					collector.Collect(issue.Build())
				} else {
					seenPKs[pkStr] = i
				}
			}
		}
	}

	if collector.HasErrors() {
		return immutable.Value{}
	}

	// Wrap the valid children
	return immutable.Wrap(validChildren)
}

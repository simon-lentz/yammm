package instance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/schema"
)

// fkPrefix is the prefix for foreign key fields in edge objects.
// FK fields are named _target_<pk_name> where pk_name is the target type's PK field.
const fkPrefix = "_target_"

// validateEdges validates all association relations for an instance.
// Returns a map of relation name -> ValidEdgeData.
func (v *Validator) validateEdges(
	ctx context.Context,
	typ *schema.Type,
	props map[string]any,
	collector *diag.Collector,
	prov *Provenance,
) map[string]*ValidEdgeData {
	edges := make(map[string]*ValidEdgeData)

	for rel := range typ.AllAssociations() {
		if err := ctx.Err(); err != nil {
			return edges
		}

		// Get the raw edge value from properties.
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
				// Collision: multiple input names fold to same relation field
				sort.Strings(candidates) // Deterministic error message
				issue := diag.NewIssue(
					diag.Error,
					ErrCaseFoldCollision,
					fmt.Sprintf("multiple input fields %v fold to relation field %q", candidates, fieldName),
				).WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyJsonField, fieldName)
				withProvenance(issue, prov, provenancePathBuilder(prov).String())
				collector.Collect(issue.Build())
				hasValue = false // Don't proceed with ambiguous match
			}
		}

		// Use schema relation name for path, not JSON field name
		basePath := provenancePathBuilder(prov).Key(rel.Name())

		// Validate the edge data
		edgeData := v.validateEdgeData(ctx, rel, rawValue, hasValue, collector, prov, basePath)
		if edgeData != nil {
			edges[rel.Name()] = edgeData
		}
	}

	if len(edges) == 0 {
		return nil
	}
	return edges
}

// validateEdgeData validates a single edge relation.
func (v *Validator) validateEdgeData(
	ctx context.Context,
	rel *schema.Relation,
	rawValue any,
	hasValue bool,
	collector *diag.Collector,
	prov *Provenance,
	basePath path.Builder,
) *ValidEdgeData {
	// Handle absent field - valid for associations (graph-layer concern).
	// Association presence/requiredness is validated at graph.Check() via E_UNRESOLVED_REQUIRED.
	if !hasValue {
		return nil
	}

	// Handle explicit null - always a shape error per spec.
	// Null is never valid for edge object fields regardless of optionality.
	if rawValue == nil {
		issue := diag.NewIssue(
			diag.Error,
			ErrEdgeShapeMismatch,
			"edge "+rel.Name()+": null is not a valid edge value",
		).WithExpectedGot(expectedShapeForRelation(rel), "null")
		withProvenance(issue, prov, basePath.String()).
			WithDetail(diag.DetailKeyJsonField, rel.FieldName())
		collector.Collect(issue.Build())
		return nil
	}

	// Validate shape based on multiplicity
	if rel.IsMany() {
		// Expect array (accept typed slices via reflection)
		arr, ok := toSliceOfAny(rawValue)
		if !ok {
			issue := diag.NewIssue(
				diag.Error,
				ErrEdgeShapeMismatch,
				"edge "+rel.Name()+": expected array, got "+kindOf(rawValue),
			)
			withProvenance(issue, prov, basePath.String()).
				WithDetail(diag.DetailKeyJsonField, rel.FieldName())
			collector.Collect(issue.Build())
			return nil
		}

		// Empty array is valid at instance layer for all associations.
		// Required association empty-array validation is deferred to graph.Check()
		// via E_UNRESOLVED_REQUIRED with reason="empty".

		// Validate each target
		targets := make([]ValidEdgeTarget, 0, len(arr))
		for i, elem := range arr {
			if err := ctx.Err(); err != nil {
				return nil
			}

			targetPath := basePath.Index(i)
			target := v.validateEdgeTarget(ctx, rel, elem, collector, prov, targetPath)
			if target != nil {
				targets = append(targets, *target)
			}
		}

		if len(targets) == 0 && collector.HasErrors() {
			return nil
		}
		return NewValidEdgeData(targets)
	}

	// Expect single object (accept typed maps via reflection)
	obj, ok := toMapOfAny(rawValue)
	if !ok {
		issue := diag.NewIssue(
			diag.Error,
			ErrEdgeShapeMismatch,
			"edge "+rel.Name()+": expected object, got "+kindOf(rawValue),
		)
		withProvenance(issue, prov, basePath.String()).
			WithDetail(diag.DetailKeyJsonField, rel.FieldName())
		collector.Collect(issue.Build())
		return nil
	}

	target := v.validateEdgeTarget(ctx, rel, obj, collector, prov, basePath)
	if target == nil {
		return nil
	}
	return NewValidEdgeData([]ValidEdgeTarget{*target})
}

// validateEdgeTarget validates a single edge target object.
// Uses per-target collector isolation to ensure each target is evaluated independently
// (P1-4 fix: eliminates global collector coupling).
func (v *Validator) validateEdgeTarget(
	_ context.Context,
	rel *schema.Relation,
	elem any,
	collector *diag.Collector,
	prov *Provenance,
	targetPath path.Builder,
) *ValidEdgeTarget {
	// P1-4: Use per-target collector to avoid coupling between targets.
	// Use unlimited collector since issues will be merged into the parent
	// collector which handles the actual limit.
	targetCollector := diag.NewCollectorUnlimited()

	// Accept typed maps via reflection for programmatic callers
	obj, ok := toMapOfAny(elem)
	if !ok {
		issue := diag.NewIssue(
			diag.Error,
			ErrEdgeShapeMismatch,
			"expected object for edge target, got "+kindOf(elem),
		)
		withProvenance(issue, prov, targetPath.String()).
			WithDetail(diag.DetailKeyJsonField, rel.FieldName())
		targetCollector.Collect(issue.Build())
		// Merge issues into parent collector
		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
		return nil
	}

	// Get target type to extract PK fields.
	// Use ResolveType with the full TypeRef to handle imported types correctly.
	targetType, found := v.schema.ResolveType(rel.Target())
	if !found {
		// This shouldn't happen if schema is valid, but handle gracefully
		issue := diag.NewIssue(
			diag.Error,
			ErrTypeNotFound,
			fmt.Sprintf("edge target type %q not found", rel.Target().String()),
		).WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyTargetType, rel.Target().String())
		withProvenance(issue, prov, targetPath.String())
		targetCollector.Collect(issue.Build())
		// Merge issues into parent collector
		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
		return nil
	}

	// Extract FK fields and build target key.
	// Build expected FK fields list upfront for diagnostic details.
	pkFields := targetType.PrimaryKeysSlice()
	allExpectedFKFields := make([]string, len(pkFields))
	for i, pk := range pkFields {
		allExpectedFKFields[i] = fkPrefix + pk.Name()
	}

	pkComponents := make([]any, 0, len(pkFields))
	presentFKFields := make([]string, 0, len(pkFields)) // Track key existence
	missingFKFields := make([]string, 0, len(pkFields)) // Track truly absent keys

	for _, pk := range pkFields {
		fkFieldName := fkPrefix + pk.Name()
		val, hasFKField := obj[fkFieldName]

		// Note: FK field matching is always case-sensitive per architecture spec.
		// StrictPropertyNames only affects property name matching, not FK fields.

		// Phase 1: Check key existence (not value)
		if !hasFKField {
			missingFKFields = append(missingFKFields, fkFieldName)
			continue
		}

		// Key exists - track as present regardless of value
		presentFKFields = append(presentFKFields, fkFieldName)

		// Phase 2: Handle null value - present but invalid per spec
		if val == nil {
			// Resolve alias to underlying type for user-friendly error messages
			constraint := pk.Constraint()
			if alias, ok := constraint.(schema.AliasConstraint); ok {
				if resolved := alias.Resolved(); resolved != nil {
					constraint = resolved
				}
			}
			expectedType := strings.ToLower(constraint.Kind().String())
			issue := diag.NewIssue(
				diag.Error,
				ErrTypeMismatch,
				fmt.Sprintf("FK field %q: expected %s, got null", fkFieldName, expectedType),
			).WithDetails(diag.RelationField(rel.Name(), fkFieldName)...).
				WithExpectedGot(expectedType, "null")
			withProvenance(issue, prov, targetPath.Key(fkFieldName).String())
			targetCollector.Collect(issue.Build())
			continue
		}

		// Phase 3: Validate FK type against PK constraint
		if err := v.checkValueWithRecovery(val, pk.Constraint()); err != nil {
			code := ErrTypeMismatch
			var checkErr *eval.CheckError
			if errors.As(err, &checkErr) && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("FK field %q: %s", fkFieldName, err.Error()),
			).WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, targetPath.Key(fkFieldName).String())
			targetCollector.Collect(issue.Build())
			continue
		}

		// Phase 4: Coerce and collect valid component
		coercedVal, err := v.coerceValueWithRecovery(val, pk.Constraint())
		if err != nil {
			coercedVal = val
		}
		pkComponents = append(pkComponents, coercedVal)
	}

	// Classification based on presence count, not validity
	expectedCount := len(allExpectedFKFields)
	presentCount := len(presentFKFields)

	if presentCount == 0 {
		// No FK fields present at all - E_MISSING_FK_TARGET
		expectedStr := strings.Join(allExpectedFKFields, ", ")
		issue := diag.NewIssue(
			diag.Error,
			ErrMissingFKTarget,
			"missing FK field(s): "+expectedStr,
		).WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyExpected, expectedStr)
		withProvenance(issue, prov, targetPath.String())
		targetCollector.Collect(issue.Build())

		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
		return nil
	} else if presentCount < expectedCount && expectedCount > 1 {
		// Partial composite FK - some present, some missing
		// presentFKFields already contains all present keys (including invalid ones)
		// and is in PK order for deterministic output
		expectedStr := strings.Join(allExpectedFKFields, ", ")
		presentStr := strings.Join(presentFKFields, ", ")
		issue := diag.NewIssue(
			diag.Error,
			ErrPartialCompositeFK,
			"incomplete composite FK: missing "+strings.Join(missingFKFields, ", "),
		).WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyExpected, expectedStr).
			WithDetail(diag.DetailKeyGot, presentStr)
		withProvenance(issue, prov, targetPath.String())
		targetCollector.Collect(issue.Build())

		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
		return nil
	}

	// present == expected: all FK fields present
	// Type errors were emitted above; check if we have all valid components
	if len(pkComponents) < expectedCount {
		// Some fields had validation errors - already emitted
		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
		return nil
	}

	// Check for unknown fields in edge object
	// Track which schema properties have been matched to detect collisions
	matchedProps := make(map[string]string) // schema property name -> first input field name
	edgeProps := make(map[string]any)
	for fieldName, fieldVal := range obj {
		// Skip FK fields (case-sensitive matching per architecture spec)
		if strings.HasPrefix(fieldName, fkPrefix) {
			continue
		}

		// Check if it's a known edge property
		prop, isProp := rel.Property(fieldName)
		if !isProp && !v.cfg.strictPropertyNames {
			// Try case-insensitive match with collision detection
			lower := strings.ToLower(fieldName)
			var candidates []*schema.Property
			for p := range rel.Properties() {
				if strings.ToLower(p.Name()) == lower {
					candidates = append(candidates, p)
				}
			}
			if len(candidates) == 1 {
				prop = candidates[0]
				isProp = true
			} else if len(candidates) > 1 {
				// Multiple schema properties fold to same input - emit collision
				var names []string
				for _, c := range candidates {
					names = append(names, c.Name())
				}
				sort.Strings(names)
				issue := diag.NewIssue(
					diag.Error,
					ErrCaseFoldCollision,
					fmt.Sprintf("input field %q matches multiple edge properties %v", fieldName, names),
				).WithDetail(diag.DetailKeyRelationName, rel.Name())
				withProvenance(issue, prov, targetPath.Key(fieldName).String())
				targetCollector.Collect(issue.Build())
				continue
			}
		}

		// Check if this schema property was already matched by another input field
		if isProp {
			if firstField, exists := matchedProps[prop.Name()]; exists {
				// Collision: multiple input fields match same schema property
				colliders := []string{firstField, fieldName}
				sort.Strings(colliders)
				issue := diag.NewIssue(
					diag.Error,
					ErrCaseFoldCollision,
					fmt.Sprintf("multiple input fields %v fold to edge property %q", colliders, prop.Name()),
				).WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyPropertyName, prop.Name())
				withProvenance(issue, prov, targetPath.Key(fieldName).String())
				targetCollector.Collect(issue.Build())
				continue
			}
			matchedProps[prop.Name()] = fieldName
		}

		if !isProp {
			if !v.cfg.allowUnknownFields {
				issue := diag.NewIssue(
					diag.Error,
					ErrUnknownEdgeField,
					fmt.Sprintf("unknown field in edge object: %q", fieldName),
				).WithDetails(diag.RelationField(rel.Name(), fieldName)...)
				withProvenance(issue, prov, targetPath.Key(fieldName).String())
				targetCollector.Collect(issue.Build())
			}
			continue
		}

		// Validate edge property type
		if err := v.checkValueWithRecovery(fieldVal, prop.Constraint()); err != nil {
			code := ErrTypeMismatch
			var checkErr *eval.CheckError
			if errors.As(err, &checkErr) && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("edge property %q: %s", prop.Name(), err.Error()),
			).WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, targetPath.Key(fieldName).String())
			targetCollector.Collect(issue.Build())
			continue
		}

		// Coerce edge property to canonical form (e.g., int -> int64).
		// CoerceValue should not fail after CheckValue passes, but use
		// original value as defensive fallback.
		coercedVal, err := v.coerceValueWithRecovery(fieldVal, prop.Constraint())
		if err != nil {
			coercedVal = fieldVal
		}
		edgeProps[prop.Name()] = coercedVal
	}

	// Check for required edge properties
	for prop := range rel.Properties() {
		if prop.IsRequired() {
			if _, has := edgeProps[prop.Name()]; !has {
				issue := diag.NewIssue(
					diag.Error,
					ErrMissingRequired,
					fmt.Sprintf("missing required edge property %q", prop.Name()),
				).WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyPropertyName, prop.Name())
				withProvenance(issue, prov, targetPath.String())
				targetCollector.Collect(issue.Build())
			}
		}
	}

	// Merge issues into parent collector
	for issue := range targetCollector.Result().Issues() {
		collector.Collect(issue)
	}

	// P1-4: Check per-target collector (not shared collector) to decide success
	if targetCollector.HasErrors() {
		return nil
	}

	// Build ValidEdgeTarget
	targetKey := immutable.WrapKeyClone(pkComponents)
	var edgeProperties immutable.Properties
	if len(edgeProps) > 0 {
		edgeProperties = immutable.WrapPropertiesClone(edgeProps)
	}

	target := NewValidEdgeTarget(targetKey, edgeProperties)
	return &target
}

// provenancePathBuilder returns a path builder from provenance, or Root() if nil.
func provenancePathBuilder(prov *Provenance) path.Builder {
	if prov == nil {
		return path.Root()
	}
	return prov.Path()
}

// kindOf returns a human-readable type description for error messages.
func kindOf(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	default:
		return "unknown"
	}
}

// expectedShapeForRelation returns the expected JSON shape description for a relation.
// Used in error messages to indicate what shape was expected.
func expectedShapeForRelation(rel *schema.Relation) string {
	if rel.IsMany() {
		return "array"
	}
	return "object"
}

// toSliceOfAny converts val to []any if it's a slice.
// Handles both []any and typed slices using reflection.
func toSliceOfAny(val any) ([]any, bool) {
	if val == nil {
		return nil, false
	}
	if slice, ok := val.([]any); ok {
		return slice, true
	}
	// Use reflection for typed slices
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	result := make([]any, rv.Len())
	for i := range rv.Len() {
		result[i] = rv.Index(i).Interface()
	}
	return result, true
}

// toMapOfAny converts val to map[string]any if it's a string-keyed map.
// Handles both map[string]any and typed maps using reflection.
func toMapOfAny(val any) (map[string]any, bool) {
	if val == nil {
		return nil, false
	}
	if m, ok := val.(map[string]any); ok {
		return m, true
	}
	// Use reflection for typed maps
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Map {
		return nil, false
	}
	if rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	result := make(map[string]any, rv.Len())
	for _, key := range rv.MapKeys() {
		result[key.String()] = rv.MapIndex(key).Interface()
	}
	return result, true
}

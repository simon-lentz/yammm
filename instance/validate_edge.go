package instance

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
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
	in inputKeys,
	collector *diag.Collector,
	prov *location.Provenance,
	base path.Builder,
) map[string]*ValidEdgeData {
	edges := make(map[string]*ValidEdgeData)

	for rel := range typ.AllAssociations() {
		if err := ctx.Err(); err != nil {
			return edges
		}

		// Absent is valid for an association: presence and requiredness are
		// graph.Check's question, reported there as E_UNRESOLVED_REQUIRED.
		inputKey, rawValue, hasValue := v.lookupRelationInput(rel, in, collector, prov, base)
		if !hasValue {
			continue
		}

		edgeData := v.validateEdgeData(ctx, rel, rawValue, collector, prov, base.Key(inputKey))
		if edgeData != nil {
			edges[rel.Name()] = edgeData
		}
	}

	if len(edges) == 0 {
		return nil
	}
	return edges
}

// validateEdgeData validates a present association value at relPath, the
// input key's location.
func (v *Validator) validateEdgeData(
	ctx context.Context,
	rel *schema.Relation,
	rawValue any,
	collector *diag.Collector,
	prov *location.Provenance,
	relPath path.Builder,
) *ValidEdgeData {
	// An explicit null is a shape error whatever the multiplicity.
	if rawValue == nil {
		collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": null is not a valid edge value",
			expectedShapeForRelation(rel), "null", prov, relPath))
		return nil
	}

	if rel.IsMany() {
		arr, ok := toSliceOfAny(rawValue)
		if !ok {
			collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected array, got "+kindOf(rawValue),
				"array", kindOf(rawValue), prov, relPath))
			return nil
		}

		// An empty array is valid at the instance layer for every association;
		// a required association's empty array is graph.Check's E_UNRESOLVED_REQUIRED.
		targets := make([]ValidEdgeTarget, 0, len(arr))
		for i, elem := range arr {
			if err := ctx.Err(); err != nil {
				return nil
			}
			if target := v.validateEdgeTarget(rel, elem, collector, prov, relPath.Index(i)); target != nil {
				targets = append(targets, *target)
			}
		}
		return NewValidEdgeData(targets)
	}

	obj, ok := toMapOfAny(rawValue)
	if !ok {
		collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected object, got "+kindOf(rawValue),
			"object", kindOf(rawValue), prov, relPath))
		return nil
	}
	target := v.validateEdgeTarget(rel, obj, collector, prov, relPath)
	if target == nil {
		return nil
	}
	return NewValidEdgeData([]ValidEdgeTarget{*target})
}

// shapeMismatch builds an E_EDGE_SHAPE_MISMATCH carrying the expected and got
// shapes as details on every arm, so a structured consumer reads them the
// same way whichever arm fired.
func shapeMismatch(rel *schema.Relation, message, expected, got string, prov *location.Provenance, at path.Builder) diag.Issue {
	issue := diag.NewIssue(diag.Error, ErrEdgeShapeMismatch, message).
		WithExpectedGot(expected, got)
	withProvenance(issue, prov, at.String()).
		WithDetail(diag.DetailKeyJSONField, rel.FieldName())
	return issue.Build()
}

// coercionIssue builds the diagnostic a failed coercion owes, classifying it the
// way the node-property path does: a recovered panic is Fatal E_INTERNAL
// carrying its stack, anything else an ordinary E_TYPE_MISMATCH.
func coercionIssue(err error, message string) *diag.IssueBuilder {
	if internalErr, ok := errors.AsType[*InternalError](err); ok {
		return diag.NewIssue(diag.Fatal, diag.E_INTERNAL, internalErr.Error()).
			WithDetail(diag.DetailKeyStackTrace, internalErr.Stack)
	}
	return diag.NewIssue(diag.Error, ErrTypeMismatch, message+": coercion error: "+err.Error())
}

// edgeObject is one edge target's input object with each key resolved to the
// member it names: a foreign-key field of the target type, an edge property
// of the relation, or neither. Resolution applies the one fold rule the node
// path applies: exact first, then under the default mode the ASCII fold, with
// an exact match never a collision and two folds to one member a collision
// reported at the object.
type edgeObject struct {
	obj      map[string]any
	fk       map[string]string           // expected FK field → input key
	props    map[*schema.Property]string // edge property → input key
	unknown  []string                    // input keys naming no member, sorted
	shadowed map[string]string           // unknown key → the member an exact match already claimed
}

func (v *Validator) resolveEdgeObject(rel *schema.Relation, targetType *schema.Type, obj map[string]any, collector *diag.Collector, prov *location.Provenance, targetPath path.Builder) edgeObject {
	eo := edgeObject{
		obj:      obj,
		fk:       make(map[string]string),
		props:    make(map[*schema.Property]string),
		shadowed: make(map[string]string),
	}
	names := slices.Sorted(maps.Keys(obj))

	fkByLower := make(map[string]string)
	for pk := range targetType.PrimaryKeys() {
		field := fkPrefix + pk.Name()
		fkByLower[strings.ToLower(field)] = field
	}

	// Exact pass.
	var unclaimed []string
	for _, name := range names {
		if _, isFK := fkByLower[strings.ToLower(name)]; isFK && fkByLower[strings.ToLower(name)] == name {
			eo.fk[name] = name
			continue
		}
		if p, ok := rel.Property(name); ok {
			eo.props[p] = name
			continue
		}
		unclaimed = append(unclaimed, name)
	}
	if v.cfg.strictPropertyNames {
		eo.unknown = unclaimed
		return eo
	}

	// Fold pass over the unclaimed keys: member → the keys folding to it.
	type member struct {
		fk   string
		prop *schema.Property
	}
	folded := make(map[member][]string)
	var order []member
	for _, name := range unclaimed {
		lower, ok := foldKey(name)
		if !ok {
			eo.unknown = append(eo.unknown, name)
			continue
		}
		var m member
		if field, isFK := fkByLower[lower]; isFK {
			if _, claimed := eo.fk[field]; claimed {
				eo.unknown = append(eo.unknown, name)
				eo.shadowed[name] = field
				continue
			}
			m = member{fk: field}
		} else if p, isProp := rel.PropertyFold(lower); isProp {
			if _, claimed := eo.props[p]; claimed {
				eo.unknown = append(eo.unknown, name)
				eo.shadowed[name] = p.Name()
				continue
			}
			m = member{prop: p}
		} else {
			eo.unknown = append(eo.unknown, name)
			continue
		}
		if _, seen := folded[m]; !seen {
			order = append(order, m)
		}
		folded[m] = append(folded[m], name)
	}
	for _, m := range order {
		keys := folded[m]
		if len(keys) > 1 {
			what := "edge property " + strconv.Quote(memberName(m.fk, m.prop))
			if m.fk != "" {
				what = "foreign-key field " + strconv.Quote(m.fk)
			}
			issue := diag.NewIssue(
				diag.Error,
				ErrCaseFoldCollision,
				fmt.Sprintf("multiple input fields %v fold to %s", keys, what),
			).WithDetail(diag.DetailKeyRelationName, rel.Name())
			if m.prop != nil {
				issue.WithDetail(diag.DetailKeyPropertyName, m.prop.Name())
			}
			withProvenance(issue, prov, targetPath.String())
			collector.Collect(issue.Build())
			continue
		}
		if m.fk != "" {
			eo.fk[m.fk] = keys[0]
		} else {
			eo.props[m.prop] = keys[0]
		}
	}
	slices.Sort(eo.unknown)
	return eo
}

func memberName(fk string, p *schema.Property) string {
	if p != nil {
		return p.Name()
	}
	return fk
}

// validateEdgeTarget validates a single edge target object at targetPath.
// Uses per-target collector isolation to ensure each target is evaluated independently.
func (v *Validator) validateEdgeTarget(
	rel *schema.Relation,
	elem any,
	collector *diag.Collector,
	prov *location.Provenance,
	targetPath path.Builder,
) *ValidEdgeTarget {
	// Use per-target collector to avoid coupling between targets.
	// Use unlimited collector since issues will be merged into the parent
	// collector which handles the actual limit.
	targetCollector := diag.NewCollectorUnlimited()
	defer func() {
		for issue := range targetCollector.Result().Issues() {
			collector.Collect(issue)
		}
	}()

	obj, ok := toMapOfAny(elem)
	if !ok {
		targetCollector.Collect(shapeMismatch(rel, "expected object for edge target, got "+kindOf(elem),
			"object", kindOf(elem), prov, targetPath))
		return nil
	}

	// Get target type to extract PK fields, resolved by the relation's
	// completion-recorded absolute identity so relations declared on imported
	// types or inherited cross-schema resolve against the true target.
	targetType, found := resolveRelationTarget(v.schema, rel)
	if !found {
		// Unreachable through public construction (every entry point rejects
		// a dangling target); kept as defense for models built outside them.
		issue := diag.NewIssue(
			diag.Error,
			ErrTypeNotFound,
			fmt.Sprintf("edge target type %q not found", rel.Target().String()),
		).WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyTargetType, rel.Target().String())
		withProvenance(issue, prov, targetPath.String())
		targetCollector.Collect(issue.Build())
		return nil
	}

	eo := v.resolveEdgeObject(rel, targetType, obj, targetCollector, prov, targetPath)

	// Extract FK fields and build target key.
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
		inputKey, hasFKField := eo.fk[fkFieldName]

		// Check key existence (not value)
		if !hasFKField {
			missingFKFields = append(missingFKFields, fkFieldName)
			continue
		}

		// Key exists - track as present regardless of value
		presentFKFields = append(presentFKFields, fkFieldName)
		val := obj[inputKey]
		fkPath := targetPath.Key(inputKey).String()

		// Handle null value — present but invalid per spec.
		if val == nil {
			expectedType := strings.ToLower(schema.ResolveAlias(pk.Constraint()).Kind().String())
			issue := diag.NewIssue(
				diag.Error,
				ErrTypeMismatch,
				fmt.Sprintf("FK field %q: expected %s, got null", fkFieldName, expectedType),
			).WithDetails(diag.RelationField(rel.Name(), fkFieldName)...).
				WithExpectedGot(expectedType, "null")
			withProvenance(issue, prov, fkPath)
			targetCollector.Collect(issue.Build())
			continue
		}

		// Validate FK type against PK constraint.
		if err := v.checkValueWithRecovery(val, pk.Constraint()); err != nil {
			code := ErrTypeMismatch
			if checkErr, ok := errors.AsType[*eval.CheckError](err); ok && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("FK field %q: %s", fkFieldName, err.Error()),
			).WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			targetCollector.Collect(issue.Build())
			continue
		}

		// Coerce and collect valid component.
		coercedVal, err := v.coerceValueWithRecovery(val, pk.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("FK field %q", fkFieldName)).
				WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			targetCollector.Collect(issue.Build())
			continue
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
		return nil
	} else if presentCount < expectedCount && expectedCount > 1 {
		// Partial composite FK - some present, some missing
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
		return nil
	}

	// present == expected: all FK fields present
	if len(pkComponents) < expectedCount {
		// Some fields had validation errors - already emitted
		return nil
	}

	// Unknown keys in the edge object.
	if !v.cfg.allowUnknownFields {
		for _, fieldName := range eo.unknown {
			issue := diag.NewIssue(
				diag.Error,
				ErrUnknownEdgeField,
				fmt.Sprintf("unknown field in edge object: %q", fieldName),
			).WithDetails(diag.RelationField(rel.Name(), fieldName)...)
			if member, ok := eo.shadowed[fieldName]; ok {
				issue.WithDetail(diag.DetailKeyReason, "case_fold_shadowed").
					WithDetail(diag.DetailKeyPropertyName, member)
			}
			withProvenance(issue, prov, targetPath.Key(fieldName).String())
			targetCollector.Collect(issue.Build())
		}
	}

	// Edge properties. An explicit null is the absent case, exactly as it is
	// for a node property: the required check below reports it, and an
	// optional one is dropped.
	edgeProps := make(map[string]any)
	present := make(map[string]bool)
	for prop := range rel.Properties() {
		fieldName, has := eo.props[prop]
		if !has {
			continue
		}
		fieldVal := obj[fieldName]
		if fieldVal == nil {
			continue
		}
		present[prop.Name()] = true
		propPath := targetPath.Key(fieldName).String()

		if err := v.checkValueWithRecovery(fieldVal, prop.Constraint()); err != nil {
			code := ErrTypeMismatch
			if checkErr, ok := errors.AsType[*eval.CheckError](err); ok && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("edge property %q: %s", prop.Name(), err.Error()),
			).WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			targetCollector.Collect(issue.Build())
			continue
		}

		coercedVal, err := v.coerceValueWithRecovery(fieldVal, prop.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("edge property %q", prop.Name())).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			targetCollector.Collect(issue.Build())
			continue
		}
		edgeProps[prop.Name()] = coercedVal
	}

	// A required edge property is missing when no key supplied a value for
	// it; a supplied value that failed its check was reported above and is
	// not also missing.
	for prop := range rel.Properties() {
		if prop.IsRequired() && !present[prop.Name()] {
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

	// Check per-target collector (not shared collector) to decide success.
	if targetCollector.HasErrors() {
		return nil
	}

	targetKey := immutable.WrapKey(pkComponents, immutable.WithClone(true))
	var edgeProperties immutable.Properties
	if len(edgeProps) > 0 {
		edgeProperties = immutable.WrapProperties(edgeProps, immutable.WithClone(true))
	}

	target := NewValidEdgeTarget(targetKey, edgeProperties)
	return &target
}

// kindOf returns a human-readable shape name for a value: the JSON shape for
// the shapes JSON produces, and the reflect kind's shape for a typed
// container a Go caller sent.
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
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Map, reflect.Struct:
		return "object"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return reflect.TypeOf(v).String()
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

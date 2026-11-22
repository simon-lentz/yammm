package instance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// fkPrefix is the prefix for foreign key fields in edge objects.
// FK fields are named _target_<pk_name> where pk_name is the target type's PK field.
const fkPrefix = "_target_"

// validateEdges validates all association relations for an instance.
// Returns a map of relation name -> ValidEdgeData.
func (v *Validator) validateEdges(
	ctx context.Context,
	typ schema.Type,
	props map[string]any,
	collector *diag.Collector,
	prov Provenance,
	base path.Path,
) map[string]ValidEdgeData {
	var edges map[string]ValidEdgeData

	for rel := range typ.AllAssociations() {
		if err := ctx.Err(); err != nil {
			return edges
		}

		// Absent is valid for an association: presence and requiredness are
		// graph.Check's question, reported there as E_UNRESOLVED_REQUIRED.
		raw := props[rel.FieldName()]
		if value.Absent(raw) {
			continue
		}

		edgeData := v.validateEdgeData(ctx, rel, raw, collector, prov, base.Key(rel.FieldName()))
		if edges == nil {
			edges = make(map[string]ValidEdgeData)
		}
		// Zero data comes with an error or a cancellation, and on each the caller
		// discards the map.
		edges[rel.Name()] = edgeData
	}

	return edges
}

// validateEdgeData validates a present association value at relPath, the
// input key's location.
func (v *Validator) validateEdgeData(
	ctx context.Context,
	rel schema.Relation,
	rawValue any,
	collector *diag.Collector,
	prov Provenance,
	relPath path.Path,
) ValidEdgeData {
	if rel.IsMany() {
		arr, ok := toSliceOfAny(rawValue)
		if !ok {
			collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected array, got "+kindOf(rawValue),
				"array", kindOf(rawValue), prov, relPath))
			return ValidEdgeData{}
		}

		// An empty array is valid at the instance layer for every association;
		// a required association's empty array is graph.Check's E_UNRESOLVED_REQUIRED.
		targets := make([]ValidEdgeTarget, 0, len(arr))
		// Two listings of one target can carry different edge properties, so a
		// repeat is refused rather than one kept: a graph holds one edge per target.
		var listedAt map[string]int
		for i, elem := range arr {
			if err := ctx.Err(); err != nil {
				return ValidEdgeData{}
			}
			target := v.validateEdgeTarget(rel, elem, collector, prov, relPath.Index(i))
			if target == nil {
				continue
			}
			key := target.targetKey.String()
			if first, repeated := listedAt[key]; repeated {
				issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_EDGE,
					fmt.Sprintf("edge %s: target key %s listed at indices %d and %d", rel.Name(), diag.CutValue(key), first, i)).
					WithDetails(diag.PathRelation(rel.Name(), rel.FieldName())...).
					WithDetail(diag.DetailKeyTargetPK, key)
				withProvenance(issue, prov, relPath.Index(i).String())
				collector.Collect(issue.Build())
				continue
			}
			if listedAt == nil {
				listedAt = make(map[string]int, len(arr))
			}
			listedAt[key] = i
			targets = append(targets, *target)
		}
		return ValidEdgeData{&validEdgeDataState{targets: targets}}
	}

	obj, ok := toMapOfAny(rawValue)
	if !ok {
		collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected object, got "+kindOf(rawValue),
			"object", kindOf(rawValue), prov, relPath))
		return ValidEdgeData{}
	}
	target := v.validateEdgeTarget(rel, obj, collector, prov, relPath)
	if target == nil {
		return ValidEdgeData{}
	}
	return ValidEdgeData{&validEdgeDataState{targets: []ValidEdgeTarget{*target}}}
}

// shapeMismatch builds an E_EDGE_SHAPE_MISMATCH carrying the expected and got
// shapes as details on every arm, so a structured consumer reads them the
// same way whichever arm fired.
func shapeMismatch(rel schema.Relation, message, expected, got string, prov Provenance, at path.Path) diag.Issue {
	issue := diag.NewIssue(diag.Error, diag.E_EDGE_SHAPE_MISMATCH, message).
		WithExpectedGot(expected, got)
	withProvenance(issue, prov, at.String()).
		WithDetails(diag.PathRelation(rel.Name(), rel.FieldName())...)
	return issue.Build()
}

// checkIssue classifies a check error the way every check site must: a
// recovered panic is Fatal E_INTERNAL with its stack, a constraint failure is
// E_CONSTRAINT_FAIL, anything else E_TYPE_MISMATCH. Two edge sites once
// classified only the CheckError and reported a recovered panic as an
// ordinary type mismatch.
func checkIssue(err error, message string) *diag.IssueBuilder {
	if internalErr, ok := errors.AsType[*InternalError](err); ok {
		return diag.NewIssue(diag.Fatal, diag.E_INTERNAL, internalErr.Error()).
			WithDetail(diag.DetailKeyStackTrace, internalErr.Stack)
	}
	code := diag.E_TYPE_MISMATCH
	if checkErr, ok := errors.AsType[*value.CheckError](err); ok && checkErr.Kind == value.KindConstraintFail {
		code = diag.E_CONSTRAINT_FAIL
	}
	return diag.NewIssue(diag.Error, code, message+": "+err.Error())
}

// coercionIssue builds the diagnostic a failed coercion owes, classifying it the
// way the node-property path does: a recovered panic is Fatal E_INTERNAL
// carrying its stack, anything else an ordinary E_TYPE_MISMATCH.
func coercionIssue(err error, message string) *diag.IssueBuilder {
	if internalErr, ok := errors.AsType[*InternalError](err); ok {
		return diag.NewIssue(diag.Fatal, diag.E_INTERNAL, internalErr.Error()).
			WithDetail(diag.DetailKeyStackTrace, internalErr.Stack)
	}
	return diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, message+": coercion error: "+err.Error())
}

// validateEdgeTarget validates a single edge target object at targetPath; a
// target that fails is nil. A foreign-key field holding a [value.Resolved]
// gives the key its component unjudged.
func (v *Validator) validateEdgeTarget(
	rel schema.Relation,
	elem any,
	collector *diag.Collector,
	prov Provenance,
	targetPath path.Path,
) *ValidEdgeTarget {
	// A per-target collector keeps one target's issues from coupling to
	// another's; unlimited, because the parent applies the cap on merge.
	targetCollector := diag.NewCollector(diag.NoLimit)
	defer func() { collector.Merge(targetCollector.Result()) }()

	obj, ok := toMapOfAny(elem)
	if !ok {
		targetCollector.Collect(shapeMismatch(rel, "expected object for edge target, got "+kindOf(elem),
			"object", kindOf(elem), prov, targetPath))
		return nil
	}

	// Get target type to extract PK fields, resolved by the relation's
	// completion-recorded absolute identity so relations declared on imported
	// types or inherited cross-schema resolve against the true target.
	targetType, _ := resolveRelationTarget(v.schema, rel)

	pkFields := slices.Collect(targetType.PrimaryKeys())
	allExpectedFKFields := make([]string, len(pkFields))
	for i, pk := range pkFields {
		allExpectedFKFields[i] = fkPrefix + pk.Name()
	}

	// Unknown keys first, as the node path reports them: a typo'd key is
	// unknown whether or not the key it was meant to be is present.
	for _, fieldName := range slices.Sorted(maps.Keys(obj)) {
		if slices.Contains(allExpectedFKFields, fieldName) {
			continue
		}
		if _, ok := rel.Property(fieldName); ok {
			continue
		}
		issue := diag.NewIssue(
			diag.Error,
			diag.E_UNKNOWN_EDGE_FIELD,
			"unknown field in edge object: "+diag.QuoteValue(fieldName),
		).WithDetails(diag.RelationField(rel.Name(), fieldName)...)
		withProvenance(issue, prov, targetPath.Key(fieldName).String())
		targetCollector.Collect(issue.Build())
	}

	pkComponents := make([]any, 0, len(pkFields))
	presentFKFields := make([]string, 0, len(pkFields)) // Track key existence
	missingFKFields := make([]string, 0, len(pkFields)) // Track truly absent keys

	for i, pk := range pkFields {
		fkFieldName := allExpectedFKFields[i]
		val, hasFKField := obj[fkFieldName]

		// Check key existence (not value)
		if !hasFKField {
			missingFKFields = append(missingFKFields, fkFieldName)
			continue
		}

		// Key exists - track as present regardless of value
		presentFKFields = append(presentFKFields, fkFieldName)
		fkPath := targetPath.Key(fkFieldName).String()
		if r, ok := val.(value.Resolved); ok {
			pkComponents = append(pkComponents, r.Value)
			continue
		}
		// Present but null is invalid, whatever the constraint.
		if value.Absent(val) {
			expectedType := strings.ToLower(schema.ResolveAlias(pk.Constraint()).Kind().String())
			issue := diag.NewIssue(
				diag.Error,
				diag.E_TYPE_MISMATCH,
				fmt.Sprintf("FK field %q: expected %s, got null", fkFieldName, expectedType),
			).WithDetails(diag.RelationField(rel.Name(), fkFieldName)...).
				WithExpectedGot(expectedType, "null")
			withProvenance(issue, prov, fkPath)
			targetCollector.Collect(issue.Build())
			continue
		}

		// A Fatal ends the target, as it ends the instance at the node site.
		if err := checkValueRecovering(val, pk.Constraint()); err != nil {
			issue := checkIssue(err, fmt.Sprintf("FK field %q", fkFieldName)).
				WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil
			}
			continue
		}

		coercedVal, err := coerceValueRecovering(val, pk.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("FK field %q", fkFieldName)).
				WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil
			}
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
			diag.E_MISSING_FK_TARGET,
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
			diag.E_PARTIAL_COMPOSITE_FK,
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

	// Edge properties. An explicit null is the absent case, exactly as it is
	// for a node property: the required check below reports it, and an
	// optional one is dropped.
	edgeProps := make(map[string]any)
	present := make(map[string]bool)
	for prop := range rel.Properties() {
		fieldVal := obj[prop.Name()]
		if value.Absent(fieldVal) {
			continue
		}
		present[prop.Name()] = true
		propPath := targetPath.Key(prop.Name()).String()

		if err := checkValueRecovering(fieldVal, prop.Constraint()); err != nil {
			issue := checkIssue(err, fmt.Sprintf("edge property %q", prop.Name())).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil
			}
			continue
		}

		coercedVal, err := coerceValueRecovering(fieldVal, prop.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("edge property %q", prop.Name())).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil
			}
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
				diag.E_MISSING_REQUIRED,
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

	targetKey := immutable.WrapKey(pkComponents)
	var edgeProperties immutable.Properties
	if len(edgeProps) > 0 {
		edgeProperties = immutable.WrapProperties(edgeProps)
	}

	return &ValidEdgeTarget{targetKey: targetKey, properties: edgeProperties}
}

// kindOf returns a human-readable shape name for a value: the JSON shape for
// the shapes JSON produces and for the containers [toMapOfAny] and
// [toSliceOfAny] read, the reflect kind's shape for a typed container a Go
// caller sent, and the type's name for any other struct.
func kindOf(v any) string {
	if value.Absent(v) {
		return "null"
	}
	switch x := v.(type) {
	case map[string]any, immutable.Properties:
		return "object"
	case []any, immutable.Slice:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case json.Number:
		if value.IsNumberLiteral(x) {
			return "number"
		}
		return "json.Number"
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Map:
		// A map keyed by no string is no object: toMapOfAny refuses it.
		if reflect.TypeOf(v).Key().Kind() == reflect.String {
			return "object"
		}
	case reflect.Slice:
		return "array"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	}
	return reflect.TypeOf(v).String()
}

// toSliceOfAny reads val as an array as every list position reads one
// ([value.ListElems]): a []any, an [immutable.Slice] or a typed slice. A value
// that holds none is no array: it is the null [kindOf] names.
func toSliceOfAny(val any) ([]any, bool) {
	return value.ListElems(val)
}

// toMapOfAny converts val to map[string]any if it's a string-keyed map: a
// map[string]any, an [immutable.Properties] with each entry
// unwrapped, or a typed map. A value that
// holds none, a nil map included, is no object: it is the null [kindOf] names.
func toMapOfAny(val any) (map[string]any, bool) {
	if value.Absent(val) {
		return nil, false
	}
	switch m := val.(type) {
	case map[string]any:
		return m, true
	case immutable.Properties:
		return unwrapEntries(m.Len(), m.All()), true
	}
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

// unwrapEntries copies an immutable map's entries, each unwrapped as
// [immutable.Value.Any] holds it: one level, as [value.ListElems] reads a
// list, so a field no reader reads is never walked.
func unwrapEntries(n int, entries iter.Seq2[string, immutable.Value]) map[string]any {
	out := make(map[string]any, n)
	for k, v := range entries {
		out[k] = v.Any()
	}
	return out
}

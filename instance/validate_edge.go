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
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// fkPrefix is the prefix for foreign key fields in edge objects.
// FK fields are named _target_<pk_name> where pk_name is the target type's PK field.
const fkPrefix = "_target_"

// validateEdges validates all association relations for an instance.
// Returns a map of relation name -> ValidEdgeData, and whether a target holds
// a [Refused] value.
func (v *Validator) validateEdges(
	ctx context.Context,
	typ *schema.Type,
	rels map[*schema.Relation]relationInput,
	collector *diag.Collector,
	prov *location.Provenance,
	base path.Builder,
) (map[string]*ValidEdgeData, bool) {
	var edges map[string]*ValidEdgeData
	refused := false

	for rel := range typ.AllAssociations() {
		if err := ctx.Err(); err != nil {
			return edges, refused
		}

		// Absent is valid for an association: presence and requiredness are
		// graph.Check's question, reported there as E_UNRESOLVED_REQUIRED.
		in := rels[rel]
		if in.state != relationPresent {
			// A collided slot has nothing to read. The member index already
			// reported it, before any pass ran.
			continue
		}

		edgeData, targetRefused := v.validateEdgeData(ctx, rel, in.value, collector, prov, base.Key(in.key))
		refused = refused || targetRefused
		if edgeData != nil {
			if edges == nil {
				edges = make(map[string]*ValidEdgeData)
			}
			edges[rel.Name()] = edgeData
		}
	}

	return edges, refused
}

// validateEdgeData validates a present association value at relPath, the
// input key's location, and reports whether a target holds a [Refused] value.
func (v *Validator) validateEdgeData(
	ctx context.Context,
	rel *schema.Relation,
	rawValue any,
	collector *diag.Collector,
	prov *location.Provenance,
	relPath path.Builder,
) (*ValidEdgeData, bool) {
	// An explicit null is a shape error whatever the multiplicity.
	if isAbsent(rawValue) {
		collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": null is not a valid edge value",
			expectedShapeForRelation(rel), "null", prov, relPath))
		return nil, false
	}

	if rel.IsMany() {
		arr, ok := toSliceOfAny(rawValue)
		if !ok {
			collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected array, got "+kindOf(rawValue),
				"array", kindOf(rawValue), prov, relPath))
			return nil, false
		}

		// An empty array is valid at the instance layer for every association;
		// a required association's empty array is graph.Check's E_UNRESOLVED_REQUIRED.
		targets := make([]ValidEdgeTarget, 0, len(arr))
		refused := false
		for i, elem := range arr {
			if err := ctx.Err(); err != nil {
				return nil, refused
			}
			target, targetRefused := v.validateEdgeTarget(rel, elem, collector, prov, relPath.Index(i))
			refused = refused || targetRefused
			if target != nil {
				targets = append(targets, *target)
			}
		}
		if refused {
			return nil, true
		}
		return NewValidEdgeData(targets), false
	}

	obj, ok := toMapOfAny(rawValue)
	if !ok {
		collector.Collect(shapeMismatch(rel, "edge "+rel.Name()+": expected object, got "+kindOf(rawValue),
			"object", kindOf(rawValue), prov, relPath))
		return nil, false
	}
	target, refused := v.validateEdgeTarget(rel, obj, collector, prov, relPath)
	if target == nil {
		return nil, refused
	}
	return NewValidEdgeData([]ValidEdgeTarget{*target}), false
}

// shapeMismatch builds an E_EDGE_SHAPE_MISMATCH carrying the expected and got
// shapes as details on every arm, so a structured consumer reads them the
// same way whichever arm fired.
func shapeMismatch(rel *schema.Relation, message, expected, got string, prov *location.Provenance, at path.Builder) diag.Issue {
	issue := diag.NewIssue(diag.Error, ErrEdgeShapeMismatch, message).
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
	code := ErrTypeMismatch
	if checkErr, ok := errors.AsType[*eval.CheckError](err); ok && checkErr.Kind == eval.KindConstraintFail {
		code = ErrConstraintFail
	}
	return diag.NewIssue(diag.Error, code, message+": "+err.Error())
}

// isAbsent reports whether v is the absent value: a value that holds none
// ([value.Absent]), bare or at some depth — a typed nil pointer is the shape
// adapter/gogen emits for an absent optional scalar, and a nil slice for an
// absent optional list. Every writer writes such a value as null ([ScalarOf]).
func isAbsent(v any) bool {
	return value.Absent(v)
}

// isRefused reports whether v is a [Refused] value: one its producer reported,
// which the validator judges no further.
func isRefused(v any) bool {
	_, ok := v.(Refused)
	return ok
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
	// collidedFK and collidedProps hold each member a collision claims. The
	// collision is that member's one issue: it is present, and judged no further.
	collidedFK    map[string]bool
	collidedProps map[*schema.Property]bool
}

func (v *Validator) resolveEdgeObject(rel *schema.Relation, targetType *schema.Type, obj map[string]any, collector *diag.Collector, prov *location.Provenance, targetPath path.Builder) edgeObject {
	claims := claimEdgeObject(rel, targetType, slices.Sorted(maps.Keys(obj)), v.cfg.strictPropertyNames)
	eo := edgeObject{obj: obj, fk: claims.fk, props: claims.props, unknown: claims.unknown, shadowed: claims.shadowed}
	for _, c := range claims.collisions {
		if c.fk != "" {
			if eo.collidedFK == nil {
				eo.collidedFK = make(map[string]bool)
			}
			eo.collidedFK[c.fk] = true
		} else {
			if eo.collidedProps == nil {
				eo.collidedProps = make(map[*schema.Property]bool)
			}
			eo.collidedProps[c.prop] = true
		}
		what := "edge property " + strconv.Quote(memberName(c.fk, c.prop))
		if c.fk != "" {
			what = "foreign-key field " + strconv.Quote(c.fk)
		}
		issue := diag.NewIssue(
			diag.Error,
			ErrCaseFoldCollision,
			fmt.Sprintf("multiple input fields %s fold to %s", nameList(c.keys), what),
		).WithDetail(diag.DetailKeyRelationName, rel.Name())
		if c.prop != nil {
			issue.WithDetail(diag.DetailKeyPropertyName, c.prop.Name())
		}
		withProvenance(issue, prov, targetPath.String())
		collector.Collect(issue.Build())
	}
	return eo
}

func memberName(fk string, p *schema.Property) string {
	if p != nil {
		return p.Name()
	}
	return fk
}

// validateEdgeTarget validates a single edge target object at targetPath, and
// reports whether a foreign-key field or an edge property holds a [Refused]
// value. A refused target is nil and reports nothing for that value.
func (v *Validator) validateEdgeTarget(
	rel *schema.Relation,
	elem any,
	collector *diag.Collector,
	prov *location.Provenance,
	targetPath path.Builder,
) (*ValidEdgeTarget, bool) {
	// A per-target collector keeps one target's issues from coupling to
	// another's; unlimited, because the parent applies the cap on merge.
	targetCollector := diag.NewCollectorUnlimited()
	defer func() { collector.Merge(targetCollector.Result()) }()

	obj, ok := toMapOfAny(elem)
	if !ok {
		targetCollector.Collect(shapeMismatch(rel, "expected object for edge target, got "+kindOf(elem),
			"object", kindOf(elem), prov, targetPath))
		return nil, false
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
		return nil, false
	}

	eo := v.resolveEdgeObject(rel, targetType, obj, targetCollector, prov, targetPath)

	pkFields := targetType.PrimaryKeysSlice()
	allExpectedFKFields := make([]string, len(pkFields))
	for i, pk := range pkFields {
		allExpectedFKFields[i] = fkPrefix + pk.Name()
	}

	// Unknown keys first, as the node path reports them: a typo'd key is
	// unknown whether or not the key it was meant to be is present.
	if !v.cfg.allowUnknownFields {
		for _, fieldName := range eo.unknown {
			issue := diag.NewIssue(
				diag.Error,
				ErrUnknownEdgeField,
				"unknown field in edge object: "+diag.QuoteValue(fieldName),
			).WithDetails(diag.RelationField(rel.Name(), fieldName)...)
			if member, ok := eo.shadowed[fieldName]; ok {
				issue.WithDetail(diag.DetailKeyReason, "case_fold_shadowed").
					WithDetail(diag.DetailKeyPropertyName, member)
			}
			withProvenance(issue, prov, targetPath.Key(fieldName).String())
			targetCollector.Collect(issue.Build())
		}
	}

	pkComponents := make([]any, 0, len(pkFields))
	presentFKFields := make([]string, 0, len(pkFields)) // Track key existence
	missingFKFields := make([]string, 0, len(pkFields)) // Track truly absent keys
	refused := false

	for i, pk := range pkFields {
		fkFieldName := allExpectedFKFields[i]
		inputKey, hasFKField := eo.fk[fkFieldName]

		// Check key existence (not value)
		if !hasFKField {
			if eo.collidedFK[fkFieldName] {
				presentFKFields = append(presentFKFields, fkFieldName)
				continue
			}
			missingFKFields = append(missingFKFields, fkFieldName)
			continue
		}

		// Key exists - track as present regardless of value
		presentFKFields = append(presentFKFields, fkFieldName)
		val := obj[inputKey]
		fkPath := targetPath.Key(inputKey).String()
		if isRefused(val) {
			refused = true
			continue
		}

		// Present but null is invalid, whatever the constraint.
		if isAbsent(val) {
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

		// A Fatal ends the target, as it ends the instance at the node site.
		if err := v.checkValueWithRecovery(val, pk.Constraint()); err != nil {
			issue := checkIssue(err, fmt.Sprintf("FK field %q", fkFieldName)).
				WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil, refused
			}
			continue
		}

		coercedVal, err := v.coerceValueWithRecovery(val, pk.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("FK field %q", fkFieldName)).
				WithDetails(diag.RelationField(rel.Name(), fkFieldName)...)
			withProvenance(issue, prov, fkPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil, refused
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
			ErrMissingFKTarget,
			"missing FK field(s): "+expectedStr,
		).WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyExpected, expectedStr)
		withProvenance(issue, prov, targetPath.String())
		targetCollector.Collect(issue.Build())
		return nil, refused
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
		return nil, refused
	}

	// present == expected: all FK fields present
	if len(pkComponents) < expectedCount {
		// Some fields had validation errors - already emitted
		return nil, refused
	}

	// Edge properties. An explicit null is the absent case, exactly as it is
	// for a node property: the required check below reports it, and an
	// optional one is dropped.
	edgeProps := make(map[string]any)
	present := make(map[string]bool)
	for prop := range rel.Properties() {
		fieldName, has := eo.props[prop]
		if !has {
			if eo.collidedProps[prop] {
				present[prop.Name()] = true
			}
			continue
		}
		fieldVal := obj[fieldName]
		if isRefused(fieldVal) {
			present[prop.Name()] = true
			refused = true
			continue
		}
		if isAbsent(fieldVal) {
			continue
		}
		present[prop.Name()] = true
		propPath := targetPath.Key(fieldName).String()

		if err := v.checkValueWithRecovery(fieldVal, prop.Constraint()); err != nil {
			issue := checkIssue(err, fmt.Sprintf("edge property %q", prop.Name())).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil, refused
			}
			continue
		}

		coercedVal, err := v.coerceValueWithRecovery(fieldVal, prop.Constraint())
		if err != nil {
			issue := coercionIssue(err, fmt.Sprintf("edge property %q", prop.Name())).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, propPath)
			built := issue.Build()
			targetCollector.Collect(built)
			if built.Severity() == diag.Fatal {
				return nil, refused
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
				ErrMissingRequired,
				fmt.Sprintf("missing required edge property %q", prop.Name()),
			).WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPropertyName, prop.Name())
			withProvenance(issue, prov, targetPath.String())
			targetCollector.Collect(issue.Build())
		}
	}

	// Check per-target collector (not shared collector) to decide success.
	if refused || targetCollector.HasErrors() {
		return nil, refused
	}

	targetKey := immutable.WrapKey(pkComponents, immutable.WithClone(true))
	var edgeProperties immutable.Properties
	if len(edgeProps) > 0 {
		edgeProperties = immutable.WrapProperties(edgeProps, immutable.WithClone(true))
	}

	target := NewValidEdgeTarget(targetKey, edgeProperties)
	return &target, false
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
	case map[string]any, immutable.Map[string], immutable.Properties:
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
		if immutable.IsNumberLiteral(x) {
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

// expectedShapeForRelation returns the expected JSON shape description for a relation.
// Used in error messages to indicate what shape was expected.
func expectedShapeForRelation(rel *schema.Relation) string {
	if rel.IsMany() {
		return "array"
	}
	return "object"
}

// toSliceOfAny reads val as an array as every list position reads one
// ([value.ListElems]): a []any, an [immutable.Slice] or a typed slice. A value
// that holds none is no array: it is the null [kindOf] names.
func toSliceOfAny(val any) ([]any, bool) {
	return value.ListElems(val)
}

// toMapOfAny converts val to map[string]any if it's a string-keyed map: a
// map[string]any, an [immutable.Map] or [immutable.Properties] with each entry
// unwrapped, or a typed map. A value that
// holds none, a nil map included, is no object: it is the null [kindOf] names.
func toMapOfAny(val any) (map[string]any, bool) {
	if value.Absent(val) {
		return nil, false
	}
	switch m := val.(type) {
	case map[string]any:
		return m, true
	case immutable.Map[string]:
		return unwrapEntries(m.Len(), m.Range()), true
	case immutable.Properties:
		return unwrapEntries(m.Len(), m.Range()), true
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
// [immutable.Value.Unwrap] holds it: one level, as [value.ListElems] reads a
// list, so a field no reader reads is never walked.
func unwrapEntries(n int, entries iter.Seq2[string, immutable.Value]) map[string]any {
	out := make(map[string]any, n)
	for k, v := range entries {
		out[k] = v.Unwrap()
	}
	return out
}

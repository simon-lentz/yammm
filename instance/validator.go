package instance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Validator validates raw instances against a schema.
//
// Validator is stateless and safe for concurrent use. Multiple goroutines
// may call Validate simultaneously with different inputs.
//
// All validation methods (Validate, ValidateOne, ValidateForComposition) require
// a non-nil context and will panic if passed nil. Use context.Background() for
// non-cancellable operations.
type Validator struct {
	schema    *schema.Schema
	cfg       *validatorConfig
	evaluator *eval.Evaluator
	checker   *eval.Checker
}

// NewValidator creates a new Validator for the given schema.
// Panics if schema is nil.
func NewValidator(s *schema.Schema, opts ...ValidatorOption) *Validator {
	if s == nil {
		panic("instance.NewValidator: nil schema")
	}
	cfg := applyOptions(opts)
	return &Validator{
		schema:    s,
		cfg:       cfg,
		evaluator: eval.NewEvaluator(),
		checker:   eval.NewChecker(cfg.valueRegistry),
	}
}

// Validate validates a batch of raw instances of the given type.
//
// Returns:
//   - valid: Successfully validated instances
//   - failures: Instances that failed validation with diagnostics
//   - err: System error (not validation errors)
//
// The triple-return semantics allow callers to:
//   - Process successful instances immediately
//   - Report failures with detailed diagnostics
//   - Handle system errors separately from validation errors
func (v *Validator) Validate(ctx context.Context, typeName string, raws []RawInstance) ([]*ValidInstance, []ValidationFailure, error) {
	if v == nil {
		return nil, nil, &InternalError{Kind: KindNilValidator, Cause: ErrNilValidator}
	}
	if ctx == nil {
		panic("instance.Validate: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}

	// Handle nil vs empty input per implementation checklist.
	if raws == nil {
		return nil, nil, nil
	}
	if len(raws) == 0 {
		return []*ValidInstance{}, nil, nil
	}

	var valid []*ValidInstance
	var failures []ValidationFailure

	for i := range raws {
		if err := ctx.Err(); err != nil {
			return valid, failures, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
		}

		instance, failure, err := v.ValidateOne(ctx, typeName, raws[i])
		if err != nil {
			return valid, failures, err
		}

		if instance != nil {
			valid = append(valid, instance)
		} else if failure != nil {
			failures = append(failures, *failure)
		}
	}

	return valid, failures, nil
}

// ValidateOne validates a single raw instance.
//
// Returns exactly one of:
//   - (valid, nil, nil) on success
//   - (nil, failure, nil) on validation failure
//   - (nil, nil, err) on system error
func (v *Validator) ValidateOne(ctx context.Context, typeName string, raw RawInstance) (*ValidInstance, *ValidationFailure, error) {
	if v == nil {
		return nil, nil, &InternalError{Kind: KindNilValidator, Cause: ErrNilValidator}
	}
	if ctx == nil {
		panic("instance.ValidateOne: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}

	// Resolve type
	typ, err := v.resolveType(typeName)
	if err != nil {
		return nil, v.typeResolutionFailure(raw, err), nil
	}

	// Validate the instance, passing the resolved type to avoid redundant resolution.
	return v.validateInstance(ctx, typeName, typ, raw)
}

// ValidateForComposition validates instances as composed children.
//
// This is used when validating part types within a composition context.
// Unlike direct validation, part types are allowed here.
func (v *Validator) ValidateForComposition(ctx context.Context, parentType, relationName string, raws []RawInstance) ([]*ValidInstance, []ValidationFailure, error) {
	if v == nil {
		return nil, nil, &InternalError{Kind: KindNilValidator, Cause: ErrNilValidator}
	}
	if ctx == nil {
		panic("instance.ValidateForComposition: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}

	// Handle nil input per implementation checklist.
	if raws == nil {
		return nil, nil, nil
	}

	// Look up the relation to find the child type
	parent, err := v.resolveType(parentType)
	if err != nil {
		failure := v.typeResolutionFailureForBatch(err, raws)
		return nil, []ValidationFailure{*failure}, nil
	}

	rel, found := parent.Relation(relationName)
	if !found {
		failure := v.compositionResolutionFailure(parentType, relationName, raws)
		return nil, []ValidationFailure{*failure}, nil
	}

	// Handle empty input after relation validation per implementation checklist.
	if len(raws) == 0 {
		return []*ValidInstance{}, nil, nil
	}

	// Get the target type name (qualified form to handle imported types)
	targetTypeName := rel.Target().String()

	// Resolve target type once, pass to all instances
	targetType, err := v.resolveType(targetTypeName)
	if err != nil {
		failure := v.typeResolutionFailureForBatch(err, raws)
		return nil, []ValidationFailure{*failure}, nil
	}

	var valid []*ValidInstance
	var failures []ValidationFailure

	for i := range raws {
		if err := ctx.Err(); err != nil {
			return valid, failures, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
		}

		instance, failure, err := v.validateComposedInstance(ctx, targetTypeName, targetType, raws[i], true)
		if err != nil {
			return valid, failures, err
		}

		if instance != nil {
			valid = append(valid, instance)
		} else if failure != nil {
			failures = append(failures, *failure)
		}
	}

	return valid, failures, nil
}

// ValidationError represents a system-level validation error.
type ValidationError struct {
	Code    diag.Code
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// parseTypeRef parses a type name string into a TypeRef.
// Handles both qualified ("alias.Name") and unqualified ("Name") forms.
func parseTypeRef(typeName string) schema.TypeRef {
	if idx := strings.Index(typeName, "."); idx > 0 {
		return schema.NewTypeRef(typeName[:idx], typeName[idx+1:], location.Span{})
	}
	return schema.LocalTypeRef(typeName, location.Span{})
}

// resolveType resolves a type name to a schema type.
// Handles both local types and imported types (qualified with alias prefix).
func (v *Validator) resolveType(typeName string) (*schema.Type, error) {
	ref := parseTypeRef(typeName)
	typ, found := v.schema.ResolveType(ref)
	if !found {
		return nil, &ValidationError{
			Code:    ErrTypeNotFound,
			Message: fmt.Sprintf("type %q not found", typeName),
		}
	}
	return typ, nil
}

// typeResolutionFailure creates a validation failure for type resolution errors.
func (v *Validator) typeResolutionFailure(raw RawInstance, err error) *ValidationFailure {
	failure := NewValidationFailure(raw, createErrorResult(ErrTypeNotFound, err.Error(), raw.Provenance))
	return &failure
}

// compositionResolutionFailure creates a ValidationFailure for composition resolution errors.
// When raws is empty, uses nil provenance.
func (v *Validator) compositionResolutionFailure(parentType, relationName string, raws []RawInstance) *ValidationFailure {
	var prov *Provenance
	if len(raws) > 0 {
		prov = raws[0].Provenance
	}

	failure := NewValidationFailure(
		RawInstance{Provenance: prov},
		createErrorResult(
			ErrCompositionNotFound,
			fmt.Sprintf("relation %q not found on type %q", relationName, parentType),
			prov,
		),
	)
	return &failure
}

// typeResolutionFailureForBatch creates a ValidationFailure for type resolution errors in batch contexts.
// When raws is empty, uses nil provenance.
func (v *Validator) typeResolutionFailureForBatch(err error, raws []RawInstance) *ValidationFailure {
	var prov *Provenance
	if len(raws) > 0 {
		prov = raws[0].Provenance
	}

	failure := NewValidationFailure(
		RawInstance{Provenance: prov},
		createErrorResult(ErrTypeNotFound, err.Error(), prov),
	)
	return &failure
}

// validateInstance validates a single instance against its type.
// canonicalName is the original type name (potentially qualified like "alias.Type")
// that should be preserved in the resulting ValidInstance.
// typ is the resolved schema type (passed to avoid redundant resolution).
func (v *Validator) validateInstance(ctx context.Context, canonicalName string, typ *schema.Type, raw RawInstance) (*ValidInstance, *ValidationFailure, error) {
	return v.validateComposedInstance(ctx, canonicalName, typ, raw, false)
}

// validateComposedInstance validates an instance, optionally allowing part types.
// typeName is the canonical type name (potentially qualified like "alias.Type")
// that should be preserved in the resulting ValidInstance.
// typ is the resolved schema type (passed to avoid redundant resolution).
func (v *Validator) validateComposedInstance(ctx context.Context, typeName string, typ *schema.Type, raw RawInstance, allowPartType bool) (*ValidInstance, *ValidationFailure, error) {
	// Check instantiation eligibility
	if typ.IsAbstract() {
		failure := NewValidationFailure(raw, createErrorResult(ErrAbstractType, fmt.Sprintf("cannot instantiate abstract type %q", typeName), raw.Provenance))
		return nil, &failure, nil
	}

	if !allowPartType && typ.IsPart() {
		failure := NewValidationFailure(raw, createErrorResult(ErrPartTypeDirect, fmt.Sprintf("part type %q cannot be instantiated directly", typeName), raw.Provenance))
		return nil, &failure, nil
	}

	// Validate properties, preserving the canonical type name
	return v.validateProperties(ctx, typ, typeName, raw)
}

// validateProperties validates all properties of an instance.
// canonicalName is the type name (potentially qualified like "alias.Type")
// that will be stored in the resulting ValidInstance.
func (v *Validator) validateProperties(ctx context.Context, typ *schema.Type, canonicalName string, raw RawInstance) (*ValidInstance, *ValidationFailure, error) {
	collector := diag.NewCollector(v.cfg.maxIssuesPerInstance)

	// Build property name mapping (input name → schema name)
	propMapping := v.buildPropertyMapping(typ, raw.Properties, collector, raw.Provenance)

	// Check for unknown fields
	if !v.cfg.allowUnknownFields {
		v.checkUnknownFields(typ, raw.Properties, propMapping, collector, raw.Provenance)
	}

	// Validate each property and build the validated properties map
	validatedProps := make(map[string]any)
	for prop := range typ.AllProperties() {
		inputName, hasInput := propMapping[prop.Name()]

		// Get the raw value
		var rawValue any
		if hasInput {
			rawValue = raw.Properties[inputName]
		}

		// Check required properties
		if rawValue == nil && prop.IsRequired() {
			issue := diag.NewIssue(
				diag.Error,
				ErrMissingRequired,
				fmt.Sprintf("missing required property %q", prop.Name()),
			).WithDetails(diag.TypeProp(typ.Name(), prop.Name())...)
			withProvenance(issue, raw.Provenance, provenancePath(raw.Provenance))
			collector.Collect(issue.Build())
			continue
		}

		// Skip nil optional properties
		if rawValue == nil {
			continue
		}

		// Validate property type
		if err := v.checkValueWithRecovery(rawValue, prop.Constraint()); err != nil {
			// Check if this is an internal error from panic recovery
			if internalErr, ok := errors.AsType[*InternalError](err); ok {
				return nil, nil, internalErr
			}
			code := ErrTypeMismatch
			if checkErr, ok := errors.AsType[*eval.CheckError](err); ok && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("property %q: %s", prop.Name(), err.Error()),
			).WithDetails(diag.TypeProp(typ.Name(), prop.Name())...)
			// Include input field name when it differs from schema property name (for debugging).
			if inputName != prop.Name() {
				issue.WithDetail(diag.DetailKeyField, inputName)
			}
			withProvenance(issue, raw.Provenance, provenancePathForProperty(raw.Provenance, prop.Name()))
			collector.Collect(issue.Build())
			continue
		}

		// Coerce to canonical type (int64, float64, []float64)
		coercedValue, err := v.coerceValueWithRecovery(rawValue, prop.Constraint())
		if err != nil {
			// Check if this is an internal error from panic recovery
			if internalErr, ok := errors.AsType[*InternalError](err); ok {
				return nil, nil, internalErr
			}
			// This should not happen after successful CheckValue
			issue := diag.NewIssue(
				diag.Error,
				ErrTypeMismatch,
				fmt.Sprintf("property %q: coercion error: %s", prop.Name(), err.Error()),
			).WithDetails(diag.TypeProp(typ.Name(), prop.Name())...)
			// Include input field name when it differs from schema property name (for debugging).
			if inputName != prop.Name() {
				issue.WithDetail(diag.DetailKeyField, inputName)
			}
			withProvenance(issue, raw.Provenance, provenancePathForProperty(raw.Provenance, prop.Name()))
			collector.Collect(issue.Build())
			continue
		}

		// Store validated and coerced property (will be cloned when wrapping)
		validatedProps[prop.Name()] = coercedValue
	}

	// If we have errors, return failure
	if collector.HasErrors() {
		failure := NewValidationFailure(raw, collector.Result())
		return nil, &failure, nil
	}

	// Check context cancellation before continuing
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}

	// Evaluate invariants
	if err := v.evaluateInvariants(ctx, typ, validatedProps, collector, raw.Provenance); err != nil {
		return nil, nil, err
	}

	// If invariant checks failed, return failure
	if collector.HasErrors() {
		failure := NewValidationFailure(raw, collector.Result())
		return nil, &failure, nil
	}

	// Extract primary key
	pkComponents := v.extractPrimaryKey(typ, validatedProps, collector, raw.Provenance)
	if collector.HasErrors() {
		failure := NewValidationFailure(raw, collector.Result())
		return nil, &failure, nil
	}

	// Validate edges (associations)
	edges := v.validateEdges(ctx, typ, raw.Properties, collector, raw.Provenance)
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}
	if collector.HasErrors() {
		failure := NewValidationFailure(raw, collector.Result())
		return nil, &failure, nil
	}

	// Validate compositions
	composed := v.validateCompositions(ctx, typ, raw.Properties, collector, raw.Provenance)
	if err := ctx.Err(); err != nil {
		return nil, nil, err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
	}
	if collector.HasErrors() {
		failure := NewValidationFailure(raw, collector.Result())
		return nil, &failure, nil
	}

	// Create ValidInstance with immutable wrappers
	// Use WrapClone to ensure defensive copying from raw input
	// Use canonicalName to preserve qualified form (e.g., "alias.Type")
	validInstance := NewValidInstance(
		canonicalName,
		typ.ID(),
		immutable.WrapKeyClone(pkComponents),
		immutable.WrapPropertiesClone(validatedProps),
		edges,
		composed,
		raw.Provenance,
	)

	return validInstance, nil, nil
}

// buildPropertyMapping builds a mapping from schema property names to input property names.
// Returns a map where key is schema property name, value is input property name.
//
// The mapping uses a two-pass approach for deterministic behavior:
//  1. First pass: exact matches only (highest priority, deterministic)
//  2. Second pass: case-fold matches, with collision detection
//
// When normalization occurs (non-strict mode, case-insensitive match) and a logger is
// configured, emits a debug log entry
//
// When multiple input fields case-fold to the same schema property (e.g., both "Name"
// and "name"), an E_CASE_FOLD_COLLISION error is emitted and neither is mapped.
//
// Complexity: O(N+M) where N is input property count and M is schema property count,
// using CanonicalPropertyMap() for O(1) case-insensitive lookups.
func (v *Validator) buildPropertyMapping(typ *schema.Type, props map[string]any, collector *diag.Collector, prov *Provenance) map[string]string {
	// Early return for empty input - no properties to map
	if len(props) == 0 {
		return make(map[string]string)
	}

	mapping := make(map[string]string)

	// Build canonical map once for O(1) case-insensitive lookups (O(M) construction)
	var canonicalMap map[string]string
	if !v.cfg.strictPropertyNames {
		canonicalMap = typ.CanonicalPropertyMap()
	}

	// Track schema properties claimed by exact matches
	exactMatches := make(map[string]bool)

	// First pass: exact matches only (deterministic, highest priority)
	for inputName := range props {
		if _, found := typ.Property(inputName); found {
			mapping[inputName] = inputName
			exactMatches[inputName] = true
		}
	}

	// Second pass: case-fold matches (only if no exact match exists)
	if !v.cfg.strictPropertyNames {
		// Track case-fold collisions: schema property -> input names that fold to it
		foldedInputs := make(map[string][]string)

		for inputName := range props {
			// Skip if this input was an exact match
			if exactMatches[inputName] {
				continue
			}

			if schemaName, found := canonicalMap[strings.ToLower(inputName)]; found {
				// Skip if this schema property already has an exact match
				if exactMatches[schemaName] {
					continue
				}
				foldedInputs[schemaName] = append(foldedInputs[schemaName], inputName)
			}
		}

		// Process folded inputs, detecting collisions
		for schemaName, inputs := range foldedInputs {
			if len(inputs) > 1 {
				// Collision: multiple input names fold to same schema property
				slices.Sort(inputs) // Deterministic error message
				issue := diag.NewIssue(
					diag.Error,
					ErrCaseFoldCollision,
					fmt.Sprintf("multiple input fields %v fold to schema property %q", inputs, schemaName),
				).WithDetail(diag.DetailKeyPropertyName, schemaName)
				withProvenance(issue, prov, provenancePath(prov))
				collector.Collect(issue.Build())
				continue
			}

			// Single case-fold match
			inputName := inputs[0]
			mapping[schemaName] = inputName

			// emit debug log when normalization occurs
			if v.cfg.logger != nil {
				v.cfg.logger.Debug("property name normalized",
					slog.String(diag.DetailKeyTypeName, typ.Name()),
					slog.String("input", inputName),
					slog.String("resolved", schemaName),
				)
			}
		}
	}

	return mapping
}

// checkUnknownFields reports diagnostics for unknown fields in the input.
func (v *Validator) checkUnknownFields(typ *schema.Type, props map[string]any, mapping map[string]string, collector *diag.Collector, prov *Provenance) {
	// Build reverse mapping to check which input names were matched as properties
	matched := make(map[string]bool)
	for _, inputName := range mapping {
		matched[inputName] = true
	}

	// Build set of valid relation field names (normalized JSON names).
	// Instance data uses Relation.FieldName() (e.g., "works_at"), not the DSL
	// relation name (e.g., "WORKS_AT").
	relationFields := make(map[string]bool)
	for rel := range typ.AllAssociations() {
		relationFields[rel.FieldName()] = true
		if !v.cfg.strictPropertyNames {
			relationFields[strings.ToLower(rel.FieldName())] = true
		}
	}
	for rel := range typ.AllCompositions() {
		relationFields[rel.FieldName()] = true
		if !v.cfg.strictPropertyNames {
			relationFields[strings.ToLower(rel.FieldName())] = true
		}
	}

	for inputName := range props {
		if matched[inputName] {
			continue
		}

		// Check if it's a relation field name (which would be handled separately)
		checkName := inputName
		if !v.cfg.strictPropertyNames {
			checkName = strings.ToLower(inputName)
		}
		if relationFields[checkName] {
			continue
		}

		issue := diag.NewIssue(
			diag.Error,
			ErrUnknownField,
			fmt.Sprintf("unknown field %q", inputName),
		).WithDetails(diag.TypeField(typ.Name(), inputName)...)
		withProvenance(issue, prov, provenancePathForProperty(prov, inputName))
		collector.Collect(issue.Build())
	}
}

// evaluateInvariants evaluates all type invariants against the validated properties.
//
// Invariants are evaluated independently - a failure in one invariant does not
// prevent evaluation of subsequent invariants. All failures are collected before
// returning, enabling comprehensive error reporting in a single validation pass.
//
// This approach trades off "fail-fast" behavior for diagnostic completeness.
// Users see all invariant violations at once rather than fixing them one at a time.
func (v *Validator) evaluateInvariants(ctx context.Context, typ *schema.Type, props map[string]any, collector *diag.Collector, prov *Provenance) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindInvariantPanic)
		}
	}()

	scope := eval.PropertyScopeFromMap(props).WithSelf(props)

	for inv := range typ.AllInvariants() {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
		}

		expr := inv.Expression()
		if expr == nil {
			continue
		}

		result, err := v.evaluator.EvaluateBool(expr, scope) //nolint:contextcheck // Evaluator API doesn't accept context
		if err != nil {
			issue := diag.NewIssue(
				diag.Error,
				ErrEvalError,
				"invariant evaluation error: "+err.Error(),
			).WithDetail(diag.DetailKeyTypeName, typ.Name())
			withProvenance(issue, prov, provenancePath(prov))
			collector.Collect(issue.Build())
			continue
		}

		if !result {
			msg := inv.Name()
			if msg == "" {
				msg = "invariant failed"
			}
			issue := diag.NewIssue(
				diag.Error,
				ErrInvariantFail,
				msg,
			).WithDetail(diag.DetailKeyTypeName, typ.Name())
			withProvenance(issue, prov, provenancePath(prov))
			collector.Collect(issue.Build())
		}
	}

	return nil
}

// extractPrimaryKey extracts primary key components from validated properties.
func (v *Validator) extractPrimaryKey(typ *schema.Type, props map[string]any, collector *diag.Collector, prov *Provenance) []any {
	var pkComponents []any

	for pk := range typ.PrimaryKeys() {
		val, ok := props[pk.Name()]
		if !ok || val == nil {
			issue := diag.NewIssue(
				diag.Error,
				ErrMissingPrimaryKey,
				fmt.Sprintf("missing primary key property %q", pk.Name()),
			).WithDetails(diag.TypeProp(typ.Name(), pk.Name())...)
			withProvenance(issue, prov, provenancePath(prov))
			collector.Collect(issue.Build())
			continue
		}
		pkComponents = append(pkComponents, val)
	}

	return pkComponents
}

// withProvenance applies provenance to an issue builder (path and span when available).
// This implements the architecture requirement to attach spans when provenance has one.
func withProvenance(b *diag.IssueBuilder, prov *Provenance, pathStr string) *diag.IssueBuilder {
	sourceName := ""
	if prov != nil {
		sourceName = prov.SourceName()
	}
	b.WithPath(sourceName, pathStr)
	if prov != nil && !prov.Span().IsZero() {
		b.WithSpan(prov.Span())
	}
	return b
}

// createErrorResult creates a diag.Result with a single error issue.
func createErrorResult(code diag.Code, message string, prov *Provenance) diag.Result {
	collector := diag.NewCollectorUnlimited()
	issue := diag.NewIssue(
		diag.Error,
		code,
		message,
	)
	withProvenance(issue, prov, provenancePath(prov))
	collector.Collect(issue.Build())
	return collector.Result()
}

// provenancePath returns the path string from provenance, or "$" if nil.
func provenancePath(prov *Provenance) string {
	if prov == nil {
		return "$"
	}
	return prov.Path().String()
}

// provenancePathForProperty returns the path string for a property.
func provenancePathForProperty(prov *Provenance, propName string) string {
	if prov == nil {
		// Use path.Root().Key() to produce canonical path syntax
		return path.Root().Key(propName).String()
	}
	return prov.AtKey(propName).Path().String()
}

// checkValueWithRecovery calls the Checker's CheckValue with panic recovery.
func (v *Validator) checkValueWithRecovery(val any, c schema.Constraint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // error is intentionally unwrapped; caller handles CheckError vs InternalError
	return v.checker.CheckValue(val, c)
}

// coerceValueWithRecovery calls the Checker's CoerceValue with panic recovery.
func (v *Validator) coerceValueWithRecovery(val any, c schema.Constraint) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindConstraintPanic)
		}
	}()
	//nolint:wrapcheck // error is intentionally unwrapped; caller handles validation errors
	return v.checker.CoerceValue(val, c)
}

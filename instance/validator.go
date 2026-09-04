package instance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
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
func NewValidator(s *schema.Schema, opts ...Option) *Validator {
	if s == nil {
		panic("instance.NewValidator: nil schema")
	}
	cfg := applyOptions(opts)
	return &Validator{
		schema:    s,
		cfg:       cfg,
		evaluator: eval.NewEvaluator(),
		checker:   eval.DefaultChecker(),
	}
}

// Validate validates a batch of raw instances of the given type.
//
// Returns:
//   - valids: one entry per input instance; non-nil for successes, nil for failures
//   - result: merged diagnostics for the entire batch (OK when all instances pass)
//
// Every diagnostic carries one [diag.DetailKeyInstanceIndex] detail naming the
// element of raws it belongs to, and the batch result carries each instance's
// truncation facts ([diag.Result.LimitReached], [diag.Result.DroppedCount]).
//
// Panics if the receiver is nil or if ctx is nil.
func (v *Validator) Validate(ctx context.Context, typeName string, raws []RawInstance) ([]*ValidInstance, diag.Result) {
	if v == nil {
		panic("instance.Validate: nil validator receiver")
	}
	if ctx == nil {
		panic("instance.Validate: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelledResult(err, firstProvenance(raws))
	}

	// Handle nil vs empty input per implementation checklist.
	if raws == nil {
		return nil, diag.OK()
	}
	if len(raws) == 0 {
		return []*ValidInstance{}, diag.OK()
	}

	typ, err := v.resolveType(typeName)
	if err != nil {
		return nil, v.typeResolutionResult(raws[0], err)
	}

	batchCollector := diag.NewCollectorUnlimited()
	var valids []*ValidInstance

	for i := range raws {
		if err := ctx.Err(); err != nil {
			batchCollector.Merge(cancelledResult(err, raws[i].Provenance))
			return valids, batchCollector.Result()
		}

		inst, result := v.validateInstance(ctx, typeName, typ, raws[i])
		batchCollector.MergeFunc(result, stampIndex(i))
		valids = append(valids, inst)
	}

	return valids, batchCollector.Result()
}

// ValidateOne validates a single raw instance.
//
// Returns:
//   - valid: the validated instance, or nil on failure
//   - result: diagnostics for this instance (OK on success, may contain warnings)
//
// Panics if the receiver is nil or if ctx is nil.
func (v *Validator) ValidateOne(ctx context.Context, typeName string, raw RawInstance) (*ValidInstance, diag.Result) {
	if v == nil {
		panic("instance.ValidateOne: nil validator receiver")
	}
	if ctx == nil {
		panic("instance.ValidateOne: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelledResult(err, raw.Provenance)
	}

	typ, err := v.resolveType(typeName)
	if err != nil {
		return nil, v.typeResolutionResult(raw, err)
	}
	return v.validateInstance(ctx, typeName, typ, raw)
}

// ValidateForComposition validates instances as composed children of the
// named composition on parentType. Part types are allowed here, where direct
// validation refuses them.
//
// relationName is the composition's DSL name ("LINES") or its field name
// ("lines"); an association's name is refused with E_COMPOSITION_NOT_FOUND,
// as is a name the parent does not declare. The parent and the relation are
// resolved before the batch is inspected, so an empty or nil batch reports
// the same misconfiguration a full one does.
//
// Returns:
//   - valids: one entry per input instance; non-nil for successes, nil for failures
//   - result: merged diagnostics for the batch (OK when all instances pass)
//
// Every diagnostic carries one [diag.DetailKeyInstanceIndex] detail naming
// the element of raws it belongs to.
//
// Panics if the receiver is nil or if ctx is nil.
func (v *Validator) ValidateForComposition(ctx context.Context, parentType, relationName string, raws []RawInstance) ([]*ValidInstance, diag.Result) {
	if v == nil {
		panic("instance.ValidateForComposition: nil validator receiver")
	}
	if ctx == nil {
		panic("instance.ValidateForComposition: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelledResult(err, firstProvenance(raws))
	}

	parent, err := v.resolveType(parentType)
	if err != nil {
		return nil, v.typeResolutionResultForBatch(err, raws)
	}
	rel, found := relationByEitherName(parent, relationName)
	if !found || !rel.IsComposition() {
		return nil, v.compositionResolutionResult(parentType, relationName, raws)
	}

	// Handle nil input per implementation checklist.
	if raws == nil {
		return nil, diag.OK()
	}

	bases := make([]path.Builder, len(raws))
	for i := range raws {
		bases[i] = provenancePathBuilder(raws[i].Provenance)
	}
	batchCollector := diag.NewCollectorUnlimited()
	valids := v.validateComposedBatch(ctx, rel, raws, bases, 1, func(i int, res diag.Result) {
		if i < 0 {
			batchCollector.Merge(res)
			return
		}
		batchCollector.MergeFunc(res, stampIndex(i))
	})
	return valids, batchCollector.Result()
}

// stampIndex returns the transform that marks an issue with the batch index
// of the instance it belongs to.
func stampIndex(i int) func(diag.Issue) diag.Issue {
	index := strconv.Itoa(i)
	return func(issue diag.Issue) diag.Issue {
		return diag.FromIssue(issue).WithDetail(diag.DetailKeyInstanceIndex, index).Build()
	}
}

// cancelledResult is the Fatal E_CONTEXT_CANCELLED result, anchored on the
// provenance in hand so a cancelled batch still names the row it stopped on.
func cancelledResult(err error, prov *location.Provenance) diag.Result {
	c := diag.NewCollectorUnlimited()
	c.Collect(cancelledIssue(err, prov, provenancePathBuilder(prov)))
	return c.Result()
}

func cancelledIssue(err error, prov *location.Provenance, base path.Builder) diag.Issue {
	issue := diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error())
	withProvenance(issue, prov, base.String())
	return issue.Build()
}

// internalIssue is the Fatal E_INTERNAL an InternalError becomes, carrying
// its stack and the provenance in hand.
func internalIssue(internalErr *InternalError, prov *location.Provenance, base path.Builder) diag.Issue {
	issue := diag.NewIssue(diag.Fatal, diag.E_INTERNAL, internalErr.Error()).
		WithDetail(diag.DetailKeyStackTrace, internalErr.Stack)
	withProvenance(issue, prov, base.String())
	return issue.Build()
}

func firstProvenance(raws []RawInstance) *location.Provenance {
	if len(raws) > 0 {
		return raws[0].Provenance
	}
	return nil
}

// validateComposedBatch validates raw children of rel's target type at the
// given composed depth, each anchored on its base path. The target is
// resolved by the relation's completion-recorded absolute identity (see
// resolveRelationTarget), so a relation declared on an imported type or
// inherited from a cross-schema parent — whose syntactic target ref is
// meaningful only in its declaring schema — validates against the true
// target. collect receives each child's result under its index in raws, or
// -1 for a result that belongs to the batch as a whole.
func (v *Validator) validateComposedBatch(
	ctx context.Context,
	rel *schema.Relation,
	raws []RawInstance,
	bases []path.Builder,
	depth int,
	collect func(i int, res diag.Result),
) []*ValidInstance {
	if len(raws) == 0 {
		return []*ValidInstance{}
	}

	targetType, found := resolveRelationTarget(v.schema, rel)
	if !found {
		collect(-1, v.typeResolutionResultForBatch(&ValidationError{
			Code:    ErrTypeNotFound,
			Message: fmt.Sprintf("type %q not found", rel.Target().String()),
		}, raws))
		return nil
	}
	// A composed child is named the way graph and snapshot name it: the
	// entry-relative form, never the declaring schema's alias. Identity is
	// TypeID; the name is display.
	targetTypeName := schema.TagForm(v.schema, targetType.ID())

	var valids []*ValidInstance
	for i := range raws {
		if err := ctx.Err(); err != nil {
			collect(i, cancelledResult(err, raws[i].Provenance))
			return valids
		}
		inst, result := v.validateComposedInstance(ctx, targetTypeName, targetType, raws[i], bases[i], true, depth)
		collect(i, result)
		valids = append(valids, inst)
	}
	return valids
}

// ValidationError represents a system-level validation error.
type ValidationError struct {
	Code    diag.Code
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// resolveRelationTarget resolves a relation's target type by the absolute
// identity recorded at completion ([schema.Relation.TargetID]), searched
// across the schema's import closure. The syntactic target ref is
// declaring-schema-relative — its qualifier is the declaring schema's import
// alias, and its bare form names a declaring-schema type — so re-resolving it
// against the entry schema would misresolve relations declared on imported
// types or inherited from cross-schema parents. Every relation a completed
// schema holds carries its TargetID; a miss reports as not found.
func resolveRelationTarget(s *schema.Schema, rel *schema.Relation) (*schema.Type, bool) {
	return s.TypeByID(rel.TargetID())
}

// relationByEitherName resolves a relation by its DSL name or its field
// name, the two spellings every name-taking surface of this package accepts.
func relationByEitherName(t *schema.Type, name string) (*schema.Relation, bool) {
	if r, ok := t.Relation(name); ok {
		return r, true
	}
	return t.RelationByField(name)
}

// resolveType resolves an entry-relative type name by the one rule
// [schema.Schema.ResolveTypeName] states.
func (v *Validator) resolveType(typeName string) (*schema.Type, error) {
	typ, found := v.schema.ResolveTypeName(typeName)
	if !found {
		return nil, &ValidationError{
			Code:    ErrTypeNotFound,
			Message: fmt.Sprintf("type %q not found", typeName),
		}
	}
	return typ, nil
}

// typeResolutionResult creates a diagnostic result for type resolution errors.
func (v *Validator) typeResolutionResult(raw RawInstance, err error) diag.Result {
	return createErrorResult(ErrTypeNotFound, err.Error(), raw.Provenance)
}

// compositionResolutionResult creates a diagnostic result for composition resolution errors.
// When raws is empty, uses nil provenance.
func (v *Validator) compositionResolutionResult(parentType, relationName string, raws []RawInstance) diag.Result {
	return createErrorResult(ErrCompositionNotFound, fmt.Sprintf("composition %q not found on type %q", relationName, parentType), firstProvenance(raws))
}

// typeResolutionResultForBatch creates a diagnostic result for type resolution errors in batch contexts.
// When raws is empty, uses nil provenance.
func (v *Validator) typeResolutionResultForBatch(err error, raws []RawInstance) diag.Result {
	return createErrorResult(ErrTypeNotFound, err.Error(), firstProvenance(raws))
}

// validateInstance validates a root instance against its type.
// canonicalName is the entry-relative type name the caller supplied, preserved
// on the resulting ValidInstance. typ is the resolved schema type.
func (v *Validator) validateInstance(ctx context.Context, canonicalName string, typ *schema.Type, raw RawInstance) (*ValidInstance, diag.Result) {
	return v.validateComposedInstance(ctx, canonicalName, typ, raw, provenancePathBuilder(raw.Provenance), false, 0)
}

// validateComposedInstance validates an instance, optionally allowing part
// types. typeName is the entry-relative type name stored on the result and
// carried on its diagnostics; base is the instance's location in the input
// document, which every diagnostic's path is built from; depth is its
// composed depth, 0 for a root.
func (v *Validator) validateComposedInstance(ctx context.Context, typeName string, typ *schema.Type, raw RawInstance, base path.Builder, allowPartType bool, depth int) (*ValidInstance, diag.Result) {
	// Check instantiation eligibility
	if typ.IsAbstract() {
		return nil, createErrorResult(ErrAbstractType, fmt.Sprintf("cannot instantiate abstract type %q", typeName), raw.Provenance)
	}

	if !allowPartType && typ.IsPart() {
		return nil, createErrorResult(ErrPartTypeDirect, fmt.Sprintf("part type %q cannot be instantiated directly", typeName), raw.Provenance)
	}

	return v.validateProperties(ctx, typ, typeName, raw, base, depth)
}

// validateProperties validates all members of an instance: properties, then
// the primary key, edges, compositions, and invariants last.
func (v *Validator) validateProperties(ctx context.Context, typ *schema.Type, typeName string, raw RawInstance, base path.Builder, depth int) (*ValidInstance, diag.Result) {
	collector := diag.NewCollector(v.cfg.maxIssuesPerInstance)
	prov := raw.Provenance
	input := v.indexInput(raw.Properties)

	// Build property name mapping (schema name → input name)
	propMapping, accounted := v.buildPropertyMapping(typ, input, collector, prov, base)

	// Check for unknown fields
	if !v.cfg.allowUnknownFields {
		v.checkUnknownFields(typ, typeName, input, propMapping, accounted, collector, prov, base)
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
			).WithDetails(diag.TypeProp(typeName, prop.Name())...)
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
			continue
		}

		// Skip nil optional properties
		if rawValue == nil {
			continue
		}

		propPath := base.Key(inputName).String()

		// Validate property type
		if err := v.checkValueWithRecovery(rawValue, prop.Constraint()); err != nil {
			if internalErr, ok := errors.AsType[*InternalError](err); ok {
				collector.Collect(internalIssue(internalErr, prov, base))
				return nil, collector.Result()
			}
			code := ErrTypeMismatch
			if checkErr, ok := errors.AsType[*eval.CheckError](err); ok && checkErr.Kind == eval.KindConstraintFail {
				code = ErrConstraintFail
			}
			issue := diag.NewIssue(
				diag.Error,
				code,
				fmt.Sprintf("property %q: %s", prop.Name(), err.Error()),
			).WithDetails(diag.TypeProp(typeName, prop.Name())...)
			if inputName != prop.Name() {
				issue.WithDetail(diag.DetailKeyField, inputName)
			}
			withProvenance(issue, prov, propPath)
			collector.Collect(issue.Build())
			continue
		}

		// Coerce to canonical type (int64, float64, []float64)
		coercedValue, err := v.coerceValueWithRecovery(rawValue, prop.Constraint())
		if err != nil {
			if internalErr, ok := errors.AsType[*InternalError](err); ok {
				collector.Collect(internalIssue(internalErr, prov, base))
				return nil, collector.Result()
			}
			// This should not happen after successful CheckValue
			issue := diag.NewIssue(
				diag.Error,
				ErrTypeMismatch,
				fmt.Sprintf("property %q: coercion error: %s", prop.Name(), err.Error()),
			).WithDetails(diag.TypeProp(typeName, prop.Name())...)
			if inputName != prop.Name() {
				issue.WithDetail(diag.DetailKeyField, inputName)
			}
			withProvenance(issue, prov, propPath)
			collector.Collect(issue.Build())
			continue
		}

		// Store validated and coerced property (will be cloned when wrapping)
		validatedProps[prop.Name()] = coercedValue
	}

	// If we have errors, return diagnostic result
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	if err := ctx.Err(); err != nil {
		collector.Collect(cancelledIssue(err, prov, base))
		return nil, collector.Result()
	}

	// Extract primary key
	pkComponents := v.extractPrimaryKey(typ, typeName, validatedProps, collector, prov, base)
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Validate edges (associations)
	edges := v.validateEdges(ctx, typ, input, collector, prov, base)
	if err := ctx.Err(); err != nil {
		collector.Collect(cancelledIssue(err, prov, base))
		return nil, collector.Result()
	}
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Validate compositions
	composed := v.validateCompositions(ctx, typ, input, collector, prov, base, depth)
	if err := ctx.Err(); err != nil {
		collector.Collect(cancelledIssue(err, prov, base))
		return nil, collector.Result()
	}
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Evaluate invariants LAST, over properties AND relations: an invariant
	// may name a relation, and evaluating before the relations were computed
	// left every one of them reading nil.
	if err := v.evaluateInvariants(ctx, typ, typeName, validatedProps, edges, composed, collector, prov, base); err != nil {
		if internalErr, ok := errors.AsType[*InternalError](err); ok {
			collector.Collect(internalIssue(internalErr, prov, base))
		} else {
			collector.Collect(cancelledIssue(err, prov, base))
		}
		return nil, collector.Result()
	}
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Create ValidInstance with immutable wrappers
	// Use WithClone to ensure defensive copying from raw input
	validInstance := newValidatedInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pkComponents, immutable.WithClone(true)),
		immutable.WrapProperties(validatedProps, immutable.WithClone(true)),
		edges,
		composed,
		prov,
	)

	return validInstance, collector.Result()
}

// inputKeys is one instance's input object with its keys indexed by their
// ASCII fold, so every member lookup — property, relation field, edge property
// — answers in O(1) under the default mode instead of rescanning the object
// once per member. names is the keys in sorted order, for deterministic
// diagnostics; byFold is nil under strict property names, where no fold is
// attempted.
type inputKeys struct {
	props  map[string]any
	names  []string
	byFold map[string][]string
}

func (v *Validator) indexInput(props map[string]any) inputKeys {
	in := inputKeys{props: props, names: slices.Sorted(maps.Keys(props))}
	if v.cfg.strictPropertyNames {
		return in
	}
	in.byFold = make(map[string][]string, len(props))
	for _, name := range in.names {
		if lower, ok := foldKey(name); ok {
			in.byFold[lower] = append(in.byFold[lower], name)
		}
	}
	return in
}

// foldKey returns the case-fold of an input key, or false for a key no schema
// name can match under the fold. Identifiers are ASCII by the language's
// grammar, so the fold is ASCII lowercasing and a key carrying any non-ASCII
// byte folds to nothing: strings.ToLower would map a KELVIN SIGN onto "k" and
// match a field the caller never wrote.
func foldKey(key string) (string, bool) {
	var b []byte
	for i := range len(key) {
		c := key[i]
		if c >= 0x80 {
			return "", false
		}
		if c >= 'A' && c <= 'Z' {
			if b == nil {
				b = []byte(key)
			}
			b[i] = c + ('a' - 'A')
		}
	}
	if b == nil {
		return key, true
	}
	return string(b), true
}

// lookupRelationInput finds the input key a relation's value is under: the
// exact field name, or under the default mode the one key that folds to it.
// Two keys folding to it is a collision, reported once at the object that
// holds them; the relation is then neither absent nor present and the caller
// moves on.
func (v *Validator) lookupRelationInput(rel *schema.Relation, in inputKeys, collector *diag.Collector, prov *location.Provenance, base path.Builder) (key string, value any, has bool) {
	if val, ok := in.props[rel.FieldName()]; ok {
		return rel.FieldName(), val, true
	}
	if in.byFold == nil {
		return "", nil, false
	}
	candidates := in.byFold[rel.FieldName()]
	switch len(candidates) {
	case 0:
		return "", nil, false
	case 1:
		return candidates[0], in.props[candidates[0]], true
	}
	kind := "relation"
	if rel.IsComposition() {
		kind = "composition"
	}
	issue := diag.NewIssue(
		diag.Error,
		ErrCaseFoldCollision,
		fmt.Sprintf("multiple input fields %v fold to %s field %q", candidates, kind, rel.FieldName()),
	).WithDetail(diag.DetailKeyRelationName, rel.Name()).
		WithDetail(diag.DetailKeyJSONField, rel.FieldName())
	withProvenance(issue, prov, base.String())
	collector.Collect(issue.Build())
	return "", nil, false
}

// buildPropertyMapping maps each schema property to the input key it is read
// from, in two passes: exact matches first, then — under the default mode —
// case-fold matches with collision detection. An exact match is claimed in
// the first pass and is never a collision. When two or more input keys fold
// to one schema property none of them matches exactly, an
// E_CASE_FOLD_COLLISION is reported at the object and neither is mapped.
//
// accounted is every input key the mapping has spoken for — mapped, or named
// in a collision — so the unknown-field check does not report it again. When
// a fold maps a key and a logger is configured, a debug record says so.
func (v *Validator) buildPropertyMapping(typ *schema.Type, in inputKeys, collector *diag.Collector, prov *location.Provenance, base path.Builder) (mapping map[string]string, accounted map[string]bool) {
	mapping = make(map[string]string, len(in.names))
	accounted = make(map[string]bool, len(in.names))

	for _, inputName := range in.names {
		if _, found := typ.Property(inputName); found {
			mapping[inputName] = inputName
			accounted[inputName] = true
		}
	}
	if in.byFold == nil {
		return mapping, accounted
	}

	// Fold pass: schema property → the unclaimed keys folding to it, in sorted
	// order because in.names is sorted.
	folded := make(map[string][]string)
	var foldedNames []string
	for _, inputName := range in.names {
		if accounted[inputName] {
			continue
		}
		lower, ok := foldKey(inputName)
		if !ok {
			continue
		}
		schemaName, found := typ.CanonicalPropertyName(lower)
		if !found || mapping[schemaName] != "" {
			continue // unknown, or shadowed by an exact match: the unknown-field check names it
		}
		if _, seen := folded[schemaName]; !seen {
			foldedNames = append(foldedNames, schemaName)
		}
		folded[schemaName] = append(folded[schemaName], inputName)
	}

	for _, schemaName := range foldedNames {
		inputs := folded[schemaName]
		if len(inputs) > 1 {
			issue := diag.NewIssue(
				diag.Error,
				ErrCaseFoldCollision,
				fmt.Sprintf("multiple input fields %v fold to schema property %q", inputs, schemaName),
			).WithDetail(diag.DetailKeyPropertyName, schemaName)
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
			for _, inputName := range inputs {
				accounted[inputName] = true
			}
			continue
		}
		inputName := inputs[0]
		mapping[schemaName] = inputName
		accounted[inputName] = true
		if v.cfg.logger != nil {
			v.cfg.logger.Debug(
				"property name normalized",
				slog.String(diag.DetailKeyTypeName, typ.Name()),
				slog.String("input", inputName),
				slog.String("resolved", schemaName),
			)
		}
	}
	return mapping, accounted
}

// checkUnknownFields reports each input key that is neither a property the
// mapping spoke for nor a relation field, in sorted order so the set that
// survives a truncated collector is a function of the input.
func (v *Validator) checkUnknownFields(typ *schema.Type, typeName string, in inputKeys, mapping map[string]string, accounted map[string]bool, collector *diag.Collector, prov *location.Provenance, base path.Builder) {
	for _, inputName := range in.names {
		if accounted[inputName] {
			continue
		}
		if v.isRelationField(typ, inputName) {
			continue
		}
		issue := diag.NewIssue(
			diag.Error,
			ErrUnknownField,
			fmt.Sprintf("unknown field %q", inputName),
		).WithDetails(diag.TypeField(typeName, inputName)...)
		if shadowed, ok := v.exactMatchShadowing(typ, inputName, mapping); ok {
			issue.WithDetail(diag.DetailKeyReason, "case_fold_shadowed").
				WithDetail(diag.DetailKeyPropertyName, shadowed)
		}
		withProvenance(issue, prov, base.Key(inputName).String())
		collector.Collect(issue.Build())
	}
}

// isRelationField reports whether an input key names one of typ's relations
// by its field name, exactly or — under the default mode — by fold.
func (v *Validator) isRelationField(typ *schema.Type, inputName string) bool {
	if _, ok := typ.RelationByField(inputName); ok {
		return true
	}
	if v.cfg.strictPropertyNames {
		return false
	}
	lower, ok := foldKey(inputName)
	if !ok {
		return false
	}
	_, ok = typ.RelationByField(lower)
	return ok
}

// exactMatchShadowing reports the schema property inputName case-folds onto
// when that property was already claimed by an exact match, which is why the
// fold was skipped and the field reported unknown. Without it the operator sees
// a bare "unknown field" for a name the schema plainly recognises.
//
// Reports false under strict property names, where no fold is attempted at all.
func (v *Validator) exactMatchShadowing(typ *schema.Type, inputName string, mapping map[string]string) (string, bool) {
	if v.cfg.strictPropertyNames {
		return "", false
	}
	lower, ok := foldKey(inputName)
	if !ok {
		return "", false
	}
	schemaName, found := typ.CanonicalPropertyName(lower)
	if !found {
		return "", false
	}
	if claimant, claimed := mapping[schemaName]; claimed && claimant != inputName {
		return schemaName, true
	}
	return "", false
}

// invariantScope returns the name→value map an invariant is evaluated against:
// the validated properties, plus one entry per relation keyed by FieldName.
//
// Relations belong here because buildStaticScope admits their field names, so
// an invariant may reference one. The two relation kinds carry different
// amounts of information, and the difference is not incidental:
//
//   - A COMPOSITION's children are part of this instance, so the entry is the
//     validated child data itself. `ITEMS -> Len`, and a lambda over a child's
//     own properties, both read real values.
//   - An ASSOCIATION's target is a REFERENCE. The instance holds the foreign
//     key, never the target's row, so the entry is the list of target keys.
//     Presence and cardinality are answerable; the target's properties are not
//     in this instance to answer with.
//
// A relation with no value is absent from the map, so it evaluates to nil and
// the `!= nil` idiom means what it says.
func (v *Validator) invariantScope(
	typ *schema.Type,
	props map[string]any,
	edges map[string]*ValidEdgeData,
	composed map[string]immutable.Value,
) map[string]any {
	if len(edges) == 0 && len(composed) == 0 {
		return props
	}

	scope := make(map[string]any, len(props)+len(edges)+len(composed))
	maps.Copy(scope, props)

	for rel := range typ.AllAssociations() {
		edge, ok := edges[rel.Name()]
		if !ok {
			continue
		}
		keys := make([]any, 0, len(edge.targets))
		for t := range edge.TargetsIter() {
			keys = append(keys, keyValue(t.TargetKey()))
		}
		scope[rel.FieldName()] = relationValue(rel, keys)
	}

	for rel := range typ.AllCompositions() {
		value, ok := composed[rel.Name()]
		if !ok {
			continue
		}
		scope[rel.FieldName()] = v.composedScopeValue(rel, value.Unwrap())
	}

	return scope
}

// composedScopeValue renders a composition as docs/SPEC.md states: the single
// child for a composition that is not many, a list otherwise, each child an
// instance whose own properties and relations are in scope.
func (v *Validator) composedScopeValue(rel *schema.Relation, value any) any {
	children := composedChildren(value)
	scopes := make([]any, 0, len(children))
	for _, child := range children {
		scopes = append(scopes, v.childScope(child))
	}
	if rel.IsMany() {
		return scopes
	}
	if len(scopes) == 0 {
		return nil
	}
	return scopes[0]
}

// composedChildren unwraps a validated composition value into its children.
func composedChildren(value any) []*ValidInstance {
	switch c := value.(type) {
	case *ValidInstance:
		return []*ValidInstance{c}
	case []*ValidInstance:
		return c
	case immutable.Slice:
		out := make([]*ValidInstance, 0, c.Len())
		for elem := range c.Iter() {
			out = append(out, composedChildren(elem.Unwrap())...)
		}
		return out
	}
	return nil
}

// childScope is a composed child as an instance in scope, built by the rule
// its parent's scope was built by: properties, then relations by field name.
func (v *Validator) childScope(child *ValidInstance) map[string]any {
	props := child.Properties().Clone()
	typ, ok := v.schema.TypeByID(child.TypeID())
	if !ok {
		return props
	}
	return v.invariantScope(typ, props, maps.Collect(child.Edges()), maps.Collect(child.Compositions()))
}

// relationValue renders a relation's entries for the invariant scope: a
// single-valued relation yields its one entry, so `works_at != nil` reads as a
// presence test rather than a one-element list.
func relationValue(rel *schema.Relation, entries []any) any {
	if rel.IsMany() {
		return entries
	}
	if len(entries) == 0 {
		return nil
	}
	return entries[0]
}

// keyValue renders a target key for the invariant scope: a single-component key
// as its component, so `works_at == "c1"` compares against the value the caller
// wrote, and a composite key as the component list.
func keyValue(k immutable.Key) any {
	parts := k.Clone()
	if len(parts) == 1 {
		return parts[0]
	}
	return parts
}

// evaluateInvariants evaluates every invariant of typ against the validated
// members. The scope is built here, under the recover that turns an
// invariant-path panic into an InternalError, and only for a type that
// declares an invariant to read it.
//
// Invariants are evaluated independently - a failure in one invariant does not
// prevent evaluation of subsequent invariants. All failures are collected before
// returning, enabling comprehensive error reporting in a single validation pass.
func (v *Validator) evaluateInvariants(
	ctx context.Context,
	typ *schema.Type,
	typeName string,
	props map[string]any,
	edges map[string]*ValidEdgeData,
	composed map[string]immutable.Value,
	collector *diag.Collector,
	prov *location.Provenance,
	base path.Builder,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = wrapPanicValue(r, KindInvariantPanic)
		}
	}()

	var scope eval.Scope
	for inv := range typ.AllInvariants() {
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // spec: return ctx.Err() directly for cancellation
		}

		expr := inv.Expression()
		if expr == nil {
			continue
		}
		if scope == nil {
			members := v.invariantScope(typ, props, edges, composed)
			scope = eval.PropertyScopeFromMap(members).WithSelf(members)
		}

		result, err := v.evaluator.EvaluateBool(expr, scope) //nolint:contextcheck // Evaluator API doesn't accept context
		if err != nil {
			issue := diag.NewIssue(
				diag.Error,
				ErrEvalError,
				"invariant evaluation error: "+err.Error(),
			).WithDetail(diag.DetailKeyTypeName, typeName)
			withProvenance(issue, prov, base.String())
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
			).WithDetail(diag.DetailKeyTypeName, typeName)
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
		}
	}

	return nil
}

// extractPrimaryKey extracts primary key components from validated properties.
func (v *Validator) extractPrimaryKey(typ *schema.Type, typeName string, props map[string]any, collector *diag.Collector, prov *location.Provenance, base path.Builder) []any {
	var pkComponents []any

	for pk := range typ.PrimaryKeys() {
		val, ok := props[pk.Name()]
		if !ok || val == nil {
			issue := diag.NewIssue(
				diag.Error,
				ErrMissingPrimaryKey,
				fmt.Sprintf("missing primary key property %q", pk.Name()),
			).WithDetails(diag.TypeProp(typeName, pk.Name())...)
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
			continue
		}
		pkComponents = append(pkComponents, val)
	}

	return pkComponents
}

// withProvenance sets an issue's path and source name, and its span when the
// provenance carries one. pathStr is the issue's own location in the input
// document; prov supplies the document's name and span.
func withProvenance(b *diag.IssueBuilder, prov *location.Provenance, pathStr string) *diag.IssueBuilder {
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

// createErrorResult creates a diag.Result with a single error issue at the
// provenance's own location.
func createErrorResult(code diag.Code, message string, prov *location.Provenance) diag.Result {
	collector := diag.NewCollectorUnlimited()
	issue := diag.NewIssue(
		diag.Error,
		code,
		message,
	)
	withProvenance(issue, prov, provenancePathBuilder(prov).String())
	collector.Collect(issue.Build())
	return collector.Result()
}

// provenancePathBuilder returns the instance's location in the input document,
// or the root when it has no provenance.
func provenancePathBuilder(prov *location.Provenance) path.Builder {
	if prov == nil {
		return path.Root()
	}
	return prov.Path()
}

// checkValueWithRecovery calls the Checker's CheckValue with panic recovery.
func (v *Validator) checkValueWithRecovery(val any, c schema.Constraint) error {
	return checkValueRecovering(v.checker, val, c)
}

// coerceValueWithRecovery calls the Checker's CoerceValue with panic recovery.
func (v *Validator) coerceValueWithRecovery(val any, c schema.Constraint) (any, error) {
	return coerceValueRecovering(v.checker, val, c)
}

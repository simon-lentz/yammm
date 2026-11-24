package instance

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Validator validates raw instances against a schema.
//
// Validator is stateless and safe for concurrent use. Multiple goroutines
// may call Validate simultaneously with different inputs.
//
// All validation methods (Validate, ValidateOne) require a non-nil context and
// will panic if passed nil. Use context.Background() for non-cancellable
// operations.
type Validator struct {
	schema    schema.Schema
	cfg       *validatorConfig
	evaluator *eval.Evaluator
}

// NewValidator creates a new Validator for the given schema.
// Panics if schema is zero.
func NewValidator(s schema.Schema, opts ...Option) *Validator {
	if s.IsZero() {
		panic("instance.NewValidator: zero Schema")
	}
	cfg := applyOptions(opts)
	return &Validator{
		schema:    s,
		cfg:       cfg,
		evaluator: eval.NewEvaluator(),
	}
}

// Validate validates a batch of raw instances of typeName. A batch that runs
// returns one entry per raw, the zero instance where the validator refused it,
// beside a *[diag.ResultError] holding every refusal, or nil when it refused
// none; a batch that does not run (a type name that does not resolve, an
// abstract or part type named by an empty batch, a cancellation) returns a nil
// slice beside its error. An issue about one element of raws carries one
// [diag.DetailKeyInstanceIndex] detail naming it, and one about typeName itself
// carries none. The error carries each instance's truncation facts
// ([diag.Result.LimitReached], [diag.Result.DroppedCount]), save a cancelled
// row's, whose issues other than E_INTERNAL it drops, and Validate panics if
// the receiver or ctx is nil.
func (v *Validator) Validate(ctx context.Context, typeName string, raws []RawInstance) ([]ValidInstance, error) {
	if v == nil {
		panic("instance.Validate: nil validator receiver")
	}
	if ctx == nil {
		panic("instance.Validate: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, resultErr(cancelledResult(err, batchProvenance(raws)))
	}

	// The type resolves before the empty-batch answer: an unknown name is a
	// failure whatever the batch.
	typ, found := v.schema.ResolveTypeName(typeName)
	if !found {
		return nil, resultErr(typeResolutionResultForBatch(typeName, raws))
	}
	if len(raws) == 0 {
		// An empty batch names a type that must be able to hold a root, as a
		// row of it must: an abstract or part type name is refused either way.
		if res, refused := instantiationRefusal(typ, typeName, false, Provenance{}); refused {
			return nil, resultErr(res)
		}
		if raws == nil {
			return nil, nil
		}
		return []ValidInstance{}, nil
	}

	batchCollector := diag.NewCollector(diag.NoLimit)
	valids := make([]ValidInstance, 0, len(raws))

	for i := range raws {
		// One cancellation rule: a batch that did not complete returns nil and
		// exactly one E_CONTEXT_CANCELLED, stamped with the row it stopped on.
		// The check at the head means no further row is validated; the check
		// after the row drops that row's partial diagnostics, because the
		// caller is told the batch did not run.
		if err := ctx.Err(); err != nil {
			batchCollector.MergeFunc(cancelledResult(err, raws[i].Provenance), stampIndex(i))
			return nil, batchCollector.Err()
		}
		inst, result := v.validateComposedInstance(ctx, typeName, typ, raws[i], basePath(raws[i].Provenance), false, 0)
		if err := ctx.Err(); err != nil {
			batchCollector.MergeFunc(keepInternalErrors(result), stampIndex(i))
			batchCollector.MergeFunc(cancelledResult(err, raws[i].Provenance), stampIndex(i))
			return nil, batchCollector.Err()
		}
		batchCollector.MergeFunc(result, stampIndex(i))
		valids = append(valids, inst)
	}

	return valids, batchCollector.Err()
}

// ValidateOne validates a single raw instance of typeName. It returns the valid
// instance, or the zero instance beside a *[diag.ResultError] holding its
// refusals. It panics if the receiver or ctx is nil.
func (v *Validator) ValidateOne(ctx context.Context, typeName string, raw RawInstance) (ValidInstance, error) {
	if v == nil {
		panic("instance.ValidateOne: nil validator receiver")
	}
	if ctx == nil {
		panic("instance.ValidateOne: nil context")
	}
	if err := ctx.Err(); err != nil {
		return ValidInstance{}, resultErr(cancelledResult(err, raw.Provenance))
	}

	typ, found := v.schema.ResolveTypeName(typeName)
	if !found {
		return ValidInstance{}, resultErr(createErrorResult(diag.E_INSTANCE_TYPE_NOT_FOUND, typeNotFound(typeName), raw.Provenance))
	}
	inst, res := v.validateComposedInstance(ctx, typeName, typ, raw, basePath(raw.Provenance), false, 0)
	// The batch rule holds at this entry too, which the snapshot revalidator
	// uses: a row the caller is told did not run reports the cancellation and no
	// partial finding, so a timeout's leftovers are never read as findings about
	// the data.
	if err := ctx.Err(); err != nil {
		out := diag.NewCollector(diag.NoLimit)
		out.Merge(keepInternalErrors(res))
		out.Merge(cancelledResult(err, raw.Provenance))
		return ValidInstance{}, out.Err()
	}
	return inst, resultErr(res)
}

// resultErr returns res as a *[diag.ResultError], or nil when it holds no
// failure. The validator draws no issue below failure severity, so res holds
// failures alone, sorted and counted by the collector that built it.
func resultErr(res diag.Result) error {
	if counts := res.SeverityCounts(); counts.Fatal+counts.Errors == 0 {
		return nil
	}
	return &diag.ResultError{Result: res}
}

// stampIndex returns the transform that marks an issue with the batch index
// of the instance it belongs to.
func stampIndex(i int) func(diag.Issue) diag.Issue {
	index := strconv.Itoa(i)
	return func(issue diag.Issue) diag.Issue {
		return diag.FromIssue(issue).WithDetail(diag.DetailKeyInstanceIndex, index).Build()
	}
}

// basePath returns the path an instance's diagnostics start from: its
// provenance's path, or [path.Root] when the provenance records none. A
// provenance keeps an empty path as given, so a snapshot writes it back.
func basePath(prov Provenance) path.Path {
	if p := prov.Path(); p != "" {
		return p
	}
	return path.Root
}

// cancelledResult is the Fatal E_CONTEXT_CANCELLED result, anchored on the
// provenance in hand so a cancelled batch still names the row it stopped on.
func cancelledResult(err error, prov Provenance) diag.Result {
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(cancelledIssue(err, prov, basePath(prov)))
	return c.Result()
}

func cancelledIssue(err error, prov Provenance, base path.Path) diag.Issue {
	issue := diag.NewIssue(diag.Fatal, diag.E_CONTEXT_CANCELLED, err.Error())
	withProvenance(issue, prov, base.String())
	return issue.Build()
}

// cancelled reports whether ctx is done and, when it is, records the one
// E_CONTEXT_CANCELLED an instance carries — at base, unless a child's result
// already put one in the collector. Every point in an instance's validation
// that ends work on cancellation goes through here.
//
// [Validator.Validate] and [Validator.ValidateOne] replace what a cancelled row
// recorded with their own cancellation, because the caller is told the row did
// not run. Its Fatal E_INTERNAL stays ([keepInternalErrors]): a library defect
// outranks a deadline on the same row, and reporting the defect as the deadline
// hides it.
func cancelled(ctx context.Context, collector *diag.Collector, prov Provenance, base path.Path) bool {
	err := ctx.Err()
	if err == nil {
		return false
	}
	collector.Collect(cancelledIssue(err, prov, base))
	return true
}

// keepInternalErrors is the part of a cancelled row's diagnostics that
// outlives the drop: its Fatal E_INTERNAL issues, and nothing else. A library
// defect and a deadline can land on one row, and reporting the defect as the
// deadline hides a class of bug behind a timeout.
func keepInternalErrors(res diag.Result) diag.Result {
	c := diag.NewCollector(diag.NoLimit)
	c.MergeRetag(res,
		func(sev diag.Severity, code diag.Code) (diag.Severity, bool) { return sev, code == diag.E_INTERNAL },
		func(issue diag.Issue) diag.Issue { return issue })
	return c.Result()
}

// internalIssue is the Fatal E_INTERNAL an InternalError becomes, carrying
// its stack and the provenance in hand.
func internalIssue(internalErr *InternalError, prov Provenance, base path.Path) diag.Issue {
	issue := diag.NewIssue(diag.Fatal, diag.E_INTERNAL, internalErr.Error()).
		WithDetail(diag.DetailKeyStackTrace, internalErr.Stack)
	withProvenance(issue, prov, base.String())
	return issue.Build()
}

func firstProvenance(raws []RawInstance) Provenance {
	if len(raws) > 0 {
		return raws[0].Provenance
	}
	return Provenance{}
}

// validateComposedBatch validates raw children of rel's target type at the
// given composed depth, each anchored on its base path, and hands collect each
// child's result. The target is resolved by the relation's completion-recorded
// absolute identity (see resolveRelationTarget), so a relation declared on an
// imported type or inherited from a cross-schema parent — whose syntactic target
// ref is meaningful only in its declaring schema — validates against the true
// target. A cancellation stops the batch, and the entry above it reports that
// cancellation and keeps only the E_INTERNAL of what was collected.
func (v *Validator) validateComposedBatch(
	ctx context.Context,
	rel schema.Relation,
	raws []RawInstance,
	bases []path.Path,
	depth int,
	collect func(res diag.Result),
) ([]ValidInstance, schema.Type) {
	targetType, _ := resolveRelationTarget(v.schema, rel)
	// A composed child is named the way graph and snapshot name it: the
	// entry-relative form, never the declaring schema's alias. Identity is
	// TypeID; the name is display.
	targetTypeName := schema.TagForm(v.schema, targetType.ID())

	valids := make([]ValidInstance, 0, len(raws))
	for i := range raws {
		if ctx.Err() != nil {
			return nil, targetType
		}
		inst, result := v.validateComposedInstance(ctx, targetTypeName, targetType, raws[i], bases[i], true, depth)
		collect(result)
		valids = append(valids, inst)
	}
	return valids, targetType
}

// resolveRelationTarget resolves a relation's target type by the absolute
// identity recorded at completion ([schema.Relation.TargetID]), searched
// across the schema's import closure. The syntactic target ref is
// declaring-schema-relative — its qualifier is the declaring schema's import
// alias, and its bare form names a declaring-schema type — so re-resolving it
// against the entry schema would misresolve relations declared on imported
// types or inherited from cross-schema parents. Every relation a completed
// schema holds carries its TargetID, and the schema's closure holds that type.
func resolveRelationTarget(s schema.Schema, rel schema.Relation) (schema.Type, bool) {
	return s.TypeByID(rel.TargetID())
}

// relationByEitherName resolves a relation by its DSL name or its field
// name, the two spellings every name-taking surface of this package accepts.
func relationByEitherName(t schema.Type, name string) (schema.Relation, bool) {
	if r, ok := t.Relation(name); ok {
		return r, true
	}
	return t.RelationByField(name)
}

// typeNotFound is the message for a type name [schema.Schema.ResolveTypeName]
// does not resolve.
func typeNotFound(typeName string) string {
	return fmt.Sprintf("type %s not found", diag.QuoteValue(typeName))
}

// batchProvenance is the provenance of a batch-wide failure — the type or
// relation name, not any row: the source the rows came from, at its root,
// with no span. Blaming raws[0] pointed the operator at a row that was fine.
func batchProvenance(raws []RawInstance) Provenance {
	return NewProvenance(firstProvenance(raws).SourceName(), path.Root, location.Span{})
}

// typeResolutionResultForBatch creates a diagnostic result for type resolution errors in batch contexts.
func typeResolutionResultForBatch(typeName string, raws []RawInstance) diag.Result {
	return createErrorResult(diag.E_INSTANCE_TYPE_NOT_FOUND, typeNotFound(typeName), batchProvenance(raws))
}

// validateComposedInstance validates an instance, optionally allowing part
// types. typeName is the entry-relative type name stored on the result and
// carried on its diagnostics; base is the instance's location in the input
// document, which every diagnostic's path is built from; depth is its
// composed depth, 0 for a root.
func (v *Validator) validateComposedInstance(ctx context.Context, typeName string, typ schema.Type, raw RawInstance, base path.Path, allowPartType bool, depth int) (ValidInstance, diag.Result) {
	if res, refused := instantiationRefusal(typ, typeName, allowPartType, raw.Provenance); refused {
		return ValidInstance{}, res
	}
	return v.validateProperties(ctx, typ, typeName, raw, base, depth)
}

// instantiationRefusal refuses an abstract type, and a part type unless
// allowPartType holds, reporting whether it refused. It needs no key check:
// completion refuses a concrete type with no primary key, declared or inherited
// ([diag.E_NO_PRIMARY_KEY], unless an error about its key's type or a supertype
// already refuses it), so with allowPartType false, a type it passes under a
// resolved name can hold a root.
func instantiationRefusal(typ schema.Type, typeName string, allowPartType bool, prov Provenance) (diag.Result, bool) {
	switch {
	case typ.IsAbstract():
		return createErrorResult(diag.E_ABSTRACT_TYPE, fmt.Sprintf("cannot instantiate abstract type %q", typeName), prov), true
	case !allowPartType && typ.IsPart():
		return createErrorResult(diag.E_PART_TYPE_DIRECT, fmt.Sprintf("part type %q cannot be instantiated directly", typeName), prov), true
	}
	return diag.Result{}, false
}

// validateProperties validates all members of an instance: properties, then
// the primary key, edges, compositions, and invariants last.
func (v *Validator) validateProperties(ctx context.Context, typ schema.Type, typeName string, raw RawInstance, base path.Path, depth int) (ValidInstance, diag.Result) {
	collector := diag.NewCollector(v.cfg.maxIssuesPerInstance)
	prov := raw.Provenance

	checkUnknownFields(typ, typeName, raw.Properties, collector, prov, base)

	// Each pass is gated on ITS OWN errors, taken as a count before and after —
	// the use Collector.ErrorCount's godoc names. Asking HasErrors made an
	// unknown field stop every pass after it, so a row reported one defect per
	// round trip.
	beforeProperties := collector.ErrorCount()

	// Validate each property and build the validated properties map
	validatedProps := make(map[string]any)
	for prop := range typ.AllProperties() {
		rawValue := raw.Properties[prop.Name()]
		// Check required properties
		if value.Absent(rawValue) && prop.IsRequired() {
			issue := diag.NewIssue(
				diag.Error,
				diag.E_MISSING_REQUIRED,
				fmt.Sprintf("missing required property %q", prop.Name()),
			).WithDetails(diag.TypeProp(typeName, prop.Name())...)
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
			continue
		}

		if value.Absent(rawValue) {
			continue
		}

		propPath := base.Key(prop.Name()).String()

		// Validate property type
		if err := checkValueRecovering(rawValue, prop.Constraint()); err != nil {
			issue := checkIssue(err, fmt.Sprintf("property %q", prop.Name())).
				WithDetails(diag.TypeProp(typeName, prop.Name())...)
			withProvenance(issue, prov, propPath)
			built := issue.Build()
			collector.Collect(built)
			if built.Severity() == diag.Fatal {
				cancelled(ctx, collector, prov, base)
				return ValidInstance{}, collector.Result()
			}
			continue
		}

		// Coerce to canonical type (int64, float64, []float64)
		coercedValue, err := coerceValueRecovering(rawValue, prop.Constraint())
		if err != nil {
			if internalErr, ok := errors.AsType[*InternalError](err); ok {
				collector.Collect(internalIssue(internalErr, prov, base))
				cancelled(ctx, collector, prov, base)
				return ValidInstance{}, collector.Result()
			}
			// This should not happen after the value check above passed
			issue := diag.NewIssue(
				diag.Error,
				diag.E_TYPE_MISMATCH,
				fmt.Sprintf("property %q: coercion error: %s", prop.Name(), err.Error()),
			).WithDetails(diag.TypeProp(typeName, prop.Name())...)
			withProvenance(issue, prov, propPath)
			collector.Collect(issue.Build())
			continue
		}

		// Store validated and coerced property (will be cloned when wrapping)
		validatedProps[prop.Name()] = coercedValue
	}

	// The property pass's own errors stop the passes after it, and an unknown
	// field does not, so the edge and composition passes report beside it.
	if cancelled(ctx, collector, prov, base) {
		return ValidInstance{}, collector.Result()
	}
	if collector.ErrorCount() > beforeProperties {
		return ValidInstance{}, collector.Result()
	}

	// extractPrimaryKey reports nothing; it reads what the pass above validated.
	pkComponents := v.extractPrimaryKey(typ, validatedProps)

	beforeEdges := collector.ErrorCount()
	edges := v.validateEdges(ctx, typ, raw.Properties, collector, prov, base)
	if cancelled(ctx, collector, prov, base) {
		return ValidInstance{}, collector.Result()
	}
	if collector.ErrorCount() > beforeEdges {
		return ValidInstance{}, collector.Result()
	}

	beforeCompositions := collector.ErrorCount()
	composed := v.validateCompositions(ctx, typ, raw.Properties, collector, prov, base, depth)
	if cancelled(ctx, collector, prov, base) {
		return ValidInstance{}, collector.Result()
	}
	if collector.ErrorCount() > beforeCompositions {
		return ValidInstance{}, collector.Result()
	}

	// The final gate before the instance is built stays HasErrors: a row with
	// ANY error yields the zero instance and runs no invariants.
	if collector.HasErrors() {
		return ValidInstance{}, collector.Result()
	}

	// The instance is built before its invariants run, so they read the one
	// scope its parent will read too (scopeOf). A failed invariant discards it.
	validInstance := newValidInstance(
		typeName,
		typ,
		immutable.WrapKey(pkComponents),
		immutable.WrapProperties(validatedProps),
		edges,
		composed,
		prov,
	)

	// Evaluate invariants LAST, over properties AND relations: an invariant
	// may name a relation, and evaluating before the relations were computed
	// left every one of them reading nil.
	if err := v.evaluateInvariants(ctx, typ, typeName, validInstance, collector, prov, base); err != nil {
		if internalErr, ok := errors.AsType[*InternalError](err); ok {
			collector.Collect(internalIssue(internalErr, prov, base))
		} else {
			cancelled(ctx, collector, prov, base)
		}
		return ValidInstance{}, collector.Result()
	}
	if collector.HasErrors() {
		return ValidInstance{}, collector.Result()
	}

	return validInstance, collector.Result()
}

// checkUnknownFields reports each input key that names no property and no
// relation field of typ, in sorted order so the set that survives a truncated
// collector is a function of the input.
func checkUnknownFields(typ schema.Type, typeName string, props map[string]any, collector *diag.Collector, prov Provenance, base path.Path) {
	for _, inputName := range slices.Sorted(maps.Keys(props)) {
		if _, ok := typ.Property(inputName); ok {
			continue
		}
		if _, ok := typ.RelationByField(inputName); ok {
			continue
		}
		issue := diag.NewIssue(
			diag.Error,
			diag.E_UNKNOWN_FIELD,
			"unknown field "+diag.QuoteValue(inputName),
		).WithDetails(diag.TypeField(typeName, inputName)...)
		withProvenance(issue, prov, base.Key(inputName).String())
		collector.Collect(issue.Build())
	}
}

// scopeOf returns inst's invariant scope, built once per instance and shared:
// the instance's own invariants read it, and every parent that holds the
// instance as a composed child holds this same map in its own scope. One
// scope per instance is what keeps a composed tree's evaluation linear in
// its depth.
func (v *Validator) scopeOf(inst ValidInstance) immutable.Properties {
	inst.scope.once.Do(func() {
		inst.scope.m = immutable.WrapProperties(v.invariantScope(inst))
	})
	return inst.scope.m
}

// invariantScope builds the name→value map an invariant is evaluated against:
// the validated properties, plus one entry per relation keyed by its
// UPPER_SNAKE name. A property's entry is the instance's own immutable value,
// not a copy; a composition's holds each child's memoised scope, shared, in a
// list built for a many composition; an association's is built from cloned
// target keys. The evaluator reads an entry by its exact key, a property's
// name or a relation's declared name, as docs/SPEC.md states.
//
// Relations belong here because the schema completer's membersOf index admits
// them, so an invariant may reference one. The two relation
// kinds carry different amounts of information, and the difference is not
// incidental:
//
//   - A COMPOSITION's children are part of this instance, so the entry is the
//     single child's own scope, or the list of the children's scopes for a
//     many composition. `ITEMS -> Len`, and a lambda over a child's own
//     properties, both read real values.
//   - An ASSOCIATION's target is a REFERENCE. The instance holds the foreign
//     key, never the target's row, so the entry is the single target key, or
//     the list of target keys for a many association, ordered as Sort orders them.
//     Presence and cardinality are answerable; the target's properties are not
//     in this instance to answer with.
//
// The entries are a function of the records a graph holds, so a root a merge
// grew and a root the snapshot reader rebuilds read as the record that wrote
// them: a graph holds an association's targets as a set and keeps no record of
// an optional listing written empty. So a many relation is a list, empty when
// it names nothing, and a single-valued relation with no value is absent from
// the map and evaluates to nil. inst's type is in v's schema, since v built
// inst.
func (v *Validator) invariantScope(inst ValidInstance) map[string]any {
	props := inst.Properties()
	scope := make(map[string]any, props.Len()+len(inst.edges)+len(inst.composed))
	for k, val := range props.All() {
		scope[k] = val.Any()
	}
	typ := inst.typ

	for rel := range typ.AllAssociations() {
		var keys []any
		for t := range inst.edges[rel.Name()].Targets() {
			keys = append(keys, keyValue(t.TargetKey()))
		}
		if len(keys) == 0 && !rel.IsMany() {
			continue
		}
		if keys == nil {
			keys = []any{}
		}
		slices.SortFunc(keys, keyOrder)
		scope[rel.Name()] = relationValue(rel, keys)
	}

	for rel := range typ.AllCompositions() {
		children := inst.composed[rel.Name()]
		if len(children) == 0 && !rel.IsMany() {
			continue
		}
		scope[rel.Name()] = v.composedScopeValue(rel, children)
	}

	return scope
}

// composedScopeValue renders a composition as docs/SPEC.md states: the single
// child for a composition that is not many, a list otherwise, each child its
// own memoised scope — an instance whose properties and relations are in
// scope, held rather than rebuilt.
func (v *Validator) composedScopeValue(rel schema.Relation, children []ValidInstance) any {
	scopes := make([]any, 0, len(children))
	for _, child := range children {
		scopes = append(scopes, v.scopeOf(child))
	}
	if rel.IsMany() {
		return scopes
	}
	if len(scopes) == 0 {
		return nil
	}
	return scopes[0]
}

// relationValue renders a relation's entries for the invariant scope: a
// single-valued relation yields its one entry, so `WORKS_AT != nil` reads as a
// presence test rather than a one-element list.
func relationValue(rel schema.Relation, entries []any) any {
	if rel.IsMany() {
		return entries
	}
	if len(entries) == 0 {
		return nil
	}
	return entries[0]
}

// keyOrder orders two target keys as the DSL's Sort orders them. A key's
// components are values the total order ranks, so it panics only on a defect,
// which the invariant pass recovers as an internal error.
func keyOrder(a, b any) int {
	c, err := value.Order(a, b)
	if err != nil {
		panic("instance: a target key the total order cannot rank: " + err.Error())
	}
	return c
}

// keyValue renders a target key for the invariant scope: a single-component key
// as its component, so `WORKS_AT == "c1"` compares against the value the caller
// wrote, and a composite key as the component list.
func keyValue(k immutable.Key) any {
	parts := k.Clone()
	if len(parts) == 1 {
		return parts[0]
	}
	return parts
}

// evaluateInvariants evaluates every invariant of typ against inst's members.
// The scope is inst's memo, built here under the recover that turns an
// invariant-path panic into an InternalError, and only for a type that
// declares an invariant to read it.
//
// Invariants are evaluated independently - a failure in one invariant does not
// prevent evaluation of subsequent invariants. All failures are collected before
// returning, enabling comprehensive error reporting in a single validation pass.
func (v *Validator) evaluateInvariants(
	ctx context.Context,
	typ schema.Type,
	typeName string,
	inst ValidInstance,
	collector *diag.Collector,
	prov Provenance,
	base path.Path,
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

		tree := expr.InvariantExpression(inv)
		if scope == nil {
			scope = eval.PropertyScopeOf(v.scopeOf(inst))
		}

		result, err := v.evaluator.EvaluateBool(ctx, tree, scope)
		if err != nil {
			issue := diag.NewIssue(
				diag.Error,
				diag.E_EVAL_ERROR,
				fmt.Sprintf("invariant %q: %v", inv.Name(), err),
			).WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyInvariantName, inv.Name())
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
			continue
		}

		if !result {
			issue := diag.NewIssue(
				diag.Error,
				diag.E_INVARIANT_FAIL,
				inv.Name(),
			).WithDetail(diag.DetailKeyTypeName, typeName).
				WithDetail(diag.DetailKeyInvariantName, inv.Name())
			withProvenance(issue, prov, base.String())
			collector.Collect(issue.Build())
		}
	}

	return nil
}

// extractPrimaryKey extracts primary key components from validated properties.
// A primary key is required by definition, so an absent or nil component was
// already reported as E_MISSING_REQUIRED by the property pass and is skipped
// here.
func (v *Validator) extractPrimaryKey(typ schema.Type, props map[string]any) []any {
	var pkComponents []any

	for pk := range typ.PrimaryKeys() {
		val, ok := props[pk.Name()]
		if !ok || val == nil {
			continue
		}
		pkComponents = append(pkComponents, val)
	}

	return pkComponents
}

// PrimaryKeyOf returns the type and primary key raw would carry as a valid root
// instance of typeName, whether or not the validator accepts the rest of raw:
// each key property is read, checked and coerced as [Validator.Validate] reads
// it. It reports false when typeName names no type that can hold a root, or a
// key component is absent or fails its constraint. Panics if the
// receiver is nil.
func (v *Validator) PrimaryKeyOf(typeName string, raw RawInstance) (schema.TypeID, immutable.Key, bool) {
	if v == nil {
		panic("instance.PrimaryKeyOf: nil validator receiver")
	}
	typ, found := v.schema.ResolveTypeName(typeName)
	if !found {
		return schema.TypeID{}, immutable.Key{}, false
	}
	if _, refused := instantiationRefusal(typ, typeName, false, Provenance{}); refused {
		return schema.TypeID{}, immutable.Key{}, false
	}
	var components []any
	for pk := range typ.PrimaryKeys() {
		val := raw.Properties[pk.Name()]
		if value.Absent(val) || checkValueRecovering(val, pk.Constraint()) != nil {
			return schema.TypeID{}, immutable.Key{}, false
		}
		coerced, err := coerceValueRecovering(val, pk.Constraint())
		if err != nil {
			return schema.TypeID{}, immutable.Key{}, false
		}
		components = append(components, coerced)
	}
	if len(components) == 0 {
		return schema.TypeID{}, immutable.Key{}, false
	}
	return typ.ID(), immutable.WrapKey(components), true
}

// withProvenance sets an issue's path and source name, and its span when the
// provenance carries one. pathStr is the issue's own location in the input
// document; prov supplies the document's name and span.
func withProvenance(b *diag.IssueBuilder, prov Provenance, pathStr string) *diag.IssueBuilder {
	b.WithPath(prov.SourceName(), pathStr)
	if !prov.Span().IsZero() {
		b.WithSpan(prov.Span())
	}
	return b
}

// createErrorResult creates a diag.Result with a single error issue at the
// provenance's own location.
func createErrorResult(code diag.Code, message string, prov Provenance) diag.Result {
	collector := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(
		diag.Error,
		code,
		message,
	)
	withProvenance(issue, prov, basePath(prov).String())
	collector.Collect(issue.Build())
	return collector.Result()
}

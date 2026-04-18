package instance

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// SchemaBuilder is a schema-aware raw-instance builder.
//
// Unlike a free-form map literal, SchemaBuilder is bound to a specific schema
// type at construction time and validates property names, relation names, and
// cardinality invariants as they are added — shifting the failure domain from
// "at ValidateOne time" to "at Build time." Shape errors (unknown property,
// unknown relation, cardinality mismatch, EdgeTo-vs-EdgeToWith mismatch,
// wrong composed type) accumulate on the builder and surface from Build()
// with a call-site file:line locator captured via runtime.Callers.
//
// SchemaBuilder is constructed via [BuilderFor].
//
// # Scope of validation
//
// Build catches shape errors (schema-structural): property/relation names,
// cardinality, the EdgeTo-vs-EdgeToWith split, wrong composition child type.
// It does NOT catch value-level errors (wrong type, out-of-range, invalid PK);
// those remain in [Validator.ValidateOne]'s domain. A SchemaBuilder that
// returns a clean (RawInstance, nil) from Build is guaranteed to pass the
// shape portion of ValidateOne.
//
// # Thread safety
//
// SchemaBuilder is NOT concurrent-safe; its internal state mutates as
// methods are called. Construct one builder per goroutine. A single
// [*schema.Schema] is immutable and may be shared freely across multiple
// builders on multiple goroutines.
//
// # One-shot on error
//
// After Build returns a non-nil error, the builder's internal state reflects
// partial accumulation. Subsequent method calls have undefined semantics
// (accumulated errors remain; new mutations may or may not be recorded
// cleanly). Do NOT reuse a builder after a failed Build; construct a fresh
// one via BuilderFor.
//
// # Performance
//
// Each builder method (Property, EdgeTo, EdgeToWith, Composed) captures the
// caller's file:line via a runtime.Callers + runtime.CallersFrames pair so
// Build-time shape errors can name the offending call site. Measured per-
// method overhead is ~400–800 ns per call on M2-class hardware; at the
// typical I/O-bound pipeline scale (low-thousands records per batch,
// 3–4 builder-method calls per record) the aggregate per-batch overhead
// is ~1–10 ms — genuinely noise against pipeline wall-clock. At a 100k+
// records-per-batch ceiling aggregate overhead reaches ~100–400 ms, still
// well below validation cost but no longer negligible. See
// [BenchmarkSchemaBuilder_CallerCapture] for the in-tree pin.
type SchemaBuilder struct {
	schema       *schema.Schema
	typeName     string // user-provided (may be qualified)
	typ          *schema.Type
	properties   map[string]any
	edges        map[string]*edgeState
	compositions map[string]*compositionState
	provenance   *location.Provenance
	errors       []*buildError

	// relByFieldName is a lazy-built secondary index that maps FieldName()
	// (lower_snake form, e.g. "in_county") to its relation. Built on first
	// miss against typ.Relation (which indexes by DSL name, e.g. "IN_COUNTY")
	// so callers using either form resolve without needing to know which
	// the schema author used.
	relByFieldName map[string]*schema.Relation
}

// edgeShape pins the call-shape of edge-producing invocations on a single
// relation. First call selects; subsequent mismatched calls surface as a
// shape error.
type edgeShape uint8

const (
	shapeUnset edgeShape = iota
	shapeTo
	shapeToWith
)

func (s edgeShape) String() string {
	switch s {
	case shapeTo:
		return "EdgeTo"
	case shapeToWith:
		return "EdgeToWith"
	default:
		return "unset"
	}
}

type edgeState struct {
	rel         *schema.Relation
	shape       edgeShape
	shapeCaller string // file:line of the call that pinned the shape
	targets     []map[string]any
}

type compositionState struct {
	rel         *schema.Relation
	children    []*SchemaBuilder
	callerLines []string // parallel to children; caller file:line per accepted child
}

// BuilderFor constructs a schema-aware builder for instances of the given
// type. Returns an error if s is nil, typeName is not in the schema, or the
// named type is abstract (abstract types cannot be directly instantiated).
// Part types are permitted so callers can construct composition children.
//
// typeName may be unqualified ("Person") or qualified with an import alias
// ("cross.Person"), matching the name-resolution rules [Validator] applies.
func BuilderFor(s *schema.Schema, typeName string) (*SchemaBuilder, error) {
	if s == nil {
		return nil, errors.New("instance.BuilderFor: nil schema")
	}
	ref := parseTypeRef(typeName)
	typ, found := s.ResolveType(ref)
	if !found {
		return nil, fmt.Errorf("instance.BuilderFor: type %q not found", typeName)
	}
	if typ.IsAbstract() {
		return nil, fmt.Errorf("instance.BuilderFor: cannot build abstract type %q", typeName)
	}
	return &SchemaBuilder{
		schema:       s,
		typeName:     typeName,
		typ:          typ,
		properties:   make(map[string]any),
		edges:        make(map[string]*edgeState),
		compositions: make(map[string]*compositionState),
	}, nil
}

// Property sets a property value on the instance under construction.
//
// If name is not a property of the bound type, Property records the error
// with the caller's file:line (captured via runtime.Callers); Build() returns
// the accumulated error including the locator. Property name matching is
// case-sensitive against the schema's canonical names — the programmatic API
// opts out of the case-insensitive fallback the validator performs on
// JSON inputs.
//
// Property(name, nil) passes through; the validator handles nil-value
// semantics (emitting E_MISSING_REQUIRED when the property is required).
func (b *SchemaBuilder) Property(name string, value any) *SchemaBuilder {
	caller := captureCaller()
	prop, ok := b.typ.Property(name)
	if !ok {
		b.recordErr(&buildError{
			kind:   kindUnknownProperty,
			typ:    b.typeName,
			target: name,
			caller: caller,
		})
		return b
	}
	b.properties[prop.Name()] = value
	return b
}

// EdgeTo adds one edge target to the named association relation for
// associations that do NOT declare edge properties. Variadic targetKey
// supports single-component and composite primary keys:
//
//	b.EdgeTo("in_state", stateFP)              // single-component PK
//	b.EdgeTo("part_of", issuerID, issueID)     // composite PK
//	b.EdgeTo("part_of", prebuiltKey...)        // pre-built slice
//
// name accepts either the schema's DSL form (e.g. "IN_STATE") or the
// lower_snake FieldName form (e.g. "in_state"); both resolve to the same
// relation.
//
// For "many"-cardinality relations, call EdgeTo multiple times — once per
// target. Cardinality is enforced at Build time: a "one" relation with more
// than one EdgeTo call surfaces a cardinality error; a "one" relation called
// via both EdgeTo and EdgeToWith is a shape error.
//
// If the relation declares edge properties, EdgeTo records a shape error
// ("relation X declares edge properties; use EdgeToWith") — each edge
// producer asserts its required shape on first call, regardless of whether
// the caller also invokes EdgeToWith later.
func (b *SchemaBuilder) EdgeTo(name string, targetKey ...any) *SchemaBuilder {
	caller := captureCaller()
	b.addEdge(name, targetKey, nil, shapeTo, caller)
	return b
}

// EdgeToWith adds one edge target plus edge properties to the named
// association. targetKey is passed as an explicit []any so props is
// unambiguous at the call site — no accidental absorption of the map into
// a variadic key slot.
//
//	b.EdgeToWith("overlaps_county", []any{countyGEOID}, map[string]any{
//	    "overlap_pct": 85.3,
//	})
//	b.EdgeToWith("part_of_weighted", []any{issuerID, issueID}, map[string]any{
//	    "weight": 0.5,
//	})
//
// If the relation does NOT declare edge properties, EdgeToWith records a
// shape error ("relation X has no edge properties; use EdgeTo").
//
// Property names are validated against the relation's declared edge
// properties at Build time; unknown keys surface with their name and the
// declared set. Missing required edge properties surface at Build time.
// Passing a nil or empty props map on a relation whose edge properties are
// all optional succeeds with no edge-property keys in the output target.
func (b *SchemaBuilder) EdgeToWith(name string, targetKey []any, props map[string]any) *SchemaBuilder {
	caller := captureCaller()
	b.addEdge(name, targetKey, props, shapeToWith, caller)
	return b
}

// addEdge is the shared implementation for EdgeTo and EdgeToWith. shape
// identifies which public entry produced the call; the two shapes cannot mix
// on the same relation.
func (b *SchemaBuilder) addEdge(name string, targetKey []any, props map[string]any, shape edgeShape, caller string) {
	rel, ok := b.resolveRelation(name)
	if !ok {
		b.recordErr(&buildError{
			kind:   kindUnknownRelation,
			typ:    b.typeName,
			target: name,
			caller: caller,
		})
		return
	}
	if !rel.IsAssociation() {
		b.recordErr(&buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: rel.Name() + " is a composition; use Composed",
			caller: caller,
		})
		return
	}
	switch shape {
	case shapeTo:
		if rel.HasProperties() {
			b.recordErr(&buildError{
				kind:   kindEdgeShape,
				typ:    b.typeName,
				target: rel.Name(),
				detail: "declares edge properties; use EdgeToWith",
				caller: caller,
			})
			return
		}
	case shapeToWith:
		if !rel.HasProperties() {
			b.recordErr(&buildError{
				kind:   kindEdgeShape,
				typ:    b.typeName,
				target: rel.Name(),
				detail: "has no edge properties; use EdgeTo",
				caller: caller,
			})
			return
		}
	}
	if len(targetKey) == 0 {
		b.recordErr(&buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("%s requires at least one target-key component", shape),
			caller: caller,
		})
		return
	}

	target, err := b.buildEdgeTarget(rel, targetKey, props, caller)
	if err != nil {
		// err is already shaped as *buildError; recordErr just appends.
		b.errors = append(b.errors, err)
		return
	}

	st := b.edgeStateFor(rel, shape, caller)
	if st == nil {
		return // shape mismatch already recorded
	}
	st.targets = append(st.targets, target)
}

// buildEdgeTarget assembles one edge-target map: {_target_<pk>: value, ...}
// with edge properties (if any) merged at the top level alongside the PK
// fields. Returns a shaped buildError on PK arity mismatch, missing target
// type, or unknown edge-property keys; otherwise nil.
func (b *SchemaBuilder) buildEdgeTarget(
	rel *schema.Relation,
	targetKey []any,
	props map[string]any,
	caller string,
) (map[string]any, *buildError) {
	targetType, found := b.schema.ResolveType(rel.Target())
	if !found {
		return nil, &buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("target type %q not found", rel.Target().String()),
			caller: caller,
		}
	}
	pks := targetType.PrimaryKeysSlice()
	if len(pks) == 0 {
		return nil, &buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("target type %q has no primary key", targetType.Name()),
			caller: caller,
		}
	}
	if len(targetKey) != len(pks) {
		return nil, &buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("target-key arity mismatch: expected %d component(s), got %d", len(pks), len(targetKey)),
			caller: caller,
		}
	}

	obj := make(map[string]any, len(pks)+len(props))
	for i, pk := range pks {
		obj[fkPrefix+pk.Name()] = targetKey[i]
	}

	for pname, pval := range props {
		ep, ok := rel.Property(pname)
		if !ok {
			b.recordErr(&buildError{
				kind:   kindUnknownEdgeProp,
				typ:    b.typeName,
				target: rel.Name(),
				detail: describeUnknownEdgeProp(pname, rel),
				caller: caller,
			})
			continue
		}
		obj[ep.Name()] = pval
	}
	return obj, nil
}

// describeUnknownEdgeProp formats the "unknown edge property X; declared: [a, b]"
// detail surfaced on kindUnknownEdgeProp.
func describeUnknownEdgeProp(name string, rel *schema.Relation) string {
	names := make([]string, 0, len(rel.PropertiesSlice()))
	for _, p := range rel.PropertiesSlice() {
		names = append(names, p.Name())
	}
	return fmt.Sprintf("%s; declared: [%s]", strconv.Quote(name), strings.Join(names, ", "))
}

// edgeStateFor returns the state for rel, creating it on first call. shape
// is pinned on the first edge call; subsequent calls with a different shape
// record a shape-mismatch error and return nil (the caller then skips the
// append).
func (b *SchemaBuilder) edgeStateFor(rel *schema.Relation, shape edgeShape, caller string) *edgeState {
	st, ok := b.edges[rel.Name()]
	if !ok {
		st = &edgeState{rel: rel, shape: shape, shapeCaller: caller}
		b.edges[rel.Name()] = st
		return st
	}
	if st.shape != shape {
		b.recordErr(&buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("cannot mix %s and %s on the same relation (first use: %s at %s)", st.shape, shape, st.shape, st.shapeCaller),
			caller: caller,
		})
		return nil
	}
	return st
}

// Composed adds one or more composed children to the named composition.
// Children are *SchemaBuilder values so the parent can enforce target-type
// matching at Build time against each child's bound type.
//
// Variadic shape supports all three call patterns:
//
//	b.Composed("addresses", a)              // single child
//	b.Composed("addresses", a, b, c)        // multiple children
//	b.Composed("addresses", children...)    // slice unpacking
//
// Repeated calls accumulate: two b.Composed("addresses", a).Composed("addresses", b)
// invocations are equivalent to b.Composed("addresses", a, b). Cardinality
// is enforced at Build time against the cumulative child count.
//
// A call with no children is a no-op — useful when the children list is
// computed at runtime and legitimately empty (schema-permitting).
//
// If the child's bound type does not match the composition's target type
// (or is not a subtype of an abstract target), Composed records a
// kindWrongComposedType error with the caller's file:line. Child builders
// are invoked (via each child's Build) when the parent's Build runs; child
// errors propagate tagged with the composition relation name.
func (b *SchemaBuilder) Composed(name string, children ...*SchemaBuilder) *SchemaBuilder {
	caller := captureCaller()
	rel, ok := b.resolveRelation(name)
	if !ok {
		b.recordErr(&buildError{
			kind:   kindUnknownRelation,
			typ:    b.typeName,
			target: name,
			caller: caller,
		})
		return b
	}
	if !rel.IsComposition() {
		b.recordErr(&buildError{
			kind:   kindEdgeShape,
			typ:    b.typeName,
			target: rel.Name(),
			detail: rel.Name() + " is an association; use EdgeTo or EdgeToWith",
			caller: caller,
		})
		return b
	}
	targetType, found := b.schema.ResolveType(rel.Target())
	if !found {
		b.recordErr(&buildError{
			kind:   kindWrongComposedType,
			typ:    b.typeName,
			target: rel.Name(),
			detail: fmt.Sprintf("target type %q not found", rel.Target().String()),
			caller: caller,
		})
		return b
	}
	st, ok := b.compositions[rel.Name()]
	if !ok {
		st = &compositionState{rel: rel}
		b.compositions[rel.Name()] = st
	}
	for _, c := range children {
		if c == nil {
			b.recordErr(&buildError{
				kind:   kindWrongComposedType,
				typ:    b.typeName,
				target: rel.Name(),
				detail: "nil child builder",
				caller: caller,
			})
			continue
		}
		if !acceptableChildType(targetType, c.typ) {
			b.recordErr(&buildError{
				kind:   kindWrongComposedType,
				typ:    b.typeName,
				target: rel.Name(),
				detail: fmt.Sprintf("expected %s, got %s", targetType.Name(), c.typ.Name()),
				caller: caller,
			})
			continue
		}
		st.children = append(st.children, c)
		st.callerLines = append(st.callerLines, caller)
	}
	return b
}

// acceptableChildType reports whether a child type can be composed under a
// declared target type. The schema layer enforces that composition targets
// are concrete part types (see schema/collision.go:274–287), so the only
// valid match is an exact ID match: a composition declared against Address
// accepts Address children and no others. Polymorphic compositions would
// require the validator to dispatch per-child on runtime type rather than
// on the declared target, which it does not (validate_composition.go:214
// re-validates children AS the declared target), so accepting subtypes here
// would just shift the rejection from Build-time to ValidateOne-time,
// defeating the primitive's core premise.
func acceptableChildType(target, child *schema.Type) bool {
	return target.ID() == child.ID()
}

// WithProvenance sets source location metadata for diagnostics. The path
// follows location/path canonical syntax (e.g. "$.Person[0]"); if pathStr
// fails to parse, it falls back to the root path to match the pre-existing
// [Builder.WithProvenance] semantics.
func (b *SchemaBuilder) WithProvenance(sourceName, pathStr string) *SchemaBuilder {
	p, err := path.Parse(pathStr)
	if err != nil {
		p = path.Root()
	}
	b.provenance = location.NewProvenance(sourceName, p, location.Span{})
	return b
}

// Build produces the RawInstance.
//
// If any earlier call on this builder recorded an error, Build returns the
// first such error. When more than one error accumulated, the return wraps
// the first via %w with a trailing "(and N more build error(s))" suffix so
// callers retain access to the primary cause via errors.Is / errors.As.
//
// On success the returned RawInstance is guaranteed to pass the shape
// portion of [Validator.ValidateOne]: property names and relation names are
// schema-valid, cardinality invariants hold, composition children match
// declared target types. Value-level validation (constraint checks, PK type
// coercion, reference integrity) still happens at ValidateOne time.
//
// Error messages include:
//   - the offending call's target (property or relation name)
//   - the bound type's name (for disambiguation across builders)
//   - the caller's file:line (captured via runtime.Callers at the offending
//     Property / EdgeTo / EdgeToWith / Composed call)
func (b *SchemaBuilder) Build() (RawInstance, error) {
	if err := b.firstErrorWithCount(); err != nil {
		return RawInstance{}, err
	}

	out := make(map[string]any, len(b.properties)+len(b.edges)+len(b.compositions))
	maps.Copy(out, b.properties)

	// Edges — iteration order on maps is nondeterministic; outputs go into
	// the RawInstance map under rel.FieldName() so no ordering-dependent
	// contract is exposed here.
	for _, st := range b.edges {
		if !st.rel.IsMany() && len(st.targets) > 1 {
			b.recordErr(&buildError{
				kind:   kindCardinality,
				typ:    b.typeName,
				target: st.rel.Name(),
				detail: fmt.Sprintf("%q is single-valued, got %d targets", st.rel.Name(), len(st.targets)),
			})
			continue
		}
		if st.shape == shapeToWith {
			for _, target := range st.targets {
				b.checkRequiredEdgeProps(st.rel, target)
			}
		}
		if st.rel.IsMany() {
			arr := make([]any, len(st.targets))
			for i, t := range st.targets {
				arr[i] = t
			}
			out[st.rel.FieldName()] = arr
		} else if len(st.targets) == 1 {
			out[st.rel.FieldName()] = st.targets[0]
		}
	}

	// Compositions — always emit []any per validate_composition.go's
	// "Compositions always expect an array" contract.
	for _, st := range b.compositions {
		if !st.rel.IsMany() && len(st.children) > 1 {
			b.recordErr(&buildError{
				kind:   kindCardinality,
				typ:    b.typeName,
				target: st.rel.Name(),
				detail: fmt.Sprintf("%q is single-valued, got %d children", st.rel.Name(), len(st.children)),
			})
			continue
		}
		if len(st.children) == 0 {
			continue
		}
		children := make([]any, 0, len(st.children))
		childFailed := false
		for i, c := range st.children {
			childRaw, childErr := c.Build()
			if childErr != nil {
				b.recordErr(&buildError{
					kind:   kindChildError,
					typ:    b.typeName,
					target: st.rel.Name(),
					caller: st.callerLines[i],
					child:  childErr,
				})
				childFailed = true
				continue
			}
			children = append(children, childRaw.Properties)
		}
		if childFailed {
			continue
		}
		out[st.rel.FieldName()] = children
	}

	if err := b.firstErrorWithCount(); err != nil {
		return RawInstance{}, err
	}
	return RawInstance{Properties: out, Provenance: b.provenance}, nil
}

// checkRequiredEdgeProps verifies that every required edge property on rel
// is present in the given edge-target map. Missing required props surface
// as kindMissingEdgeProp errors on b.
func (b *SchemaBuilder) checkRequiredEdgeProps(rel *schema.Relation, target map[string]any) {
	for ep := range rel.Properties() {
		if !ep.IsRequired() {
			continue
		}
		if _, ok := target[ep.Name()]; !ok {
			b.recordErr(&buildError{
				kind:   kindMissingEdgeProp,
				typ:    b.typeName,
				target: rel.Name(),
				detail: ep.Name(),
			})
		}
	}
}

// MustBuild is like Build but panics on error. Intended for tests where the
// inputs are known-good and a failure of the build itself is a programmer
// error worth surfacing as a panic. Production code should use Build and
// propagate the returned error.
func (b *SchemaBuilder) MustBuild() RawInstance {
	raw, err := b.Build()
	if err != nil {
		panic("instance.SchemaBuilder.MustBuild: " + err.Error())
	}
	return raw
}

// recordErr appends one shape-level error to the builder's accumulated list.
func (b *SchemaBuilder) recordErr(e *buildError) {
	b.errors = append(b.errors, e)
}

// firstErrorWithCount collapses the accumulated error list into a single
// returned error. When the list is empty, returns nil. When one error is
// present, returns it verbatim. When more than one, wraps the first via %w
// with a trailing "(and N more build error(s))" suffix so errors.Is /
// errors.As continue to reach the primary cause.
func (b *SchemaBuilder) firstErrorWithCount() error {
	if len(b.errors) == 0 {
		return nil
	}
	first := b.errors[0]
	if len(b.errors) == 1 {
		return first
	}
	return fmt.Errorf("%w (and %d more build error(s))", first, len(b.errors)-1)
}

// resolveRelation looks up a relation by name, accepting either the DSL form
// (e.g. "IN_COUNTY") or the FieldName form (e.g. "in_county"). Returns the
// resolved *schema.Relation and true on success; zero value and false on
// miss.
func (b *SchemaBuilder) resolveRelation(name string) (*schema.Relation, bool) {
	if r, ok := b.typ.Relation(name); ok {
		return r, true
	}
	if b.relByFieldName == nil {
		b.relByFieldName = make(map[string]*schema.Relation)
		for r := range b.typ.AllAssociations() {
			b.relByFieldName[r.FieldName()] = r
		}
		for r := range b.typ.AllCompositions() {
			b.relByFieldName[r.FieldName()] = r
		}
	}
	r, ok := b.relByFieldName[name]
	return r, ok
}

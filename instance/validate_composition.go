package instance

import (
	"context"
	"fmt"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// MaxComposedDepth is the deepest composed nesting a validated instance may
// hold: a root is depth 0, its composed children depth 1, and a child at a
// depth past this bound draws [ErrCompositionDepthExceeded]. The .ys wire
// enforces the same number on write and read, so every validated instance is
// one a snapshot can carry.
const MaxComposedDepth = 32

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

// validateCompositions validates all composition relations for an instance
// at the given composed depth. Returns a map of relation name -> composed
// children as immutable.Value.
func (v *Validator) validateCompositions(
	ctx context.Context,
	typ *schema.Type,
	in inputKeys,
	collector *diag.Collector,
	prov *location.Provenance,
	base path.Builder,
	depth int,
) map[string]immutable.Value {
	var composed map[string]immutable.Value

	for rel := range typ.AllCompositions() {
		if err := ctx.Err(); err != nil {
			return composed
		}

		inputKey, rawValue, hasValue := v.lookupRelationInput(rel, in, collector, prov, base)
		if !hasValue {
			// A collision was reported as the whole of what is wrong; an
			// absent required composition is reported here.
			if !rel.IsOptional() && !collector.HasErrors() {
				issue := diag.NewIssue(
					diag.Error,
					ErrUnresolvedRequiredComposition,
					fmt.Sprintf("missing required composition %q", rel.Name()),
				).WithDetail(diag.DetailKeyReason, "absent").
					WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyJSONField, rel.FieldName())
				withProvenance(issue, prov, base.String())
				collector.Collect(issue.Build())
			}
			continue
		}

		composedValue := v.validateComposition(ctx, rel, rawValue, collector, prov, base.Key(inputKey), depth)
		if !composedValue.IsNil() {
			if composed == nil {
				composed = make(map[string]immutable.Value)
			}
			composed[rel.Name()] = composedValue
		}
	}
	return composed
}

// validateComposition validates a present composition value at relPath, the
// input key's location, whose children sit at depth+1.
func (v *Validator) validateComposition(
	ctx context.Context,
	rel *schema.Relation,
	rawValue any,
	collector *diag.Collector,
	prov *location.Provenance,
	relPath path.Builder,
	depth int,
) immutable.Value {
	// An explicit null is a shape error whatever the optionality.
	if isAbsent(rawValue) {
		collector.Collect(shapeMismatch(rel, fmt.Sprintf("composition %q: null is not a valid composition value", rel.Name()),
			"array", "null", prov, relPath))
		return immutable.Value{}
	}

	// Compositions always expect an array (accept typed slices via reflection)
	arr, ok := toSliceOfAny(rawValue)
	if !ok {
		collector.Collect(shapeMismatch(rel, fmt.Sprintf("composition %q: expected array, got %s", rel.Name(), kindOf(rawValue)),
			"array", kindOf(rawValue), prov, relPath))
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
				WithDetail(diag.DetailKeyJSONField, rel.FieldName())
			withProvenance(issue, prov, relPath.String())
			collector.Collect(issue.Build())
			return immutable.Value{}
		}
		return immutable.Wrap([]*ValidInstance{})
	}

	// A composition always arrives as an array, so its multiplicity is not
	// settled by the shape the way a (one) association's object form is. A
	// slot given more children than it holds is the fact graph assembly
	// reports under the same code.
	if !rel.IsMany() && len(arr) > 1 {
		issue := diag.NewIssue(
			diag.Error,
			ErrDuplicateComposedPK,
			fmt.Sprintf("composition %q: (one) cardinality violated, got %d children", rel.Name(), len(arr)),
		).WithExpectedGot("1 child", fmt.Sprintf("%d children", len(arr))).
			WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName())
		withProvenance(issue, prov, relPath.String())
		collector.Collect(issue.Build())
		return immutable.Value{}
	}

	childDepth := depth + 1
	if childDepth > MaxComposedDepth {
		issue := diag.NewIssue(
			diag.Error,
			ErrCompositionDepthExceeded,
			fmt.Sprintf("composition %q: composed nesting depth %d exceeds limit %d", rel.Name(), childDepth, MaxComposedDepth),
		).WithDetail(diag.DetailKeyDepth, strconv.Itoa(childDepth)).
			WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName())
		withProvenance(issue, prov, relPath.String())
		collector.Collect(issue.Build())
		return immutable.Value{}
	}

	// Build raw instances for the children, each remembering its position in
	// the caller's array so a later diagnostic names that position and not
	// its position among the survivors of the shape filter.
	childRaws := make([]RawInstance, 0, len(arr))
	childBases := make([]path.Builder, 0, len(arr))
	inputIndex := make([]int, 0, len(arr))
	for i, elem := range arr {
		childObj, ok := toMapOfAny(elem)
		if !ok {
			collector.Collect(shapeMismatch(rel, "composition child must be an object, got "+kindOf(elem),
				"object", kindOf(elem), prov, relPath.Index(i)))
			continue
		}

		childBase := relPath.Index(i)
		var childProv *location.Provenance
		if prov != nil {
			childProv = location.NewProvenance(prov.SourceName(), childBase, prov.Span())
		}
		childRaws = append(childRaws, RawInstance{Properties: childObj, Provenance: childProv})
		childBases = append(childBases, childBase)
		inputIndex = append(inputIndex, i)
	}

	// Recursively validate children against the relation already in hand;
	// rel.Owner() is a declaring-schema-local name the entry schema may not
	// know, so no name round-trips through public tag resolution.
	// The relation detail is the innermost one — the relation that reached
	// the faulty child; an outer level must not stack a second entry under
	// the same keys.
	relationDetails := diag.PathRelation(rel.Name(), rel.FieldName())
	validChildren, childType := v.validateComposedBatch(ctx, rel, childRaws, childBases, childDepth, func(_ int, res diag.Result) {
		collector.MergeFunc(res, func(issue diag.Issue) diag.Issue {
			if hasDetailKey(issue, diag.DetailKeyRelationName) {
				return issue
			}
			return diag.FromIssue(issue).WithDetails(relationDetails...).Build()
		})
	})

	// Check for duplicate PKs among children - only for types that have PKs.
	// PK-less composed children use structural position (array index) for identity,
	// so no duplicate check is needed for them.
	if len(validChildren) > 0 {
		if childType != nil && childType.HasPrimaryKey() {
			seenPKs := make(map[string]int) // pk string -> first occurrence's input index
			for i, child := range validChildren {
				if child == nil {
					continue // failed validation; diagnostics already collected
				}
				pkStr := child.PrimaryKey().String()
				if firstIdx, exists := seenPKs[pkStr]; exists {
					issue := diag.NewIssue(
						diag.Error,
						ErrDuplicateComposedPK,
						fmt.Sprintf("duplicate primary key in composed children at indices %d and %d", firstIdx, inputIndex[i]),
					).WithDetail(diag.DetailKeyRelationName, rel.Name()).
						WithDetail(diag.DetailKeyJSONField, rel.FieldName()).
						WithDetail(diag.DetailKeyPrimaryKey, pkStr)
					withProvenance(issue, prov, pkPathFromInstance(relPath, child, childType, inputIndex[i]).String())
					collector.Collect(issue.Build())
				} else {
					seenPKs[pkStr] = inputIndex[i]
				}
			}
		}
	}

	if collector.HasErrors() {
		return immutable.Value{}
	}

	return immutable.Wrap(validChildren)
}

// hasDetailKey reports whether issue already carries a detail under key.
func hasDetailKey(issue diag.Issue, key string) bool {
	for _, d := range issue.Details() {
		if d.Key == key {
			return true
		}
	}
	return false
}

package instance

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// MaxComposedDepth is the deepest composed nesting a validated instance may
// hold: a root is depth 0, its composed children depth 1, and a child at a
// depth past this bound draws [diag.E_COMPOSITION_DEPTH_EXCEEDED]. The .ys reader
// refuses a document nested past the same number, so no validated instance is
// nested deeper than a snapshot can carry.
const MaxComposedDepth = 32

// pkPathFromInstance builds a PK-based path segment for a validated instance.
// Returns basePath.PK(...) if the child type has a primary key, otherwise basePath.Index(i).
// This is used for post-validation diagnostic paths where PK data is available.
func pkPathFromInstance(basePath path.Path, child ValidInstance, childType schema.Type, index int) path.Path {
	if !childType.HasPrimaryKey() {
		return basePath.Index(index)
	}

	pk := child.PrimaryKey()
	pks := slices.Collect(childType.PrimaryKeys())

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
			Value: pk.At(i).Any(),
		})
	}

	return basePath.PK(fields...)
}

// validateCompositions validates all composition relations for an instance
// at the given composed depth. Returns each relation's composed children by
// relation name.
func (v *Validator) validateCompositions(
	ctx context.Context,
	typ schema.Type,
	props map[string]any,
	collector *diag.Collector,
	prov Provenance,
	base path.Path,
	depth int,
) map[string][]ValidInstance {
	var composed map[string][]ValidInstance

	for rel := range typ.AllCompositions() {
		if err := ctx.Err(); err != nil {
			return composed
		}

		raw := props[rel.FieldName()]
		if value.Absent(raw) {
			// An absent required composition is reported here, beside an unknown
			// field or another composition's error; a property or edge error
			// ends the row before this pass.
			if !rel.IsOptional() {
				issue := diag.NewIssue(
					diag.Error,
					diag.E_UNRESOLVED_REQUIRED_COMPOSITION,
					fmt.Sprintf("missing required composition %q", rel.Name()),
				).WithDetail(diag.DetailKeyReason, "absent").
					WithDetail(diag.DetailKeyRelationName, rel.Name()).
					WithDetail(diag.DetailKeyJSONField, rel.FieldName())
				withProvenance(issue, prov, base.String())
				collector.Collect(issue.Build())
			}
			continue
		}

		children := v.validateComposition(ctx, rel, raw, collector, prov, base.Key(rel.FieldName()), depth)
		if children != nil {
			if composed == nil {
				composed = make(map[string][]ValidInstance)
			}
			composed[rel.Name()] = children
		}
	}
	return composed
}

// validateComposition validates a present composition value at relPath, whose
// children sit at depth+1, and returns them: nil when it fails, and non-nil but
// empty for an empty composition.
func (v *Validator) validateComposition(
	ctx context.Context,
	rel schema.Relation,
	rawValue any,
	collector *diag.Collector,
	prov Provenance,
	relPath path.Path,
	depth int,
) []ValidInstance {
	// Compositions always expect an array (accept typed slices via reflection)
	arr, ok := toSliceOfAny(rawValue)
	if !ok {
		collector.Collect(shapeMismatch(rel, fmt.Sprintf("composition %q: expected array, got %s", rel.Name(), kindOf(rawValue)),
			"array", kindOf(rawValue), prov, relPath))
		return nil
	}

	// Empty array is valid for optional, error for required
	if len(arr) == 0 {
		if !rel.IsOptional() {
			issue := diag.NewIssue(
				diag.Error,
				diag.E_UNRESOLVED_REQUIRED_COMPOSITION,
				fmt.Sprintf("composition %q: required composition cannot be empty", rel.Name()),
			).WithDetail(diag.DetailKeyReason, "empty").
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyJSONField, rel.FieldName())
			withProvenance(issue, prov, relPath.String())
			collector.Collect(issue.Build())
			return nil
		}
		return []ValidInstance{}
	}

	// A composition always arrives as an array, so its multiplicity is not
	// settled by the shape the way a (one) association's object form is. A
	// slot given more children than it holds draws the code the .ys reader
	// raises for the same fact.
	if !rel.IsMany() && len(arr) > 1 {
		issue := diag.NewIssue(
			diag.Error,
			diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("composition %q: (one) cardinality violated, got %d children", rel.Name(), len(arr)),
		).WithExpectedGot("1 child", fmt.Sprintf("%d children", len(arr))).
			WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName())
		withProvenance(issue, prov, relPath.String())
		collector.Collect(issue.Build())
		return nil
	}

	childDepth := depth + 1
	if childDepth > MaxComposedDepth {
		issue := diag.NewIssue(
			diag.Error,
			diag.E_COMPOSITION_DEPTH_EXCEEDED,
			fmt.Sprintf("composition %q: composed nesting depth %d exceeds limit %d", rel.Name(), childDepth, MaxComposedDepth),
		).WithDetail(diag.DetailKeyDepth, strconv.Itoa(childDepth)).
			WithDetail(diag.DetailKeyRelationName, rel.Name()).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName())
		withProvenance(issue, prov, relPath.String())
		collector.Collect(issue.Build())
		return nil
	}

	// Build raw instances for the children, each remembering its position in
	// the caller's array so a later diagnostic names that position and not
	// its position among the survivors of the shape filter.
	childRaws := make([]RawInstance, 0, len(arr))
	childBases := make([]path.Path, 0, len(arr))
	inputIndex := make([]int, 0, len(arr))
	for i, elem := range arr {
		childObj, ok := toMapOfAny(elem)
		if !ok {
			collector.Collect(shapeMismatch(rel, "composition child must be an object, got "+kindOf(elem),
				"object", kindOf(elem), prov, relPath.Index(i)))
			continue
		}

		childBase := relPath.Index(i)
		var childProv Provenance
		if !prov.IsZero() {
			childProv = NewProvenance(prov.SourceName(), childBase, prov.Span())
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
	validChildren, childType := v.validateComposedBatch(ctx, rel, childRaws, childBases, childDepth, func(res diag.Result) {
		collector.MergeFunc(res, func(issue diag.Issue) diag.Issue {
			if hasDetailKey(issue, diag.DetailKeyRelationName) || hasDetailKey(issue, diag.DetailKeyJSONField) {
				return issue
			}
			return diag.FromIssue(issue).WithDetails(relationDetails...).Build()
		})
	})

	// Check for duplicate PKs among children - only for types that have PKs.
	// PK-less composed children use structural position (array index) for identity,
	// so no duplicate check is needed for them.
	if len(validChildren) > 0 {
		if childType.HasPrimaryKey() {
			seenPKs := make(map[string]int) // pk string -> first occurrence's input index
			for i, child := range validChildren {
				if child.IsZero() {
					continue // failed validation: nothing to compare
				}
				pkStr := child.PrimaryKey().String()
				if firstIdx, exists := seenPKs[pkStr]; exists {
					issue := diag.NewIssue(
						diag.Error,
						diag.E_DUPLICATE_COMPOSED_PK,
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
		return nil
	}
	return validChildren
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

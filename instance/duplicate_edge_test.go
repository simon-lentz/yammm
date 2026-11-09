package instance_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

const duplicateEdgeSchema = `schema "dupedge"

type Company {
	id UUID primary
}

type Person {
	id String primary
	--> EMPLOYERS (many) Company { since String }
}
`

const (
	companyUpper = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
	companyLower = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	otherCompany = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
)

func employers(targets ...map[string]any) instance.RawInstance {
	list := make([]any, len(targets))
	for i, target := range targets {
		list[i] = target
	}
	return instance.RawInstance{Properties: map[string]any{"id": "p1", "employers": list}}
}

// A (many) listing that names one target twice is refused at the repeat, one
// issue per repeat, whether the two listings agree, differ in their edge
// properties, or spell the key two ways the target's key type stores as one.
func TestValidateOne_ARepeatedTargetKeyIsRefused(t *testing.T) {
	t.Parallel()
	s := mustLoadString(t, duplicateEdgeSchema, "dupedge.yammm")
	v := instance.NewValidator(s)
	wantDetails := append(diag.PathRelation("EMPLOYERS", "employers"),
		diag.Detail{Key: diag.DetailKeyTargetPK, Value: `["` + companyLower + `"]`})
	for name, c := range map[string]struct {
		raw      instance.RawInstance
		paths    []string
		messages []string
	}{
		"listed twice alike": {
			raw:      employers(map[string]any{"_target_id": companyLower}, map[string]any{"_target_id": otherCompany}, map[string]any{"_target_id": companyLower}),
			paths:    []string{"$.employers[2]"},
			messages: []string{"edge EMPLOYERS: target key " + diag.CutValue(`["`+companyLower+`"]`) + " listed at indices 0 and 2"},
		},
		"listed twice with other edge properties": {
			raw: employers(
				map[string]any{"_target_id": companyLower, "since": "2020"},
				map[string]any{"_target_id": companyLower, "since": "2021"},
			),
			paths: []string{"$.employers[1]"},
		},
		"listed under two spellings of one UUID": {
			raw:   employers(map[string]any{"_target_id": companyUpper}, map[string]any{"_target_id": companyLower}),
			paths: []string{"$.employers[1]"},
		},
		"listed three times": {
			raw: employers(
				map[string]any{"_target_id": companyLower},
				map[string]any{"_target_id": companyLower},
				map[string]any{"_target_id": companyUpper},
			),
			paths: []string{"$.employers[1]", "$.employers[2]"},
			messages: []string{
				"edge EMPLOYERS: target key " + diag.CutValue(`["`+companyLower+`"]`) + " listed at indices 0 and 1",
				"edge EMPLOYERS: target key " + diag.CutValue(`["`+companyLower+`"]`) + " listed at indices 0 and 2",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			valid, res := v.ValidateOne(t.Context(), "Person", c.raw)
			if !valid.IsZero() {
				t.Error("a refused listing yields an instance")
			}
			var paths, messages []string
			for issue := range res.Issues() {
				if issue.Code() != diag.E_DUPLICATE_EDGE || issue.Severity() != diag.Error {
					t.Fatalf("issue %s %s: %s; want Error E_DUPLICATE_EDGE alone", issue.Severity(), issue.Code(), issue.Message())
				}
				if !slices.Equal(issue.Details(), wantDetails) {
					t.Errorf("details %v, want %v", issue.Details(), wantDetails)
				}
				paths = append(paths, issue.Path())
				messages = append(messages, issue.Message())
			}
			if !slices.Equal(paths, c.paths) {
				t.Errorf("paths %q, want %q", paths, c.paths)
			}
			if c.messages != nil && !slices.Equal(messages, c.messages) {
				t.Errorf("messages %q, want %q", messages, c.messages)
			}
		})
	}
}

// Distinct targets, however many, are accepted in listing order.
func TestValidateOne_DistinctTargetsAreAccepted(t *testing.T) {
	t.Parallel()
	s := mustLoadString(t, duplicateEdgeSchema, "dupedge.yammm")
	valid, res := instance.NewValidator(s).ValidateOne(t.Context(), "Person",
		employers(map[string]any{"_target_id": otherCompany}, map[string]any{"_target_id": companyUpper}))
	if !res.OK() {
		t.Fatal(res)
	}
	edge, ok := valid.Edge("EMPLOYERS")
	if !ok {
		t.Fatal("no EMPLOYERS edge data")
	}
	var keys []string
	for target := range edge.Targets() {
		keys = append(keys, target.TargetKey().String())
	}
	if want := []string{`["` + otherCompany + `"]`, `["` + companyLower + `"]`}; !slices.Equal(keys, want) {
		t.Errorf("target keys %q, want %q", keys, want)
	}
}

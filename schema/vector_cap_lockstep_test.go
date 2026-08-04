package schema_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestVectorDimensionCaps_ArePinned holds this package's vector-dimension
// bounds and the text they report. The root internal/parse package carries its
// own copy of both, and while the two parsers ship side by side a cap raised in
// one alone makes the same schema valid under the CLI and invalid under the
// LSP. Its copy is pinned by TestChecks_ConstraintDiagnostics.
func TestVectorDimensionCaps_ArePinned(t *testing.T) {
	tests := []struct {
		name string
		dims string
		want string
	}{
		{name: "at the minimum", dims: "1"},
		{name: "at the maximum", dims: "65536"},
		{
			name: "below the minimum",
			dims: "0",
			want: "vector dimensions must be at least 1 (got 0)",
		},
		{
			name: "above the maximum",
			dims: "65537",
			want: "vector dimensions exceed maximum of 65536 (got 65537)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\ntype T {\n\tid String primary\n\tv Vector[%s]\n}\n", tc.dims)
			_, res := schema.LoadString(context.Background(), src, "v.yammm")

			var got []string
			for iss := range res.Issues() {
				if iss.Code() == diag.E_INVALID_CONSTRAINT {
					got = append(got, iss.Message())
				}
			}
			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("Vector[%s] reported %v, want no constraint diagnostic", tc.dims, got)
				}
				return
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("Vector[%s] reported %v, want exactly %q", tc.dims, got, tc.want)
			}
		})
	}
}

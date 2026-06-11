package graph_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
)

func TestHasDiagnostic_WithDiagnostic(t *testing.T) {
	issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK, "duplicate key").Build()
	d := &graph.Duplicate{
		Diagnostic: issue,
	}
	assert.True(t, d.HasDiagnostic())
}

func TestHasDiagnostic_ZeroValue(t *testing.T) {
	d := &graph.Duplicate{
		Diagnostic: diag.Issue{},
	}
	assert.False(t, d.HasDiagnostic())
}

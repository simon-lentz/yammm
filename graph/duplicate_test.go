package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/diag"
)

func TestHasDiagnostic_WithDiagnostic(t *testing.T) {
	issue := diag.NewIssue(diag.Error, diag.E_DUPLICATE_PK, "duplicate key").Build()
	d := &Duplicate{
		Diagnostic: issue,
	}
	assert.True(t, d.HasDiagnostic())
}

func TestHasDiagnostic_ZeroValue(t *testing.T) {
	d := &Duplicate{
		Diagnostic: diag.Issue{},
	}
	assert.False(t, d.HasDiagnostic())
}

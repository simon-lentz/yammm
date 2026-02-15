package instance

import (
	"github.com/simon-lentz/yammm/diag"
)

// ValidationFailure represents a data validation failure.
//
// ValidationFailure pairs the raw instance that failed validation with
// the diagnostic result explaining why validation failed. Multiple
// issues may be collected in a single failure.
type ValidationFailure struct {
	// Raw is the raw instance that failed validation.
	Raw RawInstance

	// Result contains the diagnostic issues explaining the failure.
	Result diag.Result
}

// NewValidationFailure creates a new ValidationFailure.
func NewValidationFailure(raw RawInstance, result diag.Result) ValidationFailure {
	return ValidationFailure{
		Raw:    raw,
		Result: result,
	}
}

// Error returns the first error message, or empty string if none.
func (f ValidationFailure) Error() string {
	for issue := range f.Result.Issues() {
		if issue.Severity().IsFailure() {
			return issue.Message()
		}
	}
	return ""
}

// HasErrors returns true if there are any error-level issues.
func (f ValidationFailure) HasErrors() bool {
	return !f.Result.OK()
}

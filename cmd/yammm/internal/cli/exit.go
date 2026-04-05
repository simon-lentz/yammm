package cli

import "github.com/simon-lentz/yammm/diag"

// Exit codes for the yammm CLI.
const (
	ExitOK         = 0 // Success (possibly with warnings)
	ExitValidation = 1 // Validation errors found
	ExitUsage      = 2 // Usage error (bad flags, missing arguments)
	ExitRuntime    = 3 // Runtime error (connection failure, I/O error)
)

// ExitForResult returns ExitValidation if the result contains errors,
// otherwise ExitOK.
func ExitForResult(result diag.Result) int {
	if result.HasErrors() {
		return ExitValidation
	}
	return ExitOK
}

// ExitError is a sentinel error type used by cobra RunE functions to signal
// a specific exit code. The run() function in main.go type-asserts this
// to determine the process exit code.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	switch e.Code {
	case ExitValidation:
		return "validation errors found"
	case ExitUsage:
		return "usage error"
	case ExitRuntime:
		return "runtime error"
	default:
		return "exit"
	}
}

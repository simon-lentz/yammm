package diag

// Severity represents the severity level of a diagnostic issue.
//
// Severity is an ordered enumeration where lower numeric values are more severe.
// The zero Severity names no severity. Only the zero [Issue] carries it, and a
// [Collector] refuses that Issue.
type Severity uint8

const (
	// Fatal indicates an unrecoverable condition. Fatal issues typically halt
	// further processing. Reaching a collection limit marks no severity; see
	// [Result.LimitReached].
	Fatal Severity = iota + 1

	// Error indicates a validation failure where collection can continue.
	// Errors cause the overall result to be unsuccessful.
	Error

	// Warning indicates a condition that should be corrected but the target
	// is still usable.
	Warning

	// Info provides informational diagnostics that require no correction.
	Info

	// Hint provides suggestions for improvement.
	Hint
)

// String returns the canonical lowercase label for the severity.
//
// These values are used by FormatResultJSON and are part of
// the wire format stability guarantee. The returned strings are:
// "fatal", "error", "warning", "info", "hint".
func (s Severity) String() string {
	//exhaustive:enforce
	switch s {
	case Fatal:
		return "fatal"
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Info:
		return "info"
	case Hint:
		return "hint"
	default:
		return "unknown"
	}
}

// isFailure reports whether s is Fatal or Error, the condition !Result.OK()
// checks.
func (s Severity) isFailure() bool {
	return s == Fatal || s == Error
}

// isMoreSevereThan reports whether s is more severe than other; a lower value is
// more severe.
func (s Severity) isMoreSevereThan(other Severity) bool {
	return s < other
}

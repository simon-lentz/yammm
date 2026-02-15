package diag

// Severity represents the severity level of a diagnostic issue.
//
// Severity is an ordered enumeration where lower numeric values are more severe.
// Use the comparison methods rather than raw numeric comparisons for clarity.
type Severity uint8

const (
	// Fatal indicates an unrecoverable condition or collection limit reached.
	// Fatal issues typically halt further processing.
	Fatal Severity = iota

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
// These values are used by FormatIssueJSON/FormatResultJSON and are part of
// the wire format stability guarantee. The returned strings are:
// "fatal", "error", "warning", "info", "hint".
func (s Severity) String() string {
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

// IsFailure reports whether the severity indicates a failure.
//
// Returns true for Fatal and Error severities. This matches the condition
// checked by !Result.OK().
func (s Severity) IsFailure() bool {
	return s <= Error
}

// IsMoreSevereThan reports whether s is more severe than other.
//
// Since lower numeric values are more severe, this returns s < other.
// Use this method instead of raw numeric comparisons for clarity.
func (s Severity) IsMoreSevereThan(other Severity) bool {
	return s < other
}

// IsAtLeastAsSevereAs reports whether s is at least as severe as other.
//
// Returns true when s is equal to or more severe than other.
func (s Severity) IsAtLeastAsSevereAs(other Severity) bool {
	return s <= other
}

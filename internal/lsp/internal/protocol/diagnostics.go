package protocol

// DiagnosticSeverity represents the severity of a diagnostic.
type DiagnosticSeverity = Integer

const (
	DiagnosticSeverityError       DiagnosticSeverity = 1
	DiagnosticSeverityWarning     DiagnosticSeverity = 2
	DiagnosticSeverityInformation DiagnosticSeverity = 3
	DiagnosticSeverityHint        DiagnosticSeverity = 4
)

// Diagnostic represents a diagnostic, such as a compiler error or warning.
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           *DiagnosticSeverity            `json:"severity,omitempty"`
	Code               *IntegerOrString               `json:"code,omitempty"`
	Source             *string                        `json:"source,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

// DiagnosticRelatedInformation represents a related message and source location.
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// PublishDiagnosticsParams are the parameters of the textDocument/publishDiagnostics notification.
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *Integer     `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ServerTextDocumentPublishDiagnostics is the method name for publishing diagnostics.
const ServerTextDocumentPublishDiagnostics = "textDocument/publishDiagnostics"

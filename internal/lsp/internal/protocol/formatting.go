package protocol

// DocumentFormattingParams are the parameters of a textDocument/formatting request.
type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Options      FormattingOptions      `json:"options"`
}

// FormattingOptions is a value-object describing what options formatting should use.
type FormattingOptions map[string]any

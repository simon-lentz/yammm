package protocol

// HoverParams are the parameters of a textDocument/hover request.
type HoverParams struct {
	TextDocumentPositionParams
}

// Hover is the result of a hover request.
// Contents is typed as MarkupContent since yammm-lsp only uses that form.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// MarkupContent represents a string value with a specific content type.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}

// MarkupKind describes the content type of a MarkupContent literal.
type MarkupKind = string

const (
	MarkupKindPlainText MarkupKind = "plaintext"
	MarkupKindMarkdown  MarkupKind = "markdown"
)

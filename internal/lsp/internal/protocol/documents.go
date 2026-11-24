package protocol

// TextDocumentIdentifier identifies a text document using a URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier identifies a specific version of a text document.
type VersionedTextDocumentIdentifier struct {
	TextDocumentIdentifier
	Version Integer `json:"version"`
}

// TextDocumentItem is an item to transfer a text document from the client to the server.
type TextDocumentItem struct {
	URI        string  `json:"uri"`
	LanguageID string  `json:"languageId"`
	Version    Integer `json:"version"`
	Text       string  `json:"text"`
}

// TextDocumentPositionParams is a parameter literal used in requests to pass a
// text document and a position inside that document.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

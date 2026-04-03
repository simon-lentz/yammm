package protocol

// TextDocumentSyncKind defines how the host (editor) should sync document changes.
type TextDocumentSyncKind = Integer

const (
	TextDocumentSyncKindNone        TextDocumentSyncKind = 0
	TextDocumentSyncKindFull        TextDocumentSyncKind = 1
	TextDocumentSyncKindIncremental TextDocumentSyncKind = 2
)

// TextDocumentSyncOptions describes text document sync capabilities.
type TextDocumentSyncOptions struct {
	OpenClose *bool                 `json:"openClose,omitempty"`
	Change    *TextDocumentSyncKind `json:"change,omitempty"`
}

// DidOpenTextDocumentParams are the parameters of a textDocument/didOpen notification.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// ContentChange represents a single content change event in a didChange notification.
// For full-sync (Range == nil), Text contains the entire document.
// For incremental sync (Range != nil), Text replaces the specified range.
type ContentChange struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

// IsFullSync returns true if this is a full-document replacement (no range specified).
func (c ContentChange) IsFullSync() bool {
	return c.Range == nil
}

// DidChangeTextDocumentParams are the parameters of a textDocument/didChange notification.
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []ContentChange                 `json:"contentChanges"`
}

// Note: no custom UnmarshalJSON needed — the standard decoder handles
// []ContentChange correctly, including nullable Range fields via *Range.

// ExtractFullSyncText returns the text of the last full-document replacement
// in a content changes slice. Returns ("", false) if no full-sync change exists.
func ExtractFullSyncText(changes []ContentChange) (string, bool) {
	// Walk backward to find the last full-sync entry directly.
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].IsFullSync() {
			return changes[i].Text, true
		}
	}
	return "", false
}

// DidCloseTextDocumentParams are the parameters of a textDocument/didClose notification.
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

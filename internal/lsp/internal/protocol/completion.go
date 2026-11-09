package protocol

// CompletionParams are the parameters of a textDocument/completion request.
type CompletionParams struct {
	TextDocumentPositionParams
}

// CompletionOptions specify the server capability for completions.
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// CompletionItem represents a completion proposal.
type CompletionItem struct {
	Label            string              `json:"label"`
	Kind             *CompletionItemKind `json:"kind,omitempty"`
	Detail           *string             `json:"detail,omitempty"`
	InsertText       *string             `json:"insertText,omitempty"`
	InsertTextFormat *InsertTextFormat   `json:"insertTextFormat,omitempty"`
	SortText         *string             `json:"sortText,omitempty"`
}

// CompletionItemKind defines the kind of a completion item.
type CompletionItemKind = Integer

const (
	CompletionItemKindText          CompletionItemKind = 1
	CompletionItemKindMethod        CompletionItemKind = 2
	CompletionItemKindFunction      CompletionItemKind = 3
	CompletionItemKindConstructor   CompletionItemKind = 4
	CompletionItemKindField         CompletionItemKind = 5
	CompletionItemKindVariable      CompletionItemKind = 6
	CompletionItemKindClass         CompletionItemKind = 7
	CompletionItemKindInterface     CompletionItemKind = 8
	CompletionItemKindModule        CompletionItemKind = 9
	CompletionItemKindProperty      CompletionItemKind = 10
	CompletionItemKindUnit          CompletionItemKind = 11
	CompletionItemKindValue         CompletionItemKind = 12
	CompletionItemKindEnum          CompletionItemKind = 13
	CompletionItemKindKeyword       CompletionItemKind = 14
	CompletionItemKindSnippet       CompletionItemKind = 15
	CompletionItemKindTypeParameter CompletionItemKind = 25
)

// InsertTextFormat defines whether the insert text is plain text or a snippet.
type InsertTextFormat = Integer

const (
	InsertTextFormatPlainText InsertTextFormat = 1
	InsertTextFormatSnippet   InsertTextFormat = 2
)

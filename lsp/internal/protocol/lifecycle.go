package protocol

// InitializeParams are the parameters of the initialize request.
type InitializeParams struct {
	ProcessID             *Integer           `json:"processId"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	RootPath              *string            `json:"rootPath,omitempty"`
	RootURI               *string            `json:"rootUri"`
	InitializationOptions any                `json:"initializationOptions,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	Trace                 *TraceValue        `json:"trace,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
}

// ClientInfo describes the client.
type ClientInfo struct {
	Name    string  `json:"name"`
	Version *string `json:"version,omitempty"`
}

// InitializeResult is the result of the initialize request.
type InitializeResult struct {
	Capabilities ServerCapabilities          `json:"capabilities"`
	ServerInfo   *InitializeResultServerInfo `json:"serverInfo,omitempty"`
}

// InitializeResultServerInfo describes the server.
type InitializeResultServerInfo struct {
	Name    string  `json:"name"`
	Version *string `json:"version,omitempty"`
}

// InitializedParams are the parameters of the initialized notification.
type InitializedParams struct{}

// SetTraceParams are the parameters of the $/setTrace notification.
type SetTraceParams struct {
	Value TraceValue `json:"value"`
}

// CancelParams are the parameters of the $/cancelRequest notification.
type CancelParams struct {
	ID IntegerOrString `json:"id"`
}

// ClientCapabilities define capabilities provided by the client.
type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

// TextDocumentClientCapabilities define capabilities the client provides on
// text documents.
type TextDocumentClientCapabilities struct {
	Synchronization *TextDocumentSyncClientCapabilities   `json:"synchronization,omitempty"`
	Hover           *HoverClientCapabilities              `json:"hover,omitempty"`
	Completion      *CompletionClientCapabilities         `json:"completion,omitempty"`
	Definition      *DefinitionClientCapabilities         `json:"definition,omitempty"`
	DocumentSymbol  *DocumentSymbolClientCapabilities     `json:"documentSymbol,omitempty"`
	Formatting      *DocumentFormattingClientCapabilities `json:"formatting,omitempty"`
}

// TextDocumentSyncClientCapabilities define text document sync capabilities.
type TextDocumentSyncClientCapabilities struct{}

// HoverClientCapabilities define hover capabilities.
type HoverClientCapabilities struct {
	ContentFormat []MarkupKind `json:"contentFormat,omitempty"`
}

// CompletionClientCapabilities define completion capabilities.
type CompletionClientCapabilities struct {
	CompletionItem *CompletionClientCapabilitiesItem `json:"completionItem,omitempty"`
}

// CompletionClientCapabilitiesItem describes completion item capabilities.
type CompletionClientCapabilitiesItem struct {
	SnippetSupport *bool `json:"snippetSupport,omitempty"`
}

// DefinitionClientCapabilities define definition capabilities.
type DefinitionClientCapabilities struct{}

// DocumentSymbolClientCapabilities define document symbol capabilities.
type DocumentSymbolClientCapabilities struct{}

// DocumentFormattingClientCapabilities define formatting capabilities.
type DocumentFormattingClientCapabilities struct{}

// ServerCapabilities define capabilities provided by the server.
type ServerCapabilities struct {
	TextDocumentSync           any                          `json:"textDocumentSync,omitempty"`
	CompletionProvider         *CompletionOptions           `json:"completionProvider,omitempty"`
	HoverProvider              any                          `json:"hoverProvider,omitempty"`
	DefinitionProvider         any                          `json:"definitionProvider,omitempty"`
	DocumentSymbolProvider     any                          `json:"documentSymbolProvider,omitempty"`
	DocumentFormattingProvider any                          `json:"documentFormattingProvider,omitempty"`
	Workspace                  *ServerCapabilitiesWorkspace `json:"workspace,omitempty"`
}

// ServerCapabilitiesWorkspace describes workspace-specific server capabilities.
type ServerCapabilitiesWorkspace struct {
	WorkspaceFolders *WorkspaceFoldersServerCapabilities `json:"workspaceFolders,omitempty"`
}

// WorkspaceFoldersServerCapabilities describes workspace folder capabilities.
type WorkspaceFoldersServerCapabilities struct {
	Supported           *bool         `json:"supported,omitempty"`
	ChangeNotifications *BoolOrString `json:"changeNotifications,omitempty"`
}

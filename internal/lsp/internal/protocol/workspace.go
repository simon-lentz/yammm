package protocol

// WorkspaceFolder represents a workspace folder.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// DidChangeWatchedFilesParams are the parameters of a workspace/didChangeWatchedFiles notification.
type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

// FileEvent describes a file change event.
type FileEvent struct {
	URI  string   `json:"uri"`
	Type UInteger `json:"type"`
}

// File change type constants.
const (
	FileChangeTypeCreated UInteger = 1
	FileChangeTypeChanged UInteger = 2
	FileChangeTypeDeleted UInteger = 3
)

// DidChangeWorkspaceFoldersParams are the parameters of a workspace/didChangeWorkspaceFolders notification.
type DidChangeWorkspaceFoldersParams struct {
	Event WorkspaceFoldersChangeEvent `json:"event"`
}

// WorkspaceFoldersChangeEvent describes workspace folder change events.
type WorkspaceFoldersChangeEvent struct {
	Added   []WorkspaceFolder `json:"added"`
	Removed []WorkspaceFolder `json:"removed"`
}

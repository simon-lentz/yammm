// Package lsperr defines sentinel errors for internal classification
// within the yammm-lsp server. They wrap an error the server logs, so the log
// record names its class.
//
// They are NOT for "document not open" or "stale snapshot" — those remain
// nil, nil returns per LSP convention (returning an error changes the
// JSON-RPC response from {"result": null} to {"error": {...}}).
package lsperr

import "errors"

// ErrInvalidURI indicates a document URI that cannot be parsed.
var ErrInvalidURI = errors.New("invalid document URI")

package lsp

import (
	"github.com/creachadair/jrpc2/handler"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"
)

// Mux returns the handler map.
func (s *Server) Mux() handler.Map { return s.mux }

// Workspace returns the workspace.
func (s *Server) Workspace() *workspace.Workspace { return s.workspace }

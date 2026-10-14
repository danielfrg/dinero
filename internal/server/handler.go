package server

import (
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	"net/http"
)

// NewHandler composes REST and an optional MCP transport under one policy.
func NewHandler(service *ledger.Service, opts httpapi.Options, mcp http.Handler) http.Handler {
	opts.MCPHandler = mcp
	return httpapi.New(service, opts)
}

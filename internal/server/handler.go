package server

import (
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	"net/http"
)

// NewHandler composes REST and an optional unprotected MCP transport under
// one bearer, Host, and Origin policy.
func NewHandler(service *ledger.Service, opts httpapi.Options, mcpHandler http.Handler) http.Handler {
	opts.MCPHandler = mcpHandler
	return httpapi.New(service, opts)
}

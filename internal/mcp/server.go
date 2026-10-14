// Package mcp exposes the read-only ledger service through the official MCP SDK.
package mcp

import (
	"net/http"

	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options is the same bearer, Host, and Origin policy used by the REST API.
type Options = httpapi.Options

// NewHandler serves stateless Streamable HTTP after the shared HTTP policy.
func NewHandler(service *ledger.Service, opts Options) http.Handler {
	return httpapi.Protect(NewTransportHandler(service), opts)
}

// NewTransportHandler supplies the SDK transport for composition under an
// existing httpapi.Protect layer, as server.NewHandler does.
func NewTransportHandler(service *ledger.Service) http.Handler {
	s := sdk.NewServer(&sdk.Implementation{Name: "dinero", Version: "0.1.0"}, nil)
	registerTools(s, service)
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

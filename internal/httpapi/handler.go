package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func New(service *ledger.Service, opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Dinero</title></head><body>Dinero API. REST docs: <a href=\"/api/docs\">/api/docs</a>; MCP: <a href=\"/mcp\">/mcp</a>.</body></html>\n"))
	})
	cfg := huma.DefaultConfig("Dinero API", "1.0.0")
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil
	api := humago.New(mux, cfg)
	mux.HandleFunc("GET /api/openapi", func(w http.ResponseWriter, r *http.Request) {
		copy := r.Clone(r.Context())
		copy.URL.Path = "/api/openapi.json"
		mux.ServeHTTP(w, copy)
	})
	registerTransactions(api, service)
	registerAccounts(api, service)
	registerBalance(api, service)
	registerSummary(api, service)
	registerStatus(api, service)
	if opts.MCPHandler != nil {
		mux.Handle("/mcp", opts.MCPHandler)
	}
	protected := Protect(mux, opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			start := time.Now()
			id := strconv.FormatUint(requestSequence.Add(1), 10)
			rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				log.Printf("request_id=%s method=%s path=%s status=%d duration=%s", id, r.Method, r.URL.Path, rw.status, time.Since(start).Round(time.Microsecond))
			}()
			w = rw
		}
		switch r.URL.Path {
		case "/healthz":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("ok\n"))
		case "/readyz":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if _, err := service.Accounts(ctx); err != nil {
				http.Error(w, "Unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("ready\n"))
		default:
			protected.ServeHTTP(w, r)
		}
	})
}
func apiError(err error) error {
	if err == nil {
		return nil
	}
	var invalid *ledger.ErrInvalidInput
	if errors.As(err, &invalid) {
		return huma.Error422UnprocessableEntity("Invalid query input")
	}
	var unknown *ledger.ErrUnknownAccount
	if errors.As(err, &unknown) {
		return huma.Error404NotFound("Unknown account")
	}
	return huma.Error503ServiceUnavailable("Data source unavailable")
}

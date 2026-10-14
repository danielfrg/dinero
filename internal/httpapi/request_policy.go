package httpapi

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Options struct {
	AllowedHosts   []string
	AllowedOrigins []string
	MCPHandler     http.Handler
}

var requestSequence atomic.Uint64

// Protect applies the shared Host and browser-origin policy to REST and MCP.
func Protect(next http.Handler, opts Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := strconv.FormatUint(requestSequence.Add(1), 10)
		status := 0
		defer func() {
			log.Printf("request_id=%s method=%s path=%s status=%d duration=%s", id, r.Method, logPath(r.URL.Path), status, time.Since(start).Round(time.Microsecond))
		}()
		if !allowedHost(r.Host, opts.AllowedHosts) {
			status = http.StatusForbidden
			http.Error(w, "Forbidden", status)
			return
		}
		if !allowedOrigin(r.Header.Get("Origin"), r.Host, opts.AllowedOrigins) {
			status = http.StatusForbidden
			http.Error(w, "Forbidden", status)
			return
		}
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		status = rw.status
	})
}

func logPath(path string) string {
	switch path {
	case "/", "/api/accounts", "/api/categories", "/api/transactions", "/api/balance", "/api/summary", "/api/status", "/api/openapi", "/api/openapi.json", "/api/openapi.yaml", "/api/openapi-3.0.json", "/api/openapi-3.0.yaml", "/api/docs", "/mcp", "/healthz", "/readyz":
		return path
	default:
		return "<unmatched>"
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) WriteHeader(code int)        { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Write(p []byte) (int, error) { return w.ResponseWriter.Write(p) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func allowedHost(value string, allowed []string) bool {
	host := value
	if h, _, err := net.SplitHostPort(value); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(host, strings.Trim(candidate, "[]")) {
			return true
		}
	}
	return false
}
func allowedOrigin(value, host string, allowed []string) bool {
	if value == "" {
		return true
	}
	origin, err := url.Parse(value)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return false
	}
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return strings.EqualFold(origin.Host, host) && allowedHost(host, nil)
}

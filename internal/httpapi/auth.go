package httpapi

import (
	"crypto/subtle"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Options struct {
	Token          []byte
	AllowedHosts   []string
	AllowedOrigins []string
	MCPHandler     http.Handler
}

func ReadTokenFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("DINERO_API_TOKEN_FILE is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("could not read API token file")
	}
	token := []byte(strings.TrimSpace(string(b)))
	if len(token) < 16 {
		return nil, errors.New("API token file must contain at least 16 non-whitespace bytes")
	}
	return token, nil
}

var requestSequence atomic.Uint64

// Protect applies the shared authorization and browser-origin policy to REST and MCP.
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
		if len(opts.Token) == 0 || !validBearer(r.Header.Get("Authorization"), opts.Token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			status = http.StatusUnauthorized
			http.Error(w, "Unauthorized", status)
			return
		}
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		status = rw.status
	})
}

func logPath(path string) string {
	switch path {
	case "/api/accounts", "/api/categories", "/api/transactions", "/api/balance", "/api/summary", "/api/status", "/api/openapi", "/api/openapi.json", "/api/openapi.yaml", "/api/openapi-3.0.json", "/api/openapi-3.0.yaml", "/api/docs", "/mcp", "/healthz", "/readyz":
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

func validBearer(header string, token []byte) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	supplied := []byte(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare(supplied, token) == 1
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

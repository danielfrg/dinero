package server_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/server"
)

func TestServeDrainsActiveRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; io.WriteString(w, "done") }), time.Second)
	}()
	reqErr := make(chan error, 1)
	go func() {
		res, e := http.Get("http://" + ln.Addr().String())
		if e != nil {
			reqErr <- e
			return
		}
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		if e == nil && string(b) != "done" {
			e = errors.New("response was not drained")
		}
		reqErr <- e
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	close(release)
	if e := <-reqErr; e != nil {
		t.Fatal(e)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestServeBoundsBlockedShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release }), 30*time.Millisecond)
	}()
	go http.Get("http://" + ln.Addr().String())
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("expected deadline error")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown exceeded bound")
	}
	close(release)
}
func TestServeReturnsListenerError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if e := server.Serve(context.Background(), ln, http.NotFoundHandler(), time.Second); e == nil {
		t.Fatal("listener error suppressed")
	}
}

func TestComposedMCPHandlerUsesSharedPolicy(t *testing.T) {
	h := server.NewHandler(nil, httpapi.Options{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRequest(method, "/mcp", nil)
		r.Host = "localhost"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatalf("%s unauthenticated: %d", method, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	r.Host = "localhost"
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("untrusted origin: %d", w.Code)
	}
	r.Header.Del("Origin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("MCP without credentials: %d", w.Code)
	}
}

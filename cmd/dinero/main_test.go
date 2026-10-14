package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/testutil"
)

func TestEntrypointSIGTERMDrainsActiveRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signal regression")
	}
	fx := testutil.NewPostgres(t)
	fx.Insert(t, "2026-03-08 12:00:00", "synthetic", 1, "Checking")
	dir := t.TempDir()
	binary := filepath.Join(dir, "dinero")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build entrypoint: %v %s", err, output)
	}
	token := filepath.Join(dir, "api-token")
	if err := os.WriteFile(token, []byte("synthetic-read-token-123456"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("timezone='UTC'\n[database]\nconnection_string='"+fx.DSN+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--config", cfg, "serve", "--port", port)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DINERO_DATABASE_URL=") && !strings.HasPrefix(entry, "DINERO_DATABASE_URL_FILE=") && !strings.HasPrefix(entry, "DINERO_CONFIG_FILE=") && !strings.HasPrefix(entry, "DINERO_API_TOKEN_FILE=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "DINERO_CONFIG_FILE="+cfg, "DINERO_DATABASE_URL="+fx.DSN, "DINERO_API_TOKEN_FILE="+token)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			select {
			case <-exited:
			case <-time.After(time.Second):
			}
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second}
	request := func() (*http.Response, error) {
		r, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/api/accounts", nil)
		r.Header.Set("Authorization", "Bearer synthetic-read-token-123456")
		return client.Do(r)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, e := request()
		if e == nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			if r.StatusCode != 200 {
				t.Fatal("startup response", r.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("entrypoint never listened", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Force a real request to wait inside its SQL read. Observe the lock wait,
	// send the deployment stop signal, then release the request for graceful drain.
	tx, err := fx.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("LOCK TABLE public.transactions IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	type result struct {
		status int
		err    error
	}
	response := make(chan result, 1)
	go func() {
		r, e := request()
		out := result{err: e}
		if e == nil {
			out.status = r.StatusCode
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		response <- out
	}()
	deadline = time.Now().Add(2 * time.Second)
	for {
		var waiting int
		err := fx.DB.QueryRow("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT account,%'").Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("active request never blocked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// A graceful server must retain its active request until the database unlocks.
	select {
	case got := <-response:
		t.Fatalf("SIGTERM discarded active request: %+v", got)
	case err := <-exited:
		waited = true
		t.Fatalf("entrypoint terminated before drain: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-response:
		if got.err != nil || got.status != 200 {
			t.Fatalf("active request failed after SIGTERM: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not drain")
	}
	select {
	case err := <-exited:
		waited = true
		if err != nil {
			t.Fatalf("entrypoint exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("entrypoint did not finish shutdown")
	}
}

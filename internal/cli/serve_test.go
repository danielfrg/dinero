package cli_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/cli"
	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestServeIgnoresObsoleteTokenConfiguration(t *testing.T) {
	t.Setenv("DINERO_API_TOKEN_FILE", filepath.Join(t.TempDir(), "missing-token"))
	loaded := false
	root := cli.NewRoot(cli.Dependencies{LoadConfig: func(string) (config.Config, error) {
		loaded = true
		return config.Config{}, errors.New("synthetic config failure")
	}})
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs([]string{"serve"})
	if err := root.ExecuteContext(context.Background()); !loaded || err == nil || err.Error() != "could not load configuration" {
		t.Fatalf("obsolete token configuration blocked server setup: loaded=%t err=%v", loaded, err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout=%s", out.String())
	}
}
func TestServeBindsConfiguredAddressAndStopsOnCancellation(t *testing.T) {
	fx := testutil.NewSQLite(t)
	t.Setenv("DINERO_API_TOKEN_FILE", "")
	t.Setenv("DINERO_DATABASE_URL", "sqlite:///"+fx.DSN)
	if old, present := os.LookupEnv("DINERO_DATABASE_URL_FILE"); present {
		if err := os.Unsetenv("DINERO_DATABASE_URL_FILE"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Setenv("DINERO_DATABASE_URL_FILE", old) })
	}
	address := make(chan string, 1)
	actual := make(chan string, 1)
	deps := cli.Dependencies{LoadConfig: func(string) (config.Config, error) {
		return config.Config{Timezone: "US/Central", Database: config.Database{ConnectionString: "sqlite:///" + fx.DSN}, Server: config.Server{Host: "127.0.0.1", Port: 8888, AllowedHosts: []string{"config.example.test"}, AllowedOrigins: []string{"https://config.example.test"}}}, nil
	}, Listen: func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, "127.0.0.1:0")
		if err == nil {
			address <- addr
			actual <- ln.Addr().String()
		}
		return ln, err
	}}
	root := cli.NewRoot(deps)
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs([]string{"serve", "--port", "9999", "--allowed-host", "api.example.test,dinero-api", "--allowed-host", "internal.example.test", "--allowed-origin", "https://api.example.test"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	var wanted, bound string
	select {
	case wanted = <-address:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not listen")
	}
	bound = <-actual
	req, err := http.NewRequest("GET", "http://"+bound+"/api/accounts", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("status=%d", response.StatusCode)
	}
	for _, tc := range []struct {
		host, origin string
		want         int
	}{
		{"api.example.test", "https://api.example.test", http.StatusOK},
		{"dinero-api", "", http.StatusOK},
		{"internal.example.test", "", http.StatusOK},
		{"config.example.test", "https://config.example.test", http.StatusOK},
		{"attacker.example.test", "", http.StatusForbidden},
		{"api.example.test", "https://attacker.example.test", http.StatusForbidden},
	} {
		req, err := http.NewRequest(http.MethodGet, "http://"+bound+"/api/accounts", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.want {
			t.Errorf("host=%s origin=%s status=%d, want %d", tc.host, tc.origin, response.StatusCode, tc.want)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if wanted != "127.0.0.1:9999" {
		t.Fatalf("address=%s", wanted)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout=%s", out.String())
	}
	if want := "Dinero API listening at http://" + bound + "\n"; errs.String() != want {
		t.Fatalf("startup link=%q, want %q", errs.String(), want)
	}
}

func TestServeRejectsExplicitZeroPortBeforeDatabaseOrListen(t *testing.T) {
	root := cli.NewRoot(cli.Dependencies{LoadConfig: func(string) (config.Config, error) { return config.Config{}, nil }, Listen: func(string, string) (net.Listener, error) { t.Fatal("zero port listened"); return nil, nil }})
	root.SetArgs([]string{"serve", "--port", "0"})
	err := root.ExecuteContext(context.Background())
	if err == nil || err.Error() != "port must be 1 through 65535" {
		t.Fatalf("explicit port zero: %v", err)
	}
}

func TestServeDoesNotAnnounceFailedListen(t *testing.T) {
	fx := testutil.NewSQLite(t)
	queryConfig(t, fx.DSN)
	root := cli.NewRoot(cli.Dependencies{Listen: func(string, string) (net.Listener, error) {
		return nil, errors.New("synthetic bind failure")
	}})
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs([]string{"serve"})
	if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "could not listen on") {
		t.Fatalf("listen failure: %v", err)
	}
	if strings.Contains(out.String()+stderr.String(), "listening at") {
		t.Fatal("failed bind advertised a working server URL")
	}
}

package cli_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/cli"
	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/testutil"
)

func TestServeRequiresSeparateReadToken(t *testing.T) {
	t.Setenv("DINERO_API_TOKEN_FILE", "")
	root := cli.NewRoot(cli.Dependencies{LoadConfig: func(string) (config.Config, error) {
		t.Fatal("config loaded before token checked")
		return config.Config{}, nil
	}})
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs([]string{"serve"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("serve accepted empty token file")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout=%s", out.String())
	}
}
func TestServeBindsConfiguredAddressAndStopsOnCancellation(t *testing.T) {
	fx := testutil.NewSQLite(t)
	tokenPath := filepath.Join(t.TempDir(), "api-token")
	if err := os.WriteFile(tokenPath, []byte("synthetic-read-token-123456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DINERO_API_TOKEN_FILE", tokenPath)
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
		return config.Config{Timezone: "US/Central", Database: config.Database{ConnectionString: "sqlite:///" + fx.DSN}, Server: config.Server{Host: "127.0.0.1", Port: 8888}}, nil
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
	root.SetArgs([]string{"serve", "--port", "9999"})
	ctx, cancel := context.WithCancel(context.Background())
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
	req.Header.Set("Authorization", "Bearer synthetic-read-token-123456")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("status=%d", response.StatusCode)
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
}

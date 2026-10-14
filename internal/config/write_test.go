package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielfrg/dinero/internal/config"
)

func TestSaveLinkPreservesUnrelatedValuesAndUsesMode0600(t *testing.T) {
	path := fixture(t)
	unlock, err := config.Lock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	result := config.LinkCredentials{Alias: "new_bank", Token: "sentinel-new-token", ItemID: "item-new", Accounts: map[string]string{"account_new": "Savings"}}
	if err := config.SaveLink(context.Background(), path, result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"keep-me", "keep-this-too", "sentinel-access-token", "sentinel-new-token", "item-new", "Savings"} {
		if !strings.Contains(string(data), value) {
			t.Errorf("missing retained value %q", value)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %04o", info.Mode().Perm())
	}
	for _, secret := range []string{"sentinel-new-token", "sentinel-access-token"} {
		if strings.Contains(result.String(), secret) {
			t.Fatalf("result string leaked %q", secret)
		}
	}
}

func TestReadOnlyConfigRejectedBeforeWriting(t *testing.T) {
	path := fixture(t)
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if err := config.PreflightWritable(path); err == nil {
		t.Fatal("read-only config accepted")
	}
	err := config.SaveLink(context.Background(), path, config.LinkCredentials{Alias: "new", Token: "sentinel-new-token"})
	if err == nil || strings.Contains(err.Error(), "sentinel-new-token") {
		t.Fatalf("write error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("read-only config changed")
	}
}

func TestLockWaitRespectsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	unlock, err := config.Lock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = config.Lock(ctx, path)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock error = %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("lock cancellation too slow")
	}
}

func TestSaveLinkRejectsCredentialReplacement(t *testing.T) {
	for _, result := range []config.LinkCredentials{{Alias: "bank_a", Token: "replacement"}, {Alias: "bank_a", ItemID: "replacement-item"}} {
		path := fixture(t)
		// Establish a synthetic existing alias independently of fixture naming.
		if err := config.SaveLink(context.Background(), path, config.LinkCredentials{Alias: "bank_a", Token: "original", ItemID: "original-item"}); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		if err := config.SaveLink(context.Background(), path, result); err == nil {
			t.Fatal("existing credentials replaced")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("conflict changed configuration")
		}
	}
}

func TestPreflightReadOnlyFileMountWithWritableParent(t *testing.T) {
	path := os.Getenv("DINERO_TEST_READONLY_CONFIG")
	if path == "" {
		t.Skip("requires disposable read-only file bind mount")
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 == 0 {
		t.Fatal("mount fixture must retain writable file mode bits")
	}
	probe, e := os.CreateTemp(filepath.Dir(path), "writable-parent-*")
	if e != nil {
		t.Fatal("mount parent must remain writable")
	}
	probe.Close()
	defer os.Remove(probe.Name())
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = config.PreflightWritable(path); e == nil {
		t.Fatal("read-only file mount passed preflight")
	}
	after, e := os.ReadFile(path)
	if e != nil || string(before) != string(after) {
		t.Fatal("preflight mutated config")
	}
}

package plaid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielfrg/dinero/internal/config"
	"golang.org/x/sys/unix"
)

type RecoveryError struct{ Path string }

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("Link persistence failed; protected recovery: %s; run dinero link --resume PATH", e.Path)
}

type fileLinkStore struct{}

func (fileLinkStore) Lock(ctx context.Context, p string) (func(), error) { return config.Lock(ctx, p) }
func (fileLinkStore) Load(p string) (config.Config, error)               { return config.Load(p) }
func (fileLinkStore) Preflight(p string) error                           { return config.PreflightWritable(p) }
func (fileLinkStore) Save(ctx context.Context, p string, c config.LinkCredentials) error {
	return config.SaveLink(ctx, p, c)
}
func (fileLinkStore) PreflightRecovery(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0200 == 0 {
		return errors.New("recovery directory must have mode 0700")
	}
	if e = unix.Access(dir, unix.W_OK); e != nil {
		return e
	}
	probe, e := os.CreateTemp(dir, ".link-preflight-*")
	if e != nil {
		return e
	}
	path := probe.Name()
	defer os.Remove(path)
	if e = probe.Sync(); e != nil {
		probe.Close()
		return e
	}
	if e = probe.Close(); e != nil {
		return e
	}
	return syncDir(dir)
}

type recoveryRecord struct {
	Version  int               `json:"version"`
	Alias    string            `json:"alias"`
	Token    string            `json:"token"`
	ItemID   string            `json:"item_id"`
	Accounts map[string]string `json:"accounts,omitempty"`
}

func (fileLinkStore) Protect(dir string, c config.LinkCredentials) (string, error) {
	if c.Alias == "" || c.Token == "" || c.ItemID == "" {
		return "", errors.New("incomplete recovery credentials")
	}
	temp, e := os.CreateTemp(dir, ".link-recovery-*")
	if e != nil {
		return "", e
	}
	path := temp.Name()
	keep := false
	defer func() {
		temp.Close()
		if !keep {
			os.Remove(path)
		}
	}()
	if e = temp.Chmod(0600); e != nil {
		return "", e
	}
	if e = json.NewEncoder(temp).Encode(recoveryRecord{1, c.Alias, c.Token, c.ItemID, c.Accounts}); e != nil {
		return "", e
	}
	if e = temp.Sync(); e != nil {
		return "", e
	}
	if e = temp.Close(); e != nil {
		return "", e
	}
	target := path + ".json"
	if e = os.Rename(path, target); e != nil {
		return "", e
	}
	path = target
	if e = syncDir(dir); e != nil {
		return "", e
	}
	keep = true
	return target, nil
}
func syncDir(dir string) error {
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func protectFailure(store LinkStore, dir string, c config.LinkCredentials) error {
	path, e := store.Protect(dir, c)
	if e != nil || path == "" {
		return errors.New("Link persistence and protected recovery both failed")
	}
	return &RecoveryError{Path: path}
}

// ResumeLink accepts only a protected local file and performs no provider calls.
func ResumeLink(ctx context.Context, path, configPath string) (SafeLinkResult, error) {
	result := SafeLinkResult{Accounts: map[string]string{}}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return result, errors.New("cannot open local Link recovery file")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return result, errors.New("Link recovery file must be a regular mode 0600 file")
	}
	var stat unix.Stat_t
	if e = unix.Fstat(fd, &stat); e != nil || stat.Uid != uint32(os.Getuid()) {
		return result, errors.New("Link recovery file belongs to another user")
	}
	var record recoveryRecord
	decoder := json.NewDecoder(io.LimitReader(f, 1024*1024))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&record); e != nil || record.Version != 1 || record.Alias == "" || record.Token == "" || record.ItemID == "" {
		return result, errors.New("invalid Link recovery record")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, errors.New("invalid Link recovery record")
	}
	p := config.ResolvePath(configPath)
	unlock, e := config.Lock(ctx, p)
	if e != nil {
		return result, errors.New("cannot lock Link configuration")
	}
	defer unlock()
	cfg, e := config.Load(p)
	if e != nil {
		return result, errors.New("cannot load Link configuration")
	}
	if token, exists := cfg.Plaid.Tokens[record.Alias]; exists && (token != record.Token || cfg.Plaid.ItemIDs[record.Alias] != record.ItemID) {
		return result, errors.New("Link recovery conflicts with an existing alias")
	}
	c := config.LinkCredentials{Alias: record.Alias, Token: record.Token, ItemID: record.ItemID, Accounts: record.Accounts}
	if e = config.SaveLink(ctx, p, c); e != nil {
		return result, errors.New("Link recovery was not persisted; retain recovery file")
	}
	result = SafeLinkResult{Alias: record.Alias, ItemID: record.ItemID, Accounts: record.Accounts, Persisted: true}
	if result.Accounts == nil {
		result.Accounts = map[string]string{}
	}
	if e = os.Remove(path); e != nil {
		return result, errors.New("Link credentials saved; recovery file could not be removed")
	}
	if e = syncDir(filepath.Dir(path)); e != nil {
		return result, errors.New("Link credentials saved; recovery deletion could not be synced")
	}
	return result, nil
}

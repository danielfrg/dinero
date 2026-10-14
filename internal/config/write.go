package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

type LinkCredentials struct {
	Alias    string
	Token    string
	ItemID   string
	Accounts map[string]string
}

func (r LinkCredentials) String() string {
	return fmt.Sprintf("LinkCredentials{Alias:%q, Token:[REDACTED], ItemID:[REDACTED], Accounts:%d}", r.Alias, len(r.Accounts))
}
func (r LinkCredentials) GoString() string { return r.String() }

// PreflightWritable rejects read-only mounts before a provider operation.
// Both the config file and its parent must allow writes for atomic rename.
func PreflightWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0222 == 0 {
		return errors.New("configuration file is read-only")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode().Perm()&0222 == 0 {
		return errors.New("configuration directory is read-only")
	}
	if err := unix.Access(filepath.Dir(path), unix.W_OK); err != nil {
		return errors.New("configuration directory is not writable")
	}
	return nil
}

// Lock serializes updates through a stable sidecar; renaming config.toml
// cannot invalidate the lock. The caller must retain it through SaveLink.
func Lock(ctx context.Context, path string) (func(), error) {
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }) }, nil
}

// SaveLink assumes the caller holds Lock. It re-reads the config inside that
// lock so unrelated keys are retained, then fsyncs a 0600 temporary file and
// replaces the old file atomically.
func SaveLink(ctx context.Context, path string, result LinkCredentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := PreflightWritable(path); err != nil {
		return err
	}
	if result.Alias == "" {
		return errors.New("link alias is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tree map[string]any
	if err := toml.Unmarshal(data, &tree); err != nil {
		return errors.New("invalid configuration TOML")
	}
	if tree == nil {
		tree = make(map[string]any)
	}
	plaid := table(tree, "plaid")
	tokens := table(plaid, "tokens")
	if prior, exists := tokens[result.Alias]; exists && result.Token != "" && prior != result.Token {
		return errors.New("existing Link token cannot be replaced")
	}
	itemIDs := table(plaid, "item_ids")
	if prior, exists := itemIDs[result.Alias]; exists && result.ItemID != "" && prior != result.ItemID {
		return errors.New("existing Link Item cannot be replaced")
	}
	if result.Token != "" {
		tokens[result.Alias] = result.Token
	} else if _, exists := tokens[result.Alias]; !exists {
		return errors.New("link token is required for a new alias")
	}
	if result.ItemID != "" {
		itemIDs[result.Alias] = result.ItemID
	}
	accounts := table(plaid, "account_id_to_name")
	for id, name := range result.Accounts {
		if id == "" || name == "" {
			return errors.New("account ID and name are required")
		}
		if prior, exists := accounts[id]; exists && prior != name {
			return errors.New("existing account name cannot be changed by Link")
		}
		for existingID, existingName := range accounts {
			if existingID != id && existingName == name {
				return errors.New("account name is already assigned")
			}
		}
		accounts[id] = name
	}
	content, err := toml.Marshal(tree)
	if err != nil {
		return errors.New("cannot encode configuration")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".config.toml-recovery-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func table(parent map[string]any, name string) map[string]any {
	if current, ok := parent[name].(map[string]any); ok {
		return current
	}
	current := make(map[string]any)
	parent[name] = current
	return current
}

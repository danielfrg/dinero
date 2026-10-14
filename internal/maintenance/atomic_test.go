package maintenance

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type failedRulesReader struct{ read bool }

func (r *failedRulesReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, "partial rules"), nil
	}
	return 0, errors.New("forced synthetic write failure")
}
func TestAtomicRulesPreservePreviousFileOnPrepublicationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "category_rules.json")
	if err := os.WriteFile(path, []byte("previous rules"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicRules(context.Background(), path, &failedRulesReader{}); err == nil {
		t.Fatal("partial writer accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "previous rules" {
		t.Fatalf("previous rules changed %q %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary rules leaked", entries, err)
	}
	if err := atomicRules(context.Background(), path, io.LimitReader(&failedRulesReader{}, 7)); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "partial" {
		t.Fatal("complete replacement missing", string(data), err)
	}
}

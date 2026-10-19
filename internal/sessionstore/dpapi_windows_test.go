//go:build windows

package sessionstore

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestDPAPIPersistenceEncryptionAndDelete(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	store := &dpapiStore{}
	plain := []byte("test-telegram-session-secret")
	if err := store.Save(ctx, "test", plain); err != nil {
		t.Fatal(err)
	}
	path, _ := dpapiPath("test")
	//nolint:gosec // path is a fixed test profile inside a temporary directory.
	cipher, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, plain) {
		t.Fatal("session stored unencrypted")
	}
	restored := &dpapiStore{}
	data, err := restored.Load(ctx, "test")
	if err != nil || !bytes.Equal(data, plain) {
		t.Fatalf("load: %v", err)
	}
	if err = restored.Save(ctx, "test", []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	data, err = restored.Load(ctx, "test")
	if err != nil || string(data) != "replacement" {
		t.Fatal("atomic replacement failed", err)
	}
	if _, err = restored.Load(ctx, "other"); err != ErrNotFound {
		t.Fatal("profiles not isolated", err)
	}
	if err = restored.Delete(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Load(ctx, "test"); err != ErrNotFound {
		t.Fatal(err)
	}
}

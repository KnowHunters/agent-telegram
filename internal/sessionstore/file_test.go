//go:build !windows

package sessionstore

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gotd/td/session"
)

func TestFileProviderRoundTripAndPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := Open(FileProvider, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !storage.Persistent() {
		t.Fatal("file provider must be persistent")
	}
	if _, err := storage.LoadSession(context.Background()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("empty load error = %v", err)
	}
	if err := storage.StoreSession(context.Background(), []byte("session")); err != nil {
		t.Fatal(err)
	}
	path, _ := filePath("test")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode = %o", info.Mode().Perm())
	}
	if err := storage.StoreSession(context.Background(), []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	data, err := storage.LoadSession(context.Background())
	if err != nil || string(data) != "replacement" {
		t.Fatalf("loaded session = %q, %v", data, err)
	}
	if err := storage.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadSession(context.Background()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("session was not deleted")
	}
}

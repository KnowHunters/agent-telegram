//go:build !windows

package sessionstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"agent-telegram/internal/paths"
)

const FileProvider = "file"

func init() {
	RegisterProvider(FileProvider, func() (Store, error) { return &fileStore{}, nil })
}

// fileStore persists Telegram authorization for container volumes. The
// containing directory must be private because gotd's session data contains
// an auth key.
type fileStore struct{}

func (*fileStore) Provider() string { return FileProvider }
func (*fileStore) Persistent() bool { return true }

func filePath(profile string) (string, error) {
	profile, err := normalizeProfile(profile)
	if err != nil {
		return "", err
	}
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions", profile+".json"), nil
}

func (*fileStore) Load(ctx context.Context, profile string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filePath(profile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is fixed below the config directory.
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrNotFound
	}
	return data, nil
}

func (*fileStore) Save(ctx context.Context, profile string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("session is empty")
	}
	path, err := filePath(profile)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (*fileStore) Delete(ctx context.Context, profile string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := filePath(profile)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

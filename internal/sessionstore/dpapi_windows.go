//go:build windows

package sessionstore

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"unsafe"

	"agent-telegram/internal/paths"
	"golang.org/x/sys/windows"
)

const DPAPIProvider = "dpapi"

func platformDefaultProvider() string { return DPAPIProvider }
func init()                           { RegisterProvider(DPAPIProvider, func() (Store, error) { return &dpapiStore{}, nil }) }

type dpapiStore struct{}

func (*dpapiStore) Provider() string { return DPAPIProvider }
func (*dpapiStore) Persistent() bool { return true }
func dpapiPath(profile string) (string, error) {
	profile, err := normalizeProfile(profile)
	if err != nil {
		return "", err
	}
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions", profile+".dpapi"), nil
}
func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 || uint64(len(data)) > math.MaxUint32 {
		return nil, errors.New("empty session")
	}
	//nolint:gosec // len(data) is checked against math.MaxUint32 above.
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	//nolint:gosec // Release exactly the DPAPI-allocated buffer with its required allocator.
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) }()
	//nolint:gosec // DPAPI owns out.Data; copy exactly out.Size bytes before LocalFree.
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func (*dpapiStore) Load(_ context.Context, profile string) ([]byte, error) {
	path, err := dpapiPath(profile)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // path is derived from the user config directory and validated profile.
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return protect(b, true)
}
func (*dpapiStore) Save(_ context.Context, profile string, data []byte) error {
	path, err := dpapiPath(profile)
	if err != nil {
		return err
	}
	b, err := protect(data, false)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	src, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
func (*dpapiStore) Delete(_ context.Context, profile string) error {
	path, err := dpapiPath(profile)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

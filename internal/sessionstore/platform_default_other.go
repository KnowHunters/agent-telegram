//go:build (!darwin || !cgo) && !windows

package sessionstore

func platformDefaultProvider() string { return FileProvider }

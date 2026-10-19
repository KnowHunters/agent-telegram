package testutil

import "testing"

// SetHome isolates user state on Unix and Windows alike.
func SetHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

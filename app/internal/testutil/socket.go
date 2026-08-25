// Package testutil contains small helpers shared by tests in multiple app packages.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// SocketPath returns a short temporary Unix socket path and removes its directory after the test.
// The short prefix avoids platform sun_path limits when test names are long.
func SocketPath(t testing.TB, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aiv")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

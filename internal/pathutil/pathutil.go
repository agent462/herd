package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome expands a leading ~/ to the user's home directory.
// Paths like ~otheruser/... are returned unchanged since we cannot
// reliably resolve other users' home directories.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

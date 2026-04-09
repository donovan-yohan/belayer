package cli

import (
	"os"
	"path/filepath"
)

// resolveWorkspaceDir walks up from cwd looking for a .belayer/ directory.
// Falls back to ~/.belayer/ if none found.
func resolveWorkspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return defaultWorkspaceDir()
	}
	for {
		candidate := filepath.Join(dir, ".belayer")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // hit root
		}
		dir = parent
	}
	return defaultWorkspaceDir()
}

// defaultWorkspaceDir returns ~/.belayer/
func defaultWorkspaceDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".belayer")
}

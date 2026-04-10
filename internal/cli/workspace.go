package cli

import (
	"os"
	"path/filepath"
)

// resolveWorkspaceDir walks up from cwd looking for a .belayer/ directory.
// It returns the workspace root directory that contains .belayer.
// Falls back to the user's home directory when no workspace marker is found.
func resolveWorkspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return defaultWorkspaceRoot()
	}
	for {
		candidate := filepath.Join(dir, ".belayer")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // hit root
		}
		dir = parent
	}
	return defaultWorkspaceRoot()
}

// resolveBelayerDir returns the active .belayer directory path.
func resolveBelayerDir() string {
	workspaceRoot := resolveWorkspaceDir()
	candidate := filepath.Join(workspaceRoot, ".belayer")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return filepath.Join(defaultWorkspaceRoot(), ".belayer")
}

func defaultWorkspaceRoot() string {
	home, _ := os.UserHomeDir()
	return home
}

// defaultWorkspaceDir returns ~/.belayer/
func defaultWorkspaceDir() string {
	return filepath.Join(defaultWorkspaceRoot(), ".belayer")
}

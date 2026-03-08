package defaults

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteToDir copies all embedded defaults to the given directory.
// Existing files are NOT overwritten (preserves user customizations).
func WriteToDir(dir string) error {
	return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		target := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		// Don't overwrite existing files
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		data, readErr := FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// Ensure parent directory exists
		if mkErr := os.MkdirAll(filepath.Dir(target), 0755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, data, 0644)
	})
}

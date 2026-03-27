package frameworks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// List returns the names of all built-in frameworks.
func List() ([]string, error) {
	entries, err := fs.ReadDir(BuiltinFS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Install copies a framework into the target directory.
// source is either a built-in name or a filesystem path.
func Install(source, targetDir string, force bool) error {
	var srcFS fs.FS

	// Check if source is a local path.
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		srcFS = os.DirFS(source)
	} else {
		// Try as built-in name.
		sub, err := fs.Sub(BuiltinFS, source)
		if err != nil {
			return fmt.Errorf("unknown framework %q (not a path or built-in name)", source)
		}
		// Verify it's a real directory in the embedded FS.
		if _, err := fs.Stat(sub, "pipeline.yaml"); err != nil {
			return fmt.Errorf("unknown framework %q (not a path or built-in name)", source)
		}
		srcFS = sub
	}

	// Validate: source must contain pipeline.yaml.
	if _, err := fs.Stat(srcFS, "pipeline.yaml"); err != nil {
		return fmt.Errorf("framework %q missing required pipeline.yaml", source)
	}

	// Check for existing pipeline.yaml in target.
	targetPipeline := filepath.Join(targetDir, "pipeline.yaml")
	if _, err := os.Stat(targetPipeline); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", targetPipeline)
	}

	// Copy all files from source to target.
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(srcFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		perm := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			perm = 0o755
		}
		return os.WriteFile(target, data, perm)
	})
}

// EnsureInternalDir creates .belayer/.internal/ with a .gitignore.
func EnsureInternalDir(workDir string) error {
	internalDir := filepath.Join(workDir, ".belayer", ".internal")
	if err := os.MkdirAll(internalDir, 0o755); err != nil {
		return err
	}
	gitignorePath := filepath.Join(internalDir, ".gitignore")
	return os.WriteFile(gitignorePath, []byte("*\n"), 0o644)
}

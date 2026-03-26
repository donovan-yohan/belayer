package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	belayerassets "github.com/donovan-yohan/belayer"
)

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fatalf("determine working directory: %v", err)
	}

	targetRoot := filepath.Join(repoRoot, "skills")
	files, err := belayerassets.CodexSkillFiles()
	if err != nil {
		fatalf("generate codex skill files: %v", err)
	}

	if err := os.RemoveAll(targetRoot); err != nil {
		fatalf("remove %s: %v", targetRoot, err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		fatalf("create %s: %v", targetRoot, err)
	}

	paths := make([]string, 0, len(files))
	for relPath := range files {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		target := filepath.Join(targetRoot, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fatalf("create parent directory for %s: %v", target, err)
		}
		if err := os.WriteFile(target, files[relPath], 0o644); err != nil {
			fatalf("write %s: %v", target, err)
		}
	}

	fmt.Printf("wrote %d files to %s\n", len(paths), targetRoot)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

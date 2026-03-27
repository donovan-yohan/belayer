package main

import (
	"fmt"
	"os"
	"path/filepath"

	belayerassets "github.com/donovan-yohan/belayer"
)

func main() {
	repoRoot, err := os.Getwd()
	if err != nil {
		fatalf("determine working directory: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		fatalf("gencodexskills must be run from the repository root (no go.mod found in %s)", repoRoot)
	}

	targetRoot := filepath.Join(repoRoot, "skills")
	files, err := belayerassets.CodexSkillFiles()
	if err != nil {
		fatalf("generate codex skill files: %v", err)
	}

	if err := belayerassets.WriteSkillFiles(targetRoot, files); err != nil {
		fatalf("write skill files: %v", err)
	}

	fmt.Printf("wrote %d files to %s\n", len(files), targetRoot)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

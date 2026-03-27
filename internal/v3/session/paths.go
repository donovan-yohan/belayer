package session

import (
	"fmt"
	"path/filepath"
)

// InternalDir returns the path to the gitignored runtime state directory.
func InternalDir(workDir string) string {
	return filepath.Join(workDir, ".belayer", ".internal")
}

// CompletionDir returns the path to the completion files directory.
func CompletionDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "completion")
}

// CompletionFilePath returns the attempt-scoped completion file path.
func CompletionFilePath(workDir, taskID, nodeName string, attempt int) string {
	filename := fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt)
	return filepath.Join(CompletionDir(workDir), filename)
}

// InputDir returns the path to the node input directory.
func InputDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "input")
}

// OutputDir returns the path to the node output directory.
func OutputDir(workDir string) string {
	return filepath.Join(InternalDir(workDir), "output")
}

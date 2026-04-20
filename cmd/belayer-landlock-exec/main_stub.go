//go:build !linux

// main_stub.go provides a passthrough no-op for non-Linux platforms (e.g.
// Darwin dev machines) where Landlock is unavailable. The binary compiles and
// runs but applies no restrictions, making cross-platform development possible
// without changing the launch path.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "belayer-landlock-exec: usage: belayer-landlock-exec <cmd> [args...]")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "belayer-landlock-exec: non-Linux platform, passthrough (no Landlock enforcement)")
	execArgv()
}

// execArgv replaces the current process with os.Args[1:].
func execArgv() {
	argv := os.Args[1:]
	path, err := resolveExec(argv[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "belayer-landlock-exec: resolve %q: %v\n", argv[0], err)
		os.Exit(5)
	}
	if err := syscall.Exec(path, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "belayer-landlock-exec: exec %q: %v\n", path, err)
		os.Exit(5)
	}
}

// resolveExec resolves a command to its absolute path using PATH lookup.
func resolveExec(cmd string) (string, error) {
	if strings.ContainsRune(cmd, '/') {
		return cmd, nil
	}
	pathEnv := os.Getenv("PATH")
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		candidate := dir + "/" + cmd
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("not found in PATH")
}

// parseWriteRoots splits a colon-separated path list, discarding empty entries.
func parseWriteRoots(env string) []string {
	if env == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(env, ":") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

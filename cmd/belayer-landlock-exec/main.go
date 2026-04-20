//go:build linux

// belayer-landlock-exec applies a Landlock v2 write-confinement ruleset and
// then exec-replaces itself with argv[1:]. It reads BELAYER_WRITE_ROOTS
// (colon-separated absolute paths) to build an allow-list: the filesystem is
// read-only globally, and each listed path is read+write. On non-Linux builds
// this file is excluded by the build tag; a stub (main_stub.go) handles those.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "belayer-landlock-exec: usage: belayer-landlock-exec <cmd> [args...]")
		os.Exit(1)
	}

	roots := parseWriteRoots(os.Getenv("BELAYER_WRITE_ROOTS"))
	required := os.Getenv("BELAYER_LANDLOCK_REQUIRED") == "1"

	if len(roots) == 0 {
		if required {
			fmt.Fprintln(os.Stderr, "belayer-landlock-exec: BELAYER_WRITE_ROOTS unset and BELAYER_LANDLOCK_REQUIRED=1; refusing to exec unconfined")
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "belayer-landlock-exec: no WRITE_ROOTS set, passthrough")
		execArgv()
		return
	}

	rules := buildRules(roots)
	if err := landlock.V2.BestEffort().RestrictPaths(rules...); err != nil {
		fmt.Fprintf(os.Stderr, "belayer-landlock-exec: RestrictPaths: %v\n", err)
		os.Exit(4)
	}

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

// buildRules constructs the Landlock path rules: read-only for "/", read-write
// for each root in the allow-list.
func buildRules(roots []string) []landlock.Rule {
	rules := make([]landlock.Rule, 0, 1+len(roots))
	rules = append(rules, landlock.RODirs("/"))
	for _, r := range roots {
		rules = append(rules, landlock.RWDirs(r))
	}
	return rules
}

// parseWriteRoots splits a colon-separated path list, discarding empty entries.
// Exported as a package-level function so tests can exercise it directly
// without triggering any Landlock syscalls.
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

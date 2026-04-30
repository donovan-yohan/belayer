// Package agentlint contains CI tests that verify agent prompts, skills, and
// docs only reference CLI subcommands and bridge tools that actually exist in
// the belayer surface.  Running "go test ./internal/agentlint/..." is enough;
// no pre-built binary is required.
package agentlint_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers: locate repo root
// ---------------------------------------------------------------------------

// repoRoot returns the absolute path to the repository root by walking up from
// this file's location until a go.mod is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod walking up from", file)
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// CLI surface discovery
// ---------------------------------------------------------------------------

// cmdTree maps a parent command (e.g. "artifact") to its valid subcommands.
// The root level is stored under the key "".
type cmdTree map[string]map[string]bool

// rootCommands returns the set of valid root-level subcommand names.
func (ct cmdTree) rootCommands() map[string]bool { return ct[""] }

// hasSubcmd returns true when parent has a registered subcommand child.
func (ct cmdTree) hasSubcmd(parent, child string) bool {
	subs, ok := ct[parent]
	if !ok {
		return false
	}
	return subs[child]
}

// parseAvailableCommands extracts command names from a cobra --help output.
// It looks for the "Available Commands:" section and reads indented words.
func parseAvailableCommands(output string) []string {
	var cmds []string
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "Available Commands:" {
			inSection = true
			continue
		}
		if inSection {
			// A blank line or a non-indented line ends the section.
			if line == "" || (len(line) > 0 && line[0] != ' ') {
				inSection = false
				continue
			}
			// cobra indents commands with at least two spaces.
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			// The command name is the first word on the line.
			parts := strings.Fields(trimmed)
			if len(parts) > 0 {
				cmds = append(cmds, parts[0])
			}
		}
	}
	return cmds
}

// runHelp runs "go run ./cmd/belayer [args...] --help" and returns stdout+stderr.
// Fails the test on timeout or non-zero exit so a broken CLI build does not
// quietly mutate into a flood of misleading "unknown CLI reference" errors.
func runHelp(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"run", "./cmd/belayer"}, args...)
	cmdArgs = append(cmdArgs, "--help")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", cmdArgs...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("timed out running go %s", strings.Join(cmdArgs, " "))
	}
	if err != nil {
		t.Fatalf("go %s failed: %v\noutput:\n%s", strings.Join(cmdArgs, " "), err, out)
	}
	return string(out)
}

// buildCLITree discovers the full command tree by recursively calling --help
// on each command that declares subcommands. Skips "help" and "completion"
// pseudo-commands. Keys in the returned tree are space-joined command paths
// (e.g. "" for root, "run" for `belayer run`, "run start" for
// `belayer run start` — though cobra currently stops one level below root for
// us, the walker keeps going whenever a node lists Available Commands).
func buildCLITree(t *testing.T, root string) cmdTree {
	t.Helper()
	tree := make(cmdTree)

	var walk func(path []string)
	walk = func(path []string) {
		help := runHelp(t, root, path...)
		cmds := parseAvailableCommands(help)

		var real []string
		for _, c := range cmds {
			if c == "help" || c == "completion" {
				continue
			}
			real = append(real, c)
		}
		if len(real) == 0 {
			return
		}

		key := strings.Join(path, " ")
		tree[key] = make(map[string]bool)
		for _, c := range real {
			tree[key][c] = true
			child := append(append([]string(nil), path...), c)
			walk(child)
		}
	}

	walk(nil)
	return tree
}

// ---------------------------------------------------------------------------
// Tool surface discovery
// ---------------------------------------------------------------------------

// toolNameRe matches lines like:  "name": "belayer_foo_bar",
var toolNameRe = regexp.MustCompile(`"name":\s*"(belayer_[a-z_]+)"`)

// parseToolNames returns the set of registered belayer_* tool names found
// in the Hermes plugin's schema dicts. The plugin replaced
// hermes_bridge/tools.py in the phase-2 migration; the source of truth is
// now plugins/belayer/tools.py.
func parseToolNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	toolsPath := filepath.Join(root, "plugins", "belayer", "tools.py")
	f, err := os.Open(toolsPath)
	if err != nil {
		t.Fatalf("cannot open %s: %v", toolsPath, err)
	}
	defer f.Close()

	names := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		m := toolNameRe.FindStringSubmatch(scanner.Text())
		if m != nil {
			names[m[1]] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", toolsPath, err)
	}
	return names
}

// ---------------------------------------------------------------------------
// Allowlist
// ---------------------------------------------------------------------------

// loadAllowlist reads internal/agentlint/allowlist.txt.  Lines starting with
// "#" are comments.  Tokens are returned as-is (may contain spaces for
// space-form pairs).
func loadAllowlist(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, "internal", "agentlint", "allowlist.txt")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]bool) // allowlist is optional
		}
		t.Fatalf("cannot open allowlist %s: %v", path, err)
	}
	defer f.Close()

	allowed := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowed[line] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading allowlist: %v", err)
	}
	return allowed
}

// ---------------------------------------------------------------------------
// File discovery
// ---------------------------------------------------------------------------

// collectFiles returns all files under the given directories that match the
// given extension filter.  It returns paths relative to root for display, and
// absolute paths for reading.
func collectFiles(t *testing.T, root string, dirs []string, ext string, skipDirs []string) []string {
	t.Helper()
	var files []string
	for _, dir := range dirs {
		absDir := filepath.Join(root, dir)
		err := filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				for _, skip := range skipDirs {
					if path == filepath.Join(root, skip) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if ext == "" || strings.HasSuffix(info.Name(), ext) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", absDir, err)
		}
	}
	return files
}

// ---------------------------------------------------------------------------
// Extraction regexes
// ---------------------------------------------------------------------------

// spaceFormRe extracts "belayer <firstword>" from prose.
// Group 1 = first subcommand word; group 2 = optional second word.
var spaceFormRe = regexp.MustCompile(`\bbelayer\s+([a-z][a-z0-9-]*)(?:\s+([a-z][a-z0-9-]*))?`)

// underscoreFormRe extracts belayer_* tool names.
var underscoreFormRe = regexp.MustCompile(`\bbelayer_[a-z_]+\b`)

// ---------------------------------------------------------------------------
// The test
// ---------------------------------------------------------------------------

func TestAgentPromptsOnlyReferenceRealCommands(t *testing.T) {
	root := repoRoot(t)

	tree := buildCLITree(t, root)
	toolNames := parseToolNames(t, root)
	allowlist := loadAllowlist(t, root)

	// Directories to scan.
	scanDirs := []string{
		"agents",
		"examples/templates",
		"examples/talent-catalog",
		"skills",
		"docs",
	}
	skipDirs := []string{
		"docs/design-docs",
		"docs/exec-plans",
	}

	// Collect .md files.
	mdFiles := collectFiles(t, root, scanDirs, ".md", skipDirs)

	// Also include the entrypoint shell script (no extension filter).
	entrypoint := filepath.Join(root, "docker", "belayer-entrypoint.sh")
	if _, err := os.Stat(entrypoint); err == nil {
		mdFiles = append(mdFiles, entrypoint)
	}

	type miss struct {
		file  string
		line  int
		token string
	}

	var spaceMisses []miss
	var underscoreMisses []miss

	for _, path := range mdFiles {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("cannot open %s: %v", path, err)
		}

		lineNum := 0
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			// --- space-form ---
			for _, m := range spaceFormRe.FindAllStringSubmatch(line, -1) {
				first := m[1]
				second := m[2] // may be empty

				// Skip if first word looks like a flag.
				if strings.HasPrefix(first, "-") {
					continue
				}

				// Check allowlist for the pair or the bare first word.
				pairToken := first
				if second != "" {
					pairToken = first + " " + second
				}
				if allowlist[pairToken] || allowlist[first] {
					continue
				}

				// Validate first word is a known root command.
				if !tree.rootCommands()[first] {
					spaceMisses = append(spaceMisses, miss{
						file:  path,
						line:  lineNum,
						token: "belayer " + pairToken,
					})
					continue
				}

				// If there's a second word and the first command has
				// subcommands, validate the pair.
				if second != "" && len(tree[first]) > 0 {
					if !tree.hasSubcmd(first, second) && !allowlist[pairToken] {
						spaceMisses = append(spaceMisses, miss{
							file:  path,
							line:  lineNum,
							token: "belayer " + pairToken,
						})
					}
				}
			}

			// --- underscore-form ---
			for _, m := range underscoreFormRe.FindAllString(line, -1) {
				if allowlist[m] {
					continue
				}
				if !toolNames[m] {
					underscoreMisses = append(underscoreMisses, miss{
						file:  path,
						line:  lineNum,
						token: m,
					})
				}
			}
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			t.Fatalf("scanner error on %s: %v", path, err)
		}
	}

	t.Run("space-form", func(t *testing.T) {
		for _, m := range spaceMisses {
			t.Errorf("%s:%d: unknown CLI reference: %s", m.file, m.line, m.token)
		}
	})

	t.Run("underscore-form", func(t *testing.T) {
		for _, m := range underscoreMisses {
			t.Errorf("%s:%d: unknown tool reference: %s", m.file, m.line, m.token)
		}
	})
}

# Belayer Marketplace Implementation

> **Status**: Complete | **Created**: 2026-03-13 | **Last Updated**: 2026-03-13
> **Design Doc**: `docs/design-docs/2026-03-13-belayer-marketplace-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-13 | Design | Fork and vendor harness + pr from llm-agents | Belayer hard-depends on these; canonical source should live here |
| 2026-03-13 | Design | GitHub marketplace (not local directory) | Works on any machine, Claude Code handles cloning/caching |
| 2026-03-13 | Design | Auto-install during `belayer init` | Natural extension of existing init flow |
| 2026-03-13 | Design | Idempotent registration (skip if exists) | Safe for repeated `belayer init` runs |

## Progress

- [x] Task 1: Create marketplace structure and vendor plugins _(completed 2026-03-13)_
- [x] Task 2: Plugin registry package _(completed 2026-03-13)_
- [x] Task 3: Wire plugin registration into `belayer init` _(completed 2026-03-13)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-13 | go.mod/go.sum and scm/github changes were pre-existing dirty state | Had to distinguish our changes from unrelated dirty files during review | Scoped review to only our new/modified files |

## Plan Drift

| Task | Plan Said | Actually Did | Why |
|------|-----------|-------------|-----|
| Task 2 | Simple `os.WriteFile` | Atomic write with temp file + rename | Review identified corruption risk for Claude Code's shared registry files |
| Task 2 | `readJSONMap` returns raw errors | Added file path context to error messages | Review identified undiagnosable errors |
| Task 3 | `claudeConfigDir()` returns `string` | Returns `(string, error)` | Review caught silent `os.UserHomeDir()` error discard |
| Task 3 | Map iteration for plugins | Deterministic slice iteration | Review caught non-deterministic install order |

---

## Task 1: Create Marketplace Structure and Vendor Plugins

**Goal:** Set up the marketplace manifest and copy harness + pr plugin source from llm-agents.

**Files:**
- Create: `.claude-plugin/marketplace.json`
- Create: `plugins/harness/` (entire tree copied from `/Users/donovanyohan/Documents/Programs/work/llm-agents/plugins/harness/`)
- Create: `plugins/pr/` (entire tree copied from `/Users/donovanyohan/Documents/Programs/work/llm-agents/plugins/pr/`)

**Steps:**

1. Create `.claude-plugin/marketplace.json`:
   ```json
   {
     "$schema": "https://anthropic.com/claude-code/marketplace.schema.json",
     "name": "belayer",
     "owner": { "name": "donovanyohan" },
     "metadata": {
       "description": "Belayer marketplace — plugins shipped with the multi-repo coding agent orchestrator",
       "version": "1.0.0"
     },
     "plugins": [
       {
         "name": "harness",
         "source": "./plugins/harness",
         "description": "3-tier documentation system with living execution plans"
       },
       {
         "name": "pr",
         "source": "./plugins/pr",
         "description": "PR lifecycle management — author, review, resolve, update"
       }
     ]
   }
   ```

2. Copy the harness plugin from the current local branch:
   ```bash
   cp -r /Users/donovanyohan/Documents/Programs/work/llm-agents/plugins/harness plugins/harness
   ```

3. Copy the pr plugin from the current local branch:
   ```bash
   cp -r /Users/donovanyohan/Documents/Programs/work/llm-agents/plugins/pr plugins/pr
   ```

4. Update author in `plugins/harness/.claude-plugin/plugin.json`: change `"name": "paywithextend"` to `"name": "donovanyohan"`.

5. Update author in `plugins/pr/.claude-plugin/plugin.json`: change `"name": "paywithextend"` to `"name": "donovanyohan"`.

6. Verify the structure:
   ```bash
   find plugins/ -type f | sort
   find .claude-plugin/ -type f
   ```

**Verification:** Files exist in expected structure. `cat .claude-plugin/marketplace.json` is valid JSON.

---

## Task 2: Plugin Registry Package

**Goal:** Create `internal/plugins/registry.go` that reads/writes Claude Code's plugin registry files (`known_marketplaces.json` and `installed_plugins.json`). Testable via injected base path.

**Files:**
- Create: `internal/plugins/registry.go`
- Create: `internal/plugins/registry_test.go`

**Steps:**

1. Write the failing test `internal/plugins/registry_test.go`:
   ```go
   package plugins

   import (
       "encoding/json"
       "os"
       "path/filepath"
       "testing"
   )

   func TestRegisterMarketplace_WritesNewEntry(t *testing.T) {
       dir := t.TempDir()
       r := NewRegistry(dir)

       err := r.RegisterMarketplace("donovanyohan/belayer")
       if err != nil {
           t.Fatalf("unexpected error: %v", err)
       }

       data, err := os.ReadFile(filepath.Join(dir, "plugins", "known_marketplaces.json"))
       if err != nil {
           t.Fatalf("reading marketplaces file: %v", err)
       }

       var raw map[string]json.RawMessage
       if err := json.Unmarshal(data, &raw); err != nil {
           t.Fatalf("parsing JSON: %v", err)
       }
       if _, ok := raw["belayer"]; !ok {
           t.Error("expected 'belayer' key in marketplaces")
       }
   }

   func TestRegisterMarketplace_IdempotentSkipsExisting(t *testing.T) {
       dir := t.TempDir()
       r := NewRegistry(dir)

       _ = r.RegisterMarketplace("donovanyohan/belayer")
       err := r.RegisterMarketplace("donovanyohan/belayer")
       if err != nil {
           t.Fatalf("second call should not error: %v", err)
       }
   }

   func TestInstallPlugin_WritesEntry(t *testing.T) {
       dir := t.TempDir()
       r := NewRegistry(dir)

       err := r.InstallPlugin("harness", "2.1.0")
       if err != nil {
           t.Fatalf("unexpected error: %v", err)
       }

       data, err := os.ReadFile(filepath.Join(dir, "plugins", "installed_plugins.json"))
       if err != nil {
           t.Fatalf("reading installed file: %v", err)
       }

       var raw map[string]json.RawMessage
       if err := json.Unmarshal(data, &raw); err != nil {
           t.Fatalf("parsing JSON: %v", err)
       }
       if _, ok := raw["harness@belayer"]; !ok {
           t.Error("expected 'harness@belayer' key")
       }
   }

   func TestInstallPlugin_IdempotentSkipsExisting(t *testing.T) {
       dir := t.TempDir()
       r := NewRegistry(dir)

       _ = r.InstallPlugin("harness", "2.1.0")
       err := r.InstallPlugin("harness", "2.1.0")
       if err != nil {
           t.Fatalf("second call should not error: %v", err)
       }
   }

   func TestRegisterMarketplace_MergesWithExistingFile(t *testing.T) {
       dir := t.TempDir()
       pluginsDir := filepath.Join(dir, "plugins")
       os.MkdirAll(pluginsDir, 0755)

       // Pre-existing marketplace
       existing := `{"other-marketplace":{"source":{"source":"github","repo":"someone/other"}}}`
       os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(existing), 0644)

       r := NewRegistry(dir)
       err := r.RegisterMarketplace("donovanyohan/belayer")
       if err != nil {
           t.Fatalf("unexpected error: %v", err)
       }

       data, _ := os.ReadFile(filepath.Join(pluginsDir, "known_marketplaces.json"))
       var raw map[string]json.RawMessage
       json.Unmarshal(data, &raw)

       if _, ok := raw["belayer"]; !ok {
           t.Error("expected 'belayer' key")
       }
       if _, ok := raw["other-marketplace"]; !ok {
           t.Error("existing marketplace should be preserved")
       }
   }
   ```

2. Run tests to verify they fail:
   ```bash
   go test ./internal/plugins/... -v
   ```

3. Implement `internal/plugins/registry.go`:
   ```go
   package plugins

   import (
       "encoding/json"
       "fmt"
       "os"
       "path/filepath"
       "time"
   )

   const (
       marketplaceName = "belayer"
       marketplacesFile = "known_marketplaces.json"
       installedFile    = "installed_plugins.json"
   )

   // Registry reads and writes Claude Code's plugin registry files.
   // baseDir is the Claude Code config directory (typically ~/.claude).
   type Registry struct {
       baseDir string
   }

   func NewRegistry(baseDir string) *Registry {
       return &Registry{baseDir: baseDir}
   }

   // RegisterMarketplace adds the belayer GitHub marketplace to known_marketplaces.json.
   // Idempotent: skips if already registered.
   func (r *Registry) RegisterMarketplace(repo string) error {
       path := filepath.Join(r.baseDir, "plugins", marketplacesFile)
       existing, err := readJSONMap(path)
       if err != nil {
           return fmt.Errorf("reading marketplaces: %w", err)
       }

       if _, ok := existing[marketplaceName]; ok {
           return nil // already registered
       }

       entry := map[string]any{
           "source": map[string]string{
               "source": "github",
               "repo":   repo,
           },
           "installLocation": filepath.Join(r.baseDir, "plugins", "marketplaces", marketplaceName),
           "lastUpdated":     time.Now().UTC().Format(time.RFC3339Nano),
           "autoUpdate":      true,
       }

       existing[marketplaceName] = entry
       return writeJSONMap(path, existing)
   }

   // InstallPlugin adds a plugin entry to installed_plugins.json.
   // Idempotent: skips if already installed under the belayer marketplace.
   func (r *Registry) InstallPlugin(name, version string) error {
       path := filepath.Join(r.baseDir, "plugins", installedFile)
       existing, err := readJSONMap(path)
       if err != nil {
           return fmt.Errorf("reading installed plugins: %w", err)
       }

       key := name + "@" + marketplaceName
       if _, ok := existing[key]; ok {
           return nil // already installed
       }

       entry := []map[string]any{{
           "scope":       "user",
           "installPath": filepath.Join(r.baseDir, "plugins", "cache", marketplaceName, name, version),
           "version":     version,
           "installedAt": time.Now().UTC().Format(time.RFC3339Nano),
           "lastUpdated": time.Now().UTC().Format(time.RFC3339Nano),
       }}

       existing[key] = entry
       return writeJSONMap(path, existing)
   }

   func readJSONMap(path string) (map[string]any, error) {
       data, err := os.ReadFile(path)
       if err != nil {
           if os.IsNotExist(err) {
               return make(map[string]any), nil
           }
           return nil, err
       }
       var m map[string]any
       if err := json.Unmarshal(data, &m); err != nil {
           return nil, err
       }
       return m, nil
   }

   func writeJSONMap(path string, m map[string]any) error {
       if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
           return err
       }
       data, err := json.MarshalIndent(m, "", "  ")
       if err != nil {
           return err
       }
       return os.WriteFile(path, data, 0644)
   }
   ```

4. Run tests to verify they pass:
   ```bash
   go test ./internal/plugins/... -v
   ```

5. Commit:
   ```bash
   git add internal/plugins/
   git commit -m "feat: add plugin registry package for Claude Code marketplace registration"
   ```

**Verification:** `go test ./internal/plugins/... -v` — all 5 tests pass.

---

## Task 3: Wire Plugin Registration into `belayer init`

**Goal:** Call `plugins.Registry` from the init command to register the belayer marketplace and install harness + pr.

**Files:**
- Modify: `internal/cli/init.go`
- Create: `internal/cli/init_test.go`

**Steps:**

1. Write the failing test `internal/cli/init_test.go`:
   ```go
   package cli

   import (
       "encoding/json"
       "os"
       "path/filepath"
       "testing"
   )

   func TestInitCmd_RegistersPlugins(t *testing.T) {
       // Set up temp dirs for both belayer and claude config
       claudeDir := t.TempDir()
       t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)

       belayerDir := t.TempDir()
       t.Setenv("HOME", belayerDir) // init writes to ~/.belayer/

       cmd := newInitCmd()
       cmd.SetArgs([]string{})
       err := cmd.Execute()
       if err != nil {
           t.Fatalf("init failed: %v", err)
       }

       // Verify marketplace was registered
       mpData, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "known_marketplaces.json"))
       if err != nil {
           t.Fatalf("marketplace file not created: %v", err)
       }
       var mp map[string]json.RawMessage
       json.Unmarshal(mpData, &mp)
       if _, ok := mp["belayer"]; !ok {
           t.Error("belayer marketplace not registered")
       }

       // Verify plugins were installed
       ipData, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
       if err != nil {
           t.Fatalf("installed plugins file not created: %v", err)
       }
       var ip map[string]json.RawMessage
       json.Unmarshal(ipData, &ip)
       if _, ok := ip["harness@belayer"]; !ok {
           t.Error("harness plugin not installed")
       }
       if _, ok := ip["pr@belayer"]; !ok {
           t.Error("pr plugin not installed")
       }
   }
   ```

2. Run test to verify it fails:
   ```bash
   go test ./internal/cli/ -run TestInitCmd_RegistersPlugins -v
   ```

3. Update `internal/cli/init.go` to add plugin registration after writing defaults:
   ```go
   package cli

   import (
       "fmt"
       "os"
       "path/filepath"

       "github.com/donovan-yohan/belayer/internal/config"
       "github.com/donovan-yohan/belayer/internal/defaults"
       "github.com/donovan-yohan/belayer/internal/plugins"
       "github.com/spf13/cobra"
   )

   const githubRepo = "donovanyohan/belayer"

   func newInitCmd() *cobra.Command {
       return &cobra.Command{
           Use:   "init",
           Short: "Initialize belayer configuration",
           Long:  "Creates the ~/.belayer/ directory with config.json, default prompts, validation profiles, and belayer.toml. Registers the belayer marketplace and installs required Claude Code plugins.",
           RunE: func(cmd *cobra.Command, args []string) error {
               dir, err := config.EnsureDir()
               if err != nil {
                   return fmt.Errorf("creating belayer directory: %w", err)
               }

               cfg, err := config.Load()
               if err != nil {
                   return fmt.Errorf("loading config: %w", err)
               }

               if err := config.Save(cfg); err != nil {
                   return fmt.Errorf("saving config: %w", err)
               }

               configDir := filepath.Join(dir, "config")
               if err := defaults.WriteToDir(configDir); err != nil {
                   return fmt.Errorf("writing default config: %w", err)
               }

               // Register belayer marketplace and install plugins
               if err := registerPlugins(cmd); err != nil {
                   // Non-fatal: print warning but don't fail init
                   fmt.Fprintf(cmd.ErrOrStderr(), "Warning: plugin registration failed: %v\n", err)
               }

               fmt.Fprintf(cmd.OutOrStdout(), "Initialized belayer at %s\n", dir)
               return nil
           },
       }
   }

   func registerPlugins(cmd *cobra.Command) error {
       claudeDir := claudeConfigDir()
       reg := plugins.NewRegistry(claudeDir)

       if err := reg.RegisterMarketplace(githubRepo); err != nil {
           return fmt.Errorf("registering marketplace: %w", err)
       }

       pluginVersions := map[string]string{
           "harness": plugins.HarnessVersion,
           "pr":      plugins.PRVersion,
       }

       for name, version := range pluginVersions {
           if err := reg.InstallPlugin(name, version); err != nil {
               return fmt.Errorf("installing %s: %w", name, err)
           }
       }

       fmt.Fprintf(cmd.OutOrStdout(), "Registered belayer marketplace. Installed plugins: harness, pr\n")
       return nil
   }

   // claudeConfigDir returns the Claude Code config directory.
   // Respects CLAUDE_CONFIG_DIR env var for testing.
   func claudeConfigDir() string {
       if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
           return dir
       }
       home, _ := os.UserHomeDir()
       return filepath.Join(home, ".claude")
   }
   ```

4. Add version constants to `internal/plugins/registry.go` (read from embedded plugin.json at build time would be ideal, but for now use constants that match the vendored versions):
   ```go
   // Plugin versions — keep in sync with plugins/*/. claude-plugin/plugin.json
   const (
       HarnessVersion = "2.1.0"
       PRVersion      = "1.2.0"
   )
   ```

5. Run tests:
   ```bash
   go test ./internal/cli/ -run TestInitCmd_RegistersPlugins -v
   go test ./internal/plugins/... -v
   ```

6. Run full test suite to ensure no regressions:
   ```bash
   go test ./...
   ```

7. Commit:
   ```bash
   git add internal/cli/init.go internal/cli/init_test.go internal/plugins/registry.go
   git commit -m "feat: register belayer marketplace and install plugins during init"
   ```

**Verification:** `go test ./... ` passes. `go build -o belayer ./cmd/belayer && ./belayer init` registers marketplace.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Clean 3-task decomposition mapped directly to the design
- Registry package was simple and testable via injected base path
- Review caught real issues (atomic writes, error propagation) before they could cause problems

**What didn't:**
- Initial implementation discarded `os.UserHomeDir()` error — inconsistent with codebase pattern in `config.go`

**Learnings to codify:**
- When writing to files owned by other tools (e.g., Claude Code's registry), always use atomic writes (temp file + rename)
- Follow existing error propagation patterns in the codebase — `config.go` already handled `UserHomeDir()` correctly

# Instance-to-Crag Rename + TUI Cleanup Implementation Plan

> **Status**: Completed | **Created**: 2026-03-10 | **Completed**: 2026-03-11
> **Design Doc**: N/A (cleanup/rename, not a new feature)
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-10 | Design | Rename `--instance/-i` to `--crag/-c` everywhere | Terminology was updated to climbing metaphors but CLI flags were missed |
| 2026-03-10 | Design | Rename `BELAYER_INSTANCE` env var to `BELAYER_CRAG` | Consistency with new naming |
| 2026-03-10 | Design | Add DB migration 003 to rename `instances` table to `crags` and `instance_id` to `crag_id` | Complete the rename at data layer |
| 2026-03-10 | Design | Remove TUI docs and references (code never existed) | Stale documentation pollutes context |
| 2026-03-10 | Design | Keep `internal/instance/` package name as-is | Go package renames are high-churn, low-value; the exported API uses `CragConfig` already |
| 2026-03-10 | Design | Rename disk path from `~/.belayer/instances/` to `~/.belayer/crags/` | ARCHITECTURE.md already documents `crags/` but code uses `instances/` |
| 2026-03-10 | Design | Add backwards-compat migration for existing disk layout | Users with existing crags need them to still work after upgrade |
| 2026-03-11 | Retrospective | Plan completed | All 10 tasks done + review-phase internal renames across 12 additional files |

## Progress

- [x] Task 1: DB migration 003 — rename `instances` table and `instance_id` columns _(completed 2026-03-11)_
- [x] Task 2-6: All Go code renames (batched as single task) _(completed 2026-03-11)_
- [x] Task 7: Remove TUI references from docs and code _(completed 2026-03-11)_
- [x] Task 8: Update live docs (CLAUDE.md, ARCHITECTURE.md, DESIGN.md, README.md, PLANS.md) _(completed 2026-03-11)_
- [x] Task 9: Run full test suite and fix breakages _(completed 2026-03-11)_

## Surprises & Discoveries

| 2026-03-11 | Worker also updated `testutil/db.go` to insert into `crags` table | Sensible — tests would fail without it | Included in commit |
| 2026-03-11 | Review found internal struct fields/params still using "instance" naming | `belayer.go`, `taskrunner.go`, `belayerconfig/config.go`, `logmgr.go` had internal `instanceDir`/`instanceConfigDir` fields | Fixed in review pass |
| 2026-03-11 | Review found `tasks/` vs `problems/` doc mismatch | ARCHITECTURE.md and DESIGN.md said `problems/` but code uses `tasks/` | Fixed in docs |
| 2026-03-11 | Review found migration 003 not listed in ARCHITECTURE.md Code Map | Migration list was stale | Fixed |

## Plan Drift

| Date | Planned | Actual | Impact |
|------|---------|--------|--------|
| 2026-03-11 | Plan only covered user-facing renames | Review revealed ~40 internal struct field/param/method renames needed | Added 12 additional files to the change set |

---

## File Structure

### Files to modify (code)

| File | Change |
|------|--------|
| `internal/db/migrations/003_rename_instance_to_crag.sql` | **Create**: New migration |
| `internal/model/types.go` | Rename `InstanceID` → `CragID` field |
| `internal/store/store.go` | Rename `instance_id` in SQL, `ListProblemsForInstance` → `ListProblemsForCrag` |
| `internal/config/config.go` | Rename `DefaultInstance` → `DefaultCrag`, `Instances` → `Crags` |
| `internal/instance/instance.go` | Update disk path `instances/` → `crags/`, update error messages |
| `internal/cli/helpers.go` | Rename `resolveInstanceName` → `resolveCragName`, update env var |
| `internal/cli/helpers_test.go` | Update test for new function name and env var |
| `internal/cli/belayer_cmd.go` | Rename flags, variables, re-exec args |
| `internal/cli/problem.go` | Rename flags and variables |
| `internal/cli/status.go` | Rename flags and variables |
| `internal/cli/logs.go` | Rename flags and variables |
| `internal/cli/setter_cmd.go` | Rename flags, variables, env var |
| `internal/cli/mail.go` | Rename parameter name in `mailStore` |
| `internal/belayer/belayer.go` | Rename `InstanceName` → `CragName`, `InstanceDir` → `CragDir` in Config |
| `internal/manage/prompt.go` | Rename `InstanceName` → `CragName` in PromptData |
| `internal/defaults/claudemd/setter.md` | Update `BELAYER_INSTANCE` → `BELAYER_CRAG`, `--instance` → `--crag` |

### Files to modify (docs)

| File | Change |
|------|--------|
| `CLAUDE.md` | Remove TUI row from Documentation Map |
| `README.md` | Remove TUI references, update `-i` to `-c`, remove `belayer tui` |
| `docs/ARCHITECTURE.md` | Remove TUI section, update CLI table, fix path references |
| `docs/DESIGN.md` | Remove TUI section, update CLI table, update env var references |
| `docs/PLANS.md` | Remove TUI bugfix items |
| `docs/TUI.md` | **Delete** |
| `docs/design-docs/index.md` | Mark TUI design doc as archived/removed |

### Files NOT modified (intentional)

- `internal/instance/` package name — already uses `CragConfig`, renaming package is high churn
- Completed design docs in `docs/design-docs/` — historical record, not live docs
- Completed exec plans in `docs/exec-plans/completed/` — historical record

---

### Task 1: DB Migration 003 — Rename `instances` table and `instance_id` columns

**Files:**
- Create: `internal/db/migrations/003_rename_instance_to_crag.sql`

- [ ] **Step 1: Write the migration SQL**

```sql
-- 003_rename_instance_to_crag.sql: Complete the crag rename at the data layer
-- instances -> crags, instance_id -> crag_id

ALTER TABLE instances RENAME TO crags;

ALTER TABLE problems RENAME COLUMN instance_id TO crag_id;

-- Drop old index and recreate with new name
DROP INDEX IF EXISTS idx_problems_instance;
CREATE INDEX IF NOT EXISTS idx_problems_crag ON problems(crag_id);
```

- [ ] **Step 2: Verify migration applies cleanly**

Run: `go test ./internal/db/... -v -run TestMigrations`
Expected: PASS (the migration runner applies all migrations in order)

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/003_rename_instance_to_crag.sql
git commit -m "feat: add migration 003 to rename instances table to crags"
```

---

### Task 2: Rename Go struct fields and store methods

**Files:**
- Modify: `internal/model/types.go`
- Modify: `internal/store/store.go`

- [ ] **Step 1: Rename `InstanceID` to `CragID` in model/types.go**

In `internal/model/types.go`, change the Problem struct field:
```go
CragID string `json:"crag_id"`
```

- [ ] **Step 2: Update store.go SQL and method names**

In `internal/store/store.go`:
- All SQL referencing `instance_id` → `crag_id`
- `problem.InstanceID` → `problem.CragID` in all scan/insert sites
- `ListProblemsForInstance` → `ListProblemsForCrag` (rename method + parameter)
- `GetPendingProblems` and `GetActiveProblems` parameter names: `instanceID` → `cragID`

- [ ] **Step 3: Run tests**

Run: `go test ./internal/store/... -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/model/types.go internal/store/store.go
git commit -m "rename: InstanceID -> CragID in model and store"
```

---

### Task 3: Rename CLI flags `--instance/-i` → `--crag/-c` and helper function

**Files:**
- Modify: `internal/cli/helpers.go`
- Modify: `internal/cli/helpers_test.go`
- Modify: `internal/cli/belayer_cmd.go`
- Modify: `internal/cli/problem.go`
- Modify: `internal/cli/status.go`
- Modify: `internal/cli/logs.go`
- Modify: `internal/cli/setter_cmd.go`
- Modify: `internal/cli/mail.go`

- [ ] **Step 1: Rename `resolveInstanceName` → `resolveCragName` in helpers.go**

Update the function name and error message. Keep env var as-is for now (Task 4).

```go
func resolveCragName(cragName string) (string, error) {
	if cragName != "" {
		return cragName, nil
	}
	if envName := os.Getenv("BELAYER_INSTANCE"); envName != "" {
		return envName, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}
	if cfg.DefaultInstance == "" {
		return "", fmt.Errorf("no default crag set; use --crag or run `belayer crag create` first")
	}
	return cfg.DefaultInstance, nil
}
```

- [ ] **Step 2: Update helpers_test.go**

Rename `TestResolveInstanceName_EnvFallback` → `TestResolveCragName_EnvFallback`, update all `resolveInstanceName` calls to `resolveCragName`.

- [ ] **Step 3: Rename flags in belayer_cmd.go**

In all three subcommands (start, stop, status):
- `var instanceName string` → `var cragName string`
- `resolveInstanceName(instanceName)` → `resolveCragName(cragName)`
- Flag definition: `StringVarP(&cragName, "crag", "c", "", "Crag name (defaults to default crag)")`
- In `runBelayerDaemonBackground`: `"--instance"` → `"--crag"` in the re-exec args

- [ ] **Step 4: Rename flags in problem.go**

Both `newProblemCreateCmd` and `newProblemListCmd`:
- `var instanceName string` → `var cragName string`
- Update flag definition to `"crag"` with no short flag (no `-c` to avoid conflict with `--climbs`)
- Update all references

- [ ] **Step 5: Rename flags in status.go**

- `var instanceName string` → `var cragName string`
- Flag: `StringVar(&cragName, "crag", "", "Crag name (defaults to default crag)")`

- [ ] **Step 6: Rename flags in logs.go**

All three subcommands (view, cleanup, stats):
- `var instanceName string` → `var cragName string`
- Flag: `StringVarP(&cragName, "crag", "c", "", "Crag name")`

- [ ] **Step 7: Rename flags in setter_cmd.go**

- `var instanceName string` → `var cragName string`
- Flag: `StringVarP(&cragName, "crag", "c", "", "Crag name (defaults to default crag)")`

- [ ] **Step 8: Rename parameter in mail.go**

- `func mailStore(instanceName string)` → `func mailStore(cragName string)`
- Update internal `resolveInstanceName` → `resolveCragName` call

- [ ] **Step 9: Run tests**

Run: `go test ./internal/cli/... -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/cli/
git commit -m "rename: --instance/-i -> --crag/-c in all CLI commands"
```

---

### Task 4: Rename env var `BELAYER_INSTANCE` → `BELAYER_CRAG`

**Files:**
- Modify: `internal/cli/helpers.go`
- Modify: `internal/cli/helpers_test.go`
- Modify: `internal/cli/setter_cmd.go`
- Modify: `internal/defaults/claudemd/setter.md`

- [ ] **Step 1: Update env var in helpers.go**

Change `os.Getenv("BELAYER_INSTANCE")` → `os.Getenv("BELAYER_CRAG")`.

Also add backwards compat fallback:
```go
if envName := os.Getenv("BELAYER_CRAG"); envName != "" {
    return envName, nil
}
// Backwards compat: support old env var name
if envName := os.Getenv("BELAYER_INSTANCE"); envName != "" {
    return envName, nil
}
```

- [ ] **Step 2: Update setter_cmd.go env var**

In `execClaudeInDir`:
- Change the dedup filter from `BELAYER_INSTANCE=` to `BELAYER_CRAG=`
- Change the env append to `"BELAYER_CRAG="+cragName`

- [ ] **Step 3: Update helpers_test.go**

Change `t.Setenv("BELAYER_INSTANCE", ...)` → `t.Setenv("BELAYER_CRAG", ...)`.

- [ ] **Step 4: Update setter.md template**

Change `BELAYER_INSTANCE` references to `BELAYER_CRAG` and `--instance` to `--crag`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/cli/helpers.go internal/cli/helpers_test.go internal/cli/setter_cmd.go internal/defaults/claudemd/setter.md
git commit -m "rename: BELAYER_INSTANCE -> BELAYER_CRAG env var"
```

---

### Task 5: Rename config fields

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Rename struct fields**

```go
type Config struct {
	DefaultCrag string            `json:"default_crag"`
	Crags       map[string]string `json:"crags"` // name -> path
}
```

Update `DefaultConfig()`:
```go
func DefaultConfig() *Config {
	return &Config{
		Crags: make(map[string]string),
	}
}
```

- [ ] **Step 2: Update all references to old field names**

Search for `cfg.DefaultInstance` → `cfg.DefaultCrag` and `cfg.Instances` → `cfg.Crags` across the codebase:
- `internal/cli/helpers.go`
- `internal/instance/instance.go` (Create, Load, List, Delete)

- [ ] **Step 3: Add backwards-compat JSON loading**

Add a custom `UnmarshalJSON` to Config that reads both old and new field names so existing `config.json` files still work:

```go
func (c *Config) UnmarshalJSON(data []byte) error {
	type raw struct {
		DefaultCrag     string            `json:"default_crag"`
		DefaultInstance string            `json:"default_instance"`
		Crags           map[string]string `json:"crags"`
		Instances       map[string]string `json:"instances"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	c.DefaultCrag = r.DefaultCrag
	if c.DefaultCrag == "" {
		c.DefaultCrag = r.DefaultInstance
	}
	c.Crags = r.Crags
	if c.Crags == nil {
		c.Crags = r.Instances
	}
	if c.Crags == nil {
		c.Crags = make(map[string]string)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/... ./internal/instance/... ./internal/cli/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/cli/helpers.go internal/instance/instance.go
git commit -m "rename: config fields DefaultInstance/Instances -> DefaultCrag/Crags"
```

---

### Task 6: Rename disk path `instances/` → `crags/` in instance.Create()

**Files:**
- Modify: `internal/instance/instance.go`

- [ ] **Step 1: Update the directory path in Create()**

Change:
```go
instanceDir := filepath.Join(belayerDir, "instances", name)
```
To:
```go
instanceDir := filepath.Join(belayerDir, "crags", name)
```

- [ ] **Step 2: Update error messages**

Replace "instance" with "crag" in user-facing error messages throughout the file:
- `"instance name cannot be empty"` → `"crag name cannot be empty"`
- `"instance %q already exists"` → `"crag %q already exists"`
- `"instance %q not found"` → `"crag %q not found"`
- `"repo %q not found in instance"` → `"repo %q not found in crag"`
- etc.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/instance/... -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/instance/instance.go
git commit -m "rename: disk path ~/.belayer/instances/ -> ~/.belayer/crags/"
```

---

### Task 7: Remove TUI references from docs and code

**Files:**
- Delete: `docs/TUI.md`
- Modify: `CLAUDE.md`
- Modify: `docs/design-docs/index.md`

- [ ] **Step 1: Delete docs/TUI.md**

```bash
rm docs/TUI.md
```

- [ ] **Step 2: Remove TUI row from CLAUDE.md Documentation Map**

Remove the row:
```
| Frontend | `docs/TUI.md` | bubbletea components, state management |
```

- [ ] **Step 3: Update docs/design-docs/index.md**

Add a note that the TUI design doc is archived (the design was never implemented):
Change the TUI row to add `(archived — never implemented)` suffix.

- [ ] **Step 4: Commit**

```bash
git add -A docs/TUI.md CLAUDE.md docs/design-docs/index.md
git commit -m "docs: remove stale TUI references (code never existed)"
```

---

### Task 8: Update live docs

**Files:**
- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DESIGN.md`
- Modify: `docs/PLANS.md`

- [ ] **Step 1: Update README.md**

- Remove `belayer tui` from CLI reference table
- Remove `# Interactive TUI dashboard` and `belayer tui -i my-project` from monitor section
- Change all `-i` flags to `-c` in examples
- Update "Three layers" to remove "TUI dashboard" mention

- [ ] **Step 2: Update docs/ARCHITECTURE.md**

- Remove `User (CLI / TUI)` → `User (CLI)`
- Remove `tui` from CLI commands list in Code Map
- Remove the entire `| TUI | ...` row from Code Map
- Remove TUI Dashboard section (lines 137-147)
- Update `~/.belayer/instances/` → `~/.belayer/crags/` if still wrong
- Update `--instance` references

- [ ] **Step 3: Update docs/DESIGN.md**

- Remove TUI Dashboard section (lines 135-147)
- Remove `belayer tui` from CLI commands table
- Update `BELAYER_INSTANCE` → `BELAYER_CRAG`
- Update `--instance` → `--crag` references

- [ ] **Step 4: Update docs/PLANS.md**

- Remove TUI bugfix items (items 6, 10, 11 from the post-build list)
- Clean up the table formatting (the stray `|------|-----------|-------|` line)

- [ ] **Step 5: Commit**

```bash
git add README.md docs/ARCHITECTURE.md docs/DESIGN.md docs/PLANS.md
git commit -m "docs: update for --crag rename, remove TUI references"
```

---

### Task 9: Rename Belayer config struct fields

**Files:**
- Modify: `internal/belayer/belayer.go`
- Modify: `internal/cli/belayer_cmd.go`
- Modify: `internal/manage/prompt.go`
- Modify: `internal/cli/setter_cmd.go`

- [ ] **Step 1: Rename Config fields in belayer.go**

```go
type Config struct {
	CragName     string
	CragDir      string
	MaxLeads     int
	PollInterval time.Duration
	StaleTimeout time.Duration
}
```

Update all references within belayer.go:
- `s.config.InstanceName` → `s.config.CragName`
- `s.config.InstanceDir` → `s.config.CragDir`

- [ ] **Step 2: Update belayer_cmd.go Config construction**

In `newBelayerDaemonStartCmd` and `runBelayerDaemonForeground`:
```go
cfg := belayer.Config{
    CragName:     name,
    CragDir:      cragDir,
    ...
}
```

Also rename local vars: `instanceDir` → `cragDir` where used.

- [ ] **Step 3: Rename PromptData.InstanceName → CragName in manage/prompt.go**

```go
type PromptData struct {
    CragName  string
    RepoNames []string
}
```

- [ ] **Step 4: Update setter_cmd.go to use CragName**

Update the `manage.PromptData{InstanceName: name}` → `manage.PromptData{CragName: name}`.

- [ ] **Step 5: Update setter.md template**

Change `{{.InstanceName}}` → `{{.CragName}}` throughout.

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/belayer/belayer.go internal/cli/belayer_cmd.go internal/manage/prompt.go internal/cli/setter_cmd.go internal/defaults/claudemd/setter.md
git commit -m "rename: InstanceName/InstanceDir -> CragName/CragDir in daemon and manage"
```

---

### Task 10: Final test suite run

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 2: Build binary**

Run: `go build -o belayer ./cmd/belayer`
Expected: Compiles cleanly

- [ ] **Step 3: Smoke test CLI help**

Run: `./belayer belayer start --help`
Expected: Shows `--crag` / `-c` flag, no `--instance`

---

## Outcomes & Retrospective

**What worked:**
- Plan correctly identified all user-facing rename targets (CLI flags, env vars, config fields, DB columns)
- Backwards-compat `UnmarshalJSON` for config.json planned upfront — no user disruption
- Batching tasks 2-6+9 into a single worker was more efficient than sequential handoff
- TUI cleanup was clean — confident deletion because we verified the code never existed

**What didn't:**
- Plan missed ~40 internal struct field/param renames (`instanceConfigDir`, `instanceDir` in belayer.go, taskrunner.go, belayerconfig, logmgr) — caught in review
- Pre-existing doc error (`problems/` vs `tasks/` directory name) wasn't caught until reflect phase
- The `internal/instance/` package name remains as-is (correct decision, but the `instance.json` config file on disk is now confusing context)

**Learnings to codify:**
- When doing rename operations, grep for the old name in struct fields and function params, not just user-facing code
- Always verify doc claims against actual disk paths during reflect — docs can silently drift

# Merge Criteria Patterns

These are criteria for human reviewers deciding when to merge a goal's PR, not conditions that block the agent from coding. The agent writes all code upfront; these criteria inform when each PR is safe to merge to production.

---

## Criteria Types

### Synchronous Criteria

Things CI can check automatically — tests, lint, build. These can be encoded as required CI checks before a PR is approved.

| Criteria | How to Verify |
|------|--------------|
| Tests pass | `./gradlew test` / `npm test` / `pytest` — all green |
| Code compiles | Build succeeds with no type errors |
| Linter clean | No new lint warnings introduced |
| Integration tests pass | API contract tests, component tests, end-to-end smoke tests |
| Code review approved | PR reviewed and approved by at least one peer |
| No runtime exceptions in staging | Deploy to staging, run manual smoke test, check error logs |

Synchronous criteria can be encoded as CI checks that block merge.

### Async Criteria

Things that need time or observation — they require elapsed time, external events, or collected evidence. These create natural pause points in the migration — do not rush them.

| Criteria | How to Verify | Typical Duration |
|------|--------------|-----------------|
| Observation period clean | Zero new errors/regressions in monitoring for N hours | 24–72 hours |
| Backfill complete | Row counts match between old and new storage | Depends on data volume |
| Dual writes consistent | Spot checks show no divergence between old/new storage | 24–48 hours |
| Feature flag at target % | Flag management system shows rollout at intended percentage | Minutes to hours |
| Metrics stable | Error rate, latency, throughput within baseline bounds | 1–7 days |
| On-call team sign-off | Team confirms no incidents attributable to the migration goal | After observation |

Async criteria typically require a human decision about when to merge. Document the verification result in the PR before approving.

---

## Goal Transition Criteria

### Before merging Goal: New Interface

**Merge criteria:**
- New class/component/module has unit tests written (not just code)
- All new unit tests pass
- New code compiles and linter is clean
- Code review of new interface approved
- Interface design confirmed (method signatures won't need to change during redirection)

**Why this matters:** Redirecting consumers to a broken or unstable interface is worse than keeping everything in the monolith. Verify the new home is solid before moving callers.

### Before merging Goal: Consumer Redirection

**Merge criteria:**
- Zero remaining direct calls to old interface (verified via grep/search)
- Integration tests pass with consumers using new interface
- Staging deployment shows no regressions
- Old interface methods are still present but no longer called (do not delete yet)

**Why this matters:** If your refactor includes data migration goals downstream, any consumer still reading/writing via the old path will cause divergence once dual writes begin. Even without data migration, stale callers defeat the purpose of the extraction.

### Before merging Goal: New Data Source

**Merge criteria:**
- New table/slice/context created and migration applied (for DB: migration ran successfully in staging)
- New storage is empty (expected — no data yet)
- Read and write connectivity confirmed (e.g., test writes to new table succeed)
- No schema errors or constraint violations on empty inserts

**Why this matters:** Dual writes in the next goal require the destination to exist and be writable. Validate the target before starting to write to it.

### Before merging Goal: Dual Writes (async criteria)

**Merge criteria (async):**
- Dual writes have been active in production for at least 24 hours
- Spot check: sample N rows from old storage, verify corresponding rows exist in new storage with matching values
- No dual-write errors in logs (both writes must succeed; a failed new-write should not silently continue)
- On-call team has reviewed and signed off

**Why this matters:** Backfill relies on new writes being consistent. If dual writes have bugs (e.g., type coercion errors, missing fields), backfilled data will be wrong. Catch this early.

### Before merging Goal: Backfill

**Merge criteria:**
- Row counts match: `SELECT COUNT(*) FROM old_table` equals `SELECT COUNT(*) FROM new_table`
- Spot check: sample 100–1000 rows, verify field-by-field consistency
- Backfill job has run to completion with no errors
- Backfill is idempotent (re-running produces no changes)
- For frontend: old state keys have corresponding new state entries for all active sessions

**Why this matters:** Switching reads before backfill is complete means users with pre-existing data will see missing or stale data. Do not skip this merge criteria check.

### Before merging Goal: New Source Authoritative (async criteria)

**Merge criteria (async):**
- Feature flag (if used) at 100% for at least the observation period
- No errors or regressions in monitoring for the observation period (typically 24–72 hours minimum; 1 week for high-risk systems)
- All reads confirmed going to new source (verify via logging/tracing)
- Old source writes are no longer necessary (dual writes can be removed safely)
- Rollback plan documented and stakeholders aware that the cleanup goal is irreversible

**Why this matters:** The cleanup goal is the point of no return. Deleting old code and old storage cannot be undone without a full revert. Invest in thorough observation before merging.

---

## Observation Strategies

Observation periods are how you build confidence during async criteria. Use a layered approach:

### Logging

Add structured logs at key transition points:

```kotlin
// Log which path was taken (old vs. new)
logger.info("settings_read", mapOf("source" to "new_table", "shopId" to shopId))

// Log dual write results
logger.info("settings_dual_write", mapOf(
    "old_success" to oldWriteResult.isSuccess,
    "new_success" to newWriteResult.isSuccess,
    "shop_id" to shopId
))
```

Query logs for error rates: `source=new_table AND level=ERROR` should be zero.

### Metrics Dashboards

Track these metrics during observation:

| Metric | What to Watch |
|--------|--------------|
| Error rate | Should not increase vs. baseline |
| P50/P95/P99 latency | New storage reads should not be slower |
| Throughput | Requests per second should not change |
| Cache hit rate | If caching involved, should remain stable |
| DB connection pool | New table reads should not exhaust connections |

Set up alerts if any metric exceeds baseline by more than 5–10%.

### Feature Flag Rollout

For frontend extractions or gradual backend rollouts:

1. **1% canary** — Monitor for 1–4 hours. Any errors? Stop and investigate.
2. **10% rollout** — Monitor for 4–24 hours. Compare error rates between old and new cohorts.
3. **50% rollout** — Monitor for 24 hours. At this point both paths have similar traffic.
4. **100% rollout** — Begin formal observation period.
5. **Observation period** — Hold at 100% for agreed duration before merging the cleanup goal.

Flag targeting: Use user ID or shop ID for consistent assignment (not random per request).

### Alerting

Set up dedicated alerts for the migration:

```yaml
# Example alert (DataDog/Grafana/PagerDuty)
name: "SettingsService migration regression"
condition: error_rate{service=settings-service} > 0.1%
duration: 5 minutes
severity: critical
runbook: "Check dual writes and new table reads"
```

Alerts should fire on both new errors (from new code path) and missing events (if old path is still being called unexpectedly after switch reads).

### Spot Check Queries

Run manually during observation:

```sql
-- Verify row counts match after backfill
SELECT
    (SELECT COUNT(*) FROM shops WHERE settings_migrated = true) AS old_count,
    (SELECT COUNT(*) FROM shop_settings) AS new_count;

-- Verify no divergence in critical fields (sample 1000 rows)
SELECT s.id, s.theme AS old_theme, ss.theme AS new_theme
FROM shops s
JOIN shop_settings ss ON ss.shop_id = s.id
WHERE s.theme != ss.theme
LIMIT 100;
```

Zero rows in the divergence query is the merge criteria for the backfill goal.

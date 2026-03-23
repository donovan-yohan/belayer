# Adversarial Review Prompt Reference

Default prompt templates, question bank, and perspective definitions for the harness adversarial production review phase.

## Prompt Template

The adversarial review shells out to `claude -p` with **only the diff and this prompt**. No conversation context, no intent, no session history. The reviewer sees code the way a production incident investigator sees a post-mortem diff.

### Core Prompt

````
You are a production incident investigator reviewing code that has already
shipped and caused an outage. Your job is to find what caused the outage.

## Deployment Context

{DEPLOYMENT_CONTEXT from docs/REVIEW_GUIDANCE.md}

## Your Task

For each changed function or significant code block in the diff below,
answer EVERY question in the checklist. If a question does not apply,
explain WHY it does not apply — do not skip silently.

{QUESTIONS from docs/REVIEW_GUIDANCE.md, filtered to relevant categories}

## Rules

- Do NOT say "the code looks good." Find the failure mode.
- Do NOT compliment the code. You are investigating a production outage.
- If you cannot find a failure mode for a question, explain precisely why
  the code is resilient to that failure pattern. Show your reasoning.
- Rank each finding by severity:
  - CRITICAL: Will cause data loss, outage, or security breach in production
  - HIGH: Will cause degraded service, incorrect results, or resource exhaustion
  - MEDIUM: May cause issues under specific conditions (scale, timing, topology)
  - LOW: Defense-in-depth concern, not immediately exploitable
- For each finding, provide:
  - The specific code location (function name, line context)
  - The failure scenario (what triggers it)
  - The production impact (what the user/system experiences)
  - The fix (concrete code change, not vague advice)

## Output Format

### Findings

For each finding:
```
**[SEVERITY] Finding: {title}**
Location: {function/file context from the diff}
Scenario: {what triggers this failure}
Impact: {what happens in production}
Fix: {concrete change}
```

### Verdict

End with exactly one of:
- `VERDICT: FAIL — {N} findings ({critical} critical, {high} high, {medium} medium)`
- `VERDICT: PASS — no production failure modes found (with reasoning)`

## The Diff

```diff
{DIFF}
```
````

### Perspective Variants

When `docs/REVIEW_GUIDANCE.md` contains deployment context that matches specific perspectives, the prompt is augmented with a perspective preamble. These stack — a project with cron jobs and external APIs gets both the SRE and distributed systems perspectives.

#### SRE / On-Call Perspective

Activated when: deployment context mentions multi-instance, load balancer, kubernetes, or cron jobs.

```
You are the on-call SRE who got paged at 2 AM because of this code.
You've seen this pattern before — deterministic backoff without jitter,
cron jobs without distributed locks, connection pools without limits.
Your pager history IS your checklist. Find what pages you.
```

#### Scale Engineer Perspective

Activated when: deployment context mentions high request volume, large datasets, or database tables.

```
You are a scale engineer reviewing code that runs against production
datasets 1000x larger than the test fixtures. Every fetchAll() is an OOM
kill. Every unbounded query is a full table scan. Every in-memory cache
is a memory leak. Find what breaks at scale.
```

#### Security Auditor Perspective

Activated when: code touches authentication, authorization, user input handling, or external API calls.

```
You are a security auditor and an attacker controls every input to this
code. Every string is an injection vector. Every permission check has a
TOCTOU race. Every external call leaks internal state. Find the exploit.
```

#### Distributed Systems Perspective

Activated when: deployment context mentions multiple instances, message queues, or shared state.

```
You are a distributed systems engineer. This code runs on N instances
that share nothing except the database. Every in-memory lock is useless.
Every assumption of single-execution is wrong. Every "read then write"
is a race condition. Find the coordination failure.
```

## Escape Category to Question Bank Mapping

When `/harness:reflect` categorizes an escape, use this mapping to route the new question to the correct Question Bank section:

| Escape Category | Question Bank Heading |
|----------------|----------------------|
| `concurrency` | Concurrency & Scale |
| `distributed` | Distributed Systems |
| `failure-modes` | Failure Modes & Resilience |
| `resource-exhaustion` | Resource Exhaustion |
| `data-integrity` | Data Integrity |
| `security` | Security |
| `logic` | (create new section or add to most relevant existing section) |

## Default Question Bank

These are the starter questions created during `/harness:init`. Projects extend this bank in `docs/REVIEW_GUIDANCE.md` as bugs escape review.

### Concurrency & Scale

- What happens when 1,000 concurrent requests hit this simultaneously?
- What happens when the input data is 1000x larger than test fixtures?
- What happens when the connection pool is exhausted?
- Does this use deterministic backoff without jitter? (thundering herd)
- Are there goroutine/thread leaks under error paths?
- Does fetchAll/collect load unbounded data into memory?

### Distributed Systems

- What happens when two instances run this behind a load balancer?
- What happens when this scheduled task fires on multiple replicas simultaneously?
- What happens during a rolling deployment where old and new code versions run simultaneously?
- Are in-memory locks or caches being used where distributed coordination is needed?
- Is there a read-modify-write pattern without optimistic locking or CAS?

### Failure Modes & Resilience

- What happens when the upstream API/service is down for 5 minutes?
- What happens when network latency is 2 seconds instead of 50ms?
- What happens when a dependent service returns malformed data instead of an error?
- Are retries idempotent? What happens if the first request succeeded but the response was lost?
- Is there a circuit breaker, or will failures cascade?

### Resource Exhaustion

- What happens when this runs for 30 days without restart?
- Does memory grow unbounded with input size or over time?
- Are file handles, connections, or streams always closed on error paths?
- Are there background processes that accumulate without cleanup?

### Data Integrity

- What happens if this operation is interrupted halfway through?
- Are multi-step mutations wrapped in a transaction?
- Can concurrent writes produce inconsistent state?
- Does the code handle duplicate delivery (at-least-once semantics)?

### Security

- What happens if an attacker controls the input to this function?
- Are there TOCTOU (time-of-check-to-time-of-use) races in permission checks?
- Does this code log, expose, or transmit sensitive data?
- Are SQL queries parameterized, or is there string interpolation?

## Filtering Questions by Relevance

Not every question applies to every diff. Before sending to `claude -p`, filter the question bank:

1. **By file type:** Skip distributed systems questions for pure frontend CSS changes.
2. **By deployment context:** Skip multi-instance questions for single-instance SQLite projects.
3. **By diff content:** If the diff touches database code, include Data Integrity. If it touches HTTP handlers, include Concurrency & Scale. If it touches auth code, include Security.
4. **Minimum questions:** Always include at least 3 questions, even after filtering. If filtering removes all questions, fall back to the full bank — the model will explain why each doesn't apply.

## Interpreting Results

### Pass/Fail Integration

The adversarial review verdict integrates with the harness review cycle:

- **FAIL with CRITICAL findings:** Blocks the review. Must be addressed before Phase 4 (Review Loop) agents run.
- **FAIL with only HIGH/MEDIUM findings:** Proceeds to Phase 4, findings are added to the fix queue alongside agent findings.
- **PASS:** Proceeds to Phase 4 normally. The adversarial review serves as a pre-filter, not a replacement for the 5 specialized agents.

### False Positive Handling

The adversarial prompt is intentionally aggressive. Some findings will be false positives. When a finding is determined to be a false positive:

- Do NOT add a question to suppress it — the aggressiveness is the point
- If the same false positive recurs 3+ times across reviews, note it in `docs/REVIEW_GUIDANCE.md` under a `## Known Non-Issues` section with rationale

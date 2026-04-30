# Crag Filesystem Contract

This document defines the local filesystem contract for Belayer crags. A crag
is a persistent operating context: a software company, story world, research
group, or any other durable team-and-memory boundary.

## Scopes

Belayer crag mode uses three local scopes.

```text
repo/.belayer/
  Project-local runtime config, agent overrides, climb state, and explicit crag link.

~/.belayer/talent-catalog/<category>/
  Reusable local talent supply grouped by category.

~/.belayer/crags/<crag-name>/
  Cross-project crag knowledge: teams, SOPs, gates, evaluations, promotions,
  and optional story-world state.
```

Global crag state is opt-in. A repo only participates in a user-level crag when
its `.belayer/config.yaml` explicitly links one.

## Repo-Local `.belayer/`

Repo-local `.belayer/` remains the runtime override layer.

```text
repo/.belayer/
├── config.yaml
├── agents/
├── climbs/
└── worktrees/
```

The `agents/` directory wins over any linked crag or shipped default identity.
This preserves the existing rule: project-local identity files are project-owned
and should be auditable in that repo.

### Crag Link

The first CLI implementation should write the smallest useful link:

```yaml
crag:
  name: software-company
```

`crag.name` resolves to `~/.belayer/crags/software-company`. An optional explicit
path can support advanced setups and tests:

```yaml
crag:
  name: software-company
  path: /absolute/path/to/crag
```

If `crag.path` is present, it must be absolute or relative to the repository
root, and must point at a crag directory with a valid `crag.yaml`.

## User Team Catalog

The user catalog is local supply. It is not a running organization and it is not
project state.

```text
~/.belayer/talent-catalog/
├── development/
│   └── backend-dev/
│       ├── agent.yaml
│       ├── system-prompt.md
│       ├── agents.md
│       └── talent.yaml
└── story/
    └── storyteller/
        ├── agent.yaml
        ├── system-prompt.md
        ├── agents.md
        └── talent.yaml
```

`belayer team add` copies team identity directories into `repo/.belayer/agents/`.
`belayer team remove` removes copied identities from that project. Copying is
deliberate: the repo gets a reviewable snapshot of the identities it will run.

## User Crag Directory

A crag directory is cross-project knowledge for one operating mode or team.

```text
~/.belayer/crags/<crag-name>/
├── crag.yaml
├── teams/
├── sops/
├── gates/
├── evaluations/
├── promotions/
└── generated-talents/
```

Story-world crags may also include:

```text
~/.belayer/crags/<crag-name>/
└── world-state/
```

### `crag.yaml`

Minimal `crag.yaml`:

```yaml
schema_version: "belayer-crag/v1"
name: software-company
kind: development
description: "Default software-company crag for implementation climbs"
default_team: core
catalog_categories:
  - development
```

Fields:

| Field | Required | Meaning |
|-------|----------|---------|
| `schema_version` | yes | Must be `belayer-crag/v1` for this contract. |
| `name` | yes | Directory-safe crag identifier. Must match `<crag-name>`. |
| `kind` | yes | `development`, `story`, `research`, or `custom`. |
| `description` | no | Human summary for future crag-listing surfaces. |
| `default_team` | no | Team file under `teams/` to use by default. |
| `catalog_categories` | no | Catalog categories this crag expects to draw from. |

### `teams/`

Teams are reusable rosters. They reference talent names; they do not embed full
prompt content.

```text
teams/
└── core.yaml
```

Example:

```yaml
schema_version: "belayer-team/v1"
name: core
lead: supervisor
members:
  - name: backend-dev
    role: implementer
  - name: reviewer
    role: gate
default_gates:
  - code-review
  - runtime-qa
  - acceptance
```

### `sops/`

SOPs are reusable instructions promoted from retros or written by humans.

```text
sops/
├── pull-request.md
└── story-continuity.md
```

SOPs are not injected automatically by this contract. A future workflow
discovery layer should decide which SOPs a task needs.

### `gates/`

Gates are reusable review contracts.

```text
gates/
├── acceptance.yaml
├── code-review.yaml
└── continuity.yaml
```

Example:

```yaml
schema_version: "belayer-gate/v1"
name: acceptance
stage: session
authority: blocking
trigger: completion_requested
assigned_talent:
  - pm
requires:
  - org-plan
  - gate-result
verdicts:
  - pass
  - fail
  - blocked
```

### `evaluations/`

Evaluations preserve summarized talent performance across projects.

```text
evaluations/
└── backend-dev/
    └── 2026-04-30-session-<id>.json
```

Files should use the `talent-evaluation` artifact shape where possible. They
are records, not automatic prompt mutations.

### `promotions/`

Promotion records preserve explicit changes made from climb evidence into crag
knowledge.

```text
promotions/
├── accepted/
└── rejected/
```

A promotion record should include source session, source artifacts, operator or
reviewer, timestamp, target path, and the applied or rejected change. Rejected
proposals stay auditable but do not alter crag state.

### `generated-talents/`

Generated talents are short-lived workers or NPCs that were created during a
climb and then selected for possible reuse.

```text
generated-talents/
└── tavernkeep-mara/
    ├── talent.yaml
    └── notes.md
```

Minimal generated talent metadata:

```yaml
schema_version: "belayer-generated-talent/v1"
name: tavernkeep-mara
category: story
status: candidate
origin:
  session_id: "<session-id>"
  first_artifact: "artifacts/stories/tavern-celebration.md"
role: "tavernkeep"
summary: "Warm, watchful innkeeper who remembers debts and rumors"
reuse_policy: scene-local # options: scene-local | recurring | promoted
```

Generated talents do not become full `.belayer/agents/` identities by default.
They start as compact metadata. A later reviewed promotion can turn one into a
catalog talent or crag team member.

### `world-state/`

Story crags may keep campaign or setting state here.

```text
world-state/
├── current.json
├── characters/
└── locations/
```

For the first proof, story-world state may remain repo-local and be copied here
only as an explicit evaluation artifact. The persistence decision should be made
after #115 shows whether campaign-global or repo-linked state is more useful.

## Precedence

When resolving an identity or crag asset, use this order:

1. Repo-local override in `repo/.belayer/`.
2. Linked crag default under `~/.belayer/crags/<crag-name>/`.
3. User talent catalog under `~/.belayer/talent-catalog/<category>/`.
4. Shipped Belayer defaults embedded in the binary.

This mirrors existing agent resolution: local project intent wins over shipped
defaults.

## Privacy And Safety

Cross-project knowledge is opt-in per repo. A normal climb must not silently
mutate:

- `~/.belayer/crags/<crag-name>/`
- `~/.belayer/talent-catalog/<category>/`
- another repo's `.belayer/`

Climbs produce artifacts and promotion proposals. Explicit CLI commands or human
review apply those proposals to global crag knowledge.

## Out Of Scope

- Hosted talent marketplace.
- Runtime gate enforcement.
- Daemon database tables for crag state.
- Automatic prompt mutation.
- Loading every talent as a running process.

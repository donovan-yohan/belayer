# Relay IDE Software-Company Proof

Test bed: `/Users/donovanyohan/Documents/Programs/personal/relay-ide`

Selected PRD: https://github.com/donovan-yohan/relay-ide/issues/320

This packet models a software-company crag climb for issue #320. It does not alter
relay-ide directly in this Belayer PR. That was deliberate: the goal for #115 is
to verify the crag-mode artifact shape and review criteria before handing the
task to a real implementation run.

## Files

- `org-plan.json` - decomposition and gate plan.
- `gate-result-code-review.json` - code-review gate expectation.
- `gate-result-runtime-qa.json` - runtime QA gate expectation.
- `talent-evaluation-backend-dev.json` - per-climb team growth sample.
- `org-retro.json` - crag-level retro and follow-up recommendations.

## Handoff Decision

Issue #320 is a good first real software-company run because it is small, has
clear acceptance criteria, and touches a bounded module. A future Belayer run
should implement it in relay-ide and attach the real PR/check URLs to these
same artifact kinds.

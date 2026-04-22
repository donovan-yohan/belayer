# Default Agent Team

These files are a starter team, not the belayer framework contract.

`belayer init` copies this `agents/` tree into your project at
`.belayer/agents/`. After that, the project-local copy is yours:

- edit it
- delete identities you do not want
- rename identities
- add new identities
- replace the whole team

Belayer itself does not require `web-dev`, `backend-dev`, `qa`, or
`reviewer`. Those are shipped defaults to help a project get started.

The important distinction is:

- `agents/` in this repo: shipped starter templates for `belayer init`
- `.belayer/agents/` in your repo: your runtime team definition

If you are consuming belayer from another project, customize
`.belayer/agents/` there. Do not treat this repo's shipped defaults as
the source of truth for your project unless you deliberately want to
inherit them.

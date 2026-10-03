# Live smoke test for `projects`

`scripts/live-smoke.sh` runs the `projects` CLI against real GitHub, in a
disposable sandbox repository and user Project, and checks each outcome twice:
through the CLI's own `--json` result and through an independent `gh` read.
It complements the offline Go and shell tests, which use fakes and never
mutate GitHub. Use it before a release, or after a change to the GitHub
transport, to catch behaviour that only the live API shows: search-index lag,
built-in Project workflows, archived items and option-name casing.

## No stored token

The script runs locally with the operator's existing `gh` login. It does not
create, read, print or store a token, and it is deliberately not part of CI. A
CI version would need a classic token with the `project` scope, and that scope
reaches every Project the owning user can access, which is too broad to keep in
a repository secret. The safe future option is a dedicated sandbox
organisation with a fine-grained token limited to that organisation.

## Safety

- The target repository name must end in `-sandbox`; anything else is refused
  before any GitHub call.
- The checkout's `.projects/project.md` must name exactly the target repository
  and Project, because the CLI mutates whatever the contract names.
- Every created issue has `smoke <run-id>` in its title and the `smoke` label.
  On exit, including failure or interruption, the script closes this run's open
  issues as not planned and archives their Project items. `--keep` skips that
  clean-up for debugging.
- The script creates the `smoke` and `smoke-edit` labels and a `smoke`
  milestone in the sandbox if they are missing.

## Sandbox setup (one-time)

The sandbox needs a private repository whose name ends in `-sandbox`, a user
Project linked to it, a single-Project contract pointing at both, and the
standard field profile and `Backlog` view:

```bash
gh project link NUMBER --owner OWNER --repo OWNER/NAME-sandbox
projects project setup-fields --apply
projects project setup-backlog-view --apply
```

Leave GitHub's default built-in Project workflows enabled (*Item added to
project*, *Item closed*, *Auto-close issue*); the test exercises them.

## Run

```bash
scripts/live-smoke.sh               # installed `projects` on PATH
PROJECTS_BIN=/path/to/projects scripts/live-smoke.sh
scripts/live-smoke.sh --build       # go build this checkout into a temp dir
```

| Variable | Default |
| --- | --- |
| `SMOKE_REPO` | required |
| `SMOKE_PROJECT_OWNER` | required |
| `SMOKE_PROJECT_NUMBER` | required |
| `SMOKE_ROOT` | a temporary clone of `SMOKE_REPO` |
| `SMOKE_SETTLE` | `8` seconds before re-reading workflow-affected state |

`SMOKE_REPO`, `SMOKE_PROJECT_OWNER` and `SMOKE_PROJECT_NUMBER` have no
defaults: set them in the environment, or in a config file. Values already set
in the environment win over the file. The default config path is
`${XDG_CONFIG_HOME:-$HOME/.config}/projects/live-smoke.env`, overridden by
`SMOKE_CONFIG`. The file is read line by line and only `KEY=VALUE` lines for the
settings above are used; blank lines and lines starting with `#` are ignored.

```bash
# ~/.config/projects/live-smoke.env
SMOKE_REPO=OWNER/projects-cli-sandbox
SMOKE_PROJECT_OWNER=OWNER
SMOKE_PROJECT_NUMBER=1
```

Each step prints PASS, FAIL or SKIP (an earlier step it depends on failed) with
its duration. A failure prints the CLI command, exit status, stdout and stderr.
The run ends with a summary and total wall time, and exits non-zero if any
step did not pass. A full run takes about four minutes.

## Coverage

- `contract validate`; `setup-fields` and `setup-backlog-view` plans show no
  changes on the configured sandbox.
- `issue create` with an empty body and with a multiline body, including
  `duplicateCheck` in the JSON.
- An immediate exact-title re-run is refused while the search index still lags,
  and only one issue with that title exists.
- `issue create` with a Status spelling whose case differs from the live
  option, Priority, Class and `@me`, then a delayed re-read showing the
  *Item added to project* workflow did not overwrite Status.
- `issue create` with Project fields but no Status: the Status set by the
  workflow does not fail the command; any `automationSideEffects` and the
  independently read Status are printed.
- `issue edit`: title, add and remove label, set and clear milestone; close as
  `not_planned` with and without a Project item (the *Item closed* workflow's
  change is an `automationSideEffects` entry); change the reason on an already
  closed issue; reopen.
- `project item-add`: new membership, idempotent re-runs, and restoring an item
  archived through `archiveProjectV2Item`.
- `project item-edit`: Priority, Status and Target date together, then Priority
  and Status with `--clear "Target date"`.

# `projects` CLI

`projects` handles the repeated, mechanical parts of GitHub Project
administration. The repository's `.projects/` contract still decides which
repository and Project are in scope. The `github-projects` skill still
interprets a user's request and applies its safety rules.

The CLI is optional. Keep using the repository scripts or direct `gh` and API
operations when it is not installed or does not support the requested change.

## Install on Ubuntu or Debian

Miguel's signed APT repository is already hosted on GitHub Pages. These are
one-time, human-run setup commands:

```bash
sudo apt-get install -y curl gpg
curl -fsSL https://miguelrodo.github.io/apt-miguelrodo/KEY.gpg \
  | sudo gpg --dearmor --yes -o /usr/share/keyrings/apt-miguelrodo.gpg
echo "deb [signed-by=/usr/share/keyrings/apt-miguelrodo.gpg] https://miguelrodo.github.io/apt-miguelrodo stable main" \
  | sudo tee /etc/apt/sources.list.d/apt-miguelrodo.list >/dev/null
sudo apt-get update
sudo apt-get install -y projects
```

`apt-get update` does not install anything, so it does not need `-y`.

To upgrade later:

```bash
sudo apt-get update
sudo apt-get install --only-upgrade -y projects
```

Agents should not run these `sudo` commands merely to check a version. Use one
of the read-only checks instead:

```bash
projects update check
apt-cache policy projects
```

Tagged releases also contain checksummed Linux, macOS and Windows archives.
If Go is already installed, the current source can be installed with:

```bash
go install github.com/MiguelRodo/github-projects-skill/cmd/projects@latest
```

Maintainer setup and version-bump commands are in the
[`projects` release guide](releasing.md).

## Help

```bash
projects --help
projects project --help        # or: projects help project
projects project item-edit --help
```

Requested help is written to stdout and exits 0. A usage error repeats the
usage of the command or group that was being run.

## Onboard a managed Project

Choose Project/sub-project intent before choosing an execution repository:

```bash
pj --init --project work --issue-store example/issues --subproject tools --project-owner example --project-number 40
pj -i --project work --subproject tools
```

The canonical skill's initializer reconciles both local checkouts and derives
their routing labels. Omit `--issue-store` for natural repository issues. The
central checkout must already exist in the managed workspace. Cross-checkout
changes require preview confirmation (or `--yes`) and remain local for normal
branch/PR review. Read [semantic onboarding](../skills/github-projects/references/onboarding.md)
for the standalone script, conflict rules and live Project setup. `projects`
remains optional and does not own a second onboarding contract.

## Choose the repository

Every command that reads the contract accepts `--root DIRECTORY`. Without it,
`projects` starts in the current directory and walks up to the nearest
directory containing `.projects/project.md`, so it works from any subdirectory
of a repository. The walk stops at the first directory containing `.git`: a
repository without its own contract never borrows the contract of an enclosing
workspace or parent repository. Outside any Git repository the walk can reach
the filesystem root. When nothing is found, the command fails and asks you to
run it from the repository or pass `--root`.

## Validate a repository contract

Run the command anywhere inside the repository:

```bash
projects contract validate
```

Or name another checkout without changing directory:

```bash
projects contract validate --root /path/to/repository
```

This validates the complete single-Project contract or dispatcher, including
its child contracts. It does not contact GitHub or change files. The existing
shell validator remains available:

```bash
bash .agents/skills/github-projects/scripts/validate-contract.sh .
```

Use `--json` when another program needs the resolved contract summary. Progress
is written to stderr, while JSON is written to stdout. `--quiet` hides progress.

## Read every Project item

For a single-Project repository:

```bash
projects project item-list --format json
```

The shorter `--json` flag is equivalent, and `projects project items` is an
alias for `item-list`. Human-readable table output is the default:

```bash
projects project item-list
```

The command checks the local contract, reads the live Project identity, asks
GitHub CLI for a deliberately large item set, then compares the returned items
with GitHub's reported total. It fails instead of describing a partial page as
the whole Project.

A dispatcher needs an exact route selector. Any identifiers supplied together
must agree:

```bash
projects project item-list --project-key personal --json
projects project item-list --routing-label project:personal --json
projects project item-list --project-number 40 --json
```

GitHub commands send HTTP requests directly to `api.github.com`. Authentication
uses `GH_TOKEN`, then `GITHUB_TOKEN`. If neither is set, `gh` is required only
to obtain a token once with `gh auth token --hostname github.com`. Authenticate
that fallback account and check it with:

```bash
gh auth status
```

## Create and edit issues

Mutating commands plan by default and require `--apply` to execute. Plans may
contact GitHub to inspect exact-title collisions, live schema, membership and
current values, but never write. Apply mode performs fresh inspection and
independently verifies changes through separate readback.

`issue edit` and `project item-edit` plans show each requested value as
`current → new` (for example `Title: "Old" → "New"`), and mark a value that is
already in the requested state as a no-op (`(already set)`, `(already present)`,
`(already absent)`, `(already none)` or `(already clear)`). A plan in which
nothing would change ends with `No change needed.`, and the same change lines
appear in the plan's `changes` JSON array.

Create an issue:

```bash
projects issue create --title "New issue title" --body "Issue description"
projects issue create --title "New issue title" --body "Issue description" --apply
```

Creation checks the repository for an issue with exactly the same title
(case-sensitive, after trimming outer whitespace; pull requests are ignored)
and stops rather than creating a likely duplicate. The check reads titles only,
never issue bodies, and usually costs two read calls: a title-phrase search
across open and closed issues, filtered to exact matches, plus one titles-only
page of the newest open issues created in the last 7 days to cover
search-index lag. Whenever search cannot be trusted to be complete, for
example on incomplete or over-capped results, a search error or rate limit, a
title too long for search or one containing quotes, backslashes, control
characters or no letters or digits, it instead checks only open issues created
in the last 7 days, newest first, reading at most 500 (five calls). Closed
issues and older issues are then not checked, and the plan and result say so:
JSON reports
`"duplicateCheck": {"method": "recent-open", "recentWindow": "7d", "complete": true, "unchecked": "..."}`
(`complete` is false only when the 500-issue cap was reached inside the
window) and text output adds a one-line note. An exact duplicate that is
closed or older than 7 days is therefore caught only when search works. If two
distinct issues really must have the same title, make that choice visible with
`--allow-duplicate`; the explicit override also skips the duplicate check.
An explicit `--repo` is an assertion and must agree with the contract; it is
not an escape hatch to mutate another repository.

Optionally specify labels, assignees, milestone, or initial Project fields:

```bash
projects issue create \
  --title "Add authentication preflight" \
  --label bug \
  --assignee monalisa \
  --priority P1 \
  --class Task \
  --status "In progress" \
  --apply
```

`@me` in `--assignee`, `--add-assignee` or `--remove-assignee` is resolved
once to the authenticated login before planning, so plans and readback show
the real login. When these Project fields are requested, the plan itself
validates them against the live Project (for example, a `--status` option the
Project does not define fails the plan, not only `--apply`). If anything fails
after GitHub has created the issue, the error names the created issue's URL
and says not to retry creation.

Edit an existing issue:

```bash
projects issue edit --issue 42 --title "Updated title" --add-label enhancement
projects issue edit --issue 42 --title "Updated title" --add-label enhancement --apply
projects issue edit --issue 42 --state closed --close-reason completed --apply
projects issue edit --issue 42 --state closed --close-reason not_planned --apply
```

`--close-reason` accepts `completed` or `not_planned`. Closing an open issue
without a reason closes it as completed. On an issue that is already closed, a
different requested reason is applied and verified; without `--close-reason`
the existing reason is kept.

Requested single-select values, such as `--status "In progress"`, are matched
case-insensitively and verified against the Project's exact option spelling.

Project built-in workflows can change Status while a command runs, for example
*Item added to project* setting `Todo` on a newly added item, or *Item closed*
setting `Done` when an issue is closed. These changes are reported as
`automationSideEffects` (or `Observed automation:` lines) rather than failing
the command. On an item this command just added, an unrequested Status is
always reported from the final item, even when the workflow set it before the
post-add read. Issue or pull request state changes made by Project automation
during the command (such as *Auto-close issue* when Status is set to `Done`)
are reported too, on a best-effort basis because the workflow may run after the
final readback. Any other unrequested Project change still stops it.

## Manage Project items and fields

Add an issue to a declared Project:

```bash
projects project item-add --issue 42
projects project item-add --issue 42 --apply
```

For a dispatcher contract, provide an exact selector:

```bash
projects project item-add --project-key work --issue 42 --apply
```

The plan queries the one issue or pull request for its Project memberships. It
does not download every item in the Project. Apply is idempotent: an existing
membership is reported as a verified no-op, while a new membership is read back
independently and its item ID must agree with the mutation result. Use `--url`
instead of `--issue` for a pull request. The URL repository must agree with the
contract, and `--url` and `--issue` are mutually exclusive.

Edit Project item fields with verified readback:

```bash
projects project item-edit --issue 42 --priority P1 --status "In progress"
projects project item-edit --issue 42 --priority P1 --status "In progress" --apply
```

`item-edit` never adds membership implicitly. Run `project item-add` first when
membership itself is authorised. Each Project field update uses its declared
provider field. One bounded final query checks every requested value and
compares the other scalar Project fields with the pre-write snapshot. A
multi-field edit therefore does not download the Project or repeat the
readback after each field.

Within one invocation, issue creation reuses the definitions checked before
creation when it adds and configures the new item. Definitions are never cached
between commands. Organisation field changes share one definition lookup, one
value read and one batch update, followed by an independent readback. An
unchanged issue or organisation field needs no write or second read.

Priority is mapped through the contract's declared Priority mapping. If the
contract declares `Priority mapping status: pending`, the command refuses
Priority updates until mapped. Project-native single-select fields and dates
are supported. Contract-declared organisation issue types and single-select
organisation issue fields are also supported for issues, with their own fresh
definition lookup and preservation-checked readback. Operations that change
only those issue fields do not make an additional Project-field readback.
Pull requests cannot use those issue-only locations.

Clear a field with `--clear`:

```bash
projects project item-edit --issue 42 --clear "Target date" --apply
```

Clearing is deliberately limited to fields declared by the contract at a
`project field` location. Dates must be real calendar dates in exact
`YYYY-MM-DD` form, and declared Status mappings reject unknown values.

Multi-field updates use narrow provider operations rather than a collection
replacement. GitHub does not make those operations atomic; if a later field
fails, the command says that an earlier narrow change may have applied and
requires inspection before retrying.

## Set up Project fields and the Backlog view

These one-time commands also plan by default and need `--apply`. A dispatcher
needs the same exact selector as the other Project commands.

| Command | Purpose | Details |
| --- | --- | --- |
| `projects project setup-fields` | Create or reconcile the shared Class, Priority and date fields. Organisation-wide Issue Type or Priority schema changes also need `--allow-organization-schema` with `--apply`. | [Standard Project fields](standard-project-fields.md) |
| `projects project setup-backlog-view` | Create or reconcile the shared `Backlog` table view after the fields exist. | [Standard Backlog view](standard-backlog-view.md) |

## Version and update checks

```bash
projects version
projects --version             # or -v
projects version --json
projects update check
projects update check --json
```

`projects update check` only reads the latest github.com Release, whatever
`GH_HOST` says. It does not run a package manager, install a binary or require
`sudo`. Release packages carry their version. A `go install ...@vX.Y.Z` build
reports that module version; a local or pseudo-version build reports itself as
a development build and is not compared.

## Output and failures

Normal commands print a small number of numbered stages to stderr so a person
or agent can see where work has reached. Command data goes to stdout. This keeps
JSON safe to pipe into another tool:

```bash
projects project item-list --json >project-items.json
```

### JSON results

The mutating commands report their outcome with three keys that are independent
of the payload:

- `action` names the operation (`create_issue`, `edit_issue`,
  `project_item_add` or `project_item_edit`).
- `applied` is `true` exactly when the command ran with `--apply`; a plan
  reports `applied: false`.
- `changed` is `true` only when GitHub state was actually modified by this run.
  An `--apply` run that verified nothing needed doing reports `applied: true`
  with `changed: false`, and its text output says `No change needed`. A plan
  omits `changed` for `issue edit` and `project item-edit`, which cannot know
  whether applying would change anything; `project item-add`'s plan sets it to
  `wouldAdd || wouldUnarchive`.

`project item-edit --apply --json` nests its verified result under
`projectItem` (matching `issue create`) beside `action`, `applied` and
`changed`. `project item-add`'s `applied` always means "ran with `--apply`";
use `changed` to tell whether it added or unarchived the item.
Plans of all four commands report `applied: false`; earlier versions used the key `apply` for this.

Usage errors exit with status 2. These include a Project selector that names
no configured route, a selector used with a single-Project contract, and a
dispatcher command without a selector; such failures list the configured
routes. Validation, GitHub and completeness failures exit with status 1. A
failed command names the stage that failed and includes the underlying GitHub API
message followed by the HTTP method and path or the first line of the GraphQL
operation. Full queries and issue bodies are never repeated.

An error during readback does not mean the preceding write failed. For example,
a GitHub Project workflow can move an item to Done when its issue closes. The
issue edit's strict preservation check reports that changed Project summary
even when the closure succeeded. Inspect the issue state, Project membership
and affected fields independently before deciding what remains; do not repeat
the write just because verification stopped.

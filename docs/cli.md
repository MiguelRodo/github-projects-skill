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

## Validate a repository contract

Run the command in the repository root:

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

The shorter `--json` flag is equivalent. Human-readable table output is the
default:

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

GitHub commands use the current authenticated `gh` account. Check it with:

```bash
gh auth status
```

## Create and edit issues

Mutating commands plan by default and require `--apply` to execute. Plans may
contact GitHub to inspect exact-title collisions, live schema, membership and
current values, but never write. Apply mode performs fresh inspection and
independently verifies changes through separate readback.

Create an issue:

```bash
projects issue create --title "New issue title" --body "Issue description"
projects issue create --title "New issue title" --body "Issue description" --apply
```

Creation scans the complete issue repository for an exact title match and
stops rather than creating a likely duplicate. If two distinct issues really
must have the same title, make that choice visible with `--allow-duplicate`;
the explicit override also skips the otherwise unnecessary repository scan.
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

Edit an existing issue:

```bash
projects issue edit --issue 42 --title "Updated title" --add-label enhancement
projects issue edit --issue 42 --title "Updated title" --add-label enhancement --apply
projects issue edit --issue 42 --state closed --close-reason completed --apply
```

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

## Version and update checks

```bash
projects version
projects version --json
projects update check
projects update check --json
```

`projects update check` only reads the latest GitHub Release. It does not run a
package manager, install a binary or require `sudo`.

## Output and failures

Normal commands print a small number of numbered stages to stderr so a person
or agent can see where work has reached. Command data goes to stdout. This keeps
JSON safe to pipe into another tool:

```bash
projects project item-list --json >project-items.json
```

Usage errors exit with status 2. Validation, GitHub and completeness failures
exit with status 1. A failed command names the stage that failed and includes
the underlying `gh` error when one exists.

An error during readback does not mean the preceding write failed. For example,
a GitHub Project workflow can move an item to Done when its issue closes. The
issue edit's strict preservation check reports that changed Project summary
even when the closure succeeded. Inspect the issue state, Project membership
and affected fields independently before deciding what remains; do not repeat
the write just because verification stopped.

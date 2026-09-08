# v1 architecture boundaries

Status: **current post-dogfood architecture**

The public product is the `github-projects` skill plus a small
repository-local `.projects/` contract. GitHub issues and Projects are the live
authority. The optional `projects` binary handles operations that are safer and
more reliable when they are implemented once.

## Product decision

The CLI is named `projects`. There is no `projectctl` or `projectscli`
compatibility alias, and the discarded multi-repository clone, pull, status and
command runner does not return.

Agents can still use direct provider tools or `gh`. A collaborator does not
need the binary to work in a repository. The CLI is preferred only for a
command it actually supports.

## Responsibilities

| Surface | Responsibility |
| --- | --- |
| `skills/github-projects/` | Interpret short requests; enforce inspection, authority, preservation, stale checks, narrow writes and readback. |
| Repository `.projects/` contract | Declare the exact issue repository, Project routing, field locations, mappings and local governance. |
| `projects` CLI | Validate and resolve the existing contract, then perform supported deterministic reads or writes. |
| Direct provider or `gh` adapter | Perform the same work when `projects` is unavailable or does not yet support the operation. |
| `MiguelRodo/projects` | Coordinate creation of a complete project across GitHub, Drive, the registry and a manual ChatGPT Project handoff. |
| `pj` | Start the chosen local agent in the managed workspace. It is a launcher, not an administration client. |

The skill and contract own meaning. The CLI does not infer a second routing
model or priority vocabulary. Project bootstrap may call the CLI for supported
GitHub work, but Drive and registry behaviour stays outside this repository.

## Current CLI path

The first released read path is deliberately small:

1. parse and validate the complete Markdown contract;
2. resolve one Project, requiring an exact selector for a dispatcher;
3. read the live Project identity and compare it with the contract;
4. request a generous Project item limit;
5. compare the number of returned items with GitHub's reported total;
6. emit compact text or stable JSON, or fail without claiming completeness.

This path addresses the pagination failure recorded in issue #133. Contract
validation does not need GitHub access. Live reads use the operator's current
authenticated `gh` session.

Command data is written to stdout. A few numbered progress messages go to
stderr, so JSON can be piped safely while a user or agent can still see where a
slow command has reached. `--quiet` suppresses progress.

Mutating commands plan by default and require `--apply` to execute. Their
bounded live reads resolve exact-title issue collisions, contract-declared
field locations, exact Project membership and current values. Membership and
field inspection start from the one target issue or pull request, so mutation
commands do not serialise the whole Project. Apply mode re-reads
stale-sensitive state, changes only the owned value, and verifies it separately.
A successful provider response alone is never enough.

The CLI packages sequences that have repeatedly been error-prone when assembled
ad hoc: count-checked Project inventories, target-centred membership
add/readback, exact field and option resolution, organisation issue-field
POST/readback, and preservation checks.
It does not wrap every `gh` command. Unsupported locations or mutations fail
explicitly and remain the direct adapter's responsibility.

Project membership is a separate mutation. `project item-edit` refuses a
missing item; `project item-add` is the explicit idempotent membership command.
Issue creation may add membership because the same creation request explicitly
selected a Project. Multi-field writes are sequential because GitHub offers no
atomic operation across these surfaces, so failures identify possible partial
application and require a fresh inspection before retrying.

## Existing scripts

The Bash scripts remain supported. They are the installation-free path for a
repository that has the skill but not the binary, and they contain established
behaviour that should not be lost during a port.

- `validate-contract.sh` validates single and dispatcher contracts, including
  child-route consistency and safe pending Priority state.
- `setup.sh` checks tools, authentication and declared identities. It can run a
  repository-local extension or deliberate replacement.
- `init-project.sh` discovers facts, writes repository configuration only,
  preserves existing routes and validates temporary output before replacement.

Go replacements need parity tests against the existing fixtures before a shell
entry point is retired. No script is removed merely because an equivalent Go
command exists.

## Authority and identity

The current user request supplies mutation authority. The contract limits what
that request can mean in one repository; live GitHub supplies current identity,
membership and field state.

All supplied identifiers must agree. A dispatcher needs an exact Project key,
routing label, Project number or another identifier already resolved by the
skill. A display title or first search result is not enough.

Operator authority, provider permission and apply selection remain separate.
The CLI must stop on an ambiguous target, missing permission, stale state or
failed readback.

## Privacy boundary

Shared files contain collaborator-safe configuration only. Credentials, private
source locations, personal workspace paths and private destination identifiers
stay outside the repository.

The CLI may report a local contract path to its caller, but it must not copy
that path or other operator data into a public issue, fixture or release. Tests
use synthetic repositories and fake command runners. They do not mutate live
GitHub resources.

## Current package map

| Package | Responsibility |
| --- | --- |
| `internal/contract` | Parse, validate and resolve the active Markdown contract. |
| `internal/githubcli` | Invoke `gh` without a shell, verify Project identity, provide complete inventories, and use bounded target reads for mutations. |
| `internal/cli` | Parse commands, keep stdout and stderr separate, render text or JSON, and map failures to exit codes. |
| `internal/update` | Perform the read-only latest-release comparison. |
| `internal/buildinfo` | Hold version, commit and build date supplied by GoReleaser. |
| `cmd/projects` | Minimal executable entry point. |

Keep these packages private until another real consumer needs a stable Go API.
Do not expose a root re-export package in anticipation of one.

## Older specification files

Some files under `docs/spec/`, `schemas/` and `examples/` describe the discarded
pre-dogfood `projectctl/v1` design. They are historical evidence, not the active
Markdown contract and not an interface implemented by `projects`. Reintroduce a
piece only when a bounded issue connects it to observed use.

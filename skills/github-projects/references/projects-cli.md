# Execute supported operations with `projects`

Use this reference when the shell has `projects` installed. The installed
binary's help is authoritative for its capabilities: check `projects --help`
once, then `projects <command> --help` for the operation and flags you need.
An older binary may support reads but lack mutations. Use the direct provider
fallback only for the missing operation; continue using its supported commands.

Run from the resolved repository root, or pass `--root REPOSITORY_ROOT`.
For Project commands in a dispatcher, pass the exact `--project-key`,
`--routing-label` or `--project-number` from the checked contract. All supplied
identifiers must agree. Do not install or upgrade the binary merely to execute
an ordinary request.

## Choose the command from the requested outcome

| Outcome | Command |
| --- | --- |
| Validate the complete repository contract | `projects contract validate` |
| Read the complete Project item set, including for ranking | `projects project item-list --json` |
| Create an issue | `projects issue create --title TITLE --body-file FILE` |
| Change an issue title, body, labels, assignees, milestone or state | `projects issue edit --issue NUMBER` with the requested edit flags |
| Add an issue to the declared Project | `projects project item-add --issue NUMBER` |
| Set Priority, Class, Status or Target date | `projects project item-edit --issue NUMBER` with the requested field flags |
| Clear a declared Project field | `projects project item-edit --issue NUMBER --clear FIELD` |

Every mutation above plans by default. Add `--apply` when the user's request
authorises the change. A plan is not completion. Do not ask for a second
approval merely because the CLI distinguishes planning from applying; honour
an explicit proposal-only request by omitting `--apply`.

For example, after resolving the contract, an authorised request to set issue
313 to P2 uses:

```bash
projects project item-edit --issue 313 --priority P2 --apply --json
```

Pass the common value P2: the CLI translates it through the contract's mapping.
This also covers a contract-declared organisation-native Priority field when
the installed version supports it. A request to change two supported fields
uses one invocation:

```bash
projects project item-edit --issue 313 --priority P2 --status "In progress" --apply --json
```

`item-edit` requires existing Project membership. Add membership separately
only when it is authorised or necessarily implied and permitted by the
contract. `item-add` verifies an existing membership as a no-op. For pull
requests, the Project commands accept `--url` instead of `--issue`; issue-only
field locations do not apply to pull requests.

Use additive issue flags and a body file for multiline prose:

```bash
projects issue edit --issue 313 --add-label needs-review --apply --json
projects issue edit --issue 313 --body-file /path/to/body.md --apply --json
projects issue edit --issue 313 --state closed --close-reason completed --apply --json
```

For creation, supply only grounded text and authorised metadata. The CLI checks
exact-title collisions. Do not use `--allow-duplicate` to bypass an unresolved
collision.

## Inspection and verification

The CLI validates the contract and inspects the live identity, schema,
membership and values needed by its operation. Apply mode reads current state,
performs narrow writes and separate independent readback, including preservation
checks. Use that verified result as evidence; do not duplicate the same
preflight or verification with handwritten REST or GraphQL. Inspect additional
state only when the requested outcome needs it, such as hierarchy before
closing a parent, or issue text before rewriting it.

A CLI plan is not a locked snapshot or a conditional write. Before replacing
collaborative text or acting on an earlier interpretation, compare the current
content with the version you interpreted and stop if it changed. A fresh read
alone does not establish that a previously reviewed version is unchanged.

Complete Project reads belong to `project item-list`. Do not recreate them
with `gh project item-list`, default pages, pagination scripts or GraphQL when
the command is available. Organisation-native issue fields are not included
in the Project item inventory; fetch those separately when needed for a ranking.
There is no `projects issue view` command: a targeted `gh issue view` remains
appropriate for issue details that the CLI does not expose.

## When to fall back or stop

Use direct `gh`, REST, GraphQL or a capable connector when the binary is absent,
its help does not expose the necessary command/flag, or the operation is outside
its supported surface. This includes native parent/sub-issue relationships,
Project field definitions/options, and removing membership. Read
[the direct recipes](github-operations.md), retain all inspection and readback
rules, and report the concrete capability gap briefly.

An operational failure is different: authentication, access, contract validation,
ambiguous targets, stale state, incomplete reads and failed readback must stop
the operation. If a multi-field apply fails after an earlier field was written,
inspect what applied before any retry. Do not switch to another mutation
endpoint to work around a failed safety check or an uncertain write.

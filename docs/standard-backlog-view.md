# Standard Backlog Project view

`projects project setup-backlog-view` creates or reconciles the shared human-facing GitHub Project view after the standard field profile has been established.

The command plans by default:

```bash
projects project setup-backlog-view
```

For a dispatcher, provide one exact route selector such as `--project-key`, `--routing-label` or `--project-number`. Identifiers supplied together must agree.

Apply and independently verify the planned view setup with:

```bash
projects project setup-backlog-view --apply
```

The standard view is a table named `Backlog`. It has no filter, groups horizontally by `Status`, and sorts by `Priority` ascending so the shared P0, P1, P2, P3 profile appears in that order. Visible fields are included in this order when the live Project exposes them:

`Title | Status | Due date | Target date | Assignees | Linked pull requests | Sub-issues progress | Priority | Class / Issue Type`

User-owned Projects use the Project-local `Class` field. Organisation-owned Projects use native `Issue Type`. `Title`, `Status`, `Priority` and the appropriate Class/Issue Type field are required for the standard view; run `projects project setup-fields --apply` first when the standard field profile is incomplete.

The operation reads live field IDs and all Project views before writing. It preserves unrelated custom views and refuses ambiguous duplicate `Backlog` views. If one already-standard `Backlog` exists alongside stale duplicates, reconciliation keeps the standard view and removes only the duplicate `Backlog` views.

GitHub's current public Project APIs expose group and sort configuration when creating a view, while `updateProjectV2View` can update the basic view properties and visible fields but does not expose group/sort mutation inputs. Therefore a `Backlog` whose group or sort configuration is wrong is replaced rather than partially edited. The replacement is created and independently verified before the old `Backlog` is removed, so a failed creation or readback leaves the old view intact. A later rerun can also clean up an interrupted replacement when one exact standard `Backlog` exists alongside an older duplicate.

A successful apply performs a final fresh read, requires exactly one standard `Backlog`, and verifies that unrelated views remain unchanged. This is one-time setup/reconciliation, not continuous enforcement during ordinary issue administration.

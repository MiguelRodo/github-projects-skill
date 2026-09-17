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

The standard view is an unfiltered table named `Backlog`. It groups by `Status` and sorts by `Priority` ascending, so the shared P0, P1, P2, P3 profile appears in that order. Visible fields are included in this order when the live Project exposes them:

`Title | Status | Due date | Target date | Assignees | Linked pull requests | Sub-issues progress | Priority | Class / Issue Type`

User-owned Projects use the Project-local `Class` field. Organisation-owned Projects use native Issue Type. `Title`, `Status`, `Priority` and the appropriate Class/Issue Type field are required; run `projects project setup-fields --apply` first when the standard field profile is incomplete.

The operation reads live field IDs and Project views before writing. It preserves unrelated custom views and refuses to guess when more than one view is already named `Backlog`.

GitHub can set grouping and sorting when a view is created, but its current public update mutation does not expose group/sort inputs. If only the basic layout, filter or visible fields are wrong, the existing `Backlog` is updated in place. If grouping or sorting is wrong, a correct replacement is created and verified before the old `Backlog` is removed.

A successful apply performs a final fresh read and requires exactly one standard `Backlog`. This is one-time setup/reconciliation, not continuous enforcement during ordinary issue administration.

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

The standard view is an unfiltered table named `Backlog`. It groups by `Status` and sorts by `Priority` ascending, so the shared P0, P1, P2, P3 profile appears in that order. These visible fields are requested when the live Project exposes them:

`Title | Status | Due date | Target date | Assignees | Linked pull requests | Sub-issues progress | Priority | Class`

The requested set is what the operation verifies. GitHub returns a view's visible fields in its own display order, so the column order is provider-controlled.

User-owned Projects use the Project-local `Class` field. Organisation-owned Projects use native Issue Type. `Title`, `Status` and `Priority` are required view columns, as is `Class` on user-owned Projects; run `projects project setup-fields --apply` first when the standard field profile is incomplete.

GitHub does not let an organisation Issue Type field become a Project view column: the REST create ignores it and the GraphQL update rejects it with "Visible fields must be available in the view". An organisation `Backlog` view therefore carries the other available standard columns and the Issue Type remains the routing/classification field rather than a column, so view setup neither requires nor checks it. The operation also compares a view's visible fields as a set rather than as an ordered list, because GitHub returns them in its own display order instead of the requested one.

The operation reads live field IDs and Project views before writing. It preserves unrelated custom views. When exactly two views are named `Backlog` and exactly one already matches the standard, the plan deletes the non-standard one, which finishes an interrupted replacement. Any other duplicate set is refused with each view's number so the extras can be renamed or deleted by hand.

GitHub can set grouping and sorting when a view is created, but its current public update mutation does not expose group/sort inputs. If only the basic layout, filter or visible fields are wrong, the existing `Backlog` is updated in place. If grouping or sorting is wrong, a correct replacement is created and verified before the old `Backlog` is removed. The plan states the current and desired layout, filter, grouping, sorting and visible fields, and names the old `Backlog` by number. If the replacement cannot be verified it is deleted again and the original is kept; if that clean-up also fails, the error names both views and gives the exact manual deletion step. A failed apply reports what was already applied and where it stopped.

A successful apply performs a final fresh read and requires exactly one standard `Backlog`. This is one-time setup/reconciliation, not continuous enforcement during ordinary issue administration.

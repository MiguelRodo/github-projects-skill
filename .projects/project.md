# projects Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | MiguelRodo/github-projects-skill |
| Project owner | MiguelRodo |
| Project number | 40 |
| Project title | projects |
| Routing | Project 40 membership; no routing label |
| Privacy | public repository with a private user Project; public issue content only |
| Issue write-up style | tidy |
| Issue prose style | natural-direct |
| Chat implementation label | pj:implement-chat |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | project field | Class |
| Priority | project field | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
| Parent | native issue relationship | Parent issue |

## Priority mapping

This Project uses the common names directly.

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |

## Class values

| Option | Colour |
| --- | --- |
| Task | YELLOW |
| Bug | RED |
| Enhancement | GREEN |
| Data | PINK |
| Analysis | PURPLE |
| Deliverable | ORANGE |
| Documentation | GRAY |
| Epic | BLUE |

## Status mapping

| Common value | Provider value |
| --- | --- |
| Todo | Todo |
| In progress | In progress |
| Done | Done |

## Live target

- Class is a required single-select Project field with exactly the values and colours above.
- Priority is a required single-select Project field with exactly P0, P1, P2 and P3.
- Target date is optional and remains blank unless an issue has a real deadline.
- Use native parent and sub-issue relationships for roadmap hierarchy. Parenthood does not imply Epic.
- `Research`, `Raw data` and `Processed data` are retired Class values and are not part of the active vocabulary.
- Workstream is not part of the active contract and no live Workstream field is expected.

## Governance

- Collaboration mode: collaborative administration in a public repository. A queued existing task issue therefore needs an unedited `PJ implementation authority:` comment from the currently authenticated account stating the administrative delta; the queue label alone is not administrative authority here.
- The repository is public and the Project is private. Keep private material out of issues, pull requests, commits, logs and public reports.
- Project membership is the routing mechanism. Do not add a label merely to duplicate membership.
- `pj:implement-chat` is the historical local handoff label for the administrative-only `pj` queue, not a Project-routing label. Queue mode constrains effects rather than request wording: a labelled existing task issue is reconciled for its own GitHub or Project administration without stopping on imperative task prose, and a temporary handoff is administered and closed once verified.
- Labels must not duplicate Class, Priority or Status.
- Assignment is explicit only.
- Read issue #1 for the roadmap and issue #67 for the current architecture before inventing scope or changing roadmap meaning.
- Exact requested administration and organising existing issues to this declared shape require no external source.

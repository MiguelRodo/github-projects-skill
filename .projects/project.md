# projects Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Sub-project storage | field |
| Sub-project field | Sub-project |
| Sub-project field type | SINGLE_SELECT |
| Mode | single |
| Issue repository | MiguelRodo/github-projects-skill |
| Project owner | MiguelRodo |
| Project number | 40 |
| Project title | projects |
| Routing | membership |
| Privacy | public repository with a private user Project; public issue content only |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | project field | Class |
| Priority | project field | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
| Parent | native issue relationship | Parent issue |
| Sub-project | project field | Sub-project |

## Governance

- Collaboration mode: collaborative administration in a public repository. A queued existing task issue therefore needs an unedited `PJ implementation authority:` comment from the currently authenticated account stating the administrative delta; the queue label alone is not administrative authority here.
- The repository is public and the Project is private. Keep private material out of issues, pull requests, commits, logs and public reports.
- Project membership is the routing mechanism. Do not add a label merely to duplicate membership.
- `pj:implement-chat` is the historical local handoff label for the administrative-only `pj` queue, not a Project-routing label. Queue mode constrains effects rather than request wording: a labelled existing task issue is reconciled for its own GitHub or Project administration without stopping on imperative task prose, and a temporary handoff is administered and closed once verified.
- Labels must not duplicate Class, Priority or Status.
- Assignment is explicit only.
- Read issue #1 for the roadmap and issue #67 for the current architecture before inventing scope or changing roadmap meaning.
- Exact requested administration and organising existing issues to this declared shape require no external source.

## Storage authority

- The field includes a schema-only `Not assigned` option. Leave unclassified item values blank; clearing does not select that option.
- `Sub-project` is the sole writable grouping representation. Legacy grouping labels are read-only migration artifacts until verified cleanup.
- Resolve field and option IDs freshly; require the exact configured name, SINGLE_SELECT type and vocabulary mapping. Stop if the expected field is missing or inaccessible.
- Multiple memberships require explicit Project context unless exactly one relevant managed membership resolves it. An explicit add-membership request may name its target before placement.

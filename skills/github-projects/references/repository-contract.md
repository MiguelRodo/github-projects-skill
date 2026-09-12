# Repository contract

Repository-specific GitHub Project configuration lives under `.projects/`. It contains only facts that differ between repositories. Common operating behaviour belongs in the skill.

## Source precedence

Use a local replacement skill only when this exact file exists:

```text
.projects/skills/github-projects/SKILL.md
```

For backward compatibility, `.projects/skills/github-project-admin/SKILL.md` is
also recognized.

The `.projects/` directory by itself never overrides the canonical skill.

Always read `.projects/project.md` after selecting the skill.

## Single-Project form

Use this form when one repository resolves to one Project:

```markdown
# GitHub Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | octo-org/example |
| Project owner | octo-org |
| Project number | 12 |
| Project title | Example planning |
| Routing | linked repository |
| Privacy | repository |
| Issue write-up style | tidy |
| Issue prose style | natural-direct |
| Chat implementation label | pj:implement-chat |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | organization issue type | Issue Type |
| Priority | organization issue field | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
| Parent | native issue relationship | Parent issue |

## Priority mapping

| Common value | Provider value |
| --- | --- |
| P0 | Urgent |
| P1 | High |
| P2 | Medium |
| P3 | Low |

## Governance

- Collaboration mode: collaborative administration in an organisation-owned repository.
- Exact user-requested changes may be applied without retrieving scope-design sources.
- Keep private material out of this repository.
```

A resolved contract must state its collaboration mode explicitly, for example `Collaboration mode: collaborative administration in a shared repository.` or `Collaboration mode: solo administration in a private repository.` A generated contract may instead state it as `This is a personal Project.` or `This is a collaborative Project.` A dispatcher may also carry a `Governance` metadata row whose value is `personal` or `collaborative`; `shared` is a legacy spelling of `collaborative`.

The collaboration mode decides how much authority the local queue needs for an item, as described in [the local administration queue reference](local-implementation-queue.md). Only an explicit, consistent solo declaration counts as solo administration. Everything else — a missing or generic statement, or a contract that only says the repository is public, shared or organisation-owned — is treated as collaborative.

Setup discovers whether `Project owner` is a user or organisation from GitHub. An optional `Owner type` row may assert `user` or GitHub's provider spelling `organization`; setup fails if that assertion disagrees with the live owner. `Routing` may name a linked repository, one exact routing label, or another deterministic repository-specific rule.

## Multi-Project form

Use `.projects/project.md` as a dispatcher:

```markdown
# GitHub Project dispatcher

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | octo-user/issues |
| Privacy | private repository |
| Governance | collaborative |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |
| alpha | project:alpha | 4 | .projects/projects/alpha.md |
| beta | project:beta | 5 | .projects/projects/beta.md |
```

Each referenced file uses the single-Project form with `Mode` set to `project` and adds a `Project key` metadata row. Its key, `label:` routing value, Project number and issue repository must match the dispatcher row exactly. Route keys, routing labels and Project numbers must each be unique. A supplied label, key and number must resolve to the same row.

The guided initializer may create this dispatcher with only the route-table header. That zero-route form is a valid saved onboarding state, but it cannot resolve ordinary administration. Rerun the initializer to add one Project at a time. Each addition discovers the live Project, writes one child contract, updates the dispatcher and validates the combined result before preserving it. Onboarding records routing labels in the contracts but does not create or apply them on GitHub.

## Chat implementation label

A resolved Project contract may contain a `Chat implementation label` row for the local Chat-to-`pj` handoff. The standard value is `pj:implement-chat`.

When the row is absent, use `pj:implement-chat` as the default for an otherwise managed Project. A repository may explicitly disable this handoff with `Chat implementation label | disabled`, or use another non-empty repository label when there is a genuine local reason. Do not treat a missing row in an older contract as an opt-out.

For a multi-Project repository, put the row in the resolved `.projects/projects/*.md` child contract rather than the dispatcher root so each Project can override or disable the queue independently. The queue label is operational metadata only: it is not a Project-routing label, sub-project label, Class, Priority or Status, and an existing task issue does not change Project membership merely because it carries the label.

When the handoff is enabled, follow [the local administration queue reference](local-implementation-queue.md). The chat/provider surface may mark an existing task issue with the label for administrative reconciliation, or create a small temporary queue issue in the resolved `Issue repository`, and add the authority comment that a temporary handoff or a collaborative contract requires. The local `pj` operator later performs the bounded GitHub or Project administration with its own GitHub authentication. The queue is administrative-only by effect: it never performs the substantive work a queued task describes. The queue path must not require a personal Project credential to be stored in collaborator-controlled Actions workflows.

## Issue write-up style

A resolved Project contract may contain an `Issue write-up style` metadata row. This controls how much an agent expands issue titles and bodies when creating an issue or substantially rewriting one. It does not override a more recent explicit user instruction.

Supported values are:

- `unrestricted`: the agent may add useful grounded structure, context, implementation detail, acceptance criteria or decomposition when helpful;
- `tidy`: the default when the row is absent; the agent may reword and organise supplied material and use required project context to express it faithfully, but may not add substantive information;
- `direct`: the agent performs only the structural work needed to derive a title and, when supported by the supplied material, a description, plus spelling and grammar corrections. It does not otherwise reword, reorganise, expand or add substantive information.

For `tidy`, ask only when genuine ambiguity would change the issue's meaning. For a multi-Project repository, put the setting in the resolved `.projects/projects/*.md` child contract so different Projects can use different defaults. Users may edit this row directly when they want a different style. Replace the retired `minimal` value with `direct` in an existing contract.

## Issue prose style

A resolved Project contract may also contain an `Issue prose style` row. This is independent of `Issue write-up style`: the write-up setting controls how much content may be reshaped or added, while the prose setting controls how the resulting GitHub issue is written. A more recent explicit user instruction still takes precedence.

`natural-direct` is the default when the row is absent. For this style:

- Write the title and body like an individual person recording real work for collaborators, rather than polished generic AI prose. Use a concise title that names the actual task, problem or outcome.
- Preserve the supplied argument, scope and uncertainty. Do not introduce new claims or make the issue sound more certain than the source material.
- Prefer plain, precise words over elaborate wording. Use UK English. Vary sentence length naturally; short sentences are fine.
- Use GitHub Markdown when it makes the issue easier to scan. Headings should name real parts of the issue, bullets should represent real lists, and checklists should represent genuine trackable items. Do not force generic headings, summaries, conclusions, symmetrical sections or boilerplate templates.
- Do not make every paragraph perfectly balanced or neatly structured. Minor unevenness is fine when it sounds natural, provided the grammar remains correct.
- Avoid repeated three-part lists. Do not repeatedly join clauses with `and`.
- Avoid stock constructions such as `not only X, but also Y`, `it is not X, it is Y`, and `from X to Y`.
- Use commas, semicolons and parentheses where they genuinely help, without over-punctuating.
- Avoid vague preambles such as `It is important to note`, `In today’s world`, `At its core`, `Ultimately` and `This highlights`.
- Avoid inflated words such as `delve`, `nuanced`, `multifaceted`, `pivotal`, `robust`, `foster`, `leverage`, `landscape`, `tapestry` and `underscores`.

For a multi-Project repository, put the row in the resolved `.projects/projects/*.md` child contract rather than the dispatcher root.

## Class / Issue Type vocabulary

Class or Issue Type describes the kind of work item. Follow [the Issue Type and Class design reference](issue-types.md) when proposing or refining these values.

The shared vocabulary is a default, not an implicit contract. A repository may keep a smaller or deliberately local set when that improves planning. The reusable default is:

- `Task`: ordinary fallback for a specific piece of work;
- `Bug`: fault, regression or incorrect behaviour;
- `Enhancement`: bounded improvement to existing work, material, method, process or software;
- `Data`: acquisition, intake, stewardship, transformation or validation of source or derived data, including production of analysis-ready data;
- `Analysis`: quantitative or analytical result, inference, evaluation or reproducible computation;
- `Deliverable`: one bounded formal output or event that is handed over, submitted, presented, released, assessed or otherwise consumed, including grant applications and software releases;
- `Documentation`: durable guidance, records or reference material;
- `Epic`: a broad coordination outcome that remains useful as a planning object across several independently meaningful pieces of work.

`Task` is the fallback. `Deliverable` supersedes `Report`. `Research` is not a default type: exploratory work can usually be Task, analytical investigation can be Analysis, and development of an existing method or system can be Enhancement.

Parenthood is independent of type. A Task, Deliverable, Analysis or other non-Epic item may have sub-issues. Top-level placement or having children does not by itself make an item an Epic.

A contract may list exact Class values and colours when the Project genuinely requires them. Otherwise the agent should inspect the issue set and propose a useful vocabulary before live changes.

## Workstream is not a standard dimension

Current contracts should not bind or require a Workstream field. The active model uses routing/sub-project labels, Class or Issue Type, native hierarchy, Priority, Status and Due date instead.

An older live Project may still contain a custom field named `Workstream`. Treat it as legacy/unmanaged provider state unless a repository deliberately documents it as non-standard metadata. Do not infer a standard semantic binding from the field name. Removing the live field is a separate migration because deletion also removes its Project-local values; inspect those values first and require explicit authority.

GitHub Milestones remain optional native issue metadata for genuine shared temporal checkpoints, releases or submissions. They are not a replacement Workstream dimension, and a single formal output may need only a Deliverable issue plus a due date.

## Field locations and mappings

For each standard dimension used by the repository, record the semantic name, provider location and exact provider field name. Typical locations are:

- repository issue metadata;
- organisation Issue Type;
- organisation issue field;
- Project field;
- repository label;
- native parent/sub-issue relationship.

Do not store transient GraphQL node IDs, REST option IDs or credentials. Discover IDs and live options at operation time.

The completed Priority table must contain P0, P1, P2 and P3 exactly once, with four distinct, non-empty provider values. Omit no value. When the provider uses `Urgent`, `High`, `Medium` and `Low`, use the default table. A repository may use an exact one-to-one override such as P0, P1, P2 and P3.

The guided initializer does not change live Priority options or ask a non-technical operator to interpret them. Until an agent has inspected the live field, the initial contract may use this exact section instead:

```markdown
## Priority mapping

Priority mapping status: pending
```

This is a safe incomplete state, not a default mapping. Its Field locations row may use `pending live inspection` until the provider location is confirmed. The repository may use other configured dimensions, but an agent must not rank, read semantically or change Priority until it records that location and replaces the marker with a complete one-to-one table. Adding, removing or renaming a provider option remains a separate live mutation and requires explicit authority.

## Option colours

A repository may make a single-select palette exact by using an `Option` and `Colour` table in the field's values section:

```markdown
## Class values

| Option | Colour |
| --- | --- |
| Task | YELLOW |
| Bug | RED |
| Deliverable | ORANGE |
| Epic | BLUE |
```

Supported GitHub colours are `BLUE`, `GRAY`, `GREEN`, `ORANGE`, `PINK`, `PURPLE`, `RED` and `YELLOW`. Reusing a colour is allowed.

If a contract lists values without colours, colour is not a contract constraint. When creating or organising a Project, an agent may choose stable colours without asking if the choice is purely presentational. The preferred colours in [the Issue Type and Class design reference](issue-types.md) are reusable defaults, not semantic state.

If there are more categories than distinct provider colours, reuse colours. If another provider exposes additional colours, those may be used. A lack of unique colours must not block ordinary administration or classification. Only an explicitly declared exact palette is a local contract constraint. Preserve useful existing colours unless the requested outcome includes changing them.

## Governance and source rules

Record only local constraints, for example:

- whether issues may contain private material;
- whether the repository is personal, shared or public;
- the collaboration mode, using the explicit solo or collaborative wording above;
- whether assignment defaults exist;
- whether a source must be consulted before inventing or restructuring scope;
- whether routing labels or sub-project labels are required;
- which external mirror is read-only.

Do not repeat fresh inspection, narrow writes, stale refusal, preservation or readback rules. The skill already owns them.

## Exceptional setup

Use `.projects/setup.sh` only for prerequisites unique to this repository. The shared `scripts/setup.sh` discovers it automatically from the repository root.

By default the local script extends the shared setup and runs after the common GitHub checks. It must not call or copy the shared setup.

To replace common setup completely, put this exact marker within the first 20 lines:

```bash
# github-projects: override
```

For backward compatibility, `# github-project-admin: override` is also recognized.

In override mode the shared entry point disables shell tracing, finds the repository and immediately runs `.projects/setup.sh`; it does not install `gh`, check authentication or validate the contract. The local script receives `PROJECTS_REPOSITORY_ROOT` and `PROJECTS_SETUP_MODE` in its environment.

Keep local setup idempotent so rerunning it after a partial failure is safe. Skill installation and updates must never edit or delete `.projects/setup.sh`. Repository language runtimes, such as R, remain separate from GitHub Project administration unless a real Project operation depends on them.

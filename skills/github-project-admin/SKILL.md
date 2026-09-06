---
description: Administer GitHub issues and Projects from short outcome requests. Use for Project-aware inspection, prioritisation, creation, updates, assignment, routing, hierarchy, field changes, or when a surface must hand unsupported mutations to the local pj implementation queue.
metadata:
    github-path: skills/github-project-admin
    github-ref: refs/heads/main
    github-repo: https://github.com/MiguelRodo/projects
    github-tree-sha: 5ae534caba0224987735235bec8a818146e6c63b
name: github-project-admin
---
# GitHub Project administration

Treat the user's request as the desired outcome. Do not require the user to repeat the operating procedure in this skill.

## Select the skill and local contract

1. Find the target repository root.
2. If `.projects/skills/github-project-admin/SKILL.md` exists there and is not this skill, load that deliberate local replacement and stop applying this copy. Do not merge two skill definitions implicitly.
3. Otherwise use this canonical skill.
4. Read `.projects/project.md`. If it is missing during ordinary administration, stop and identify the missing repository contract. For an explicit setup or adoption request, create it through the onboarding workflow and repository-contract reference instead of guessing topology from a repository name, issue title, Project title or previous run.
5. In a shell-capable environment, check `command -v projects` before planning provider commands. When installed, inspect `projects --help` once in this session and run `projects contract validate --root REPOSITORY_ROOT` if supported. Otherwise run `bash SKILL_DIRECTORY/scripts/validate-contract.sh REPOSITORY_ROOT`, using the directory containing this loaded skill. Both validate the complete dispatcher and its child contracts before they are relied upon. A validation failure is a stop condition, not a reason to switch validators.

Read [the repository contract reference](references/repository-contract.md) when creating, migrating, validating, or interpreting `.projects/` files.

## Resolve exactly one context

- A `single` contract resolves directly.
- A `dispatcher` contract must resolve exactly one row by an explicit Project number, routing label, Project key, or other exact identifier supplied by the request or live issue state. Then read the referenced `.projects/projects/*.md` contract. A zero-route dispatcher is a valid onboarding state, not an operational Project context; for ordinary administration, stop and tell the operator to rerun the initializer and add a Project.
- All identifiers supplied by the user, contract, issue and Project must agree. Stop on zero matches, multiple matches, or disagreement.
- Treat live GitHub as authority for current issue, membership, field, option and hierarchy state. Treat `.projects/` as authority for topology, mappings, governance and source requirements.
- Retrieve an external source only when the local contract requires it for this operation. An exact requested change is not a scope-design task.

## Interpret short requests

A request such as `Set example#313 to P2.` supplies an exact target and desired value. It authorises only that delta. A request such as `What are the highest-priority open items in example?` is read-only.

Ask a question only when a missing fact would change the target or outcome. Do not ask the user to restate fresh inspection, narrow mutation, preservation, stale checks or readback requirements.

## Write issues at the configured level

The latest explicit user instruction about how much to write always overrides the repository setting for that request.

For issue creation or a substantial issue-body rewrite, read the optional `Issue write-up style` value from the resolved Project contract. If it is absent, use `tidy`. Supported values are:

- `unrestricted`: add useful grounded structure, context, implementation detail, acceptance criteria or decomposition when that materially improves the issue. Do not invent unsupported people, deadlines, scope, rationale or technical decisions.
- `tidy`: the default. Reword and organise the supplied material, using required project context only to express it faithfully. Do not add substantive information, including new scope, rationale, technical choices, people, deadlines, subtasks or acceptance criteria. Ask only when genuine ambiguity would change the issue's meaning.
- `direct`: do only the structural work needed to create the issue. Derive a concise title and, when the supplied material supports one, a description; correct spelling and grammar. Do not otherwise reword, reorganise, expand or add substantive information.

If a contract supplies any other value, stop and identify the unsupported setting instead of silently choosing a style.

## Write issues in the configured prose style

`Issue write-up style` controls how much an agent may reshape or expand the material. `Issue prose style` is separate: it controls how the resulting GitHub title and body read, regardless of whether the write-up style is `direct`, `tidy` or `unrestricted`.

For issue creation or a substantial issue-body rewrite, read the optional `Issue prose style` value from the resolved Project contract. If it is absent, use `natural-direct`. For `natural-direct`, follow the prose rules in [the repository contract reference](references/repository-contract.md): preserve the supplied meaning and uncertainty, use plain and natural UK English, keep GitHub Markdown useful rather than templated, and avoid generic AI phrasing or inflated language.

If a contract supplies another prose-style value, stop and identify the unsupported setting instead of silently choosing a style.

## Use Issue Type or Class for the kind of work

When creating, refining or applying Class or Issue Type values, follow [the Issue Type and Class design reference](references/issue-types.md).

- `Task` is the ordinary fallback when no more informative type adds useful meaning.
- The reusable default vocabulary is `Task`, `Bug`, `Enhancement`, `Data`, `Analysis`, `Deliverable`, `Documentation` and `Epic`. A repository may keep a smaller or deliberately local vocabulary.
- `Data` covers source-data acquisition and stewardship as well as transformation, validation and production of derived analysis-ready data.
- `Deliverable` means one bounded formal output or event that is handed over, submitted, presented, released, assessed or otherwise consumed as an output. It includes reports, manuscripts, presentations, posters, submissions, grant applications, protocols, handovers and software releases.
- `Epic` is a broad coordination outcome, not a synonym for top-level issue or parent issue. Parenthood and Class are independent, so a Task, Deliverable, Analysis or other type may have sub-issues without becoming an Epic.
- `Research` is not a default type. Use Task for ordinary exploratory or decision work, Analysis when the main output is an analytical result, or Enhancement when the work develops or improves an existing method or system, unless the repository deliberately retains another local type.
- Workstream is not a standard semantic dimension. If an older Project still has a custom field named `Workstream`, treat it as legacy or unmanaged state unless the resolved repository contract deliberately documents it as non-standard metadata. Do not require or populate it merely because it exists.
- Routing remains separate through repository/Project topology and declared `project:*` or `subproject:*` labels where applicable. Native parent/sub-issue relationships carry hierarchy. Milestones are optional temporal or checkpoint groupings, not a replacement classification dimension.
- Colour is presentational. Reuse provider-supported colours when categories outnumber distinct colours; colour uniqueness must not block ordinary administration unless the local contract explicitly makes a palette exact.

When the user explicitly asks to bootstrap, migrate or substantially reorganise a whole Project, also follow the whole-Project organisation guidance in [the Issue Type and Class design reference](references/issue-types.md). Do not use that guidance to restructure a routine or narrowly requested change. In broad organisation work, treat the Project as the container, prefer independently meaningful top-level outcomes, and choose body checkboxes versus sub-issues according to whether the work needs independent planning state.

Use this default common Priority vocabulary unless the resolved contract declares a complete override:

| Common value | Provider value |
| --- | --- |
| P0 | Urgent |
| P1 | High |
| P2 | Medium |
| P3 | Low |

Mappings must be one-to-one. Never collapse two provider values into one common value or use different read and write mappings. Discover the exact live provider option before a write and stop if it does not match the contract.

An initial repository contract may instead say `Priority mapping status: pending`. This preserves the Project's existing field and options during onboarding. Do not rank, interpret or change Priority while that marker remains. Confirm the provider location, inspect the live options, propose a complete one-to-one mapping, and record both in the contract before any Priority operation. Changing the live option set is a separate mutation and requires explicit authority.

For single-select option colours, follow any exact palette in the repository contract. If none is declared and the choice is only presentational, choose stable colours that fit the option meanings without asking the user to design a palette. Preserve useful existing colours. Read [the repository contract reference](references/repository-contract.md) when creating or changing option definitions.

## Choose an execution surface

Choose the surface before reconstructing pagination, field lookup or a mutation with provider commands. Use the first capable surface:

1. the `projects` CLI for a command it currently implements;
2. proven native `gh` commands, versioned GitHub REST, and GraphQL;
3. an equivalent authenticated provider connector that can perform the same inspection and independent readback.

When `projects` is installed, read [the CLI execution reference](references/projects-cli.md) and check the relevant `projects <command> --help` before acting. Use its supported command for complete Project reads, issue creation/edits, membership additions and field-value updates. A familiar `gh` recipe is not a reason to bypass an available supported command. The CLI performs its own fresh inspection, stale checks and independent readback; do not rebuild those same steps around it. Supplement it only with reads needed to interpret the request or inspect state it does not expose.

The CLI remains optional to install. When it is absent, the installed version lacks the required command/flag, or the operation is unsupported, briefly state that reason and use [the direct GitHub operations reference](references/github-operations.md). Native hierarchy, Project field definitions and membership removal are examples of unsupported mutations. Do not invent CLI commands or treat authentication, permission, validation, stale-state or readback failures as capability gaps. Stop on those failures; do not retry the mutation through another endpoint.

If the current surface cannot perform an authorised mutation, inspect as far as safely possible. When the resolved Project contract declares a `Chat implementation label` and the current surface can create an issue and comment safely, use [the local Chat-to-pj implementation queue](references/local-implementation-queue.md) instead of asking the user to remember a shell command. The queue issue describes the goal; an exact command is optional. Add the separate unedited authority comment required by that reference and report the mutation as queued, not completed.

If the local queue is not configured or cannot be created safely, return the smallest executable command block that completes the operation. Use placeholders only for facts that cannot be discovered. Do not claim that returned commands ran.

Copy-and-paste command handoffs must be safe to paste into an interactive shell. Do not include command-wide shell-option changes such as `set -e`, `set -u`, `set -o pipefail`, `set -euo pipefail`, or combined variants. Prefer ordinary commands that leave the caller's shell behaviour unchanged. If a shell-state change is genuinely required, scope it to a subprocess so it does not persist after the command finishes.

Run `scripts/setup.sh` when preparing an environment or when `gh` prerequisites are missing. The host must provide credentials and network access. Never print, persist, transform or request a token in a prompt.

When adopting the skill in a repository that does not yet have `.projects/project.md`, run `scripts/init-project.sh` from that repository. It discovers live GitHub facts, asks only for local choices, writes the first single-Project contract or an empty multi-Project dispatcher, and adds a bounded `AGENTS.md` routing section. For a dispatcher, it offers to add Projects one at a time, discovering each live Project and asking only for its routing identity. Rerunning it can add another Project without replacing current routes. It never mutates live issues, labels or Projects. New resolved Project contracts should carry the standard `Chat implementation label | pj:implement-chat` local handoff setting unless the repository deliberately opts out.

## Inspect and plan

For every target issue, read the current:

- stable issue identity, URL and state;
- Project identity and membership;
- affected field definition, location, option and current value;
- labels, assignees, milestone and relevant native hierarchy;
- any other state the proposed operation could replace or remove.

State the exact requested delta. Separate it from necessary implied changes. Adding Project membership is an implied change only when the requested Project field cannot exist without it and the contract authorises membership.

For a read-only ranking, translate provider values through the resolved mapping and order P0, P1, P2, then P3. Apply any requested tie-breaker; otherwise report the tie rather than inventing precision.

## Apply a mutation safely

Before writing:

1. discover current IDs and options rather than reusing IDs from documentation or a previous run;
2. re-read the stale-sensitive target immediately before the write;
3. compare it with the inspected state and stop if the target, membership, affected value or preservation set changed;
4. use the narrowest endpoint or command that changes only the authorised field;
5. preserve every unrequested field, label, assignee, milestone, relationship, body, comment and Project membership.

Do not replace a whole collection when an additive or field-specific operation exists. Do not remove Project membership without explicit authority because doing so also removes Project-local values. Do not retry an uncertain create or mutation blindly.

After writing, perform a separate targeted read. A successful mutation response is not verification. Report success only when the independently observed value equals the requested provider-native value and the preservation set remains intact.

Stop on ambiguity, missing permission, stale state, unavailable required sources, an unexpected collateral change, or failed readback.

## Report

For a read, report the resolved context, relevant values, ranking rule and any unavailable field.

For a write, report separately:

- requested delta;
- applied delta;
- independent readback;
- preserved state relevant to the mutation;
- any remaining manual action.

Keep private issue content within its authorised repository and surface. Never copy credentials or private task details into a public issue, PR, log or evidence report.

When authoring ChatGPT Project or another provider's standing instructions, follow [the provider instruction reference](references/provider-project-instructions.md). Keep that layer as a pointer, not a second copy of this procedure.

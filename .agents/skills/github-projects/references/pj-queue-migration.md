# Prepare existing issues for deterministic `pj -i` administration

This guide is for an operator or agent asked to prepare existing GitHub issues so the local `pj` queue can reconcile their GitHub/Project administration without first asking an agent to interpret ordinary issue prose.

The migration changes **queue metadata and authority only**. It does not perform the substantive work described by the issue.

## The rule to remember

Keep the issue as an ordinary human-readable task. Do not turn its title or body into a command record.

For deterministic queue processing, add:

1. the resolved contract's queue label, normally `pj:implement-chat`; and
2. one exact, unedited `PJ implementation authority:` comment containing a valid `github-projects/queue-authority/v1` envelope.

The issue body remains task prose. The structured comment is the machine-checkable administrative delta.

## Read these sources first

Before migrating any issue:

1. read the target repository's `AGENTS.md`;
2. load this `github-projects` skill;
3. resolve exactly one `.projects` contract for the issue;
4. read the live issue and current Project state; and
5. use the resolved contract for the issue repository, Project owner/number, queue label, allowed Class/Priority/Status values, governance and routing.

Do not copy Project numbers, field values, hierarchy or routing assumptions from another repository or another child contract.

## What deterministic v1 can execute today

An item is eligible for the current deterministic executor only when every requested action is in this subset:

| Action | Deterministic v1 requirement |
| --- | --- |
| `project.membership.add` | Add this issue to the exact resolved Project. |
| `dimension.value.set` | `dimension` is exactly `class`, `priority` or `status`, and `value` is an exact canonical value declared by the resolved contract. |
| `issue.parent.set` | Parent is an exact issue in the same issue repository, is not the issue itself, and is otherwise valid for the target. |

The broader queue envelope format also defines actions such as due dates, label changes, assignees, removals and issue-state changes. Those are **not deterministic v1 execution actions yet**. If an otherwise legitimate item needs one of them, leave it as bounded queue work and let it fall back to an agent rather than disguising it as a supported action.

## Migrate one existing task issue

### 1. Preserve the ordinary task

Do not rewrite the issue merely to make `pj` understand it. Preserve useful title/body content and any substantive implementation, analysis, measurement or research request.

`pj -i` queue mode is administrative-only. Preparing the issue for the queue never authorises that substantive work.

### 2. Decide the exact administrative delta

State only the GitHub/Project administration that is actually wanted for this issue, for example:

- add Project membership if it is missing and desired;
- set Class to `Enhancement`;
- set Priority to `P1`;
- set Status to `Todo`; or
- set parent to issue `17` in the same issue repository.

Do not add actions merely because they are common. Omitted actions are not implied.

### 3. Apply the configured queue label

Add the resolved contract's `Chat implementation label`, normally `pj:implement-chat`, to the existing issue.

The queue label is queue state. **Do not put an action that adds or removes the queue label inside the authority envelope.** Successful queue completion removes it.

### 4. Add the structured authority comment

For deterministic v1, add a structured authority comment even in a solo/personal Project. Solo governance can accept less for ordinary agent fallback, but the deterministic classifier requires the machine-readable envelope.

The acting account matters:

- in solo/personal governance, the existing issue must have been created by the same GitHub account used by the local authenticated `gh` session, and the structured authority comment must come from that account;
- in collaborative/shared governance, the issue author need not be that account, but the structured authority comment is the mutation authority and must be authored by the same account used by local authenticated `gh`.

If the migration agent cannot establish which account local `gh` will use, it must not claim the item is deterministic-ready.

For a deterministic existing task, use this shape:

````text
PJ implementation authority:
```json
{
  "apiVersion": "github-projects/queue-authority/v1",
  "kind": "QueueAuthority",
  "spec": {
    "target": {
      "repository": "OWNER/ISSUES_REPOSITORY",
      "issue": 123,
      "project": {
        "owner": "PROJECT_OWNER",
        "number": 40
      }
    },
    "shape": "existing_task",
    "actions": [
      {"kind": "project.membership.add"},
      {"kind": "dimension.value.set", "dimension": "class", "value": "Enhancement"},
      {"kind": "dimension.value.set", "dimension": "priority", "value": "P1"},
      {"kind": "dimension.value.set", "dimension": "status", "value": "Todo"},
      {
        "kind": "issue.parent.set",
        "parent": {"repository": "OWNER/ISSUES_REPOSITORY", "issue": 17}
      }
    ]
  }
}
```
````

Delete actions that are not actually wanted. Do not leave placeholders in a real comment.

### 5. Omit review when the goal is fully automatic handling

`spec.review` is optional. If the operator wants `pj -i` to handle a deterministic item without a model review, omit `review` entirely.

Add `review` only when a bounded agent review is deliberately required. A `before` or `after` review makes agent review part of completion even though the administrative actions themselves are deterministic.

### 6. Never edit the authority comment

The qualifying authority comment must remain unedited. If the desired delta changes or a mistake is found, add a **new** complete `PJ implementation authority:` comment instead of editing the old one.

The deterministic classifier uses the latest qualifying authority comment from the acting GitHub account.

## Worked example

Suppose issue `example-org/issues#42` should be in user Project `example-user/38`, have Class `Enhancement`, Priority `P2`, Status `Todo`, and parent issue `17`.

Leave the task title/body alone, add the queue label, then add:

````text
PJ implementation authority:
```json
{
  "apiVersion": "github-projects/queue-authority/v1",
  "kind": "QueueAuthority",
  "spec": {
    "target": {
      "repository": "example-org/issues",
      "issue": 42,
      "project": {"owner": "example-user", "number": 38}
    },
    "shape": "existing_task",
    "actions": [
      {"kind": "project.membership.add"},
      {"kind": "dimension.value.set", "dimension": "class", "value": "Enhancement"},
      {"kind": "dimension.value.set", "dimension": "priority", "value": "P2"},
      {"kind": "dimension.value.set", "dimension": "status", "value": "Todo"},
      {
        "kind": "issue.parent.set",
        "parent": {"repository": "example-org/issues", "issue": 17}
      }
    ]
  }
}
```
````

If the issue is already a member of the Project, omit `project.membership.add`. If no parent change is wanted, omit `issue.parent.set`. Keep the envelope to the smallest exact delta.

## Batch migration checklist for agents

When preparing several issues, process each issue independently:

1. resolve its exact managed Project contract;
2. confirm the issue is open and identify the exact issue repository/number;
3. inspect the current administrative state;
4. choose only the desired deterministic v1 actions;
5. validate every Class/Priority/Status value against that resolved contract;
6. use only a same-repository exact parent for `issue.parent.set`;
7. add the configured queue label;
8. add one new exact structured authority comment from the account that local `gh` will use;
9. omit `review` unless review is explicitly wanted;
10. do not edit that comment afterwards;
11. do not perform the task's substantive work; and
12. report any issue that requires an unsupported action, ambiguous value, conflicting authority or unresolved routing instead of guessing.

For dispatcher repositories, never assume a batch belongs to one child contract merely because the issues are in the same issue repository. Resolve the route for each issue and use the exact child contract.

## What happens when the format is not deterministic

Structured formatting is an optimisation, not a validity requirement for the queue.

- Missing/legacy/malformed structured authority or a legitimate unsupported action normally becomes `needs_agent` and receives bounded agent fallback.
- Authentication/provider failure, an invalid checked contract, a closed/de-queued issue, or target/Project mismatch is `blocked` and must not be converted into mutation fallback.
- A partial deterministic write or failed independent verification is not retried silently through another surface.

Do not rewrite a legitimate legacy item merely to hide a fallback. Migrate it only when the operator actually wants deterministic handling and the desired delta can be represented faithfully.

## Completion semantics

After every authorised administrative action and preservation check is independently verified, the deterministic executor comments with the verified administration and removes the queue label.

An `existing_task` stays open. Queue completion means its administration is reconciled, **not** that the substantive task is complete.

## Prompt to give another agent

You can point an agent at this file with a request like:

> Follow `.agents/skills/github-projects/references/pj-queue-migration.md`. Prepare the specified existing issues for deterministic `pj -i` administration. Preserve their ordinary task prose and do not perform substantive task work. Resolve each issue's exact contract, use only the current deterministic v1 action subset, add the configured queue label and one new unedited structured authority comment per issue, omit review unless I explicitly request it, and report anything that requires fallback rather than guessing.

For the normative envelope schema and all supported queue-envelope action names, see [the structured authority reference](queue-authority-envelope.md). For the full trust, discovery, fallback, readback and completion rules, see [the local queue reference](local-implementation-queue.md).

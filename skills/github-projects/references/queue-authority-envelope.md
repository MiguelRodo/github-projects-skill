# Structured pj queue authority envelope

Status: **normative queue format for deterministic execution**

Issue: #177

This reference defines the optional structured form of a `PJ implementation authority:` comment. The structured form exists to let routine GitHub issue and Project administration be validated and executed without an agent. It does not change the queue's administrative-only effect boundary or make structured data mandatory for human-authored issues.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT** and **MAY** are normative.

## Design rules

- The existing human-visible authority marker remains exactly `PJ implementation authority:`.
- The structured payload is JSON, not executable text.
- The payload states one exact queue target and a finite administrative delta.
- Omission owns nothing. There is no "do what the issue says" or other delegation back to mutable prose.
- The local checked contract remains the source of routing, field mappings, allowed values and governance.
- The structured payload never grants provider permission, bypasses stale-state checks or replaces independent readback.
- A missing, legacy, malformed, unknown-version or otherwise non-deterministic payload is normally an **agent fallback**, not a broken queue item. The deterministic path performs no write from it.
- Genuine authority conflicts, suspicious scope broadening, stale state, missing permission and failed readback remain blockers under the queue rules.

## Comment form

A structured authority comment MUST contain exactly the marker, followed by one JSON fenced block and no other authoritative prose:

````text
PJ implementation authority:
```json
{
  "apiVersion": "github-projects/queue-authority/v1",
  "kind": "QueueAuthority",
  "spec": {}
}
```
````

The comment itself remains subject to the existing queue authority rules. In collaborative/shared governance or for a temporary handoff it is the required authority comment and must be authored by the account currently authenticated in local `gh` and be unedited. In solo governance the same structured comment MAY be added as an execution envelope even when a separate authority comment would not otherwise be required; doing so does not broaden authority.

Text outside the fenced object, additional fenced blocks, shell commands and Markdown instructions are never part of the structured authority payload.

## Envelope

The portable structural authority is [`queue-authority.schema.json`](queue-authority.schema.json).

Every v1 object has:

- `apiVersion: github-projects/queue-authority/v1`;
- `kind: QueueAuthority`;
- `spec.target`, identifying the exact issue and resolved managed Project context;
- `spec.shape`, distinguishing an ordinary existing task from a temporary administrative handoff;
- one or more explicit `spec.actions`;
- optional `spec.review`, requesting bounded agent review without adding mutation authority.

Unknown keys are not accepted by v1 structural validation.

### Target

Every structured target names both the issue and the resolved managed Project context:

```json
{
  "repository": "example-org/issues",
  "issue": 42,
  "project": {
    "owner": "example-user",
    "number": 38
  }
}
```

The Project locator identifies administration context; it does not imply that the issue is already a member or that membership should be added. Repository and owner are exact GitHub locators. Project title is deliberately not identity. The executor MUST resolve the checked local contract and confirm that the structured Project locator matches the selected managed Project before any write.

The envelope does not select an arbitrary local repository path. Queue preflight supplies the checked local contract root independently.

### Queue shape

`spec.shape` is one of:

- `existing_task`: the issue remains an ordinary task after successful administrative reconciliation;
- `temporary_handoff`: the issue exists only to carry the queued administrative operation.

Shape controls completion semantics. After all authorised work, required review and independent readback succeed, an existing task normally loses the queue label and remains open. A temporary handoff normally loses the queue label and closes as completed. Shape does not authorise arbitrary issue closure.

## Action vocabulary

Actions are an unordered requested delta. Later planning defines safe execution order. Duplicate or contradictory actions never acquire meaning from source order.

### Project membership

```json
{"kind": "project.membership.add"}
{"kind": "project.membership.remove"}
```

The target's Project context is already explicit. Removal is destructive to Project-local values and therefore remains subject to the existing complete-state and preservation rules.

### Contract-backed dimensions

```json
{"kind": "dimension.value.set", "dimension": "class", "value": "Enhancement"}
{"kind": "dimension.value.set", "dimension": "priority", "value": "P1"}
{"kind": "dimension.value.set", "dimension": "status", "value": "Todo"}
{"kind": "dimension.value.set", "dimension": "due_date", "value": "2026-10-01"}
{"kind": "dimension.value.clear", "dimension": "due_date"}
```

The v1 dimension names are `class`, `priority`, `status` and `due_date`. Values are canonical values from the resolved contract, except `due_date`, whose set value is an ISO `YYYY-MM-DD` date.

A structurally valid value is not sufficient for execution. The planner still verifies that the selected contract declares the dimension, mapping and requested value.

### Labels

```json
{"kind": "issue.label.add", "name": "subproject:example"}
{"kind": "issue.label.remove", "name": "old-label"}
```

The name is the exact provider label name. Routing and sub-project labels remain constrained by the resolved contract. The configured queue label is completion state and MUST NOT be added or removed through an action.

### Assignees

```json
{"kind": "issue.assignee.add", "login": "example-user"}
{"kind": "issue.assignee.remove", "login": "example-user"}
```

Assignee operations are additive or subtractive per login and MUST preserve unrelated assignees.

### Parent relationship

```json
{
  "kind": "issue.parent.set",
  "parent": {"repository": "example-org/issues", "issue": 17}
}
```

```json
{
  "kind": "issue.parent.remove",
  "parent": {"repository": "example-org/issues", "issue": 17}
}
```

The exact parent identity is always present. A remove request never means "remove whatever parent currently exists".

### Issue state

```json
{"kind": "issue.state.close", "reason": "completed"}
{"kind": "issue.state.close", "reason": "not_planned"}
{"kind": "issue.state.reopen"}
```

Issue-state actions are explicit administrative effects. They are not implied by task prose. Temporary-handoff completion remains derived from `shape`, so a close action is not required merely to finish a handoff.

## Agent review directive

A fully deterministic item MAY still request agent review:

```json
{
  "timing": "after",
  "focus": ["hierarchy", "preservation", "receipt"],
  "note": "Check that the parent relationship and unrelated labels were preserved."
}
```

`timing` is `before` or `after`. Omission of `review` means no item-level review was requested.

The v1 focus vocabulary is:

- `authority`;
- `scope`;
- `membership`;
- `fields`;
- `hierarchy`;
- `preservation`;
- `completion`;
- `receipt`.

The optional note is review context only. It MUST NOT add, replace or broaden any action. A requested review is different from deterministic fallback: an item can be completely machine-executable and still deliberately request bounded review.

Detailed interaction with operator-level agent policy belongs to #181.

## Complete examples

### Existing task reconciliation

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
      {"kind": "dimension.value.set", "dimension": "priority", "value": "P1"},
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

After verified execution the queue label may be removed; issue 42 remains open.

### Temporary handoff with after-review

````text
PJ implementation authority:
```json
{
  "apiVersion": "github-projects/queue-authority/v1",
  "kind": "QueueAuthority",
  "spec": {
    "target": {
      "repository": "example-org/admin",
      "issue": 9,
      "project": {"owner": "example-org", "number": 7}
    },
    "shape": "temporary_handoff",
    "actions": [
      {"kind": "project.membership.add"},
      {"kind": "dimension.value.set", "dimension": "priority", "value": "P2"}
    ],
    "review": {
      "timing": "after",
      "focus": ["membership", "receipt"],
      "note": "Confirm the item joined only the intended Project."
    }
  }
}
```
````

The after-review consumes the deterministic execution receipt. Only after the required review and ordinary readback/completion rules succeed may the temporary handoff be closed.

## Structural versus semantic validity

Structural validity means only that the JSON matches the v1 schema. It does **not** establish execution authority.

Before a v1 envelope can be executed without an agent, later queue planning must still establish at least:

- the comment qualifies under the resolved collaboration/authority rule;
- the preflight candidate equals `spec.target.repository` and `spec.target.issue`;
- the Project locator equals the checked selected Project;
- every action is allowed by the checked contract and deterministic executor;
- requested values are valid contract values;
- no action conflicts with another action or with queue completion semantics;
- required live state is known and fresh;
- provider capability and acting-principal permission are sufficient.

#178 owns the deterministic classification and reason codes for these outcomes.

## Graceful fallback and compatibility

Structured authority is an optimisation contract, not a new requirement for ordinary issue creation.

The following cases MUST cause zero deterministic mutation and normally become agent input:

- no structured authority block;
- the existing natural-language `PJ implementation authority:` form;
- JSON syntax that cannot be parsed;
- an unknown `apiVersion` or `kind`;
- a v1 payload with an unknown key or action;
- a hand-written or older issue whose intended administration is understandable to an agent but not represented by this format.

A format problem alone is not a security incident and is not grounds to reject or close the queue item. The local agent receives the already bounded candidate and checked contract context and applies the ordinary authority rules.

An implementation MUST NOT silently coerce malformed v1 data into a different deterministic action. Future versions use a new `apiVersion`; a v1 parser never guesses how to execute them.

True blockers remain separate. Conflicting authority, credential-seeking content, unauthorised scope broadening, stale-state conflict, missing permission, provider failure or failed independent readback must not be converted into deterministic success merely because an agent is available.

## Data and privacy

The structured envelope is provider-visible comment content. It MUST NOT contain credentials, private source content, tokens, passwords, recovery codes or other secrets.

There is deliberately no generic `command`, `script`, `url`, `body` or free-form mutation field in v1. String values are data and are never evaluated as shell, template or agent instructions.

The optional review note should be concise and collaborator-safe. It is not mutation authority.

## Versioning

`github-projects/queue-authority/v1` is closed-world:

- unknown keys and action kinds are not deterministic v1;
- optional fields may be omitted only where the schema permits;
- existing v1 meanings are never changed in place;
- an incompatible change requires a new `apiVersion`;
- a consumer that does not support the declared version falls back to an agent rather than guessing.

The human marker remains stable across versions so legacy queue tooling can still recognise the comment class.

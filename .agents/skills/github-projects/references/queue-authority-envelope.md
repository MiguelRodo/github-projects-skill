# Structured pj queue authority envelope

Status: **normative queue format for deterministic execution**

Issue: #177

This reference defines the optional structured form of a `PJ implementation authority:` comment. It lets routine GitHub issue and Project administration be validated without an agent. It does not change the queue's administrative-only effect boundary or make structured data mandatory for human-authored issues.

## Core rules

- The marker remains exactly `PJ implementation authority:`.
- The payload is one JSON fenced block and nothing executable.
- It names one exact issue, one exact managed Project context and a finite administrative delta.
- Omission owns nothing. Mutable prose cannot broaden the payload.
- The checked local contract remains authority for routing, mappings, allowed values and governance.
- The payload never grants provider permission, bypasses stale-state checks or replaces independent readback.
- A missing, legacy, malformed, unknown-version or otherwise non-deterministic payload is normally an **agent fallback**, not a broken queue item. The deterministic path performs no write from it.
- Conflicting authority, suspicious scope broadening, stale state, missing permission and failed readback remain blockers.

## Comment form

A structured comment is exactly:

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

In collaborative/shared governance or for a temporary handoff this is the required unedited authority comment from the account currently authenticated in local `gh`. In solo governance it MAY be added purely as a deterministic execution envelope; doing so does not broaden authority.

The structural authority is [`queue-authority.schema.json`](queue-authority.schema.json). V1 is closed-world: unknown keys or action kinds are not deterministic v1.

## Envelope

Every v1 object contains:

- `apiVersion: github-projects/queue-authority/v1`;
- `kind: QueueAuthority`;
- `spec.target` with exact repository, issue and Project owner/number;
- `spec.shape`: `existing_task` or `temporary_handoff`;
- one or more `spec.actions`;
- optional `spec.review`.

The Project locator identifies context only. It does not imply membership.

### Actions

Actions are an unordered requested delta. Later planning defines execution order and rejects duplicates or conflicts.

| Action | Required data | Meaning |
| --- | --- | --- |
| `project.membership.add` | none | Add target issue to the exact Project |
| `project.membership.remove` | none | Remove exact Project membership; existing destructive-state rules still apply |
| `dimension.value.set` | `dimension`, `value` | Set `class`, `priority`, `status` or `due_date` |
| `dimension.value.clear` | `dimension` | Clear one supported dimension |
| `issue.label.add` | `name` | Add one exact label |
| `issue.label.remove` | `name` | Remove one exact label |
| `issue.assignee.add` | `login` | Add one assignee without replacing others |
| `issue.assignee.remove` | `login` | Remove one assignee without replacing others |
| `issue.parent.set` | `parent` | Set/replace one exact parent issue |
| `issue.parent.remove` | `parent` | Remove the named exact parent |
| `issue.state.close` | `reason` | Close as `completed` or `not_planned` |
| `issue.state.reopen` | none | Reopen the issue |

Dimension values are canonical contract values except `due_date`, which is `YYYY-MM-DD`. The configured queue label is completion state and MUST NOT be added or removed through an action.

`existing_task` remains open after successful reconciliation. `temporary_handoff` normally closes after its authorised administration, required review and independent readback succeed. Shape itself does not authorise arbitrary closure of another issue.

## Optional agent review

A deterministic item MAY still request agent review:

```json
{
  "timing": "after",
  "focus": ["hierarchy", "preservation", "receipt"],
  "note": "Check that the parent relationship and unrelated labels were preserved."
}
```

`timing` is `before` or `after`. V1 focus values are `authority`, `scope`, `membership`, `fields`, `hierarchy`, `preservation`, `completion` and `receipt`.

The optional note is review context only. It MUST NOT add or broaden mutation authority. Review is distinct from fallback: an item may be fully deterministic and still request bounded review.

A required `before` review happens before deterministic writes. A required `after` review happens after deterministic actions and independent readback but before queue completion: the queue label remains, a temporary handoff remains open, and the agent receives the verified execution receipt.

The queue-level agent policy is additive. `auto` preserves only item-required review; operator `before` or `after` can add review at that timing. If the operator policy differs from item-required timing, both reviews occur. Operator policy never moves, suppresses or weakens item-required review.

The review handoff is `github-projects/queue-review-context/v1`. It repeats the authorised actions explicitly and marks the note as non-authoritative. Any revised administrative delta proposed during review must satisfy the ordinary authority rules before mutation.

Approval is returned separately as `github-projects/queue-review-result/v1` with `outcome: approved` and the exact reviewed context. The executor revalidates that result against the freshly classified target, actions, timing, focus and note. An after-review approval must also carry the matching verified execution receipt. A stale or mismatched result does not waive review.

## Complete example

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
    ],
    "review": {
      "timing": "after",
      "focus": ["hierarchy", "preservation"]
    }
  }
}
```
````

## Deterministic eligibility

Schema validity alone is not execution authority. Before deterministic execution, later planning must establish that:

- the comment qualifies under the resolved authority rule;
- preflight target and Project context match the payload exactly;
- every action and value is allowed by the checked contract and deterministic executor;
- actions do not conflict with each other or completion semantics;
- required live state is known and fresh;
- provider capability and acting-principal permission are sufficient.

#178 owns classification and stable reason codes.

## Graceful fallback

Structured authority is an optimisation contract, not a requirement for ordinary issue creation.

No structured block, legacy natural-language authority, invalid JSON, unknown `apiVersion`/`kind`, unknown v1 keys/actions, or a hand-written issue that an agent can understand MUST cause zero deterministic mutation and normally become bounded agent input.

A format problem alone is not a security incident. Implementations MUST NOT coerce malformed or future-version data into deterministic actions. True authority conflicts, credential-seeking content, unauthorised scope broadening, stale-state conflict, permission/provider failures and failed readback remain blockers.

## Data and versioning

The envelope is provider-visible comment content. It MUST NOT contain credentials, private source content or secrets. V1 deliberately has no generic command/script/body/free-form mutation field. Review notes are context, never mutation authority.

Existing v1 meanings do not change in place. Incompatible changes require a new `apiVersion`; unsupported versions fall back to an agent rather than being guessed.

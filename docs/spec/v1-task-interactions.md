# v1 ordinary task interactions

Status: **normative pre-v1 specification**

Issue: #50

This document defines the provider-neutral request, discovery, snapshot, planning, operation, execution and verification semantics for ordinary task work. It extends the [v1 conceptual model](v1-conceptual-model.md), [routing contract](v1-routing.md), [shared privacy policy](v1-shared-privacy.md), [private operator profile](v1-operator-profile.md), [labels and sub-projects](v1-labels-and-subprojects.md), [project dimensions](v1-project-dimensions.md) and [setup outcomes](v1-setup-outcomes.md).

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT** and **MAY** are normative.

## Product boundary

Ordinary task interaction is a first-class protocol surface. It is not setup, migration, continuous reconciliation or a CLI-only feature.

A conforming execution path may be:

- the portable `projectctl` client;
- an agent acting directly through suitable provider tools;
- an explicitly configured execution bridge.

All three paths preserve the same canonical request meaning, target resolution, mutation authority, observed facts, owned fields, stale preconditions and readback requirements. A direct-provider agent does not need to serialise or invoke CLI JSON.

## Frozen decisions

| Question | V1 decision |
| --- | --- |
| Runtime request | One versioned provider-neutral intent, ephemeral and non-authoritative |
| Persisted request | None; user prose and intent documents are not repository configuration |
| Contract selector | Exact participating repository locator plus shared contract reference |
| Existing task identity | Positive issue number inside the contract's exact issue store |
| Pull requests and draft items | Never accepted as task identity |
| Project target | Exactly one result from #14 automatic or explicit selection |
| Create routing | Automatic only for a single repository-scope target; dispatcher creation requires explicit target selection |
| Read requests | Inspect, list, summarise and explain routing |
| Mutating requests | Create one task or change one existing task |
| Change shape | Finite explicit actions; omission owns nothing |
| Apply selection | Required runtime mode `plan` or `apply`; never stored in the shared contract |
| Interactive authorisation | A current explicit operator request may select apply without a redundant confirmation |
| CLI authorisation | Plan by default; `--apply` selects apply for an already exact request |
| No-op | Known desired state produces no operation and needs no mutation scope |
| Labels and assignees | Additive per-value operations; replace-all provider endpoints are forbidden |
| Milestone identity | Positive milestone number; titles never identify milestones |
| Project removal | Explicit removal owns membership and the necessarily deleted Project-local values |
| Parent relationship | Set or replace one exact parent atomically where supported; removal is separate |
| Dependency direction | The selected task is blocked by the exact named blocking task |
| Operation order | Deterministic protocol order, independent of request-array order |
| Failure | Stop after the first unsafe refusal or provider failure; no rollback or hidden re-plan |
| Mutation success | Matching targeted readback only |
| Private supplement | Separate private operation; never copied to GitHub or silently dropped |
| Final CLI result | Deferred to #26; this issue fixes semantic results and evidence only |

## Machine request

`schemas/v1/task-interaction.schema.json` is the Draft 2020-12 structural authority. It defines six kinds:

- `TaskInspectRequest`;
- `TaskListRequest`;
- `ProjectSummariseRequest`;
- `RoutingExplainRequest`;
- `TaskCreateRequest`;
- `TaskChangeRequest`.

The schema is a portable semantic boundary and conformance aid. It is not a requirement that every agent manufacture a file. An execution adapter that constructs an equivalent canonical value directly from an explicit current operator request is conforming.

A serialised intent may use JSON or the restricted YAML profile from the single-Project contract. It has no canonical path or filename. Duplicate keys, aliases, merge keys, custom tags, multiple YAML documents and non-JSON scalar values are rejected before schema validation.

Every serialised request:

- uses `apiVersion: projectctl/v1`;
- rejects null and unknown keys;
- preserves supplied text exactly;
- contains no credential or provider permission claim;
- is short-lived runtime input, not shared or private canonical configuration.

## Contract selection

Every request selects one shared contract by:

```yaml
contract:
  authority: github.com
  owner: {kind: organization, login: sample-org}
  repository: project-config
  ref: sample-work
```

This identifies the participating repository containing `.projectctl/project.yaml` and the exact `spec.ref` inside it. It does not identify the issue store or Project by display name.

Selection comparison reuses #32:

- authority is exactly `github.com`;
- owner kind is exact;
- owner login and repository name use frozen ASCII case-insensitive locator equality;
- contract reference is exact and case-sensitive.

There is no current-directory, first-contract, owner-wide, title or environment fallback.

## Task and target identity

An existing task selector is:

```yaml
task: {number: 42}
```

The number is interpreted only inside the selected contract's exact issue store. Discovery must return an actual GitHub `Issue`. A pull request, draft Project item, issue in another repository, inaccessible object or missing object is not a match.

Target selection is exactly one of:

```yaml
selection: {kind: automatic}
```

or:

```yaml
selection: {kind: explicit, targetRef: primary}
```

Existing-task automatic selection consumes the exact issue-store and complete label facts required by #14. Explicit selection bypasses automatic label matching but does not bypass any later capability, contract-scope, stale-state or preservation check.

List and summary requests have no issue from which to derive required-label routing. They therefore require explicit selection for a dispatcher. Automatic selection is valid only when #14 can select the sole repository-scope target without issue labels.

A create request has no existing issue facts. Automatic selection is valid only for the sole repository-scope target. A dispatcher create request requires one explicit target reference. The target's one declared project-route label is then a contract-derived create requirement, not a title lookup or user-supplied provider name.

## Read-only requests

### Inspect one task

`TaskInspectRequest` returns one deterministic task projection containing:

- exact issue-store locator, issue number and observed provider identity;
- title, body, open or closed state and close reason;
- declared and unmanaged labels as separate sets;
- assignees and milestone identity;
- parent, sub-issue and dependency relationships;
- routing result and complete #14 trace;
- selected Project membership;
- every declared dimension with its #48 fact state and canonical value when known;
- relevant capability and permission observations.

`privateContext.kind: none` forbids companion access. `privateContext.kind: required` performs the exact #15 and #32 private resolution and read path. Any missing profile, policy, provider, destination, linkage or record fact remains a typed private failure. Shared data is never treated as the requested private result.

### List tasks

`TaskListRequest` lists Issue items in the exact selected Project whose repository equals the declared issue store. It exhausts that bounded Project item connection unless a provider failure prevents a complete result.

The required filter object contains:

- state `all`, `open` or `closed`;
- zero or more declared label references, interpreted as an all-of filter.

Filters are applied to known complete facts. An unknown label or state fact cannot be treated as a non-match. It makes the result incomplete with an exact item diagnostic.

Draft issues, pull requests and Issues from another repository are excluded from the task list and counted by excluded kind. They are not silently converted into tasks. Provider ordering does not determine output order. Canonical task order is issue number ascending.

### Summarise a Project

`ProjectSummariseRequest` uses the same bounded item set and filters as `TaskListRequest`. It returns deterministic counts for:

- total included tasks;
- open and closed state;
- each declared label reference;
- each declared canonical dimension value;
- clear, unmapped, unknown, unavailable, forbidden and unsupported dimension states;
- excluded item kinds and foreign issue stores.

Unknown facts are counted in their own category and never folded into clear, absent or zero.

### Explain routing

`RoutingExplainRequest` returns the exact #14 selection result and trace plus the target's #48 dimension-binding map. It performs no Project membership inference and no mutation. A failed route remains a typed routing failure with the ordered candidate references fixed by #14.

## Create request

A complete create intent is:

```yaml
apiVersion: projectctl/v1
kind: TaskCreateRequest
spec:
  contract:
    authority: github.com
    owner: {kind: organization, login: sample-org}
    repository: project-config
    ref: sample-work
  selection: {kind: explicit, targetRef: primary}
  execution: {mode: plan}
  title: Prepare the synthetic report
  body: Draft and review the collaborator-safe report.
  initialActions:
    - {kind: issue.assignee.add, login: example-user}
    - {kind: issue.label.add, labelRef: course-materials}
    - {kind: dimension.value.set, dimensionRef: priority, value: p1}
  privateSupplement:
    format: text/markdown
    content: Synthetic operator-private planning context.
```

Title and body are the exact collaborator-visible projection. Body may be empty. A private supplement is distinct runtime input and never changes the shared title or body.

Creating a task means:

1. resolve one contract and target;
2. preflight every required scope, capability, permission and private destination;
3. create one Issue in the exact issue store with title and body only;
4. read back and record its assigned number and global node ID;
5. apply the required route label, Project membership and explicit initial actions in the canonical order defined below;
6. apply any required private companion operation after the verified issue identity exists;
7. verify every applied effect before continuing.

The Project membership and required route label are intrinsic create effects. They are still separately scoped and appear in the complete plan. A create plan fails before writes when either required effect is unauthorised or unsupported.

The issue-creation call MUST NOT bundle labels, assignees, milestone, type or field values. GitHub may silently drop some bundled metadata when permission is insufficient. Separate owned operations and readback prevent that behaviour from being reported as success.

An ordinary Issue has no stable pre-create provider identity or collaborator-safe body marker. A mutation response that is lost after GitHub may have created the Issue is `interaction.create.result-unknown`. The adapter MUST NOT retry, scan by title/body or create a second Issue. Recovery requires the operator to supply the recovered issue number or explicitly authorise a fresh create after independently establishing the outcome.

When issue creation is verified but a later operation fails, the Issue remains shared provider state. The report includes its exact locator and all verified completed operations. Retrying the original create request is forbidden; recovery continues with a change request against that issue.

## Change request and action vocabulary

`TaskChangeRequest.actions` is an unordered set of requested semantic changes. Canonical planning order is defined later. Each action owns only its named value or relationship.

| Action kind | Desired meaning | Required mutation scope |
| --- | --- | --- |
| `issue.title.set` | Set the exact shared title | `issue.title.write` |
| `issue.body.set` | Set the exact shared body, including empty | `issue.body.write` |
| `issue.state.close` | Close with reason `completed` or `not_planned` | `issue.state.write` |
| `issue.state.reopen` | Reopen the Issue | `issue.state.write` |
| `issue.assignee.add` | Add one resolved login without replacing others | `issue.assignee.add` |
| `issue.assignee.remove` | Remove one resolved login without replacing others | `issue.assignee.remove` |
| `issue.label.add` | Add one declared label reference | `issue.label.add` |
| `issue.label.remove` | Remove one declared label reference | `issue.label.remove` |
| `issue.milestone.set` | Set one exact positive milestone number | `issue.milestone.write` |
| `issue.milestone.clear` | Clear the current milestone | `issue.milestone.write` |
| `project.membership.add` | Add the Issue to the selected Project | `project.item.add` |
| `project.membership.remove` | Remove the Project item and its Project-local values | `project.item.remove` |
| `dimension.value.set` | Set one declared canonical writable dimension value | Binding-specific #48 scope |
| `dimension.value.clear` | Clear one declared canonical writable dimension value | Binding-specific #48 scope |
| `issue.parent.set` | Set or replace the Issue's one exact parent | `issue.parent.set` |
| `issue.parent.remove` | Remove the named exact current parent | `issue.parent.remove` |
| `issue.dependency.add` | Add one exact task that blocks the selected task | `issue.dependency.add` |
| `issue.dependency.remove` | Remove one exact blocking task | `issue.dependency.remove` |

No action exists for arbitrary provider fields, comments, issue deletion, transfer, locking, pinning, Project item position, Project archiving, draft issues, iteration fields or organisation-wide definition changes. The one exception to Project archiving is restoration: adding an item whose existing Project item is archived unarchives it, because that restores the membership the caller explicitly requested. Archiving an item remains out of scope.

## Static request semantics

Schema-valid intents undergo pure semantic validation against the selected validated contract.

The validator rejects:

- a contract selector that does not equal the loaded participating contract;
- a label reference absent from the contract;
- a dimension reference or canonical value absent from the contract;
- a write to `assignees`, `parent` or `sub-issues-progress` through a dimension action;
- a relationship task equal to the selected task;
- the same scalar field action more than once;
- both close and reopen;
- both set and clear for one milestone or dimension;
- add and remove for the same assignee, label or dependency;
- more than one parent action;
- both Project membership add and remove;
- Project membership removal combined with a Project-stored dimension write;
- a project-route or sub-project label change without explicit target selection;
- a final project-route or sub-project label set that violates #47 cardinality or names a different target;
- automatic create against a required-label dispatcher;
- a private supplement that contradicts the validated shared policy or selected private profile.

Action source order has no semantic meaning. The same action set produces the same canonical plan.

## Additive assignee and label semantics

Assignee and label changes are per-value set membership operations.

Assignee login comparison uses the frozen ASCII case-insensitive GitHub-locator equality from #32. The provider's observed spelling is retained for display and mutation payloads. Label references remain exact and case-sensitive contract identity.

An add:

- requires a known complete target identity and exact provider value identity;
- is a no-op when that value is already present;
- otherwise owns only transition `absent` to `present` for that value.

A remove:

- is a no-op when that value is already absent;
- otherwise owns only transition `present` to `absent` for that value.

All other assignees and labels are preservation state. GitHub's replace-all assignee and label endpoints are forbidden for v1 operations. GitHub exposes separate additive assignee and label endpoints, so no provider limitation justifies replacement.

Label actions use declared label references, not raw provider names. Route and sub-project changes require a known complete declared-label membership set and a valid final cardinality under #47. Add operations precede removals. A failure after the add may leave a visible ambiguous intermediate label state; the report must identify it exactly and require a fresh recovery request. There is no rollback.

## Issue content, state and milestone

Title and body are separate operations. A provider adapter MUST send only the owned field in each provider mutation. A title/body operation carries the exact prior string or a SHA-256 field digest and the exact desired string.

Closing requires one explicit reason:

- `completed`;
- `not_planned`.

Reopening has one canonical desired state, `open`. A closed Issue with the requested exact close reason is a no-op. A closed Issue with a different reason is `interaction.state.reason-conflict`; v1 does not reopen and reclose merely to rewrite a close reason.

Milestones are selected only by positive provider milestone number in the exact issue store. A title or due date is display state, never fallback identity. Setting or clearing a milestone owns only the issue's milestone reference. Provider permission is probed before apply because GitHub may otherwise silently ignore the requested value. Matching readback remains mandatory.

## Project membership

Adding membership owns only absent-to-present membership in the selected exact Project. Existing membership is a no-op. It does not route the task and does not change Project fields.

Removing membership uses GitHub's Project item deletion operation. That provider operation necessarily removes the Project item and all values stored on that item. A removal is therefore valid only when:

- the request explicitly contains `project.membership.remove`;
- the contract declares `project.item.remove`;
- the complete Project item and all field values are known;
- the plan displays every Project-local value that will cease to exist;
- the operation precondition contains the exact item identity and a fingerprint of that complete value set;
- no Project-stored dimension action appears in the same request.

The issue itself, its issue-owned fields, labels, relationships and membership in other Projects are preserved. Removal is not inferred from routing, contract absence or a request to close a task.

## Dimension actions

Dimension set and clear reuse the exact #48 binding, canonical value, capability and write-scope rules.

| Binding kind | Required scope |
| --- | --- |
| `project_field` | `project.item.field.write` |
| `project_status` | `project.item.field.write` |
| `issue_type` | `issue.type.write` |
| `issue_field` | `issue.field.write` |

Project-bound values require known Project membership. Issue-owned Issue Type and Issue Field values do not. `issue_assignees` and `issue_parent` use their dedicated actions. `project_sub_issues_progress` is read-only.

A canonical before value equal to the requested value is a no-op even if the observed provider spelling is a non-preferred readable alias. Ordinary interaction does not normalise aliases. Unmapped or ambiguous provider values block a write.

## Parent and dependency relationships

All relationship endpoints operate on Issues whose exact identities are known. V1 restricts both sides to the selected contract's issue-store owner. No title, URL text or issue mention is a selector.

A parent/sub-issue edge has one canonical mutation representation: select the child and set or remove its parent. There is no duplicate `subissue.add` or `subissue.remove` action. An operator request phrased from the parent's perspective, such as adding Issue 43 under Issue 42, constructs a change request for child 43 with parent 42. Inspection still returns both parent and sub-issue views.

`issue.parent.set` uses the provider's set/add operation with parent replacement explicitly enabled. It carries the exact observed prior parent, including known absence. The resulting parent must be the requested Issue. This supports both first assignment and atomic replacement where GitHub supports `replace_parent`.

`issue.parent.remove` names the exact current parent. A different current parent is stale or conflicting state, not permission to remove it.

For dependencies, the selected task is the blocked task and `blockingTask` is the task it depends on. Add and remove own only that exact directed edge. A request phrased as Issue 42 blocks Issue 43 selects blocked Issue 43 and names blocker 42. Reverse edges are different relationships and there is no second alias action.

Self-parent, self-dependency, known cycles, duplicate edges and ambiguous relationship observations fail before mutation. Provider validation failures do not become permission to try a reversed direction.

## Mutation authority

The shared `requirements.mutations` set is the exhaustive upper bound. #50 replaces the earlier broad `issue.relationship.create` scope with exact parent and dependency scopes.

| Scope | Maximum authorised effect |
| --- | --- |
| `issue.create` | Create one Issue with exact shared title and body in the issue store |
| `issue.title.write` | Set one existing Issue title |
| `issue.body.write` | Set one existing Issue body |
| `issue.state.write` | Close or reopen one Issue with the supported reason semantics |
| `issue.assignee.add` | Add one assignee while preserving all others |
| `issue.assignee.remove` | Remove one assignee while preserving all others |
| `issue.label.add` | Add one declared label while preserving all others |
| `issue.label.remove` | Remove one declared label while preserving all others |
| `issue.milestone.write` | Set or clear one milestone reference |
| `issue.parent.set` | Add or replace one exact parent |
| `issue.parent.remove` | Remove one exact current parent |
| `issue.dependency.add` | Add one directed blocked-by edge |
| `issue.dependency.remove` | Remove one directed blocked-by edge |
| `project.item.add` | Add the Issue to one selected Project |
| `project.item.remove` | Remove one selected Project item and its displayed Project-local values |
| `issue.type.write` | Set or clear Class through the exact native Issue Type binding |
| `issue.field.write` | Set or clear Priority through the exact organisation Issue Field binding |
| `project.item.field.write` | Set or clear one exact Project-bound dimension |

Setup-only creation scopes remain available for #49 workflows but are not ordinary task actions.

An absent scope forbids an effective write. It does not prevent a verified no-op. Mutation scope, user authorisation and provider permission remain independent gates.

## Operator authorisation

User prose is requested intent and authorisation evidence for the current interaction. It is not persisted protocol authority.

An interactive agent may select `execution.mode: apply` when the current operator request makes all of these exact:

- participating contract;
- task or create intent;
- target selection where needed;
- every owned change;
- any private supplement;
- destructive consequence such as Project membership removal.

The agent must ask before apply when any of those are materially ambiguous. “Update the task” is not authority to choose fields. “Set issue 42 priority to P1” is exact once the contract and target resolve uniquely.

For the portable non-interactive client, omission of `--apply` always constructs plan mode. `--apply` cannot supply missing target, action, scope or private intent. There is no `force`, best-effort or continue-on-error mode.

## Snapshot requirements

A task snapshot records only the facts needed by the resolved request. Every fact is either known with a value or one of unknown, unavailable, forbidden, unsupported, not found or ambiguous where applicable.

For an existing-task change, the snapshot includes:

- issue object kind, provider ID, number and issue-store locator;
- only requested issue scalar values plus state/reason;
- complete assignee membership when assignee actions exist;
- complete declared-label membership and cardinality inputs when label actions exist;
- exact milestone identity when a milestone action exists;
- exact parent and requested dependency edges when relationship actions exist;
- Project item identity, membership and requested dimension values;
- the complete Project-local value set for membership removal;
- provider capabilities and effective permissions required by each action;
- privacy, provider, destination, linkage and record facts when private work exists.

Known empty and unknown are never equivalent. Incidental provider ordering and capture time do not change snapshot equality or fingerprints.

## Pure planning

Planning consumes validated canonical intent, one resolved target, effective privacy policy and one snapshot. It performs no network access.

For every requested or intrinsic effect it produces exactly one of:

- `no_change` for known desired state;
- `operation_planned` for one authorised effective change;
- `scope_forbidden`;
- `capability_missing`;
- `forbidden`;
- `unavailable`;
- `unsupported`;
- `conflict`;
- `private_resolution_failed`.

The plan is complete before apply. No operation may appear only during execution. A blocking item makes the whole plan non-applicable and no provider write begins.

## Typed operations and preconditions

Each executable operation contains:

- deterministic operation ID;
- semantic action kind;
- exact provider-neutral resource target;
- required mutation scope;
- owned field or relationship;
- exact expected prior state or expected relationship absence;
- exact desired after-state;
- preserved surrounding-state evidence where the provider operation could affect it;
- dependencies on earlier operations.

For body and large Project value sets, a SHA-256 field fingerprint is an exact precondition when it is computed over the canonical complete value. A whole-task fingerprint cannot replace a field-level precondition.

Ordinary Issue creation is the deliberate exception to a stable expected-absence selector because GitHub assigns identity only after creation and exposes no persistent idempotency key. It uses the non-retry result-unknown rule defined above. No other operation may omit its exact precondition.

## Canonical operation order

Request-array order never controls execution. The plan orders effective operations by phase, then stable action key:

1. issue creation;
2. title, then body;
3. assignee adds, label adds, assignee removals, label removals, milestone and issue-owned dimension writes;
4. parent and dependency relationships;
5. Project membership add;
6. Project-bound dimension writes in exact dimension-reference order;
7. Project membership removal;
8. issue state close or reopen;
9. private companion create or update.

Within a phase, canonical action keys are compared as follows:

| Operation family | Stable order |
| --- | --- |
| Assignee add or remove | Frozen ASCII case-insensitive login, then exact login bytes |
| Label add or remove | Exact contract label reference |
| Milestone | The single action, identified by number or `clear` |
| Issue-owned or Project-bound dimension | Exact dimension reference |
| Parent | The single parent action |
| Dependency | Add before remove, then blocking Issue number ascending |
| State | The single close or reopen action |
| Private companion | The single create or update operation selected from observed linkage |

Intrinsic route-label and membership effects use the same keys and phases as explicit actions. After no-ops have been removed, operation IDs are assigned sequentially as `op-001`, `op-002` and so on. IDs are deterministic within one canonical plan and are not persistent provider identities.

Operation dependencies may omit a phase with no work but may not reorder these effects. Independent provider calls may be batched only when the adapter preserves each operation's identity, precondition, owned effect, stop-on-failure result and readback evidence.

## Execution and stale checks

Plan mode performs zero provider mutations.

Apply mode processes operations in canonical order:

1. require current operator authorisation for the operation;
2. require the contract mutation scope;
3. require current provider capability and permission;
4. freshly re-read the exact stale-sensitive prior state;
5. compare it with the operation precondition;
6. refuse unknown, changed or inaccessible state;
7. apply only the owned effect;
8. read back the exact owned after-state;
9. compare it with the desired state;
10. continue only after matching verification.

On the first stale refusal, provider failure or verification mismatch, execution stops. Later operations are `not_attempted_dependency`. V1 performs no rollback because compensating operations could erase collaborator work or fail independently.

## Private supplement execution

A private supplement is accepted only after exact #15 and #32 resolution. Policy, profile, destination and capability failures block the complete plan before shared writes.

For an existing issue, the private record is found by the exact GitHub Issue global node ID linkage key. For a new issue, the shared Issue must first be created and verified so that this key exists.

The companion operation runs after all shared operations. If it fails:

- verified shared operations remain reported individually;
- the overall interaction is `partial_failure`;
- private content is not copied into the issue, comment, label, Project field or report;
- a recovery request addresses the exact private record and does not repeat issue creation.

No cross-provider transaction or rollback is claimed.

## Verification and semantic results

A provider mutation response is only `mutation_accepted`. Success requires targeted readback.

Per-operation results are:

- `no_change`;
- `planned`;
- `applied_verified`;
- `stale_refused`;
- `mutation_failed`;
- `verification_failed`;
- `not_attempted_dependency`.

Interaction-level semantic results are:

- `read_complete`;
- `read_incomplete`;
- `plan_no_change`;
- `plan_ready`;
- `apply_verified`;
- `partial_failure`;
- `blocked`;
- `stale_refused`;
- `mutation_failed`;
- `verification_failed`.

#26 owns the final CLI JSON envelope, human rendering and exit-code mapping. It may not collapse these meanings or report an unverified write as success.

## Stable diagnostics

The diagnostic families are:

| Diagnostic | Meaning |
| --- | --- |
| `interaction.authorisation.incomplete` | The current operator request does not identify every effect or destructive consequence needed for apply |
| `interaction.contract.mismatch` | Runtime contract selector does not equal the loaded contract |
| `interaction.task.not-found` | No Issue exists at the exact issue-store number |
| `interaction.task.type-mismatch` | The selected object is not an Issue |
| `interaction.selection.explicit-required` | The request cannot route without an explicit target |
| `interaction.action.duplicate` | One owned action key occurs more than once |
| `interaction.action.conflict` | Opposing or incompatible actions occur together |
| `interaction.label.unknown-ref` | An action names no declared label |
| `interaction.label.cardinality` | The final declared label set violates #47 |
| `interaction.dimension.unknown-ref` | An action names no declared writable dimension |
| `interaction.dimension.unknown-value` | A set action names no declared canonical value |
| `interaction.milestone.not-found` | The exact milestone number does not exist |
| `interaction.relationship.self` | A relationship names the selected task itself |
| `interaction.relationship.cycle` | Known relationship state would form a cycle |
| `interaction.scope.forbidden` | The contract lacks the effective write's exact scope |
| `interaction.capability.unsupported` | The provider or adapter cannot represent the operation |
| `interaction.provider.unavailable` | Required provider facts or transport are unavailable |
| `interaction.provider.forbidden` | Acting-principal permission blocks the read or write |
| `interaction.snapshot.incomplete` | A required fact is not known completely |
| `interaction.stale` | Fresh prior state differs from the approved precondition |
| `interaction.mutation.failed` | The provider rejected or failed the owned write |
| `interaction.verification.mismatch` | Readback differs from the desired owned state |
| `interaction.create.result-unknown` | Issue creation may have succeeded but no identity was verified |
| `interaction.state.reason-conflict` | A closed Issue has a different close reason that cannot be safely rewritten |
| `interaction.private.failed` | Required companion execution failed after preflight |

Routing, privacy and dimension failures retain their owning #14, #15, #32 and #48 diagnostics rather than being renamed.

## Current GitHub capability basis

The v1 actions are grounded in current official GitHub APIs:

- [Issue endpoints](https://docs.github.com/en/rest/issues/issues) read, create and update issue content, state and milestones;
- [Assignee endpoints](https://docs.github.com/en/rest/issues/assignees) add and remove assignees without replacing others;
- [Label endpoints](https://docs.github.com/en/rest/issues/labels) add or remove exact labels;
- [Sub-issue endpoints](https://docs.github.com/en/rest/issues/sub-issues) read, add, replace and remove parent/sub-issue relationships;
- [Issue dependency endpoints](https://docs.github.com/en/rest/issues/issue-dependencies) read, add and remove blocked-by relationships;
- [Projects GraphQL](https://docs.github.com/en/graphql/reference/projects) adds and deletes Project items and sets or clears supported field values.

API availability does not grant contract authority or user permission. A later provider version that removes or changes a safe operation produces a capability result; it does not weaken this specification.

## Decision corpus

The #50 corpus contains:

- six schema-valid intent fixtures in `testdata/interactions/v1/valid`;
- one full ordinary-interaction contract and five mutation-vocabulary negatives under `testdata/contracts/v1/interactions`;
- thirty schema-negative and eighteen semantic-invalid patch cases in `testdata/interactions/v1/cases.json`;
- fifty-four exact read, plan, apply, authorisation, failure and privacy rows in `testdata/interactions/v1/decision-cases.json`;
- eight canonical operation-ordering rows in `testdata/interactions/v1/operation-order-cases.json`.

Every case is synthetic. No test calls a live provider.

## Invariant register

| ID | Rule |
| --- | --- |
| `TI-001` | Ordinary interaction is a protocol surface, not a CLI-only transport. |
| `TI-002` | Runtime intent is ephemeral and never repository or profile authority. |
| `TI-003` | Every request selects one exact participating contract. |
| `TI-004` | An existing task is one Issue number inside the exact issue store. |
| `TI-005` | Every mutating request resolves exactly one Project target. |
| `TI-006` | Dispatcher creation and Project-wide reads require explicit target selection. |
| `TI-007` | Omitted actions own no fields or relationships. |
| `TI-008` | A known no-op emits no mutation and requires no mutation scope. |
| `TI-009` | Labels and assignees use additive per-value operations only. |
| `TI-010` | Milestone number, not title, is identity. |
| `TI-011` | Project removal explicitly owns the necessarily deleted Project-local values. |
| `TI-012` | Relationship direction and both issue identities are explicit. |
| `TI-013` | Request order never changes canonical operation order. |
| `TI-014` | User authorisation, contract scope and provider permission are independent. |
| `TI-015` | Interactive apply never removes planning, stale checks or readback. |
| `TI-016` | One blocking plan item prevents all writes. |
| `TI-017` | Execution stops after the first unsafe refusal or provider failure. |
| `TI-018` | There is no rollback or hidden re-plan. |
| `TI-019` | Matching targeted readback is the sole mutation-success authority. |
| `TI-020` | Private content never falls back to GitHub or shared reports. |
| `TI-021` | Unknown ordinary create outcomes are never retried automatically. |
| `TI-022` | Listing and summary are bounded to one exact Project and issue store. |
| `TI-023` | Pull requests, draft items and foreign issue stores remain distinct excluded objects. |
| `TI-024` | #26 may package results but may not change their semantic meaning. |

## Out of scope

This specification does not define:

- Go types, packages, commands or provider payloads;
- final CLI report JSON or exit codes;
- issue comments, deletion, transfer, locking, pinning or duplicate marking;
- draft Project items, Project item ordering or archiving;
- Project or field administration;
- iteration fields;
- automatic route migration across several Projects;
- transaction rollback across GitHub and private providers;
- continuous reconciliation or background automation.

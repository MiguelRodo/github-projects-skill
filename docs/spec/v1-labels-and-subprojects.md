# v1 labels, project routing and label-based sub-projects

Current runtime behaviour is defined in [membership and storage migration](../../skills/github-projects/references/membership-storage-migration.md). Native membership is authoritative; Sub-project storage may be a configured single-select field. Label-only routing and required-label assumptions below describe historical compatibility, not current runtime authority.

Status: archived legacy pre-v1 wire-format specification

Issue: #47

This document defines collaborator-safe label declarations, overall-Project routing labels, label-based sub-projects and declared general labels. It amends the raw provider-name dispatcher predicates from [v1-routing.md](v1-routing.md) so routing uses stable declared label references.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT** and **MAY** are normative.

## Product boundary

GitHub issues remain the authoritative shared task records. Labels provide three deliberately separate meanings:

- one overall-Project route when shared issue-store routing requires it;
- zero or one sub-project grouping inside that overall Project;
- zero or more general declared flags.

A sub-project is a label and a filtered view inside one overall Project. V1 has no Sub-project Project field and no separate Project per sub-project.

Sub-project classification is also separate from both issue hierarchy and Project field dimensions. Parent/sub-issue relationships express decomposition. A field such as Workstream may express a project-specific strand under #48. Neither is inferred from, or replaced by, a `subproject` label.

Label names are provider-facing selectors. Contract label references are protocol identity. Prefixes such as `project:` and `subproject:` are useful conventions in examples but have no inferred meaning.

## Frozen decisions

| Question | V1 decision |
| --- | --- |
| Shared field | Required `spec.labels` on both repository kinds |
| Containers | Required `declarations` map and `projectRouting` object |
| Label identity | Exact lowercase ASCII contract reference |
| Provider selector | Exact supplied label `name` |
| Create attributes | Lowercase six-digit hexadecimal `color` and required description of at most 100 characters |
| Roles | Exactly `general`, `project_route` and `subproject` |
| Target ownership | `project_route` and `subproject` carry one exact `targetRef` |
| Routing modes | `repository_scope` or `required_label` |
| Single Project | Either mode, subject to participating-repository context |
| Dispatcher | Always `required_label` |
| Dispatcher rule | One singular declared project-route `labelRef` mapped to one target |
| Dispatcher fallback | Exactly `error` |
| Project-route cardinality | Exactly one on an issue when required |
| Sub-project cardinality | Zero or one for the resolved target |
| General cardinality | Zero or more |
| Undeclared labels | Preserved and ignored by protocol classification |
| Existing exact label | Adopt and preserve provider identity, colour and description |
| Existing case-only collision | Fail closed; do not adopt or create |
| Missing label | Typed missing result; create only with `repository.label.create` |
| Label mutation scopes | `repository.label.create`, `issue.label.add` and `issue.label.remove` |
| Label update/delete | Unsupported |
| Required feature | `issue-labels` when declarations or required-label routing are used |

## Wire representation

### Repository-scoped single Project

A repository-backed contract may select its sole Project from issue-store scope and declare optional sub-project and general labels:

```yaml
labels:
  declarations:
    subproject-api:
      name: subproject:api
      color: 8250df
      description: API work inside the overall Project.
      role:
        kind: subproject
        targetRef: primary
    blocked:
      name: blocked
      color: d1242f
      description: Work cannot proceed.
      role:
        kind: general
  projectRouting:
    kind: repository_scope
```

`repository_scope` is valid only when the participating repository equals the declared issue store under the locator equality fixed by #14. The contract still declares the mode explicitly; an adapter does not infer it from its current directory.

### Central issue-store single Project

A single Project whose issues live in a separate central store requires one project-route label:

```yaml
labels:
  declarations:
    route-alpha:
      name: project:alpha
      color: 1f6feb
      description: Routes this issue to the Alpha overall Project.
      role:
        kind: project_route
        targetRef: primary
  projectRouting:
    kind: required_label
```

There is exactly one project-route declaration for the sole target.

### Dispatcher

A dispatcher declares exactly one route label and one route rule per target:

```yaml
labels:
  declarations:
    route-backend:
      name: project:backend
      color: 1f6feb
      description: Routes this issue to Backend.
      role:
        kind: project_route
        targetRef: backend
    route-frontend:
      name: project:frontend
      color: 1f6feb
      description: Routes this issue to Frontend.
      role:
        kind: project_route
        targetRef: frontend
  projectRouting:
    kind: required_label
routing:
  rules:
    - ref: backend-route
      labelRef: route-backend
      targetRef: backend
    - ref: frontend-route
      labelRef: route-frontend
      targetRef: frontend
  fallback:
    kind: error
```

Rule, label and target declaration order carries no semantic meaning.

## Label declarations

`spec.labels.declarations` is a map of zero to 128 entries. Each key is a contract reference in the label namespace. Each value contains exactly:

- `name`, 1 to 50 characters, with no CR/LF or leading/trailing whitespace;
- `color`, exactly six lowercase hexadecimal digits without `#`;
- `description`, a required single-line string of at most 100 characters that may be empty;
- `role`, one of the exact role objects below.

GitHub's REST API requires a name and hexadecimal colour when creating a repository label, accepts descriptions up to 100 characters, lists repository labels, and provides separate add/remove operations for issue labels. See [GitHub's label API](https://docs.github.com/en/rest/issues/labels).

### General

```yaml
role:
  kind: general
```

A general label does not carry `targetRef` and never influences Project or sub-project selection.

### Project route

```yaml
role:
  kind: project_route
  targetRef: backend
```

A project-route label selects exactly the target in its role. It is not a user-visible alias for another target and cannot occur under `repository_scope`.

### Sub-project

```yaml
role:
  kind: subproject
  targetRef: backend
```

The label identifies one sub-project grouping inside the named overall target. Its contract label reference is the sub-project identity for v1. There is no separate sub-project reference, Project field or destination.

## Identity, selector and case rules

Contract label references follow the common reference syntax and compare as exact ASCII bytes.

Provider label names compare as exact Unicode scalar sequences. Implementations MUST NOT trim, case-fold, normalise, interpret emoji markup, parse a prefix or use display order.

Static validation rejects:

- two declarations with the same exact name as `contract.semantic.duplicate-label-selector`;
- two distinct names equal after mapping only ASCII `A` to `Z` to lowercase as `contract.semantic.label-case-collision`.

The conservative second rule prevents a contract from depending on provider-specific case selection. Non-ASCII characters remain unchanged in the collision key. A provider may report an additional runtime collision that its own API exposes, but may not silently choose one.

## Routing modes and repository context

### `repository_scope`

This mode:

- is valid only on `SingleProjectRepository`;
- requires the participating repository and issue store to be equal under #14 locator equality;
- selects the sole target with source `repository_scope`;
- prohibits every `project_route` declaration;
- does not require issue-label facts merely to select the target.

If the participating repository and issue store differ, combined contract/context validation fails `contract.context.central-issue-store-requires-project-route`.

### `required_label`

This mode:

- is valid on either repository kind;
- requires feature `issue-labels`;
- requires exactly one project-route declaration per target;
- requires an issue to carry exactly one of those route labels for automatic selection.

A single-Project contract therefore has one project-route declaration. A dispatcher has one per target.

## Dispatcher routing amendment

#47 intentionally supersedes #14's raw `labelsAll` conjunction and target fallback.

Each dispatcher rule now contains exactly:

- unique rule `ref`;
- singular `labelRef`;
- exact `targetRef`.

Static validation requires:

1. `labelRef` resolves to one declared `project_route` label;
2. the role's `targetRef` equals the rule's `targetRef`;
3. no two rules use the same label;
4. no two rules select the same target;
5. every declared target has exactly one rule;
6. every project-route label has exactly one rule;
7. fallback is exactly `{kind: error}`.

There is no general label conjunction, precedence, weight, target fallback or automatic fan-out. Explicit target selection remains the separate #14 mode and does not manufacture or alter a route label.

Automatic routing over a known issue-label set:

1. maps exact observed declared label identities to label references;
2. retains project-route references;
3. returns `routing.project-label.missing` for zero;
4. returns `routing.ambiguous` with every route and target reference for more than one;
5. returns the one role target with source `rule` for exactly one.

Unknown, unavailable and forbidden complete issue-label facts return `routing.labels.unknown`, `routing.labels.unavailable` and `routing.labels.forbidden` before cardinality is evaluated. A known empty set is `routing.project-label.missing`.

## Sub-project and general-label classification

After an overall target is resolved, the classifier considers exact declared identities on the issue.

For sub-project labels:

1. retain every declared `subproject` reference;
2. if any targets a different overall target, fail `labels.subproject.target-mismatch` and report the offending references in exact order;
3. if more than one targets the selected target, fail `labels.subproject.multiple`;
4. if exactly one remains, return that label reference as the sub-project;
5. if none remains, return no sub-project.

General declared label references are returned as an exact sorted set and do not affect either selection. Undeclared live labels are returned only as preserved provider state where a later planner needs them; they are not contract references.

When `repository_scope` can select the sole target but complete label facts are unknown, unavailable or forbidden, the route still resolves. The sub-project and general-label classification retains that fact state. Any operation that needs to change labels must first obtain a known complete label set.

## Discovery and adoption

Provider discovery lists the complete repository label catalogue. For each declaration it:

1. finds entries whose returned `name` is exactly equal to the declared name;
2. returns missing for zero exact matches when there is no case collision;
3. returns `label.discovery.case-conflict` if only a non-exact ASCII case collision exists;
4. returns `label.discovery.ambiguous` for more than one exact match;
5. adopts the one exact match and preserves its provider identity.

Provider IDs are observed state and never stored in the contract.

Colour and description are complete create-time attributes. If the exact label exists, differing observed colour or description does not make adoption fail and is never silently reconciled. A missing label may be planned for creation only when `repository.label.create` is declared. There is no rename, recolour, description update or label deletion operation in v1.

Discovery keeps these catalogue states distinct:

| State | Diagnostic |
| --- | --- |
| Unknown | `label.catalog.unknown` |
| Unavailable | `label.catalog.unavailable` |
| Forbidden | `label.catalog.forbidden` |
| Missing exact declaration | Typed missing result, not success |
| Case-only collision | `label.discovery.case-conflict` |
| Multiple exact results | `label.discovery.ambiguous` |

## Feature and mutation authority

`issue-labels` means the complete repository label catalogue and complete issue-label membership needed by the requested workflow can be discovered. It is a requirement to probe, not capability evidence.

The three label scopes are exhaustive:

| Scope | Maximum authorised effect |
| --- | --- |
| `repository.label.create` | Create one missing declared label with its exact name, colour and description |
| `issue.label.add` | Add one exact declared label to one resolved issue |
| `issue.label.remove` | Remove one exact declared label from one resolved issue |

All require a non-empty declaration set and feature `issue-labels`. They do not authorise labels absent from the contract.

There is deliberately no set-all-labels scope because provider set endpoints can erase unrelated live labels. A planner owns only the declared label membership it explicitly changes and preserves every other observed label.

### Label-transition guard

Before lowering any label change, a pure guard consumes a complete observed before-state plus abstract `addRefs` and `removeRefs` sets. [V1 ordinary task interactions](v1-task-interactions.md) defines the ordinary request wire shape.

The guard evaluates in this order:

1. require a known complete before-state, otherwise return `labels.before-state.unknown`, `labels.before-state.unavailable` or `labels.before-state.forbidden`;
2. resolve every requested reference exactly and return `labels.change.unknown-ref` with exact sorted references for any miss;
3. return `labels.change.conflict` when one reference occurs in both requested sets;
4. reduce adding an already-present declared label and removing an absent declared label to no-ops;
5. require `issue.label.add` for every effective addition and `issue.label.remove` for every effective removal, otherwise return `labels.mutation.add-forbidden` or `labels.mutation.remove-forbidden`;
6. apply effective additions and removals as set operations while retaining every undeclared observed label;
7. run overall-Project route cardinality on the complete after-state;
8. run sub-project target and cardinality checks against the after-state target;
9. return exact sorted effective changes and exact sorted after-state names.

Route replacement and its associated sub-project replacement may therefore be one atomic valid plan. Removing a required route label without adding its one replacement is `routing.project-label.missing`. Adding a second route without removing the old one is `routing.ambiguous`. Adding a second sub-project or retaining one for the old target uses the ordinary sub-project diagnostics.

Mutation authority applies only to effective writes. A completely idempotent request is a valid no-op even under a read-only contract. Execution still performs stale guards against the before-state fixed by the eventual plan.

## Static semantic diagnostics

| Diagnostic | Condition |
| --- | --- |
| `contract.semantic.duplicate-label-selector` | Two declarations have the same exact provider name |
| `contract.semantic.label-case-collision` | Distinct names share the frozen ASCII case-collision key |
| `contract.semantic.unknown-label-target` | A project-route or sub-project role names no target |
| `contract.semantic.project-route-forbidden` | `repository_scope` declares a project-route label |
| `contract.semantic.project-route-count` | Required-label routing does not declare exactly one project route per target |
| `contract.semantic.unknown-route-label` | A dispatcher rule names no declared label |
| `contract.semantic.route-label-role` | A rule names a label that is not `project_route` |
| `contract.semantic.route-target-mismatch` | Rule target differs from the route label's role target |
| `contract.semantic.duplicate-route-label` | Two rules name one route label |
| `contract.semantic.duplicate-route-target` | Two rules select one target |
| `contract.semantic.incomplete-route-map` | Rule, route-label and target sets are not one-to-one |
| `contract.context.central-issue-store-requires-project-route` | `repository_scope` is used outside the participating issue-store repository |

The existing #14 target, issue-store, Project and field selector diagnostics remain unchanged.

The label-transition guard additionally uses:

| Diagnostic | Condition |
| --- | --- |
| `labels.before-state.unknown` | Complete issue labels have not been established |
| `labels.before-state.unavailable` | The provider cannot supply complete issue labels |
| `labels.before-state.forbidden` | The acting principal cannot read complete issue labels |
| `labels.change.unknown-ref` | A requested add or remove reference is undeclared |
| `labels.change.conflict` | One reference is requested for both add and remove |
| `labels.mutation.add-forbidden` | An effective addition lacks `issue.label.add` |
| `labels.mutation.remove-forbidden` | An effective removal lacks `issue.label.remove` |

## Decision corpora

The #47 corpus contains:

- contract fixtures under `testdata/contracts/v1/labels/`;
- exact combined issue-label routing and classification rows, without duplicate routing traces, at `testdata/labels/v1/cases.json`;
- exact repository label discovery/adoption rows at `testdata/labels/v1/discovery-cases.json`;
- exact mutation authority and after-state rows at `testdata/labels/v1/transition-cases.json`;
- amended dispatcher routing rows at `testdata/routing/v1/cases.json`.

Fixture values are synthetic. Every row preserves undeclared labels and proves that provider display text never becomes protocol identity.

## Invariant register

| ID | Rule |
| --- | --- |
| `LB-001` | Every repository contract explicitly contains `labels.declarations` and `labels.projectRouting`. |
| `LB-002` | Contract label references are identity; names are exact provider selectors. |
| `LB-003` | Role is explicit and never inferred from a name prefix. |
| `LB-004` | The only roles are general, project route and sub-project. |
| `LB-005` | Project-route and sub-project labels target one declared overall Project. |
| `LB-006` | Repository-scope routing is single-Project and valid only in the participating issue-store repository. |
| `LB-007` | Central issue stores and dispatchers require exactly one project-route label per issue. |
| `LB-008` | Dispatcher route rules, project-route labels and targets are one-to-one. |
| `LB-009` | Raw provider-name conjunctions and target fallbacks are absent. |
| `LB-010` | An issue has zero or one sub-project for its selected overall target. |
| `LB-011` | General and undeclared labels never route an issue. |
| `LB-012` | Undeclared live labels are preserved. |
| `LB-013` | Exact existing labels are adopted without colour or description reconciliation. |
| `LB-014` | Case conflict and ambiguity fail closed. |
| `LB-015` | V1 creates labels and adds/removes declared membership but never updates or deletes labels. |
| `LB-016` | Label writes require a known complete prior set and a cardinality-valid after-state. |
| `LB-017` | Idempotent additions and removals become no-ops and require no mutation authority. |
| `LB-018` | Route and sub-project replacements may be validated atomically but never expose an invalid after-state. |

## Interaction integration

[V1 ordinary task interactions](v1-task-interactions.md) defines the finite label-add and label-remove request actions, additive provider operations, canonical operation order and complete-plan behaviour. This specification remains authoritative for label identity, routing and sub-project cardinality.

## Explicitly deferred

This specification does not define:

- Project field dimensions, owned by #48;
- view and auto-add setup outcomes, owned by #49;
- version migration from the superseded pre-v1 `labelsAll` form, owned by #34 and #31;
- Go model, discovery or planner APIs;
- live GitHub tests.

Later work MUST use declared label references and these cardinality rules. It may not restore raw name predicates, infer roles from prefixes, replace sub-project labels with a Project field or use a set-all endpoint that erases unrelated labels.

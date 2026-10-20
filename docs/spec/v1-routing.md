# V1 dispatcher and routing specification

Current runtime behaviour is defined in [membership and storage migration](../../skills/github-projects/references/membership-storage-migration.md). Native membership is authoritative; Sub-project storage may be a configured single-select field. Label-only routing and required-label assumptions below describe historical compatibility, not current runtime authority.

Status: archived legacy `projectctl/v1` wire-format specification.

## Scope

This document defines the multi-Project dispatcher topology and the pure operation that resolves one GitHub Project target. It extends the [v1 single-Project repository contract](v1-single-project-contract.md) and uses the label declarations and cardinality rules in [v1 labels and sub-projects](v1-labels-and-subprojects.md).

It does not define provider discovery, private operator profiles, mutation planning or Go APIs.

## Frozen decisions

| Concern | V1 decision |
| --- | --- |
| Output cardinality | Exactly one target or one typed failure |
| Shared issue store | Every dispatcher target names the same issue store |
| Automatic dispatcher signal | Exactly one declared `project_route` label on the issue |
| Dispatcher rule | One stable label reference mapped to one exact target reference |
| Rule ordering | Unordered and never precedence |
| No matching route label | `routing.project-label.missing` |
| More than one route label | `routing.ambiguous` |
| Fallback | Error only; there is no target fallback |
| Explicit target selection | Separate mode that resolves an exact target reference |
| Existing Project membership | Observed state only; never a routing signal |
| Issue-store comparison | Owner login and repository name use frozen ASCII case-insensitive equality |
| Label-name comparison | Exact and case-sensitive |

The superseded pre-v1 `labelsAll` predicate and target fallback are invalid. There is no compatibility inference for either shape.

## Dispatcher wire shape

A dispatcher uses `kind: DispatcherRepository` and has this complete topology:

```yaml
apiVersion: projectctl/v1
kind: DispatcherRepository
spec:
  ref: shared-work
  targets:
    - target:
        ref: backend
        issueStore:
          owner: {kind: organization, login: example-org}
          repository: central-tasks
        project:
          owner: {kind: organization, login: example-org}
          number: 83
      fields: {}
      dimensionBindings: {}
    - target:
        ref: frontend
        issueStore:
          owner: {kind: organization, login: example-org}
          repository: central-tasks
        project:
          owner: {kind: organization, login: example-org}
          number: 84
      fields: {}
      dimensionBindings: {}
  dimensions: {}
  routing:
    rules:
      - {ref: backend-route, labelRef: route-backend, targetRef: backend}
      - {ref: frontend-route, labelRef: route-frontend, targetRef: frontend}
    fallback: {kind: error}
  labels:
    declarations:
      route-backend:
        name: project:backend
        color: 1f6feb
        description: Routes this issue to Backend.
        role: {kind: project_route, targetRef: backend}
      route-frontend:
        name: project:frontend
        color: 1f6feb
        description: Routes this issue to Frontend.
        role: {kind: project_route, targetRef: frontend}
    projectRouting: {kind: required_label}
  sourceRefs: []
  requirements:
    features: [issue-labels, issues, projects-v2]
    mutations: []
```

Every shown container is required. Null is invalid everywhere. A dispatcher has at least two targets, at least two rules, at least two label declarations and `projectRouting.kind: required_label`.

The schema at `schemas/v1/repository-contract.schema.json` is the structural authority.

## Targets

Each `spec.targets` entry contains exactly one base target, one custom-field map and one semantic dimension-binding map. The canonical dimension declarations are shared at `spec.dimensions`; [v1 project dimensions](v1-project-dimensions.md) requires every target to bind the same declared dimension set even when their provider storage kinds differ.

Static validation requires:

1. every target reference is unique;
2. every target uses the same exact issue-store locator under the frozen locator comparison;
3. every Project locator is unique under owner kind, ASCII case-insensitive owner login and exact Project number;
4. field selector and option selector uniqueness is checked independently inside each target;
5. dimension bindings are checked independently inside each target against the one shared canonical vocabulary;
6. no title, Project display name or declaration order acts as identity.

Targets are canonically ordered by exact target reference. Source order has no semantic meaning.

## Route rules

Each rule contains exactly:

- unique contract reference `ref`;
- one declared label reference `labelRef`;
- one target reference `targetRef`.

The route graph MUST be a bijection:

1. every `labelRef` resolves to one declaration whose role is `project_route`;
2. the label role and rule name the same target;
3. no two rules name the same label;
4. no two rules name the same target;
5. every target has exactly one rule;
6. every project-route declaration has exactly one rule.

`routing.fallback` is required and is exactly `{kind: error}`. Empty rules, raw provider names, conjunctions, disjunctions, priorities, weights, wildcards and target fallbacks are invalid.

Rules are canonically ordered by exact rule reference. Route matches and diagnostics are ordered by exact label reference so input label order cannot change a result.

## Equality

Contract references use exact Unicode scalar equality.

GitHub owner logins and repository names use ASCII case-insensitive equality: map only ASCII `A` through `Z` to lowercase for comparison. Preserve supplied spelling for provider calls and diagnostics. Owner kind and Project number remain exact.

Observed label names use exact Unicode scalar equality. In particular, `project:backend` does not match `Project:Backend`. Label discovery handles provider case collisions separately and fails closed.

No comparison is locale-sensitive. No value is trimmed, normalised or inferred by the resolver.

## Static diagnostics

The base dispatcher diagnostics are:

| Diagnostic | Condition |
| --- | --- |
| `contract.semantic.duplicate-target-ref` | A later canonical target repeats an exact target reference |
| `contract.semantic.mixed-issue-stores` | A later canonical target names a different issue store |
| `contract.semantic.duplicate-project-selector` | Two target references select the same Project |
| `contract.semantic.duplicate-route-ref` | A later canonical route repeats an exact route reference |
| `contract.semantic.duplicate-field-selector` | Two field references in one target select the same exact field name |
| `contract.semantic.duplicate-value-selector` | Two value references in one field select the same exact option name |

The label-to-route diagnostics, including unknown labels, wrong roles, mismatched targets and incomplete bijections, are defined in the label specification. A validator MAY return more than one semantic error, but each error MUST have its stable diagnostic and canonical JSON Pointer.

## Resolver inputs

One pure resolution request contains:

- a validated repository contract;
- a selection with kind `automatic` or `explicit`;
- an observed issue-store fact;
- an observed complete issue-label fact.

Explicit selection additionally contains one exact `targetRef`.

Issue-store and label facts have one state from:

| State | Meaning |
| --- | --- |
| `known` | A complete value is present |
| `unknown` | The caller has not established the fact |
| `unavailable` | The provider cannot supply the fact |
| `forbidden` | The acting principal cannot read the fact |

A known label fact is a set of exact names. Known empty and unknown are different. An adapter MUST NOT replace unknown, unavailable or forbidden with an empty set.

## Resolution algorithm

### Common issue-store gate

The resolver first:

1. maps issue-store state `unknown`, `unavailable` and `forbidden` to `routing.issue-store.unknown`, `routing.issue-store.unavailable` and `routing.issue-store.forbidden`;
2. compares a known locator with the contract's shared issue store;
3. returns `routing.issue-store.mismatch` when they differ.

No target or label is considered before this gate succeeds.

### Explicit mode

After the common gate:

1. resolve `selection.targetRef` by exact reference equality;
2. return `routing.target.unknown` if it does not resolve;
3. otherwise return the target with source `explicit` and no route label.

Explicit selection does not inspect label facts. It therefore works when labels are unknown, unavailable or forbidden. It is not a precedence override for automatic routing.

### Automatic repository-scope mode

When `labels.projectRouting.kind` is `repository_scope`, return the sole target with source `repository_scope` and no route label. Label facts are not needed for target selection.

Context validation permits this mode only when the contract's issue store is the participating repository represented by that contract. A central issue store requires `required_label`.

### Automatic required-label mode

After the common gate:

1. map label states `unknown`, `unavailable` and `forbidden` to `routing.labels.unknown`, `routing.labels.unavailable` and `routing.labels.forbidden`;
2. for a known set, map exact observed names to declared label references;
3. retain declarations whose role is `project_route`;
4. return `routing.project-label.missing` when none remain;
5. return `routing.ambiguous` with all route label and target references when more than one remains;
6. return the sole role target with source `rule` and that `routeLabelRef`.

All project-route declarations are evaluated. General, sub-project and undeclared labels do not match a route. The resolver never stops at the first match.

## Result and trace

Every result contains `status` and a deterministic trace.

A resolved result contains exactly:

- one `targetRef`;
- one source from `repository_scope`, `explicit` or `rule`;
- one `routeLabelRef` only for source `rule`, otherwise null.

A failed result contains one stable diagnostic and any ordered references specified by that diagnostic.

The trace contains:

- `issueStoreGate`, one of `matched`, `mismatched`, `unknown`, `unavailable` or `forbidden`;
- `evaluatedRouteLabelRefs`, exact sorted references considered during automatic required-label routing;
- `matchedRouteLabelRefs`, exact sorted matching references;
- `matchedTargetRefs`, aligned with the matched route references.

The three trace arrays are empty before label evaluation, in explicit mode and in repository-scope mode.

After target resolution, sub-project and general-label classification follows the label specification. A classification failure does not replace or reinterpret the selected overall target.

## Existing membership and mutation authority

Observed membership in zero, one or several declared Projects does not select a route, break ambiguity or invalidate a route result. Automatic fan-out is forbidden. A caller that deliberately needs separate work against several Projects makes separate explicit requests.

`sourceRefs` and `requirements` remain contract-wide. Mutation declarations apply only to the selected target and, for field writes, that target's mappings. Selection does not prove provider capability, permission, Project membership or mutation authority.

## Decision corpus

`testdata/routing/v1/cases.json` is the executable routing table. It covers:

- each routing mode;
- exact and case-mismatched labels;
- zero, one and multiple project-route labels;
- unknown, unavailable and forbidden facts;
- explicit selection;
- issue-store gating;
- canonical traces.

The classification and label-discovery tables are under `testdata/labels/v1/`.

## Invariant register

| ID | Rule |
| --- | --- |
| `RT-001` | One resolution returns exactly one target or one failure. |
| `RT-002` | A dispatcher has at least two targets sharing one issue store. |
| `RT-003` | Each target owns its own field mappings. |
| `RT-004` | Target and Project selectors are unique. |
| `RT-005` | Routing is explicit in every dispatcher. |
| `RT-006` | One route contains one label reference and one target reference. |
| `RT-007` | Route references are unique and unordered. |
| `RT-008` | Route rules, project-route labels and targets form a bijection. |
| `RT-009` | Automatic matching uses exact declared label identity. |
| `RT-010` | Fallback is error only. |
| `RT-011` | Zero route labels fails missing and multiple route labels fail ambiguous. |
| `RT-012` | Explicit mode does not inspect labels. |
| `RT-013` | The issue-store gate precedes every selection mode. |
| `RT-014` | Repository-scope mode is limited to its participating repository. |
| `RT-015` | Mutation scope is contract-wide but applies only to the selected target. |
| `RT-016` | Existing Project membership is never a routing signal. |

## Explicitly deferred

[V1 ordinary task interactions](v1-task-interactions.md) now defines request envelopes and operation lowering while reusing this specification unchanged for selection and traces.

This specification does not define:

- YAML loading, normalisation or diagnostic rendering, owned by #34;
- provider discovery and observed snapshots, owned by #35 and #38;
- Project field dimensions, owned by #48;
- create-once views, manual auto-add workflow expectations and setup target keys, defined by [v1 setup outcomes](v1-setup-outcomes.md);
- Go package or model APIs;
- live GitHub tests.

Later work MUST consume this specification and its corpora. It may not restore raw label predicates, route precedence, target fallback, Project-title matching or automatic fan-out.

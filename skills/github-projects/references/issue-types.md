# Issue Type and Class design

Use Class or Issue Type to describe **what kind of work item this is**. Keep that classification separate from routing, priority, status, due dates, milestones and native parent/sub-issue hierarchy.

The default vocabulary is deliberately rich enough to distinguish common kinds of work without adding a second functional-lane taxonomy.

| Class / Issue Type | Preferred colour | Use for |
| --- | --- | --- |
| Task | YELLOW | The ordinary fallback: a specific piece of work when no more informative type adds useful meaning. |
| Bug | RED | An unexpected fault, regression or incorrect behaviour. |
| Enhancement | GREEN | A bounded improvement to existing work, material, method, process or software. |
| Data | PINK | Acquisition, intake, stewardship, transformation or validation of source or derived data, including production of analysis-ready data. |
| Analysis | PURPLE | Work whose main output is a quantitative or analytical result, inference, evaluation or reproducible computation. |
| Deliverable | ORANGE | One bounded formal output or event that is handed over, submitted, presented, released, assessed or otherwise consumed as an output. |
| Documentation | GRAY | Durable guidance, records or reference material rather than a substantive project output. |
| Epic | BLUE | A broad coordination outcome that remains useful as a planning object while several independently meaningful pieces of work are tracked separately. |

`Task` is intentionally the fallback category. The more specific types are specialisations of ordinary work, not philosophically disjoint categories. Use a specific type only when it makes the issue easier to understand, filter or manage.

`Data` combines source-data and processed-data work because both have a dataset or data state as their main output. Use `Analysis` instead when the main output is a result, inference, evaluation or reproducible computation based on data.

`Deliverable` supersedes the narrower `Report` name. It includes reports, manuscripts, presentations, posters, submissions, assessments, grant applications, registered protocols, handovers, release packages and software releases. Supporting work remains its own type: for example, fixing a figure can be a Task or Enhancement while the final submitted manuscript is a Deliverable.

`Research` is not part of the default vocabulary. Exploratory or decision-oriented work can normally be a Task; work whose main outcome is an analytical result can be Analysis; work that develops or improves an existing method or system can be Enhancement. A repository may deliberately retain another local type when it carries a useful stable distinction.

## Hierarchy is independent of type

Parenthood and Class are independent. A Task, Deliverable, Analysis, Enhancement or other non-Epic type may have sub-issues and remain that type.

Use Epic only when the broad coordinating outcome is useful in its own right, usually because it spans several independently actionable pieces or phases. Do **not** make an issue an Epic merely because it is top-level, has children, or would otherwise leave the Project without a root issue.

A presentation can remain a Deliverable while having preparation children. A simulation analysis can remain Analysis while having data-generation, fitting and figure children. A Project may have several top-level issues and no single root Epic.

## Organising a whole Project

Apply this guidance only when the user explicitly authorises broad organisation, such as bootstrapping a new Project, migrating an existing task system, or substantially restructuring a poorly organised Project. Do not use it to silently reshape a Project during routine issue creation or a narrow field/body update.

- Treat the GitHub Project as the container. Do not manufacture a root issue whose outcome is merely to "do", "complete" or "manage" the Project, and do not make all other work descend from such an issue.
- Prefer top-level issues that describe independently meaningful outcomes, deliverables, analyses, decisions or bounded improvements. They should be useful to prioritise, defer, complete or cancel without referring to the whole Project.
- Use parent/sub-issue hierarchy for genuine scope containment. A child should materially contribute to completing its parent, and the parent should remain a useful planning object in its own right. Avoid category-only wrappers such as generic "Deliverables", "Analysis", "Supervision" or "Project management" issues when the children already stand on their own.
- Do not create standing issues for activity that is intrinsic to running the Project, such as "supervise the project", "keep the project moving" or "maintain the analysis". Create a concrete issue when there is a specific review, decision, change or output to track.
- Separate producing a result from reporting that result when they have different lifecycles. An Analysis issue can produce the scientific or quantitative result while a reporting task sits under the relevant Deliverable and links back to that analysis.
- Keep hierarchy as shallow as the work allows. Several top-level Epics or other outcomes are fine; there is no requirement for one root Epic. Add another level only when it makes planning clearer rather than merely making the tree look complete.
- Use body checkboxes for small, local steps where only completion within the parent matters and the step does not need its own priority, status, assignee, due date, discussion, dependency or relationship. Use a sub-issue when the work is independently actionable, substantial enough to track separately, or benefits from any of that independent planning state. If a checkbox grows into separately managed work, promote it to a sub-issue and remove or replace the old checkbox so the same work is not tracked twice.
- When repairing an imported Project, preserve useful history and meaning. Propose retiring or closing generic wrapper/standing issues only after their useful children, links and current state are represented directly.

## No standard Workstream dimension

The active project-administration model does not use Workstream as a standard semantic dimension. Do not require every issue to carry a second functional-lane classification such as `Methods`, `Analysis`, `Validation`, `Reporting` or `Administration`.

Use the existing dimensions instead:

- repository/Project topology and declared `project:*` labels route an issue to the correct overall Project;
- optional `subproject:*` labels provide genuine local grouping where needed;
- Class or Issue Type describes the kind of work item;
- native parent/sub-issue relationships provide scope and decomposition;
- Priority, Status and Due date carry planning state;
- issue titles and bodies carry the specific subject matter.

A live custom field named `Workstream` in an older Project is legacy/unmanaged state unless the repository deliberately documents it as non-standard metadata. The shared skill must not infer semantic meaning from that field name, require it, or populate it merely because it exists.

## Milestones are optional checkpoints

GitHub Milestones may be useful when several issues converge on one real temporal checkpoint, release or submission. They are optional and are not a replacement for Workstream.

A single formal output often needs only a Deliverable issue and a due date. Use a Milestone when the shared checkpoint itself helps organise several issues, such as a release, D3 submission or end-of-term assessment.

## Colours are presentational

Preferred colours make repeated types easier to recognise across Projects, but colour is not semantic state. GitHub's standard single-select palette is `BLUE`, `GRAY`, `GREEN`, `ORANGE`, `PINK`, `PURPLE`, `RED` and `YELLOW`; reuse colours when necessary. Preserve useful existing colours unless the requested outcome includes changing them.

## Migration and refinement

When standardising an existing Project, inspect the actual issues first and prefer useful distinctions over cosmetic churn.

- Map `Report` to `Deliverable` when the existing value represents formal outputs.
- Map `Raw data` and `Processed data` to `Data` unless the repository deliberately retains that distinction as useful local vocabulary.
- If an older Project uses `Research`, classify each affected issue as Task, Analysis or Enhancement according to the work rather than applying a blind one-to-one rename.
- Keep useful repository-specific Issue Types when they genuinely improve planning.
- Migrate or remove a legacy Workstream field only when that migration is explicitly authorised. Read current values before removal and verify the resulting Project and issue state afterwards.
- Do not rewrite closed history merely to make old records look identical unless the requested migration includes it.

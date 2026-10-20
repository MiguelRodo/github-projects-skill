# Membership routing and Sub-project storage

Native GitHub Project membership is current routing authority. A checked CLI
Project selector or the authenticated, unedited queue authority comment selects
an explicit target. Adding membership may name its target before membership
exists. Without explicit context, resolve only exactly one relevant managed
membership; zero or several is an error. Labels never break a membership tie.

The queue retains **one active Project request per issue**. Finish or withdraw
the active request before adding one for another Project. Discovery lists the
queue label alone, then inspects membership and authenticated authority comments.
An unplaced issue is discoverable only with explicit `project.membership.add`
intent. Completion rechecks authority and retains the queue on partial failure.
Existing ordinary task prose is never implementation authority.

## Stable per-Project storage

Record one binding in each resolved Project contract (and its implementation
mirrors). A configured single-select field is preferred:

```markdown
| Sub-project storage | field |
| Sub-project field | Sub-project |
| Sub-project field type | SINGLE_SELECT |

## Sub-project vocabulary
| Key | Provider value | Implementation repository |
| --- | --- | --- |
| tools | Tools | example/tools |
```

Field and option IDs are discovered fresh; name, type and exact option mapping
must agree. Missing, inaccessible or changed expected fields stop the operation.
Never fall back to labels after a failed field read. Creating a field is a
separate migration, not an implicit change of authority during an item write.

For a contract bound to field storage, `projects project setup-fields` creates
the configured single-select field with a `Not assigned` option even when the
vocabulary is empty. Existing options, IDs, colours and descriptions are
preserved. This option is schema only: setup never fills blank issue values,
and clearing a classification still leaves the field blank. It is not a real
sub-project key unless the contract explicitly declares it. Legacy label-bound
contracts remain unchanged until their separate migration.

If there is no configured field, use `Sub-project storage: labels` and declare
`Sub-project label scope: project` with labels such as
`subproject:work:tools`. Each vocabulary key maps to one exact label. A deliberate
cross-Project grouping may instead declare `shared` scope. Legacy v1 vocabulary
tables remain fixed label bindings until migrated; they do not select storage
from the result of each provider call. No dual-writable field/label binding.

Dispatcher routes use `-` in the legacy Routing label column and `membership`
in the leaf Routing row. Existing label routes remain readable during rollout.
New onboarding does not create redundant `project:*` labels. Never remove an
unrelated label, including another Project's grouping label.

## Direct and agentic commands

Validate the complete repository first. With the resolved contract and checked
target number, the repository helper supports direct selection and mutation:

```bash
python3 SKILL/scripts/subproject.py --contract .projects/projects/work.md --project-number 4 --issue 42 --select
python3 SKILL/scripts/subproject.py --contract .projects/projects/work.md --project-number 4 --issue 42 --set tools
python3 SKILL/scripts/subproject.py --contract .projects/projects/work.md --project-number 4 --issue 42 --set tools --apply
python3 SKILL/scripts/subproject.py --contract .projects/projects/work.md --project-number 4 --issue 42 --clear --apply
```

The helper defaults to planning. It verifies exact membership, expected schema,
requested value and preservation independently. Label storage ensures a missing
vocabulary label exists, adds the new grouping first, then removes only the old
label owned by this binding. A partial failure retains the queue and requires
fresh inspection. Agent envelopes use `subproject.value.set` with `value` equal
to the vocabulary key, or `subproject.value.clear` with no value. Place an explicit
membership-add action before grouping an unplaced issue. Existing `projects`
commands continue to handle membership and ordinary dimensions.

## Staged rollout and rollback

No production migration is part of deploying this code. Keep snapshots and
rollout evidence outside public repositories; journals contain issue content.

1. Inventory every managed contract, implementation mirror, queue launcher,
   installed skill, Global tasks/aggregation registry and other label consumer.
   Include newly managed Projects in that registry before retiring its route.
   Inspect live membership, fields/options, existing grouping and view filters.
2. Prepare target contracts and mirror PRs with one deterministic binding. Keep
   old contracts as source snapshots. Create any missing field/options through
   an explicitly approved setup operation. Verify exact schema before cutover.
3. Deploy upgraded discovery and storage consumers. Verify label-free and
   unplaced-intent discovery read-only. Pause writers for the bounded migration.
   Record deployed revision and verification evidence, including aggregation.
4. Run `migrate-project-storage.py` with `--source OLD.md --target NEW.md
   --issues 42 43 --journal /private/path/snapshot.json --mode dry-run`, then
   `--mode snapshot`. Bound issue numbers explicitly; reconcile them against a
   complete live membership inventory. The tool refuses ambiguous or conflicting
   source/target values. No provider writes occur in these modes.
5. Supply `--mode apply --rollout-evidence /private/path/evidence.json` only after
   the rollout gates below hold. Omit `--owned-routing-label` during initial
   preparation. Add the exact legacy declared route label only in a separately
   reviewed snapshot after all route consumers and setup are verified. The
   durable journal is saved before any write. It permits readback-based resume
   after partial failure; it never infers success from an exit status.
6. Run `--mode verify`. Only then activate target contracts, resume writers and
   replace existing classification view filters through the supported API or
   browser workflow. Preserve table columns/grouping/sort and useful extras.
   Existing boards and roadmaps must remain intact. Never strip labels before
   upgraded discovery is deployed and verified.
7. For rollback, pause writers again, use the same contract digests and journal,
   and run `--mode rollback --rollout-evidence ...`, then `--mode verify`.
   Restore the source contract revision and the snapshotted filters separately.
   A per-Project migration retains legacy shared grouping labels when the issue has other memberships; retire those only after every sharing consumer is independently verified. The tool restores only recorded item values and exact labels, refusing
   unrelated concurrent changes. Retain journals until rollout is accepted.

Evidence JSON must include `targetDigest` from the snapshot, `writersPaused`,
`consumersMigrated`, `discoveryVerified`, `membershipVerified`, `setupVerified`
(all true), a `deploymentRevision`, and a `verificationRecord` pointing to the
operator's actual evidence. These assertions are an explicit human verification
boundary, not a substitute for performing those checks. The migration tool does
not deploy consumers, edit registries, create fields, publish contracts, or
rewrite live view filters implicitly.

## New view defaults and supported boundary

Use `project-views.py --contract CONTRACT --project-number NUMBER --name All`
for an overall table, or add `--name Tools --subproject tools` for a filtered
table. Review the plan, then add `--apply`. It uses documented `gh api` REST
Project view creation and GraphQL readback. It never replaces existing views.
An existing exact match is an idempotent no-op; an existing mismatch is preserved
and reported for explicit reconciliation. `projects project setup-backlog-view`
also preserves existing boards/roadmaps and useful table extras.

Every new overall or Sub-project view is TABLE, grouped by Status, with Priority
ascending. Show Title, Due date and Target date where existing, Priority, Status,
Class (native Type for organisation Projects), and Sub-issues progress where
existing. All/Backlog additionally shows Sub-project for field storage or Labels
for label storage; filtered views use the exact configured classification and
hide that redundant column. Do not create missing date fields implicitly.

The API returns visible columns without a reliable display order, and native
Type may be omitted/rejected by the public view API. The command reports an
explicit browser verification boundary for order and native Type. Confirm both
in GitHub, preserve useful existing extra columns, and do not use undocumented
endpoints or create credentials/permissions to bypass API limitations.

Supported provider references: [REST Project views](https://docs.github.com/en/rest/projects/views), [GraphQL Issues](https://docs.github.com/en/graphql/reference/issues), [GraphQL Projects](https://docs.github.com/en/graphql/reference/projects), and [Project filtering](https://docs.github.com/en/issues/planning-and-tracking-with-projects/customizing-views-in-your-project/filtering-projects).

# Onboard by Project intent

Start in the implementation repository. Choose a managed Project key and an
optional sub-project; the tooling derives routing labels and contracts.

```bash
# Natural issue repository
pj --init --project work --project-owner example --project-number 40
# Central issue store
pj --init --project work --issue-store example/issues --project-owner example --project-number 40
# A sub-project implemented by the current repository
pj --init --project work --issue-store example/issues --subproject tools --project-owner example --project-number 40
```

Without pj, pass the same arguments to
`bash .agents/skills/github-projects/scripts/init-project.sh`.
The semantic path needs Python 3 and authenticated gh. Missing Project number
is prompted; an existing checked Project identity and issue store are reused.
Owner defaults to the implementation repository owner for a new Project.
New contracts default to collaborative governance; `--governance personal`
explicitly selects solo administration. Existing governance and overrides are
preserved. Explicit changes that disagree with existing governance stop for
review rather than silently replacing it.

The issue store must have exactly one checkout under
`${PJ_WORKSPACE:-~/planning}` (or the helper's `--workspace DIRECTORY`). A checked
dispatcher identity or exact GitHub origin identifies it; a directory's name is
never a repository identity. Nothing clones a missing repository.

## What reconciliation changes

For natural issues, the helper creates a single contract with a Project key and
optional sub-project vocabulary. Membership scopes the Project; there is no
Project routing label.

For a central store, it prepares the store's dispatcher and Project child,
plus the implementation repository's single contract. Both sides agree on
issue repository, Project identity and `project:KEY` routing. Sub-project
vocabulary uses `subproject:KEY` on both sides. Only missing declared routing
and grouping labels are created, followed by independent readback. Existing
labels and all unrequested contract sections are preserved.

The central child binds either its overall scope (`Implementation repository`
metadata) or individual vocabulary entries (`Implementation repository` column)
to checked implementation identities. Implementation contracts carry their own
identity and `Queue source | mirror`: these are context copies, while the store
owns queue discovery. Project/sub-project queue selectors therefore use the
store's issue queue and the implementation repository's instructions.

Existing single, dispatcher, custom child paths and legacy vocabulary tables
remain supported. Membership-only legacy implementation contracts can adopt
the derived central route; existing different labels/identities, orphan children,
ambiguous checkouts, sub-project implementation collisions and unsafe paths are
conflicts. Existing unbound sub-project entries need review before binding;
the tool never guesses which repository implements them. A matching legacy single-Project issue store can become a dispatcher: its
complete existing contract is preserved as the selected child. A different
Project or conflicting route requires a separate reviewed migration.

## Preview, apply and publication

The helper stages and validates the complete contract sets in temporary
folders before changing either checkout or GitHub. It displays affected files,
Project/sub-project identity and missing labels. Cross-checkout work requires
confirmation or `--yes`; cancellation writes nothing. It checks the inputs
again after confirmation and refuses uncommitted changes to files it would edit.
Unrelated working-tree/index changes are preserved.

On default/main/master branches it creates local `onboarding/KEY` branches;
on other branches it keeps the current branch. It does not commit, push or
merge. Follow each repository's AGENTS.md and normal commit/PR workflow for
publication. Review both repositories together. If a branch cannot be prepared,
no contracts are written and any newly prepared branches are restored.

Local write/validation failure restores the original files. GitHub label
operations cannot be transactional with local files: an uncertain write or
readback failure stops, reports labels that may remain, and leaves both
contracts unchanged. Inspect provider state before retrying. A repeated,
already-consistent run writes nothing and needs no confirmation.

This path does not create Projects, migrate issues or change existing Project
membership. New issues still follow the resolved skill's routing and membership
rules. Standard fields/Backlog setup remains a separate optional operation:

```bash
projects project setup-fields --apply
projects project setup-backlog-view --apply
```

Organisation schema changes retain their separate explicit approval requirement.
The original no-option initializer continues to offer repository-backed single
or multiple Project setup and the established live profile setup. A separate
issue store selected there enters semantic reconciliation.

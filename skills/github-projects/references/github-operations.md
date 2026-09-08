# Proven GitHub operations

These are direct-provider fallback recipes and supplementary reads. Before choosing one, check the execution surface in `SKILL.md`: when `projects` is installed and supports the operation, use [the CLI execution reference](projects-cli.md). The native commands below do not override that preference. Read this reference before performing or returning a direct-provider mutation. Substitute values only after resolving the repository contract and inspecting live state. Never hard-code credentials or reuse provider IDs from examples.

Use the current stable GitHub REST API version declared by GitHub documentation. The examples below use `2026-03-10`.

## Baseline inspection

Inspect ordinary issue state:

```bash
gh issue view "$ISSUE_NUMBER" --repo "$REPOSITORY" \
  --json number,title,state,labels,assignees,milestone,projectItems,url
```

Inspect the REST issue identity when a database ID is required:

```bash
gh api "repos/$REPOSITORY/issues/$ISSUE_NUMBER" \
  --jq '{id,number,state,url:.html_url}'
```

For Project identity and fields, GraphQL works for both user-owned and organisation-owned Projects and exposes option IDs. Discover the owner type first, then select exactly one GraphQL root. Do not query both roots in one request: GitHub may reject the inapplicable root instead of returning `null`.

```bash
observed_owner_type="$(gh api "users/$PROJECT_OWNER" --jq .type)"
case "$observed_owner_type" in
  User) project_owner_type="user" ;;
  Organization) project_owner_type="organization" ;;
  *) echo "Unsupported Project owner type: $observed_owner_type" >&2; exit 1 ;;
esac

case "$project_owner_type" in
  user) project_owner_root="user" ;;
  organization) project_owner_root="organization" ;;
esac

gh api graphql \
  -f query="query(\$login: String!, \$number: Int!) {
    ${project_owner_root}(login: \$login) {
      projectV2(number: \$number) { ...ProjectData }
    }
  }
  fragment ProjectData on ProjectV2 {
    id number title
    fields(first: 100) {
      nodes {
        __typename
        ... on ProjectV2FieldCommon { id name dataType }
        ... on ProjectV2SingleSelectField { options { id name } }
      }
      pageInfo { hasNextPage }
    }
  }" \
  -F login="$PROJECT_OWNER" \
  -F number="$PROJECT_NUMBER"
```

If the repository contract declares `Owner type`, compare it with the discovered value and stop on disagreement. Require one non-null Project at the discovered root. Stop if field pagination reports another page rather than silently ignoring fields.

When `projects` is installed, prefer its count-checked read for a complete Project inventory. Run it from the participating repository root and supply an exact route selector for a dispatcher:

```bash
projects project item-list --format json
projects project item-list --project-key "$PROJECT_KEY" --format json
```

The command validates the local contract, verifies the live Project identity and fails when the number of returned items differs from GitHub's reported total. It does not mutate GitHub.

When the CLI is unavailable, use `gh project item-list` with an explicit generous limit. The command's default page can omit a newly created or newly added item on a non-trivial Project and make a successful mutation look like a failure.

```bash
gh project item-list "$PROJECT_NUMBER" \
  --owner "$PROJECT_OWNER" \
  --format json \
  --limit 1000
```

Treat an unbounded/default `gh project item-list` result as incomplete evidence, not proof that an item is absent. If a target is still absent after a sufficiently broad read, then use an exact GraphQL lookup or diagnose the preceding mutation. Do not add the same issue again merely because the first read used the default page. Do not assume Project item output includes organisation-native issue fields.

Observed provider limitation: with GitHub CLI 2.98.0, `gh project field-list NUMBER --owner USER --format json` can fail with `unknown owner type` for a user-owned Project. Use GraphQL. Do not interpret that message as proof that the Project is absent.

## Reliable command selection and fallbacks

Prefer one deliberate execution path for each property rather than cycling through equivalent CLI, REST and GraphQL mutations.

Use `projects project item-add --issue NUMBER --apply` for ordinary issue-to-Project membership when supported. Its targeted inspection and independent readback make a separate full-Project inventory unnecessary. When that command is unavailable:

1. inspect the issue and current Project membership;
2. use `gh project item-add` once when membership is authorised;
3. read the Project again with an explicit generous `--limit` and capture the resulting item ID;
4. only if `gh project item-add` itself returns a reproducible provider/CLI limitation, use the equivalent GraphQL mutation;
5. after membership is verified, edit Project fields and independently read them back.

A successful mutation followed by an incomplete read is a readback problem until proven otherwise. Re-run the read correctly before changing mutation mechanism.

When a command fails because of a deterministic permission, eligibility or authentication condition, stop and report that condition. Do not retry the same requested change through several equivalent endpoints in the hope that one bypasses GitHub's access rules. Use another surface only for a documented capability gap or provider-specific CLI limitation.

For exact Project item diagnosis, GraphQL is an appropriate fallback because it can target the Project and issue node IDs directly. It should not be the first response to ordinary pagination mistakes.

## Organisation-native issue fields

Discover the field and its exact option names immediately before use:

```bash
gh api \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2026-03-10" \
  "orgs/$ORGANIZATION/issue-fields"
```

Inspect all current issue-field values:

```bash
gh api \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2026-03-10" \
  "repos/$REPOSITORY/issues/$ISSUE_NUMBER/issue-field-values"
```

Use `POST` to add or update only the named field. GitHub accepts the exact single-select option name as `value`:

```bash
gh api --method POST \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2026-03-10" \
  "repos/$REPOSITORY/issues/$ISSUE_NUMBER/issue-field-values" \
  --input - <<JSON
{"issue_field_values":[{"field_id":$ISSUE_FIELD_ID,"value":"$PROVIDER_VALUE"}]}
JSON
```

Do not use `PUT` for a one-field change. The set endpoint replaces all existing issue-field values. Read the values again with `GET` and verify the field ID, option name and preservation of other values.

Classic personal access tokens need `read:org` to list organisation issue fields. Fine-grained credentials need the relevant organisation Issue Fields permission plus repository Issues write permission for mutations. Report the exact permission failure instead of guessing.

## Project fields

`gh project` can create, list and delete fields, but it does not currently expose a command for editing the options of an existing single-select field. When the authorised outcome changes a field definition, use GraphQL `updateProjectV2Field`.

This mutation replaces the supplied `singleSelectOptions` collection. The same rule applies when adding an option or changing names, descriptions or colours. To change the authorised properties without clearing existing item values:

1. query the exact field and its complete option collection, including each option's `id`, `name`, `color` and `description`;
2. re-query immediately before writing and stop if the field or option collection changed;
3. send every existing option back with its existing `id`, changing only the authorised properties; append a genuinely new option without an `id`;
4. re-query the complete option collection and affected Project items independently after writing;
5. verify that every previous option ID and item value is preserved and that the exact requested option properties changed.

Use this mutation shape with a JSON variables object assembled from the fresh query result:

```graphql
mutation UpdateSingleSelectOptions(
  $fieldId: ID!
  $options: [ProjectV2SingleSelectFieldOptionInput!]!
) {
  updateProjectV2Field(
    input: {fieldId: $fieldId, singleSelectOptions: $options}
  ) {
    projectV2Field {
      ... on ProjectV2SingleSelectField {
        id
        name
        options { id name color description }
      }
    }
  }
}
```

Do not omit existing option IDs, because GitHub uses them to preserve option identity and item values. Do not remove, rename, recolour or reorder another option unless that separate change is explicitly authorised.

For contract-declared field values, use `projects project item-edit` when supported; changing the field definition itself still needs the direct recipe above. When the required field-value operation is unsupported, discover the current Project item, Project field and option IDs, then update one single-select field:

```bash
gh project item-edit \
  --id "$PROJECT_ITEM_ID" \
  --project-id "$PROJECT_ID" \
  --field-id "$PROJECT_FIELD_ID" \
  --single-select-option-id "$OPTION_ID"
```

For a date field, replace the final option with `--date YYYY-MM-DD`. For a name-based interactive form, `gh project item-edit NUMBER --owner OWNER --url ISSUE_URL --field FIELD --value VALUE` is available, but ID-based scripts make the exact target easier to audit.

Re-query the item and affected field after the mutation. Do not use the command's success status as readback.

Adding an issue to a Project is a separate mutation. If `projects project item-add` is unavailable, use:

```bash
gh project item-add "$PROJECT_NUMBER" \
  --owner "$PROJECT_OWNER" \
  --url "$ISSUE_URL"
```

Perform it only when membership itself is authorised or necessarily implied by an authorised Project-field change and allowed by the local contract. Read membership back with an explicit generous `--limit` before editing fields. If the command itself fails because of a documented CLI/provider limitation, use GraphQL deliberately; do not switch mutation mechanisms merely because a default-page readback omitted the item.

## Ordinary issue mutations

Use `projects issue create` or `projects issue edit` for supported ordinary issue mutations. When the required command/flag is unavailable, prefer the narrow native command that owns only the requested property, for example:

```bash
gh issue edit "$ISSUE_NUMBER" --repo "$REPOSITORY" --add-assignee "$LOGIN"
gh issue close "$ISSUE_NUMBER" --repo "$REPOSITORY" --reason completed
gh issue reopen "$ISSUE_NUMBER" --repo "$REPOSITORY"
```

Before assigning a user, check that GitHub considers the login assignable to the repository:

```bash
gh api "repos/$REPOSITORY/assignees/$LOGIN" >/dev/null
```

A successful response means the user is eligible for assignment. A not-found or permission response is a real eligibility/access condition: report it and stop instead of trying REST, GraphQL and `gh issue edit` variants in succession. If the preflight succeeds but assignment still fails, inspect the exact error and issue state once before choosing a documented fallback.

For labels, use additive or subtractive flags rather than replacing the full label set. Read the issue again after the command.

Issue bodies and comments are collaborative text. Replace a body only when the request explicitly authorises that exact replacement and the pre-write body still matches the inspected version.

## Native hierarchy

Resolve the child issue's current REST database ID, inspect both issues and their existing hierarchy, then add one sub-issue:

```bash
gh api --method POST \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2026-03-10" \
  "repos/$REPOSITORY/issues/$PARENT_NUMBER/sub_issues" \
  -F sub_issue_id="$CHILD_DATABASE_ID"
```

Verify through the sub-issues endpoint or a GraphQL issue query. Do not represent the relationship only with a Markdown checklist.

## Command-returning surfaces

When the current surface cannot write:

1. resolve every safe value it can read;
2. return only the commands required for the delta and readback in one terminal-ready block;
3. keep discovered non-secret values literal;
4. leave a placeholder only for a fact the user must supply;
5. state that the command has not run.

Ordinary handoffs are commands to paste directly into a terminal, not files for the user to create. Do not add a shebang, `set -e`, `set -u`, `set -o pipefail` or script boilerplate to a short command block. Use a named script only when the operation is long enough that pasting it would be awkward or unreliable.

Check authentication at the start without forcing a new login when `gh` is already authenticated. A short block may use this shape:

```bash
if gh auth status; then
  # focused operation and independent readback
else
  echo "Authenticate gh, then paste this block again."
fi
```

Prefer commands that are safe to paste again after a partial run. Inspect before creating a resource, skip a change that already has the requested value, and keep the final readback useful even when the mutation became a no-op. Do not blindly retry an uncertain create.

If a pasted block fails, accept either the complete terminal output or the exact command where it stopped. Re-inspect live state because earlier commands may already have succeeded, then return only the corrected or remaining copy-paste commands. Do not make the user recreate a script merely to recover.

Avoid Python when `gh --json`, `--jq`, shell or `jq` is simpler. When Python is genuinely useful, discover an available command from `python3`, `python` and `py` rather than assuming its name or version.

If a failure reveals a reusable error mode in this skill or its provider recipes, offer to prepare a focused pull request for `MiguelRodo/github-projects-skill`. Do not create the issue or pull request unless the user explicitly accepts. If accepted, remove credentials and private repository content from the reproduction.

Do not return a broad command block that rewrites unrelated state when one focused `gh` operation and one readback suffice.

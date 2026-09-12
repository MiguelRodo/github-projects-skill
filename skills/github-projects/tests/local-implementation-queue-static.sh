#!/usr/bin/env bash
set -Eeuo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || exit 1
skill_dir="$(cd "$test_dir/.." && pwd)" || exit 1
repo_root="$(cd "$skill_dir/../.." && pwd)" || exit 1
reference="$skill_dir/references/local-implementation-queue.md"
installed_reference="$repo_root/.agents/skills/github-projects/references/local-implementation-queue.md"
contract="$repo_root/.projects/project.md"

[ -f "$reference" ]
[ -f "$installed_reference" ]
[ ! -e "$repo_root/.github/workflows/project-admin-bridge.yml" ]

cmp -s "$reference" "$installed_reference"
grep -Fq 'pj:implement-chat' "$reference"
grep -Fq 'PJ implementation authority:' "$reference"
grep -Fq 'created_at == updated_at' "$reference"
grep -Fq 'do not ask the operator for a routine preview or confirmation' "$reference"
grep -Fq 'administrative-only' "$reference"
grep -Fq 'Queue mode must never perform the substantive work a task represents.' "$reference"
grep -Fq 'edit application or repository files as part of the underlying task' "$reference"
grep -Fq 'implement product, code or configuration changes' "$reference"
grep -Fq 'run implementation tests merely to perform the task' "$reference"
grep -Fq 'collect measurements or perform research, analysis or data work requested by the task' "$reference"
grep -Fq 'create implementation branches or pull requests' "$reference"
grep -Fq 'delegate the substantive task to another coding agent' "$reference"
grep -Fq 'Substantive work is never queue-executable' "$reference"
grep -Fq 'separate explicit non-queue invocation' "$reference"
grep -Fq 'Chat implementation label | pj:implement-chat' "$contract"

# The boundary is an effect boundary, not a request-type boundary.
grep -Fq 'The boundary is an effect boundary' "$reference"
grep -Fq 'Queue mode may freely use code and tooling' "$reference"
grep -Fq 'Administrative-only constrains the resulting effects, not the mechanisms available to the agent.' "$reference"
grep -Fq "do not skip the item's administrative work because the substantive work exists" "$reference"

# An existing ordinary task issue remains an administrable queue record.
grep -Fq 'an existing ordinary task issue whose GitHub or Project administration should be reconciled.' "$reference"
grep -Fq "It is not queue execution authority, and it must never cause the issue's administrative work to be skipped." "$reference"
grep -Fq 'perform the administrative portion and leave the substantive work untouched' "$reference"
grep -Fq 'A trusted existing task issue needs no separate authority comment.' "$reference"
grep -Fq 'does not by itself make an issue unusual' "$reference"
grep -Fq 'Do not ask the operator to confirm a routine reconciliation of that kind.' "$reference"
grep -Fq 'For an existing task issue, do not close it merely because its administration is complete.' "$reference"

# The stronger authority-comment model survives for handoffs and unusual mutations.
grep -Fq '### Temporary handoffs' "$reference"
grep -Fq 'Require the unedited authority comment described below when the requested administrative outcome is unusual or explicit' "$reference"
grep -Fq 'close the temporary handoff issue as completed' "$reference"

# Authority depends on the resolved contract's collaboration mode.
grep -Fq '### Who is acting' "$reference"
grep -Fq '"Currently authenticated user" always means the account that the local authenticated `gh` session reports to `pj`.' "$reference"
grep -Fq '### Determine the collaboration mode' "$reference"
grep -Fq 'a `Governance` metadata row: `personal` declares solo administration, and `collaborative` or `shared` declares collaborative administration;' "$reference"
grep -Fq 'Treat the mode as solo only when the resolved contract states solo or personal administration explicitly and consistently.' "$reference"
grep -Fq 'Use the collaborative rule when governance is missing, generic, contradictory, unrecognised or self-inconsistent' "$reference"
grep -Fq '### Solo administration' "$reference"
grep -Fq '### Collaborative administration' "$reference"
grep -Fq 'the issue body is mutable text that other people may edit, so the queue label alone is not administrative authority' "$reference"
grep -Fq 'The comment must state the administrative delta itself.' "$reference"
grep -Fq 'are not authority, because that text can change after the comment is written' "$reference"
grep -Fq 'Collaboration mode: collaborative administration in a public repository.' "$contract"
grep -Fq 'Collaboration mode: solo administration in a private repository.' \
  "$skill_dir/references/repository-contract.md"
grep -Fq 'is treated as collaborative' "$skill_dir/references/repository-contract.md"
grep -Fq 'solo administration' "$skill_dir/SKILL.md"
grep -Fq 'solo administration' "$repo_root/README.md"
grep -Fq 'solo administration' "$skill_dir/README.md"

# Synthetic worked regression examples stay pinned to the effect boundary.
grep -Fq '| "Build X", with an explicit `Class`, `Priority` and `Status` metadata line |' "$reference"
grep -Fq '| "Measure production behaviour" |' "$reference"
grep -Fq '| "Fix bug Y" |' "$reference"
grep -Fq 'building X' "$reference"
grep -Fq 'performing any measurement' "$reference"
grep -Fq 'editing repository files, running the repository test suite, opening a fix pull request' "$reference"

# The main skill and public documentation state the same boundary.
grep -Fq 'The queue is administrative-only by effect.' "$skill_dir/SKILL.md"
grep -Fq "never causes the issue's administrative work to be skipped" "$skill_dir/SKILL.md"
grep -Fq 'effect boundary' "$repo_root/README.md"
grep -Fq 'effect boundary' "$skill_dir/README.md"
grep -Fq 'Queue mode is administrative-only by effect' \
  "$skill_dir/references/provider-project-instructions.md"

if grep -Fq 'The queue supports one shape only' "$reference"; then
  echo 'ERROR: queue still excludes existing task issues from administration' >&2
  exit 1
fi

if grep -Fq 'Do not mark the underlying implementation/task issue itself with the queue label' "$reference"; then
  echo 'ERROR: queue still refuses to label an existing task issue' >&2
  exit 1
fi

if grep -Fq 'Implementation requests are never queue-executable' "$reference"; then
  echo 'ERROR: queue still uses a request-type boundary that skips whole items' >&2
  exit 1
fi

if grep -Fq 'For a queue issue that satisfies both trusted-author rules above' "$reference"; then
  echo 'ERROR: queue still requires an authority comment for every trusted item' >&2
  exit 1
fi

if grep -Fq 'Repository implementation work' "$reference"; then
  echo 'ERROR: queue still contains repository implementation execution guidance' >&2
  exit 1
fi

# The label-only shortcut must stay scoped to solo administration, and the
# unedited-comment requirement to collaborative administration.
solo_line="$(grep -n '^### Solo administration$' "$reference" | head -n 1 | cut -d: -f1)"
label_line="$(grep -n 'A trusted existing task issue needs no separate authority comment\.' "$reference" |
  head -n 1 | cut -d: -f1)"
if [[ -z "$solo_line" || -z "$label_line" || "$label_line" -lt "$solo_line" ]]; then
  echo 'ERROR: the label-only authority shortcut is not scoped to solo administration' >&2
  exit 1
fi

collaborative_line="$(grep -n '^### Collaborative administration$' "$reference" | head -n 1 | cut -d: -f1)"
delta_line="$(grep -n 'The comment must state the administrative delta itself\.' "$reference" |
  head -n 1 | cut -d: -f1)"
if [[ -z "$collaborative_line" || -z "$delta_line" || "$delta_line" -lt "$collaborative_line" ]]; then
  echo 'ERROR: the delta-stating authority comment is not scoped to collaborative administration' >&2
  exit 1
fi

if grep -R -Fq 'PROJECTS_TOKEN' "$repo_root/.github/workflows" 2>/dev/null; then
  echo 'ERROR: repository workflow still references PROJECTS_TOKEN' >&2
  exit 1
fi

if grep -R -Fq 'OPENAI_API_KEY' "$repo_root/.github/workflows" 2>/dev/null; then
  echo 'ERROR: repository workflow still references OPENAI_API_KEY' >&2
  exit 1
fi

echo 'local administration queue static tests passed'

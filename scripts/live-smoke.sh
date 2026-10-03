#!/usr/bin/env bash
# Live smoke test for the `projects` CLI against a disposable sandbox
# repository and Project. It performs real GitHub mutations with the
# operator's existing `gh` login, so it refuses any repository whose name
# does not end in "-sandbox". See docs/live-smoke.md.

set -uo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/live-smoke.sh [--build] [--keep] [--help]

Runs the projects CLI with --apply --json against a disposable sandbox and
checks every outcome through the CLI's JSON and independent `gh api` reads.

Options:
  --build   go build ./cmd/projects from this checkout into a temp dir and test it
  --keep    skip the final clean-up (issues stay open, items stay visible)
  --help    show this help

Environment:
  PROJECTS_BIN          projects binary to test (default: `projects` on PATH)
  SMOKE_REPO            sandbox repository   (default: MiguelRodo/projects-cli-sandbox)
  SMOKE_PROJECT_OWNER   sandbox Project owner (default: MiguelRodo)
  SMOKE_PROJECT_NUMBER  sandbox Project number (default: 44)
  SMOKE_ROOT            existing sandbox checkout to use as --root (default: temp clone)
  SMOKE_SETTLE          seconds to wait before re-reading workflow-affected state (default: 8)

The repository name must end in "-sandbox", and the checkout's contract must
name exactly SMOKE_REPO and SMOKE_PROJECT_OWNER/SMOKE_PROJECT_NUMBER.
EOF
}

BUILD=0
KEEP=0
for arg in "$@"; do
  case "$arg" in
    --build) BUILD=1 ;;
    --keep) KEEP=1 ;;
    -h | --help) usage; exit 0 ;;
    *) echo "live-smoke: unknown argument: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

REPO=${SMOKE_REPO:-MiguelRodo/projects-cli-sandbox}
PROJECT_OWNER=${SMOKE_PROJECT_OWNER:-MiguelRodo}
PROJECT_NUMBER=${SMOKE_PROJECT_NUMBER:-44}
SETTLE=${SMOKE_SETTLE:-8}

# --- Guard: never point this at real work. --------------------------------
if [[ ! "$REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  echo "live-smoke: SMOKE_REPO must be OWNER/NAME, got '$REPO'" >&2
  exit 2
fi
REPO_OWNER=${REPO%%/*}
REPO_NAME=${REPO#*/}
if [[ "$REPO_NAME" != *-sandbox ]]; then
  echo "live-smoke: refusing to run: repository '$REPO' does not end in '-sandbox'" >&2
  exit 2
fi
if [[ ! "$PROJECT_NUMBER" =~ ^[0-9]+$ ]]; then
  echo "live-smoke: SMOKE_PROJECT_NUMBER must be a number, got '$PROJECT_NUMBER'" >&2
  exit 2
fi

for tool in gh jq git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "live-smoke: $tool is required" >&2; exit 2; }
done

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CHECKOUT=$(cd "$SCRIPT_DIR/.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/projects-live-smoke.XXXXXX")
OUT=$TMP/stdout
ERR=$TMP/stderr
LAST_CMD=""
RC=0

now_ms() { date +%s%3N; }
START_MS=$(now_ms)
RUN_ID="$(date -u +%Y%m%d%H%M%S)-$(printf '%04x' $((RANDOM % 65536)))"

# --- Binary selection. ------------------------------------------------------
if ((BUILD)); then
  command -v go >/dev/null 2>&1 || { echo "live-smoke: --build needs go" >&2; exit 2; }
  mkdir -p "$TMP/bin"
  (cd "$CHECKOUT" && go build -o "$TMP/bin/projects" ./cmd/projects) || {
    echo "live-smoke: go build failed" >&2; exit 2; }
  PROJECTS=$TMP/bin/projects
elif [[ -n "${PROJECTS_BIN:-}" ]]; then
  PROJECTS=$PROJECTS_BIN
else
  PROJECTS=$(command -v projects || true)
fi
if [[ -z "$PROJECTS" || ! -x "$PROJECTS" ]]; then
  echo "live-smoke: no executable projects binary (set PROJECTS_BIN or use --build)" >&2
  exit 2
fi

# --- Output helpers. --------------------------------------------------------
declare -a STEP_NAMES=() STEP_RESULTS=() STEP_MS=()
declare -a CREATED=()
FAILS=0

note() { printf '    %s\n' "$*"; }

# cli GROUP COMMAND [flags...]: run the CLI against the sandbox checkout.
cli() {
  local group=$1 command=$2
  shift 2
  LAST_CMD="projects $group $command --root <sandbox> $*"
  "$PROJECTS" "$group" "$command" --root "$ROOT" "$@" >"$OUT" 2>"$ERR"
  RC=$?
}

show_last() {
  note "last command: $LAST_CMD"
  note "exit status: $RC"
  note "stdout:"
  sed 's/^/      /' "$OUT"
  note "stderr:"
  sed 's/^/      /' "$ERR"
}

fail() {
  note "FAIL: $*"
  return 1
}

expect_rc() {
  [[ "$RC" == "$1" ]] || fail "expected exit $1 from CLI, got $RC"
}

# j FILTER: read the last CLI stdout as JSON.
j() { jq -r "$1" "$OUT" 2>/dev/null; }

assert_eq() { # assert_eq DESCRIPTION ACTUAL EXPECTED
  [[ "$2" == "$3" ]] || fail "$1: expected '$3', got '$2'"
}

need() { # need VAR...: skip when an earlier step did not produce a value
  local v
  for v in "$@"; do
    [[ -n "${!v:-}" ]] || { note "SKIP: depends on $v from an earlier failed step"; return 77; }
  done
}

step() {
  local name=$1
  shift
  local t0 rc=0 result
  t0=$(now_ms)
  printf -- '--- %s\n' "$name"
  "$@" || rc=$?
  local ms=$(($(now_ms) - t0))
  if ((rc == 0)); then
    result=PASS
  elif ((rc == 77)); then
    result=SKIP
    FAILS=$((FAILS + 1))
  else
    result=FAIL
    FAILS=$((FAILS + 1))
    show_last
  fi
  printf '    %s (%d ms)\n' "$result" "$ms"
  STEP_NAMES+=("$name")
  STEP_RESULTS+=("$result")
  STEP_MS+=("$ms")
}

# --- Independent GitHub reads. ---------------------------------------------
gh_issue() {
  gh issue view "$1" -R "$REPO" --json number,title,body,state,stateReason,labels,milestone,assignees
}

# gh_item NUMBER: the issue's item in the sandbox Project (archived included)
# as {id, projectId, archived, fields:{name:value}}, or null.
gh_item() {
  # shellcheck disable=SC2016 # GraphQL variables, not shell expansions
  gh api graphql \
    -f query='query($owner:String!,$repo:String!,$number:Int!){repository(owner:$owner,name:$repo){issue(number:$number){projectItems(first:50,includeArchived:true){nodes{id isArchived project{id number owner{... on User{login} ... on Organization{login}}} fieldValues(first:50){nodes{__typename ... on ProjectV2ItemFieldSingleSelectValue{name field{... on ProjectV2FieldCommon{name}}} ... on ProjectV2ItemFieldDateValue{date field{... on ProjectV2FieldCommon{name}}} ... on ProjectV2ItemFieldTextValue{text field{... on ProjectV2FieldCommon{name}}}}}}}}}}' \
    -f owner="$REPO_OWNER" -f repo="$REPO_NAME" -F number="$1" |
    jq -c --arg owner "$PROJECT_OWNER" --argjson n "$PROJECT_NUMBER" '
      [.data.repository.issue.projectItems.nodes[]
        | select(.project.number == $n and .project.owner.login == $owner)
        | {id, projectId: .project.id, archived: .isArchived,
           fields: ([.fieldValues.nodes[]
             | select(.field.name != null)
             | {key: .field.name, value: (.name // .date // .text)}] | from_entries)}]
      | .[0] // null'
}

gh_archive_item() { # gh_archive_item PROJECT_ID ITEM_ID
  # shellcheck disable=SC2016
  gh api graphql \
    -f query='mutation($p:ID!,$i:ID!){archiveProjectV2Item(input:{projectId:$p,itemId:$i}){item{id}}}' \
    -f p="$1" -f i="$2" >/dev/null
}

# Count non-PR issues in the sandbox with exactly this title (REST list, not search).
gh_title_count() {
  gh api "repos/$REPO/issues?state=all&per_page=100&sort=created&direction=desc" |
    jq --arg t "$1" '[.[] | select(.pull_request == null and .title == $t)] | length'
}

# --- Clean-up (also on failure). -------------------------------------------
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if ((KEEP)); then
    echo "--- clean-up skipped (--keep); run id $RUN_ID"
  elif [[ -n "${ROOT:-}" ]]; then
    echo "--- clean-up: closing leftovers as not planned and archiving their Project items"
    local numbers n state item
    numbers=$(
      {
        printf '%s\n' "${CREATED[@]}"
        gh issue list -R "$REPO" --state all --label smoke --search "\"$RUN_ID\" in:title" \
          --json number --jq '.[].number' 2>/dev/null
      } | grep -E '^[0-9]+$' | sort -un
    )
    for n in $numbers; do
      state=$(gh issue view "$n" -R "$REPO" --json state --jq .state 2>/dev/null)
      if [[ "$state" == OPEN ]]; then
        if gh issue close "$n" -R "$REPO" --reason "not planned" >/dev/null 2>&1; then
          note "closed #$n as not planned"
        else
          note "WARNING: could not close #$n"
        fi
      fi
      item=$(gh_item "$n" 2>/dev/null)
      if [[ -n "$item" && "$item" != null && $(jq -r .archived <<<"$item") == false ]]; then
        if gh_archive_item "$(jq -r .projectId <<<"$item")" "$(jq -r .id <<<"$item")"; then
          note "archived Project item for #$n"
        else
          note "WARNING: could not archive item for #$n"
        fi
      fi
    done
  fi
  rm -rf "$TMP"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# --- Preflight. --------------------------------------------------------------
echo "projects live smoke test"
echo "  run id:  $RUN_ID"
echo "  binary:  $PROJECTS ($("$PROJECTS" --version 2>&1 | head -n1))"
echo "  target:  $REPO, Project $PROJECT_OWNER/$PROJECT_NUMBER"

gh auth status >/dev/null 2>&1 || { echo "live-smoke: gh is not authenticated" >&2; exit 2; }
LOGIN=$(gh api user --jq .login) || { echo "live-smoke: cannot read gh user" >&2; exit 2; }

if [[ -n "${SMOKE_ROOT:-}" ]]; then
  ROOT=$(cd "$SMOKE_ROOT" && pwd)
else
  ROOT=$TMP/checkout
  gh repo clone "$REPO" "$ROOT" -- -q --depth 1 >/dev/null 2>&1 ||
    { echo "live-smoke: cannot clone $REPO" >&2; ROOT=""; exit 2; }
fi
echo "  root:    $ROOT"

# The CLI mutates whatever the contract names, so the contract must name the
# sandbox exactly.
contract_json=$("$PROJECTS" contract validate --root "$ROOT" --json --quiet) ||
  { echo "live-smoke: sandbox contract is invalid" >&2; ROOT=""; exit 2; }
if [[ "$(jq -r '.mode' <<<"$contract_json")" != single ||
  "$(jq -r '.repository' <<<"$contract_json")" != "$REPO" ||
  "$(jq -r '.project.owner' <<<"$contract_json")" != "$PROJECT_OWNER" ||
  "$(jq -r '.project.number' <<<"$contract_json")" != "$PROJECT_NUMBER" ]]; then
  echo "live-smoke: refusing to run: the checkout's contract does not name exactly $REPO and Project $PROJECT_OWNER/$PROJECT_NUMBER" >&2
  ROOT=""
  exit 2
fi

# Sandbox-only fixtures: labels and a milestone.
if ! gh label create smoke -R "$REPO" --color BFD4F2 --description "projects live smoke test" --force >/dev/null ||
  ! gh label create smoke-edit -R "$REPO" --color D4C5F9 --description "projects live smoke test (edit)" --force >/dev/null; then
  echo "live-smoke: cannot ensure sandbox labels" >&2
  exit 2
fi
if [[ $(gh api "repos/$REPO/milestones?state=all&per_page=100" --jq '[.[] | select(.title == "smoke")] | length') == 0 ]]; then
  gh api -X POST "repos/$REPO/milestones" -f title=smoke -f description="projects live smoke test" >/dev/null ||
    { echo "live-smoke: cannot create sandbox milestone" >&2; exit 2; }
fi

# Requested Status spelling that differs in case from the live option.
# shellcheck disable=SC2016
LIVE_IN_PROGRESS=$(gh api graphql -f query='query($o:String!,$n:Int!){repositoryOwner(login:$o){... on ProjectV2Owner{projectV2(number:$n){field(name:"Status"){... on ProjectV2SingleSelectField{options{name}}}}}}}' \
  -f o="$PROJECT_OWNER" -F n="$PROJECT_NUMBER" \
  --jq '.data.repositoryOwner.projectV2.field.options[].name | select(ascii_downcase == "in progress")')
if [[ -z "$LIVE_IN_PROGRESS" ]]; then
  echo "live-smoke: sandbox Project Status has no 'In progress' option" >&2
  exit 2
fi
REQ_IN_PROGRESS=${LIVE_IN_PROGRESS,,}
[[ "$REQ_IN_PROGRESS" == "$LIVE_IN_PROGRESS" ]] && REQ_IN_PROGRESS=${LIVE_IN_PROGRESS^^}
echo "  status:  requesting '$REQ_IN_PROGRESS' for live option '$LIVE_IN_PROGRESS'"
echo

T="smoke $RUN_ID"
ISSUE_A="" ISSUE_B="" ISSUE_C="" TITLE_A="$T empty body"

track() { CREATED+=("$1"); }

# --- Steps. ------------------------------------------------------------------
s_contract_validate() {
  cli contract validate --json
  expect_rc 0 || return 1
  assert_eq "contract repository" "$(j .repository)" "$REPO"
}

s_setup_fields_plan() {
  cli project setup-fields --json
  expect_rc 0 || return 1
  assert_eq "setup-fields planned changes" "$(j '.changes | length')" 0
}

s_setup_view_plan() {
  cli project setup-backlog-view --json
  expect_rc 0 || return 1
  assert_eq "setup-backlog-view planned changes" "$(j '.changes | length')" 0
}

s_create_empty_body() {
  cli issue create --title "$TITLE_A" --label smoke --apply --json
  local n
  n=$(j '.issue.number // empty')
  [[ -n "$n" ]] && track "$n"
  expect_rc 0 || return 1
  [[ -n "$n" ]] || fail "no .issue.number in JSON" || return 1
  ISSUE_A=$n
  [[ -n "$(j '.duplicateCheck.method // empty')" ]] || fail "duplicateCheck missing from create JSON" || return 1
  note "duplicateCheck: $(jq -c .duplicateCheck "$OUT")"
  assert_eq "JSON body" "$(j .issue.body)" "" || return 1
  local v
  v=$(gh_issue "$n") || fail "gh issue view #$n failed" || return 1
  assert_eq "gh title" "$(jq -r .title <<<"$v")" "$TITLE_A" || return 1
  assert_eq "gh body" "$(jq -r .body <<<"$v")" "" || return 1
  assert_eq "gh labels" "$(jq -r '[.labels[].name] | sort | join(",")' <<<"$v")" "smoke" || return 1
  note "created #$n"
}

s_duplicate_refused() {
  need ISSUE_A || return
  cli issue create --title "$TITLE_A" --label smoke --apply --json
  local n
  n=$(j '.issue.number // empty')
  [[ -n "$n" ]] && track "$n"
  expect_rc 1 || return 1
  grep -q "exact-title issue already exists" "$ERR" || fail "stderr does not report the exact-title duplicate" || return 1
  grep -q "/issues/$ISSUE_A" "$ERR" || fail "stderr does not name #$ISSUE_A" || return 1
  assert_eq "issues titled '$TITLE_A' (REST)" "$(gh_title_count "$TITLE_A")" 1
}

s_create_with_body() {
  local body title="$T with body"
  # shellcheck disable=SC2016 # literal backticks in the body
  body=$(printf 'Synthetic smoke body.\n\n- line two with `code`\n- run %s' "$RUN_ID")
  cli issue create --title "$title" --body "$body" --label smoke --apply --json
  local n
  n=$(j '.issue.number // empty')
  [[ -n "$n" ]] && track "$n"
  expect_rc 0 || return 1
  ISSUE_B=$n
  [[ -n "$(j '.duplicateCheck.method // empty')" ]] || fail "duplicateCheck missing" || return 1
  [[ "$(j 'has("projectItem")')" == false ]] || fail "unexpected projectItem without Project flags" || return 1
  local v
  v=$(gh_issue "$n") || return 1
  assert_eq "gh body" "$(jq -r .body <<<"$v")" "$body" || return 1
  local item
  item=$(gh_item "$n")
  assert_eq "Project membership without Project flags" "$item" null || return 1
  note "created #$n"
}

s_create_status_fields_me() {
  local title="$T status priority class me"
  cli issue create --title "$title" --label smoke --status "$REQ_IN_PROGRESS" --priority P1 --class Bug \
    --assignee @me --apply --json
  local n
  n=$(j '.issue.number // empty')
  [[ -n "$n" ]] && track "$n"
  expect_rc 0 || return 1
  ISSUE_C=$n
  assert_eq "JSON assignees" "$(j '[.issue.assignees[].login] | join(",")')" "$LOGIN" || return 1
  assert_eq "JSON Status" "$(j '.projectItem.fields.Status')" "$LIVE_IN_PROGRESS" || return 1
  assert_eq "JSON Priority" "$(j '.projectItem.fields.Priority')" P1 || return 1
  assert_eq "JSON Class" "$(j '.projectItem.fields.Class')" Bug || return 1
  local effects
  effects=$(j '.projectItem.automationSideEffects // [] | join("; ")')
  [[ -n "$effects" ]] && note "automationSideEffects: $effects"
  local v item
  v=$(gh_issue "$n") || return 1
  assert_eq "gh assignees" "$(jq -r '[.assignees[].login] | join(",")' <<<"$v")" "$LOGIN" || return 1
  item=$(gh_item "$n")
  [[ "$item" != null ]] || fail "gh: #$n is not in the Project" || return 1
  assert_eq "gh Status" "$(jq -r '.fields.Status' <<<"$item")" "$LIVE_IN_PROGRESS" || return 1
  assert_eq "gh Priority" "$(jq -r '.fields.Priority' <<<"$item")" P1 || return 1
  assert_eq "gh Class" "$(jq -r '.fields.Class' <<<"$item")" Bug || return 1
  note "created #$n (item $(jq -r .id <<<"$item"))"
}

s_status_survives_workflow() {
  need ISSUE_C || return
  note "waiting ${SETTLE}s for the Item added workflow to settle"
  sleep "$SETTLE"
  local item
  item=$(gh_item "$ISSUE_C")
  LAST_CMD="gh_item $ISSUE_C (independent read after ${SETTLE}s)"
  assert_eq "gh Status after settle" "$(jq -r '.fields.Status' <<<"$item")" "$LIVE_IN_PROGRESS"
}

s_create_project_no_status() {
  local title="$T project no status"
  cli issue create --title "$title" --label smoke --priority P2 --class Task --apply --json
  local n
  n=$(j '.issue.number // empty')
  [[ -n "$n" ]] && track "$n"
  expect_rc 0 || return 1
  [[ -n "$(j '.projectItem.itemId // empty')" ]] || fail "no projectItem.itemId in JSON" || return 1
  note "CLI fields: $(jq -c '.projectItem.fields' "$OUT")"
  note "automationSideEffects: $(j '.projectItem.automationSideEffects // [] | join("; ")')"
  sleep "$SETTLE"
  local item
  item=$(gh_item "$n")
  [[ "$item" != null ]] || fail "gh: #$n is not in the Project" || return 1
  assert_eq "gh item id" "$(jq -r .id <<<"$item")" "$(j .projectItem.itemId)" || return 1
  note "gh Status after ${SETTLE}s: $(jq -r '.fields.Status // "(none)"' <<<"$item")"
}

s_edit_title_labels_milestone() {
  need ISSUE_A || return
  local title="$T edited"
  cli issue edit --issue "$ISSUE_A" --title "$title" --add-label smoke-edit --milestone smoke --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON title" "$(j .issue.title)" "$title" || return 1
  local v
  v=$(gh_issue "$ISSUE_A") || return 1
  assert_eq "gh title" "$(jq -r .title <<<"$v")" "$title" || return 1
  assert_eq "gh labels" "$(jq -r '[.labels[].name] | sort | join(",")' <<<"$v")" "smoke,smoke-edit" || return 1
  assert_eq "gh milestone" "$(jq -r '.milestone.title // ""' <<<"$v")" smoke
}

s_edit_remove_label_milestone() {
  need ISSUE_A || return
  cli issue edit --issue "$ISSUE_A" --remove-label smoke-edit --milestone "" --apply --json
  expect_rc 0 || return 1
  local v
  v=$(gh_issue "$ISSUE_A") || return 1
  assert_eq "gh labels" "$(jq -r '[.labels[].name] | sort | join(",")' <<<"$v")" "smoke" || return 1
  assert_eq "gh milestone" "$(jq -r '.milestone.title // ""' <<<"$v")" ""
}

s_close_not_planned_plain() {
  need ISSUE_B || return
  cli issue edit --issue "$ISSUE_B" --state closed --close-reason not_planned --apply --json
  expect_rc 0 || return 1
  local v
  v=$(gh_issue "$ISSUE_B") || return 1
  assert_eq "gh state" "$(jq -r .state <<<"$v")" CLOSED || return 1
  assert_eq "gh stateReason" "$(jq -r '.stateReason | ascii_upcase' <<<"$v")" NOT_PLANNED
}

s_close_not_planned_project() {
  need ISSUE_C || return
  cli issue edit --issue "$ISSUE_C" --state closed --close-reason not_planned --apply --json
  local effects
  effects=$(j '.issue.automationSideEffects // [] | join("; ")')
  [[ -n "$effects" ]] && note "automationSideEffects: $effects"
  expect_rc 0 || return 1
  local v
  v=$(gh_issue "$ISSUE_C") || return 1
  assert_eq "gh state" "$(jq -r .state <<<"$v")" CLOSED || return 1
  assert_eq "gh stateReason" "$(jq -r '.stateReason | ascii_upcase' <<<"$v")" NOT_PLANNED
}

s_change_closed_reason() {
  need ISSUE_C || return
  cli issue edit --issue "$ISSUE_C" --state closed --close-reason completed --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON stateReason" "$(j '.issue.stateReason | ascii_upcase')" COMPLETED || return 1
  local v
  v=$(gh_issue "$ISSUE_C") || return 1
  assert_eq "gh state" "$(jq -r .state <<<"$v")" CLOSED || return 1
  assert_eq "gh stateReason" "$(jq -r '.stateReason | ascii_upcase' <<<"$v")" COMPLETED
}

s_reopen() {
  need ISSUE_C || return
  cli issue edit --issue "$ISSUE_C" --state open --apply --json
  local effects
  effects=$(j '.issue.automationSideEffects // [] | join("; ")')
  [[ -n "$effects" ]] && note "automationSideEffects: $effects"
  expect_rc 0 || return 1
  local v
  v=$(gh_issue "$ISSUE_C") || return 1
  assert_eq "gh state" "$(jq -r .state <<<"$v")" OPEN
}

ITEM_A=""
s_item_add_new() {
  need ISSUE_A || return
  cli project item-add --issue "$ISSUE_A" --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON alreadyMember" "$(j .alreadyMember)" false || return 1
  assert_eq "JSON applied" "$(j .applied)" true || return 1
  local item
  item=$(gh_item "$ISSUE_A")
  [[ "$item" != null ]] || fail "gh: #$ISSUE_A is not in the Project" || return 1
  assert_eq "gh item id" "$(jq -r .id <<<"$item")" "$(j .itemId)" || return 1
  ITEM_A=$(j .itemId)
}

s_item_add_idempotent() {
  need ITEM_A ISSUE_C || return
  cli project item-add --issue "$ISSUE_A" --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON alreadyMember (#$ISSUE_A)" "$(j .alreadyMember)" true || return 1
  assert_eq "JSON applied (#$ISSUE_A)" "$(j .applied)" false || return 1
  assert_eq "JSON itemId (#$ISSUE_A)" "$(j .itemId)" "$ITEM_A" || return 1
  cli project item-add --issue "$ISSUE_C" --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON alreadyMember (#$ISSUE_C)" "$(j .alreadyMember)" true
}

s_item_edit_set() {
  need ITEM_A || return
  cli project item-edit --issue "$ISSUE_A" --priority P0 --status "done" --target-date 2030-01-31 --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON Priority" "$(j .fields.Priority)" P0 || return 1
  assert_eq "JSON Status" "$(j .fields.Status)" Done || return 1
  note "automationSideEffects: $(j '.automationSideEffects // [] | join("; ")')"
  # GitHub's built-in "Auto-close issue" workflow may close the issue when
  # Status becomes Done; that is an issue change, recorded but not asserted.
  note "issue state afterwards: $(gh issue view "$ISSUE_A" -R "$REPO" --json state,stateReason --jq '"\(.state) \(.stateReason)"')"
  local item
  item=$(gh_item "$ISSUE_A")
  assert_eq "gh Priority" "$(jq -r .fields.Priority <<<"$item")" P0 || return 1
  assert_eq "gh Status" "$(jq -r .fields.Status <<<"$item")" Done || return 1
  assert_eq "gh Target date" "$(jq -r '.fields["Target date"] // ""' <<<"$item")" 2030-01-31
}

s_item_edit_clear() {
  need ITEM_A || return
  cli project item-edit --issue "$ISSUE_A" --priority P3 --status "$REQ_IN_PROGRESS" --clear "Target date" --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON Priority" "$(j .fields.Priority)" P3 || return 1
  local item
  item=$(gh_item "$ISSUE_A")
  assert_eq "gh Priority" "$(jq -r .fields.Priority <<<"$item")" P3 || return 1
  assert_eq "gh Status" "$(jq -r .fields.Status <<<"$item")" "$LIVE_IN_PROGRESS" || return 1
  assert_eq "gh Target date" "$(jq -r '.fields["Target date"] // ""' <<<"$item")" ""
}

s_archive_then_restore() {
  need ITEM_A || return
  local item
  item=$(gh_item "$ISSUE_A")
  LAST_CMD="gh api graphql archiveProjectV2Item (#$ISSUE_A)"
  gh_archive_item "$(jq -r .projectId <<<"$item")" "$(jq -r .id <<<"$item")" || fail "archive mutation failed" || return 1
  item=$(gh_item "$ISSUE_A")
  assert_eq "gh archived after archive" "$(jq -r .archived <<<"$item")" true || return 1
  cli project item-add --issue "$ISSUE_A" --apply --json
  expect_rc 0 || return 1
  assert_eq "JSON unarchived" "$(j .unarchived)" true || return 1
  assert_eq "JSON applied" "$(j .applied)" true || return 1
  assert_eq "JSON itemId" "$(j .itemId)" "$ITEM_A" || return 1
  item=$(gh_item "$ISSUE_A")
  assert_eq "gh archived after item-add" "$(jq -r .archived <<<"$item")" false || return 1
  assert_eq "gh Priority preserved" "$(jq -r .fields.Priority <<<"$item")" P3
}

step "contract validate" s_contract_validate
step "setup-fields plan has no changes" s_setup_fields_plan
step "setup-backlog-view plan has no changes" s_setup_view_plan
step "issue create, empty body" s_create_empty_body
step "issue create, immediate exact-title re-run refused" s_duplicate_refused
step "issue create, with body" s_create_with_body
step "issue create, Status case + Priority/Class + @me" s_create_status_fields_me
step "requested Status survives Item added workflow" s_status_survives_workflow
step "issue create, Project fields without Status" s_create_project_no_status
step "issue edit, title + add label + milestone" s_edit_title_labels_milestone
step "issue edit, remove label + clear milestone" s_edit_remove_label_milestone
step "issue edit, close not_planned (no Project item)" s_close_not_planned_plain
step "issue edit, close not_planned (Project item)" s_close_not_planned_project
step "issue edit, change reason on closed issue" s_change_closed_reason
step "issue edit, reopen" s_reopen
step "project item-add, new membership" s_item_add_new
step "project item-add, idempotent re-run" s_item_add_idempotent
step "project item-edit, Priority + Status + Target date" s_item_edit_set
step "project item-edit, Priority + Status + clear" s_item_edit_clear
step "archived item restored by item-add" s_archive_then_restore

# --- Summary. ----------------------------------------------------------------
echo
echo "Summary (run $RUN_ID, $("$PROJECTS" --version 2>&1 | head -n1))"
for i in "${!STEP_NAMES[@]}"; do
  printf '  %-4s %7d ms  %s\n' "${STEP_RESULTS[$i]}" "${STEP_MS[$i]}" "${STEP_NAMES[$i]}"
done
total=${#STEP_NAMES[@]}
printf '  %d/%d passed; wall time %d ms\n' "$((total - FAILS))" "$total" "$(($(now_ms) - START_MS))"
((FAILS == 0))

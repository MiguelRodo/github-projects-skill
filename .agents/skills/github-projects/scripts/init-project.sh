#!/usr/bin/env bash

# Guided, repository-local onboarding for github-projects. This script
# writes repository configuration only. It never changes live GitHub issues or
# Project fields. Keep this file compatible with Bash 3.2.
set +x
set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
skill_dir="$(cd "$script_dir/.." && pwd)"
generated_contract=""
transaction_dir=""
changed_contract_paths=()
issue_repository=""
first_request_project_context=""
first_request_class_name="Issue Type or Class"
section_number=0
colour_reset=""
colour_bold=""
colour_blue=""
colour_green=""
colour_yellow=""
colour_red=""

if [[ -t 1 && -z "${NO_COLOR:-}" && "${TERM:-dumb}" != "dumb" ]]; then
  colour_reset="$(printf '\033[0m')"
  colour_bold="$(printf '\033[1m')"
  colour_blue="$(printf '\033[34m')"
  colour_green="$(printf '\033[32m')"
  colour_yellow="$(printf '\033[33m')"
  colour_red="$(printf '\033[31m')"
fi

section() {
  section_number=$((section_number + 1))
  printf '\n%s%s%d. %s%s\n' \
    "$colour_bold" "$colour_blue" "$section_number" "$1" "$colour_reset"
  printf '%s\n' '----------------------------------------'
}

success() {
  printf '%s%s[OK]%s %s\n' "$colour_bold" "$colour_green" "$colour_reset" "$1"
}

note() {
  printf '%s[INFO]%s %s\n' "$colour_blue" "$colour_reset" "$1"
}

warning() {
  printf '%s%s[WARNING]%s %s\n' \
    "$colour_bold" "$colour_yellow" "$colour_reset" "$1" >&2
}

die() {
  printf '%s%s[ERROR]%s %s\n' \
    "$colour_bold" "$colour_red" "$colour_reset" "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "$generated_contract" && -f "$generated_contract" ]]; then
    rm -f -- "$generated_contract"
  fi
  if [[ -n "$transaction_dir" && -d "$transaction_dir" ]]; then
    rm -rf -- "$transaction_dir"
  fi
}
trap cleanup EXIT

ask_default() {
  local prompt="$1" default="$2" answer
  read -r -p "$prompt [$default]: " answer ||
    die "input ended before setup was complete"
  printf '%s' "${answer:-$default}"
}

ask_yes_no() {
  local prompt="$1" default="$2" answer hint
  if [[ "$default" == "yes" ]]; then
    hint="Y/n"
  else
    hint="y/N"
  fi
  while true; do
    read -r -p "$prompt [$hint]: " answer ||
      die "input ended before setup was complete"
    answer="${answer:-$default}"
    answer="$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')"
    case "$answer" in
      y|yes) return 0 ;;
      n|no) return 1 ;;
      *) echo "Please answer yes or no." >&2 ;;
    esac
  done
}

require_text() {
  local prompt="$1" answer
  while true; do
    read -r -p "$prompt: " answer ||
      die "input ended before setup was complete"
    if [[ -n "$answer" && "$answer" != *"|"* && "$answer" != *$'\n'* ]]; then
      printf '%s' "$answer"
      return 0
    fi
    echo "Enter a value without a table separator (|)." >&2
  done
}

append_agents_pointer() {
  local agents_file="$repository_root/AGENTS.md"
  if [[ -f "$agents_file" ]] && {
    grep -Fq '<!-- github-projects:start -->' "$agents_file" ||
    grep -Fq '<!-- github-project-admin:start -->' "$agents_file"
  }; then
    note "AGENTS.md already contains the GitHub Project starting point."
    return 0
  fi

  if [[ -s "$agents_file" ]]; then
    printf '\n' >>"$agents_file"
  fi
  cat >>"$agents_file" <<'EOF'
<!-- github-projects:start -->
## GitHub issues and Projects

For GitHub issue or Project administration, use
`.agents/skills/github-projects/SKILL.md` and read
`.projects/project.md` before acting.
<!-- github-projects:end -->
EOF
  success "Added the GitHub Project starting point to AGENTS.md."
}

validate_issue_repository() {
  local candidate="$1"
  local resolved
  if [[ "$candidate" == *"|"* || "$candidate" == *$'\n'* ]]; then
    die "issue repository must not contain a table separator (|) or newline"
  fi
  if [[ ! "$candidate" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]]; then
    die "issue repository must be in owner/repo format: $candidate"
  fi
  if [[ "$candidate" == "$repository" ]]; then
    printf '%s' "$repository"
    return 0
  fi
  resolved="$(gh repo view "$candidate" --json nameWithOwner --jq '.nameWithOwner' 2>/dev/null)" ||
    die "could not find or access issue repository: $candidate"
  if [[ -z "$resolved" || ! "$resolved" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]]; then
    die "could not find or access issue repository: $candidate"
  fi
  printf '%s' "$resolved"
}

print_auth_help() {
  cat <<'EOF'

Run these commands, then start the initializer again:

  gh auth login --web --scopes "project,read:org"
  gh auth status

If you were already signed in but Project access is missing, run:

  gh auth refresh --scopes "project,read:org"
EOF
}

relative_skill_path() {
  case "$skill_dir" in
    "$repository_root"/*)
      printf '%s' "${skill_dir#"$repository_root"/}"
      ;;
    *)
      return 1
      ;;
  esac
}

contract_table_value() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^\|/ {
      key = trim($2)
      if (key == wanted) {
        print trim($3)
        exit
      }
    }
  ' "$file"
}

mark_contract_path() {
  local path="$1" existing
  for existing in "${changed_contract_paths[@]}"; do
    [[ "$existing" != "$path" ]] || return 0
  done
  changed_contract_paths+=("$path")
}

slugify() {
  local value="$1"
  value="$(printf '%s' "$value" |
    tr '[:upper:]' '[:lower:]' |
    sed -e 's/[^a-z0-9][^a-z0-9]*/-/g' -e 's/^-//' -e 's/-$//')"
  printf '%s' "$value"
}

discover_project() {
  local observed_owner_type project_selector project_query project_record
  local observed_number

  observed_owner_type="$(gh api "users/$project_owner" --jq .type 2>/dev/null)" ||
    die "could not discover whether the Project owner is a person or organisation"
  case "$observed_owner_type" in
    User)
      project_owner_type="user"
      project_selector='.data.user.projectV2'
      project_query='query($login: String!, $number: Int!) { user(login: $login) { projectV2(number: $number) { number title public } } }'
      class_location="project field"
      class_field="Class"
      ;;
    Organization)
      project_owner_type="organization"
      project_selector='.data.organization.projectV2'
      project_query='query($login: String!, $number: Int!) { organization(login: $login) { projectV2(number: $number) { number title public } } }'
      class_location="organization issue type"
      class_field="Issue Type"
      ;;
    *) die "unsupported GitHub owner type: $observed_owner_type" ;;
  esac

  project_record="$(gh api graphql -f query="$project_query" \
    -F login="$project_owner" -F number="$project_number" \
    --jq "$project_selector | [.number,.title,(.public|tostring)] | @tsv")" ||
    die "could not read Project $project_owner/$project_number"
  IFS=$'\t' read -r observed_number project_title project_public <<<"$project_record"
  [[ "$observed_number" == "$project_number" && -n "$project_title" ]] ||
    die "GitHub did not return the expected Project"
  [[ "$project_title" != *"|"* && "$project_title" != *$'\n'* ]] ||
    die "this Project title needs agent-assisted contract generation"

  if [[ "$visibility_lower" == "private" ]]; then
    privacy="private repository"
  elif [[ "$project_public" == "false" ]]; then
    privacy="$visibility_lower repository with a private Project"
  else
    privacy="$visibility_lower repository"
  fi

  if [[ "$project_owner_type" == "organization" ]]; then
    first_request_class_name="Issue Type"
  else
    first_request_class_name="Class"
  fi
  first_request_project_context="GitHub Project $project_owner/$project_number"
  success "Found $project_title, owned by the GitHub $project_owner_type $project_owner."
}

write_project_contract() {
  local target="$1" mode="$2" project_key="$3" routing="$4"
  local routing_rule

  cat >"$target" <<EOF
# GitHub Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | $mode |
EOF
  if [[ "$mode" == "project" ]]; then
    printf '| Project key | %s |\n' "$project_key" >>"$target"
  fi
  cat >>"$target" <<EOF
| Issue repository | $issue_repository |
| Project owner | $project_owner |
EOF
  if [[ "$mode" == "project" ]]; then
    printf '| Owner type | %s |\n' "$project_owner_type" >>"$target"
  fi
  cat >>"$target" <<EOF
| Project number | $project_number |
| Project title | $project_title |
| Routing | $routing |
| Privacy | $privacy |
| Issue write-up style | tidy |
| Issue prose style | natural-direct |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | $class_location | $class_field |
| Priority | pending live inspection | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
| Parent | native issue relationship | Parent issue |

## Priority mapping

Priority mapping status: pending

The initializer left the Project's existing Priority field and options unchanged.
Before using Priority, an agent must confirm its provider location, inspect the
live options and replace the pending status with a complete one-to-one P0, P1,
P2 and P3 mapping.

## Class / Issue Type

The Class or Issue Type option set is intentionally not fixed by onboarding.
An agent may inspect the existing issues and suggest a useful vocabulary before
the live Project is changed. Workstream is not a standard semantic dimension.

## Governance

- This is a $governance Project.
EOF
  if [[ "$mode" == "project" ]]; then
    routing_rule="The dispatcher routing label selects this Project; it is not a sub-project label."
  else
    routing_rule="Project membership determines Project scope; no routing label is required."
  fi
  cat >>"$target" <<EOF
- $routing_rule
- Labels must not duplicate Class, Priority or Status.
- Assignment is explicit only unless a later repository decision says otherwise.
- Exact requested administration requires no separate scope-design source.
EOF
}

create_dispatcher_contract() {
  mkdir -p "$repository_root/.projects/projects"
  if [[ ! -e "$repository_root/.projects/projects/.gitkeep" ]]; then
    : >"$repository_root/.projects/projects/.gitkeep"
    mark_contract_path ".projects/projects/.gitkeep"
  fi

  generated_contract="$(mktemp)"
  cat >"$generated_contract" <<EOF
# GitHub Project dispatcher

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | $issue_repository |
| Privacy | $repository_privacy |
| Governance | $governance |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |
EOF
  mv "$generated_contract" "$contract_file"
  generated_contract=""
  mark_contract_path ".projects/project.md"
}

dispatcher_route_count() {
  awk -F'|' '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## Routes[[:space:]]*$/ { in_routes = 1; next }
    in_routes && /^## / { exit }
    in_routes && /^\|/ {
      key = trim($2)
      if (key != "" && key != "Project key" && key != "---") count += 1
    }
    END { print count + 0 }
  ' "$contract_file"
}

dispatcher_contract_for_number() {
  local wanted="$1"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## Routes[[:space:]]*$/ { in_routes = 1; next }
    in_routes && /^## / { exit }
    in_routes && /^\|/ && trim($4) == wanted {
      print trim($5)
      exit
    }
  ' "$contract_file"
}

dispatcher_has_value() {
  local column="$1" wanted="$2"
  awk -F'|' -v column="$column" -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## Routes[[:space:]]*$/ { in_routes = 1; next }
    in_routes && /^## / { exit }
    in_routes && /^\|/ && trim($column) == wanted { found = 1; exit }
    END { exit(found ? 0 : 1) }
  ' "$contract_file"
}

append_dispatcher_route() {
  local file="$1" row="$2" updated
  updated="$file.updated"
  awk -v row="$row" '
    /^## Routes[[:space:]]*$/ { in_routes = 1 }
    in_routes && /^## / && $0 !~ /^## Routes[[:space:]]*$/ && !inserted {
      print row
      inserted = 1
      in_routes = 0
    }
    { print }
    END {
      if (in_routes && !inserted) print row
    }
  ' "$file" >"$updated"
  mv "$updated" "$file"
}

add_project_to_dispatcher() {
  local default_key existing_contract leaf leaf_owner leaf_owner_type leaf_title
  local project_key routing_label child_contract route_row

  project_owner="$(ask_default \
    "GitHub user or organisation that owns the Project" "$repository_owner")"
  [[ "$project_owner" =~ ^[A-Za-z0-9_.-]+$ ]] ||
    die "invalid Project owner login"

  echo
  echo "Find the Project number after /projects/ in its web address:"
  echo "  https://github.com/users/example/projects/40       means 40"
  echo "  https://github.com/orgs/example-org/projects/12    means 12"
  project_number="$(require_text "Project number")"
  [[ "$project_number" =~ ^[1-9][0-9]*$ ]] ||
    die "Project number must be a positive integer"

  discover_project
  existing_contract="$(dispatcher_contract_for_number "$project_number")"
  if [[ -n "$existing_contract" ]]; then
    leaf="$repository_root/$existing_contract"
    leaf_owner="$(contract_table_value "$leaf" "Project owner")"
    leaf_owner_type="$(contract_table_value "$leaf" "Owner type")"
    leaf_title="$(contract_table_value "$leaf" "Project title")"
    [[ "$leaf_owner" == "$project_owner" ]] ||
      die "Project number $project_number is already routed to a different owner"
    [[ -z "$leaf_owner_type" || "$leaf_owner_type" == "$project_owner_type" ]] ||
      die "the configured Project owner type no longer matches GitHub"
    [[ "$leaf_title" == "$project_title" ]] ||
      die "the configured Project title no longer matches GitHub"
    first_request_project_context="Project $project_owner/$project_number"
    note "Project $project_owner/$project_number is already configured; no route was changed."
    return 0
  fi

  default_key="$(slugify "$project_title")"
  [[ -n "$default_key" ]] || default_key="project-$project_number"
  project_key="$(ask_default "Project key" "$default_key")"
  [[ "$project_key" =~ ^[a-z0-9][a-z0-9-]*$ ]] ||
    die "Project key must use lowercase letters, numbers and hyphens"
  dispatcher_has_value 2 "$project_key" &&
    die "Project key $project_key is already configured"

  routing_label="$(ask_default "Routing label" "project:$project_key")"
  [[ -n "$routing_label" && "$routing_label" != *"|"* &&
     "$routing_label" != *$'\n'* ]] ||
    die "routing label must be non-empty and contain no table separator"
  dispatcher_has_value 3 "$routing_label" &&
    die "routing label $routing_label is already configured"

  child_contract=".projects/projects/$project_key.md"
  [[ ! -e "$repository_root/$child_contract" ]] ||
    die "$child_contract already exists without a matching route"

  transaction_dir="$(mktemp -d)"
  mkdir -p "$transaction_dir/.projects/projects"
  cp "$contract_file" "$transaction_dir/.projects/project.md"
  if [[ -d "$repository_root/.projects/projects" ]]; then
    cp -R "$repository_root/.projects/projects/." \
      "$transaction_dir/.projects/projects/"
  fi
  write_project_contract "$transaction_dir/$child_contract" project \
    "$project_key" "label:$routing_label"
  route_row="| $project_key | $routing_label | $project_number | $child_contract |"
  append_dispatcher_route "$transaction_dir/.projects/project.md" "$route_row"
  bash "$script_dir/validate-contract.sh" "$transaction_dir"

  mkdir -p "$repository_root/.projects/projects"
  mv "$transaction_dir/$child_contract" "$repository_root/$child_contract"
  mv "$transaction_dir/.projects/project.md" "$contract_file"
  rm -rf -- "$transaction_dir"
  transaction_dir=""
  mark_contract_path ".projects/project.md"
  mark_contract_path "$child_contract"
  first_request_project_context="Project key $project_key ($project_owner/$project_number)"
  success "Added Project $project_owner/$project_number as route $project_key."
}

configure_dispatcher_projects() {
  section "Add Projects"
  if ! ask_yes_no "Would you like to add a Project now?" yes; then
    note "The empty dispatcher is saved, but ordinary Project work needs at least one route."
    return 0
  fi

  while true; do
    add_project_to_dispatcher
    if ! ask_yes_no "Would you like to add another Project?" no; then
      break
    fi
  done
}

load_first_request_from_dispatcher() {
  local route project_key project_number child_contract leaf owner owner_type
  route="$(awk -F'|' '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## Routes[[:space:]]*$/ { in_routes = 1; next }
    in_routes && /^## / { exit }
    in_routes && /^\|/ {
      key = trim($2)
      if (key != "" && key != "Project key" && key != "---") {
        print key "\t" trim($4) "\t" trim($5)
        exit
      }
    }
  ' "$contract_file")"
  [[ -n "$route" ]] || return 1
  IFS=$'\t' read -r project_key project_number child_contract <<<"$route"
  leaf="$repository_root/$child_contract"
  owner="$(contract_table_value "$leaf" "Project owner")"
  owner_type="$(contract_table_value "$leaf" "Owner type")"
  first_request_project_context="Project key $project_key ($owner/$project_number)"
  if [[ "$owner_type" == "organization" ]]; then
    first_request_class_name="Issue Type"
  elif [[ "$owner_type" == "user" ]]; then
    first_request_class_name="Class"
  else
    first_request_class_name="Issue Type or Class"
  fi
}

save_onboarding_files() {
  local skill_path="" branch="" contract_path existing_path duplicate
  local -a save_paths
  save_paths=()

  skill_path="$(relative_skill_path 2>/dev/null || true)"
  if [[ -n "$skill_path" && -d "$skill_path" ]]; then
    save_paths+=("$skill_path")
  fi
  if [[ -e "$repository_root/.projects/project.md" ]]; then
    save_paths+=(".projects/project.md")
  fi
  for contract_path in "${changed_contract_paths[@]}"; do
    [[ -e "$repository_root/$contract_path" ]] || continue
    duplicate="no"
    for existing_path in "${save_paths[@]}"; do
      if [[ "$existing_path" == "$contract_path" ]]; then
        duplicate="yes"
        break
      fi
    done
    [[ "$duplicate" == "yes" ]] || save_paths+=("$contract_path")
  done
  if [[ -e "$repository_root/AGENTS.md" ]]; then
    save_paths+=("AGENTS.md")
  fi

  section "Save the repository setup"
  if ((${#save_paths[@]} == 0)) ||
     [[ -z "$(git status --porcelain -- "${save_paths[@]}")" ]]; then
    success "The onboarding files are already committed."
    return 0
  fi

  echo "The setup changed only these onboarding paths:"
  printf '  %s\n' "${save_paths[@]}"
  echo
  if ! ask_yes_no "May I stage, commit and push these onboarding files?" no; then
    note "The files were left uncommitted."
    echo "Review them with:"
    echo
    printf '  git status --short --'
    printf ' %q' "${save_paths[@]}"
    printf '\n'
    return 0
  fi

  if ! git add -- "${save_paths[@]}"; then
    warning "Git could not stage the onboarding files. Nothing was committed or pushed."
    echo "After fixing the reported problem, run:"
    echo
    printf '  git add --'
    printf ' %q' "${save_paths[@]}"
    printf '\n'
    return 0
  fi

  if git diff --cached --quiet -- "${save_paths[@]}"; then
    success "There was nothing new to commit."
    return 0
  fi

  if ! git commit -m "Configure GitHub Project administration" -- \
       "${save_paths[@]}"; then
    warning "Git could not create the commit. The onboarding files remain staged."
    echo "Fix the error above, then run:"
    echo
    echo "  git commit -m \"Configure GitHub Project administration\""
    echo "  git push"
    return 0
  fi
  success "Created the onboarding commit."

  branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
  if [[ -z "$branch" ]]; then
    warning "The repository is in detached-HEAD state, so the commit was not pushed."
    echo "Create or switch to a branch, then run git push."
    return 0
  fi

  if git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' \
       >/dev/null 2>&1; then
    if git push; then
      success "Pushed the onboarding commit."
    else
      warning "The commit is safe locally, but Git could not push it."
      echo "After fixing access, the remote or branch protection, run:"
      echo
      echo "  git push"
    fi
  else
    if git push -u origin "$branch"; then
      success "Pushed the onboarding commit."
    else
      warning "The commit is safe locally, but Git could not push it."
      echo "After fixing access, the remote or branch protection, run:"
      echo
      printf '  git push -u origin %s\n' "$branch"
    fi
  fi
}

print_provider_intro() {
  section "Choose how you will use the repository"
  cat <<'EOF'
I will now show two ways to use the repository: a chat interface that can
use the configured handoff for unsupported changes, and an execution-capable
agent that can run and verify authorised work.

Before using either remote surface, make sure the installed skill and
repository configuration are committed and pushed. If the previous section
left local work or a failed push, complete the recovery commands it printed
first.
EOF
}

print_chatgpt_setup() {
  section "Use the repository with a chat interface"
  cat <<EOF
1. Open https://chatgpt.com/projects and create or open a ChatGPT Project.
2. Make $repository available to that Project through its GitHub connection.
3. Paste this into the ChatGPT Project instructions:

  For work concerning a GitHub repository, especially reading or updating
  GitHub issues or Projects, first retrieve and follow the target repository's
  AGENTS.md. Follow the skill and configuration files it references. If the
  repository or AGENTS.md is unavailable, say so rather than guessing.

  Treat my prompt as the desired outcome. If this chat cannot make an authorised
  GitHub change, follow the repository's configured handoff. When its local Chat
  administration queue is enabled, mark the existing task issue for bounded
  administrative reconciliation, or create a temporary handoff issue, and add
  the authority comment the skill requires. Report the change as queued. Queue
  mode is administrative-only by effect: it never authorises repository
  implementation, and ordinary task prose in a queued issue must not stop that
  issue's administration. Otherwise return the smallest executable command block
  with an independent result check.

After that, ask for the outcome you want in ordinary language. A specific change
request supplies authority for that change; broad organisation starts with a
proposal for approval. The chat makes supported changes and uses the configured
handoff for the rest. Run pj -i locally to process a queued handoff after setting
up the launcher from projects.
EOF
}

print_codex_setup() {
  section "Use the repository with an execution-capable agent"
  cat <<EOF
The example below uses Codex cloud.

1. Open https://chatgpt.com/codex/settings/environments and create an environment.
2. Choose the $repository repository.
3. Use this setup command:

  bash .agents/skills/github-projects/scripts/setup.sh

4. Create a classic GitHub personal access token at:

  https://github.com/settings/tokens/new

   Give it an expiry and the repo, read:org and project scopes. If your
   organisation uses SSO, authorise the token for that organisation.
5. Add the token to the environment as an environment variable named GH_TOKEN.
   Do not add it as a setup-only secret because the agent needs it after setup.
6. Enable internet access for the agent phase and allow:

  github.com
  api.github.com

Codex can then read AGENTS.md, propose the work, and run and verify the GitHub
commands after you approve its proposal.
EOF
}

print_first_request() {
  section "Choose a useful first request"
  if ! ask_yes_no \
    "Would you like a proposal for organising the existing issues next?" \
    yes; then
    note "Setup is complete. You can now make ordinary requests when you need them."
    return 0
  fi

  cat <<EOF

Use the same first request in a chat interface or an execution-capable agent:

  Start from AGENTS.md. Resolve $first_request_project_context and inspect its
  current issues and GitHub Project. Propose how you would confirm the pending
  Priority location and complete one-to-one mapping from the existing field
  without adding, removing or renaming options; set up or refine
  $first_request_class_name from the issue evidence; preserve useful existing
  definitions while choosing sensible colours; and organise the issues using
  Project fields and useful native parent/sub-issue relationships. Treat the
  Project itself as the container rather than creating a generic root issue;
  prefer independently meaningful top-level outcomes, propose retiring generic
  category or standing wrappers where appropriate, and use body checkboxes for
  small local steps versus sub-issues for independently trackable work. Suggest
  optional sub-project labels only where they are genuinely useful. Show me
  the exact proposed changes and do not change GitHub until I approve them.

After you approve the proposal, an execution-capable agent can apply and verify
it. A chat interface that cannot write should instead return the smallest safe
command block for you to run, including independent readback.
EOF
}

print_empty_dispatcher_next_step() {
  section "Add a Project before ordinary administration"
  cat <<EOF
The repository now has a validated dispatcher with no routes. That is a safe
onboarding state, but it cannot resolve ordinary Project requests yet.

Run the initializer again and choose to add a Project:

  bash .agents/skills/github-projects/scripts/init-project.sh

It will preserve the dispatcher, add one Project at a time and ask whether you
want to add another.
EOF
}

command -v git >/dev/null 2>&1 || die "git is required"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
  die "run this command inside the repository you want to configure"
repository_root="$(git rev-parse --show-toplevel)"
cd "$repository_root"

section "Check GitHub"
if ! command -v gh >/dev/null 2>&1; then
  echo "Install GitHub CLI from https://cli.github.com/ and run:" >&2
  print_auth_help >&2
  die "GitHub CLI (gh) is not installed"
fi
if ! gh auth status >/dev/null 2>&1; then
  print_auth_help >&2
  die "GitHub CLI is not authenticated"
fi
authenticated_login="$(gh api user --jq .login 2>/dev/null)" ||
  die "could not read the active GitHub login"
if ! gh project list --owner "$authenticated_login" --limit 1 \
     >/dev/null 2>&1; then
  print_auth_help >&2
  die "the active GitHub login does not have Project access"
fi
success "GitHub CLI is authenticated as $authenticated_login with Project access."

repository_record="$(gh repo view --json nameWithOwner,visibility \
  --jq '[.nameWithOwner,.visibility] | @tsv')" ||
  die "could not discover the current GitHub repository"
IFS=$'\t' read -r repository visibility <<<"$repository_record"
[[ "$repository" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]] ||
  die "GitHub returned an invalid repository identity"
repository_owner="${repository%%/*}"
visibility_lower="$(printf '%s' "$visibility" | tr '[:upper:]' '[:lower:]')"
repository_privacy="$visibility_lower repository"

contract_file="$repository_root/.projects/project.md"
if [[ -e "$contract_file" ]]; then
  section "Check the existing repository setup"
  note "A repository contract already exists at .projects/project.md."
  bash "$script_dir/validate-contract.sh" "$repository_root"
  append_agents_pointer
  existing_mode="$(contract_table_value "$contract_file" "Mode")"
  if [[ "$existing_mode" != "dispatcher" ]]; then
    success "The existing repository setup was not replaced."
    save_onboarding_files
    echo
    note "For usage and update instructions, read .agents/skills/github-projects/README.md."
    exit 0
  fi

  governance="$(contract_table_value "$contract_file" "Governance")"
  [[ -n "$governance" ]] || governance="shared"
  issue_repository="$(contract_table_value "$contract_file" "Issue repository")"
  [[ -n "$issue_repository" ]] || issue_repository="$repository"
  success "The existing dispatcher and routes will be preserved."
  configure_dispatcher_projects
  bash "$script_dir/validate-contract.sh" "$repository_root"
  save_onboarding_files
  print_provider_intro
  print_chatgpt_setup
  print_codex_setup
  if [[ "$(dispatcher_route_count)" == "0" ]]; then
    print_empty_dispatcher_next_step
  else
    if [[ -z "$first_request_project_context" ]]; then
      load_first_request_from_dispatcher
    fi
    print_first_request
  fi
  echo
  success "Repository onboarding is complete."
  echo "The initializer did not change any live GitHub issue or Project value."
  exit 0
fi

section "Configure the repository"
cat <<EOF
I will ask a few questions to configure $repository so that ChatGPT and coding
agents can understand its GitHub Project and work with it safely. GitHub will
supply facts it already knows; the questions are only about local choices.

No live issue or Project value will be changed during this setup.
EOF
echo

if ask_yes_no \
  "Will anyone else work with you on Projects managed from this repository?" \
  no; then
  governance="collaborative"
else
  governance="personal"
fi

issue_repository="$(ask_default \
  "GitHub repository where issues are tracked" "$repository")"
issue_repository="$(validate_issue_repository "$issue_repository")"

if ! ask_yes_no "Does this repository use one GitHub Project?" yes; then
  create_dispatcher_contract
  append_agents_pointer
  bash "$script_dir/validate-contract.sh" "$repository_root"
  success "Added the empty dispatcher and the AGENTS.md starting point."
  configure_dispatcher_projects
  bash "$script_dir/validate-contract.sh" "$repository_root"
  save_onboarding_files
  print_provider_intro
  print_chatgpt_setup
  print_codex_setup
  if [[ "$(dispatcher_route_count)" == "0" ]]; then
    print_empty_dispatcher_next_step
  else
    if [[ -z "$first_request_project_context" ]]; then
      load_first_request_from_dispatcher
    fi
    print_first_request
  fi
  echo
  success "Repository onboarding is complete."
  echo "The initializer did not change any live GitHub issue or Project value."
  exit 0
fi

project_owner="$(ask_default \
  "GitHub user or organisation that owns the Project" "$repository_owner")"
[[ "$project_owner" =~ ^[A-Za-z0-9_.-]+$ ]] ||
  die "invalid Project owner login"

echo
echo "Find the Project number after /projects/ in its web address:"
echo "  https://github.com/users/example/projects/40       means 40"
echo "  https://github.com/orgs/example-org/projects/12    means 12"
project_number="$(require_text "Project number")"
[[ "$project_number" =~ ^[1-9][0-9]*$ ]] ||
  die "Project number must be a positive integer"

discover_project

generated_contract="$(mktemp)"
write_project_contract "$generated_contract" single "" \
  "Project membership; no routing label"

mkdir -p "$repository_root/.projects"
mv "$generated_contract" "$contract_file"
generated_contract=""
mark_contract_path ".projects/project.md"
bash "$script_dir/validate-contract.sh" "$repository_root"
append_agents_pointer
success "Added .projects/project.md and the AGENTS.md starting point."

save_onboarding_files
print_provider_intro
print_chatgpt_setup
print_codex_setup
print_first_request

echo
success "Repository onboarding is complete."
echo "The initializer did not change any live GitHub issue or Project value."

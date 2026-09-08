#!/usr/bin/env bash

# Generic environment preflight for github-projects. The host supplies
# credentials and network access. This script never prints or stores a token.
set +x
set -Eeuo pipefail

readonly GH_CLI_VERSION="2.98.0"
readonly GH_CLI_MINIMUM="2.96.0"
readonly GH_CLI_SHA256_AMD64="3b8ac6b30336802fc1a858d7c084e11cdf24ac1a761ca90b68022d7d729208de"
readonly GH_CLI_SHA256_ARM64="cf689084f3a3618f7eae4a2420d335d74626d65f5e594b9828d125d69f800d86"

setup_tmp_dir=""
repository=""
project_owner=""
project_owner_type=""
project_number=""
project_title=""
contract_root=""
discover_repository=true
skip_install=false
skill_source=""
skill_agent=""
skill_scope="user"
repository_root=""
local_setup=""
local_setup_mode="none"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: setup.sh [options]

Options:
  --repository OWNER/REPO    Verify access to this repository.
  --no-repository            Do not discover or verify a repository.
  --project-owner LOGIN      Verify this user or organisation Project owner.
  --project-owner-type TYPE  Optional owner-type assertion: user or organization.
  --project-number NUMBER    Verify this Project number.
  --project-title TITLE      Also verify the exact Project title.
  --contract-root DIRECTORY  Validate DIRECTORY/.projects/project.md.
  --no-contract              Skip automatic local contract validation.
  --skip-install             Do not install or upgrade gh.
  --install-skill-from REPO  Install github-projects from REPO.
  --agent AGENT              Override the universal agent adapter.
  --scope SCOPE              Skill scope: user or project (default: user).
  --help                     Show this help.
EOF
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
      value = trim($3)
      if (key == wanted) {
        print value
        exit
      }
    }
  ' "$file"
}

run_local_setup() {
  echo "Running repository setup ($local_setup_mode): .projects/setup.sh"
  (
    cd "$repository_root"
    PROJECTS_REPOSITORY_ROOT="$repository_root" \
      PROJECTS_SETUP_MODE="$local_setup_mode" \
      bash "$local_setup"
  )
}

while (($#)); do
  case "$1" in
    --repository)
      (($# >= 2)) || die "--repository requires OWNER/REPO"
      repository="$2"
      discover_repository=false
      shift 2
      ;;
    --no-repository)
      discover_repository=false
      shift
      ;;
    --project-owner)
      (($# >= 2)) || die "--project-owner requires a login"
      project_owner="$2"
      shift 2
      ;;
    --project-owner-type)
      (($# >= 2)) || die "--project-owner-type requires user or organization"
      project_owner_type="$2"
      shift 2
      ;;
    --project-number)
      (($# >= 2)) || die "--project-number requires a number"
      project_number="$2"
      shift 2
      ;;
    --project-title)
      (($# >= 2)) || die "--project-title requires a title"
      project_title="$2"
      shift 2
      ;;
    --contract-root)
      (($# >= 2)) || die "--contract-root requires a directory"
      contract_root="$2"
      shift 2
      ;;
    --no-contract)
      contract_root="-"
      shift
      ;;
    --skip-install)
      skip_install=true
      shift
      ;;
    --install-skill-from)
      (($# >= 2)) || die "--install-skill-from requires a repository or directory"
      skill_source="$2"
      shift 2
      ;;
    --agent)
      (($# >= 2)) || die "--agent requires an agent name"
      skill_agent="$2"
      shift 2
      ;;
    --scope)
      (($# >= 2)) || die "--scope requires user or project"
      skill_scope="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

if [[ -n "$project_owner" || -n "$project_owner_type" || -n "$project_number" || -n "$project_title" ]]; then
  [[ -n "$project_owner" && -n "$project_number" ]] ||
    die "--project-owner and --project-number must be supplied together"
fi
[[ -z "$project_owner_type" || "$project_owner_type" == "user" || "$project_owner_type" == "organization" ]] ||
  die "--project-owner-type must be user or organization"
[[ -z "$project_number" || "$project_number" =~ ^[1-9][0-9]*$ ]] ||
  die "--project-number must be a positive integer"
[[ -z "$repository" || "$repository" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]] ||
  die "--repository must use OWNER/REPO form"
[[ "$skill_scope" == "user" || "$skill_scope" == "project" ]] ||
  die "--scope must be user or project"
if [[ -n "$skill_source" && -z "$skill_agent" ]]; then
  skill_agent="universal"
elif [[ -z "$skill_source" && -n "$skill_agent" ]]; then
  die "--agent requires --install-skill-from"
fi

if [[ "${GITHUB_PROJECT_ADMIN_SETUP_ACTIVE:-}" == "1" ]]; then
  die "repository setup called the shared setup recursively"
fi
export GITHUB_PROJECT_ADMIN_SETUP_ACTIVE=1

if [[ -n "$contract_root" && "$contract_root" != "-" ]]; then
  [[ -d "$contract_root" ]] || die "contract root is not a directory: $contract_root"
  repository_root="$(cd "$contract_root" && pwd)"
elif command -v git >/dev/null 2>&1 &&
     git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  repository_root="$(git rev-parse --show-toplevel)"
else
  repository_root="$(pwd)"
fi

local_setup="$repository_root/.projects/setup.sh"
if [[ -f "$local_setup" ]]; then
  if head -n 20 "$local_setup" |
     grep -Eq '^[[:space:]]*#[[:space:]]*(github-projects|github-project-admin):[[:space:]]*override[[:space:]]*$'; then
    local_setup_mode="override"
  else
    local_setup_mode="extend"
  fi
fi

if [[ "$local_setup_mode" == "override" ]]; then
  run_local_setup
  echo "Repository override setup completed."
  exit 0
fi

main_contract="$repository_root/.projects/project.md"
if [[ "$contract_root" != "-" && -f "$main_contract" ]]; then
  contract_mode="$(contract_table_value "$main_contract" "Mode")"
  contract_repository="$(contract_table_value "$main_contract" "Issue repository")"
  [[ -n "$contract_repository" ]] || die "$main_contract has no Issue repository"

  if [[ -n "$repository" && "$repository" != "$contract_repository" ]]; then
    die "--repository disagrees with $main_contract"
  fi
  repository="$contract_repository"
  discover_repository=false

  if [[ "$contract_mode" == "single" ]]; then
    contract_owner="$(contract_table_value "$main_contract" "Project owner")"
    contract_owner_type="$(contract_table_value "$main_contract" "Owner type")"
    contract_number="$(contract_table_value "$main_contract" "Project number")"
    contract_title="$(contract_table_value "$main_contract" "Project title")"

    [[ -z "$project_owner" || "$project_owner" == "$contract_owner" ]] ||
      die "--project-owner disagrees with $main_contract"
    [[ -z "$project_owner_type" || "$project_owner_type" == "$contract_owner_type" ]] ||
      die "--project-owner-type disagrees with $main_contract"
    [[ -z "$project_number" || "$project_number" == "$contract_number" ]] ||
      die "--project-number disagrees with $main_contract"
    [[ -z "$project_title" || "$project_title" == "$contract_title" ]] ||
      die "--project-title disagrees with $main_contract"

    project_owner="$contract_owner"
    if [[ -z "$project_owner_type" ]]; then
      project_owner_type="$contract_owner_type"
    fi
    project_number="$contract_number"
    project_title="$contract_title"
  fi
fi

if [[ -n "$project_owner" || -n "$project_owner_type" || -n "$project_number" || -n "$project_title" ]]; then
  [[ -n "$project_owner" && -n "$project_number" ]] ||
    die "Project owner and number must resolve together"
fi
[[ -z "$project_owner_type" || "$project_owner_type" == "user" || "$project_owner_type" == "organization" ]] ||
  die "Project owner type must be user or organization"
[[ -z "$project_number" || "$project_number" =~ ^[1-9][0-9]*$ ]] ||
  die "Project number must be a positive integer"
[[ -z "$repository" || "$repository" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]] ||
  die "repository must use OWNER/REPO form"

cleanup() {
  if [[ -n "$setup_tmp_dir" && "$setup_tmp_dir" != "/" && -d "$setup_tmp_dir" ]]; then
    rm -rf -- "$setup_tmp_dir"
  fi
}
trap cleanup EXIT

version_at_least() {
  local actual="${1%%-*}" required="$2" index
  local -a actual_parts required_parts
  IFS=. read -r -a actual_parts <<<"$actual"
  IFS=. read -r -a required_parts <<<"$required"
  for index in 0 1 2; do
    local actual_part="${actual_parts[$index]:-0}"
    local required_part="${required_parts[$index]:-0}"
    [[ "$actual_part" =~ ^[0-9]+$ && "$required_part" =~ ^[0-9]+$ ]] || return 1
    ((actual_part > required_part)) && return 0
    ((actual_part < required_part)) && return 1
  done
  return 0
}

install_gh() {
  local architecture archive_name archive_path expected_sha extracted_binary
  local install_dir user_directory path_line shell_rc

  case "$(uname -m)" in
    x86_64)
      architecture="amd64"
      expected_sha="$GH_CLI_SHA256_AMD64"
      ;;
    aarch64|arm64)
      architecture="arm64"
      expected_sha="$GH_CLI_SHA256_ARM64"
      ;;
    *)
      die "unsupported architecture for automatic gh installation: $(uname -m)"
      ;;
  esac

  for prerequisite in curl install tar sha256sum; do
    command -v "$prerequisite" >/dev/null 2>&1 ||
      die "$prerequisite is required to install gh"
  done

  setup_tmp_dir="$(mktemp -d)"
  archive_name="gh_${GH_CLI_VERSION}_linux_${architecture}.tar.gz"
  archive_path="$setup_tmp_dir/$archive_name"
  curl --fail --location --retry 3 --silent --show-error \
    --output "$archive_path" \
    "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/${archive_name}"
  printf '%s  %s\n' "$expected_sha" "$archive_path" |
    sha256sum --check --status ||
    die "downloaded gh archive failed SHA-256 verification"

  tar --no-same-owner -xzf "$archive_path" -C "$setup_tmp_dir"
  extracted_binary="$setup_tmp_dir/gh_${GH_CLI_VERSION}_linux_${architecture}/bin/gh"
  [[ -x "$extracted_binary" ]] ||
    die "downloaded gh archive did not contain the expected binary"

  user_directory=""
  if [[ -d /usr/local/bin && -w /usr/local/bin ]]; then
    install_dir="/usr/local/bin"
  elif [[ -d /usr/bin && -w /usr/bin ]]; then
    install_dir="/usr/bin"
  else
    command -v getent >/dev/null 2>&1 ||
      die "getent is required to locate a user tool directory"
    user_directory="$(getent passwd "$(id -u)" | cut -d: -f6)"
    [[ -n "$user_directory" && -d "$user_directory" ]] ||
      die "could not resolve a writable user directory for gh"
    install_dir="$user_directory/.local/bin"
  fi

  mkdir -p "$install_dir"
  install -m 0755 "$extracted_binary" "$install_dir/gh"
  export PATH="$install_dir:$PATH"

  if [[ -n "$user_directory" ]]; then
    path_line="$(printf 'export PATH=%q:$PATH' "$install_dir")"
    for shell_rc in "$user_directory/.profile" "$user_directory/.bashrc"; do
      touch "$shell_rc"
      grep -Fqx "$path_line" "$shell_rc" || printf '%s\n' "$path_line" >>"$shell_rc"
    done
  fi
}

gh_is_usable=false
if command -v gh >/dev/null 2>&1; then
  installed_version="$(gh version 2>/dev/null | awk 'NR == 1 { print $3 }')"
  if version_at_least "$installed_version" "$GH_CLI_MINIMUM"; then
    gh_is_usable=true
  fi
fi

if [[ "$gh_is_usable" != true ]]; then
  [[ "$skip_install" == false ]] ||
    die "gh $GH_CLI_MINIMUM or newer is required"
  install_gh
fi

command -v gh >/dev/null 2>&1 || die "gh installation failed"
gh project --help >/dev/null 2>&1 || die "this gh build does not provide Project commands"

gh auth status >/dev/null 2>&1 ||
  die "GitHub is not authenticated; authenticate gh or configure a task-scoped GH_TOKEN"
login="$(gh api user --jq .login 2>/dev/null)" ||
  die "GitHub authentication failed; configure a task-scoped GH_TOKEN or authenticate gh"
[[ -n "$login" ]] || die "GitHub authentication returned no login"

if [[ "$discover_repository" == true && -z "$repository" ]] &&
   command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  repository="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || true)"
fi

if [[ -n "$repository" ]]; then
  observed_repository="$(gh repo view "$repository" --json nameWithOwner --jq .nameWithOwner)"
  [[ "$observed_repository" == "$repository" ]] ||
    die "repository identity mismatch"
  gh issue list --repo "$repository" --limit 1 --json number >/dev/null
fi

if [[ -n "$project_owner" ]]; then
  observed_owner_type="$(gh api "users/$project_owner" --jq .type 2>/dev/null)" ||
    die "could not discover the Project owner type"
  case "$observed_owner_type" in
    User) discovered_owner_type="user" ;;
    Organization) discovered_owner_type="organization" ;;
    *) die "unsupported GitHub Project owner type: $observed_owner_type" ;;
  esac
  if [[ -n "$project_owner_type" && "$project_owner_type" != "$discovered_owner_type" ]]; then
    die "declared Project owner type disagrees with GitHub"
  fi
  project_owner_type="$discovered_owner_type"

  case "$project_owner_type" in
    user)
      project_query='query($login: String!, $number: Int!) {
        user(login: $login) { projectV2(number: $number) { id number title } }
      }'
      project_selector='.data.user.projectV2'
      ;;
    organization)
      project_query='query($login: String!, $number: Int!) {
        organization(login: $login) { projectV2(number: $number) { id number title } }
      }'
      project_selector='.data.organization.projectV2'
      ;;
  esac

  observed_project_number="$(gh api graphql -f query="$project_query" \
    -F login="$project_owner" -F number="$project_number" \
    --jq "$project_selector.number")" ||
    die "could not read the supplied $project_owner_type Project"
  [[ "$observed_project_number" == "$project_number" ]] ||
    die "expected one readable $project_owner_type Project for the supplied owner and number"

  if [[ -n "$project_title" ]]; then
    observed_title="$(gh api graphql -f query="$project_query" \
      -F login="$project_owner" -F number="$project_number" \
      --jq "$project_selector.title")"
    [[ "$observed_title" == "$project_title" ]] ||
      die "Project title mismatch"
  fi
fi

if [[ -n "$skill_source" ]]; then
  skill_install_args=(
    "$skill_source"
    github-projects
    --agent "$skill_agent"
    --scope "$skill_scope"
    --force
  )
  if [[ -d "$skill_source" ]]; then
    skill_install_args+=(--from-local)
  fi
  gh skill install "${skill_install_args[@]}"
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ "$contract_root" != "-" ]]; then
  [[ -f "$script_dir/validate-contract.sh" ]] ||
    die "contract validator is missing; use the complete skill or pass --no-contract"
  if [[ -n "$contract_root" ]]; then
    bash "$script_dir/validate-contract.sh" "$contract_root"
  elif [[ -f "$repository_root/.projects/project.md" ]]; then
    bash "$script_dir/validate-contract.sh" "$repository_root"
  fi
fi

if [[ "$local_setup_mode" == "extend" ]]; then
  run_local_setup
fi

echo "GitHub Project-administration preflight passed for $login."
[[ -z "$repository" ]] || echo "Verified repository: $repository."
[[ -z "$project_owner" ]] || echo "Verified Project: $project_owner/$project_number."

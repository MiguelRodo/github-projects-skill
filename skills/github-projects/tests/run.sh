#!/usr/bin/env bash

set -Eeuo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
skill_dir="$(cd "$test_dir/.." && pwd)"
validator="$skill_dir/scripts/validate-contract.sh"
setup="$skill_dir/scripts/setup.sh"
initializer="$skill_dir/scripts/init-project.sh"

bash -n "$validator"
bash -n "$setup"
bash -n "$initializer"
python3 "$test_dir/test-agent-cli-choice.py"

bash "$validator" "$test_dir/fixtures/single"
bash "$validator" "$test_dir/fixtures/single-user"
bash "$validator" "$test_dir/fixtures/dispatcher"
bash "$validator" "$test_dir/fixtures/empty-dispatcher"
if bash "$validator" "$test_dir/fixtures/invalid" >/dev/null 2>&1; then
  echo "ERROR: lossy Priority mapping unexpectedly validated" >&2
  exit 1
fi
if bash "$validator" "$test_dir/fixtures/invalid-dispatcher" >/dev/null 2>&1; then
  echo "ERROR: mismatched dispatcher leaf unexpectedly validated" >&2
  exit 1
fi
if bash "$validator" "$test_dir/fixtures/invalid-colour" >/dev/null 2>&1; then
  echo "ERROR: unsupported option colour unexpectedly validated" >&2
  exit 1
fi
if bash "$validator" "$test_dir/fixtures/invalid-pending" >/dev/null 2>&1; then
  echo "ERROR: mixed pending and partial Priority mapping unexpectedly validated" >&2
  exit 1
fi
if bash "$validator" "$test_dir/fixtures/invalid-writeup" >/dev/null 2>&1; then
  echo "ERROR: retired minimal issue write-up style unexpectedly validated" >&2
  exit 1
fi
if bash "$validator" "$test_dir/fixtures/invalid-prose" >/dev/null 2>&1; then
  echo "ERROR: unsupported issue prose style unexpectedly validated" >&2
  exit 1
fi

grep -Fqx 'name: github-projects' "$skill_dir/SKILL.md"
test -f "$skill_dir/README.md"
test -f "$skill_dir/references/issue-types.md"
grep -Fq 'Set example#313 to P2.' "$test_dir/short-requests.md"
grep -Fq '## Issue creation styles' "$test_dir/short-requests.md"
grep -Fq 'P3 | Low' "$skill_dir/SKILL.md"
grep -Fq 'Priority mapping status: pending' "$skill_dir/SKILL.md"
grep -Fq 'Deliverable' "$skill_dir/references/issue-types.md"
grep -Fq 'Treat the GitHub Project as the container.' "$skill_dir/references/issue-types.md"
grep -Fq 'use body checkboxes for' "$initializer"
grep -Fq '`Task`, `Bug`, `Enhancement`, `Data`, `Analysis`, `Deliverable`, `Documentation` and `Epic`' \
  "$skill_dir/SKILL.md"
grep -Fq '| Data | PINK |' "$skill_dir/references/issue-types.md"
grep -Fq '`direct`: do only the structural work needed to create the issue' "$skill_dir/SKILL.md"
grep -Fq '`tidy`: the default. Reword and organise the supplied material' "$skill_dir/SKILL.md"
grep -Fq '`natural-direct`' "$skill_dir/SKILL.md"
grep -Fq 'Workstream is not a standard semantic dimension' "$skill_dir/SKILL.md"
grep -Fq 'gh auth login --web --scopes "project,read:org"' "$skill_dir/README.md"
grep -Fq 'https://chatgpt.com/codex/settings/environments' "$skill_dir/README.md"
if grep -Eq '\$\{[^}]+(,,|\^\^)\}|(^|[^[:alnum:]_])(mapfile|readarray)([^[:alnum:]_]|$)|declare[[:space:]]+-A' \
  "$initializer"; then
  echo "ERROR: initializer uses a Bash feature newer than Bash 3.2" >&2
  exit 1
fi

test_tmp_dir="$(mktemp -d)"
cleanup() {
  if [[ -n "$test_tmp_dir" && "$test_tmp_dir" != "/" && -d "$test_tmp_dir" ]]; then
    rm -rf -- "$test_tmp_dir"
  fi
}
trap cleanup EXIT

mkdir -p "$test_tmp_dir/bin"
cat >"$test_tmp_dir/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "${1:-}" in
  version)
    echo "gh version 2.98.0 (test)"
    ;;
  project)
    if [[ "${2:-}" == "--help" ]]; then
      exit 0
    elif [[ "${2:-}" == "list" ]]; then
      echo '{"projects":[]}'
    elif [[ "${2:-}" == "field-list" ]]; then
      printf 'Status\nPriority\n'
    else
      exit 2
    fi
    ;;
  auth)
    [[ "${2:-}" == "status" ]]
    ;;
  api)
    if [[ "${2:-}" == "user" ]]; then
      echo "octocat"
    elif [[ "${2:-}" == users/* ]]; then
      case "${2#users/}" in
        octo-user) echo "User" ;;
        octo-org) echo "Organization" ;;
        *) exit 2 ;;
      esac
    elif [[ "${2:-}" == "graphql" ]]; then
      if [[ -n "${GH_GRAPHQL_LOG:-}" ]]; then
        printf '%s\n' "$*" >>"$GH_GRAPHQL_LOG"
      fi
      if [[ "$*" == *"user(login:"* && "$*" == *"organization(login:"* ]]; then
        echo "combined owner query is forbidden" >&2
        exit 90
      elif [[ "$*" == *"@tsv"* ]]; then
        if [[ "$*" == *"number=13"* ]]; then
          printf '13\tSecond planning\tfalse\n'
        elif [[ "$*" == *"number=14"* ]]; then
          printf '14\tThird planning\tfalse\n'
        else
          printf '12\tExample planning\ttrue\n'
        fi
      elif [[ "$*" == *".number"* ]]; then
        if [[ "$*" == *"organization(login:"* ]]; then echo "12"; else echo "4"; fi
      elif [[ "$*" == *"organization(login:"* ]]; then
        echo "Example planning"
      else
        echo "User planning"
      fi
    else
      exit 2
    fi
    ;;
  repo)
    [[ "${2:-}" == "view" ]]
    if [[ "$*" == *"missing/repo"* ]]; then
      exit 1
    elif [[ "$*" == *"nameWithOwner,visibility"* ]]; then
      printf 'octo-org/example\tPUBLIC\n'
    elif [[ "$*" == *"octo-user/example"* ]]; then
      echo "octo-user/example"
    elif [[ "$*" == *"octo-user/issues"* ]]; then
      echo "octo-user/issues"
    else
      echo "octo-org/example"
    fi
    ;;
  issue)
    [[ "${2:-}" == "list" ]]
    echo "[]"
    ;;
  skill)
    [[ "${2:-}" == "install" ]]
    if [[ -n "${GH_SKILL_LOG:-}" ]]; then
      printf '%s\n' "$*" >>"$GH_SKILL_LOG"
    fi
    ;;
  *)
    exit 2
    ;;
esac
EOF
chmod 0755 "$test_tmp_dir/bin/gh"

secret_value="test-token-must-not-appear"
PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  bash -x "$setup" --skip-install --no-contract \
  --repository octo-org/example \
  --project-owner octo-org --project-owner-type organization --project-number 12 \
  --project-title "Example planning" \
  --install-skill-from octo-org/project-skills --agent codex \
  >"$test_tmp_dir/setup.log" 2>&1

if grep -Fq "$secret_value" "$test_tmp_dir/setup.log"; then
  echo "ERROR: setup output exposed GH_TOKEN" >&2
  exit 1
fi
grep -Fq 'preflight passed' "$test_tmp_dir/setup.log"
grep -Fq 'Verified repository: octo-org/example.' "$test_tmp_dir/setup.log"
grep -Fq 'Verified Project: octo-org/12.' "$test_tmp_dir/setup.log"

PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  bash "$setup" --skip-install --no-contract --no-repository \
  --project-owner octo-user --project-number 4 \
  --project-title "User planning" \
  >"$test_tmp_dir/discovered-user.log" 2>&1
grep -Fq 'Verified Project: octo-user/4.' "$test_tmp_dir/discovered-user.log"

if PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  GH_GRAPHQL_LOG="$test_tmp_dir/mismatch-graphql.log" \
  bash "$setup" --skip-install --no-contract --no-repository \
  --project-owner octo-user --project-owner-type organization --project-number 4 \
  >"$test_tmp_dir/mismatch.log" 2>&1; then
  echo "ERROR: mismatched declared owner type unexpectedly passed" >&2
  exit 1
fi
grep -Fq 'declared Project owner type disagrees with GitHub' "$test_tmp_dir/mismatch.log"
if [[ -s "$test_tmp_dir/mismatch-graphql.log" ]]; then
  echo "ERROR: mismatched owner type reached the Project GraphQL query" >&2
  exit 1
fi

PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  GH_SKILL_LOG="$test_tmp_dir/local-skill.log" \
  bash "$setup" --skip-install --no-contract --no-repository \
  --install-skill-from "$test_dir/fixtures/single" \
  >"$test_tmp_dir/local-skill-output.log" 2>&1
grep -Fq -- "skill install $test_dir/fixtures/single github-projects --agent universal --scope user --force --from-local" \
  "$test_tmp_dir/local-skill.log"

PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  bash "$setup" --skip-install \
  --contract-root "$test_dir/fixtures/single" \
  >"$test_tmp_dir/contract-setup.log" 2>&1
grep -Fq 'Verified repository: octo-org/example.' "$test_tmp_dir/contract-setup.log"
grep -Fq 'Verified Project: octo-org/12.' "$test_tmp_dir/contract-setup.log"

PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  bash "$setup" --skip-install \
  --contract-root "$test_dir/fixtures/single-user" \
  >"$test_tmp_dir/user-contract-setup.log" 2>&1
grep -Fq 'Verified repository: octo-user/example.' "$test_tmp_dir/user-contract-setup.log"
grep -Fq 'Verified Project: octo-user/4.' "$test_tmp_dir/user-contract-setup.log"

mkdir -p "$test_tmp_dir/extend/.projects"
cat >"$test_tmp_dir/extend/.projects/setup.sh" <<'EOF'
#!/usr/bin/env bash
printf '%s:%s\n' "$PROJECTS_SETUP_MODE" "$PROJECTS_REPOSITORY_ROOT" >"$LOCAL_SETUP_LOG"
EOF
extend_setup_sha="$(sha256sum "$test_tmp_dir/extend/.projects/setup.sh" | awk '{print $1}')"
(
  cd "$test_tmp_dir/extend"
  PATH="$test_tmp_dir/bin:$PATH" LOCAL_SETUP_LOG="$test_tmp_dir/extend.log" \
    bash "$setup" --skip-install --no-contract --no-repository \
    >"$test_tmp_dir/extend-output.log" 2>&1
)
grep -Fq "extend:$test_tmp_dir/extend" "$test_tmp_dir/extend.log"
grep -Fq 'Running repository setup (extend)' "$test_tmp_dir/extend-output.log"
[[ "$(sha256sum "$test_tmp_dir/extend/.projects/setup.sh" | awk '{print $1}')" == "$extend_setup_sha" ]]

mkdir -p "$test_tmp_dir/override/.projects"
cat >"$test_tmp_dir/override/.projects/setup.sh" <<'EOF'
#!/usr/bin/env bash
# github-projects: override
printf '%s:%s\n' "$PROJECTS_SETUP_MODE" "$PROJECTS_REPOSITORY_ROOT" >"$LOCAL_SETUP_LOG"
EOF
(
  cd "$test_tmp_dir/override"
  LOCAL_SETUP_LOG="$test_tmp_dir/override.log" \
    bash "$setup" --skip-install --no-contract --no-repository \
    >"$test_tmp_dir/override-output.log" 2>&1
)
grep -Fq "override:$test_tmp_dir/override" "$test_tmp_dir/override.log"
grep -Fq 'Repository override setup completed.' "$test_tmp_dir/override-output.log"
if grep -Fq 'preflight passed' "$test_tmp_dir/override-output.log"; then
  echo "ERROR: override setup unexpectedly ran the shared preflight" >&2
  exit 1
fi

mkdir -p "$test_tmp_dir/legacy-override/.projects"
cat >"$test_tmp_dir/legacy-override/.projects/setup.sh" <<'EOF'
#!/usr/bin/env bash
# github-project-admin: override
printf '%s:%s\n' "$PROJECTS_SETUP_MODE" "$PROJECTS_REPOSITORY_ROOT" >"$LOCAL_SETUP_LOG"
EOF
(
  cd "$test_tmp_dir/legacy-override"
  LOCAL_SETUP_LOG="$test_tmp_dir/legacy-override.log" \
    bash "$setup" --skip-install --no-contract --no-repository \
    >"$test_tmp_dir/legacy-override-output.log" 2>&1
)
grep -Fq "override:$test_tmp_dir/legacy-override" "$test_tmp_dir/legacy-override.log"
grep -Fq 'Repository override setup completed.' "$test_tmp_dir/legacy-override-output.log"

mkdir -p "$test_tmp_dir/init-single"
git -C "$test_tmp_dir/init-single" init -q
printf '# Existing repository guidance\n\nKeep this text.\n' >"$test_tmp_dir/init-single/AGENTS.md"
(
  cd "$test_tmp_dir/init-single"
  printf '\n\n\n\n12\n\n\n' | PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-output.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-single"
grep -Fq 'Keep this text.' "$test_tmp_dir/init-single/AGENTS.md"
grep -Fq '<!-- github-projects:start -->' "$test_tmp_dir/init-single/AGENTS.md"
grep -Fq '| Issue repository | octo-org/example |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Project title | Example planning |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Class | organization issue type | Issue Type |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Priority | pending live inspection | Priority |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Routing | Project membership; no routing label |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Issue write-up style | tidy |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq '| Issue prose style | natural-direct |' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fxq 'Priority mapping status: pending' "$test_tmp_dir/init-single/.projects/project.md"
grep -Fq 'This is a personal Project.' "$test_tmp_dir/init-single/.projects/project.md"
if grep -Fq '| Workstream |' "$test_tmp_dir/init-single/.projects/project.md"; then
  echo "ERROR: initializer generated a standard Workstream dimension" >&2
  exit 1
fi
grep -Fq 'I will ask a few questions to configure' "$test_tmp_dir/init-output.log"
awk '
  previous == "No live issue or Project value will be changed during this setup." &&
    $0 == "" { found = 1 }
  { previous = $0 }
  END { exit(found ? 0 : 1) }
' "$test_tmp_dir/init-output.log"
grep -Fq 'after /projects/ in its web address' "$test_tmp_dir/init-output.log"
grep -Fq 'GitHub user or organisation that owns the Project' "$initializer"
grep -Fq '1. Check GitHub' "$test_tmp_dir/init-output.log"
grep -Fq 'Choose how you will use the repository' "$test_tmp_dir/init-output.log"
grep -Fq 'I will now show two ways to use the repository' "$test_tmp_dir/init-output.log"
grep -Fq 'Use the repository with a chat interface' "$test_tmp_dir/init-output.log"
grep -Fq 'Use the repository with an execution-capable agent' "$test_tmp_dir/init-output.log"
grep -Fq 'https://chatgpt.com/codex/settings/environments' "$test_tmp_dir/init-output.log"
grep -Fq 'Issue Type from the issue evidence' "$test_tmp_dir/init-output.log"
grep -Fq 'do not change' "$test_tmp_dir/init-output.log"
grep -Fq 'until I approve them' "$test_tmp_dir/init-output.log"
grep -Fq 'committed and pushed' "$test_tmp_dir/init-output.log"
grep -Fq 'The files were left uncommitted.' "$test_tmp_dir/init-output.log"
if grep -Eq 'Current Project fields|following repository contract|Choose the provider.s Priority values|Does Project membership alone|How should the agent proceed|Do this now' \
  "$test_tmp_dir/init-output.log"; then
  echo "ERROR: initializer printed a removed diagnostic or question" >&2
  exit 1
fi
PATH="$test_tmp_dir/bin:$PATH" GH_TOKEN="$secret_value" \
  bash "$setup" --skip-install --contract-root "$test_tmp_dir/init-single" \
  >"$test_tmp_dir/init-contract-setup.log" 2>&1
grep -Fq 'Verified Project: octo-org/12.' "$test_tmp_dir/init-contract-setup.log"

init_contract_sha="$(sha256sum "$test_tmp_dir/init-single/.projects/project.md" | awk '{print $1}')"
init_agents_sha="$(sha256sum "$test_tmp_dir/init-single/AGENTS.md" | awk '{print $1}')"
(
  cd "$test_tmp_dir/init-single"
  printf '\n' | PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-rerun.log" 2>&1
)
grep -Fq 'existing repository setup was not replaced' "$test_tmp_dir/init-rerun.log"
[[ "$(sha256sum "$test_tmp_dir/init-single/.projects/project.md" | awk '{print $1}')" == "$init_contract_sha" ]]
[[ "$(sha256sum "$test_tmp_dir/init-single/AGENTS.md" | awk '{print $1}')" == "$init_agents_sha" ]]

mkdir -p "$test_tmp_dir/init-multiple"
git -C "$test_tmp_dir/init-multiple" init -q
(
  cd "$test_tmp_dir/init-multiple"
  printf '%s\n' '' '' n y '' 12 '' '' n n y | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-multiple.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-multiple"
grep -Fq '<!-- github-projects:start -->' "$test_tmp_dir/init-multiple/AGENTS.md"
grep -Fq '| Mode | dispatcher |' \
  "$test_tmp_dir/init-multiple/.projects/project.md"
grep -Fq '| Governance | personal |' \
  "$test_tmp_dir/init-multiple/.projects/project.md"
grep -Fq '| example-planning | project:example-planning | 12 | .projects/projects/example-planning.md |' \
  "$test_tmp_dir/init-multiple/.projects/project.md"
grep -Fq '| Mode | project |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
grep -Fq '| Project key | example-planning |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
grep -Fq '| Owner type | organization |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
grep -Fq '| Routing | label:project:example-planning |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
grep -Fq '| Issue write-up style | tidy |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
grep -Fq '| Issue prose style | natural-direct |' \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"
if grep -Fq '| Issue prose style |' "$test_tmp_dir/init-multiple/.projects/project.md"; then
  echo "ERROR: dispatcher root unexpectedly received an Issue prose style" >&2
  exit 1
fi
if grep -Fq '| Workstream |' "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md"; then
  echo "ERROR: dispatcher child generated a standard Workstream dimension" >&2
  exit 1
fi
test -e "$test_tmp_dir/init-multiple/.projects/projects/.gitkeep"
grep -Fq 'Add Projects' "$test_tmp_dir/init-multiple.log"
grep -Fq 'Added Project octo-org/12 as route example-planning.' \
  "$test_tmp_dir/init-multiple.log"
grep -Fq '  .projects/projects/.gitkeep' "$test_tmp_dir/init-multiple.log"
grep -Fq '  .projects/projects/example-planning.md' \
  "$test_tmp_dir/init-multiple.log"
grep -Fq 'Use the same first request in a chat interface or an execution-capable agent' \
  "$test_tmp_dir/init-multiple.log"
grep -Fq 'optional sub-project labels only where they are genuinely useful.' \
  "$test_tmp_dir/init-multiple.log"
if grep -Fq 'Finish the multi-Project routing' "$test_tmp_dir/init-multiple.log"; then
  echo "ERROR: multi-Project onboarding printed the removed handoff" >&2
  exit 1
fi

first_route_sha="$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md" | awk '{print $1}')"
pre_collision_dispatcher_sha="$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/project.md" | awk '{print $1}')"
if (
  cd "$test_tmp_dir/init-multiple"
  printf '%s\n' y '' 14 example-planning | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-multiple-collision.log" 2>&1
); then
  echo "ERROR: duplicate Project key unexpectedly replaced an existing route" >&2
  exit 1
fi
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/project.md" | awk '{print $1}')" == \
  "$pre_collision_dispatcher_sha" ]]
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md" | awk '{print $1}')" == \
  "$first_route_sha" ]]
test ! -e "$test_tmp_dir/init-multiple/.projects/projects/third-planning.md"
grep -Fq 'Project key example-planning is already configured' \
  "$test_tmp_dir/init-multiple-collision.log"

(
  cd "$test_tmp_dir/init-multiple"
  printf '%s\n' y '' 13 '' '' n n n | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-multiple-add.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-multiple"
grep -Fq '| example-planning | project:example-planning | 12 | .projects/projects/example-planning.md |' \
  "$test_tmp_dir/init-multiple/.projects/project.md"
grep -Fq '| second-planning | project:second-planning | 13 | .projects/projects/second-planning.md |' \
  "$test_tmp_dir/init-multiple/.projects/project.md"
grep -Fq '| Project title | Second planning |' \
  "$test_tmp_dir/init-multiple/.projects/projects/second-planning.md"
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md" | awk '{print $1}')" == \
  "$first_route_sha" ]]
grep -Fq 'The existing dispatcher and routes will be preserved.' \
  "$test_tmp_dir/init-multiple-add.log"
grep -Fq 'Added Project octo-org/13 as route second-planning.' \
  "$test_tmp_dir/init-multiple-add.log"
grep -Fq '  .projects/projects/second-planning.md' \
  "$test_tmp_dir/init-multiple-add.log"

dispatcher_sha="$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/project.md" | awk '{print $1}')"
second_route_sha="$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/second-planning.md" | awk '{print $1}')"
(
  cd "$test_tmp_dir/init-multiple"
  printf '%s\n' y '' 12 n n n | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-multiple-noop.log" 2>&1
)
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/project.md" | awk '{print $1}')" == \
  "$dispatcher_sha" ]]
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/example-planning.md" | awk '{print $1}')" == \
  "$first_route_sha" ]]
[[ "$(sha256sum \
  "$test_tmp_dir/init-multiple/.projects/projects/second-planning.md" | awk '{print $1}')" == \
  "$second_route_sha" ]]
grep -Fq 'Project octo-org/12 is already configured; no route was changed.' \
  "$test_tmp_dir/init-multiple-noop.log"

mkdir -p "$test_tmp_dir/init-collaborative-multiple"
git -C "$test_tmp_dir/init-collaborative-multiple" init -q
(
  cd "$test_tmp_dir/init-collaborative-multiple"
  printf '%s\n' y '' n n n | PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-collaborative-multiple.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-collaborative-multiple"
grep -Fq '| Mode | dispatcher |' \
  "$test_tmp_dir/init-collaborative-multiple/.projects/project.md"
grep -Fq '| Governance | collaborative |' \
  "$test_tmp_dir/init-collaborative-multiple/.projects/project.md"
test -e "$test_tmp_dir/init-collaborative-multiple/.projects/projects/.gitkeep"
test "$(find "$test_tmp_dir/init-collaborative-multiple/.projects/projects" \
  -type f -name '*.md' | wc -l | tr -d ' ')" = "0"
grep -Fq 'Valid empty GitHub Project dispatcher' \
  "$test_tmp_dir/init-collaborative-multiple.log"
grep -Fq 'it cannot resolve ordinary Project requests yet' \
  "$test_tmp_dir/init-collaborative-multiple.log"

mkdir -p "$test_tmp_dir/init-collaborative-single"
git -C "$test_tmp_dir/init-collaborative-single" init -q
(
  cd "$test_tmp_dir/init-collaborative-single"
  printf '%s\n' y '' '' '' 12 n n | PATH="$test_tmp_dir/bin:$PATH" \
    bash "$initializer" >"$test_tmp_dir/init-collaborative-single.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-collaborative-single"
grep -Fq 'This is a collaborative Project.' \
  "$test_tmp_dir/init-collaborative-single/.projects/project.md"

mkdir -p "$test_tmp_dir/init-commit" "$test_tmp_dir/init-remote.git"
git -C "$test_tmp_dir/init-remote.git" init -q --bare
git -C "$test_tmp_dir/init-commit" init -q -b main
git -C "$test_tmp_dir/init-commit" config user.name "Setup Test"
git -C "$test_tmp_dir/init-commit" config user.email "setup@example.invalid"
printf 'base\n' >"$test_tmp_dir/init-commit/base.txt"
git -C "$test_tmp_dir/init-commit" add base.txt
git -C "$test_tmp_dir/init-commit" commit -qm "Base"
git -C "$test_tmp_dir/init-commit" remote add origin "$test_tmp_dir/init-remote.git"
git -C "$test_tmp_dir/init-commit" push -qu origin main
printf 'keep staged\n' >"$test_tmp_dir/init-commit/unrelated.txt"
git -C "$test_tmp_dir/init-commit" add unrelated.txt
(
  cd "$test_tmp_dir/init-commit"
  PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-commit.log" 2>&1 <<'EOF'




12
y
n
EOF
)
[[ "$(git -C "$test_tmp_dir/init-commit" log -1 --format=%s)" == \
   "Configure GitHub Project administration" ]]
grep -Fqx 'unrelated.txt' <(git -C "$test_tmp_dir/init-commit" diff --cached --name-only)
if git -C "$test_tmp_dir/init-commit" show --format= --name-only HEAD | grep -Fqx 'unrelated.txt'; then
  echo "ERROR: onboarding commit included an unrelated staged file" >&2
  exit 1
fi
[[ "$(git -C "$test_tmp_dir/init-commit" rev-parse HEAD)" == \
   "$(git -C "$test_tmp_dir/init-remote.git" rev-parse refs/heads/main)" ]]
grep -Fq 'Pushed the onboarding commit.' "$test_tmp_dir/init-commit.log"

mkdir -p "$test_tmp_dir/init-push-failure"
git -C "$test_tmp_dir/init-push-failure" init -q -b main
git -C "$test_tmp_dir/init-push-failure" config user.name "Setup Test"
git -C "$test_tmp_dir/init-push-failure" config user.email "setup@example.invalid"
(
  cd "$test_tmp_dir/init-push-failure"
  PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-push-failure.log" 2>&1 <<'EOF'




12
y
n
EOF
)
grep -Fq 'The commit is safe locally, but Git could not push it.' \
  "$test_tmp_dir/init-push-failure.log"
grep -Fq 'git push -u origin main' "$test_tmp_dir/init-push-failure.log"
[[ "$(git -C "$test_tmp_dir/init-push-failure" log -1 --format=%s)" == \
   "Configure GitHub Project administration" ]]

mkdir -p "$test_tmp_dir/init-separate-single"
git -C "$test_tmp_dir/init-separate-single" init -q
printf '# Existing guidance\n' >"$test_tmp_dir/init-separate-single/AGENTS.md"
(
  cd "$test_tmp_dir/init-separate-single"
  printf '%s\n' '' 'octo-user/issues' '' '' 12 n n | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-separate-single.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-separate-single"
grep -Fq '| Issue repository | octo-user/issues |' \
  "$test_tmp_dir/init-separate-single/.projects/project.md"
grep -Fq '<!-- github-projects:start -->' \
  "$test_tmp_dir/init-separate-single/AGENTS.md"
if grep -Fq 'octo-user/issues' "$test_tmp_dir/init-separate-single/AGENTS.md"; then
  echo "ERROR: AGENTS.md leaked the separate issue repository destination" >&2
  exit 1
fi

mkdir -p "$test_tmp_dir/init-separate-multiple"
git -C "$test_tmp_dir/init-separate-multiple" init -q
(
  cd "$test_tmp_dir/init-separate-multiple"
  printf '%s\n' '' 'octo-user/issues' n y '' 12 '' '' n n n | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-separate-multiple.log" 2>&1
)
bash "$validator" "$test_tmp_dir/init-separate-multiple"
grep -Fq '| Issue repository | octo-user/issues |' \
  "$test_tmp_dir/init-separate-multiple/.projects/project.md"
grep -Fq '| Issue repository | octo-user/issues |' \
  "$test_tmp_dir/init-separate-multiple/.projects/projects/example-planning.md"
if grep -Fq 'octo-user/issues' "$test_tmp_dir/init-separate-multiple/AGENTS.md"; then
  echo "ERROR: AGENTS.md leaked the separate issue repository destination" >&2
  exit 1
fi

mkdir -p "$test_tmp_dir/init-invalid-issue-repo"
git -C "$test_tmp_dir/init-invalid-issue-repo" init -q
if (
  cd "$test_tmp_dir/init-invalid-issue-repo"
  printf '%s\n' '' 'missing/repo' | \
    PATH="$test_tmp_dir/bin:$PATH" bash "$initializer" \
    >"$test_tmp_dir/init-invalid-issue-repo.log" 2>&1
); then
  echo "ERROR: invalid issue repository unexpectedly passed onboarding" >&2
  exit 1
fi
grep -Fq 'could not find or access issue repository: missing/repo' \
  "$test_tmp_dir/init-invalid-issue-repo.log"
test ! -e "$test_tmp_dir/init-invalid-issue-repo/.projects/project.md"

echo "github-projects tests passed"

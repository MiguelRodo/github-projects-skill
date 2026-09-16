#!/usr/bin/env bash

set -Eeuo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
preflight="$(cd "$test_dir/../scripts" && pwd)/queue-preflight.sh"
tmp="$(mktemp -d)" || exit 1
trap 'rm -rf "$tmp"' EXIT
workspace="$tmp/workspace"
provider="$tmp/provider-read"

mkdir -p "$workspace/issues/.projects/projects" "$workspace/other/.projects"

cat >"$workspace/issues/.projects/project.md" <<'EOF'
# Dispatcher
| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | octo/issues |
| Privacy | private |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |
| personal | project:personal | 38 | .projects/projects/personal.md |
EOF

cat >"$workspace/issues/.projects/projects/personal.md" <<'EOF'
# Personal
| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | project |
| Project key | personal |
| Issue repository | octo/issues |
| Project owner | octo |
| Owner type | user |
| Project number | 38 |
| Project title | Personal |
| Routing | label:project:personal |
| Privacy | private |
| Chat implementation label | pj:implement-chat |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Priority | project field | Priority |

## Priority mapping

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |

## Sub-project vocabulary

| Key | Label | Purpose |
| --- | --- | --- |
| monitoring | subproject:monitoring | Monitoring |
| finances | subproject:finances | Finances |
EOF

cat >"$workspace/other/.projects/project.md" <<'EOF'
# Other
| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Project key | other |
| Issue repository | octo/other |
| Project owner | octo |
| Owner type | user |
| Project number | 7 |
| Project title | Other |
| Routing | Project membership only |
| Privacy | public |
| Chat implementation label | pj:implement-chat |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Priority | project field | Priority |

## Priority mapping

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |
EOF

cat >"$provider" <<'EOF'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >>"$PROVIDER_LOG"
if [ "${1:-}" = auth ] && [ "${2:-}" = status ]; then
  exit 0
fi
if [ "${1:-}" != issue ] || [ "${2:-}" != list ]; then
  echo "unexpected provider call: $*" >&2
  exit 90
fi
args=" $* "
if [[ "$args" == *" --repo octo/issues "* &&
      "$args" == *" --state open "* &&
      "$args" == *" --label pj:implement-chat "* &&
      "$args" == *" --label project:personal "* &&
      "$args" == *" --label subproject:monitoring "* ]]; then
  printf '42\thttps://github.com/octo/issues/issues/42\n'
elif [[ "$args" == *" --repo octo/issues "* &&
        "$args" == *" --label subproject:finances "* ]]; then
  :
elif [[ "$args" == *" --repo octo/other "* ]]; then
  printf '9\thttps://github.com/octo/other/issues/issues/9\n'
fi
EOF
chmod +x "$provider"

# A repository selector should isolate this run from unrelated broken managed
# contracts. Without early repository filtering, validation of this fixture
# would fail before octo/issues is queried.
mkdir -p "$workspace/broken/.projects"
cat >"$workspace/broken/.projects/project.md" <<'EOF'
# Broken unrelated contract
| Key | Value |
| --- | --- |
| Mode | nonsense |
| Issue repository | octo/broken |
EOF

run_preflight() {
  PROVIDER_LOG="$tmp/provider.log" PROJECTS_GH_BIN="$provider" bash "$preflight" --workspace "$workspace" "$@"
}

assert_line() {
  local wanted="$1" output="$2"
  if ! grep -Fqx "$wanted" <<<"$output"; then
    printf 'missing expected line: %s\nactual output:\n%s\nprovider calls:\n' "$wanted" "$output" >&2
    cat "$tmp/provider.log" >&2
    exit 1
  fi
}

: >"$tmp/provider.log"
output="$(run_preflight --repo octo/issues --project personal --subproject monitoring)"
expected_status="$(printf 'status\tready')"
expected_candidate="$(printf 'candidate\tocto/issues\t42\thttps://github.com/octo/issues/issues/42\tpersonal\tmonitoring\t%s\t%s' "$workspace/issues" "$workspace/issues/.projects/projects/personal.md")"
assert_line "$expected_status" "$output"
assert_line "$expected_candidate" "$output"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"
rm -rf "$workspace/broken"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
assert_line "$(printf 'status\tempty')" "$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
assert_line "$(printf 'status\tunmatched')" "$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"

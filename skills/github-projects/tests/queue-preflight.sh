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
if [ "$1" = auth ] && [ "$2" = status ]; then
  exit 0
fi
if [ "$1" != issue ] || [ "$2" != list ]; then
  echo "unexpected provider call: $*" >&2
  exit 90
fi
args=" $* "
case "$args" in
  *" --repo octo/issues "*" --state open "*" --label pj:implement-chat "*" --label project:personal "*" --label subproject:monitoring "*)
    printf '42\thttps://github.com/octo/issues/issues/42\n'
    ;;
  *" --repo octo/issues "*" --label subproject:finances "*)
    ;;
  *" --repo octo/other "*)
    printf '9\thttps://github.com/octo/other/issues/9\n'
    ;;
esac
EOF
chmod +x "$provider"

run_preflight() {
  PROVIDER_LOG="$tmp/provider.log" PROJECTS_GH_BIN="$provider" \
    bash "$preflight" --workspace "$workspace" "$@"
}

: >"$tmp/provider.log"
output="$(run_preflight --repo octo/issues --project personal --subproject monitoring)"
grep -Fqx $'status\tready' <<<"$output"
grep -Fqx "candidate"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"octo/issues"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"42"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"https://github.com/octo/issues/issues/42"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"personal"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"monitoring"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"
\t'"$workspace/issues" <<<"$output"
grep -Fq -- '--state open' "$tmp/provider.log"
grep -Fq -- '--label pj:implement-chat' "$tmp/provider.log"
grep -Fq -- '--label project:personal' "$tmp/provider.log"
grep -Fq -- '--label subproject:monitoring' "$tmp/provider.log"
! grep -Fq -- '--repo octo/other' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject finances)"
grep -Fqx $'status\tempty' <<<"$output"
grep -Fq -- '--label subproject:finances' "$tmp/provider.log"

: >"$tmp/provider.log"
output="$(run_preflight --project personal --subproject missing)"
grep -Fqx $'status\tunmatched' <<<"$output"
! grep -Fq 'issue list' "$tmp/provider.log"

echo "queue preflight tests passed"

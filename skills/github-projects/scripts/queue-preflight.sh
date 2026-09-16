#!/usr/bin/env bash

set -Eeuo pipefail

die() {
  echo "queue preflight: $*" >&2
  exit 1
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

table_value() {
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

same_ci() {
  [ "${1,,}" = "${2,,}" ]
}

valid_repo_selector() {
  [[ "$1" =~ ^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)?$ ]]
}

valid_scope_selector() {
  local pattern='^[A-Za-z0-9_. -]+$'
  [[ "$1" =~ $pattern ]]
}

repo_matches() {
  local repository="$1" selector="$2"
  [ -z "$selector" ] && return 0
  if [[ "$selector" == */* ]]; then
    same_ci "$repository" "$selector"
  else
    same_ci "${repository##*/}" "$selector"
  fi
}

subproject_label() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    function lower(value) {
      return tolower(value)
    }
    /^## Sub-project vocabulary[[:space:]]*$/ { in_section = 1; next }
    in_section && /^## / { exit }
    in_section && /^\|/ {
      key = trim($2)
      label = trim($3)
      if (lower(key) == lower(wanted)) {
        print key "\t" label
        exit
      }
    }
  ' "$file"
}

workspace=""
repo_selector=""
project_selector=""
subproject_selector=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --workspace)
      [ "$#" -ge 2 ] || die "--workspace requires a path"
      workspace="$2"
      shift 2
      ;;
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a selector"
      repo_selector="$2"
      shift 2
      ;;
    --project)
      [ "$#" -ge 2 ] || die "--project requires a selector"
      project_selector="$2"
      shift 2
      ;;
    --subproject)
      [ "$#" -ge 2 ] || die "--subproject requires a selector"
      subproject_selector="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$workspace" ] || die "--workspace is required"
[ -d "$workspace" ] || die "workspace does not exist: $workspace"
[ -z "$repo_selector" ] || valid_repo_selector "$repo_selector" ||
  die "invalid repository selector: $repo_selector"
[ -z "$project_selector" ] || valid_scope_selector "$project_selector" ||
  die "invalid Project selector: $project_selector"
[ -z "$subproject_selector" ] || valid_scope_selector "$subproject_selector" ||
  die "invalid sub-project selector: $subproject_selector"

gh_bin="${PROJECTS_GH_BIN:-gh}"
command -v "$gh_bin" >/dev/null 2>&1 || die "GitHub CLI (gh) is required"
"$gh_bin" auth status >/dev/null 2>&1 || die "gh is not authenticated"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || exit 1
validator="$script_dir/validate-contract.sh"
[ -f "$validator" ] || die "contract validator is missing: $validator"

tmp="$(mktemp -d)" || exit 1
trap 'rm -rf "$tmp"' EXIT
scopes="$tmp/scopes"
candidates="$tmp/candidates"
: >"$scopes"
: >"$candidates"

add_scope() {
  local root="$1" contract="$2" route_label="$3"
  local repository queue_label project_identity routing sub_row sub_key sub_label

  repository="$(table_value "$contract" "Issue repository")"
  [ -n "$repository" ] || die "$contract is missing Issue repository"
  repo_matches "$repository" "$repo_selector" || return 0

  project_identity="$(table_value "$contract" "Project key")"
  [ -n "$project_identity" ] || project_identity="$(table_value "$contract" "Project title")"
  [ -n "$project_identity" ] || die "$contract is missing Project identity"
  [ -z "$project_selector" ] || same_ci "$project_identity" "$project_selector" || return 0

  queue_label="$(table_value "$contract" "Chat implementation label")"
  [ -n "$queue_label" ] || queue_label="pj:implement-chat"
  [ "$queue_label" != "disabled" ] || return 0

  sub_key="-"
  sub_label="-"
  if [ -n "$subproject_selector" ]; then
    sub_row="$(subproject_label "$contract" "$subproject_selector")"
    [ -n "$sub_row" ] || return 0
    IFS=$'\t' read -r sub_key sub_label <<<"$sub_row"
    same_ci "$sub_label" "subproject:$sub_key" ||
      die "$contract has invalid sub-project label '$sub_label' for key '$sub_key'"
  fi

  if [ -z "$route_label" ]; then
    routing="$(table_value "$contract" "Routing")"
    case "$routing" in
      label:*) route_label="${routing#label:}" ;;
    esac
  fi
  [ -n "$route_label" ] || route_label="-"

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$repository" "$queue_label" "$route_label" "$sub_label" "$project_identity" "$sub_key" "$root" "$contract" >>"$scopes"
}

for root in "$workspace"/*; do
  [ -d "$root" ] || continue
  main="$root/.projects/project.md"
  [ -f "$main" ] || continue

  # Repository selection is available from the root contract, so a narrowly
  # scoped run need not validate unrelated managed repositories first.
  root_repository="$(table_value "$main" "Issue repository")"
  if [ -n "$repo_selector" ] && [ -n "$root_repository" ] &&
     ! repo_matches "$root_repository" "$repo_selector"; then
    continue
  fi

  if ! validation="$(bash "$validator" "$root" 2>&1)"; then
    die "invalid managed contract at $root: $validation"
  fi

  mode="$(table_value "$main" "Mode")"
  case "$mode" in
    single)
      add_scope "$root" "$main" ""
      ;;
    dispatcher)
      while IFS=$'\t' read -r project_key route_label child; do
        [ -n "$project_key" ] || continue
        [ -z "$project_selector" ] || same_ci "$project_key" "$project_selector" || continue
        [ -n "$child" ] || die "$main contains an incomplete route for $project_key"
        add_scope "$root" "$root/$child" "$route_label"
      done < <(
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
            label = trim($3)
            child = trim($5)
            if (key != "" && key != "Project key" && key != "---") {
              print key "\t" label "\t" child
            }
          }
        ' "$main"
      )
      ;;
    *)
      die "$main has unsupported Mode '$mode'"
      ;;
  esac
done

if [ ! -s "$scopes" ]; then
  printf 'status\tunmatched\n'
  exit 0
fi

sort -u "$scopes" -o "$scopes"

while IFS=$'\t' read -r repository queue_label route_label sub_label project_identity sub_key root contract; do
  args=(issue list --repo "$repository" --state open --label "$queue_label" --limit 1000 --json number,url)
  [ "$route_label" = "-" ] || args+=(--label "$route_label")
  [ "$sub_label" = "-" ] || args+=(--label "$sub_label")

  if ! found="$("$gh_bin" "${args[@]}" --jq '.[] | [.number, .url] | @tsv' 2>&1)"; then
    die "GitHub queue read failed for $repository: $found"
  fi

  [ -n "$found" ] || continue
  count="$(printf '%s\n' "$found" | sed '/^$/d' | wc -l)"
  [ "$count" -lt 1000 ] ||
    die "queue scope $repository/$project_identity returned at least 1000 issues; refine the selectors"

  while IFS=$'\t' read -r number url; do
    [ -n "$number" ] || continue
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$repository" "$number" "$url" "$project_identity" "$sub_key" "$root" "$contract" >>"$candidates"
  done <<<"$found"
done <"$scopes"

if [ ! -s "$candidates" ]; then
  printf 'status\tempty\n'
  exit 0
fi

awk -F'\t' '!seen[$3]++' "$candidates" >"$tmp/unique"
candidate_count="$(wc -l <"$tmp/unique")"
[ "$candidate_count" -le 200 ] ||
  die "queue preflight found $candidate_count candidates; refine the selectors"

printf 'status\tready\n'
while IFS=$'\t' read -r repository number url project_identity sub_key root contract; do
  printf 'candidate\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$repository" "$number" "$url" "$project_identity" "$sub_key" "$root" "$contract"
done <"$tmp/unique"

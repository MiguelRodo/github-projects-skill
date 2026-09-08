#!/usr/bin/env bash

set -Eeuo pipefail

die() {
  echo "ERROR: $*" >&2
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

require_table_value() {
  local file="$1" key="$2" value
  value="$(table_value "$file" "$key")"
  [[ -n "$value" ]] || die "$file is missing table value: $key"
  printf '%s' "$value"
}

priority_value() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## Priority mapping[[:space:]]*$/ { in_mapping = 1; next }
    in_mapping && /^## / { exit }
    in_mapping && /^\|/ {
      key = trim($2)
      value = trim($3)
      if (key == wanted) {
        print value
      }
    }
  ' "$file"
}

validate_priority_mapping() {
  local file="$1" common provider providers="" count duplicate pending_count
  grep -Eq '^## Priority mapping[[:space:]]*$' "$file" ||
    die "$file is missing the Priority mapping section"

  if grep -Fxq 'Priority mapping status: pending' "$file"; then
    pending_count="$(grep -Fxc 'Priority mapping status: pending' "$file")"
    [[ "$pending_count" == "1" ]] ||
      die "$file must declare the pending Priority status exactly once"
    for common in P0 P1 P2 P3; do
      provider="$(priority_value "$file" "$common")"
      [[ -z "$provider" ]] ||
        die "$file mixes a pending Priority status with a $common mapping"
    done
    return 0
  fi

  for common in P0 P1 P2 P3; do
    provider="$(priority_value "$file" "$common")"
    [[ -n "$provider" ]] || die "$file is missing a non-empty $common mapping"
    count="$(grep -Ec "^\\|[[:space:]]*$common[[:space:]]*\\|" "$file")"
    [[ "$count" == "1" ]] || die "$file must map $common exactly once"
    providers+="$provider"$'\n'
  done

  duplicate="$(printf '%s' "$providers" | sed '/^$/d' | sort | uniq -d | head -n 1)"
  [[ -z "$duplicate" ]] ||
    die "$file Priority mapping is not one-to-one"
}

validate_colour_tables() {
  local file="$1" option colour
  while IFS=$'\t' read -r option colour; do
    [[ -n "$option" ]] || die "$file has an empty option in a colour table"
    case "$colour" in
      BLUE|GRAY|GREEN|ORANGE|PINK|PURPLE|RED|YELLOW) ;;
      *) die "$file has unsupported colour '$colour' for option '$option'" ;;
    esac
  done < <(awk -F'|' '
    function trim(value) {
      sub(/^[[:space:]]+/, "", value)
      sub(/[[:space:]]+$/, "", value)
      return value
    }
    /^## / { in_colour_table = 0 }
    /^\|/ {
      option = trim($2)
      colour = trim($3)
      if (option == "Option" && colour == "Colour") {
        in_colour_table = 1
        next
      }
      if (in_colour_table && option != "---" && colour != "---") {
        print option "\t" colour
      }
      next
    }
    in_colour_table && /[^[:space:]]/ { in_colour_table = 0 }
  ' "$file")
}

validate_issue_writeup_style() {
  local file="$1" style
  style="$(table_value "$file" "Issue write-up style")"
  case "$style" in
    ""|direct|tidy|unrestricted) ;;
    *) die "$file has unsupported Issue write-up style '$style'; use direct, tidy or unrestricted" ;;
  esac
}

validate_issue_prose_style() {
  local file="$1" style
  style="$(table_value "$file" "Issue prose style")"
  case "$style" in
    ""|natural-direct) ;;
    *) die "$file has unsupported Issue prose style '$style'; use natural-direct" ;;
  esac
}

validate_project_file() {
  local file="$1" expected_mode="$2" version mode repository owner owner_type number title
  [[ -f "$file" ]] || die "missing Project contract: $file"

  version="$(require_table_value "$file" "Contract version")"
  [[ "$version" == "1" ]] || die "$file has unsupported Contract version"
  mode="$(require_table_value "$file" "Mode")"
  [[ "$mode" == "$expected_mode" ]] || die "$file must use Mode $expected_mode"
  if [[ "$expected_mode" == "project" ]]; then
    require_table_value "$file" "Project key" >/dev/null
  fi

  repository="$(require_table_value "$file" "Issue repository")"
  [[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
    die "$file has an invalid Issue repository"
  owner="$(require_table_value "$file" "Project owner")"
  [[ "$owner" =~ ^[A-Za-z0-9_.-]+$ ]] || die "$file has an invalid Project owner"
  owner_type="$(table_value "$file" "Owner type")"
  [[ -z "$owner_type" || "$owner_type" == "user" || "$owner_type" == "organization" ]] ||
    die "$file Owner type must be user or organization when supplied"
  number="$(require_table_value "$file" "Project number")"
  [[ "$number" =~ ^[1-9][0-9]*$ ]] || die "$file has an invalid Project number"
  title="$(require_table_value "$file" "Project title")"
  [[ -n "$title" ]] || die "$file has an empty Project title"
  require_table_value "$file" "Routing" >/dev/null
  require_table_value "$file" "Privacy" >/dev/null

  grep -Eq '^## Field locations[[:space:]]*$' "$file" ||
    die "$file is missing Field locations"
  grep -Eq '^\|[[:space:]]*Priority[[:space:]]*\|' "$file" ||
    die "$file does not declare the Priority field location"
  validate_priority_mapping "$file"
  validate_colour_tables "$file"
  validate_issue_writeup_style "$file"
  validate_issue_prose_style "$file"

  if grep -Eq '(gh[pousr]_[A-Za-z0-9]{20,}|GH_TOKEN[[:space:]]*=|GITHUB_TOKEN[[:space:]]*=)' "$file"; then
    die "$file appears to contain a credential"
  fi
}

validate_dispatcher() {
  local file="$1" root="$2" version mode repository route_count=0
  local project_key routing_label project_number contract extra leaf
  local leaf_key leaf_label leaf_number leaf_repository
  local keys=$'\n' labels=$'\n' numbers=$'\n'

  version="$(require_table_value "$file" "Contract version")"
  [[ "$version" == "1" ]] || die "$file has unsupported Contract version"
  mode="$(require_table_value "$file" "Mode")"
  [[ "$mode" == "dispatcher" ]] || die "$file must use Mode dispatcher"
  repository="$(require_table_value "$file" "Issue repository")"
  [[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] ||
    die "$file has an invalid Issue repository"
  require_table_value "$file" "Privacy" >/dev/null
  grep -Eq '^## Routes[[:space:]]*$' "$file" || die "$file is missing Routes"

  while IFS='|' read -r _ project_key routing_label project_number contract extra; do
    project_key="$(trim "$project_key")"
    routing_label="$(trim "$routing_label")"
    project_number="$(trim "$project_number")"
    contract="$(trim "$contract")"

    [[ -n "$project_key" ]] || continue
    [[ "$project_key" != "Project key" && "$project_key" != "---" ]] || continue
    [[ -n "$routing_label" && -n "$contract" ]] || die "$file has an incomplete route"
    [[ "$project_number" =~ ^[1-9][0-9]*$ ]] || die "$file has an invalid route Project number"
    [[ "$contract" == .projects/projects/*.md && "$contract" != *".."* ]] ||
      die "$file route contract must be under .projects/projects/"

    [[ "$keys" != *$'\n'"$project_key"$'\n'* ]] || die "$file has a duplicate Project key"
    [[ "$labels" != *$'\n'"$routing_label"$'\n'* ]] || die "$file has a duplicate routing label"
    [[ "$numbers" != *$'\n'"$project_number"$'\n'* ]] || die "$file has a duplicate Project number"
    keys+="$project_key"$'\n'
    labels+="$routing_label"$'\n'
    numbers+="$project_number"$'\n'

    leaf="$root/$contract"
    validate_project_file "$leaf" project
    leaf_key="$(require_table_value "$leaf" "Project key")"
    leaf_label="$(require_table_value "$leaf" "Routing")"
    leaf_number="$(require_table_value "$leaf" "Project number")"
    leaf_repository="$(require_table_value "$leaf" "Issue repository")"
    [[ "$leaf_key" == "$project_key" ]] || die "$file route key disagrees with $contract"
    [[ "$leaf_label" == "label:$routing_label" ]] || die "$file route label disagrees with $contract"
    [[ "$leaf_number" == "$project_number" ]] || die "$file route number disagrees with $contract"
    [[ "$leaf_repository" == "$repository" ]] || die "$file issue repository disagrees with $contract"
    ((route_count += 1))
  done < <(awk '
    /^## Routes[[:space:]]*$/ { in_routes = 1; next }
    in_routes && /^## / { exit }
    in_routes && /^\|/ { print }
  ' "$file")

  dispatcher_route_count="$route_count"
}

repository_root="${1:-.}"
dispatcher_route_count=""
[[ -d "$repository_root" ]] || die "repository root is not a directory"
main_contract="$repository_root/.projects/project.md"
[[ -f "$main_contract" ]] || die "missing $main_contract"

main_mode="$(require_table_value "$main_contract" "Mode")"
case "$main_mode" in
  single)
    validate_project_file "$main_contract" single
    ;;
  dispatcher)
    validate_dispatcher "$main_contract" "$repository_root"
    ;;
  *)
    die "$main_contract Mode must be single or dispatcher"
    ;;
esac

if [[ "$main_mode" == "dispatcher" && "$dispatcher_route_count" == "0" ]]; then
  echo "Valid empty GitHub Project dispatcher: $main_contract (no routes configured)"
else
  echo "Valid GitHub Project contract: $main_contract"
fi

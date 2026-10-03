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

temp_files=()

cleanup_temp_files() {
  local file
  for file in ${temp_files[@]+"${temp_files[@]}"}; do
    rm -f -- "$file"
  done
}
trap cleanup_temp_files EXIT

# Fence-stripped copy: every line inside a fenced code block (including the
# fence lines) becomes an empty line, so line numbers are preserved.
fence_stripped_copy() {
  local source="$1"
  stripped_copy="$(mktemp)"
  temp_files+=("$stripped_copy")
  awk '
    function fence_line(line,   s, c, n, rest) {
      s = line
      sub(/^[[:space:]]*/, "", s)
      c = substr(s, 1, 1)
      if (c != "`" && c != "~") return 0
      n = 0
      while (substr(s, n + 1, 1) == c) n++
      if (n < 3) return 0
      rest = substr(s, n + 1)
      if (c == "`" && index(rest, "`") > 0) return 0
      fi_char = c
      fi_len = n
      fi_rest = rest
      return 1
    }
    {
      if (in_fence) {
        print ""
        if (fence_line($0) && fi_char == open_char && fi_len >= open_len && fi_rest ~ /^[[:space:]]*$/) {
          in_fence = 0
        }
        next
      }
      if (fence_line($0)) {
        in_fence = 1
        open_char = fi_char
        open_len = fi_len
        print ""
        next
      }
      print $0
    }
  ' "$source" >"$stripped_copy"
}

table_value() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) { sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); return value }
    /^\|/ { key=trim($2); if (key==wanted) { print trim($3); exit } }
  ' "$file"
}

require_table_value() {
  local file="$1" key="$2" display="${3:-$1}" value
  value="$(table_value "$file" "$key")"
  [[ -n "$value" ]] || die "$display is missing table value: $key"
  printf '%s' "$value"
}

priority_value() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) { sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); return value }
    /^## Priority mapping[[:space:]]*$/ { in_mapping=1; next }
    in_mapping && /^## / { exit }
    in_mapping && /^\|/ { key=trim($2); if (key==wanted) print trim($3) }
  ' "$file"
}

priority_mapping_count() {
  local file="$1" wanted="$2"
  awk -F'|' -v wanted="$wanted" '
    function trim(value) { sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); return value }
    /^## Priority mapping[[:space:]]*$/ { in_mapping=1; next }
    in_mapping && /^## / { exit }
    in_mapping && /^\|/ { if (trim($2)==wanted) count++ }
    END { print count+0 }
  ' "$file"
}

priority_pending_count() {
  local file="$1"
  awk '
    /^## Priority mapping[[:space:]]*$/ { in_mapping=1; next }
    in_mapping && /^## / { exit }
    in_mapping && $0=="Priority mapping status: pending" { count++ }
    END { print count+0 }
  ' "$file"
}

validate_priority_mapping() {
  local file="$1" display="${2:-$1}" common provider providers="" count duplicate pending_count
  if ! grep -Eq '^## Priority mapping[[:space:]]*$' "$file"; then
    return 0
  fi

  pending_count="$(priority_pending_count "$file")"
  if [[ "$pending_count" != "0" ]]; then
    [[ "$pending_count" == "1" ]] || die "$display must declare the pending Priority status exactly once"
    for common in P0 P1 P2 P3; do
      provider="$(priority_value "$file" "$common")"
      [[ -z "$provider" ]] || die "$display mixes a pending Priority status with a $common mapping"
    done
    return 0
  fi

  for common in P0 P1 P2 P3; do
    provider="$(priority_value "$file" "$common")"
    [[ -n "$provider" ]] || die "$display is missing a non-empty $common mapping"
    count="$(priority_mapping_count "$file" "$common")"
    [[ "$count" == "1" ]] || die "$display must map $common exactly once"
    providers+="$provider"$'\n'
  done
  duplicate="$(printf '%s' "$providers" | sed '/^$/d' | sort | uniq -d | head -n 1)"
  [[ -z "$duplicate" ]] || die "$display Priority mapping is not one-to-one"
}

validate_colour_tables() {
  local file="$1" display="${2:-$1}" option colour
  while IFS=$'\t' read -r option colour; do
    [[ -n "$option" ]] || die "$display has an empty option in a colour table"
    case "$colour" in
      BLUE|GRAY|GREEN|ORANGE|PINK|PURPLE|RED|YELLOW) ;;
      *) die "$display has unsupported colour '$colour' for option '$option'" ;;
    esac
  done < <(awk -F'|' '
    function trim(value) { sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); return value }
    /^## / { in_colour_table=0 }
    /^\|/ {
      option=trim($2); colour=trim($3)
      if (option=="Option" && colour=="Colour") { in_colour_table=1; next }
      if (in_colour_table && option!="---" && colour!="---") print option "\t" colour
      next
    }
    in_colour_table && /[^[:space:]]/ { in_colour_table=0 }
  ' "$file")
}

validate_styles() {
  local file="$1" display="${2:-$1}" value
  value="$(table_value "$file" "Issue write-up style")"
  case "$value" in
    ""|direct|tidy|unrestricted) ;;
    *) die "$display has unsupported Issue write-up style '$value'; use direct, tidy or unrestricted" ;;
  esac
  value="$(table_value "$file" "Issue prose style")"
  case "$value" in
    ""|natural-direct) ;;
    *) die "$display has unsupported Issue prose style '$value'; use natural-direct" ;;
  esac
}

validate_project_file() {
  local file="$1" expected_mode="$2" content version mode repository owner owner_type number title
  [[ -f "$file" ]] || die "missing Project contract: $file"
  fence_stripped_copy "$file"
  content="$stripped_copy"

  version="$(require_table_value "$content" "Contract version" "$file")"
  [[ "$version" == "1" ]] || die "$file has unsupported Contract version"
  mode="$(require_table_value "$content" "Mode" "$file")"
  [[ "$mode" == "$expected_mode" ]] || die "$file must use Mode $expected_mode"
  if [[ "$expected_mode" == "project" ]]; then require_table_value "$content" "Project key" "$file" >/dev/null; fi

  repository="$(require_table_value "$content" "Issue repository" "$file")"
  [[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "$file has an invalid Issue repository"
  owner="$(require_table_value "$content" "Project owner" "$file")"
  [[ "$owner" =~ ^[A-Za-z0-9_.-]+$ ]] || die "$file has an invalid Project owner"
  owner_type="$(table_value "$content" "Owner type")"
  [[ -z "$owner_type" || "$owner_type" == "user" || "$owner_type" == "organization" ]] || die "$file Owner type must be user or organization when supplied"
  number="$(require_table_value "$content" "Project number" "$file")"
  [[ "$number" =~ ^[1-9][0-9]*$ ]] || die "$file has an invalid Project number"
  title="$(require_table_value "$content" "Project title" "$file")"
  [[ -n "$title" ]] || die "$file has an empty Project title"
  require_table_value "$content" "Routing" "$file" >/dev/null
  require_table_value "$content" "Privacy" "$file" >/dev/null

  grep -Eq '^## Field locations[[:space:]]*$' "$content" || die "$file is missing Field locations"
  grep -Eq '^\|[[:space:]]*Priority[[:space:]]*\|' "$content" || die "$file does not declare the Priority field location"
  validate_priority_mapping "$content" "$file"
  validate_colour_tables "$content" "$file"
  validate_styles "$content" "$file"

  if grep -Eq '(gh[pousr]_[A-Za-z0-9]{20,}|GH_TOKEN[[:space:]]*=|GITHUB_TOKEN[[:space:]]*=)' "$file"; then
    die "$file appears to contain a credential"
  fi
}

validate_dispatcher() {
  local file="$1" root="$2" content version mode repository route_count=0
  local project_key routing_label project_number contract extra leaf leaf_content
  local leaf_key leaf_label leaf_number leaf_repository
  local keys=$'\n' labels=$'\n' numbers=$'\n'

  fence_stripped_copy "$file"
  content="$stripped_copy"

  version="$(require_table_value "$content" "Contract version" "$file")"
  [[ "$version" == "1" ]] || die "$file has unsupported Contract version"
  mode="$(require_table_value "$content" "Mode" "$file")"
  [[ "$mode" == "dispatcher" ]] || die "$file must use Mode dispatcher"
  repository="$(require_table_value "$content" "Issue repository" "$file")"
  [[ "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "$file has an invalid Issue repository"
  require_table_value "$content" "Privacy" "$file" >/dev/null
  grep -Eq '^## Routes[[:space:]]*$' "$content" || die "$file is missing Routes"

  while IFS='|' read -r _ project_key routing_label project_number contract extra; do
    project_key="$(trim "$project_key")"; routing_label="$(trim "$routing_label")"
    project_number="$(trim "$project_number")"; contract="$(trim "$contract")"
    [[ -n "$project_key" ]] || continue
    [[ "$project_key" != "Project key" && "$project_key" != "---" ]] || continue
    [[ -n "$routing_label" && -n "$contract" ]] || die "$file has an incomplete route"
    [[ "$project_number" =~ ^[1-9][0-9]*$ ]] || die "$file has an invalid route Project number"
    [[ "$contract" == .projects/projects/*.md && "$contract" != *".."* ]] || die "$file route contract must be under .projects/projects/"
    [[ "$keys" != *$'\n'"$project_key"$'\n'* ]] || die "$file has a duplicate Project key"
    [[ "$labels" != *$'\n'"$routing_label"$'\n'* ]] || die "$file has a duplicate routing label"
    [[ "$numbers" != *$'\n'"$project_number"$'\n'* ]] || die "$file has a duplicate Project number"
    keys+="$project_key"$'\n'; labels+="$routing_label"$'\n'; numbers+="$project_number"$'\n'

    leaf="$root/$contract"
    validate_project_file "$leaf" project
    fence_stripped_copy "$leaf"
    leaf_content="$stripped_copy"
    leaf_key="$(require_table_value "$leaf_content" "Project key" "$leaf")"
    leaf_label="$(require_table_value "$leaf_content" "Routing" "$leaf")"
    leaf_number="$(require_table_value "$leaf_content" "Project number" "$leaf")"
    leaf_repository="$(require_table_value "$leaf_content" "Issue repository" "$leaf")"
    [[ "$leaf_key" == "$project_key" ]] || die "$file route key disagrees with $contract"
    [[ "$leaf_label" == "label:$routing_label" ]] || die "$file route label disagrees with $contract"
    [[ "$leaf_number" == "$project_number" ]] || die "$file route number disagrees with $contract"
    [[ "$leaf_repository" == "$repository" ]] || die "$file issue repository disagrees with $contract"
    ((route_count += 1))
  done < <(awk '/^## Routes[[:space:]]*$/ { in_routes=1; next } in_routes && /^## / { exit } in_routes && /^\|/ { print }' "$content")
  dispatcher_route_count="$route_count"
}

repository_root="${1:-.}"
dispatcher_route_count=""
[[ -d "$repository_root" ]] || die "repository root is not a directory"
main_contract="$repository_root/.projects/project.md"
[[ -f "$main_contract" ]] || die "missing $main_contract"
fence_stripped_copy "$main_contract"
main_content="$stripped_copy"
main_mode="$(require_table_value "$main_content" "Mode" "$main_contract")"
case "$main_mode" in
  single) validate_project_file "$main_contract" single ;;
  dispatcher) validate_dispatcher "$main_contract" "$repository_root" ;;
  *) die "$main_contract Mode must be single or dispatcher" ;;
esac
if [[ "$main_mode" == "dispatcher" && "$dispatcher_route_count" == "0" ]]; then
  echo "Valid empty GitHub Project dispatcher: $main_contract (no routes configured)"
else
  echo "Valid GitHub Project contract: $main_contract"
fi

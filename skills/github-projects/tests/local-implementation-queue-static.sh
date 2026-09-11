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
grep -Fq 'must never edit repository files' "$reference"
grep -Fq 'Implementation requests are never queue-executable' "$reference"
grep -Fq 'separate explicit non-queue invocation' "$reference"
grep -Fq 'Chat implementation label | pj:implement-chat' "$contract"

if grep -Fq 'The queue supports two shapes' "$reference"; then
  echo 'ERROR: queue still supports repository implementation items' >&2
  exit 1
fi

if grep -Fq 'Repository implementation work' "$reference"; then
  echo 'ERROR: queue still contains repository implementation execution guidance' >&2
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

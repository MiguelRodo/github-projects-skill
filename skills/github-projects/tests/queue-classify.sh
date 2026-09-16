#!/usr/bin/env bash
set -Eeuo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
classifier="$(cd "$test_dir/../scripts" && pwd)/queue-classify.py"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
contract="$tmp/project.md"
provider="$tmp/gh"

cat >"$contract" <<'EOF'
# Test Project
| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | octo/issues |
| Project owner | octo |
| Project number | 38 |
| Project title | projects |
| Chat implementation label | pj:implement-chat |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | project field | Class |
| Priority | project field | Priority |
| Status | project field | Status |

## Priority mapping

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |

## Class values

| Option | Colour |
| --- | --- |
| Task | YELLOW |
| Enhancement | GREEN |
| Documentation | GRAY |
| Epic | BLUE |

## Status mapping

| Common value | Provider value |
| --- | --- |
| Todo | Todo |
| In progress | In progress |
| Done | Done |

## Governance

- Collaboration mode: collaborative administration.
EOF

cat >"$provider" <<'EOF'
#!/usr/bin/env python3
import json
import os
import sys

scenario = os.environ.get("SCENARIO", "deterministic")
args = sys.argv[1:]

if args[:2] == ["auth", "status"]:
    sys.exit(1 if scenario == "auth_fail" else 0)

if args == ["api", "user"]:
    print(json.dumps({"login": "octocat"}))
    sys.exit(0)

if args[:1] != ["api"]:
    print("unexpected provider call", args, file=sys.stderr)
    sys.exit(90)

endpoint = args[-1]
if endpoint == "repos/octo/issues/issues/42":
    labels = [] if scenario == "missing_label" else [{"name": "pj:implement-chat"}]
    state = "closed" if scenario == "closed" else "open"
    author = "someone-else" if scenario == "other_author" else "octocat"
    print(json.dumps({
        "state": state,
        "labels": labels,
        "user": {"login": author},
        "body": "A deliberately messy human issue body."
    }))
    sys.exit(0)

if endpoint.startswith("repos/octo/issues/issues/42/comments"):
    target_repo = "octo/other" if scenario == "target_mismatch" else "octo/issues"
    project_number = 99 if scenario == "project_mismatch" else 38
    priority = "P9" if scenario == "bad_value" else "P1"
    action = (
        {"kind": "issue.label.add", "name": "example"}
        if scenario == "unsupported_action"
        else {"kind": "dimension.value.set", "dimension": "priority", "value": priority}
    )
    envelope = {
        "apiVersion": "github-projects/queue-authority/v1",
        "kind": "QueueAuthority",
        "spec": {
            "target": {
                "repository": target_repo,
                "issue": 42,
                "project": {"owner": "octo", "number": project_number}
            },
            "shape": "existing_task",
            "actions": [
                {"kind": "project.membership.add"},
                action,
                {
                    "kind": "issue.parent.set",
                    "parent": {"repository": "octo/issues", "issue": 17}
                }
            ],
            "review": {
                "timing": "after",
                "focus": (
                    ["unknown"]
                    if scenario == "invalid_review"
                    else ["hierarchy", "preservation"]
                )
            }
        }
    }
    if scenario == "legacy":
        body = "PJ implementation authority: add this to the Project and set P1."
    elif scenario == "malformed":
        body = "PJ implementation authority:\n" + "```json\n{bad json}\n```"
    elif scenario == "no_authority":
        print("[]")
        sys.exit(0)
    else:
        body = "PJ implementation authority:\n" + "```json\n" + json.dumps(envelope) + "\n```"

    comment = {
        "body": body,
        "user": {"login": "octocat"},
        "created_at": "2026-09-16T12:00:00Z",
        "updated_at": "2026-09-16T12:00:00Z"
    }
    if scenario == "edited":
        comment["updated_at"] = "2026-09-16T12:01:00Z"
    print(json.dumps([[comment]]))
    sys.exit(0)

print("unexpected endpoint", endpoint, file=sys.stderr)
sys.exit(90)
EOF
chmod +x "$provider"

run_case() {
  local scenario="$1"
  SCENARIO="$scenario" PROJECTS_GH_BIN="$provider"     python3 "$classifier" --contract "$contract" --repository octo/issues --issue 42
}

assert_result() {
  local scenario="$1" classification="$2" reason="$3"
  local output
  output="$(run_case "$scenario")"
  python3 - "$output" "$classification" "$reason" <<'PY'
import json
import sys
payload = json.loads(sys.argv[1])
assert payload["classification"] == sys.argv[2], payload
assert payload["reason"] == sys.argv[3], payload
PY
}

assert_result deterministic deterministic queue.ready.structured
output="$(run_case deterministic)"
python3 - "$output" <<'PY'
import json
import sys
payload = json.loads(sys.argv[1])
assert payload["review"]["timing"] == "after"
assert payload["review"]["focus"] == ["hierarchy", "preservation"]
assert len(payload["actions"]) == 3
PY

assert_result legacy needs_agent queue.agent.legacy_authority
assert_result malformed needs_agent queue.agent.envelope_malformed
assert_result no_authority needs_agent queue.agent.structured_authority_missing
assert_result edited needs_agent queue.agent.authority_edited
assert_result unsupported_action needs_agent queue.agent.action_not_deterministic
assert_result invalid_review needs_agent queue.agent.envelope_invalid
assert_result bad_value needs_agent queue.agent.value_not_in_contract
assert_result target_mismatch blocked queue.blocked.target_mismatch
assert_result project_mismatch blocked queue.blocked.project_mismatch
assert_result missing_label blocked queue.blocked.queue_label_missing
assert_result closed blocked queue.blocked.issue_not_open
assert_result auth_fail blocked queue.blocked.authentication

sed 's/Collaboration mode: collaborative administration/Collaboration mode: solo administration/'   "$contract" >"$tmp/solo.md"
contract="$tmp/solo.md"
assert_result other_author needs_agent queue.agent.authority_untrusted

echo "queue classifier tests passed"

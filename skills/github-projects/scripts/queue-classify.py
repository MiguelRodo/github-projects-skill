#!/usr/bin/env python3
"""Classify one bounded pj queue candidate without mutating GitHub."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Any

from queue_common import flatten_pages, gh_json, run, table_value

MARKER = "PJ implementation authority:"
VERSION = "github-projects/queue-authority/v1"
SUPPORTED_ACTIONS = {
    "project.membership.add",
    "dimension.value.set",
    "issue.parent.set",
}
REVIEW_FOCUS = {
    "authority",
    "scope",
    "membership",
    "fields",
    "hierarchy",
    "preservation",
    "completion",
    "receipt",
}


def emit(classification: str, reason: str, **extra: Any) -> None:
    payload = {"classification": classification, "reason": reason, **extra}
    print(json.dumps(payload, separators=(",", ":"), sort_keys=True))


def section_first_column(text: str, heading: str) -> set[str]:
    rows: set[str] = set()
    active = False
    for line in text.splitlines():
        if line.strip() == f"## {heading}":
            active = True
            continue
        if active and line.startswith("## "):
            break
        if not active or not line.startswith("|"):
            continue
        cells = [cell.strip() for cell in line.split("|")[1:-1]]
        if not cells or cells[0] in {"", "---", "Option", "Common value", "Common dimension"}:
            continue
        rows.add(cells[0])
    return rows


def governance(text: str) -> str:
    row = table_value(text, "Governance").lower()
    if row in {"personal", "solo"}:
        return "solo"
    if row in {"collaborative", "shared"}:
        return "collaborative"

    match = re.search(r"Collaboration mode:\s*(solo|personal|collaborative|shared)", text, re.I)
    if match:
        return "solo" if match.group(1).lower() in {"solo", "personal"} else "collaborative"
    if re.search(r"\bpersonal Project\b", text, re.I):
        return "solo"
    return "collaborative"


def structured_payload(body: str) -> tuple[str, Any]:
    if not body.startswith(MARKER):
        return "not_authority", None
    match = re.fullmatch(
        r"PJ implementation authority:\s*\n\x60\x60\x60json\n(.*?)\n\x60\x60\x60\s*",
        body,
        re.S,
    )
    if not match:
        return "legacy", None
    try:
        return "structured", json.loads(match.group(1))
    except json.JSONDecodeError:
        return "malformed", None


def exact_keys(value: Any, required: set[str], optional: set[str] = set()) -> bool:
    return isinstance(value, dict) and required <= value.keys() and value.keys() <= required | optional


def validate_review(value: Any) -> bool:
    if not exact_keys(value, {"timing", "focus"}, {"note"}):
        return False
    if value["timing"] not in {"before", "after"}:
        return False
    focus = value["focus"]
    if (
        not isinstance(focus, list)
        or not focus
        or len(focus) > 8
        or len(set(focus)) != len(focus)
        or any(item not in REVIEW_FOCUS for item in focus)
    ):
        return False
    note = value.get("note")
    return note is None or isinstance(note, str) and 0 < len(note) <= 1024


def validate_envelope(value: Any) -> tuple[str, str]:
    if not exact_keys(value, {"apiVersion", "kind", "spec"}):
        return "needs_agent", "queue.agent.envelope_invalid"
    if value["apiVersion"] != VERSION or value["kind"] != "QueueAuthority":
        return "needs_agent", "queue.agent.version_unsupported"

    spec = value["spec"]
    if not exact_keys(spec, {"target", "shape", "actions"}, {"review"}):
        return "needs_agent", "queue.agent.envelope_invalid"
    if spec["shape"] not in {"existing_task", "temporary_handoff"}:
        return "needs_agent", "queue.agent.envelope_invalid"

    target = spec["target"]
    if not exact_keys(target, {"repository", "issue", "project"}):
        return "needs_agent", "queue.agent.envelope_invalid"
    project = target["project"]
    if (
        not isinstance(target["repository"], str)
        or not isinstance(target["issue"], int)
        or target["issue"] < 1
        or not exact_keys(project, {"owner", "number"})
        or not isinstance(project["owner"], str)
        or not isinstance(project["number"], int)
        or project["number"] < 1
    ):
        return "needs_agent", "queue.agent.envelope_invalid"

    actions = spec["actions"]
    if not isinstance(actions, list) or not 1 <= len(actions) <= 32 or not all(isinstance(a, dict) for a in actions):
        return "needs_agent", "queue.agent.envelope_invalid"
    if "review" in spec and not validate_review(spec["review"]):
        return "needs_agent", "queue.agent.envelope_invalid"
    return "ok", "queue.ready.structured"


def classify_action(
    action: dict[str, Any],
    repository: str,
    issue: int,
    classes: set[str],
    priorities: set[str],
    statuses: set[str],
) -> tuple[str, str]:
    kind = action.get("kind")
    if kind not in SUPPORTED_ACTIONS:
        return "needs_agent", "queue.agent.action_not_deterministic"

    if kind == "project.membership.add":
        return (
            ("ok", "queue.ready.structured")
            if exact_keys(action, {"kind"})
            else ("needs_agent", "queue.agent.envelope_invalid")
        )

    if kind == "dimension.value.set":
        if not exact_keys(action, {"kind", "dimension", "value"}):
            return "needs_agent", "queue.agent.envelope_invalid"
        dimension, value = action["dimension"], action["value"]
        allowed = {"class": classes, "priority": priorities, "status": statuses}.get(dimension)
        if not isinstance(value, str) or allowed is None:
            return "needs_agent", "queue.agent.action_not_deterministic"
        if value not in allowed:
            return "needs_agent", "queue.agent.value_not_in_contract"
        return "ok", "queue.ready.structured"

    if not exact_keys(action, {"kind", "parent"}):
        return "needs_agent", "queue.agent.envelope_invalid"
    parent = action["parent"]
    if (
        not exact_keys(parent, {"repository", "issue"})
        or parent["repository"] != repository
        or not isinstance(parent["issue"], int)
        or parent["issue"] < 1
        or parent["issue"] == issue
    ):
        return "needs_agent", "queue.agent.parent_not_deterministic"
    return "ok", "queue.ready.structured"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--issue", required=True, type=int)
    parser.add_argument("--gh", default=os.environ.get("PROJECTS_GH_BIN", "gh"))
    args = parser.parse_args()

    contract_path = Path(args.contract)
    try:
        contract = contract_path.read_text(encoding="utf-8")
    except OSError as exc:
        emit("blocked", "queue.blocked.contract_unavailable", detail=str(exc))
        return 0

    queue_label = table_value(contract, "Chat implementation label") or "pj:implement-chat"
    project_owner = table_value(contract, "Project owner")
    project_number = table_value(contract, "Project number")
    if not project_owner or not project_number.isdigit():
        emit("blocked", "queue.blocked.contract_invalid")
        return 0

    if run(args.gh, "auth", "status").returncode:
        emit("blocked", "queue.blocked.authentication")
        return 0

    try:
        profile = gh_json(args.gh, "api", "user")
        live = gh_json(args.gh, "api", f"repos/{args.repository}/issues/{args.issue}")
        pages = gh_json(
            args.gh,
            "api",
            "--paginate",
            "--slurp",
            f"repos/{args.repository}/issues/{args.issue}/comments?per_page=100",
        )
    except (RuntimeError, json.JSONDecodeError) as exc:
        emit("blocked", "queue.blocked.provider_read", detail=str(exc))
        return 0

    login = profile.get("login") if isinstance(profile, dict) else None
    if not isinstance(login, str) or not login:
        emit("blocked", "queue.blocked.authentication")
        return 0
    if live.get("state") != "open":
        emit("blocked", "queue.blocked.issue_not_open")
        return 0

    labels = {
        label.get("name")
        for label in live.get("labels", [])
        if isinstance(label, dict) and isinstance(label.get("name"), str)
    }
    if queue_label not in labels:
        emit("blocked", "queue.blocked.queue_label_missing")
        return 0

    comments = flatten_pages(pages)
    marker_comments = [
        comment
        for comment in comments
        if isinstance(comment.get("body"), str)
        and comment["body"].startswith(MARKER)
        and isinstance(comment.get("user"), dict)
        and comment["user"].get("login") == login
    ]
    marker_comments.sort(key=lambda c: c.get("created_at") or "")
    latest = marker_comments[-1] if marker_comments else None

    mode = governance(contract)
    issue_author = (live.get("user") or {}).get("login")
    if mode == "solo" and issue_author != login:
        emit("needs_agent", "queue.agent.authority_untrusted")
        return 0
    if latest is None:
        emit("needs_agent", "queue.agent.structured_authority_missing")
        return 0
    if latest.get("created_at") != latest.get("updated_at"):
        emit("needs_agent", "queue.agent.authority_edited")
        return 0

    payload_kind, envelope = structured_payload(latest["body"])
    if payload_kind == "legacy":
        emit("needs_agent", "queue.agent.legacy_authority")
        return 0
    if payload_kind == "malformed":
        emit("needs_agent", "queue.agent.envelope_malformed")
        return 0
    if payload_kind != "structured":
        emit("needs_agent", "queue.agent.structured_authority_missing")
        return 0

    outcome, reason = validate_envelope(envelope)
    if outcome != "ok":
        emit(outcome, reason)
        return 0

    spec = envelope["spec"]
    target = spec["target"]
    project = target["project"]
    if target["repository"].lower() != args.repository.lower() or target["issue"] != args.issue:
        emit("blocked", "queue.blocked.target_mismatch")
        return 0
    if project["owner"].lower() != project_owner.lower() or project["number"] != int(project_number):
        emit("blocked", "queue.blocked.project_mismatch")
        return 0

    classes = section_first_column(contract, "Class values")
    priorities = section_first_column(contract, "Priority mapping")
    statuses = section_first_column(contract, "Status mapping")
    for action in spec["actions"]:
        outcome, reason = classify_action(
            action, args.repository, args.issue, classes, priorities, statuses
        )
        if outcome != "ok":
            emit(outcome, reason)
            return 0

    emit(
        "deterministic",
        "queue.ready.structured",
        repository=args.repository,
        issue=args.issue,
        shape=spec["shape"],
        actions=spec["actions"],
        review=spec.get("review"),
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

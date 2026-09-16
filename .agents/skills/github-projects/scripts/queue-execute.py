#!/usr/bin/env python3
"""Execute one classifier-approved pj queue item and emit a verified receipt."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any


def run(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, check=False)


def parse_json(proc: subprocess.CompletedProcess[str]) -> Any:
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip() or proc.stdout.strip() or "command failed")
    return json.loads(proc.stdout)


def gh_json(gh: str, *args: str) -> Any:
    return parse_json(run(gh, *args))


def table_value(text: str, wanted: str) -> str:
    for line in text.splitlines():
        if not line.startswith("|"):
            continue
        cells = [cell.strip() for cell in line.split("|")[1:-1]]
        if len(cells) >= 2 and cells[0] == wanted:
            return cells[1]
    return ""


def field_locations(text: str) -> dict[str, tuple[str, str]]:
    result: dict[str, tuple[str, str]] = {}
    active = False
    for line in text.splitlines():
        if line.strip() == "## Field locations":
            active = True
            continue
        if active and line.startswith("## "):
            break
        if not active or not line.startswith("|"):
            continue
        cells = [cell.strip() for cell in line.split("|")[1:-1]]
        if len(cells) >= 3 and cells[0] not in {"", "---", "Common dimension"}:
            result[cells[0].lower().replace(" ", "_")] = (cells[1], cells[2])
    return result


def flatten_pages(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        return []
    if value and all(isinstance(page, list) for page in value):
        return [item for page in value for item in page if isinstance(item, dict)]
    return [item for item in value if isinstance(item, dict)]


def command_json(command: list[str]) -> tuple[Any | None, str | None]:
    proc = run(*command)
    if proc.returncode:
        return None, proc.stderr.strip() or proc.stdout.strip() or "command failed"
    try:
        return json.loads(proc.stdout), None
    except json.JSONDecodeError:
        return None, "command returned invalid JSON"


def op(kind: str, status: str, **extra: Any) -> dict[str, Any]:
    return {"kind": kind, "status": status, **extra}


def receipt(
    classification: dict[str, Any],
    status: str,
    operations: list[dict[str, Any]],
    completion: dict[str, Any] | None = None,
    reason: str | None = None,
    preservation: dict[str, Any] | None = None,
) -> dict[str, Any]:
    value: dict[str, Any] = {
        "status": status,
        "target": {
            "repository": classification.get("repository"),
            "issue": classification.get("issue"),
        },
        "classification": {
            "classification": classification.get("classification"),
            "reason": classification.get("reason"),
        },
        "planned": classification.get("actions", []),
        "operations": operations,
        "review": classification.get("review"),
    }
    if completion is not None:
        value["completion"] = completion
    if preservation is not None:
        value["preservation"] = preservation
    if reason is not None:
        value["reason"] = reason
    return value


def emit(value: dict[str, Any]) -> int:
    print(json.dumps(value, separators=(",", ":"), sort_keys=True))
    return 0


def issue_snapshot(issue: dict[str, Any]) -> dict[str, Any]:
    milestone = issue.get("milestone")
    return {
        "title": issue.get("title"),
        "body": issue.get("body"),
        "state": issue.get("state"),
        "labels": sorted(
            item.get("name")
            for item in issue.get("labels", [])
            if isinstance(item, dict) and isinstance(item.get("name"), str)
        ),
        "assignees": sorted(
            item.get("login")
            for item in issue.get("assignees", [])
            if isinstance(item, dict) and isinstance(item.get("login"), str)
        ),
        "milestone": milestone.get("number") if isinstance(milestone, dict) else None,
    }


def project_command(
    projects: str,
    subcommand: str,
    root: str,
    repository: str,
    project_number: str,
) -> list[str]:
    return [
        projects,
        "project",
        subcommand,
        "--root",
        root,
        "--repo",
        repository,
        "--project-number",
        project_number,
    ]


def set_parent(gh: str, repository: str, issue: int, parent: int) -> tuple[dict[str, Any], str | None]:
    try:
        child = gh_json(gh, "api", f"repos/{repository}/issues/{issue}")
        gh_json(gh, "api", f"repos/{repository}/issues/{parent}")
        before = flatten_pages(
            gh_json(
                gh,
                "api",
                "--paginate",
                "--slurp",
                f"repos/{repository}/issues/{parent}/sub_issues?per_page=100",
            )
        )
    except (RuntimeError, json.JSONDecodeError) as exc:
        return op("issue.parent.set", "read_failed", error=str(exc)), str(exc)

    child_id = child.get("id")
    if not isinstance(child_id, int):
        return op("issue.parent.set", "read_failed", error="child REST database ID missing"), "child REST database ID missing"
    if any(item.get("id") == child_id for item in before):
        return op(
            "issue.parent.set",
            "no_change",
            parent=parent,
            childId=child_id,
            preservation={"existingSubIssues": "verified"},
        ), None

    proc = run(
        gh,
        "api",
        "--method",
        "POST",
        "-H",
        "Accept: application/vnd.github+json",
        "-H",
        "X-GitHub-Api-Version: 2026-03-10",
        f"repos/{repository}/issues/{parent}/sub_issues",
        "-F",
        f"sub_issue_id={child_id}",
        "-F",
        "replace_parent=true",
    )
    if proc.returncode:
        error = proc.stderr.strip() or proc.stdout.strip() or "parent mutation failed"
        return op("issue.parent.set", "mutation_failed", error=error), error

    try:
        after = flatten_pages(
            gh_json(
                gh,
                "api",
                "--paginate",
                "--slurp",
                f"repos/{repository}/issues/{parent}/sub_issues?per_page=100",
            )
        )
    except (RuntimeError, json.JSONDecodeError) as exc:
        return op("issue.parent.set", "verification_failed", error=str(exc)), str(exc)
    after_ids = {item.get("id") for item in after}
    before_ids = {item.get("id") for item in before}
    if child_id not in after_ids:
        return op("issue.parent.set", "verification_failed", error="parent readback mismatch"), "parent readback mismatch"
    if not before_ids <= after_ids:
        return op("issue.parent.set", "verification_failed", error="existing sub-issue relationship changed"), "existing sub-issue relationship changed"
    return op(
        "issue.parent.set",
        "applied_verified",
        parent=parent,
        childId=child_id,
        preservation={"existingSubIssues": "verified"},
    ), None


def ensure_comment(gh: str, repository: str, issue: int, body: str) -> tuple[dict[str, Any], str | None]:
    try:
        comments = flatten_pages(
            gh_json(
                gh,
                "api",
                "--paginate",
                "--slurp",
                f"repos/{repository}/issues/{issue}/comments?per_page=100",
            )
        )
    except (RuntimeError, json.JSONDecodeError) as exc:
        return {"status": "read_failed", "error": str(exc)}, str(exc)

    for comment in comments:
        if comment.get("body") == body:
            return {"status": "no_change", "commentId": comment.get("id")}, None

    proc = run(
        gh,
        "api",
        "--method",
        "POST",
        f"repos/{repository}/issues/{issue}/comments",
        "-f",
        f"body={body}",
    )
    try:
        created = parse_json(proc)
        comment_id = created["id"]
        readback = gh_json(gh, "api", f"repos/{repository}/issues/comments/{comment_id}")
    except (RuntimeError, json.JSONDecodeError, KeyError, TypeError) as exc:
        return {"status": "verification_failed", "error": str(exc)}, str(exc)
    if readback.get("body") != body:
        return {"status": "verification_failed", "error": "completion comment readback mismatch"}, "completion comment readback mismatch"
    return {"status": "applied_verified", "commentId": comment_id}, None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", required=True)
    parser.add_argument("--root", required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--issue", required=True, type=int)
    parser.add_argument("--gh", default=os.environ.get("PROJECTS_GH_BIN", "gh"))
    parser.add_argument("--projects", default=os.environ.get("PROJECTS_BIN", "projects"))
    args = parser.parse_args()

    classifier_path = Path(__file__).with_name("queue-classify.py")
    classified_proc = run(
        sys.executable,
        str(classifier_path),
        "--contract",
        args.contract,
        "--repository",
        args.repository,
        "--issue",
        str(args.issue),
        "--gh",
        args.gh,
    )
    try:
        classified = parse_json(classified_proc)
    except (RuntimeError, json.JSONDecodeError) as exc:
        return emit({
            "status": "blocked",
            "reason": "queue.execute.classifier_failed",
            "error": str(exc),
            "operations": [],
        })

    if classified.get("classification") != "deterministic":
        return emit(receipt(
            classified,
            classified.get("classification", "blocked"),
            [],
            reason=classified.get("reason"),
        ))

    review = classified.get("review")
    if isinstance(review, dict) and review.get("timing") == "before":
        return emit(receipt(
            classified,
            "review_required",
            [],
            reason="queue.execute.before_review_required",
        ))

    if shutil.which(args.projects) is None:
        return emit(receipt(
            classified,
            "needs_agent",
            [],
            reason="queue.execute.projects_unavailable",
        ))

    try:
        contract = Path(args.contract).read_text(encoding="utf-8")
    except OSError as exc:
        value = receipt(classified, "blocked", [], reason="queue.execute.contract_unavailable")
        value["error"] = str(exc)
        return emit(value)

    project_number = table_value(contract, "Project number")
    queue_label = table_value(contract, "Chat implementation label") or "pj:implement-chat"
    locations = field_locations(contract)
    if not project_number.isdigit():
        return emit(receipt(classified, "blocked", [], reason="queue.execute.contract_invalid"))

    try:
        baseline_issue = gh_json(args.gh, "api", f"repos/{args.repository}/issues/{args.issue}")
    except (RuntimeError, json.JSONDecodeError) as exc:
        value = receipt(classified, "blocked", [], reason="queue.execute.baseline_read_failed")
        value["error"] = str(exc)
        return emit(value)
    baseline_labels = {
        item.get("name")
        for item in baseline_issue.get("labels", [])
        if isinstance(item, dict) and isinstance(item.get("name"), str)
    }
    if baseline_issue.get("state") != "open" or queue_label not in baseline_labels:
        return emit(receipt(
            classified,
            "blocked",
            [],
            reason="queue.execute.baseline_state_changed",
        ))
    baseline_snapshot = issue_snapshot(baseline_issue)

    actions = classified["actions"]
    operations: list[dict[str, Any]] = []
    if any(action["kind"] == "project.membership.add" for action in actions):
        result, error = command_json(
            project_command(
                args.projects, "item-add", args.root, args.repository, project_number
            )
            + [
                "--issue",
                str(args.issue),
                "--apply",
                "--json",
                "--quiet",
            ]
        )
        if error:
            operations.append(op("project.membership.add", "mutation_failed", error=error))
            return emit(receipt(classified, "partial_failure", operations, reason="queue.execute.membership_failed"))
        operations.append(op(
            "project.membership.add",
            "applied_verified" if result.get("applied") else "no_change",
            evidence=result,
        ))

    dimensions = [a for a in actions if a["kind"] == "dimension.value.set"]
    if dimensions:
        flags: list[str] = []
        for action in dimensions:
            dimension = action["dimension"]
            location = locations.get(dimension)
            if location is None or location[0] != "project field":
                operations.append(op(
                    "dimension.value.set",
                    "not_attempted",
                    dimension=dimension,
                    reason="field binding is not a Project field",
                ))
                return emit(receipt(
                    classified,
                    "needs_agent",
                    operations,
                    reason="queue.execute.field_binding_not_deterministic",
                ))
            flags.extend([f"--{dimension}", action["value"]])

        plan, error = command_json(
            project_command(
                args.projects, "item-edit", args.root, args.repository, project_number
            )
            + [
                "--issue",
                str(args.issue),
                *flags,
                "--json",
                "--quiet",
            ]
        )
        if error:
            operations.append(op("dimension.value.set", "read_failed", error=error))
            return emit(receipt(classified, "partial_failure", operations, reason="queue.execute.field_plan_failed"))

        current = (plan.get("current") or {}).get("fields") or {}
        delta = plan.get("delta") or {}
        differs = False
        for action in dimensions:
            dimension = action["dimension"]
            field_name = locations[dimension][1]
            if current.get(field_name) != delta.get(dimension):
                differs = True
                break

        if not differs:
            operations.append(op("dimension.value.set", "no_change", actions=dimensions, evidence=plan))
        else:
            result, error = command_json(
                project_command(
                    args.projects, "item-edit", args.root, args.repository, project_number
                )
                + [
                    "--issue",
                    str(args.issue),
                    *flags,
                    "--apply",
                    "--json",
                    "--quiet",
                ]
            )
            if error:
                operations.append(op("dimension.value.set", "mutation_failed", actions=dimensions, error=error))
                return emit(receipt(classified, "partial_failure", operations, reason="queue.execute.field_mutation_failed"))
            operations.append(op("dimension.value.set", "applied_verified", actions=dimensions, evidence=result))

    for action in (a for a in actions if a["kind"] == "issue.parent.set"):
        parent_number = action["parent"]["issue"]
        result, error = set_parent(args.gh, args.repository, args.issue, parent_number)
        operations.append(result)
        if error:
            return emit(receipt(classified, "partial_failure", operations, reason="queue.execute.parent_failed"))

    try:
        fresh_issue = gh_json(args.gh, "api", f"repos/{args.repository}/issues/{args.issue}")
    except (RuntimeError, json.JSONDecodeError) as exc:
        value = receipt(
            classified,
            "partial_failure",
            operations,
            reason="queue.execute.completion_read_failed",
        )
        value["error"] = str(exc)
        return emit(value)
    if issue_snapshot(fresh_issue) != baseline_snapshot:
        return emit(receipt(
            classified,
            "partial_failure",
            operations,
            reason="queue.execute.preservation_failed",
            preservation={"issueState": "mismatch"},
        ))
    labels = {
        item.get("name")
        for item in fresh_issue.get("labels", [])
        if isinstance(item, dict) and isinstance(item.get("name"), str)
    }
    if fresh_issue.get("state") != "open" or queue_label not in labels:
        return emit(receipt(
            classified,
            "partial_failure",
            operations,
            reason="queue.execute.completion_state_changed",
        ))

    summary = (
        f"PJ deterministic administration: verified {len(operations)} operation group(s); "
        "queue completion verified separately."
    )
    comment, error = ensure_comment(args.gh, args.repository, args.issue, summary)
    if error:
        return emit(receipt(
            classified,
            "partial_failure",
            operations,
            completion={"comment": comment},
            reason="queue.execute.comment_failed",
        ))

    completion_command = [
        args.projects,
        "issue",
        "edit",
        "--root",
        args.root,
        "--repo",
        args.repository,
        "--issue",
        str(args.issue),
        "--remove-label",
        queue_label,
    ]
    if classified.get("shape") == "temporary_handoff":
        completion_command += ["--state", "closed", "--close-reason", "completed"]
    completion_result, error = command_json(
        completion_command + ["--apply", "--json", "--quiet"]
    )
    if error:
        return emit(receipt(
            classified,
            "partial_failure",
            operations,
            completion={"comment": comment, "queue": {"status": "mutation_failed", "error": error}},
            reason="queue.execute.completion_failed",
            preservation={
                "issueState": "verified",
                "projectScalarFields": "verified_by_projects_cli" if dimensions else "not_applicable",
            },
        ))

    return emit(receipt(
        classified,
        "applied_verified",
        operations,
        completion={
            "comment": comment,
            "queue": {
                "status": "applied_verified",
                "evidence": completion_result,
                "preservation": "verified_by_projects_cli",
            },
        },
        preservation={
            "issueState": "verified",
            "projectScalarFields": "verified_by_projects_cli" if dimensions else "not_applicable",
        },
    ))


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Execute one classifier-approved pj queue item and emit a verified receipt."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
from pathlib import Path
from typing import Any

from queue_agent import build_agent_context
from queue_common import flatten_pages, gh_json, json_command, run, table_value
from queue_review import build_review_context, validate_review_result


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


def command_json(command: list[str]) -> tuple[Any | None, str | None]:
    try:
        return json_command(*command), None
    except (RuntimeError, json.JSONDecodeError) as exc:
        return None, str(exc)


def op(kind: str, status: str, **extra: Any) -> dict[str, Any]:
    return {"kind": kind, "status": status, **extra}


def receipt(
    classification: dict[str, Any],
    status: str,
    operations: list[dict[str, Any]],
    completion: dict[str, Any] | None = None,
    reason: str | None = None,
    preservation: dict[str, Any] | None = None,
    remaining: list[dict[str, Any]] | None = None,
    agent_context: dict[str, Any] | None = None,
    review_context: dict[str, Any] | None = None,
) -> dict[str, Any]:
    planned = classification.get("actions", [])
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
        "planned": planned,
        "operations": operations,
        "remaining": remaining if remaining is not None else ([] if status == "applied_verified" else planned),
        "review": classification.get("review"),
    }
    if completion is not None:
        value["completion"] = completion
    if preservation is not None:
        value["preservation"] = preservation
    if agent_context is not None:
        value["agentContext"] = agent_context
    if review_context is not None:
        value["reviewContext"] = review_context
    if reason is not None:
        value["reason"] = reason
    return value


def emit(value: dict[str, Any]) -> int:
    print(json.dumps(value, separators=(",", ":"), sort_keys=True))
    return 0


def agent_fallback_receipt(
    classification: dict[str, Any],
    gh: str,
    contract: str,
    root: str,
    repository: str,
    issue: int,
    reason: str | None = None,
    operations: list[dict[str, Any]] | None = None,
    remaining: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    operations = operations or []
    fallback_reason = reason or classification.get("reason")
    decision = {
        **classification,
        "classification": "needs_agent",
        "reason": fallback_reason,
    }
    try:
        context = build_agent_context(
            gh, contract, root, repository, issue, decision
        )
    except (OSError, RuntimeError, json.JSONDecodeError) as exc:
        value = receipt(
            classification,
            "blocked",
            operations,
            reason="queue.execute.agent_context_failed",
            remaining=remaining,
        )
        value["error"] = str(exc)
        return value

    return receipt(
        classification,
        "needs_agent",
        operations,
        reason=fallback_reason,
        remaining=remaining,
        agent_context=context,
    )


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


def completion_guard(
    gh: str,
    repository: str,
    issue: int,
    queue_label: str,
    baseline: dict[str, Any],
) -> tuple[str | None, str | None]:
    try:
        fresh = gh_json(gh, "api", f"repos/{repository}/issues/{issue}")
    except (RuntimeError, json.JSONDecodeError) as exc:
        return "queue.execute.completion_read_failed", str(exc)

    labels = {
        item.get("name")
        for item in fresh.get("labels", [])
        if isinstance(item, dict) and isinstance(item.get("name"), str)
    }
    if fresh.get("state") != "open" or queue_label not in labels:
        return "queue.execute.completion_state_changed", None
    if issue_snapshot(fresh) != baseline:
        return "queue.execute.preservation_failed", None
    return None, None


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

    try:
        created = json_command(
            gh,
            "api",
            "--method",
            "POST",
            f"repos/{repository}/issues/{issue}/comments",
            "-f",
            f"body={body}",
        )
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
    parser.add_argument("--review-result", help="JSON review approval for a prior reviewContext")
    args = parser.parse_args()

    classifier_path = Path(__file__).with_name("queue-classify.py")
    try:
        classified = json_command(
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
    except (RuntimeError, json.JSONDecodeError) as exc:
        classified = {
            "classification": "blocked",
            "reason": "queue.execute.classifier_failed",
            "repository": args.repository,
            "issue": args.issue,
            "actions": [],
            "review": None,
        }
        value = receipt(classified, "blocked", [], reason="queue.execute.classifier_failed")
        value["error"] = str(exc)
        return emit(value)

    classified.setdefault("repository", args.repository)
    classified.setdefault("issue", args.issue)

    if classified.get("classification") != "deterministic":
        if classified.get("classification") == "needs_agent":
            return emit(agent_fallback_receipt(
                classified,
                args.gh,
                args.contract,
                args.root,
                args.repository,
                args.issue,
            ))
        return emit(receipt(
            classified,
            classified.get("classification", "blocked"),
            [],
            reason=classified.get("reason"),
        ))

    review = classified.get("review")
    review_result: Any = None
    review_result_error: str | None = None
    if args.review_result:
        try:
            review_result = json.loads(Path(args.review_result).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            review_result_error = str(exc)

    if isinstance(review, dict) and review.get("timing") == "before":
        approved = False
        if review_result_error is None and review_result is not None:
            try:
                validate_review_result(review_result, classified, "before")
                approved = True
            except ValueError as exc:
                review_result_error = str(exc)
        if not approved:
            try:
                context = build_review_context(
                    args.gh,
                    args.contract,
                    args.root,
                    args.repository,
                    args.issue,
                    classified,
                    "before",
                )
            except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as exc:
                value = receipt(
                    classified,
                    "blocked",
                    [],
                    reason="queue.execute.review_context_failed",
                )
                value["error"] = str(exc)
                return emit(value)
            value = receipt(
                classified,
                "review_required",
                [],
                reason=(
                    "queue.execute.review_result_invalid"
                    if review_result_error is not None
                    else "queue.execute.before_review_required"
                ),
                review_context=context,
            )
            if review_result_error is not None:
                value["error"] = review_result_error
            return emit(value)

    if shutil.which(args.projects) is None:
        return emit(agent_fallback_receipt(
            classified,
            args.gh,
            args.contract,
            args.root,
            args.repository,
            args.issue,
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
    memberships = [a for a in actions if a["kind"] == "project.membership.add"]
    dimensions = [a for a in actions if a["kind"] == "dimension.value.set"]
    parents = [a for a in actions if a["kind"] == "issue.parent.set"]
    pending = list(actions)
    operations: list[dict[str, Any]] = []

    def finish(status: str, reason: str | None = None, **extra: Any) -> int:
        if status == "needs_agent":
            return emit(agent_fallback_receipt(
                classified,
                args.gh,
                args.contract,
                args.root,
                args.repository,
                args.issue,
                reason=reason,
                operations=operations,
                remaining=pending,
            ))
        return emit(receipt(
            classified,
            status,
            operations,
            reason=reason,
            remaining=pending,
            **extra,
        ))

    dimension_names = [action["dimension"] for action in dimensions]
    if (
        len(memberships) > 1
        or len(parents) > 1
        or len(dimension_names) != len(set(dimension_names))
    ):
        return finish("needs_agent", "queue.execute.plan_conflict")

    field_flags: list[str] = []
    for action in dimensions:
        dimension = action["dimension"]
        location = locations.get(dimension)
        if location is None or location[0] != "project field":
            return finish("needs_agent", "queue.execute.field_binding_not_deterministic")
        field_flags.extend([f"--{dimension}", action["value"]])

    if memberships:
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
            return finish("partial_failure", "queue.execute.membership_failed")
        operations.append(op(
            "project.membership.add",
            "applied_verified" if result.get("applied") else "no_change",
            evidence=result,
        ))
        pending.remove(memberships[0])

    if dimensions:
        plan, error = command_json(
            project_command(
                args.projects, "item-edit", args.root, args.repository, project_number
            )
            + [
                "--issue",
                str(args.issue),
                *field_flags,
                "--json",
                "--quiet",
            ]
        )
        if error:
            operations.append(op("dimension.value.set", "read_failed", actions=dimensions, error=error))
            return finish("partial_failure", "queue.execute.field_plan_failed")

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
                    *field_flags,
                    "--apply",
                    "--json",
                    "--quiet",
                ]
            )
            if error:
                operations.append(op("dimension.value.set", "mutation_failed", actions=dimensions, error=error))
                return finish("partial_failure", "queue.execute.field_mutation_failed")
            operations.append(op("dimension.value.set", "applied_verified", actions=dimensions, evidence=result))
        for action in dimensions:
            pending.remove(action)

    for action in parents:
        parent_number = action["parent"]["issue"]
        result, error = set_parent(args.gh, args.repository, args.issue, parent_number)
        operations.append(result)
        if error:
            return finish("partial_failure", "queue.execute.parent_failed")
        pending.remove(action)

    guard_reason, guard_error = completion_guard(
        args.gh, args.repository, args.issue, queue_label, baseline_snapshot
    )
    if guard_reason:
        extra: dict[str, Any] = {}
        if guard_error is not None:
            extra["error"] = guard_error
        if guard_reason == "queue.execute.preservation_failed":
            extra["preservation"] = {"issueState": "mismatch"}
        return finish("partial_failure", guard_reason, **extra)

    verified_preservation = {
        "issueState": "verified",
        "projectScalarFields": (
            "verified_by_projects_cli" if dimensions else "not_applicable"
        ),
    }
    if isinstance(review, dict) and review.get("timing") == "after":
        approved = False
        if review_result_error is None and review_result is not None:
            try:
                validate_review_result(review_result, classified, "after")
                approved = True
            except ValueError as exc:
                review_result_error = str(exc)
        if not approved:
            execution_receipt = receipt(
                classified,
                "applied_verified",
                operations,
                completion={"status": "pending_review"},
                preservation=verified_preservation,
                remaining=[],
            )
            try:
                context = build_review_context(
                    args.gh,
                    args.contract,
                    args.root,
                    args.repository,
                    args.issue,
                    classified,
                    "after",
                    execution_receipt=execution_receipt,
                )
            except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as exc:
                value = receipt(
                    classified,
                    "partial_failure",
                    operations,
                    completion={"status": "pending_review"},
                    preservation=verified_preservation,
                    reason="queue.execute.review_context_failed",
                    remaining=[],
                )
                value["error"] = str(exc)
                return emit(value)
            value = receipt(
                classified,
                "review_required",
                operations,
                completion={"status": "pending_review"},
                preservation=verified_preservation,
                reason=(
                    "queue.execute.review_result_invalid"
                    if review_result_error is not None
                    else "queue.execute.after_review_required"
                ),
                remaining=[],
                review_context=context,
            )
            if review_result_error is not None:
                value["error"] = review_result_error
            return emit(value)

    summary = f"PJ deterministic administration: verified {len(operations)} operation group(s)."
    comment, error = ensure_comment(args.gh, args.repository, args.issue, summary)
    if error:
        return finish(
            "partial_failure",
            "queue.execute.comment_failed",
            completion={"comment": comment},
        )

    guard_reason, guard_error = completion_guard(
        args.gh, args.repository, args.issue, queue_label, baseline_snapshot
    )
    if guard_reason:
        extra = {"completion": {"comment": comment}}
        if guard_error is not None:
            extra["error"] = guard_error
        if guard_reason == "queue.execute.preservation_failed":
            extra["preservation"] = {"issueState": "mismatch"}
        return finish("partial_failure", guard_reason, **extra)

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
        return finish(
            "partial_failure",
            "queue.execute.completion_failed",
            completion={"comment": comment, "queue": {"status": "mutation_failed", "error": error}},
            preservation={
                "issueState": "verified",
                "projectScalarFields": "verified_by_projects_cli" if dimensions else "not_applicable",
            },
        )

    return finish(
        "applied_verified",
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
    )


if __name__ == "__main__":
    raise SystemExit(main())

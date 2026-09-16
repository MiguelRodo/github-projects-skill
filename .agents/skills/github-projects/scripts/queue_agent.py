"""Build bounded read-only context for pj queue agent fallback."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from queue_common import flatten_pages, gh_json, table_value

MARKER = "PJ implementation authority:"
VERSION = "github-projects/queue-agent-context/v1"
MAX_ISSUE_BODY = 65536
MAX_AUTHORITY_BODY = 16384
MAX_AUTHORITY_COMMENTS = 20


def _bounded_text(value: Any, limit: int) -> tuple[str, bool]:
    text = value if isinstance(value, str) else ""
    return text[:limit], len(text) > limit


def _names(items: Any, key: str) -> list[str]:
    if not isinstance(items, list):
        return []
    return sorted(
        item[key]
        for item in items
        if isinstance(item, dict) and isinstance(item.get(key), str)
    )


def build_agent_context(
    gh: str,
    contract_path: str,
    root: str,
    repository: str,
    issue: int,
    decision: dict[str, Any],
) -> dict[str, Any]:
    """Return exact-target context without broad workspace discovery."""
    checked_contract = Path(contract_path).resolve()
    contract = checked_contract.read_text(encoding="utf-8")
    queue_label = table_value(contract, "Chat implementation label") or "pj:implement-chat"
    contract_repository = table_value(contract, "Issue repository")
    if not contract_repository or contract_repository.lower() != repository.lower():
        raise RuntimeError("checked contract repository disagrees with the queue target")
    project_number = table_value(contract, "Project number")
    project_owner = table_value(contract, "Project owner")
    if not project_owner or not project_number.isdigit():
        raise RuntimeError("checked contract has incomplete Project identity")

    profile = gh_json(gh, "api", "user")
    live = gh_json(gh, "api", f"repos/{repository}/issues/{issue}")
    comments = flatten_pages(
        gh_json(
            gh,
            "api",
            "--paginate",
            "--slurp",
            f"repos/{repository}/issues/{issue}/comments?per_page=100",
        )
    )

    login = profile.get("login") if isinstance(profile, dict) else None
    if not isinstance(login, str) or not login:
        raise RuntimeError("authenticated GitHub login is unavailable")

    labels = _names(live.get("labels"), "name")
    if live.get("state") != "open" or queue_label not in labels:
        raise RuntimeError("queue target changed before agent handoff")

    marker_comments = [
        comment
        for comment in comments
        if isinstance(comment.get("body"), str)
        and comment["body"].startswith(MARKER)
    ]
    marker_comments.sort(key=lambda comment: comment.get("created_at") or "")
    authority_comments = []
    for comment in marker_comments[-MAX_AUTHORITY_COMMENTS:]:
        body, truncated = _bounded_text(comment.get("body"), MAX_AUTHORITY_BODY)
        author = (comment.get("user") or {}).get("login")
        authority_comments.append(
            {
                "id": comment.get("id"),
                "author": author,
                "createdAt": comment.get("created_at"),
                "updatedAt": comment.get("updated_at"),
                "unedited": comment.get("created_at") == comment.get("updated_at"),
                "authenticatedAuthor": author == login,
                "body": body,
                "bodyTruncated": truncated,
            }
        )

    issue_body, issue_body_truncated = _bounded_text(live.get("body"), MAX_ISSUE_BODY)
    milestone = live.get("milestone")
    return {
        "apiVersion": VERSION,
        "effectBoundary": "github_issue_project_administration_only",
        "target": {
            "repository": repository,
            "issue": issue,
            "url": live.get("html_url")
            or f"https://github.com/{repository}/issues/{issue}",
        },
        "classification": {
            "classification": decision.get("classification"),
            "reason": decision.get("reason"),
        },
        "workspace": {
            "root": root,
            "contractPath": str(checked_contract),
        },
        "contract": {
            "mode": table_value(contract, "Mode"),
            "issueRepository": table_value(contract, "Issue repository"),
            "queueLabel": queue_label,
            "project": {
                "owner": project_owner,
                "number": int(project_number),
                "title": table_value(contract, "Project title"),
                "key": table_value(contract, "Project key") or None,
            },
        },
        "authenticatedLogin": login,
        "issue": {
            "title": live.get("title"),
            "body": issue_body,
            "bodyTruncated": issue_body_truncated,
            "state": live.get("state"),
            "labels": labels,
            "assignees": _names(live.get("assignees"), "login"),
            "milestone": (
                {
                    "number": milestone.get("number"),
                    "title": milestone.get("title"),
                }
                if isinstance(milestone, dict)
                else None
            ),
            "author": (live.get("user") or {}).get("login"),
        },
        "authorityComments": authority_comments,
        "authorityCommentCount": len(marker_comments),
        "authorityCommentsTruncated": len(marker_comments) > len(authority_comments),
        "otherCommentCount": len(comments) - len(marker_comments),
    }

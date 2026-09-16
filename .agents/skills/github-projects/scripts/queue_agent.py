"""Build bounded read-only context for pj queue agent fallback."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from queue_common import flatten_pages, gh_json, table_value

MARKER = "PJ implementation authority:"
VERSION = "github-projects/queue-agent-context/v1"


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
    contract = Path(contract_path).read_text(encoding="utf-8")
    queue_label = table_value(contract, "Chat implementation label") or "pj:implement-chat"

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

    authority_comments = []
    for comment in comments:
        body = comment.get("body")
        author = (comment.get("user") or {}).get("login")
        if not isinstance(body, str) or not body.startswith(MARKER):
            continue
        authority_comments.append(
            {
                "id": comment.get("id"),
                "author": author,
                "createdAt": comment.get("created_at"),
                "updatedAt": comment.get("updated_at"),
                "unedited": comment.get("created_at") == comment.get("updated_at"),
                "authenticatedAuthor": author == login,
                "body": body,
            }
        )

    project_number = table_value(contract, "Project number")
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
            "contractPath": str(Path(contract_path)),
        },
        "contract": {
            "mode": table_value(contract, "Mode"),
            "issueRepository": table_value(contract, "Issue repository"),
            "queueLabel": queue_label,
            "project": {
                "owner": table_value(contract, "Project owner"),
                "number": int(project_number) if project_number.isdigit() else None,
                "title": table_value(contract, "Project title"),
                "key": table_value(contract, "Project key") or None,
            },
        },
        "authenticatedLogin": login,
        "issue": {
            "title": live.get("title"),
            "body": live.get("body"),
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
        "otherCommentCount": len(comments) - len(authority_comments),
    }

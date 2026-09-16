"""Build bounded review plans and contexts for deterministic pj queue items."""

from __future__ import annotations

from typing import Any

from queue_agent import build_agent_context

VERSION = "github-projects/queue-review-context/v1"
FULL_FOCUS = [
    "authority",
    "scope",
    "membership",
    "fields",
    "hierarchy",
    "preservation",
    "completion",
    "receipt",
]


def resolve_review_plan(
    review: dict[str, Any] | None,
    operator_policy: str = "auto",
) -> dict[str, Any]:
    """Combine mandatory item review with an optional launcher policy."""
    if operator_policy not in {"auto", "before", "after"}:
        raise ValueError(f"unsupported agent policy: {operator_policy}")

    item_timing = review.get("timing") if isinstance(review, dict) else None
    before = item_timing == "before" or operator_policy == "before"
    after = item_timing == "after" or operator_policy == "after"
    return {
        "operatorPolicy": operator_policy,
        "itemRequired": item_timing in {"before", "after"},
        "itemTiming": item_timing,
        "before": before,
        "after": after,
    }


def build_review_context(
    gh: str,
    contract_path: str,
    root: str,
    repository: str,
    issue: int,
    classification: dict[str, Any],
    timing: str,
    *,
    operator_policy: str = "auto",
    execution_receipt: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Return one bounded review packet without granting mutation authority."""
    if timing not in {"before", "after"}:
        raise ValueError(f"unsupported review timing: {timing}")

    review = classification.get("review")
    plan = resolve_review_plan(review if isinstance(review, dict) else None, operator_policy)
    if not plan[timing]:
        raise ValueError(f"{timing} review is not required")

    item_review_here = (
        isinstance(review, dict) and review.get("timing") == timing
    )
    focus = list(review["focus"]) if item_review_here else list(FULL_FOCUS)
    note = review.get("note") if item_review_here else None

    decision = {
        **classification,
        "classification": "deterministic",
        "reason": "queue.review.required",
    }
    context = {
        "apiVersion": VERSION,
        "effectBoundary": "github_issue_project_administration_only",
        "mode": "review_only",
        "timing": timing,
        "focus": focus,
        "note": note,
        "noteMayAuthoriseMutations": False,
        "authorisedActions": classification.get("actions", []),
        "plan": plan,
        "agentContext": build_agent_context(
            gh, contract_path, root, repository, issue, decision
        ),
    }
    if execution_receipt is not None:
        context["executionReceipt"] = execution_receipt
    return context

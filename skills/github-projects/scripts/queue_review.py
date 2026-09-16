"""Build bounded review plans and contexts for deterministic pj queue items."""

from __future__ import annotations

from typing import Any

from queue_agent import build_agent_context

VERSION = "github-projects/queue-review-context/v1"
RESULT_VERSION = "github-projects/queue-review-result/v1"
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




def validate_review_result(
    value: Any,
    classification: dict[str, Any],
    timing: str,
) -> None:
    """Verify a review approval without allowing it to broaden authority."""
    if timing not in {"before", "after"}:
        raise ValueError(f"unsupported review timing: {timing}")
    if not isinstance(value, dict) or set(value) != {"apiVersion", "outcome", "context"}:
        raise ValueError("review result has an invalid shape")
    if value["apiVersion"] != RESULT_VERSION or value["outcome"] != "approved":
        raise ValueError("review result is not an approved v1 result")

    context = value["context"]
    if not isinstance(context, dict):
        raise ValueError("review result context is missing")
    required = {
        "apiVersion",
        "effectBoundary",
        "mode",
        "timing",
        "focus",
        "note",
        "noteMayAuthoriseMutations",
        "authorisedActions",
        "plan",
        "agentContext",
    }
    allowed = required | {"executionReceipt"}
    if not required <= set(context) <= allowed:
        raise ValueError("review context has an invalid shape")
    if (
        context["apiVersion"] != VERSION
        or context["effectBoundary"] != "github_issue_project_administration_only"
        or context["mode"] != "review_only"
        or context["timing"] != timing
        or context["noteMayAuthoriseMutations"] is not False
        or context["authorisedActions"] != classification.get("actions", [])
    ):
        raise ValueError("review result does not match the current authorised plan")

    review = classification.get("review")
    if not isinstance(review, dict) or review.get("timing") != timing:
        raise ValueError("review result does not match the current item review")
    if context["focus"] != review.get("focus") or context["note"] != review.get("note"):
        raise ValueError("review result focus or note does not match current authority")

    plan = context["plan"]
    if (
        not isinstance(plan, dict)
        or plan.get("itemRequired") is not True
        or plan.get("itemTiming") != timing
        or plan.get(timing) is not True
    ):
        raise ValueError("review result policy does not match current authority")

    agent = context["agentContext"]
    target = agent.get("target") if isinstance(agent, dict) else None
    if (
        not isinstance(target, dict)
        or target.get("repository") != classification.get("repository")
        or target.get("issue") != classification.get("issue")
    ):
        raise ValueError("review result target does not match the current item")

    execution = context.get("executionReceipt")
    if timing == "before":
        if execution is not None:
            raise ValueError("before-review result must not contain an execution receipt")
        return

    if (
        not isinstance(execution, dict)
        or execution.get("status") != "applied_verified"
        or execution.get("planned") != classification.get("actions", [])
        or execution.get("remaining") != []
        or execution.get("completion") != {"status": "pending_review"}
    ):
        raise ValueError("after-review result lacks a verified matching execution receipt")


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

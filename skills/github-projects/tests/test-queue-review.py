#!/usr/bin/env python3
"""Offline policy tests for structured queue review routing."""

from __future__ import annotations

import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent.parent / "scripts"
sys.path.insert(0, str(SCRIPT_DIR))

from queue_review import RESULT_VERSION, VERSION, resolve_review_plan, validate_review_result


def main() -> None:
    before = {"timing": "before", "focus": ["authority"]}
    after = {"timing": "after", "focus": ["receipt"]}

    assert resolve_review_plan(None, "auto") == {
        "operatorPolicy": "auto",
        "itemRequired": False,
        "itemTiming": None,
        "before": False,
        "after": False,
    }
    assert resolve_review_plan(before, "auto")["before"] is True
    assert resolve_review_plan(before, "auto")["after"] is False
    assert resolve_review_plan(after, "auto")["after"] is True
    assert resolve_review_plan(after, "auto")["before"] is False

    # An operator can add review, but cannot move or suppress item-required review.
    forced_after = resolve_review_plan(before, "after")
    assert forced_after["before"] is True and forced_after["after"] is True
    forced_before = resolve_review_plan(after, "before")
    assert forced_before["before"] is True and forced_before["after"] is True
    assert resolve_review_plan(None, "before")["before"] is True
    assert resolve_review_plan(None, "after")["after"] is True


    classification = {
        "repository": "octo/issues",
        "issue": 42,
        "actions": [{"kind": "project.membership.add"}],
        "review": before,
    }
    context = {
        "apiVersion": VERSION,
        "effectBoundary": "github_issue_project_administration_only",
        "mode": "review_only",
        "timing": "before",
        "focus": ["authority"],
        "note": None,
        "noteMayAuthoriseMutations": False,
        "authorisedActions": classification["actions"],
        "plan": resolve_review_plan(before, "auto"),
        "agentContext": {
            "target": {"repository": "octo/issues", "issue": 42},
        },
    }
    result = {"apiVersion": RESULT_VERSION, "outcome": "approved", "context": context}
    validate_review_result(result, classification, "before")

    widened = {
        **result,
        "context": {
            **context,
            "authorisedActions": [{"kind": "issue.label.add", "name": "triage"}],
        },
    }
    try:
        validate_review_result(widened, classification, "before")
    except ValueError:
        pass
    else:
        raise AssertionError("review result must not broaden authorised actions")

    try:
        resolve_review_plan(None, "never")
    except ValueError:
        pass
    else:
        raise AssertionError("unknown operator policy must fail closed")

    print("queue review policy tests passed")


if __name__ == "__main__":
    main()

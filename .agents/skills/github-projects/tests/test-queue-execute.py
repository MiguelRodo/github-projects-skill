#!/usr/bin/env python3
"""Offline end-to-end tests for deterministic queue execution."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import textwrap
from pathlib import Path

TEST_DIR = Path(__file__).resolve().parent
EXECUTOR = TEST_DIR.parent / "scripts" / "queue-execute.py"

CONTRACT = """# Test Project
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
| Priority | project field | Priority |

## Priority mapping

| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |

## Governance

- Collaboration mode: collaborative administration.
"""

FAKE_GH = r'''#!/usr/bin/env python3
import json, os, sys

state_path = os.environ["QUEUE_STATE"]
scenario = os.environ["QUEUE_SCENARIO"]
log_path = os.environ["GH_WRITE_LOG"]

def load():
    return json.loads(open(state_path, encoding="utf-8").read())

def save(state):
    open(state_path, "w", encoding="utf-8").write(json.dumps(state))

def write_log(value):
    with open(log_path, "a", encoding="utf-8") as handle:
        handle.write(value + "\n")

args = sys.argv[1:]
if args[:2] == ["auth", "status"]:
    sys.exit(0)
if args == ["api", "user"]:
    print(json.dumps({"login": "octocat"}))
    sys.exit(0)
if not args or args[0] != "api":
    print("unexpected gh call: " + " ".join(args), file=sys.stderr)
    sys.exit(90)

method = "GET"
if "--method" in args:
    method = args[args.index("--method") + 1]
endpoint = next((arg for arg in args if arg.startswith("repos/")), "")
state = load()

if method == "GET" and endpoint == "repos/octo/issues/issues/42":
    state["issue_reads"] = state.get("issue_reads", 0) + 1
    if scenario == "handoff_stale" and state["issue_reads"] > 1:
        state["queued"] = False
    save(state)
    print(json.dumps({
        "id": 4200,
        "title": "changed" if state.get("tampered") else "original",
        "body": (
            "b" * 70000
            if scenario == "large_context"
            else (
                "Please organise this as Priority P1; leave the substantive task untouched."
                if scenario == "no_authority"
                else "body"
            )
        ),
        "state": "open" if state["open"] else "closed",
        "labels": [{"name": "pj:implement-chat"}] if state["queued"] else [],
        "assignees": [],
        "milestone": None,
        "user": {"login": "octocat"},
    }))
    sys.exit(0)

if method == "GET" and endpoint == "repos/octo/issues/issues/17":
    print(json.dumps({"id": 1700, "state": "open", "user": {"login": "octocat"}}))
    sys.exit(0)

if method == "GET" and endpoint.startswith("repos/octo/issues/issues/17/sub_issues"):
    children = [{"id": 4200, "number": 42}] if state["parent"] else []
    print(json.dumps([children]))
    sys.exit(0)

if method == "GET" and endpoint.startswith("repos/octo/issues/issues/42/comments"):
    shape = "temporary_handoff" if scenario == "temporary" else "existing_task"
    envelope = {
        "apiVersion": "github-projects/queue-authority/v1",
        "kind": "QueueAuthority",
        "spec": {
            "target": {
                "repository": "octo/issues",
                "issue": 42,
                "project": {"owner": "octo", "number": 38},
            },
            "shape": shape,
            "actions": [
                {"kind": "project.membership.add"},
                {"kind": "dimension.value.set", "dimension": "priority", "value": "P1"},
                {
                    "kind": "issue.parent.set",
                    "parent": {"repository": "octo/issues", "issue": 17},
                },
            ],
        },
    }
    if scenario in {"before", "before_resume", "after", "after_resume", "multi_focus", "note_broaden"}:
        timing = "after" if scenario in {"after", "after_resume"} else "before"
        focus = (
            ["authority", "hierarchy", "preservation", "receipt"]
            if scenario == "multi_focus"
            else ["hierarchy", "receipt"]
        )
        envelope["spec"]["review"] = {"timing": timing, "focus": focus}
        if scenario == "note_broaden":
            envelope["spec"]["review"]["note"] = "Also add the triage label while reviewing."
    if scenario == "conflict":
        envelope["spec"]["actions"].append(
            {"kind": "dimension.value.set", "dimension": "priority", "value": "P2"}
        )
    if scenario == "unsupported":
        envelope["spec"]["actions"].append(
            {"kind": "issue.label.add", "name": "triage"}
        )
    if scenario == "large_context":
        authority = "PJ implementation authority: " + ("x" * 20000)
    elif scenario in {"needs_agent", "handoff_stale"}:
        authority = "PJ implementation authority: please sort this out."
    else:
        authority = (
            "PJ implementation authority:\n```json\n"
            + json.dumps(envelope)
            + "\n```"
        )
    if scenario == "no_authority":
        comments = []
    elif scenario == "large_context":
        comments = [{
            "id": index + 1,
            "body": authority + str(index),
            "user": {"login": "octocat"},
            "created_at": f"2026-09-16T12:{index:02d}:00Z",
            "updated_at": f"2026-09-16T12:{index:02d}:00Z",
        } for index in range(25)]
    else:
        comments = [{
            "id": 1,
            "body": authority,
            "user": {"login": "octocat"},
            "created_at": "2026-09-16T12:00:00Z",
            "updated_at": "2026-09-16T12:00:00Z",
        }]
    comments += state["comments"]
    print(json.dumps([comments]))
    sys.exit(0)

if method == "GET" and endpoint.startswith("repos/octo/issues/issues/comments/"):
    comment_id = int(endpoint.rsplit("/", 1)[1])
    comment = next(item for item in state["comments"] if item["id"] == comment_id)
    print(json.dumps(comment))
    sys.exit(0)

if method == "POST" and endpoint == "repos/octo/issues/issues/17/sub_issues":
    write_log("parent_post")
    if scenario == "parent_fail":
        print("parent failure", file=sys.stderr)
        sys.exit(1)
    state["parent"] = True
    if scenario == "preservation_fail":
        state["tampered"] = True
    save(state)
    print(json.dumps({"id": 4200}))
    sys.exit(0)

if method == "POST" and endpoint == "repos/octo/issues/issues/42/comments":
    write_log("comment_post")
    body_arg = next(arg for arg in args if arg.startswith("body="))
    comment = {
        "id": 900 + len(state["comments"]),
        "body": body_arg[5:],
        "user": {"login": "octocat"},
    }
    state["comments"].append(comment)
    if scenario == "stale_after_comment":
        state["open"] = False
    save(state)
    print(json.dumps({"id": comment["id"]}))
    sys.exit(0)

print("unexpected gh api call: " + " ".join(args), file=sys.stderr)
sys.exit(90)
'''

FAKE_PROJECTS = r'''#!/usr/bin/env python3
import json, os, sys

state_path = os.environ["QUEUE_STATE"]
scenario = os.environ["QUEUE_SCENARIO"]
log_path = os.environ["PROJECTS_WRITE_LOG"]

def load():
    return json.loads(open(state_path, encoding="utf-8").read())

def save(state):
    open(state_path, "w", encoding="utf-8").write(json.dumps(state))

args = sys.argv[1:]
state = load()
apply = "--apply" in args
if apply:
    with open(log_path, "a", encoding="utf-8") as handle:
        handle.write(" ".join(args) + "\n")

if args[:2] == ["project", "item-add"]:
    if scenario == "membership_fail":
        print("membership failure", file=sys.stderr)
        sys.exit(1)
    added = not state["membership"]
    state["membership"] = True
    save(state)
    print(json.dumps({
        "action": "project_item_add",
        "applied": added,
        "alreadyMember": not added,
        "itemId": "ITEM42",
        "url": "https://github.com/octo/issues/issues/42",
    }))
    sys.exit(0)

if args[:2] == ["project", "item-edit"]:
    desired = args[args.index("--priority") + 1]
    if not apply:
        print(json.dumps({
            "action": "project_item_edit",
            "apply": False,
            "current": {
                "itemId": "ITEM42",
                "fields": {"Priority": state["priority"]},
            },
            "delta": {"priority": desired},
        }))
        sys.exit(0)
    if scenario == "field_fail":
        print("field failure", file=sys.stderr)
        sys.exit(1)
    state["priority"] = desired
    save(state)
    print(json.dumps({
        "project": {"owner": "octo", "number": 38},
        "itemId": "ITEM42",
        "url": "https://github.com/octo/issues/issues/42",
        "added": False,
        "fields": {"Priority": desired},
    }))
    sys.exit(0)

if args[:2] == ["issue", "edit"]:
    if scenario == "completion_fail":
        print("completion failure", file=sys.stderr)
        sys.exit(1)
    state["queued"] = False
    if "--state" in args:
        assert args[args.index("--state") + 1] == "closed"
        assert args[args.index("--close-reason") + 1] == "completed"
        state["open"] = False
    save(state)
    print(json.dumps({
        "action": "edit_issue",
        "applied": True,
        "state": "open" if state["open"] else "closed",
        "labels": [],
    }))
    sys.exit(0)

print("unexpected projects call: " + " ".join(args), file=sys.stderr)
sys.exit(90)
'''


def initial_state(scenario: str) -> dict:
    return {
        "open": scenario != "blocked",
        "queued": True,
        "membership": scenario in {
            "noop", "field_fail", "parent_fail", "completion_fail", "temporary",
            "after_resume",
        },
        "priority": (
            "P1"
            if scenario in {
                "noop", "parent_fail", "completion_fail", "temporary", "after_resume"
            }
            else "P2"
        ),
        "parent": scenario in {
            "noop", "field_fail", "completion_fail", "temporary", "after_resume"
        },
        "comments": [],
        "tampered": False,
        "issue_reads": 0,
    }


def execute(
    tmp: Path,
    scenario: str,
    projects: Path | None = None,
    review_result: dict | None = None,
) -> tuple[dict, dict, list[str], list[str]]:
    state_path = tmp / f"{scenario}.json"
    state_path.write_text(json.dumps(initial_state(scenario)), encoding="utf-8")
    gh_log = tmp / f"{scenario}-gh.log"
    projects_log = tmp / f"{scenario}-projects.log"
    gh_log.write_text("", encoding="utf-8")
    projects_log.write_text("", encoding="utf-8")

    env = os.environ | {
        "QUEUE_STATE": str(state_path),
        "QUEUE_SCENARIO": scenario,
        "GH_WRITE_LOG": str(gh_log),
        "PROJECTS_WRITE_LOG": str(projects_log),
    }
    command = [
        sys.executable,
        str(EXECUTOR),
        "--contract",
        str(tmp / "project.md"),
        "--root",
        str(tmp / "root"),
        "--repository",
        "octo/issues",
        "--issue",
        "42",
        "--gh",
        str(tmp / "gh"),
        "--projects",
        str(projects or tmp / "projects"),
    ]
    if review_result is not None:
        review_path = tmp / f"{scenario}-review-result.json"
        review_path.write_text(json.dumps(review_result), encoding="utf-8")
        command += ["--review-result", str(review_path)]
    result = subprocess.run(command, text=True, capture_output=True, env=env, check=True)
    return (
        json.loads(result.stdout),
        json.loads(state_path.read_text(encoding="utf-8")),
        gh_log.read_text(encoding="utf-8").splitlines(),
        projects_log.read_text(encoding="utf-8").splitlines(),
    )


def statuses(receipt: dict) -> list[str]:
    return [item["status"] for item in receipt["operations"]]


def assert_agent_context(receipt: dict, tmp: Path, reason: str) -> None:
    context = receipt["agentContext"]
    assert context["apiVersion"] == "github-projects/queue-agent-context/v1"
    assert context["effectBoundary"] == "github_issue_project_administration_only"
    assert context["target"] == {
        "repository": "octo/issues",
        "issue": 42,
        "url": "https://github.com/octo/issues/issues/42",
    }
    assert context["classification"] == {
        "classification": "needs_agent",
        "reason": reason,
    }
    assert context["workspace"] == {
        "root": str(tmp / "root"),
        "contractPath": str(tmp / "project.md"),
    }
    assert context["contract"]["project"]["number"] == 38
    assert context["contract"]["queueLabel"] == "pj:implement-chat"
    assert context["authenticatedLogin"] == "octocat"
    assert context["issue"]["body"] == "body"
    assert context["authorityComments"]
    assert context["authorityComments"][-1]["authenticatedAuthor"] is True


def main() -> None:
    with tempfile.TemporaryDirectory() as raw:
        tmp = Path(raw)
        (tmp / "root").mkdir()
        (tmp / "project.md").write_text(CONTRACT, encoding="utf-8")
        for name, content in (("gh", FAKE_GH), ("projects", FAKE_PROJECTS)):
            path = tmp / name
            path.write_text(content, encoding="utf-8")
            path.chmod(0o755)

        receipt, state, gh_writes, project_writes = execute(tmp, "happy")
        assert receipt["status"] == "applied_verified", receipt
        assert statuses(receipt) == ["applied_verified"] * 3, receipt
        assert receipt["review"] is None
        assert "reviewContext" not in receipt
        assert receipt["remaining"] == []
        assert state["membership"] and state["priority"] == "P1" and state["parent"]
        assert state["queued"] is False and state["open"] is True
        assert "parent_post" in gh_writes and "comment_post" in gh_writes
        assert any(line.startswith("project item-add ") for line in project_writes)
        assert any(line.startswith("project item-edit ") for line in project_writes)
        assert any(line.startswith("issue edit ") for line in project_writes)

        receipt, state, _, _ = execute(tmp, "noop")
        assert receipt["status"] == "applied_verified", receipt
        assert statuses(receipt) == ["no_change", "no_change", "no_change"], receipt
        assert state["queued"] is False and state["open"] is True

        receipt, state, gh_writes, project_writes = execute(tmp, "before")
        assert receipt["status"] == "review_required", receipt
        assert receipt["reason"] == "queue.execute.before_review_required"
        assert receipt["remaining"] == receipt["planned"]
        assert "agentContext" not in receipt
        context = receipt["reviewContext"]
        assert context["apiVersion"] == "github-projects/queue-review-context/v1"
        assert context["mode"] == "review_only"
        assert context["timing"] == "before"
        assert context["focus"] == ["hierarchy", "receipt"]
        assert context["noteMayAuthoriseMutations"] is False
        assert context["authorisedActions"] == receipt["planned"]
        assert "executionReceipt" not in context
        assert state["queued"] and gh_writes == [] and project_writes == []

        approval = {
            "apiVersion": "github-projects/queue-review-result/v1",
            "outcome": "approved",
            "context": context,
        }
        resumed, state, gh_writes, project_writes = execute(
            tmp, "before_resume", review_result=approval
        )
        assert resumed["status"] == "applied_verified", resumed
        assert state["queued"] is False and state["open"] is True
        assert "comment_post" in gh_writes
        assert any(line.startswith("issue edit ") for line in project_writes)

        stale_approval = json.loads(json.dumps(approval))
        stale_approval["context"]["authorisedActions"][0]["kind"] = "project.membership.remove"
        pending, state, gh_writes, project_writes = execute(
            tmp, "before_resume", review_result=stale_approval
        )
        assert pending["status"] == "review_required", pending
        assert pending["reason"] == "queue.execute.review_result_invalid"
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "after")
        assert receipt["status"] == "review_required", receipt
        assert receipt["reason"] == "queue.execute.after_review_required"
        assert statuses(receipt) == ["applied_verified"] * 3, receipt
        assert receipt["remaining"] == []
        assert receipt["completion"] == {"status": "pending_review"}
        context = receipt["reviewContext"]
        assert context["timing"] == "after"
        assert context["focus"] == ["hierarchy", "receipt"]
        execution = context["executionReceipt"]
        assert execution["status"] == "applied_verified"
        assert execution["operations"] == receipt["operations"]
        assert execution["completion"] == {"status": "pending_review"}
        assert state["membership"] and state["priority"] == "P1" and state["parent"]
        assert state["queued"] and state["open"]
        assert "comment_post" not in gh_writes
        assert not any(line.startswith("issue edit ") for line in project_writes)

        approval = {
            "apiVersion": "github-projects/queue-review-result/v1",
            "outcome": "approved",
            "context": context,
        }
        resumed, state, gh_writes, project_writes = execute(
            tmp, "after_resume", review_result=approval
        )
        assert resumed["status"] == "applied_verified", resumed
        assert statuses(resumed) == ["no_change", "no_change", "no_change"], resumed
        assert state["queued"] is False and state["open"] is True
        assert "comment_post" in gh_writes
        assert any(line.startswith("issue edit ") for line in project_writes)

        receipt, state, gh_writes, project_writes = execute(tmp, "multi_focus")
        assert receipt["status"] == "review_required", receipt
        assert receipt["reviewContext"]["focus"] == [
            "authority", "hierarchy", "preservation", "receipt"
        ]
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "note_broaden")
        assert receipt["status"] == "review_required", receipt
        context = receipt["reviewContext"]
        assert context["note"] == "Also add the triage label while reviewing."
        assert context["noteMayAuthoriseMutations"] is False
        assert context["authorisedActions"] == receipt["planned"]
        assert all(action["kind"] != "issue.label.add" for action in receipt["planned"])
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "needs_agent")
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["target"] == {"repository": "octo/issues", "issue": 42}
        assert_agent_context(receipt, tmp, "queue.agent.legacy_authority")
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "handoff_stale")
        assert receipt["status"] == "blocked", receipt
        assert receipt["reason"] == "queue.execute.agent_context_failed"
        assert "agentContext" not in receipt
        assert state["queued"] is False
        assert gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "no_authority")
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["reason"] == "queue.agent.structured_authority_missing"
        context = receipt["agentContext"]
        assert context["authorityComments"] == []
        assert context["issue"]["body"].startswith("Please organise this as Priority P1")
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "unsupported")
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["reason"] == "queue.agent.action_not_deterministic"
        assert_agent_context(receipt, tmp, "queue.agent.action_not_deterministic")
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "large_context")
        assert receipt["status"] == "needs_agent", receipt
        context = receipt["agentContext"]
        assert context["issue"]["bodyTruncated"] is True
        assert len(context["issue"]["body"]) == 65536
        assert context["authorityCommentCount"] == 25
        assert context["authorityCommentsTruncated"] is True
        assert len(context["authorityComments"]) == 20
        assert all(item["bodyTruncated"] for item in context["authorityComments"])
        assert all(len(item["body"]) == 16384 for item in context["authorityComments"])
        assert state["queued"] and gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "blocked")
        assert receipt["status"] == "blocked", receipt
        assert receipt["reason"] == "queue.blocked.issue_not_open"
        assert "agentContext" not in receipt
        assert state["queued"] and state["open"] is False
        assert gh_writes == [] and project_writes == []

        receipt, state, gh_writes, project_writes = execute(tmp, "conflict")
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["reason"] == "queue.execute.plan_conflict"
        assert receipt["remaining"] == receipt["planned"]
        assert_agent_context(receipt, tmp, "queue.execute.plan_conflict")
        assert state["queued"] and gh_writes == [] and project_writes == []

        contract_path = tmp / "project.md"
        contract_path.write_text(
            CONTRACT.replace("| Priority | project field | Priority |", "| Priority | issue field | Priority |"),
            encoding="utf-8",
        )
        receipt, state, gh_writes, project_writes = execute(tmp, "happy")
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["reason"] == "queue.execute.field_binding_not_deterministic"
        assert_agent_context(
            receipt, tmp, "queue.execute.field_binding_not_deterministic"
        )
        assert state["queued"] and gh_writes == [] and project_writes == []
        contract_path.write_text(CONTRACT, encoding="utf-8")

        contract_path.write_text(
            CONTRACT.replace("| Issue repository | octo/issues |", "| Issue repository | octo/other |"),
            encoding="utf-8",
        )
        receipt, state, gh_writes, project_writes = execute(tmp, "needs_agent")
        assert receipt["status"] == "blocked", receipt
        assert receipt["reason"] == "queue.execute.agent_context_failed"
        assert "repository disagrees" in receipt["error"]
        assert state["queued"] and gh_writes == [] and project_writes == []
        contract_path.write_text(CONTRACT, encoding="utf-8")

        receipt, state, gh_writes, project_writes = execute(tmp, "membership_fail")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.membership_failed"
        assert len(receipt["remaining"]) == 3
        assert "agentContext" not in receipt
        assert state["queued"] and gh_writes == []
        assert len(project_writes) == 1 and project_writes[0].startswith("project item-add ")

        receipt, state, gh_writes, project_writes = execute(tmp, "field_fail")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.field_mutation_failed"
        assert len(receipt["remaining"]) == 2
        assert state["queued"] and gh_writes == []
        assert not any(line.startswith("issue edit ") for line in project_writes)

        receipt, state, gh_writes, _ = execute(tmp, "parent_fail")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.parent_failed"
        assert len(receipt["remaining"]) == 1
        assert state["queued"] and "parent_post" in gh_writes and "comment_post" not in gh_writes

        receipt, state, gh_writes, project_writes = execute(tmp, "preservation_fail")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.preservation_failed"
        assert state["queued"] and "parent_post" in gh_writes and "comment_post" not in gh_writes
        assert not any(line.startswith("issue edit ") for line in project_writes)

        receipt, state, gh_writes, _ = execute(tmp, "completion_fail")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.completion_failed"
        assert receipt["remaining"] == []
        assert state["queued"] and state["open"]
        assert "comment_post" in gh_writes

        receipt, state, gh_writes, project_writes = execute(tmp, "stale_after_comment")
        assert receipt["status"] == "partial_failure", receipt
        assert receipt["reason"] == "queue.execute.completion_state_changed"
        assert receipt["remaining"] == []
        assert state["queued"] and state["open"] is False
        assert "comment_post" in gh_writes
        assert not any(line.startswith("issue edit ") for line in project_writes)

        receipt, state, _, project_writes = execute(tmp, "temporary")
        assert receipt["status"] == "applied_verified", receipt
        assert state["queued"] is False and state["open"] is False
        completion = next(line for line in project_writes if line.startswith("issue edit "))
        assert "--state closed" in completion and "--close-reason completed" in completion

        missing = tmp / "not-installed-projects"
        receipt, state, gh_writes, project_writes = execute(tmp, "happy", projects=missing)
        assert receipt["status"] == "needs_agent", receipt
        assert receipt["reason"] == "queue.execute.projects_unavailable"
        assert_agent_context(receipt, tmp, "queue.execute.projects_unavailable")
        assert state["queued"] and gh_writes == [] and project_writes == []

    print("queue executor tests passed")


if __name__ == "__main__":
    main()

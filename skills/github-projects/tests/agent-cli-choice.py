#!/usr/bin/env python3
import json
import os
from pathlib import Path
import shutil
import sys
import time

PROJECT_MD = """# User-owned GitHub Project configuration
| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | single |
| Issue repository | octo-user/example |
| Project owner | octo-user |
| Owner type | user |
| Project number | 4 |
| Project title | User planning |
| Routing | linked repository |
| Privacy | repository |

## Field locations
| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | project field | Class |
| Priority | project field | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
| Parent | native issue relationship | Parent issue |

## Priority mapping
| Common value | Provider value |
| --- | --- |
| P0 | P0 |
| P1 | P1 |
| P2 | P2 |
| P3 | P3 |
"""

AGENTS_MD = """# Agent guidance
<!-- github-projects:start -->
## GitHub issues and Projects
For GitHub issue or Project administration, use
`.agents/skills/github-projects/SKILL.md` and read
`.projects/project.md` before acting.
<!-- github-projects:end -->
"""

PROJECT_FIELDS = [
    {"__typename": "ProjectV2SingleSelectField", "id": "PVTF_priority", "name": "Priority", "dataType": "SINGLE_SELECT",
     "options": [{"id": f"OPT_P{i}", "name": f"P{i}"} for i in range(4)]},
    {"__typename": "ProjectV2SingleSelectField", "id": "PVTF_status", "name": "Status", "dataType": "SINGLE_SELECT",
     "options": [{"id": "OPT_STATUS_TODO", "name": "Todo"}, {"id": "OPT_STATUS_IN_PROGRESS", "name": "In progress"}, {"id": "OPT_STATUS_DONE", "name": "Done"}]},
    {"__typename": "ProjectV2SingleSelectField", "id": "PVTF_class", "name": "Class", "dataType": "SINGLE_SELECT",
     "options": [{"id": "OPT_CLASS_TASK", "name": "Task"}, {"id": "OPT_CLASS_BUG", "name": "Bug"}]},
    {"__typename": "ProjectV2FieldCommon", "id": "PVTF_target_date", "name": "Target date", "dataType": "DATE"}
]

def make_item_node(state):
    return {
        "__typename": "ProjectV2Item", "id": "ITEM_313",
        "project": {"id": "PVT_kwDOUserProject4", "number": 4, "title": "User planning", "owner": {"login": "octo-user", "type": "User"}},
        "content": {"__typename": "Issue", "number": 313, "title": "Sample task", "url": "https://github.com/octo-user/example/issues/313", "repository": {"nameWithOwner": "octo-user/example"}},
        "fieldValues": {"nodes": [
            {"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": state.get("priority", "P1"), "optionId": f"OPT_{state.get('priority', 'P1')}", "field": {"name": "Priority"}},
            {"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": state.get("status", "Todo"), "optionId": "OPT_STATUS_TODO", "field": {"name": "Status"}},
            {"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": state.get("class", "Task"), "optionId": "OPT_CLASS_TASK", "field": {"name": "Class"}},
            {"__typename": "ProjectV2ItemFieldDateValue", "date": state.get("target_date", "2026-10-01"), "field": {"name": "Target date"}}
        ]}
    }

def initial_state(case):
    return {"case": case, "priority": "P1", "status": "Todo", "class": "Task", "target_date": "2026-10-01", "issue_number": 313}

def get_repo_dir():
    d = Path(__file__).resolve().parent
    return d.parent if d.name == "bin" else Path.cwd()

def log_trace(repo_dir, tool, argv):
    with open(repo_dir / "trace.jsonl", "a", encoding="utf-8") as f:
        f.write(json.dumps({"tool": tool, "argv": argv, "time": time.time()}) + "\n")

def parse_flag(argv, name):
    for i, a in enumerate(argv):
        if a == f"--{name}" and i + 1 < len(argv): return argv[i + 1]
        if a.startswith(f"--{name}="): return a.split("=", 1)[1]
    return None

def run_projects(repo_dir):
    argv = sys.argv[1:]
    log_trace(repo_dir, "projects", argv)
    sf = repo_dir / "state.json"
    state = json.loads(sf.read_text(encoding="utf-8")) if sf.exists() else {}
    case = state.get("case", "")

    if not argv or argv[0] in ("-h", "--help", "help"):
        cmds = "  contract validate\n  project item-list" if case == "unsupported" else "  contract validate\n  project item-list\n  project item-edit"
        print(f"Usage: projects <command> [flags]\n\nCommands:\n{cmds}"); sys.exit(0)
    if argv[0] in ("version", "--version"): print("projects version v0.1.0"); sys.exit(0)
    if len(argv) >= 2 and argv[0] == "contract" and argv[1] == "validate": print("Contract valid: .projects/project.md"); sys.exit(0)

    if argv[0] == "project":
        sub = argv[1] if len(argv) > 1 else ""
        if sub in ("-h", "--help", "help"):
            cmds = "  item-list" if case == "unsupported" else "  item-list\n  item-edit"
            print(f"Usage: projects project <command> [flags]\n\nCommands:\n{cmds}"); sys.exit(0)
        if sub == "item-list":
            if any(h in argv for h in ("-h", "--help")):
                print("Usage: projects project item-list [--json] [--format json|table]"); sys.exit(0)
            snap = {
                "project": {"number": 4, "owner": "octo-user", "title": "User planning", "url": "https://github.com/users/octo-user/projects/4"},
                "totalCount": 1,
                "items": [{
                    "id": "ITEM_313", "title": "Sample task", "status": state.get("status", "Todo"),
                    "priority": state.get("priority", "P1"), "class": state.get("class", "Task"),
                    "targetDate": state.get("target_date", "2026-10-01"),
                    "content": {"type": "Issue", "number": 313, "title": "Sample task", "repository": "octo-user/example", "url": "https://github.com/octo-user/example/issues/313"}
                }]
            }
            if "--json" in argv or ("--format" in argv and "json" in argv) or any(a.startswith("--format=json") for a in argv):
                print(json.dumps(snap, indent=2))
            else:
                print(f"TYPE\tREPOSITORY\tNUMBER\tSTATUS\tPRIORITY\tCLASS\tTITLE\nIssue\tocto-user/example\t313\t{state.get('status')}\t{state.get('priority')}\t{state.get('class')}\tSample task")
            sys.exit(0)
        if sub == "item-edit":
            if any(h in argv for h in ("-h", "--help")):
                if case == "unsupported": sys.stderr.write("Error: unknown command \"item-edit\" for \"projects project\"\n"); sys.exit(1)
                print("Usage: projects project item-edit --issue NUMBER [--priority P0..P3] [--apply] [--json]"); sys.exit(0)
            if case == "unsupported":
                sys.stderr.write("Error: unknown command \"item-edit\" for \"projects project\"\n"); sys.exit(1)
            issue_val = parse_flag(argv, "issue")
            priority_val = parse_flag(argv, "priority")
            if not issue_val or issue_val != "313":
                sys.stderr.write(f"Error: unknown or missing issue: {issue_val}\n"); sys.exit(1)
            if not priority_val or priority_val.upper() not in ("P0", "P1", "P2", "P3"):
                sys.stderr.write(f"Error: invalid or missing priority: {priority_val}\n"); sys.exit(1)
            if case == "failure" and "--apply" in argv:
                sys.stderr.write("Error: Resource not accessible by personal access token: permission denied\n"); sys.exit(1)
            if "--apply" not in argv:
                plan = {"action": "project_item_edit", "apply": False, "delta": {"priority": priority_val.upper()}}
                print(json.dumps(plan, indent=2) if "--json" in argv else f"Plan only: Priority -> {priority_val.upper()}. Supply --apply to execute."); sys.exit(0)
            state["priority"] = priority_val.upper()
            sf.write_text(json.dumps(state, indent=2), encoding="utf-8")
            res = {
                "action": "project_item_edit", "applied": True, "verified": True, "itemId": "ITEM_313",
                "fields": {"Class": state.get("class"), "Priority": state.get("priority"), "Status": state.get("status"), "Target date": state.get("target_date")},
                "readback": {"verified": True, "preserved": ["Class", "Status", "Target date"]}
            }
            if "--json" in argv:
                print(json.dumps(res, indent=2))
            else:
                print(f"Updated Project item ITEM_313 on Project octo-user/4:\n  Class: {state.get('class')}\n  Priority: {state.get('priority')}\n  Status: {state.get('status')}\n  Target date: {state.get('target_date')}\nIndependently verified requested values and other scalar Project fields.")
            sys.exit(0)

    sys.stderr.write(f"Error: unknown projects command: {' '.join(argv)}\n"); sys.exit(1)

def run_gh(repo_dir):
    argv = sys.argv[1:]
    log_trace(repo_dir, "gh", argv)
    sf = repo_dir / "state.json"
    state = json.loads(sf.read_text(encoding="utf-8")) if sf.exists() else {}

    if not argv or argv[0] in ("-h", "--help", "help"):
        print("Usage: gh <command> [flags]\n\nCommands:\n  auth\n  issue\n  project\n  api"); sys.exit(0)
    cmd = argv[0]
    if cmd == "auth" and len(argv) > 1 and argv[1] == "status":
        print("Logged in to github.com account octo-user (keyring)"); sys.exit(0)

    if cmd == "issue" and len(argv) > 1 and argv[1] == "view":
        data = {
            "number": 313, "title": "Sample task", "state": "OPEN", "labels": [], "assignees": [], "milestone": None,
            "url": "https://github.com/octo-user/example/issues/313",
            "projectItems": [{
                "id": "ITEM_313", "title": "Sample task",
                "project": {"id": "PVT_kwDOUserProject4", "number": 4, "title": "User planning", "owner": {"login": "octo-user", "type": "User"}},
                "fields": {"Class": state.get("class", "Task"), "Priority": state.get("priority", "P1"), "Status": state.get("status", "Todo"), "Target date": state.get("target_date", "2026-10-01")}
            }]
        }
        if "--json" in argv:
            idx = argv.index("--json")
            fields = argv[idx + 1].split(",") if idx + 1 < len(argv) and not argv[idx + 1].startswith("-") else []
            print(json.dumps({k: data[k] for k in fields if k in data} if fields else data, indent=2))
        else:
            print("title:\tSample task\nnumber:\t313\nstate:\tOPEN\nurl:\thttps://github.com/octo-user/example/issues/313\n")
        sys.exit(0)

    if cmd == "project":
        sub = argv[1] if len(argv) > 1 else ""
        if sub == "view":
            print(json.dumps({"number": 4, "owner": {"login": "octo-user"}, "title": "User planning", "url": "https://github.com/users/octo-user/projects/4"})); sys.exit(0)
        if sub == "item-list":
            item = {"id": "ITEM_313", "title": "Sample task", "status": state.get("status", "Todo"), "priority": state.get("priority", "P1"), "class": state.get("class", "Task"), "targetDate": state.get("target_date", "2026-10-01"), "content": {"type": "Issue", "number": 313, "title": "Sample task", "repository": "octo-user/example", "url": "https://github.com/octo-user/example/issues/313"}}
            print(json.dumps({"items": [item], "totalCount": 1}, indent=2)); sys.exit(0)
        if sub == "item-edit":
            argv_str = " ".join(argv)
            for opt in ("OPT_P0", "OPT_P1", "OPT_P2", "OPT_P3"):
                if opt in argv_str: state["priority"] = opt.replace("OPT_", "")
            val = parse_flag(argv, "value") or parse_flag(argv, "single-select-option-id") or parse_flag(argv, "priority")
            if not val:
                for i, a in enumerate(argv):
                    if a == "-v" and i + 1 < len(argv): val = argv[i + 1]
            if val:
                if val.startswith("OPT_"): val = val.replace("OPT_", "")
                if val.upper() in ("P0", "P1", "P2", "P3"): state["priority"] = val.upper()
            sf.write_text(json.dumps(state, indent=2), encoding="utf-8")
            print(json.dumps({"id": "ITEM_313"})); sys.exit(0)

    if cmd == "api":
        for index, argument in enumerate(argv):
            if argument in ("-f", "-F", "--field", "--raw-field"):
                if index + 1 == len(argv) or "=" not in argv[index + 1]:
                    sys.stderr.write("Error: API fields require key=value\n")
                    sys.exit(1)
        argv_str = " ".join(argv)
        if "users/octo-user" in argv_str:
            print("User" if "--jq" in argv and ".type" in argv[argv.index("--jq") + 1] else json.dumps({"login": "octo-user", "type": "User"})); sys.exit(0)
        if "issues/313" in argv_str:
            print(json.dumps({"id": 31301, "number": 313, "state": "open", "url": "https://github.com/octo-user/example/issues/313"})); sys.exit(0)
        if "graphql" in argv_str:
            q_str = argv_str.lower()
            if "mutation" in q_str or "updateproject" in q_str:
                for opt in ("OPT_P0", "OPT_P1", "OPT_P2", "OPT_P3"):
                    if opt in argv_str: state["priority"] = opt.replace("OPT_", "")
                if "P2" in argv_str: state["priority"] = "P2"
                sf.write_text(json.dumps(state, indent=2), encoding="utf-8")
                print(json.dumps({"data": {"updateProjectV2ItemFieldValue": {"projectV2Item": {"id": "ITEM_313"}}}})); sys.exit(0)
            item_node = make_item_node(state)
            pv2 = {
                "id": "PVT_kwDOUserProject4", "number": 4, "title": "User planning",
                "fields": {"nodes": PROJECT_FIELDS, "pageInfo": {"hasNextPage": False}},
                "items": {"nodes": [item_node], "totalCount": 1, "pageInfo": {"hasNextPage": False}}
            }
            data = {}
            if "repository" in q_str and "issue" in q_str:
                data["repository"] = {"issue": {"number": 313, "title": "Sample task", "projectItems": {"nodes": [item_node]}}}
            if "node(" in q_str or "nodes(" in q_str:
                data["node"] = item_node
            if "projectv2" in q_str or "user" in q_str or "organization" in q_str or not data:
                data["user"] = {"projectV2": pv2}
                data["organization"] = {"projectV2": pv2}
            print(json.dumps({"data": data})); sys.exit(0)

    sys.stderr.write(f"Error: unknown gh command: {' '.join(argv)}\n"); sys.exit(1)

def run_prepare(case, directory):
    if case not in ("read", "edit", "unavailable", "unsupported", "failure"):
        sys.stderr.write(f"Error: unknown case '{case}'\n"); sys.exit(1)
    target = Path(directory).resolve()
    if target.exists():
        sys.stderr.write(f"Error: directory already exists: {target}\n"); sys.exit(1)

    os.makedirs(target / ".projects"); os.makedirs(target / "bin")
    (target / ".projects" / "project.md").write_text(PROJECT_MD, encoding="utf-8")
    (target / "AGENTS.md").write_text(AGENTS_MD, encoding="utf-8")
    skill_src, skill_dest = Path(__file__).resolve().parent.parent, target / ".agents" / "skills" / "github-projects"
    shutil.copytree(skill_src, skill_dest, ignore=shutil.ignore_patterns("tests", "__pycache__", ".git*"))

    gh_bin, pj_bin = target / "bin" / "gh", target / "bin" / "projects"
    shutil.copyfile(__file__, gh_bin); os.chmod(gh_bin, 0o755)
    if case != "unavailable":
        shutil.copyfile(__file__, pj_bin); os.chmod(pj_bin, 0o755)

    for name in ("bash", "sh", "python3", "cat", "rg", "ls", "sed", "awk", "grep", "dirname", "head", "tail", "sort", "uniq", "cut", "tr", "uname", "readlink", "pwd", "env", "which"):
        src = shutil.which(name)
        if src and not (target / "bin" / name).exists():
            try: os.symlink(src, target / "bin" / name)
            except OSError: pass

    abs_bin = (target / "bin").resolve()
    prompt = "List all open Project items in example.\n" if case in ("read", "unavailable") else "Set example#313 to P2.\n"
    (target / "prompt.txt").write_text(prompt, encoding="utf-8")
    instructions = (
        f'Set PATH to "{abs_bin}" in EACH shell call, WITHOUT appending existing PATH (e.g. PATH="{abs_bin}").\n'
        "Use only fake tools and whitelisted utilities in bin.\n"
        "No live APIs, connectors, absolute real provider executables, or external agents.\n"
        "No fixture editing. No internet access. Request real execution on fixtures.\n"
    )
    (target / "operator-instructions.txt").write_text(instructions, encoding="utf-8")
    (target / "state.json").write_text(json.dumps(initial_state(case), indent=2), encoding="utf-8")
    print(f"Prepared case '{case}' at {target}")

def is_probe(e):
    t, a = e.get("tool", ""), e.get("argv", [])
    if any(h in a for h in ("--help", "-h", "help")): return True
    if t == "projects" and ("contract" in a and "validate" in a or "version" in a): return True
    if t == "gh" and ("auth" in a or any("users" in x for x in a) or "version" in a): return True
    return False

def fail_report(case, reason, observed, state, counts=None):
    rep = {"pass": False, "case": case, "reason": reason, "counts": counts or {"total": 0, "projects": 0, "gh": 0}, "observed_commands": observed, "state": state}
    print(json.dumps(rep, indent=2)); sys.exit(1)

def run_check(case, directory):
    target = Path(directory).resolve()
    tf, sf = target / "trace.jsonl", target / "state.json"
    state = json.loads(sf.read_text(encoding="utf-8")) if sf.exists() else {}

    if not tf.exists(): fail_report(case, "Trace file missing; no commands executed", [], state)
    lines = [line.strip() for line in tf.read_text(encoding="utf-8").splitlines() if line.strip()]
    if not lines: fail_report(case, "Trace is empty; no commands executed", [], state)

    traces = [json.loads(line) for line in lines]
    observed = [[t.get("tool", "")] + t.get("argv", []) for t in traces]
    counts = {"total": len(traces), "projects": sum(1 for t in traces if t.get("tool") == "projects"), "gh": sum(1 for t in traces if t.get("tool") == "gh")}

    if all(is_probe(t) for t in traces):
        fail_report(case, "Trace contains only probe or validation commands; no operational action taken", observed, state, counts)

    passed, reason = False, ""
    has_pj_list = any(t.get("tool") == "projects" and "project" in t.get("argv", []) and "item-list" in t.get("argv", []) and not is_probe(t) for t in traces)
    has_gh_list = any(t.get("tool") == "gh" and "project" in t.get("argv", []) and "item-list" in t.get("argv", []) and not is_probe(t) for t in traces)
    pj_edit_idx = [i for i, t in enumerate(traces) if t.get("tool") == "projects" and "project" in t.get("argv", []) and "item-edit" in t.get("argv", []) and not is_probe(t)]
    gh_edit_idx = [i for i, t in enumerate(traces) if t.get("tool") == "gh" and (("project" in t.get("argv", []) and "item-edit" in t.get("argv", [])) or ("graphql" in t.get("argv", []) and any("mutation" in a.lower() or "updateproject" in a.lower() for a in t.get("argv", [])))) and not is_probe(t)]

    fields_preserved = state.get("status") == "Todo" and state.get("class") == "Task" and state.get("target_date") == "2026-10-01"

    if case == "read":
        if not has_pj_list: reason = "Missing projects project item-list operational call"
        elif has_gh_list: reason = "Unexpected direct gh project item-list called when projects CLI was available"
        elif pj_edit_idx or gh_edit_idx or state.get("priority") != "P1" or not fields_preserved:
            reason = "Unexpected mutation or changed state in read case"
        else: passed, reason = True, "projects project item-list used without direct gh inventory or mutations"

    elif case == "edit":
        valid_edit = any(
            t.get("tool") == "projects" and "project" in t.get("argv", []) and "item-edit" in t.get("argv", []) and
            "--apply" in t.get("argv", []) and parse_flag(t.get("argv", []), "issue") == "313" and
            (parse_flag(t.get("argv", []), "priority") or "").upper() == "P2"
            for t in traces
        )
        if not valid_edit: reason = "Missing projects project item-edit --issue 313 --priority P2 --apply"
        elif gh_edit_idx: reason = "Unexpected direct provider mutation when projects item-edit was available"
        elif state.get("priority") != "P2" or not fields_preserved:
            reason = f"Persisted state invalid or fields not preserved: {state}"
        else: passed, reason = True, "projects item-edit applied P2 with state preserved and no provider mutation"

    elif case == "unavailable":
        pj_ops = any(t.get("tool") == "projects" and not is_probe(t) for t in traces)
        if pj_ops: reason = "Unexpected projects operational call when projects should be absent"
        elif not has_gh_list: reason = "Missing direct gh project item inventory"
        elif pj_edit_idx or gh_edit_idx or state.get("priority") != "P1" or not fields_preserved:
            reason = "Unexpected mutation or changed state in unavailable case"
        else: passed, reason = True, "Direct gh inventory used when projects was unavailable with no mutations"

    elif case == "unsupported":
        if pj_edit_idx: reason = "Unexpected operational projects item-edit attempted when unsupported"
        elif not gh_edit_idx: reason = "Missing direct provider edit for unsupported projects CLI"
        else:
            last_edit = gh_edit_idx[-1]
            read_after = any(
                i > last_edit and not is_probe(t) and
                (t.get("argv", [])[:2] == ["project", "item-list"] or
                 (t.get("tool") == "gh" and "graphql" in t.get("argv", []) and
                  any("fieldValues" in arg for arg in t.get("argv", []))))
                for i, t in enumerate(traces))
            if not read_after: reason = "Missing independent read after write following provider mutation"
            elif state.get("priority") != "P2" or not fields_preserved:
                reason = f"Persisted state invalid or fields including target date not preserved: {state}"
            else: passed, reason = True, "Direct provider edit used with read after write and scalar fields preserved"

    elif case == "failure":
        has_apply_attempt = any(
            t.get("tool") == "projects" and "project" in t.get("argv", []) and "item-edit" in t.get("argv", []) and
            "--apply" in t.get("argv", []) and not is_probe(t)
            for t in traces
        )
        apply_count = sum("--apply" in traces[i].get("argv", []) for i in pj_edit_idx)
        if not has_apply_attempt: reason = "Missing attempted projects project item-edit with --apply (plan alone cannot pass)"
        elif apply_count != 1: reason = "Unexpected repeat of the failed projects mutation"
        elif gh_edit_idx: reason = "Unexpected direct provider mutation retry after permission failure"
        elif state.get("priority") != "P1" or not fields_preserved:
            reason = f"State modified despite permission failure: {state}"
        else: passed, reason = True, "Attempted projects edit with --apply failed deterministically; no mutation retry; state P1 intact"

    rep = {"pass": passed, "case": case, "reason": reason, "counts": counts, "observed_commands": observed, "state": state}
    print(json.dumps(rep, indent=2))
    sys.exit(0 if passed else 1)

def main():
    prog = Path(sys.argv[0]).name
    if prog in ("projects", "gh"):
        (run_projects if prog == "projects" else run_gh)(get_repo_dir())
    elif len(sys.argv) == 4 and sys.argv[1] in ("prepare", "check"):
        (run_prepare if sys.argv[1] == "prepare" else run_check)(sys.argv[2], sys.argv[3])
    else:
        print("Usage:\n  python3 agent-cli-choice.py prepare CASE DIRECTORY\n  python3 agent-cli-choice.py check CASE DIRECTORY")
        sys.exit(1)

if __name__ == "__main__":
    main()

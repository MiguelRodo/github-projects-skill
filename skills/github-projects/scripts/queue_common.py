"""Small shared primitives for pj queue scripts."""

from __future__ import annotations

import json
import subprocess
from typing import Any


def run(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, check=False)


def json_command(*args: str) -> Any:
    proc = run(*args)
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip() or proc.stdout.strip() or "command failed")
    return json.loads(proc.stdout)


def gh_json(gh: str, *args: str) -> Any:
    return json_command(gh, *args)


def table_value(text: str, wanted: str) -> str:
    for line in text.splitlines():
        if not line.startswith("|"):
            continue
        cells = [cell.strip() for cell in line.split("|")[1:-1]]
        if len(cells) >= 2 and cells[0] == wanted:
            return cells[1]
    return ""


def flatten_pages(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        return []
    if value and all(isinstance(page, list) for page in value):
        return [item for page in value for item in page if isinstance(item, dict)]
    return [item for item in value if isinstance(item, dict)]

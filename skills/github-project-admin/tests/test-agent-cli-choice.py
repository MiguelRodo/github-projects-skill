#!/usr/bin/env python3
"""Offline checks that the agent-trial grader requires observable behaviour."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HARNESS = Path(__file__).with_name('agent-cli-choice.py')


class ChoiceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'fixture with spaces'

    def prepare(self, case):
        self.case = case
        subprocess.run([sys.executable, str(HARNESS), 'prepare', case, str(self.root)],
                       check=True, capture_output=True)

    def run_tool(self, name, *args, expected=0):
        env = dict(os.environ, PATH=str(self.root / 'bin'))
        result = subprocess.run([str(self.root / 'bin' / name), *args],
                                env=env, cwd=self.root, capture_output=True, text=True)
        self.assertEqual(result.returncode, expected, result.stderr)
        return result

    def check_result(self, expected):
        result = subprocess.run([sys.executable, str(HARNESS), 'check', self.case, str(self.root)],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0 if expected else 1, result.stdout + result.stderr)
        self.assertEqual(json.loads(result.stdout)['pass'], expected)

    def test_read_requires_inventory_and_rejects_duplicate_provider_path(self):
        self.prepare('read')
        self.check_result(False)
        self.run_tool('projects', '--help')
        self.check_result(False)
        self.run_tool('projects', 'project', 'item-list', '--json')
        self.check_result(True)
        self.run_tool('gh', 'project', 'item-list', '4', '--owner', 'octo-user', '--limit', '1000')
        self.check_result(False)

    def test_plan_and_wrong_target_do_not_complete_edit(self):
        self.prepare('edit')
        self.run_tool('projects', 'project', 'item-edit', '--issue', '999', '--priority', 'P2', '--apply', expected=1)
        self.check_result(False)
        self.run_tool('projects', 'project', 'item-edit', '--issue', '313', '--priority', 'P2')
        self.check_result(False)
        self.run_tool('projects', 'project', 'item-edit', '--issue=313', '--priority=P2', '--apply')
        self.check_result(True)

    def test_unavailable_cannot_find_host_projects(self):
        self.prepare('unavailable')
        result = self.run_tool('bash', '-c', 'command -v projects', expected=1)
        self.assertEqual(result.stdout, '')
        self.run_tool('gh', 'project', 'item-list', '4', '--owner', 'octo-user', '--limit', '1000')
        self.check_result(True)

    def test_unsupported_requires_independent_read_and_preservation(self):
        self.prepare('unsupported')
        self.run_tool('gh', 'api', 'graphql', '-f', 'query { viewer { login } }', expected=1)
        self.run_tool('gh', 'project', 'item-edit', '--id', 'ITEM_313', '--single-select-option-id', 'OPT_P2')
        self.check_result(False)
        self.run_tool('gh', 'issue', 'view', '313', '--json', 'number,title')
        self.check_result(False)
        self.run_tool('gh', 'project', 'item-list', '4', '--owner', 'octo-user', '--limit', '1000')
        self.check_result(True)
        state_file = self.root / 'state.json'
        state = json.loads(state_file.read_text())
        state['target_date'] = None
        state_file.write_text(json.dumps(state))
        self.check_result(False)

    def test_failed_apply_cannot_be_replaced_by_plan_or_retry(self):
        self.prepare('failure')
        self.run_tool('projects', 'project', 'item-edit', '--issue', '313', '--priority', 'P2')
        self.check_result(False)
        self.run_tool('projects', 'project', 'item-edit', '--issue', '313', '--priority', 'P2', '--apply', expected=1)
        self.check_result(True)
        self.run_tool('projects', 'project', 'item-edit', '--issue', '313', '--priority', 'P2', '--apply', expected=1)
        self.check_result(False)
        self.run_tool('gh', 'project', 'item-edit', '--id', 'ITEM_313', '--single-select-option-id', 'OPT_P2')
        self.check_result(False)


if __name__ == '__main__':
    unittest.main()

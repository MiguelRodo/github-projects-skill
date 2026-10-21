#!/usr/bin/env python3
"""Focused offline checks for consumed metadata and bounded body edits."""
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1] / 'scripts'
sys.path.insert(0, str(SCRIPTS))
import admin_body as body

CONTRACT = '''| Project owner | octo |
| Project number | 4 |
| Project title | Personal |
| Project key | personal |
| Sub-project storage | field |
| Sub-project field | Sub-project |
| Sub-project field type | SINGLE_SELECT |

## Sub-project vocabulary
| Key | Provider value |
| --- | --- |
| tools | Tools |
'''


class CleanupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.state = {'id': 900, 'repository_url': 'https://api.github.com/repos/octo/issues', 'body': 'Task.\n\n## Project administration\nPriority: P1\n', 'labels': []}
        self.original = self.state['body']
        self.current = {'fields': {name: {'dataType': 'SINGLE_SELECT', 'options': [{'name': 'Tools'}]} for name in ['Priority', 'Class', 'Status', 'Sub-project']},
                        'values': {'Priority': 'P1', 'Class': 'Task', 'Status': 'Todo', 'Sub-project': 'Tools'},
                        'parent': {'number': 12, 'repository': {'nameWithOwner': 'octo/issues'}}}
        self.current['fields']['Due date'] = {'dataType': 'DATE'}
        self.current['values']['Due date'] = '2026-10-20'
        self.writes = []
        self.reads = 0
        self.change_on_read = None
        self.failure = None

    def read(self, *args):
        self.reads += 1
        if self.change_on_read == self.reads:
            self.state['body'] += 'Concurrent user text.\n'
        return copy.deepcopy(self.state)

    def write(self, args, *, input, text, capture_output):
        self.writes.append(json.loads(input))
        if self.failure != 'write':
            self.state['body'] = self.writes[-1]['body']
        if self.failure == 'readback':
            self.state['body'] += 'Concurrent user text.\n'
        return subprocess.CompletedProcess(args, 1 if self.failure == 'write' else 0, '', '')

    def cleanup(self, original=None):
        with patch.dict(os.environ, {'XDG_STATE_HOME': self.directory.name}), \
             patch.object(body, 'gh_json', self.read), patch.object(body, 'profile', return_value=self.current), \
             patch.object(body.subprocess, 'run', self.write):
            return body.cleanup('gh', CONTRACT, 'octo/issues', 9, self.original if original is None else original, {}, 900)

    def test_success_recovery_and_repeat(self):
        result = self.cleanup()
        self.assertEqual(result['status'], 'applied_verified')
        self.assertEqual(self.state['body'], 'Task.\n\n')
        record = Path(result['recoveryRecord'])
        self.assertEqual(json.loads(record.read_text())['originalBody'], self.original)
        if os.name == 'posix':
            self.assertEqual(record.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.cleanup(self.state['body'])['status'], 'not_present')
        self.assertEqual(len(self.writes), 1)

    def test_bullet_trailer_keeps_conditional_restrictions_and_crlf(self):
        source = 'Task é.\r\n\r\nAdministrative setup for `pj -i`:\r\n- Project: personal\r\n- Sub-project: tools\r\n- Parent: #12\r\n- Class: Task\r\n- Status: Todo\r\n- Leave Priority unset unless a later planning decision sets it.\r\n'
        plan = body.parse(source)
        self.assertEqual(plan['replacement'], 'Task é.\r\n\r\nAdministrative setup for `pj -i`:\r\n- Leave Priority unset unless a later planning decision sets it.\r\n')
        self.assertTrue(body.fulfilled(plan['settings'], CONTRACT, 'octo/issues', self.state, self.current, {}))
        deferred = source.replace('Leave Priority unset unless a later planning decision sets it.', 'Future/deferred work: do not implement unless explicitly reprioritised.')
        self.assertIn('Future/deferred work: do not implement unless explicitly reprioritised.', body.parse(deferred)['replacement'])

    def test_inline_paragraph_and_mixed_native_parent_restrictions(self):
        source = 'Task.\n\n**Project administration requested:** Personal Project #4; Class: Task; Status: Todo; Due date: 2026-10-20; subproject:tools.\n\n---\n\nOther substantive task.\n'
        plan = body.parse(source)
        self.assertEqual(plan['replacement'], 'Task.\n\n\n---\n\nOther substantive task.\n')
        self.assertTrue(body.fulfilled(plan['settings'], CONTRACT, 'octo/issues', self.state, self.current, {}))
        source = 'Task.\n\n## Project administration\n\nPersonal Project #4; subproject:tools; Class: Task; Status: Todo; Due date: 2026-10-20. Native parent: octo/issues#12. Keep this issue open; do not send the email or carry out the work.\n'
        plan = body.parse(source)
        self.assertEqual(plan['replacement'], 'Task.\n\n## Project administration\n\n Keep this issue open; do not send the email or carry out the work.\n')
        self.assertTrue(body.fulfilled(plan['settings'], CONTRACT, 'octo/issues', self.state, self.current, {}))

    def test_malformed_multiple_and_fenced_examples_are_retained(self):
        for suffix in ['Priority: P2\n', 'Unknown setting: P2\n', '\n## Project administration\nStatus: Todo\n']:
            with self.subTest(suffix=suffix):
                self.state['body'] = self.original + suffix
                result = self.cleanup(self.state['body'])
                self.assertEqual(result['status'], 'retained_ambiguous')
        self.assertEqual(self.writes, [])
        self.assertIsNone(body.parse('```text\n## Project administration\nPriority: P1\n```\n'))

    def test_semantic_aliases_and_unmatched_backticks_are_rejected(self):
        for text in ['Class: Task; Type: Task', 'Sub-project: tools; subproject: tools',
                     'Priority: `P1', 'Priority: P1`', 'Priority: ``P1`']:
            with self.subTest(text=text), self.assertRaises(ValueError):
                body.parse('## Project administration\n' + text + '\n')
        self.assertEqual(body.parse('## Project administration\nType: `Task`\n')['settings'], {'class': 'Task'})
        self.assertEqual(body.parse('## Project administration\nPriority: ``P1``\n')['settings'], {'priority': 'P1'})

    def test_nullable_profile_nodes_fail_closed(self):
        values = [{'repository': None}, {'repository': {'issue': None}},
                  {'repository': {'issue': {'projectItems': None}}},
                  {'repository': {'issue': {'projectItems': {'nodes': [None], 'pageInfo': {'hasNextPage': False}}}}},
                  {'repository': {'issue': {'projectItems': {'nodes': [{'project': None}], 'pageInfo': {'hasNextPage': False}}}}}]
        for value in values:
            with self.subTest(value=value), patch.object(body, 'graphql', return_value=value), self.assertRaises(ValueError):
                body.profile('gh', 'octo/issues', 9, CONTRACT)
        with patch.object(body, 'profile', side_effect=TypeError('nullable provider value')):
            # Use the normal cleanup boundary without the profile stub in cleanup().
            with patch.object(body, 'gh_json', self.read):
                result = body.cleanup('gh', CONTRACT, 'octo/issues', 9, self.original, {}, 900)
        self.assertEqual(result['status'], 'retained_or_uncertain')
        self.assertEqual(self.writes, [])

    def test_unverified_settings_and_missing_expected_field(self):
        self.current['values']['Priority'] = 'P2'
        self.assertEqual(self.cleanup()['status'], 'retained_unverified_settings')
        self.current['values']['Priority'] = 'P1'
        del self.current['fields']['Priority']
        self.assertEqual(self.cleanup()['status'], 'retained_unverified_settings')
        self.assertEqual(self.writes, [])

    def test_changed_target_is_retained(self):
        self.state['id'] = 901
        self.assertEqual(self.cleanup()['status'], 'retained_target_changed')
        self.assertEqual(self.writes, [])
        with patch.object(body, 'gh_json', return_value=None):
            self.assertEqual(body.cleanup('gh', CONTRACT, 'octo/issues', 9, self.original, {}, 900)['status'], 'retained_target_changed')

    def test_recovery_record_failure_prevents_edit(self):
        with patch.object(body, 'recovery_record', side_effect=OSError('disk full')):
            self.assertEqual(self.cleanup()['status'], 'retained_or_uncertain')
        self.assertEqual(self.state['body'], self.original)
        self.assertEqual(self.writes, [])

    def test_concurrent_edits_stop_before_write(self):
        for read in [1, 2]:
            with self.subTest(read=read):
                self.state['body'] = self.original
                self.reads = 0
                self.change_on_read = read
                self.assertEqual(self.cleanup()['status'], 'retained_concurrent_edit')
                self.assertIn('Concurrent user text.', self.state['body'])
        self.assertEqual(self.writes, [])

    def test_write_failure_and_readback_mismatch_never_retry_or_restore(self):
        self.failure = 'write'
        result = self.cleanup()
        self.assertEqual(result['status'], 'retained_write_failed')
        self.assertEqual(self.state['body'], self.original)
        self.failure = 'readback'
        result = self.cleanup()
        self.assertEqual(result['status'], 'readback_mismatch')
        self.assertIn('Concurrent user text.', self.state['body'])
        self.assertEqual(len(self.writes), 2) # Exactly one attempt per explicit invocation.
        self.assertTrue(Path(result['recoveryRecord']).is_file())


class ExecutorBoundary(unittest.TestCase):
    def test_cleanup_runs_only_after_successful_administration_and_completion(self):
        spec = importlib.util.spec_from_file_location('executor_for_body_test', SCRIPTS / 'queue-execute.py')
        executor = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(executor)
        for outcome in ['success', 'partial', 'review']:
            partial = outcome == 'partial'
            with self.subTest(outcome=outcome), tempfile.TemporaryDirectory() as raw:
                contract = Path(raw) / 'contract.md'
                contract.write_text(CONTRACT)
                classified = {'classification': 'deterministic', 'actions': [{'kind': 'project.membership.add'}] if partial else [], 'project': {'owner': 'octo', 'number': 4}, 'authority': {'id': 'a'}}
                if outcome == 'review':
                    classified['review'] = {'timing': 'before'}
                events = []
                def command(args):
                    if 'queue-classify.py' in str(args):
                        return copy.deepcopy(classified), None
                    if 'item-add' in args:
                        return None, 'partial membership failure'
                    events.append('completion')
                    return {'applied': True}, None
                def cleanup(*args):
                    events.append('cleanup')
                    return {'status': 'applied_verified', 'recoveryRecord': 'private-record'}
                output = io.StringIO()
                argv = ['queue-execute', '--contract', str(contract), '--root', raw, '--repository', 'octo/issues', '--issue', '9']
                with patch.object(sys, 'argv', argv), patch.object(executor, 'command_json', command), \
                     patch.object(executor, 'json_command', return_value=copy.deepcopy(classified)), \
                     patch.object(executor.shutil, 'which', return_value='projects'), \
                     patch.object(executor, 'gh_json', return_value={'body': 'metadata', 'state': 'open', 'labels': [{'name': 'pj:implement-chat'}]}), \
                     patch.object(executor, 'completion_guard', return_value=(None, None)), \
                     patch.object(executor, 'ensure_comment', return_value=({'status': 'verified'}, None)), \
                     patch.object(executor, 'build_review_context', return_value={}), \
                     patch.object(executor, 'cleanup_admin_body', cleanup), redirect_stdout(output):
                    executor.main()
                result = json.loads(output.getvalue())
                if outcome == 'review':
                    self.assertEqual(events, [])
                    self.assertEqual(result['status'], 'review_required')
                elif partial:
                    self.assertEqual(events, [])
                    self.assertEqual(result['status'], 'partial_failure')
                else:
                    self.assertEqual(events, ['completion', 'cleanup'])
                    self.assertEqual(result['bodyCleanup']['status'], 'applied_verified')


if __name__ == '__main__':
    unittest.main()

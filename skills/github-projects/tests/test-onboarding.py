#!/usr/bin/env python3
"""Offline semantic onboarding and queue routing regressions using a strict fake gh."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1] / 'scripts'

FAKE_GH = '''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
state = pathlib.Path(os.environ['FAKE_STATE'])
with (state / 'calls').open('a') as log:
    log.write(json.dumps(args) + '\\n')
if args == ['auth', 'status']:
    pass
elif args[:2] == ['repo', 'view']:
    identity = args[2] if args[2] != '--json' else 'octo/' + pathlib.Path.cwd().name
    if '--jq' in args:
        print(identity + '\\tPRIVATE')
    else:
        print(json.dumps({'nameWithOwner': identity, 'visibility': 'PRIVATE', 'defaultBranchRef': {'name': os.environ.get('FAKE_DEFAULT_BRANCH', 'main')}}))
elif args[:2] == ['api', 'users/octo']:
    print(json.dumps({'type': 'User'}))
elif args[:2] == ['api', 'user']:
    print('octo')
elif args[:2] == ['project', 'list']:
    print('[]')
elif args[:2] == ['project', 'view']:
    print(json.dumps({'number': int(args[2]), 'title': 'Work'}))
elif args == ['api', '--paginate', '--slurp', 'repos/octo/issues/labels'] or args == ['api', '--paginate', '--slurp', 'repos/octo/tools/labels']:
    print(json.dumps([[{'name': name} for name in json.loads((state / 'labels').read_text())]]))
elif args[:2] == ['label', 'create']:
    if os.environ.get('FAIL_LABEL') == args[2]:
        sys.exit('fake label creation failure')
    labels = json.loads((state / 'labels').read_text())
    labels.append(args[2])
    (state / 'labels').write_text(json.dumps(labels))
elif args[:2] == ['issue', 'list']:
    assert args[args.index('--repo')+1] == 'octo/issues'
    assert args[args.index('--state')+1] == 'open'
    assert 'project:work' in args
    print('42\\thttps://github.com/octo/issues/issues/42')
else:
    sys.exit('unexpected fake provider call: ' + repr(args))
'''


class Onboarding(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.workspace = Path(self.temp.name)
        self.state = self.workspace / 'state'
        self.state.mkdir()
        (self.state / 'labels').write_text('[]')
        self.gh = self.workspace / 'gh'
        self.gh.write_text(FAKE_GH)
        self.gh.chmod(0o755)
        self.env = dict(os.environ, PROJECTS_GH_BIN=str(self.gh), FAKE_STATE=str(self.state), PJ_WORKSPACE=str(self.workspace),
                        PATH=str(self.workspace) + ':' + os.environ['PATH'])
        # Keep subprocesses independent of any host token.
        for key in ('GH_TOKEN', 'GITHUB_TOKEN', 'GH_ENTERPRISE_TOKEN', 'GITHUB_ENTERPRISE_TOKEN'):
            self.env.pop(key, None)
        self.tools = self.checkout('tools')
        self.issues = self.checkout('issues')

    def checkout(self, name):
        root = self.workspace / name
        root.mkdir()
        self.git(root, 'init', '-q', '-b', 'main')
        self.git(root, 'config', 'user.name', 'Fixture')
        self.git(root, 'config', 'user.email', 'fixture@example.invalid')
        self.git(root, 'remote', 'add', 'origin', f'https://github.com/octo/{name}.git')
        (root / 'AGENTS.md').write_text('Preserve this guidance.\n')
        (root / 'unrelated').write_text('untouched\n')
        self.commit(root)
        return root

    def git(self, root, *args):
        return subprocess.check_output(['git', '-C', str(root), *args], text=True, stderr=subprocess.DEVNULL).strip()

    def commit(self, root):
        self.git(root, 'add', '.')
        self.git(root, 'commit', '-qm', 'fixture state')

    def run_init(self, *args, central=False, yes=True, input='', env=None):
        flags = ['--project', 'work', '--project-owner', 'octo', '--project-number', '40']
        if central:
            flags += ['--issue-store', 'octo/issues']
        if yes:
            flags += ['--yes']
        return subprocess.run(['bash', str(SCRIPTS / 'init-project.sh'), *flags, *args], cwd=self.tools,
                              env=env or self.env, text=True, input=input, capture_output=True)

    def success(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def contracts(self):
        return {str(path.relative_to(self.workspace)): path.read_bytes()
                for root in (self.tools, self.issues) for path in root.rglob('*')
                if path.is_file() and '.git' not in path.parts}

    def test_natural_issue_repository(self):
        self.success(self.run_init())
        contract = (self.tools / '.projects/project.md').read_text()
        self.assertIn('| Issue repository | octo/tools |', contract)
        self.assertIn('| Routing | Project membership; no routing label |', contract)
        self.assertFalse((self.issues / '.projects').exists())
        self.assertEqual(json.loads((self.state / 'labels').read_text()), [])
        self.assertEqual(self.git(self.tools, 'branch', '--show-current'), 'onboarding/work')

    def test_custom_default_branch_and_unrelated_staged_work(self):
        self.git(self.tools, 'branch', '-m', 'trunk')
        (self.tools / 'unrelated').write_text('staged work\n')
        self.git(self.tools, 'add', 'unrelated')
        self.success(self.run_init(env=dict(self.env, FAKE_DEFAULT_BRANCH='trunk')))
        self.assertEqual(self.git(self.tools, 'branch', '--show-current'), 'onboarding/work')
        self.assertEqual(self.git(self.tools, 'diff', '--cached', '--name-only'), 'unrelated')
        self.assertEqual((self.tools / 'unrelated').read_text(), 'staged work\n')

    def test_central_project_route(self):
        self.success(self.run_init(central=True))
        local = (self.tools / '.projects/project.md').read_text()
        dispatcher = (self.issues / '.projects/project.md').read_text()
        child = (self.issues / '.projects/projects/work.md').read_text()
        self.assertIn('| work | project:work | 40 | .projects/projects/work.md |', dispatcher)
        for text in (local, child):
            self.assertIn('| Issue repository | octo/issues |', text)
            self.assertIn('| Routing | label:project:work |', text)
        self.assertIn('| Implementation repository | octo/tools |', child)
        self.assertEqual(json.loads((self.state / 'labels').read_text()), ['project:work'])
        for root in (self.tools, self.issues):
            self.assertIn('Preserve this guidance.', (root / 'AGENTS.md').read_text())
            self.assertEqual(self.git(root, 'branch', '--show-current'), 'onboarding/work')
            self.assertEqual((root / 'unrelated').read_text(), 'untouched\n')

    def test_central_subproject_grouping(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        child = (self.issues / '.projects/projects/work.md').read_text()
        self.assertIn('| tools | subproject:tools | octo/tools |', child)
        self.assertIn('| tools | subproject:tools |', (self.tools / '.projects/project.md').read_text())
        self.assertEqual(set(json.loads((self.state / 'labels').read_text())), {'project:work', 'subproject:tools'})

    def test_repeated_onboarding_noop(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        before = self.contracts()
        (self.state / 'calls').write_text('')
        result = self.run_init('--subproject', 'tools', central=True, yes=False)
        self.success(result)
        self.assertIn('No change needed', result.stdout)
        self.assertEqual(before, self.contracts())
        self.assertNotIn('"create"', (self.state / 'calls').read_text())

    def test_conflicting_local_contract_no_partial_rewrite(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        path = self.tools / '.projects/project.md'
        path.write_text(path.read_text().replace('| Project number | 40 |', '| Project number | 41 |'))
        for root in (self.tools, self.issues):
            self.commit(root)
        before = self.contracts()
        result = self.run_init('--subproject', 'tools', central=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('conflicting Project number', result.stderr)
        self.assertEqual(before, self.contracts())

    def test_conflicting_dispatcher_no_partial_rewrite(self):
        self.success(self.run_init(central=True))
        path = self.issues / '.projects/project.md'
        path.write_text(path.read_text().replace('| work | project:work | 40 |', '| work | project:other | 40 |'))
        self.commit(self.issues)
        before = self.contracts()
        result = self.run_init(central=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('disagrees', result.stderr)
        self.assertEqual(before, self.contracts())

    def test_cross_checkout_confirmation_cancel(self):
        before = self.contracts()
        result = self.run_init('--subproject', 'tools', central=True, yes=False, input='n\n')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(str(self.issues / '.projects/project.md'), result.stdout)
        self.assertIn('subproject:tools', result.stdout)
        self.assertEqual(before, self.contracts())
        self.assertEqual(json.loads((self.state / 'labels').read_text()), [])
        self.assertEqual(self.git(self.issues, 'branch', '--show-current'), 'main')

    def test_missing_store_fails_closed(self):
        self.git(self.issues, 'remote', 'remove', 'origin')
        result = self.run_init(central=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('exactly one local checkout', result.stderr)
        self.assertFalse((self.tools / '.projects').exists())

    def test_provider_failure_leaves_both_contracts_unchanged(self):
        before = self.contracts()
        result = self.run_init('--subproject', 'tools', central=True,
                               env=dict(self.env, FAIL_LABEL='subproject:tools'))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Provider partial failure', result.stderr)
        self.assertEqual(before, self.contracts())
        for root in (self.tools, self.issues):
            self.assertEqual(self.git(root, 'branch', '--show-current'), 'main')

    def test_subproject_collision_fails_closed(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        other = self.checkout('other')
        before = self.contracts()
        result = subprocess.run(['python3', str(SCRIPTS / 'onboard-project.py'), '--project', 'work',
                                 '--issue-store', 'octo/issues', '--subproject', 'tools', '--yes'],
                                cwd=other, env=self.env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('conflicting implementation repository', result.stderr)
        self.assertEqual(before, self.contracts())
        self.assertFalse((other / '.projects').exists())

    def test_add_subproject_preserves_existing_bindings(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        self.commit(self.issues)
        other = self.checkout('other')
        result = subprocess.run(['python3', str(SCRIPTS / 'onboard-project.py'), '--project', 'work',
                                 '--issue-store', 'octo/issues', '--subproject', 'other', '--yes'],
                                cwd=other, env=self.env, text=True, capture_output=True)
        self.success(result)
        child = (self.issues / '.projects/projects/work.md').read_text()
        self.assertIn('| tools | subproject:tools | octo/tools |', child)
        self.assertIn('| other | subproject:other | octo/other |', child)
        self.assertEqual(child.count('| Project key | work |'), 1)

    def test_legacy_membership_mirror_adopts_central_route(self):
        self.success(self.run_init('--governance', 'personal'))
        path = self.tools / '.projects/project.md'
        text = path.read_text().replace('| Issue repository | octo/tools |', '| Issue repository | octo/issues |')
        text = text.replace('| Project key | work |\n', '')
        path.write_text(text)
        self.commit(self.tools)
        # The no-option initializer must also reconcile an existing separate store.
        result = subprocess.run(['bash', str(SCRIPTS / 'init-project.sh')], cwd=self.tools,
                                env=self.env, input='y\n', text=True, capture_output=True)
        self.success(result)
        self.assertIn('| Routing | label:project:work |', path.read_text())
        self.assertIn('| Governance | personal |', path.read_text())
        self.assertIn('| Governance | personal |', (self.issues / '.projects/projects/work.md').read_text())

    def test_matching_single_store_becomes_dispatcher(self):
        self.success(self.run_init())
        text = (self.tools / '.projects/project.md').read_text()
        text = text.replace('| Issue repository | octo/tools |', '| Issue repository | octo/issues |')
        text = text.replace('| Implementation repository | octo/tools |\n', '')
        text += '\n## Local preservation rule\n\nKeep this exact override.\n'
        directory = self.issues / '.projects'
        directory.mkdir()
        (directory / 'project.md').write_text(text)
        self.commit(self.issues)
        # A new implementation checkout has no contradictory natural-issue contract.
        other = self.checkout('other')
        result = subprocess.run(['python3', str(SCRIPTS / 'onboard-project.py'), '--project', 'work',
                                 '--issue-store', 'octo/issues', '--yes'], cwd=other,
                                env=self.env, text=True, capture_output=True)
        self.success(result)
        child = (directory / 'projects/work.md').read_text()
        self.assertIn('Keep this exact override.', child)
        self.assertIn('| Mode | project |', child)
        self.assertIn('| Implementation repository | octo/other |', child)
        self.assertIn('| Mode | dispatcher |', (directory / 'project.md').read_text())

    def test_preserves_fenced_examples_and_file_modes(self):
        self.success(self.run_init())
        path = self.tools / '.projects/project.md'
        example = '```markdown\n| Contract version | 1 |\n| Project number | 999 |\n```\n'
        text = path.read_text().replace('| Project key | work |', '| Project key | |').replace('| Implementation repository | octo/tools |', '| Implementation repository | |')
        path.write_text(example + text)
        path.chmod(0o640)
        self.commit(self.tools)
        self.success(self.run_init('--subproject', 'tools'))
        self.assertTrue(path.read_text().startswith(example))
        self.assertEqual(path.stat().st_mode & 0o777, 0o640)
        result = subprocess.run(['bash', str(SCRIPTS / 'validate-contract.sh'), str(self.tools)], text=True, capture_output=True)
        self.success(result)

    def test_local_write_failure_restores_both_checkouts(self):
        spec = importlib.util.spec_from_file_location('onboard', SCRIPTS / 'onboard-project.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        before = self.contracts()
        write = Path.write_bytes
        failed = False

        def fail_once(path, content):
            nonlocal failed
            if path == self.tools / '.projects/project.md' and not failed:
                failed = True
                raise OSError('fixture local write failure')
            return write(path, content)

        previous = Path.cwd()
        try:
            os.chdir(self.tools)
            with mock.patch.dict(os.environ, self.env, clear=True), mock.patch('sys.argv', [
                    'onboard', '--project', 'work', '--issue-store', 'octo/issues',
                    '--project-owner', 'octo', '--project-number', '40', '--yes']), \
                    mock.patch.object(Path, 'write_bytes', fail_once):
                with self.assertRaisesRegex(OSError, 'fixture local write failure'):
                    module.main()
        finally:
            os.chdir(previous)
        self.assertTrue(failed)
        self.assertEqual(before, self.contracts())
        for root in (self.tools, self.issues):
            self.assertEqual(self.git(root, 'branch', '--show-current'), 'main')

    def test_semantic_queue_resolves_implementation_without_repo(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        launcher = os.environ.get('PJ_TEST_LAUNCHER')
        if launcher:
            args = ['bash', launcher, '-i', '--project', 'work', '--subproject', 'tools', '--agent=before']
            agent = self.workspace / 'codex'
            agent.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n')
            agent.chmod(0o755)
            # pj expects a gh command as well as the canonical provider override.
            bin_dir = self.workspace / 'bin'
            bin_dir.mkdir()
            (bin_dir / 'gh').symlink_to(self.gh)
            (bin_dir / 'codex').symlink_to(agent)
            env = dict(self.env, PATH=str(bin_dir) + ':' + os.environ['PATH'], PJ_BACKEND='codex',
                       PJ_QUEUE_PREFLIGHT_SCRIPT=str(SCRIPTS / 'queue-preflight.sh'))
        else:
            args = ['bash', str(SCRIPTS / 'queue-preflight.sh'), '--workspace', str(self.workspace),
                    '--project', 'work', '--subproject', 'tools']
            env = self.env
        result = subprocess.run(args, cwd=self.tools, env=env, text=True, capture_output=True)
        self.success(result)
        self.assertIn('octo/issues', result.stdout)
        self.assertIn(str(self.tools / '.projects/project.md'), result.stdout)
        self.assertNotIn(str(self.issues / '.projects/projects/work.md'), result.stdout)
        calls = [json.loads(line) for line in (self.state / 'calls').read_text().splitlines()]
        issue_reads = [call for call in calls if call[:2] == ['issue', 'list']]
        self.assertEqual(len(issue_reads), 1)
        self.assertIn('subproject:tools', issue_reads[0])

    def test_broad_queue_keeps_central_implementation_vocabulary(self):
        self.success(self.run_init(central=True))
        for root in (self.tools, self.issues):
            self.commit(root)
        self.success(self.run_init('--subproject', 'tools', central=True))
        result = subprocess.run(['bash', str(SCRIPTS / 'queue-preflight.sh'), '--workspace', str(self.workspace),
                                 '--project', 'work'], env=self.env, text=True, capture_output=True)
        self.success(result)
        self.assertIn(str(self.issues / '.projects/projects/work.md'), result.stdout)
        self.assertNotIn(str(self.tools / '.projects/project.md'), result.stdout)

    def test_queue_refuses_inconsistent_mirror(self):
        self.success(self.run_init('--subproject', 'tools', central=True))
        path = self.tools / '.projects/project.md'
        path.write_text(path.read_text().replace('| Project number | 40 |', '| Project number | 41 |'))
        result = subprocess.run(['bash', str(SCRIPTS / 'queue-preflight.sh'), '--workspace', str(self.workspace),
                                 '--project', 'work', '--subproject', 'tools'], env=self.env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('disagrees on Project number', result.stderr)


if __name__ == '__main__':
    unittest.main()

#!/usr/bin/env python3
"""Reconcile semantic Project intent across local checkouts, without publishing."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent


def command(*args, cwd=None):
    result = subprocess.run(args, cwd=cwd, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or f'command failed: {args[0]}')
    return result.stdout.strip()


def visible_lines(text):
    fence = None
    for index, line in enumerate(text.splitlines(keepends=True)):
        match = re.match(r'^\s*(`{3,}|~{3,})(.*)$', line.rstrip('\n'))
        if fence:
            if match and match[1][0] == fence[0] and len(match[1]) >= len(fence) and not match[2].strip():
                fence = None
            continue
        if match:
            fence = match[1]
            continue
        yield index, line


def rows(text, section=None):
    active = section is None
    for _, line in visible_lines(text):
        if section and line.startswith('## '):
            active = line.strip() == f'## {section}'
        if active and line.startswith('|'):
            cells = [part.strip() for part in line.split('|')[1:-1]]
            if len(cells) >= 2:
                yield cells


def value(text, key):
    matches = [row[1] for row in rows(text) if row[0] == key]
    if len(matches) > 1:
        raise RuntimeError(f'conflicting duplicate metadata: {key}')
    return matches[0] if matches else ''


def metadata(text, key, wanted):
    existing = value(text, key)
    if existing and existing != wanted:
        raise RuntimeError(f'conflicting {key}: {existing!r}; requested {wanted!r}')
    if existing:
        return text
    # Match the actual metadata table, never a fenced example.
    lines = text.splitlines(keepends=True)
    for index, line in visible_lines(text):
        if line.startswith('|') and line.split('|')[1].strip() == key:
            lines[index] = f'| {key} | {wanted} |\n'
            return ''.join(lines)
    insertion = next(i for i, line in visible_lines(text)
                     if line.startswith('|') and line.split('|')[1].strip() == 'Contract version')
    lines.insert(insertion + 1, f'| {key} | {wanted} |\n')
    return ''.join(lines)


def append_row(text, section, row):
    lines = text.splitlines(keepends=True)
    start = next(i for i, line in visible_lines(text) if line.strip() == f'## {section}')
    end = next((i for i, line in visible_lines(text) if i > start and line.startswith('## ')), len(lines))
    lines.insert(end, row + '\n')
    return ''.join(lines)


def subproject(text, key, implementation=''):
    if not key:
        return text
    existing = [row for row in rows(text, 'Sub-project vocabulary') if row[0] == key]
    if existing:
        if len(existing) != 1 or existing[0][1] != f'subproject:{key}':
            raise RuntimeError(f'conflicting sub-project {key}')
        if implementation:
            header = next(rows(text, 'Sub-project vocabulary'))
            if 'Implementation repository' not in header:
                raise RuntimeError(f'sub-project {key} has no implementation identity; review existing topology')
            index = header.index('Implementation repository')
            if len(existing[0]) <= index or existing[0][index] != implementation:
                raise RuntimeError(f'conflicting implementation repository for sub-project {key}')
        return text
    if not any(line.strip() == '## Sub-project vocabulary' for _, line in visible_lines(text)):
        text += '\n## Sub-project vocabulary\n\n| Key | Label | Implementation repository |\n| --- | --- | --- |\n'
    header = next(rows(text, 'Sub-project vocabulary'))
    cells = [''] * len(header)
    cells[0:2] = [key, f'subproject:{key}']
    if implementation:
        if 'Implementation repository' not in header:
            # Keep existing Purpose cells while adding the implementation binding.
            lines = text.splitlines(keepends=True)
            active = False
            for index, line in visible_lines(text):
                if line.startswith('## '):
                    active = line.strip() == '## Sub-project vocabulary'
                if active and line.startswith('|'):
                    first = line.split('|')[1].strip()
                    new_cell = 'Implementation repository' if first == 'Key' else '---' if first == '---' else ''
                    lines[index] = line.rstrip() + f' {new_cell} |\n'
            text = ''.join(lines)
            cells.append(implementation)
        else:
            cells[header.index('Implementation repository')] = implementation
    return append_row(text, 'Sub-project vocabulary', '| ' + ' | '.join(cells) + ' |')


def project_contract(key, store, owner, number, title, owner_type, privacy, governance, mode, routing):
    location = 'organization issue field' if owner_type == 'organization' else 'project field'
    class_location, class_name = ('organization issue type', 'Issue Type') if owner_type == 'organization' else ('project field', 'Class')
    return f'''# GitHub Project configuration

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | {mode} |
| Project key | {key} |
| Issue repository | {store} |
| Project owner | {owner} |
| Owner type | {owner_type} |
| Project number | {number} |
| Project title | {title} |
| Routing | {routing} |
| Privacy | {privacy} |
| Governance | {governance} |

## Field locations

| Common dimension | Provider location | Provider field |
| --- | --- | --- |
| Class | {class_location} | {class_name} |
| Priority | {location} | Priority |
| Status | project field | Status |
| Due date | project field | Target date |
'''


POINTER = '''<!-- github-projects:start -->
## GitHub issues and Projects

For GitHub issue or Project administration, use
`.agents/skills/github-projects/SKILL.md` and read
`.projects/project.md` before acting.
<!-- github-projects:end -->
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project', required=True, help='managed Project key')
    parser.add_argument('--issue-store', help='issue repository, default: this repository')
    parser.add_argument('--subproject', default='')
    parser.add_argument('--project-owner')
    parser.add_argument('--project-number', type=int)
    parser.add_argument('--governance', choices=['personal', 'collaborative'])
    parser.add_argument('--workspace', default=os.environ.get('PJ_WORKSPACE', str(Path.home() / 'planning')))
    parser.add_argument('--yes', action='store_true', help='confirm the displayed topology changes')
    args = parser.parse_args()
    for key in (args.project, args.subproject):
        if key and not re.fullmatch('[a-z0-9][a-z0-9-]*', key):
            parser.error('Project and sub-project keys use lowercase letters, numbers and hyphens')
    gh = os.environ.get('PROJECTS_GH_BIN', 'gh')
    root = Path(command('git', 'rev-parse', '--show-toplevel')).resolve()
    command(gh, 'auth', 'status')

    def repo(identity=None, cwd=None):
        flags = [identity] if identity else []
        record = json.loads(command(gh, 'repo', 'view', *flags, '--json', 'nameWithOwner,visibility,defaultBranchRef', cwd=cwd))
        if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', record['nameWithOwner']):
            raise RuntimeError('invalid repository identity from GitHub')
        return record

    current = repo(cwd=root)
    identity = current['nameWithOwner']
    local_before = (root / '.projects/project.md').read_text() if (root / '.projects/project.md').is_file() else ''
    requested_store = args.issue_store or value(local_before, 'Issue repository') or identity
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', requested_store):
        raise RuntimeError('issue store must be OWNER/REPO')
    store_record = repo(requested_store) if requested_store != identity else current
    store = store_record['nameWithOwner']
    central = store != identity
    store_root = root
    if central:
        matches = []
        workspace = Path(args.workspace).resolve()
        for candidate in sorted(workspace.iterdir()):
            if not candidate.is_dir() or candidate.is_symlink():
                continue
            contract = candidate / '.projects/project.md'
            local_identity = ''
            if contract.is_file():
                text = contract.read_text()
                local_identity = value(text, 'Issue repository') if value(text, 'Mode') == 'dispatcher' else value(text, 'Implementation repository')
            if not local_identity:
                result = subprocess.run(['git', '-C', str(candidate), 'remote', 'get-url', 'origin'], text=True, capture_output=True)
                match = re.fullmatch(r'(?:https://github.com/|git@github.com:)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?', result.stdout.strip())
                local_identity = match[1] if match else ''
            if local_identity == store:
                if Path(command('git', 'rev-parse', '--show-toplevel', cwd=candidate)).resolve() != candidate.resolve():
                    raise RuntimeError(f'not a repository root: {candidate}')
                matches.append(candidate.resolve())
        if len(matches) != 1:
            raise RuntimeError(f'issue store {store} needs exactly one local checkout in {workspace}; found {len(matches)}')
        store_root = matches[0]
        if store_root == root:
            raise RuntimeError('issue store identity disagrees with the current repository')
        if repo(cwd=store_root)['nameWithOwner'] != store:
            raise RuntimeError('central checkout identity disagrees with its contract')

    roots = list(dict.fromkeys([root, store_root]))
    originals = {}
    planned = {}
    for checkout in roots:
        projects = checkout / '.projects'
        if projects.is_symlink() or (projects.exists() and not projects.is_dir()):
            raise RuntimeError(f'unsafe path: {projects}')
        if projects.exists():
            for path in projects.rglob('*'):
                if path.is_symlink() or (not path.is_dir() and not path.is_file()):
                    raise RuntimeError(f'unsafe path: {path}')
                if path.is_file():
                    originals[path] = path.read_bytes()
        agents = checkout / 'AGENTS.md'
        if agents.is_symlink() or (agents.exists() and not agents.is_file()):
            raise RuntimeError(f'unsafe path: {agents}')
        if agents.exists():
            originals[agents] = agents.read_bytes()
        if (checkout / '.projects/project.md').exists():
            command('bash', str(HERE / 'validate-contract.sh'), str(checkout))

    def read(path):
        return originals.get(path, b'').decode()

    main_path = root / '.projects/project.md'
    dispatcher_path = store_root / '.projects/project.md'
    child_path = store_root / f'.projects/projects/{args.project}.md'
    dispatcher = read(dispatcher_path) if central else ''
    leaf = read(child_path) if central else read(main_path)
    routes, found = [], []
    if central and dispatcher and value(dispatcher, 'Mode') == 'single':
        if leaf:
            raise RuntimeError('conflicting orphan Project child')
        if value(dispatcher, 'Issue repository') != store:
            raise RuntimeError('conflicting central issue repository')
        # A matching legacy single store becomes the child; preserve all its overrides.
        lines = dispatcher.splitlines(keepends=True)
        for index, line in visible_lines(dispatcher):
            if line.startswith('|') and line.split('|')[1].strip() == 'Mode':
                lines[index] = '| Mode | project |\n'
        leaf = ''.join(lines)
        dispatcher = ''
    if central and dispatcher:
        if value(dispatcher, 'Mode') != 'dispatcher' or value(dispatcher, 'Issue repository') != store:
            raise RuntimeError('conflicting central contract: expected dispatcher for the issue store')
        routes = [row for row in rows(dispatcher, 'Routes') if row[0] not in ('Project key', '---')]
        found = [row for row in routes if row[0] == args.project]
        if found:
            if len(found) != 1 or found[0][1] != f'project:{args.project}':
                raise RuntimeError('conflicting dispatcher route')
            child_path = store_root / found[0][3]
            leaf = read(child_path)
        elif leaf:
            raise RuntimeError('conflicting orphan Project child')
    elif central and read(child_path):
        raise RuntimeError('conflicting orphan Project child')
    local = read(main_path)
    existing = leaf or local
    owner = args.project_owner or value(existing, 'Project owner') or identity.split('/')[0]
    number = args.project_number or value(existing, 'Project number')
    if number is None or number == '':
        number = input('Project number (from its web address): ').strip()
    if not re.fullmatch('[A-Za-z0-9_.-]+', owner) or not re.fullmatch('[1-9][0-9]*', str(number)):
        raise RuntimeError('invalid Project owner or number')
    number = str(number)
    governance = args.governance or value(existing, 'Governance') or value(dispatcher, 'Governance') or 'collaborative'
    owner_record = json.loads(command(gh, 'api', f'users/{owner}'))
    owner_type = {'User': 'user', 'Organization': 'organization'}.get(owner_record['type'])
    if not owner_type:
        raise RuntimeError('unsupported Project owner type')
    project = json.loads(command(gh, 'project', 'view', number, '--owner', owner, '--format', 'json'))
    if str(project['number']) != number:
        raise RuntimeError('GitHub returned a different Project')
    title = project['title']
    if not title or any(char in title for char in '|\n\r'):
        raise RuntimeError('unsafe Project title')
    routing = f'label:project:{args.project}' if central else 'Project membership; no routing label'

    def reconcile(text, mode, privacy):
        if not text:
            return project_contract(args.project, store, owner, number, title, owner_type, privacy, governance, mode, routing)
        for key, wanted in [('Mode', mode), ('Issue repository', store), ('Project owner', owner),
                            ('Project number', number), ('Project title', title)]:
            if value(text, key) != wanted:
                raise RuntimeError(f'conflicting {key} in existing contract')
        if value(text, 'Routing') != routing:
            if value(text, 'Routing').startswith('label:'):
                raise RuntimeError('conflicting Routing in existing contract')
            if central:
                # Adopt a legacy membership-only mirror without changing its other settings.
                old = value(text, 'Routing')
                if old not in ('linked repository', 'Project membership only', 'Project membership; no routing label'):
                    raise RuntimeError('conflicting Routing in existing contract')
                lines = text.splitlines(keepends=True)
                for index, line in visible_lines(text):
                    if line.startswith('|') and line.split('|')[1].strip() == 'Routing':
                        lines[index] = f'| Routing | {routing} |\n'
                text = ''.join(lines)
        text = metadata(text, 'Project key', args.project)
        if value(text, 'Owner type'):
            text = metadata(text, 'Owner type', owner_type)
        if args.governance:
            if not value(text, 'Governance'):
                raise RuntimeError('existing prose governance needs review before an explicit override')
            text = metadata(text, 'Governance', args.governance)
        return text

    local = reconcile(local, 'single', current['visibility'].lower() + ' repository')
    local = metadata(local, 'Implementation repository', identity)
    local = subproject(local, args.subproject)
    if central:
        local = metadata(local, 'Queue source', 'mirror')
        leaf = reconcile(leaf, 'project', store_record['visibility'].lower() + ' repository')
        if args.subproject:
            leaf = subproject(leaf, args.subproject, identity)
        else:
            leaf = metadata(leaf, 'Implementation repository', identity)
        if not dispatcher:
            dispatcher = f'''# GitHub Project dispatcher

| Key | Value |
| --- | --- |
| Contract version | 1 |
| Mode | dispatcher |
| Issue repository | {store} |
| Privacy | {store_record['visibility'].lower()} repository |
| Governance | {governance} |

## Routes

| Project key | Routing label | Project number | Contract |
| --- | --- | --- | --- |
'''
        if not found:
            if any(row[1] == f'project:{args.project}' or row[2] == number for row in routes):
                raise RuntimeError('conflicting dispatcher label or Project number')
            dispatcher = append_row(dispatcher, 'Routes', f'| {args.project} | project:{args.project} | {number} | {child_path.relative_to(store_root)} |')
        planned[dispatcher_path] = dispatcher.encode()
        planned[child_path] = leaf.encode()
    planned[main_path] = local.encode()
    for checkout in roots:
        agents = checkout / 'AGENTS.md'
        text = read(agents)
        if '<!-- github-projects:start -->' not in text and '<!-- github-project-admin:start -->' not in text:
            planned[agents] = (text + ('\n' if text else '') + POINTER).encode()

    for checkout in roots:
        with tempfile.TemporaryDirectory() as directory:
            stage = Path(directory)
            if (checkout / '.projects').exists():
                shutil.copytree(checkout / '.projects', stage / '.projects')
            for path, content in planned.items():
                if path.is_relative_to(checkout) and path.name != 'AGENTS.md':
                    destination = stage / path.relative_to(checkout)
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    destination.write_bytes(content)
            command('bash', str(HERE / 'validate-contract.sh'), str(stage))
    changes = {path: content for path, content in planned.items() if originals.get(path) != content}
    labels = [f'project:{args.project}'] if central else []
    for text in (local, leaf if central else ''):
        for row in rows(text, 'Sub-project vocabulary'):
            if row[0] in ('Key', '---'):
                continue
            if not re.fullmatch('[a-z0-9][a-z0-9-]*', row[0]) or row[1] != f'subproject:{row[0]}':
                raise RuntimeError(f'conflicting sub-project vocabulary: {row[0]}')
            labels.append(row[1])

    def read_labels():
        pages = json.loads(command(gh, 'api', '--paginate', '--slurp', f'repos/{store}/labels'))
        return {entry['name'] for page in pages for entry in page}

    missing = set(labels) - read_labels() if labels else set()
    if not changes and not missing:
        print('No change needed: Project topology and labels already agree.')
        return
    print(f'Project {args.project} ({owner}/#{number})' + (f', sub-project {args.subproject}' if args.subproject else ''))
    print(f'  Issue queue: {store}; implementation: {identity}')
    if central:
        print(f'  Central dispatcher route: {args.project} -> project:{args.project}; mirrored in the implementation contract')
    for path in changes:
        print(f'  Update {path}')
    for label in sorted(missing):
        print(f'  Create label {label} in {store}')
    print('  Keep changes on local branches for each repository\'s normal review/PR workflow; no automatic push.')
    if central and not args.yes:
        if input('Apply these changes to both checkouts? [y/N]: ').strip().lower() not in ('y', 'yes'):
            raise RuntimeError('onboarding cancelled; no changes written')
    def check_inputs():
        current_inputs = set()
        for checkout in roots:
            directory = checkout / '.projects'
            if checkout.is_symlink() or directory.is_symlink() or (directory.exists() and not directory.is_dir()):
                raise RuntimeError(f'stale unsafe directory: {directory}')
            if directory.exists():
                for path in directory.rglob('*'):
                    if path.is_symlink() or (not path.is_dir() and not path.is_file()):
                        raise RuntimeError(f'stale unsafe path: {path}')
                    if path.is_file():
                        current_inputs.add(path)
            agents = checkout / 'AGENTS.md'
            if agents.exists() or agents.is_symlink():
                current_inputs.add(agents)
        if current_inputs != set(originals):
            raise RuntimeError('stale onboarding inputs: files changed during confirmation')
        for path, before in originals.items():
            if path.is_symlink() or not path.is_file() or path.read_bytes() != before:
                raise RuntimeError(f'stale onboarding input: {path}')
        for path in changes:
            if path not in originals and (path.exists() or path.is_symlink()):
                raise RuntimeError(f'stale onboarding destination: {path}')
            checkout = next(r for r in roots if path.is_relative_to(r))
            if command('git', 'status', '--porcelain', '--', str(path.relative_to(checkout)), cwd=checkout):
                raise RuntimeError(f'onboarding path has uncommitted changes: {path}; commit or stash it first')

    check_inputs()
    branches, written, created_labels = [], [], []
    try:
        for checkout in roots:
            if not any(path.is_relative_to(checkout) for path in changes):
                continue
            branch = command('git', 'symbolic-ref', '--short', 'HEAD', cwd=checkout)
            result = subprocess.run(['git', 'symbolic-ref', '--short', 'refs/remotes/origin/HEAD'], cwd=checkout, capture_output=True, text=True)
            record = current if checkout == root else store_record
            default = (record.get('defaultBranchRef') or {}).get('name') or result.stdout.strip().removeprefix('origin/')
            if branch in ('main', 'master', default):
                target = f'onboarding/{args.project}'
                command('git', 'switch', '-c', target, cwd=checkout)
                branches.append((checkout, branch, target))
        for label in sorted(missing):
            if label in read_labels():
                continue
            created_labels.append(label)
            command(gh, 'label', 'create', label, '--repo', store, '--color', '5319e7')
            if label not in read_labels():
                raise RuntimeError(f'label readback failed: {label}')
        check_inputs()
        for path, content in changes.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            written.append(path)
            path.write_bytes(content)
        for checkout in roots:
            command('bash', str(HERE / 'validate-contract.sh'), str(checkout))
        for path, content in planned.items():
            if path.read_bytes() != content:
                raise RuntimeError(f'contract readback failed: {path}')
    except BaseException:
        for path in reversed(written):
            if path in originals:
                path.write_bytes(originals[path])
            else:
                path.unlink(missing_ok=True)
        for checkout, branch, target in reversed(branches):
            command('git', 'switch', branch, cwd=checkout)
            command('git', 'branch', '-D', target, cwd=checkout)
        if created_labels:
            print('Provider partial failure: labels may remain: ' + ', '.join(created_labels) + '. Inspect before retrying.', file=sys.stderr)
        raise
    print('Topology and routing labels verified. Review, commit and open a PR in each changed repository.')
    print('Existing issues and Project membership were not migrated. Standard fields/Backlog setup remains available through projects.')


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, EOFError) as error:
        raise SystemExit(f'onboarding: {error}')

#!/usr/bin/env python3
"""Snapshot/plan/apply/verify/rollback a bounded Project storage migration.

Snapshots contain issue state: keep the journal outside public repositories.
No writes occur without --mode apply or rollback and explicit rollout evidence.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import tempfile
from pathlib import Path
from urllib.parse import quote

from project_context import binding, project_identity
from queue_common import gh_json, run, table_value
from subproject import change, inspect


def digest(text):
    return hashlib.sha256(text.encode()).hexdigest()


def save(path, journal):
    # Persist intended writes before sending them. Never overwrite via a symlink.
    if path.is_symlink() or path.parent.is_symlink():
        raise ValueError('unsafe migration journal path')
    fd, name = tempfile.mkstemp(dir=path.parent, prefix=path.name + '.', suffix='.tmp')
    temporary = Path(name)
    try:
        with os.fdopen(fd, 'w') as handle:
            json.dump(journal, handle, indent=2, sort_keys=True)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        if hasattr(os, 'O_DIRECTORY'):
            directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
    finally:
        # A handled disk/write failure removes only this invocation's file.
        # A killed process may leave an orphan; its unique name never prevents
        # a later apply/rollback from saving its own journal state.
        temporary.unlink(missing_ok=True)


def plan(gh, source, target, issues, owned_routes):
    if project_identity(source) != project_identity(target) or table_value(source, 'Issue repository') != table_value(target, 'Issue repository'):
        raise ValueError('migration must keep the same repository and Project identity')
    old, new = binding(source), binding(target)
    if old.storage not in ('labels', 'none') or new.storage not in ('field', 'labels', 'none'):
        raise ValueError('supported migration is legacy labels/membership to a fixed field or scoped label binding')
    if new.storage == 'none' and old.vocabulary:
        raise ValueError('cannot discard a Sub-project vocabulary')
    if set(old.vocabulary) != set(new.vocabulary):
        raise ValueError('migration must preserve every vocabulary key')
    routing = table_value(source, 'Routing')
    if owned_routes and (len(owned_routes) != 1 or routing != 'label:' + owned_routes[0]):
        raise ValueError('routing cleanup may own only the exact legacy route label declared by this contract')
    repository = table_value(target, 'Issue repository')
    entries = []
    for issue in sorted(set(issues)):
        if issue < 1:
            raise ValueError('invalid issue number')
        # The target field is intentionally inspected before cutover. It must
        # already exist with the exact type and requested option vocabulary.
        before = inspect(gh, target, repository, issue)
        selected = [key for key, label in old.vocabulary.items() if label in before['labels']]
        if len(selected) > 1:
            raise ValueError(f'issue {issue} has ambiguous legacy grouping')
        key = selected[0] if selected else None
        desired = new.resolve(key) if key else None
        if before['current'] not in (None, desired):
            raise ValueError(f'issue {issue} has conflicting target grouping; reconcile before migration')
        previous_key = next((k for k, value in new.vocabulary.items() if value == before['current']), None)
        remove = set(owned_routes) & set(before['labels'])
        # A shared legacy grouping may still classify this issue in another
        # Project. A per-Project cutover cannot retire that other consumer.
        if old.storage == 'labels' and (old.scope == 'project' or len(before['memberships']) == 1):
            remove |= set(old.vocabulary.values()) & set(before['labels'])
        remove -= set(new.vocabulary.values()) if new.storage == 'labels' else set()
        entries.append({'issue': issue, 'key': key, 'previousKey': previous_key, 'before': before,
                        'removeLabels': sorted(remove), 'status': 'planned'})
    return {'version': 1, 'project': project_identity(target), 'repository': repository,
            'sourceDigest': digest(source), 'targetDigest': digest(target),
            'ownedRoutingLabels': owned_routes, 'entries': entries, 'phase': 'snapshot'}


def expected_labels(entry, target):
    labels = set(entry['before']['labels']) - set(entry['removeLabels'])
    store = binding(target)
    if store.storage == 'labels':
        labels -= set(store.vocabulary.values())
        if entry['key']:
            labels.add(store.resolve(entry['key']))
    return labels


def check_preservation(entry, state, target, final=True):
    before = entry['before']
    if before['issue'] != state['issue'] or before['memberships'] != state['memberships']:
        raise ValueError('issue or memberships changed; stop migration')
    field_id = (state['field'] or {}).get('id')
    if {k: v for k, v in before['values'].items() if k != field_id} != {k: v for k, v in state['values'].items() if k != field_id}:
        raise ValueError('unrelated Project fields changed; stop migration')
    desired = binding(target).resolve(entry['key']) if entry['key'] else None
    if final and (state['current'] != desired or set(state['labels']) != expected_labels(entry, target)):
        raise ValueError('migration readback mismatch')


def evidence(path, journal):
    value = json.loads(path.read_text())
    required = ('writersPaused', 'consumersMigrated', 'discoveryVerified', 'membershipVerified', 'setupVerified')
    if any(value.get(key) is not True for key in required) or not value.get('deploymentRevision') or not value.get('verificationRecord'):
        raise ValueError('rollout evidence requires paused writers, migrated consumers, verified discovery/membership/setup, deployment revision and verification record')
    if value.get('targetDigest') != journal['targetDigest']:
        raise ValueError('rollout evidence does not identify this target contract')
    return value


def execute(gh, journal, target, path, rollback=False):
    repository = journal['repository']
    for entry in journal['entries']:
        issue = entry['issue']
        state = inspect(gh, target, repository, issue)
        check_preservation(entry, state, target, final=False)
        original_labels = set(entry['before']['labels'])
        permitted_labels = original_labels | expected_labels(entry, target)
        if not set(state['labels']) <= permitted_labels or not original_labels - set(entry['removeLabels']) - set(binding(target).vocabulary.values()) <= set(state['labels']):
            raise ValueError('unrelated labels changed since snapshot')
        desired = binding(target).resolve(entry['key']) if entry['key'] else None
        if state['current'] not in (entry['before']['current'], desired):
            raise ValueError('grouping changed since snapshot')
        entry['status'] = 'rollback_pending' if rollback else 'apply_pending'
        save(path, journal)
        # Retry is based on fresh readback, never the mutation exit status alone.
        if rollback:
            change(gh, target, repository, issue, entry['previousKey'], apply=True)
            fresh = inspect(gh, target, repository, issue)
            check_preservation(entry, fresh, target, final=False)
            fresh_labels = set(fresh['labels'])
            migrated_labels = expected_labels(entry, target)
            migration_created_labels = migrated_labels - original_labels
            if (not fresh_labels.issubset(original_labels | migration_created_labels)
                    or not (original_labels & migrated_labels).issubset(fresh_labels)):
                raise ValueError('unrelated labels changed during rollback; preserving concurrent labels')
            add = original_labels - fresh_labels
            remove = fresh_labels & migration_created_labels
        else:
            change(gh, target, repository, issue, entry['key'], apply=True)
            add, remove = set(), set(entry['removeLabels'])
        for label in sorted(add):
            gh_json(gh, 'api', '--method', 'POST', f'repos/{repository}/issues/{issue}/labels', '-f', 'labels[]=' + label)
        labels = {v['name'] for v in gh_json(gh, 'api', f'repos/{repository}/issues/{issue}')['labels']}
        for label in sorted(remove & labels):
            proc = run(gh, 'api', '--method', 'DELETE', f'repos/{repository}/issues/{issue}/labels/' + quote(label, safe=''))
            if proc.returncode:
                raise RuntimeError('label removal failed or uncertain; journal retained, inspect then retry: ' + proc.stderr)
        after = inspect(gh, target, repository, issue)
        if rollback:
            before = entry['before']
            if after != before:
                raise ValueError('rollback readback differs from complete snapshot')
            entry['status'] = 'rolled_back'
        else:
            check_preservation(entry, after, target)
            entry['status'] = 'verified'
        save(path, journal)
    journal['phase'] = 'rolled_back' if rollback else 'verified'
    save(path, journal)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True)
    parser.add_argument('--target', required=True)
    parser.add_argument('--journal', type=Path, required=True)
    parser.add_argument('--issues', nargs='+', type=int)
    parser.add_argument('--owned-routing-label', action='append', default=[])
    parser.add_argument('--mode', choices=('dry-run', 'snapshot', 'apply', 'verify', 'rollback'), default='dry-run')
    parser.add_argument('--rollout-evidence', type=Path)
    parser.add_argument('--gh', default=os.environ.get('PROJECTS_GH_BIN', 'gh'))
    args = parser.parse_args()
    source, target = Path(args.source).read_text(), Path(args.target).read_text()
    if args.mode in ('dry-run', 'snapshot'):
        if not args.issues:
            parser.error('--issues must explicitly bound the migration')
        journal = plan(args.gh, source, target, args.issues, args.owned_routing_label)
        if args.mode == 'snapshot':
            if args.journal.exists():
                raise ValueError('snapshot already exists; verify/resume it or choose a new journal')
            save(args.journal, journal)
        print(json.dumps(journal, indent=2))
        return
    journal = json.loads(args.journal.read_text())
    if journal.get('version') != 1 or journal['sourceDigest'] != digest(source) or journal['targetDigest'] != digest(target):
        raise ValueError('contract changed since snapshot; journal cannot be reused')
    if args.mode == 'verify':
        for entry in journal['entries']:
            state = inspect(args.gh, target, journal['repository'], entry['issue'])
            if journal['phase'] == 'rolled_back':
                if state != entry['before']:
                    raise ValueError('rollback no longer matches snapshot')
            else:
                check_preservation(entry, state, target)
        print(json.dumps({'verified': True, 'phase': journal['phase']}))
        return
    if not args.rollout_evidence:
        parser.error('apply/rollback requires --rollout-evidence; snapshot is not authority to cut over')
    journal['rolloutEvidence'] = evidence(args.rollout_evidence, journal)
    save(args.journal, journal)
    execute(args.gh, journal, target, args.journal, rollback=args.mode == 'rollback')
    print(json.dumps({'verified': True, 'phase': journal['phase']}))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, KeyError, OSError) as error:
        raise SystemExit(f'migration: {error}')

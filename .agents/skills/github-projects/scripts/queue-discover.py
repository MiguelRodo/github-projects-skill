#!/usr/bin/env python3
"""Discover queued issues by membership or authenticated explicit placement intent."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from project_context import authority_payload, memberships, project_identity, select_context, single_request
from queue_common import flatten_pages, gh_json, table_value
from subproject import inspect


def discover(gh, scopes, explicit=False):
    login = gh_json(gh, 'api', 'user')['login']
    by_repository = {}
    for scope in scopes:
        by_repository.setdefault(scope[0], []).append(scope)
    candidates = []
    for repository, configured in by_repository.items():
        issues = {}
        for queue in sorted({v[1] for v in configured}):
            found = gh_json(gh, 'issue', 'list', '--repo', repository, '--state', 'open',
                            '--label', queue, '--limit', '1000', '--json', 'number,url,labels')
            if len(found) >= 1000:
                raise ValueError(f'queue for {repository} reaches the 1000 issue bound; refine scope')
            for issue in found:
                issues[issue['number']] = issue
        texts = {v[7]: Path(v[7]).read_text() for v in configured}
        managed = {project_identity(t) for t in texts.values()}
        for number, issue in issues.items():
            comments = flatten_pages(gh_json(gh, 'api', '--paginate', '--slurp',
                                    f'repos/{repository}/issues/{number}/comments?per_page=100'))
            latest = single_request(comments, login)
            intent = authority_payload(latest) if latest else None
            if intent:
                target = intent['spec']['target']
                if target.get('repository', '').lower() != repository.lower() or target.get('issue') != number:
                    raise ValueError(f'{repository}#{number}: authority target mismatch')
                identity = (target['project']['owner'].lower(), target['project']['number'])
                if identity not in managed:
                    # A narrow run need not consume another managed Project's request.
                    continue
                if latest.get('created_at') != latest.get('updated_at'):
                    raise ValueError(f'{repository}#{number}: authority comment was edited')
            live = memberships(gh, repository, number)
            requested = next(iter(managed)) if explicit and len(managed) == 1 else None
            try:
                selected = select_context(managed, live, requested, intent)
            except ValueError as error:
                raise ValueError(f'{repository}#{number}: {error}') from error
            matches = [v for v in configured if project_identity(texts[v[7]]) == selected]
            if len(matches) != 1:
                raise ValueError(f'{repository}#{number}: duplicate managed Project context')
            repository, queue, route, sub_value, identity, sub_key, root, contract = matches[0]
            if queue not in {v['name'] for v in issue['labels']}:
                continue
            if sub_key != '-':
                # A set intent can select a not-yet-written grouping, including an
                # unplaced issue; otherwise read its authoritative storage.
                wanted = [a for a in (intent or {}).get('spec', {}).get('actions', [])
                          if a.get('kind') == 'subproject.value.set']
                if wanted:
                    if len(wanted) != 1 or wanted[0].get('value') != sub_key:
                        continue
                elif inspect(gh, texts[contract], repository, number)['current'] != sub_value:
                    continue
            candidates.append([repository, str(number), issue['url'], identity, sub_key, root, contract])
    if len(candidates) > 200:
        raise ValueError('more than 200 queue candidates; refine selectors')
    return candidates


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--gh', required=True)
    parser.add_argument('--scopes', required=True)
    parser.add_argument('--explicit-project', action='store_true')
    args = parser.parse_args()
    scopes = [line.split('\t') for line in Path(args.scopes).read_text().splitlines()]
    candidates = discover(args.gh, scopes, args.explicit_project)
    print('status\t' + ('ready' if candidates else 'empty'))
    for candidate in candidates:
        if any('\n' in value or '\r' in value or '\t' in value for value in candidate):
            raise ValueError('unsafe queue candidate')
        print('candidate\t' + '\t'.join(candidate))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, OSError, KeyError, TypeError) as error:
        raise SystemExit(f'queue preflight: {error}')

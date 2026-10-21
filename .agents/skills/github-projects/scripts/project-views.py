#!/usr/bin/env python3
"""Create overall or filtered Sub-project table views; preserve every existing view."""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
from pathlib import Path

from project_context import binding, graphql, project_identity
from queue_common import flatten_pages, gh_json, table_value


def spec(text, fields, owner_type, key=None):
    store = binding(text)
    by_name = {f['name']: f for f in fields}
    if len(by_name) != len(fields):
        raise ValueError('ambiguous Project field names')
    visible = []
    for name in ['Title', 'Due date', 'Target date', 'Priority'] + (['Class'] if owner_type == 'user' or 'Class' in by_name else []) + ['Sub-issues progress', 'Status', 'Assignees', 'Linked pull requests']:
        if name in by_name:
            visible.append(by_name[name]['id'])
        elif name in ['Title', 'Priority', 'Status', 'Class']:
            raise ValueError('missing required view field: ' + name)
    if store.storage == 'field' and (store.field not in by_name or by_name[store.field]['data_type'].lower() != 'single_select'):
        raise ValueError('expected Sub-project single-select field missing or changed')
    filter_text = ''
    classification = store.field if store.storage == 'field' else 'Labels'
    if key:
        value = store.resolve(key)
        if classification not in by_name:
            raise ValueError('classification field missing')
        if store.storage == 'field':
            field = by_name[classification]
            if field['data_type'].lower() != 'single_select':
                raise ValueError('expected Sub-project field type changed')
            qualifier = table_value(text, 'Sub-project filter qualifier') or classification.lower()
            if not re.fullmatch(r'[a-z0-9_-]+', qualifier):
                raise ValueError('configure the exact Sub-project filter qualifier for a field with spaces')
            filter_text = qualifier + ':' + json.dumps(value)
        else:
            filter_text = 'label:' + json.dumps(value)
    elif store.storage != 'none':
        if classification not in by_name:
            raise ValueError('classification field missing; no silent fallback')
        visible.append(by_name[classification]['id'])
    return {'layout': 'table', 'filter': filter_text, 'visible_fields': visible,
            'group_by': [by_name['Status']['id']], 'sort_by': [[by_name['Priority']['id'], 'asc']]}


def read_views(gh, owner, number, owner_type):
    root = 'user' if owner_type == 'user' else 'organization'
    data = graphql(gh, '''query($owner:String!,$number:Int!) { ROOT(login:$owner) {
      projectV2(number:$number) { number views(first:100) { nodes {
        id name layout filter
        configuration { visibleFields(first:100) { nodes { ... on ProjectV2FieldCommon { id name } } pageInfo { hasNextPage } } }
        groupByFields(first:10) { nodes { ... on ProjectV2FieldCommon { id name } } pageInfo { hasNextPage } }
        sortByFields(first:10) { nodes { direction field { ... on ProjectV2FieldCommon { id name } } } pageInfo { hasNextPage } }
      } pageInfo { hasNextPage } } }
    } }'''.replace('ROOT', root), owner=owner, number=number)
    project = (data.get(root) or {}).get('projectV2')
    if not project or project['number'] != number or project['views']['pageInfo']['hasNextPage']:
        raise RuntimeError('inaccessible or incomplete Project views')
    views = project['views']['nodes']
    for view in views:
        connections = [view['configuration']['visibleFields'], view['groupByFields'], view['sortByFields']]
        if any(c['pageInfo']['hasNextPage'] for c in connections):
            raise RuntimeError('incomplete view configuration')
    return views


def matches(view, body, fields):
    ids = {f['id']: f['node_id'] for f in fields}
    return (view['layout'] == 'TABLE_LAYOUT' and (view['filter'] or '') == body['filter']
            and {f['id'] for f in view['configuration']['visibleFields']['nodes']} == {ids[i] for i in body['visible_fields']}
            and [f['id'] for f in view['groupByFields']['nodes']] == [ids[i] for i in body['group_by']]
            and [(f['field']['id'], f['direction'].lower()) for f in view['sortByFields']['nodes']]
                == [(ids[i], direction) for i, direction in body['sort_by']])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', required=True)
    parser.add_argument('--project-number', type=int, required=True)
    parser.add_argument('--name', help='defaults to the Sub-project key for filtered views, otherwise all')
    parser.add_argument('--subproject')
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--gh', default=os.environ.get('PROJECTS_GH_BIN', 'gh'))
    args = parser.parse_args()
    if args.name is None:
        args.name = args.subproject or 'all'
    text = Path(args.contract).read_text()
    owner, number = project_identity(text)
    if number != args.project_number:
        parser.error('explicit Project assertion disagrees with contract')
    owner_type = table_value(text, 'Owner type') or {'User': 'user', 'Organization': 'organization'}[gh_json(args.gh, 'api', 'users/' + owner)['type']]
    endpoint = ('users/' if owner_type == 'user' else 'orgs/') + f'{owner}/projectsV2/{number}'
    fields = flatten_pages(gh_json(args.gh, 'api', '--paginate', '--slurp', endpoint + '/fields?per_page=100'))
    store = binding(text)
    if store.storage == 'field':
        definitions = gh_json(args.gh, 'project', 'field-list', str(number), '--owner', owner, '--format', 'json', '--limit', '100')
        if definitions.get('totalCount', 0) > len(definitions.get('fields', [])):
            raise ValueError('incomplete Project field definitions')
        field_matches = [f for f in definitions.get('fields', []) if f['name'] == store.field]
        if len(field_matches) != 1 or field_matches[0].get('type') not in ('ProjectV2SingleSelectField', 'SINGLE_SELECT'):
            raise ValueError('expected Sub-project field missing or changed')
        options = [o['name'] for o in field_matches[0].get('options', [])]
        if any(options.count(value) != 1 for value in store.vocabulary.values()):
            raise ValueError('Sub-project option mapping missing or ambiguous')
    body = dict(spec(text, fields, owner_type, args.subproject), name=args.name)
    views = read_views(args.gh, owner, number, owner_type)
    existing = [v for v in views if v['name'] == args.name]
    manual = [
        'Verify browser order: Title, Due date, Target date, Priority, Class (or native Type), Sub-issues progress, Status, Assignees, Linked pull requests; overall views also show Sub-project, filtered subproject views hide it.',
        'Set/verify browser widths in pixels: Title 517; Due date 131; Target date 157; Priority 130; Class/Type 116; Sub-issues progress 174; Status 128; Assignees 135; Linked pull requests 154. The supported API does not set or verify widths or column order.',
    ]
    if args.subproject:
        manual.append('Verify visible items match the exact Sub-project classification in the browser.')
    if owner_type == 'organization':
        manual.append('Show native Type in the browser. Public API may reject or omit this column; verify it manually.')
    if existing:
        if len(existing) != 1 or not matches(existing[0], body, fields):
            raise ValueError('existing view preserved; choose a new name or reconcile its settings explicitly in the browser')
        print(json.dumps({'action': 'no_change', 'verified': True, 'manualVerification': manual}))
        return
    if not args.apply:
        print(json.dumps({'action': 'create_view', 'body': body, 'manualVerification': manual}))
        return
    if read_views(args.gh, owner, number, owner_type) != views:
        raise RuntimeError('stale view state; nothing written')
    proc = subprocess.run([args.gh, 'api', '--method', 'POST', '-H', 'Accept: application/vnd.github+json',
                           '-H', 'X-GitHub-Api-Version: 2026-03-10', endpoint + '/views', '--input', '-'],
                          input=json.dumps(body), text=True, capture_output=True)
    if proc.returncode:
        raise RuntimeError('view create failed or uncertain; inspect before retry: ' + proc.stderr)
    after = read_views(args.gh, owner, number, owner_type)
    created = [v for v in after if v['name'] == args.name]
    if len(created) != 1 or not matches(created[0], body, fields) or not all(v in after for v in views):
        raise RuntimeError('view readback failed; preserve state and inspect before retry')
    print(json.dumps({'action': 'create_view', 'verified': True, 'viewId': created[0]['id'], 'manualVerification': manual}))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, KeyError, OSError) as error:
        raise SystemExit(f'project views: {error}')

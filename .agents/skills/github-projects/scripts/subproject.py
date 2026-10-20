#!/usr/bin/env python3
"""Plan, set, clear or select a contract-bound Sub-project through documented gh APIs."""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from urllib.parse import quote

from project_context import binding, graphql, memberships, project_identity
from queue_common import flatten_pages, gh_json, table_value


def inspect(gh: str, text: str, repository: str, issue: int) -> dict:
    owner, number = project_identity(text)
    all_memberships = memberships(gh, repository, issue)
    selected = [v for v in all_memberships if (v['owner'], v['number']) == (owner, number)]
    if len(selected) != 1:
        raise ValueError('exact target Project membership required; add membership explicitly first')
    member = selected[0]
    schema = graphql(gh, '''query($id:ID!) { node(id:$id) { ... on ProjectV2 {
      id fields(first:100) { nodes {
        ... on ProjectV2FieldCommon { id name dataType }
        ... on ProjectV2SingleSelectField { options { id name } }
      } pageInfo { hasNextPage } }
    } } }''', id=member['projectId'])['node']
    if not schema or schema['fields']['pageInfo']['hasNextPage']:
        raise RuntimeError('incomplete or inaccessible Project field definitions')
    store = binding(text)
    if store.storage == 'labels' and not store.scope and len(all_memberships) > 1:
        raise ValueError('legacy label binding with multiple memberships requires declared shared or project scope')
    matches = [f for f in schema['fields']['nodes'] if f.get('name') == store.field]
    field = None
    if store.storage == 'field':
        if len(matches) != 1 or matches[0].get('dataType') != 'SINGLE_SELECT':
            raise ValueError('expected Sub-project single-select field missing or changed; no label fallback')
        field = matches[0]
        options = field.get('options', [])
        for name in store.vocabulary.values():
            if sum(o['name'] == name for o in options) != 1:
                raise ValueError(f'expected Sub-project option missing or ambiguous: {name}')
    elif matches and store.storage == 'labels':
        # A new field cannot become a second writable representation.
        raise ValueError('Sub-project field now exists; migrate label storage before authority cutover')
    node = graphql(gh, '''query($id:ID!) { node(id:$id) { ... on ProjectV2Item {
      id fieldValues(first:100) { nodes {
        ... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { id name } } }
        ... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { id name } } }
        ... on ProjectV2ItemFieldDateValue { date field { ... on ProjectV2FieldCommon { id name } } }
        ... on ProjectV2ItemFieldNumberValue { number field { ... on ProjectV2FieldCommon { id name } } }
      } pageInfo { hasNextPage } }
    } } }''', id=member['itemId'])['node']
    if not node or node['fieldValues']['pageInfo']['hasNextPage']:
        raise RuntimeError('incomplete or inaccessible Project item values')
    values = {v['field']['id']: v for v in node['fieldValues']['nodes'] if v.get('field')}
    live = gh_json(gh, 'api', f'repos/{repository}/issues/{issue}')
    labels = sorted(v['name'] for v in live['labels'])
    current = values.get(field['id'], {}).get('name') if field else None
    if not field:
        owned = set(store.vocabulary.values()) & set(labels)
        if len(owned) > 1:
            raise ValueError('multiple Sub-project labels in this binding; resolve explicitly before writing')
        current = next(iter(owned), None)
    return {'membership': member, 'field': field, 'values': values, 'labels': labels,
            'current': current, 'issue': {k: live.get(k) for k in
              ('id', 'title', 'body', 'state', 'assignees', 'milestone')},
            'memberships': memberships(gh, repository, issue)}


def change(gh: str, text: str, repository: str, issue: int, key: str | None, apply=False) -> dict:
    store = binding(text)
    desired = store.resolve(key) if key is not None else None
    before = inspect(gh, text, repository, issue)
    result = {'project': {'owner': project_identity(text)[0], 'number': project_identity(text)[1]},
              'repository': repository, 'issue': issue, 'storage': store.storage,
              'current': before['current'], 'desired': desired, 'applied': False,
              'changed': before['current'] != desired}
    if not apply:
        return result
    if inspect(gh, text, repository, issue) != before:
        raise RuntimeError('stale Sub-project state; nothing written')
    if store.storage == 'field' and result['changed']:
        field = before['field']
        member = before['membership']
        variables = {'projectId': member['projectId'], 'itemId': member['itemId'], 'fieldId': field['id']}
        if desired is None:
            graphql(gh, '''mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!) {
              clearProjectV2ItemFieldValue(input:{projectId:$projectId,itemId:$itemId,fieldId:$fieldId}) { projectV2Item { id } }
            }''', **variables)
        else:
            option = next(o['id'] for o in field['options'] if o['name'] == desired)
            graphql(gh, '''mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!,$option:String!) {
              updateProjectV2ItemFieldValue(input:{projectId:$projectId,itemId:$itemId,fieldId:$fieldId,
                value:{singleSelectOptionId:$option}}) { projectV2Item { id } }
            }''', option=option, **variables)
    elif store.storage == 'labels' and result['changed']:
        endpoint = f'repos/{repository}'
        if desired:
            labels = flatten_pages(gh_json(gh, 'api', '--paginate', '--slurp', endpoint + '/labels?per_page=100'))
            if any(v['name'].lower() == desired.lower() and v['name'] != desired for v in labels):
                raise ValueError('case-only label collision; inspect before writing')
            if desired not in {v['name'] for v in labels}:
                gh_json(gh, 'api', '--method', 'POST', endpoint + '/labels', '-f', 'name=' + desired, '-f', 'color=5319e7')
                gh_json(gh, 'api', endpoint + '/labels/' + quote(desired, safe=''))
            gh_json(gh, 'api', '--method', 'POST', f'{endpoint}/issues/{issue}/labels', '-f', 'labels[]=' + desired)
            # Adding first prevents a failure from losing the existing grouping.
        if before['current']:
            # gh DELETE may return no JSON; use run and check its exit status.
            from queue_common import run
            proc = run(gh, 'api', '--method', 'DELETE', f'{endpoint}/issues/{issue}/labels/' + quote(before['current'], safe=''))
            if proc.returncode:
                raise RuntimeError('Sub-project label removal failed; inspect partial state before retry: ' + proc.stderr)
    after = inspect(gh, text, repository, issue)
    if after['current'] != desired:
        raise RuntimeError('Sub-project readback mismatch; inspect partial state before retry')
    expected_labels = set(before['labels'])
    if store.storage == 'labels':
        expected_labels.discard(before['current'])
        if desired:
            expected_labels.add(desired)
    if set(after['labels']) != expected_labels or before['issue'] != after['issue'] or before['memberships'] != after['memberships']:
        raise RuntimeError('unrequested issue, label or membership changed')
    field_id = (before['field'] or {}).get('id')
    preserved_before = {k: v for k, v in before['values'].items() if k != field_id}
    preserved_after = {k: v for k, v in after['values'].items() if k != field_id}
    if preserved_before != preserved_after:
        raise RuntimeError('unrequested Project field changed')
    result.update(applied=True, verified=True, labelsAfter=after['labels'])
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', required=True, help='checked, resolved Project contract')
    parser.add_argument('--issue', type=int, required=True)
    parser.add_argument('--project-number', type=int, required=True, help='explicit target assertion')
    parser.add_argument('--gh', default=os.environ.get('PROJECTS_GH_BIN', 'gh'))
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--set', dest='key')
    group.add_argument('--clear', action='store_true')
    group.add_argument('--select', action='store_true', help='list choices and current selection without writing')
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    text = Path(args.contract).read_text()
    if project_identity(text)[1] != args.project_number or args.issue < 1:
        parser.error('issue or explicit Project assertion does not match contract')
    repository = table_value(text, 'Issue repository')
    if args.select:
        if args.apply:
            parser.error('--select is read-only')
        print(json.dumps({'vocabulary': binding(text).vocabulary, 'current': inspect(args.gh, text, repository, args.issue)['current']}))
    else:
        print(json.dumps(change(args.gh, text, repository, args.issue, args.key, args.apply)))


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, OSError, KeyError) as error:
        raise SystemExit(f'subproject: {error}')

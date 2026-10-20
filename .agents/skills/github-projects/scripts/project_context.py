"""Project membership and contract-bound Sub-project storage. No implicit writes."""
from __future__ import annotations

import hashlib
import json
import re
from dataclasses import dataclass
from typing import Any

from queue_common import gh_json, table_value

MARKER = 'PJ implementation authority:'


def rows(text: str, heading: str) -> list[list[str]]:
    active = False
    fence = None
    result = []
    for line in text.splitlines():
        marker = re.match(r"^\s*(`{3,}|~{3,})(.*)$", line)
        if fence:
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                fence = None
            continue
        if marker:
            fence = marker[1]
            continue
        if line.startswith('## '):
            active = line.strip() == '## ' + heading
        if active and line.startswith('|'):
            cells = [v.strip() for v in line.split('|')[1:-1]]
            if cells and cells[0] not in ('Key', '---', 'Common dimension'):
                result.append(cells)
    return result


@dataclass(frozen=True)
class Binding:
    storage: str
    field: str
    vocabulary: dict[str, str]
    scope: str = ''

    def resolve(self, key: str) -> str:
        if key not in self.vocabulary:
            raise ValueError(f'unknown Sub-project key {key!r}')
        return self.vocabulary[key]


def binding(text: str) -> Binding:
    values = rows(text, 'Sub-project vocabulary')
    vocabulary: dict[str, str] = {}
    for row in values:
        if len(row) < 2 or not re.fullmatch(r'[a-z0-9][a-z0-9-]*', row[0]) or not row[1]:
            raise ValueError('invalid Sub-project vocabulary')
        if row[0] in vocabulary or row[1] in vocabulary.values():
            raise ValueError('Sub-project vocabulary must be one-to-one')
        vocabulary[row[0]] = row[1]
    storage = table_value(text, 'Sub-project storage')
    # Existing v1 contracts remain readable. This is a fixed legacy binding,
    # never a fallback after a provider field read fails.
    if not storage:
        storage = 'labels' if vocabulary else 'none'
    if storage not in ('field', 'labels', 'none'):
        raise ValueError('Sub-project storage must be field, labels or none')
    field = table_value(text, 'Sub-project field') or 'Sub-project'
    scope = table_value(text, 'Sub-project label scope')
    if storage != 'field' and (table_value(text, 'Sub-project field') or table_value(text, 'Sub-project field type')):
        raise ValueError('only field storage may configure a Sub-project field')
    if storage == 'field' and scope:
        raise ValueError('field storage may not configure label scope')
    if storage == 'field':
        if table_value(text, 'Sub-project field type') != 'SINGLE_SELECT':
            raise ValueError('field storage requires Sub-project field type SINGLE_SELECT')
    elif storage == 'labels' and table_value(text, 'Sub-project storage'):
        if scope not in ('project', 'shared'):
            raise ValueError('label storage requires explicit project or shared scope')
        if scope == 'project':
            key = table_value(text, 'Project key')
            if not key or any(not label.startswith(f'subproject:{key}:') for label in vocabulary.values()):
                raise ValueError('project label scope requires subproject:PROJECT:KEY labels')
    if storage == 'none' and vocabulary:
        raise ValueError('Sub-project vocabulary requires storage')
    return Binding(storage, field, vocabulary, scope)


def graphql(gh: str, query: str, **variables: Any) -> dict:
    args = ['api', 'graphql', '-f', 'query=' + query]
    for key, value in variables.items():
        if value is not None:
            args += ['-F' if isinstance(value, (int, bool)) else '-f', f'{key}={value}']
    result = gh_json(gh, *args)
    if result.get('errors'):
        raise RuntimeError('GitHub GraphQL: ' + str(result['errors']))
    if not isinstance(result.get('data'), dict):
        raise RuntimeError('GitHub GraphQL returned no data')
    return result['data']


def memberships(gh: str, repository: str, issue: int) -> list[dict]:
    owner, name = repository.split('/')
    query = '''query($owner:String!,$name:String!,$issue:Int!,$cursor:String) {
      repository(owner:$owner,name:$name) { issue(number:$issue) {
        id projectItems(first:100,after:$cursor,includeArchived:true) {
          nodes { id project { id number owner { ... on User { login } ... on Organization { login } } } }
          pageInfo { hasNextPage endCursor }
        }
      } }
    }'''
    cursor = None
    result = []
    while True:
        data = graphql(gh, query, owner=owner, name=name, issue=issue, cursor=cursor)
        node = (data.get('repository') or {}).get('issue')
        if not node:
            raise RuntimeError('issue not found or membership inaccessible')
        connection = node['projectItems']
        for item in connection['nodes']:
            project = item.get('project')
            if not project or not (project.get('owner') or {}).get('login'):
                raise RuntimeError('inaccessible Project membership; refusing incomplete resolution')
            result.append({'owner': project['owner']['login'].lower(), 'number': project['number'],
                           'projectId': project['id'], 'itemId': item['id'], 'issueId': node['id']})
        info = connection['pageInfo']
        if not info['hasNextPage']:
            return result
        if not info.get('endCursor') or info['endCursor'] == cursor:
            raise RuntimeError('invalid membership pagination')
        cursor = info['endCursor']


def project_identity(text: str) -> tuple[str, int]:
    return table_value(text, 'Project owner').lower(), int(table_value(text, 'Project number'))


def active_authorities(comments: list[dict], login: str) -> list[dict]:
    ordered = sorted(comments, key=lambda c: (c.get('created_at', ''), c.get('id', 0)))
    active = []
    completed = []
    for comment in ordered:
        if (comment.get('user') or {}).get('login', '').lower() != login.lower():
            continue
        body = comment.get('body') or ''
        if body.startswith('PJ deterministic administration:'):
            completed = active or completed
            active = []
        elif body.startswith(MARKER):
            active.append(comment)
    # A verified receipt can precede a failed queue-label removal. Retain its
    # request for an idempotent retry until a newer authority comment arrives.
    return active or completed


def authority_payload(comment: dict) -> dict | None:
    body = comment.get('body', '')
    match = re.fullmatch(r'PJ implementation authority:\s*\n```json\n(.*?)\n```\s*', body, re.S)
    if not match:
        return None
    try:
        value = json.loads(match[1])
        target = value['spec']['target']
        project = target['project']
        if value['apiVersion'] != 'github-projects/queue-authority/v1' or value['kind'] != 'QueueAuthority':
            return None
        if (not isinstance(project['owner'], str) or not project['owner']
                or type(project['number']) is not int or project['number'] < 1
                or not isinstance(target['repository'], str)
                or type(target['issue']) is not int or target['issue'] < 1
                or not isinstance(value['spec']['actions'], list)
                or not all(isinstance(action, dict) for action in value['spec']['actions'])):
            return None
        return value
    except (ValueError, KeyError, TypeError):
        return None


def authority_identity(comment: dict) -> str:
    return hashlib.sha256(json.dumps(comment, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def single_request(comments: list[dict], login: str) -> dict | None:
    active = active_authorities(comments, login)
    contexts = set()
    for comment in active:
        payload = authority_payload(comment)
        if payload:
            target = payload['spec']['target']
            contexts.add((str(target.get('repository')).lower(), target.get('issue'),
                          target['project']['owner'].lower(), target['project']['number']))
    if len(contexts) > 1:
        raise ValueError('multiple active Project requests on one issue; complete or withdraw them before queuing another context')
    return active[-1] if active else None


def select_context(managed: set[tuple[str, int]], live: list[dict], explicit: tuple[str, int] | None,
                   intent: dict | None = None) -> tuple[str, int]:
    if intent:
        project = intent['spec']['target']['project']
        target = project['owner'].lower(), project['number']
        if explicit and explicit != target:
            raise ValueError('explicit Project disagrees with authority comment')
        explicit = target
    existing = {(v['owner'].lower(), v['number']) for v in live}
    if explicit:
        if explicit not in managed:
            raise ValueError('target Project is not managed by this repository')
        if explicit not in existing and not (intent and any(
                a.get('kind') == 'project.membership.add' for a in intent['spec'].get('actions', []))):
            raise ValueError('target membership missing; explicit membership.add authority required')
        return explicit
    relevant = existing & managed
    if len(relevant) != 1:
        raise ValueError('zero or multiple relevant memberships; supply an explicit Project context')
    return next(iter(relevant))

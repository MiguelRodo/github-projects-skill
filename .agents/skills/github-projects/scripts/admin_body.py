"""Remove only recognised, independently fulfilled administration metadata.

No authority comes from this text. This is a post-success direct-executor step.
GitHub has no atomic issue-body CAS: observed changes stop cleanup, and a private
original is retained before the single edit. A read/write race still exists.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path

from project_context import binding, graphql, project_identity, rows
from queue_common import gh_json, table_value

MARKER = re.compile(r'^(?:Administrative setup for `pj -i`:|(?:\*\*)?Project administration requested:(?:\*\*)?|#{1,6}\s+Project administration(?: requested)?\b:?)', re.I)
SETTING = re.compile(r'\s*(?:[-*]\s+)?(Project|Sub-project|Subproject|Native parent|Parent|Class|Type|Priority|Status|Due date|Target date)\s*:\s*(.+)', re.I | re.S)


def parse(body: str) -> dict | None:
    markers = []
    offset = 0
    fence = None
    lines = body.splitlines(keepends=True)
    for index, line in enumerate(lines):
        match = re.match(r'^\s*(`{3,}|~{3,})(.*)', line)
        if fence:
            if match and match[1][0] == fence[0] and len(match[1]) >= len(fence) and not match[2].strip():
                fence = None
        elif match:
            fence = match[1]
        elif heading := MARKER.match(line):
            markers.append((index, offset, heading.end()))
        offset += len(line)
    if not markers:
        return None
    if len(markers) != 1:
        raise ValueError('multiple administration blocks')
    index, start, heading_length = markers[0]
    content_start = start + heading_length
    if not lines[index][heading_length:].strip():
        content_start = start + len(lines[index])
    end = start + len(lines[index])
    rest = lines[index + 1:]
    if content_start == end:
        while rest and not rest[0].strip():
            content_start += len(rest[0])
            end += len(rest.pop(0))
    for line in rest:
        if not line.strip():
            break
        end += len(line)
    settings, spans = {}, []
    substantive = False
    clauses = []
    for chunk in re.finditer(r'[^;\r\n]+(?:;|\r?\n|$)', body[content_start:end]):
        raw = chunk[0].rstrip(';\r\n')
        begin = 0
        for sentence in re.finditer(r'\.(?=\s+[A-Z])', raw):
            clauses.append((chunk.start() + begin, chunk.start() + sentence.end(), raw[begin:sentence.end()]))
            begin = sentence.end()
        clauses.append((chunk.start() + begin, chunk.end(), raw[begin:]))
    for begin, finish, raw in clauses:
        if not raw.strip():
            continue
        match = SETTING.fullmatch(raw)
        project_ref = re.fullmatch(r'\s*(.+?)\s+Project\s+#(\d+)\.?\s*', raw, re.I)
        if project_ref:
            key, value = 'project', raw.strip().rstrip('.')
        elif match:
            key, value = match[1].lower(), match[2]
            key = {'subproject': 'sub-project', 'native parent': 'parent', 'type': 'class'}.get(key, key)
        else:
            key, value = None, None
        if key is None:
            if re.match(r'\s*(?:[-*]\s+)?[A-Za-z][A-Za-z -]{0,30}:', raw):
                raise ValueError('unknown or malformed administration setting')
            substantive = True
            continue
        if substantive:
            raise ValueError('metadata follows substantive restrictions')
        if key in settings:
            raise ValueError('duplicate administration setting')
        value = value.strip().rstrip('.')
        if '`' in value:
            wrapped = re.fullmatch(r'(`+)([^`]+)\1', value)
            if not wrapped:
                raise ValueError('unmatched or embedded administration backticks')
            value = wrapped[2]
        if not value or len(value) > 200:
            raise ValueError('malformed administration value')
        settings[key] = value
        spans.append((content_start + begin, content_start + finish))
    if not settings:
        raise ValueError('no recognised administration settings')
    if not substantive:
        spans = [(start, end)]
    replacement = body
    for begin, finish in reversed(spans):
        replacement = replacement[:begin] + replacement[finish:]
    return {'settings': settings, 'spans': spans, 'replacement': replacement,
            'originalHash': hashlib.sha256(body.encode()).hexdigest(),
            'replacementHash': hashlib.sha256(replacement.encode()).hexdigest()}


def profile(gh: str, repository: str, issue: int, contract: str) -> dict:
    owner, number = project_identity(contract)
    repo_owner, repo_name = repository.split('/')
    payload = graphql(gh, '''query($owner:String!,$name:String!,$issue:Int!){
      repository(owner:$owner,name:$name){issue(number:$issue){
        parent{number repository{nameWithOwner}}
        projectItems(first:100,includeArchived:true){nodes{
          project{number owner{...on User{login}...on Organization{login}}
            fields(first:100){nodes{...on ProjectV2FieldCommon{name dataType}
              ...on ProjectV2SingleSelectField{options{name}}}pageInfo{hasNextPage}}}
          fieldValues(first:100){nodes{...on ProjectV2ItemFieldSingleSelectValue{name
            field{...on ProjectV2FieldCommon{name}}}
            ...on ProjectV2ItemFieldDateValue{date field{...on ProjectV2FieldCommon{name}}}}
            pageInfo{hasNextPage}}
        }pageInfo{hasNextPage}}
      }}}''', owner=repo_owner, name=repo_name, issue=issue)
    repository_data = payload.get('repository')
    if not isinstance(repository_data, dict) or not isinstance(repository_data.get('issue'), dict):
        raise ValueError('inaccessible issue profile')
    data = repository_data['issue']
    def complete_nodes(connection):
        if not isinstance(connection, dict) or not isinstance(connection.get('pageInfo'), dict) or connection['pageInfo'].get('hasNextPage') is not False:
            raise ValueError('incomplete profile connection')
        nodes = connection.get('nodes')
        if not isinstance(nodes, list) or any(not isinstance(v, dict) for v in nodes):
            raise ValueError('unavailable profile node')
        return nodes
    members = complete_nodes(data.get('projectItems'))
    for member in members:
        project = member.get('project')
        if not isinstance(project, dict) or not isinstance(project.get('owner'), dict) or not isinstance(project['owner'].get('login'), str):
            raise ValueError('unavailable membership identity')
    selected = [v for v in members if v['project'].get('number') == number and v['project']['owner']['login'].casefold() == owner.casefold()]
    if len(selected) != 1:
        raise ValueError('exact target membership required')
    item = selected[0]
    fields = [v for v in complete_nodes(item['project'].get('fields')) if v]
    values = [v for v in complete_nodes(item.get('fieldValues')) if v]
    if any(not isinstance(v.get('name'), str) or not isinstance(v.get('dataType'), str) for v in fields):
        raise ValueError('unavailable field identity')
    if any(not isinstance(v.get('field'), dict) or not isinstance(v['field'].get('name'), str) or not isinstance(v.get('name', v.get('date')), str) for v in values):
        raise ValueError('unavailable field value')
    if len({v['name'] for v in fields}) != len(fields):
        raise ValueError('ambiguous field names')
    if len({v['field']['name'] for v in values}) != len(values):
        raise ValueError('ambiguous field values')
    return {'fields': {v['name']: v for v in fields},
            'values': {v['field']['name']: v.get('name', v.get('date')) for v in values},
            'parent': data.get('parent')}


def fulfilled(settings: dict, contract: str, repository: str, live: dict, current: dict, locations: dict) -> bool:
    owner, number = project_identity(contract)
    for key, value in settings.items():
        if key == 'project':
            names = [table_value(contract, 'Project key'), table_value(contract, 'Project title'), str(number), f'{owner}/{number}']
            ref = re.fullmatch(r'(.+?)\s+Project\s+#(\d+)', value, re.I)
            if ref:
                if int(ref[2]) != number or ref[1].casefold() not in {v.casefold() for v in names if v}:
                    return False
            elif value.casefold() not in {v.casefold() for v in names if v}:
                return False
        elif key == 'parent':
            parent = current['parent']
            if value.lower() in ('none', 'unset', '-'):
                if parent is not None:
                    return False
            else:
                ref = re.fullmatch(r'(?:(?P<repo>[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#?(?P<number>\d+)', value)
                if not ref or not parent or parent['number'] != int(ref['number']) or parent['repository']['nameWithOwner'].casefold() != (ref['repo'] or repository).casefold():
                    return False
        elif key == 'sub-project':
            store = binding(contract)
            wanted = store.resolve(value) if value in store.vocabulary else None
            if wanted is None and value.lower() not in ('none', 'unset', 'clear', '-'):
                return False
            if store.storage == 'field':
                field = current['fields'].get(store.field)
                if not field or field['dataType'] != 'SINGLE_SELECT' or (wanted is not None and sum(v['name'] == wanted for v in field.get('options', [])) != 1):
                    return False
                if current['values'].get(store.field) != wanted:
                    return False
            elif store.storage == 'labels':
                selected = {v['name'] for v in live['labels']} & set(store.vocabulary.values())
                if selected != ({wanted} if wanted else set()):
                    return False
            else:
                return False
        else:
            common = 'class' if key == 'type' else key.replace(' ', '_')
            default_name = key[0].upper() + key[1:]
            kind, name = locations.get(common, ('project field', default_name))
            mapping = {v[0]: v[1] for v in rows(contract, common.title() + ' mapping') if len(v) >= 2}
            wanted = mapping.get(value, value)
            if value.lower() in ('unset', 'clear', '-'):
                wanted = None
            if kind != 'project field':
                if common != 'class' or kind != 'organization issue type' or name not in ('Issue Type', 'Type'):
                    return False
                actual = live.get('type', {}).get('name') if isinstance(live.get('type'), dict) else None
                if wanted is None or actual != wanted:
                    return False
            else:
                field = current['fields'].get(name)
                expected_type = 'DATE' if key in ('due date', 'target date') else 'SINGLE_SELECT'
                if not field or field['dataType'] != expected_type or current['values'].get(name) != wanted:
                    return False
    return True


def recovery_record(repository: str, issue: int, original: str, plan: dict) -> Path:
    directory = Path(os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state'))) / 'pj/body-cleanup'
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=directory, suffix='.json', delete=False) as handle:
        json.dump({'repository': repository, 'issue': issue, 'originalBody': original, **plan}, handle)
        handle.flush()
        os.fsync(handle.fileno())
        if os.name == 'posix':
            directory_fd = os.open(directory, os.O_RDONLY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        return Path(handle.name)


def cleanup(gh: str, contract: str, repository: str, issue: int, original: str, locations: dict, expected_id: int | None = None) -> dict:
    try:
        plan = parse(original or '')
    except ValueError as exc:
        return {'status': 'retained_ambiguous', 'reason': str(exc)}
    if plan is None:
        return {'status': 'not_present'}
    endpoint = f'repos/{repository}/issues/{issue}'
    record = None
    def target_matches(value):
        if not isinstance(value, dict) or not isinstance(value.get('repository_url'), str):
            return False
        canonical_repo = value['repository_url'].rsplit('/repos/', 1)[-1]
        return expected_id is not None and value.get('id') == expected_id and canonical_repo.casefold() == repository.casefold()
    try:
        live = gh_json(gh, 'api', endpoint)
        if not target_matches(live):
            return {'status': 'retained_target_changed'}
        if live.get('body') != original:
            return {'status': 'retained_concurrent_edit'}
        current = profile(gh, repository, issue, contract)
        if not fulfilled(plan['settings'], contract, repository, live, current, locations):
            return {'status': 'retained_unverified_settings'}
        record = recovery_record(repository, issue, original, {**plan, 'issueId': expected_id})
        # No intervening provider calls after this immediate body/hash guard.
        fresh = gh_json(gh, 'api', endpoint)
        if not target_matches(fresh) or fresh.get('body') != original or hashlib.sha256(fresh['body'].encode()).hexdigest() != plan['originalHash']:
            return {'status': 'retained_concurrent_edit', 'recoveryRecord': str(record)}
        result = subprocess.run([gh, 'api', '--method', 'PATCH', endpoint, '--input', '-'],
                                input=json.dumps({'body': plan['replacement']}), text=True, capture_output=True)
        after = gh_json(gh, 'api', endpoint)
        if target_matches(after) and after.get('body') == plan['replacement']:
            return {'status': 'applied_verified', 'recoveryRecord': str(record), 'removedSpans': plan['spans']}
        return {'status': 'retained_write_failed' if after.get('body') == original else 'readback_mismatch',
                'providerExit': result.returncode, 'recoveryRecord': str(record)}
    except (RuntimeError, ValueError, KeyError, TypeError, OSError) as exc:
        result = {'status': 'retained_or_uncertain', 'reason': str(exc)}
        if record is not None:
            result['recoveryRecord'] = str(record)
        return result

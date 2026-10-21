#!/usr/bin/env python3
"""Regression coverage for label-free routing, storage authority and reversible migration."""
import copy
import importlib.util
import io
import json
import subprocess
from contextlib import redirect_stdout
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1] / 'scripts'
sys.path.insert(0, str(SCRIPTS))
import project_context as context
import subproject


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


discovery = load('discovery', 'queue-discover.py')
migration = load('migration', 'migrate-project-storage.py')
views = load('views', 'project-views.py')
classifier = load('classifier', 'queue-classify.py')

BASE = '''| Contract version | 1 |
| Mode | single |
| Project key | work |
| Issue repository | octo/issues |
| Project owner | octo |
| Project number | 4 |
| Project title | Work |
| Routing | membership |
'''
FIELD = BASE + '''| Sub-project storage | field |
| Sub-project field | Sub-project |
| Sub-project field type | SINGLE_SELECT |
## Sub-project vocabulary
| Key | Provider value |
| --- | --- |
| tools | Tools |
| ops | Operations |
'''
LABELS = BASE + '''| Sub-project storage | labels |
| Sub-project label scope | project |
## Sub-project vocabulary
| Key | Label |
| --- | --- |
| tools | subproject:work:tools |
| ops | subproject:work:ops |
'''
LEGACY = BASE.replace('membership', 'label:project:work') + '''## Sub-project vocabulary
| Key | Label |
| --- | --- |
| tools | subproject:tools |
| ops | subproject:ops |
'''


def intent(number=4, actions=None):
    return {'apiVersion':'github-projects/queue-authority/v1','kind':'QueueAuthority','spec':{
        'target':{'repository':'octo/issues','issue':42,'project':{'owner':'octo','number':number}},
        'shape':'existing_task','actions':actions or [{'kind':'project.membership.add'}]}}


def comment(payload, date='2026-01-01', login='octo'):
    return {'id':1,'user':{'login':login},'created_at':date,'updated_at':date,
            'body':'PJ implementation authority:\n```json\n'+json.dumps(payload)+'\n```'}


class Fake:
    def __init__(self, field=True):
        self.labels = {'unrelated', 'project:work', 'subproject:tools'}
        self.field = field
        self.value = None
        self.extra = 'P2'
        self.writes = []
        self.fail_remove = False
        self.tamper = False

    def graphql(self, gh, query, **variables):
        if 'projectItems' in query:
            return {'repository':{'issue':{'id':'I','projectItems':{'nodes':[{
                'id':'ITEM','project':{'id':'P','number':4,'owner':{'login':'octo'}}}],
                'pageInfo':{'hasNextPage':False}}}}}
        if 'clearProject' in query or 'updateProject' in query:
            self.value = None if 'clearProject' in query else {'TOOLS':'Tools','OPS':'Operations'}[variables['option']]
            self.writes.append(('field', self.value))
            if self.tamper:
                self.extra = 'P0'
            return {}
        if 'fieldValues' in query:
            nodes = [{'name':self.extra,'field':{'id':'PRIORITY','name':'Priority'}}]
            if self.value:
                nodes.append({'name':self.value,'field':{'id':'SUB','name':'Sub-project'}})
            return {'node':{'id':'ITEM','fieldValues':{'nodes':nodes,'pageInfo':{'hasNextPage':False}}}}
        if 'fields(first' in query:
            fields = [{'id':'SUB','name':'Sub-project','dataType':'SINGLE_SELECT','options':[
                {'name':'Tools','id':'TOOLS'},{'name':'Operations','id':'OPS'}]}] if self.field else []
            return {'node':{'id':'P','fields':{'nodes':fields,'pageInfo':{'hasNextPage':False}}}}
        raise AssertionError(query)

    def gh(self, gh, *args):
        if args == ('api', 'user'):
            return {'login':'octo'}
        if args[:2] == ('issue', 'list'):
            self.assert_no_route(args)
            return [{'number':42,'url':'https://github.com/octo/issues/issues/42','labels':[{'name':'pj:implement-chat'}]}]
        if 'comments?' in args[-1]:
            return [[comment(intent())]]
        if '--method' in args:
            self.writes.append(args)
            if any(v.startswith('name=') for v in args):
                return {'name':next(v[5:] for v in args if v.startswith('name='))}
            label = next(v.removeprefix('labels[]=') for v in args if v.startswith('labels[]='))
            self.labels.add(label)
            return {}
        if args[-1].endswith('/labels?per_page=100'):
            return [[{'name':v} for v in self.labels]]
        return {'id':42,'title':'Keep title','body':'Keep body','state':'open','assignees':[],
                'milestone':None,'labels':[{'name':v} for v in sorted(self.labels)]}

    def assert_no_route(self, args):
        assert 'project:work' not in args and 'subproject:tools' not in args

    def run(self, *args):
        from urllib.parse import unquote
        import subprocess
        if self.fail_remove:
            return subprocess.CompletedProcess(args, 1, '', 'simulated network failure')
        self.labels.discard(unquote(args[-1].split('/labels/')[1]))
        self.writes.append(('remove', args[-1]))
        return subprocess.CompletedProcess(args, 0, '', '')

    def patches(self):
        from contextlib import ExitStack
        stack = ExitStack()
        for module in (context, subproject, discovery, migration):
            if hasattr(module, 'gh_json'):
                stack.enter_context(patch.object(module, 'gh_json', self.gh))
        stack.enter_context(patch.object(context, 'graphql', self.graphql))
        stack.enter_context(patch.object(subproject, 'graphql', self.graphql))
        stack.enter_context(patch('queue_common.run', self.run))
        stack.enter_context(patch.object(migration, 'run', self.run))
        return stack


class Context(unittest.TestCase):
    def test_membership_resolution(self):
        managed = {('octo',4),('octo',5)}
        live = [{'owner':'octo','number':4}]
        self.assertEqual(context.select_context(managed, live, None), ('octo',4))
        for memberships in ([], live+[{'owner':'octo','number':5}]):
            with self.assertRaisesRegex(ValueError, 'zero or multiple'):
                context.select_context(managed, memberships, None)
        self.assertEqual(context.select_context(managed, [], ('octo',4), intent()), ('octo',4))
        with self.assertRaisesRegex(ValueError, 'membership missing'):
            context.select_context(managed, [], ('octo',4))
        self.assertEqual(context.select_context(managed, live+[{'owner':'octo','number':5}], ('octo',4)), ('octo',4))
        with self.assertRaisesRegex(ValueError, 'disagrees'):
            context.select_context(managed, live, ('octo',5), intent())

    def test_one_active_project_request_and_receipt_retry(self):
        first = comment(intent())
        second = comment(intent(5), '2026-01-03')
        with self.assertRaisesRegex(ValueError, 'multiple active'):
            context.single_request([first, second], 'octo')
        receipt = {'user':{'login':'octo'},'body':'PJ deterministic administration: verified', 'created_at':'2026-01-02'}
        self.assertEqual(context.single_request([first, receipt], 'octo'), first)
        self.assertEqual(context.single_request([first, receipt, second], 'octo'), second)
        self.assertEqual(context.single_request([first, comment(intent(5), login='intruder')], 'octo'), first)

    def test_binding_old_and_new(self):
        self.assertEqual(context.binding(LEGACY).storage, 'labels')
        self.assertEqual(context.binding(FIELD).resolve('tools'), 'Tools')
        self.assertEqual(context.binding(LABELS).resolve('tools'), 'subproject:work:tools')
        for invalid in (FIELD.replace('SINGLE_SELECT','TEXT'), LABELS.replace('subproject:work:tools','subproject:tools')):
            with self.assertRaises(ValueError): context.binding(invalid)

    def test_agentic_subproject_set_clear_and_vocabulary_guard(self):
        import io
        import subprocess
        from contextlib import redirect_stdout
        fake = Fake()
        fake.labels.add('pj:implement-chat')
        for action, outcome in [({'kind':'subproject.value.set','value':'tools'}, 'deterministic'),
                                ({'kind':'subproject.value.clear'}, 'deterministic'),
                                ({'kind':'subproject.value.set','value':'unknown'}, 'blocked')]:
            original = fake.gh
            def gh(gh, *args):
                if 'comments?' in args[-1]:
                    return [[comment(intent(actions=[action]))]]
                return original(gh, *args)
            with tempfile.TemporaryDirectory() as directory, fake.patches():
                path = Path(directory)/'contract.md'
                path.write_text(FIELD)
                output = io.StringIO()
                with patch.object(classifier, 'gh_json', gh), patch.object(classifier, 'run', return_value=subprocess.CompletedProcess([],0,'','')), \
                     patch.object(sys, 'argv', ['classify','--contract',str(path),'--repository','octo/issues','--issue','42']), redirect_stdout(output):
                    classifier.main()
                self.assertEqual(json.loads(output.getvalue())['classification'], outcome)
                self.assertFalse(fake.writes)

    def test_label_free_discovery_and_unplaced_intent(self):
        fake = Fake()
        fake.labels = set()
        with tempfile.TemporaryDirectory() as directory, fake.patches():
            path = Path(directory)/'contract.md'
            path.write_text(FIELD)
            scope = ['octo/issues','pj:implement-chat','project:work','-','work','-',directory,str(path)]
            self.assertEqual(discovery.discover('gh',[scope])[0][1], '42')
            with patch.object(discovery, 'memberships', return_value=[]):
                self.assertEqual(discovery.discover('gh',[scope])[0][1], '42')

    def test_field_plan_set_clear_preserves_labels(self):
        fake = Fake()
        with fake.patches():
            original = set(fake.labels)
            self.assertFalse(subproject.change('gh', FIELD, 'octo/issues',42,'tools')['applied'])
            self.assertFalse(fake.writes)
            self.assertTrue(subproject.change('gh', FIELD, 'octo/issues',42,'tools',True)['verified'])
            self.assertEqual(fake.value,'Tools')
            self.assertEqual(fake.labels,original)
            subproject.change('gh', FIELD, 'octo/issues',42,None,True)
            self.assertIsNone(fake.value)
            self.assertEqual(fake.labels,original)

    def test_missing_expected_field_and_new_field_migration_boundary(self):
        fake = Fake(False)
        with fake.patches(), self.assertRaisesRegex(ValueError,'expected Sub-project'):
            subproject.change('gh', FIELD,'octo/issues',42,'tools',True)
        self.assertFalse(fake.writes)
        fake.field = True
        with fake.patches(), self.assertRaisesRegex(ValueError,'migrate'):
            subproject.change('gh', LABELS,'octo/issues',42,'tools',True)

    def test_label_fallback_only_owns_exact_vocabulary(self):
        fake = Fake(False)
        fake.labels |= {'subproject:work:ops','subproject:another:tools'}
        with fake.patches():
            subproject.change('gh', LABELS,'octo/issues',42,'tools',True)
            self.assertNotIn('subproject:work:ops',fake.labels)
            self.assertIn('subproject:another:tools',fake.labels)
            self.assertIn('subproject:tools',fake.labels)
            subproject.change('gh', LABELS,'octo/issues',42,None,True)
            self.assertNotIn('subproject:work:tools',fake.labels)
            self.assertIn('unrelated',fake.labels)

    def test_readback_detects_collateral_field_change(self):
        fake = Fake()
        fake.tamper = True
        with fake.patches(), self.assertRaisesRegex(RuntimeError,'unrequested Project field'):
            subproject.change('gh',FIELD,'octo/issues',42,'tools',True)

    def test_migration_snapshot_retry_verify_rollback(self):
        fake = Fake()
        with fake.patches(), tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'snapshot.json'
            journal = migration.plan('gh',LEGACY,FIELD,[42],['project:work'])
            original = copy.deepcopy(journal['entries'][0]['before'])
            migration.save(path,journal)
            self.assertFalse(fake.writes)
            fake.fail_remove = True
            with self.assertRaisesRegex(RuntimeError,'removal failed'):
                migration.execute('gh',journal,FIELD,path)
            self.assertIn('project:work',fake.labels)
            self.assertEqual(json.loads(path.read_text())['entries'][0]['status'],'apply_pending')
            fake.fail_remove = False
            migration.execute('gh',journal,FIELD,path)
            migration.execute('gh',journal,FIELD,path)
            self.assertEqual(fake.labels,{'unrelated'})
            self.assertEqual(fake.value,'Tools')
            migration.execute('gh',journal,FIELD,path,rollback=True)
            self.assertEqual(subproject.inspect('gh',FIELD,'octo/issues',42),original)
            migration.execute('gh',journal,FIELD,path,rollback=True)

    def test_journal_save_recovers_from_orphans_and_cleans_failed_writes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'journal.json'
            migration.save(path, {'phase':'snapshot'})
            # Both the previous fixed-name format and a killed new writer can
            # leave files behind. Neither must block saving the next phase.
            orphans = [Path(directory)/'journal.json.tmp', Path(directory)/'journal.json.killed.tmp']
            for orphan in orphans:
                orphan.write_text('interrupted write')
            migration.save(path, {'phase':'apply_pending'})
            self.assertEqual(json.loads(path.read_text()), {'phase':'apply_pending'})
            before = set(Path(directory).iterdir())
            with patch.object(migration.json,'dump',side_effect=OSError('disk full')):
                with self.assertRaisesRegex(OSError,'disk full'):
                    migration.save(path, {'phase':'must not commit'})
            self.assertEqual(json.loads(path.read_text()), {'phase':'apply_pending'})
            self.assertEqual(set(Path(directory).iterdir()),before)
            with patch.object(migration.os,'replace',side_effect=OSError('interrupted rename')):
                with self.assertRaisesRegex(OSError,'interrupted rename'):
                    migration.save(path, {'phase':'must not commit'})
            self.assertEqual(set(Path(directory).iterdir()),before)
            migration.save(path, {'phase':'rolled_back'})
            self.assertEqual(json.loads(path.read_text()), {'phase':'rolled_back'})

    def test_rollback_preserves_label_added_after_initial_preservation_check(self):
        fake = Fake()
        with fake.patches(), tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'snapshot.json'
            journal = migration.plan('gh',LEGACY,FIELD,[42],['project:work'])
            migration.save(path,journal)
            migration.execute('gh',journal,FIELD,path)
            original_change = migration.change
            def concurrent_change(*args, **kwargs):
                result = original_change(*args, **kwargs)
                fake.labels.add('concurrent-user-label')
                return result
            writes_before = len(fake.writes)
            with patch.object(migration,'change',concurrent_change), \
                 self.assertRaisesRegex(ValueError,'preserving concurrent labels'):
                migration.execute('gh',journal,FIELD,path,rollback=True)
            self.assertIn('concurrent-user-label',fake.labels)
            self.assertFalse(any('concurrent-user-label' in str(write)
                                 for write in fake.writes[writes_before:]))
            self.assertEqual(json.loads(path.read_text())['entries'][0]['status'],'rollback_pending')

    def test_migration_rejects_unowned_routing_label(self):
        with self.assertRaisesRegex(ValueError,'exact legacy route'):
            migration.plan('gh', LEGACY,FIELD,[42],['unrelated'])

    def test_rollback_preserves_concurrent_removal_of_unrelated_label(self):
        fake = Fake()
        with fake.patches(), tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'snapshot.json'
            journal = migration.plan('gh',LEGACY,FIELD,[42],['project:work'])
            migration.save(path,journal)
            migration.execute('gh',journal,FIELD,path)
            original_change = migration.change
            def concurrent_change(*args, **kwargs):
                result = original_change(*args, **kwargs)
                fake.labels.discard('unrelated')
                return result
            with patch.object(migration,'change',concurrent_change), \
                 self.assertRaisesRegex(ValueError,'preserving concurrent labels'):
                migration.execute('gh',journal,FIELD,path,rollback=True)
            self.assertNotIn('unrelated',fake.labels)
            self.assertEqual(json.loads(path.read_text())['entries'][0]['status'],'rollback_pending')

    def test_migration_preserves_shared_grouping_for_other_memberships(self):
        fake = Fake()
        native = fake.graphql
        def graphql(gh, query, **variables):
            value = native(gh, query, **variables)
            if 'projectItems' in query:
                value['repository']['issue']['projectItems']['nodes'].append({
                    'id':'OTHER','project':{'id':'OTHER_PROJECT','number':5,'owner':{'login':'octo'}}})
            return value
        with fake.patches(), patch.object(context, 'graphql', graphql):
            journal = migration.plan('gh', LEGACY,FIELD,[42],['project:work'])
            self.assertEqual(journal['entries'][0]['removeLabels'], ['project:work'])

    def test_view_defaults_and_filtered_classification(self):
        fields = [{'id':i,'node_id':f'N{i}','name':name,'data_type':'single_select' if name=='Sub-project' else 'text'}
                  for i,name in enumerate(['Title','Due date','Target date','Priority','Status','Class','Sub-issues progress','Sub-project','Labels'],1)]
        overall = views.spec(FIELD,fields,'user')
        self.assertEqual(overall['layout'],'table')
        self.assertEqual(overall['group_by'],[5])
        self.assertEqual(overall['sort_by'],[[4,'asc']])
        self.assertIn(8,overall['visible_fields'])
        filtered = views.spec(FIELD,fields,'user','tools')
        self.assertEqual(filtered['filter'],'sub-project:"Tools"')
        self.assertNotIn(8,filtered['visible_fields'])
        self.assertEqual(views.spec(LABELS,fields,'user','tools')['filter'],'label:"subproject:work:tools"')
        self.assertIn(9,views.spec(LABELS,fields,'user')['visible_fields'])
        fields += [{'id':10,'name':'Assignees','data_type':'assignees'},
                   {'id':11,'name':'Linked pull requests','data_type':'linked_pull_requests'}]
        self.assertEqual(views.spec(FIELD,fields,'user')['visible_fields'],[1,2,3,4,6,7,5,10,11,8])


class ProjectViewsCLI(unittest.TestCase):
    """Exercise main(), including the provider write and independent readback."""

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name)/'contract.md'
        self.fields = [{'id':i, 'node_id':f'N{i}', 'name':name,
                        'data_type':'single_select' if name == 'Sub-project' else 'text'}
                       for i,name in enumerate(['Title','Due date','Target date','Priority','Status',
                                                'Class','Sub-issues progress','Sub-project','Labels'],1)]
        self.live = [{'id':'ROADMAP','name':'Timeline','layout':'ROADMAP_LAYOUT'}]
        self.posts = []
        self.reads = 0
        self.failure = None

    def provider(self, gh, *args):
        if args[:2] == ('project','field-list'):
            return {'totalCount':1,'fields':[{'name':'Sub-project','type':'ProjectV2SingleSelectField',
                       'options':[{'name':'Tools','id':'TOOLS'},{'name':'Operations','id':'OPS'}]}]}
        if args[-1].endswith('/fields?per_page=100'):
            return [self.fields]
        self.fail(f'unexpected gh read: {args}')

    def current_views(self, *args):
        self.reads += 1
        if self.failure == 'readback' and self.reads == 3:
            raise RuntimeError('simulated readback unavailable after create')
        return copy.deepcopy(self.live)

    def create(self, args, *, input, text, capture_output):
        self.assertIn('users/octo/projectsV2/4/views', args)
        self.assertIn('POST', args)
        body = json.loads(input)
        self.assertEqual(body['layout'],'table')
        self.assertEqual(body['group_by'],[5])
        self.assertEqual(body['sort_by'],[[4,'asc']])
        self.posts.append(body)
        self.live.append(self.expected_view(body['filter']))
        if self.failure == 'uncertain':
            return subprocess.CompletedProcess(args,1,'','connection lost after provider committed create')
        return subprocess.CompletedProcess(args,0,'{}','')

    def expected_view(self, filter_text=''):
        columns = list(range(1,8))
        if not self.filtered and self.storage != 'none':
            columns.append(8 if self.storage == 'field' else 9)
        return {'id':'VIEW','name':'tools' if self.filtered else 'all','layout':'TABLE_LAYOUT','filter':filter_text,
                'configuration':{'visibleFields':{'nodes':[{'id':f'N{i}'} for i in columns]}},
                'groupByFields':{'nodes':[{'id':'N5'}]},
                'sortByFields':{'nodes':[{'field':{'id':'N4'},'direction':'ASC'}]}}

    def configure(self, storage, filtered=False):
        self.storage, self.filtered = storage, filtered
        contract = {'field':FIELD,'labels':LABELS,'none':BASE}[storage]
        self.path.write_text(contract.replace('| Project key |','| Owner type | user |\n| Project key |'))
        self.live = self.live[:1]
        self.posts, self.reads, self.failure = [], 0, None

    def invoke(self, apply=False):
        args = ['project-views','--contract',str(self.path),'--project-number','4']
        if self.filtered:
            args += ['--subproject','tools']
        if apply:
            args += ['--apply']
        output = io.StringIO()
        with patch.object(sys,'argv',args), patch.object(views,'gh_json',self.provider), \
             patch.object(views,'read_views',self.current_views), \
             patch.object(views.subprocess,'run',self.create), redirect_stdout(output):
            views.main()
        return json.loads(output.getvalue())

    def test_existing_noop_all_storage_modes(self):
        for storage in ('none','labels','field'):
            with self.subTest(storage=storage):
                self.configure(storage)
                self.live.append(self.expected_view())
                self.assertEqual(self.invoke(apply=True)['action'],'no_change')
                self.assertEqual(self.posts,[])

    def test_plan_apply_readback_and_repeat_all_storage_modes(self):
        for storage in ('none','labels','field'):
            with self.subTest(storage=storage):
                self.configure(storage)
                self.assertEqual(self.invoke()['action'],'create_view')
                self.assertEqual(self.posts,[])
                result = self.invoke(apply=True)
                self.assertEqual(result['viewId'],'VIEW')
                self.assertTrue(result['verified'])
                self.assertEqual(self.invoke(apply=True)['action'],'no_change')
                self.assertEqual(len(self.posts),1)
                self.assertEqual(self.posts[0]['name'],'all')
                self.assertEqual(self.live[0]['layout'],'ROADMAP_LAYOUT')

    def test_filtered_apply_and_existing_noop(self):
        for storage, wanted in [('labels','label:"subproject:work:tools"'),('field','sub-project:"Tools"')]:
            with self.subTest(storage=storage):
                self.configure(storage,filtered=True)
                self.assertTrue(self.invoke(apply=True)['verified'])
                self.assertEqual(self.posts[0]['filter'],wanted)
                self.assertEqual(self.posts[0]['name'],'tools')
                self.assertNotIn(8,self.posts[0]['visible_fields'])
                self.assertNotIn(9,self.posts[0]['visible_fields'])
                self.assertEqual(self.invoke(apply=True)['action'],'no_change')

    def test_uncertain_create_and_failed_readback_recover_without_duplicate_post(self):
        for storage in ('none','labels','field'):
            for failure in ('uncertain','readback'):
                with self.subTest(storage=storage,failure=failure):
                    self.configure(storage)
                    self.failure = failure
                    with self.assertRaises(RuntimeError):
                        self.invoke(apply=True)
                    self.assertEqual(len(self.posts),1)
                    self.failure = None
                    self.assertEqual(self.invoke(apply=True)['action'],'no_change')
                    self.assertEqual(len(self.posts),1)


if __name__ == '__main__':
    unittest.main()

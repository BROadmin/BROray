"""Typed queue admission through the native guard with labelled owner fixtures."""
import json
import hashlib
import os
import shlex
import shutil
import unittest
import uuid
import test_operation_queue as queue


def disable_jq_regex(case):
    """Emulate the unavailable optional regex backend; all other jq stays real."""
    actual=shutil.which('jq',path=case.env.get('PATH',os.environ.get('PATH','')))
    case.assertIsNotNone(actual)
    directory=case.temp/'no-regexp';directory.mkdir(mode=0o700)
    wrapper=directory/'jq'
    wrapper.write_text("""#!/bin/ash
for part; do
  case "$part" in *'test('*|*'test ('*)
    echo JQ_REGEX_UNAVAILABLE >&2; exit 5 ;;
  esac
done
exec """+shlex.quote(actual)+""" "$@"
""")
    wrapper.chmod(0o755)
    case.env['PATH']=str(directory)+os.pathsep+case.env.get('PATH','')


class OperationResources(unittest.TestCase):
    setUp = queue.OperationQueue.setUp
    tearDown = queue.OperationQueue.tearDown
    set_owner = queue.OperationQueue.set_owner
    call = queue.OperationQueue.call
    submit = queue.OperationQueue.submit
    queue_file = queue.OperationQueue.queue_file

    def claim(self, request, nonce=None, pid='900001', expected=0):
        return self.call('queue-claim', request['requestId'], nonce or uuid.uuid4().hex,
                         pid, expected=expected)

    def ack(self, operation, pid='900001'):
        return self.call('ack', operation['operationId'], operation['token'], pid)

    def opfile(self, operation, name):
        return self.temp/'ram/steps'/operation['operationId']/name

    def finish(self, operation):
        return self.call('finish', operation['operationId'], operation['token'], 'completed', '')

    def test_typed_claim_without_jq_regex(self):
        disable_jq_regex(self)
        operation=self.claim(self.submit())
        self.ack(operation)
        self.assertEqual(operation['resourceLocks'],['background-prepare'])
        self.finish(operation)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_atomic_tick_verifies_caller_birth_before_any_change(self):
        operation=self.claim(self.submit())
        self.ack(operation)
        self.call('tick',operation['operationId'],operation['token'],'checking','900001')
        state=self.opfile(operation,'state.json').read_bytes()
        heartbeat=(self.temp/'ram'/f'{operation["operationId"]}.json').read_bytes()
        self.set_owner(self.owner|{'startTicks':'101'})
        result=self.call('tick',operation['operationId'],operation['token'],'checking','900001',expected=2)
        self.assertEqual(result['errorCode'],'OWNER_CHANGED')
        self.assertEqual(self.opfile(operation,'state.json').read_bytes(),state)
        self.assertEqual((self.temp/'ram'/f'{operation["operationId"]}.json').read_bytes(),heartbeat)

    def test_mixed_or_corrupt_supervisor_evidence_is_not_partly_retired(self):
        operation=self.claim(self.submit())
        self.ack(operation)
        first=self.owner|{'pid':900002,'startTicks':'201'}
        second=self.owner|{'pid':900003,'startTicks':'301'}
        self.identities.write_text(json.dumps({'900001':self.owner,
            '900002':first|{'status':'absent'},'900003':second}))
        entries=[];ledgers=[]
        for index,owner in enumerate([first,second],1):
            sid=str(index)*32
            entries.append({'supervisorId':sid,'owner':owner})
            path=self.temp/'ram/supervisors'/operation['operationId']/sid/'children.json'
            path.parent.mkdir(parents=True)
            path.write_text(json.dumps({'schemaVersion':1,'operationId':operation['operationId'],
                'supervisorId':sid,'supervisorPid':owner['pid'],'supervisorStartTicks':owner['startTicks'],
                'bootId':owner['bootId'],'children':[],'termSent':True,'killTriggered':True}))
            ledgers.append(path)
        registry=self.opfile(operation,'supervisors.json')
        registry.write_text(json.dumps({'schemaVersion':1,'supervisors':entries}))
        paths=[registry,*ledgers,self.opfile(operation,'state.json')]
        before=[p.read_bytes() for p in paths]
        reply=self.call('helpers-drain',operation['operationId'],operation['token'],expected=2)
        self.assertEqual(reply['errorCode'],'CHILDREN_UNCONFIRMED')
        self.assertEqual([p.read_bytes() for p in paths],before)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
        self.identities.write_text(json.dumps({'900001':self.owner,
            '900002':first|{'status':'absent'},'900003':second|{'status':'absent'}}))
        ledgers[1].write_bytes(b'{broken')
        before=[p.read_bytes() for p in paths]
        reply=self.call('helpers-drain',operation['operationId'],operation['token'],expected=2)
        self.assertEqual(reply['errorCode'],'CHILDREN_UNCONFIRMED')
        self.assertEqual([p.read_bytes() for p in paths],before)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_atomic_finish_refuses_reused_pid_and_keeps_resource(self):
        operation=self.claim(self.submit())
        self.ack(operation)
        state=self.opfile(operation,'state.json').read_bytes()
        self.set_owner(self.owner|{'startTicks':'101'})
        result=self.call('finish',operation['operationId'],operation['token'],'completed','','900001',expected=2)
        self.assertEqual(result['errorCode'],'OWNER_CHANGED')
        self.assertEqual(self.opfile(operation,'state.json').read_bytes(),state)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())
        self.set_owner(self.owner)
        self.call('finish',operation['operationId'],operation['token'],'completed','','900001')
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_invalid_owner_or_state_cannot_ack_or_change_evidence(self):
        operation=self.claim(self.submit())
        owner=self.opfile(operation,'owner.json')
        state=self.opfile(operation,'state.json')
        original_owner=owner.read_bytes();original_state=state.read_bytes()
        cases=[
            ('owner',('owner','pid'),1),
            ('owner',('owner','startTicks'),'bad'),
            ('owner',('owner','bootId'),''),
            ('owner',('owner','executable'),'relative'),
            ('owner',('owner','commandDigest'),'g'*64),
            ('owner',('token',),'z'*32),
            ('state',('operationId',),'other'),
            ('state',('revision',),'1'),
            ('state',('running',),'true'),
            ('state',('resourceLocks',),'global'),
            ('state',('queueStep','schemaVersion'),2),
        ]
        for filename,keys,value in cases:
            with self.subTest(filename=filename,keys=keys):
                owner.write_bytes(original_owner);state.write_bytes(original_state)
                path=owner if filename=='owner' else state
                record=json.loads(path.read_bytes());target=record
                for key in keys[:-1]:target=target[key]
                target[keys[-1]]=value
                path.write_text(json.dumps(record))
                before=path.read_bytes()
                reply=self.call('ack',operation['operationId'],operation['token'],'900001',expected=1)
                self.assertEqual(reply['errorCode'],'STATE_UNAVAILABLE')
                self.assertEqual(path.read_bytes(),before)
                self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_observer_and_prepare_can_overlap_writer_safely(self):
        writer = self.call('begin', 'routes', 'check', 'test-route',
                           'USER', '900001', 'protected', uuid.uuid4().hex)
        self.ack(writer)
        fence = (self.temp/'global.lock/owner.json').read_bytes()
        prepare = self.claim(self.submit())
        self.ack(prepare)
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        self.assertEqual(prepare['resourceLocks'], ['background-prepare'])
        self.assertEqual(observer['resourceLocks'], ['active-observer'])
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(), fence)
        self.finish(observer)
        self.finish(prepare)
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(), fence)

    def test_legacy_runtime_writer_cannot_overlap_observer(self):
        for action in ['auto-switch', 'subscriptions:scheduler']:
            with self.subTest(action=action):
                writer = self.call('begin', 'system', action, 'servers', 'USER',
                                   '900001', 'cooperative', uuid.uuid4().hex)
                self.ack(writer)
                request = self.submit('servers:active-health', 'active', 'AUTO_SWITCH')
                self.assertEqual(self.claim(request, expected=2)['errorCode'], 'OPERATION_BUSY')
                self.assertFalse((self.temp/'ram/resources/active-observer').is_symlink())
                self.finish(writer)

    def test_observer_blocks_legacy_runtime_writer_start(self):
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        fence = (self.temp/'ram/resources/active-observer/owner.json').read_bytes()
        for action in ['auto-switch', 'subscriptions:scheduler']:
            self.assertEqual(self.call('begin', 'system', action, 'servers', 'USER',
                                       '900001', 'cooperative', uuid.uuid4().hex,
                                       expected=2)['errorCode'], 'RESOURCE_BUSY')
        self.assertEqual((self.temp/'ram/resources/active-observer/owner.json').read_bytes(), fence)

    def test_second_prepare_denied_and_next_skips_occupied_resource(self):
        first = self.claim(self.submit(target='first'))
        self.ack(first)
        second = self.submit(target='second')
        self.assertEqual(self.claim(second, expected=2)['errorCode'], 'RESOURCE_BUSY')
        self.assertIsNone(self.call('queue-next')['requestId'])
        observer = self.submit('servers:active-health', 'active', 'AUTO_SWITCH')
        self.assertEqual(self.call('queue-next')['requestId'], observer['requestId'])

    def test_legacy_server_probe_reserves_heavy_capacity_only(self):
        writer = self.call('begin', 'system', 'servers:check', 'one',
                           'USER', '900001', 'cooperative', uuid.uuid4().hex)
        self.ack(writer)
        fence = (self.temp/'global.lock/owner.json').read_bytes()
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        request = self.submit(target='second')
        self.assertEqual(self.claim(request, expected=2)['errorCode'], 'OPERATION_BUSY')
        self.assertIsNone(self.call('queue-next')['requestId'])
        self.assertEqual((self.temp/'global.lock/owner.json').read_bytes(), fence)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_prepare_blocks_legacy_server_probe_start(self):
        prepare = self.claim(self.submit())
        self.ack(prepare)
        fence = (self.temp/'ram/resources/background-prepare/owner.json').read_bytes()
        for action in ['servers:check', 'servers:quality']:
            self.assertEqual(self.call('begin', 'system', action, 'one', 'USER',
                                       '900001', 'cooperative', uuid.uuid4().hex,
                                       expected=2)['errorCode'], 'RESOURCE_BUSY')
        self.assertEqual((self.temp/'ram/resources/background-prepare/owner.json').read_bytes(), fence)
        self.assertFalse((self.temp/'global.lock').exists())

    def test_observer_cannot_commit_or_take_protected_helper(self):
        op = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(op)
        for phase in ['committing', 'switching', 'fetching']:
            self.assertEqual(self.call('tick', op['operationId'], op['token'], phase,
                                      expected=2)['errorCode'], 'STEP_PERMISSION_DENIED')
        self.assertEqual(self.call('route-supervisor-register', op['operationId'], op['token'],
                                  '900001', uuid.uuid4().hex, expected=2)['errorCode'], 'STEP_PERMISSION_DENIED')
        self.assertFalse((self.temp/'global.lock').exists())

    def test_claim_replay_binds_nonce_owner_and_request(self):
        request = self.submit()
        nonce = uuid.uuid4().hex
        op = self.claim(request, nonce)
        self.assertEqual(self.claim(request, nonce), op)
        self.assertEqual(self.claim(request, expected=2)['errorCode'], 'OWNER_CHANGED')
        self.set_owner({**self.owner, 'startTicks': '101'})
        self.assertEqual(self.claim(request, nonce, expected=2)['errorCode'], 'OWNER_CHANGED')
        self.assertEqual(self.call('ack', op['operationId'], op['token'], '900001',
                                  expected=2)['errorCode'], 'OWNER_CHANGED')

    def test_forged_resource_and_priority_rejected(self):
        op = self.claim(self.submit())
        record = json.loads(self.opfile(op, 'state.json').read_bytes())
        record['resourceLocks'] = ['global']
        self.opfile(op, 'state.json').write_text(json.dumps(record))
        before = self.opfile(op, 'state.json').read_bytes()
        self.assertEqual(self.call('ack', op['operationId'], op['token'], '900001',
                                  expected=2)['errorCode'], 'OWNER_CHANGED')
        self.assertEqual(self.opfile(op, 'state.json').read_bytes(), before)

    def test_live_child_blocks_finish_and_release(self):
        op = self.claim(self.submit())
        self.ack(op)
        self.opfile(op, 'children.json').write_text(json.dumps({'children': [self.owner]}))
        self.assertEqual(self.call('finish', op['operationId'], op['token'], 'completed', '',
                                  expected=2)['errorCode'], 'CHILDREN_UNCONFIRMED')
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def test_lost_yield_is_idempotent_and_previous_owner_cannot_publish_next_stage(self):
        request = self.submit('subscriptions:refresh', 'sub-one', 'SUBSCRIPTION_AUTO')
        first = self.claim(request)
        self.ack(first)
        result = self.temp/'ram/requests'/request['requestId']/'result.json'
        result.write_bytes(b'{"prepared":true}\n')
        result.chmod(0o600)
        digest = hashlib.sha256(result.read_bytes()).hexdigest()
        args = ('queue-yield', first['operationId'], first['token'], '900001', 'parse', digest)
        self.call(*args)
        self.call(*args)
        self.assertEqual(self.call('queue-next')['stage'], 'parse')
        second = self.claim(request)
        self.ack(second)
        self.assertNotEqual(first['operationId'], second['operationId'])
        self.assertEqual(self.call('owner-check', first['operationId'], first['token'],
                                  '900001', expected=2)['errorCode'], 'OPERATION_FINISHED')
        self.call(*args)
        row = json.loads(self.queue_file().read_bytes())['requests'][0]
        self.assertEqual(row['operationId'], second['operationId'])
        self.assertEqual(result.read_bytes(), b'{"prepared":true}\n')

    def test_background_fairness_advances_on_grant_not_observation(self):
        for action, source in [('servers:quality', 'SERVER_CHECK_AUTO'),
                               ('subscriptions:refresh', 'SUBSCRIPTION_AUTO'),
                               ('dot:auto-check', 'SCHEDULER')]:
            for i in range(3):
                self.submit(action, f'{source}-{i}', source)
        selection = []
        for i in range(6):
            item = self.call('queue-next')
            self.assertEqual(self.call('queue-next')['requestId'], item['requestId'])
            op = self.claim(item)
            self.ack(op)
            selection.append(op['priority'])
            self.finish(op)
            if i == 2:
                observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
                self.ack(observer)
                self.finish(observer)
        self.assertEqual(selection, [3, 3, 3, 4, 4, 5])

    def test_runtime_mutation_waits_for_observer_and_legacy_fence_preserved(self):
        op = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(op)
        result = self.call('begin', 'system', 'xray:update', 'xray', 'USER', '900001',
                           'protected', uuid.uuid4().hex, expected=2)
        self.assertEqual(result['errorCode'], 'RESOURCE_BUSY')
        self.finish(op)
        legacy = self.temp/'legacy.lock'
        legacy.mkdir()
        for name in ['pid', 'scope', 'action', 'bundle', 'startedAt']:
            (legacy/name).write_text('preserve\n')
        before = {x.name: x.read_bytes() for x in legacy.iterdir()}
        self.assertEqual(self.claim(self.submit(), expected=2)['errorCode'], 'DOMAIN_OPERATION_BUSY')
        self.assertEqual({x.name: x.read_bytes() for x in legacy.iterdir()}, before)


if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)

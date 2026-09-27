"""Addressed merges under the real guard; owner/context facts are labelled fixtures."""
import json
import hashlib
import unittest
import uuid
import time
import subprocess
from pathlib import Path
import test_atomic_publication as publication
import test_operation_resources as resources


class StepPublication(unittest.TestCase):
    set_owner = publication.Publications.set_owner
    raw = publication.Publications.raw
    call = publication.Publications.call
    tearDown = resources.OperationResources.tearDown
    submit = resources.OperationResources.submit
    claim = resources.OperationResources.claim
    ack = resources.OperationResources.ack
    finish = resources.OperationResources.finish
    queue_file = resources.OperationResources.queue_file
    def opfile(self, operation, name):
        if operation.get('resourceLocks')==['global']:
            return self.state/'operations'/operation['operationId']/name
        return resources.OperationResources.opfile(self,operation,name)

    def setUp(self):
        publication.Publications.setUp(self)
        self.context = self.app/'run/current-context.json'
        self.context.write_text(json.dumps({'serverId': 'active', 'context': 'a'*64}))
        self.context.chmod(0o600)
        self.env['BRORAY_OPS_TEST_ACTIVE_CONTEXT'] = str(self.context)
        self.target.write_text(json.dumps({'schemaVersion': 3, 'enabled': True, 'status': 'healthy',
            'consecutiveFailures': 0, 'candidateCount': 1, 'qualityRefresh': self.quality_progress(0)}))
        self.target.chmod(0o600)

    @staticmethod
    def quality_progress(count):
        return {'status':'running', 'totalCount':3, 'checkedCount':count,
                'availableCount':count, 'unavailableCount':0, 'errorCount':0}

    def publish(self, op, resource, payload, expected=0, nonce=None, revision=None, key=''):
        file = self.app/'run'/('input-'+op['operationId']+'.json')
        file.write_text(json.dumps(payload)); file.chmod(0o600)
        revision = revision or json.loads(self.opfile(op, 'state.json').read_bytes())['revision']
        return self.call('publish-json', op['operationId'], op['token'], '900001',
                         resource, key, str(file), nonce or uuid.uuid4().hex,
                         str(revision), expected=expected)

    def health(self, epoch):
        return {'method':'current-socks-https', 'serverId':'active', 'context':'a'*64,
                'status':'healthy', 'checkedEpoch':epoch, 'checkedAt':'2026-09-26T00:00:00Z'}

    def server_probe_payload(self):
        directory = self.app/'servers'
        directory.mkdir(exist_ok=True)
        self.server_file = directory/'node-one.json'
        self.server_file.write_bytes(b'{"id":"node-one","credential":"old"}\n')
        self.server_file.chmod(0o600)
        quality = self.app/'run/server-quality'
        quality.mkdir(exist_ok=True)
        self.quality_file = quality/'node-one.json'
        self.quality_file.write_bytes(b'{"status":"unavailable"}\n')
        self.quality_file.chmod(0o600)
        return {'status':'available','measurementSource':'scheduled',
                'successfulChecks':1,'failedChecks':0,'disconnects':0,'durationMs':20,
                'serverFingerprint':hashlib.sha256(self.server_file.read_bytes()).hexdigest()}

    def test_server_quality_changed_input_rejected_before_intent(self):
        op = self.claim(self.submit(target='all'))
        self.ack(op)
        payload = self.server_probe_payload()
        before = self.quality_file.read_bytes()
        self.server_file.write_bytes(b'{"id":"node-one","credential":"new"}\n')
        result = self.publish(op, 'server-quality', payload, expected=2, key='node-one')
        self.assertEqual(result['errorCode'], 'SERVER_CONTEXT_CHANGED')
        self.assertEqual(self.quality_file.read_bytes(), before)
        self.assertFalse(self.opfile(op,'publication.json').exists())
        self.finish(op)

    def quality_crash(self, point):
        op = self.claim(self.submit(target='all'))
        self.ack(op)
        payload = self.server_probe_payload()
        before = json.loads(self.quality_file.read_bytes())
        self.input.write_text(json.dumps(payload)); self.input.chmod(0o600)
        revision = json.loads(self.opfile(op,'state.json').read_bytes())['revision']
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH'] = point
        result = self.raw('publish-json',op['operationId'],op['token'],'900001',
                          'server-quality','node-one',str(self.input),uuid.uuid4().hex,str(revision))
        self.assertEqual(result.returncode,-9,(point,result.stdout,result.stderr))
        self.env.pop('BRORAY_OPS_TEST_PUBLICATION_CRASH')
        self.set_owner({'status':'absent'})
        self.call('queue-recover')
        expected = payload if point in ['replaced','restored'] else before
        self.assertEqual(json.loads(self.quality_file.read_bytes()),expected)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        self.assertFalse(Path(str(self.quality_file)+'.ops-pending').exists())
        snapshot=self.quality_file.read_bytes()
        self.call('queue-recover')
        self.assertEqual(self.quality_file.read_bytes(),snapshot)

    def test_server_quality_crash_reserved(self): self.quality_crash('reserved')
    def test_server_quality_crash_protected(self): self.quality_crash('protected')
    def test_server_quality_crash_prepared(self): self.quality_crash('prepared')
    def test_server_quality_crash_replaced(self): self.quality_crash('replaced')
    def test_server_quality_crash_restored(self): self.quality_crash('restored')

    def active_state_payload(self, operation):
        config=self.app/'config/system/server-auto-switch.json'
        config.parent.mkdir(parents=True,exist_ok=True)
        if not config.exists():
            config.write_text('{"enabled":true,"failureThreshold":3}')
            config.chmod(0o600)
        health=self.health(100)
        health['status']='unhealthy'
        return {'schemaVersion':3,'backgroundOperationId':operation['operationId'],
            'enabled':True,'status':'waiting-threshold','activeServerId':'active',
            'activeHealth':health,'consecutiveFailures':1,'lastProxyContext':'a'*64,
            'lastEvaluationAt':health['checkedAt'],'lastEvaluationEpoch':100,
            'lastReason':'Fixture failed proxy sample','lastError':None,
            'autoConfigSha256':hashlib.sha256(config.read_bytes()).hexdigest()}

    def admitted_failover(self):
        observer=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(observer)
        state=self.active_state_payload(observer)
        state['consecutiveFailures']=3
        state['activeHealth']['checkedEpoch']=int(time.time())
        state['lastEvaluationEpoch']=state['activeHealth']['checkedEpoch']
        self.publish(observer,'active-state',state)
        request=self.submit('servers:failover','active','AUTO_SWITCH')
        self.finish(observer)
        operation=self.claim(request);self.ack(operation)
        payload={'schemaVersion':1,'requestId':request['requestId'],'operationId':operation['operationId'],
                 'sourceContext':'a'*64,'serverId':'active','autoConfigSha256':state['autoConfigSha256'],
                 'status':'probing','checkedCount':0,'totalCount':3,'candidateCount':0,
                 'selectedServerId':None,'lastAttemptEpoch':int(time.time()),
                 'updatedAt':'2026-09-27T00:00:00Z','updatedEpoch':int(time.time())}
        return operation,payload

    def test_legacy_global_mutation_cannot_overtake_pending_failover(self):
        operation,progress=self.admitted_failover()
        request=progress['requestId']
        directory=self.temp/'ram/requests'/request;directory.mkdir(parents=True,exist_ok=True)
        result=directory/'result.json';result.write_text('{}');result.chmod(0o600)
        self.call('queue-yield',operation['operationId'],operation['token'],'900001','probe',
                  hashlib.sha256(result.read_bytes()).hexdigest())
        self.finish(operation)
        before=self.queue_file().read_bytes()
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        result=self.call('begin','system','servers:activate','another','USER','900001',
                         'protected',uuid.uuid4().hex,expected=2)
        self.assertEqual(result['errorCode'],'OPERATION_BUSY')
        self.assertEqual(self.queue_file().read_bytes(),before)
        self.assertFalse((self.temp/'global.lock').exists())

    def test_paused_pending_failover_does_not_block_manual_mutation(self):
        operation,progress=self.admitted_failover()
        request=progress['requestId']
        directory=self.temp/'ram/requests'/request;directory.mkdir(parents=True,exist_ok=True)
        result=directory/'result.json';result.write_text('{}');result.chmod(0o600)
        self.call('queue-yield',operation['operationId'],operation['token'],'900001','probe',
                  hashlib.sha256(result.read_bytes()).hexdigest())
        self.finish(operation)
        self.call('pause')
        before=self.queue_file().read_bytes()
        self.assertIsNone(self.call('queue-next')['requestId'])
        manual=self.call('begin','system','servers:activate','another','USER','900001',
                         'protected',uuid.uuid4().hex)
        self.ack(manual);self.finish(manual)
        self.assertEqual(self.queue_file().read_bytes(),before)
        self.call('resume')
        self.assertEqual(self.call('queue-next')['requestId'],request)

    def test_failover_progress_preserves_newer_observer_and_quality(self):
        operation,payload=self.admitted_failover()
        peer=self.claim(self.submit('servers:active-health','active','USER'));self.ack(peer)
        self.publish(peer,'active-health',self.health(int(time.time())+1))
        before=json.loads(self.target.read_bytes())
        self.publish(operation,'failover-progress',payload)
        after=json.loads(self.target.read_bytes())
        self.assertEqual(after.pop('failover'),payload)
        self.assertEqual(after,before)

    def test_stale_failover_progress_is_rejected_before_intent(self):
        operation,payload=self.admitted_failover()
        before=self.target.read_bytes()
        self.context.write_text(json.dumps({'serverId':'active','context':'b'*64}))
        result=self.publish(operation,'failover-progress',payload,expected=2)
        self.assertEqual(result['errorCode'],'FAILOVER_CONTEXT_CHANGED')
        self.assertIs(result['mutationStarted'],False)
        self.assertEqual(self.target.read_bytes(),before)
        self.assertFalse(self.opfile(operation,'publication.json').exists())

    def test_cooperative_failover_cannot_replace_complete_cache(self):
        operation,_=self.admitted_failover()
        before=self.target.read_bytes()
        payload=json.loads(before);payload['backgroundOperationId']=operation['operationId']
        result=self.publish(operation,'auto-state',payload,expected=1)
        self.assertEqual(result['errorCode'],'INVALID_PUBLICATION')
        self.assertEqual(self.target.read_bytes(),before)

    def test_pending_failover_context_change_is_not_clean_refusal(self):
        operation,payload=self.admitted_failover()
        before=self.target.read_bytes()
        self.input.write_text(json.dumps(payload));self.input.chmod(0o600)
        revision=json.loads(self.opfile(operation,'state.json').read_bytes())['revision']
        args=['publish-json',operation['operationId'],operation['token'],'900001',
              'failover-progress','',str(self.input),uuid.uuid4().hex,str(revision)]
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']='reserved'
        interrupted=self.raw(*args)
        self.assertEqual(interrupted.returncode,-9,(interrupted.stdout,interrupted.stderr))
        self.env.pop('BRORAY_OPS_TEST_PUBLICATION_CRASH')
        intent=self.opfile(operation,'publication.json').read_bytes()
        self.context.write_text(json.dumps({'serverId':'active','context':'b'*64}))
        response=self.call(*args,expected=75)
        self.assertEqual(response['errorCode'],'PUBLICATION_UNCONFIRMED')
        self.assertNotEqual(response.get('mutationStarted'),False)
        self.assertEqual(self.target.read_bytes(),before)
        self.assertEqual(self.opfile(operation,'publication.json').read_bytes(),intent)
        self.assertTrue((self.temp/'ram/resources/background-prepare').is_symlink())

    def client_context_response(self,code,started):
        # Client response mapping only: owner/transport are explicit stubs.
        # Native guarded pre-intent and pending-intent cases are above.
        directory=self.temp/'client-publication';directory.mkdir(exist_ok=True)
        (directory/'state.json').write_text('{"revision":1}')
        calls=directory/'calls';calls.write_text('')
        script='''. "$BRORAY_ROOT/lib/operation-job.sh"
broray_job_require_owner() { return 0; }
broray_ops_operation_directory() { printf '%s\\n' "$TEST_PUBLICATION_DIRECTORY"; }
broray_ops_call() {
  printf '%s\\n' called >>"$TEST_PUBLICATION_CALLS"
  printf '%s\\n' "$TEST_PUBLICATION_REPLY"
  return 2
}
BRORAY_BACKGROUND_OPERATION_ID=fixture
BRORAY_BACKGROUND_OPERATION_TOKEN=fixture
BRORAY_JOB_UNRESOLVED=false
rc=0
broray_job_publish_json failover-progress '' "$TEST_PUBLICATION_DIRECTORY/input.json" || rc=$?
jq -nc --argjson rc "$rc" --argjson unresolved "$BRORAY_JOB_UNRESOLVED" '{rc:$rc,unresolved:$unresolved}'
'''
        env={**self.env,'TEST_PUBLICATION_DIRECTORY':str(directory),
             'TEST_PUBLICATION_CALLS':str(calls),
             'TEST_PUBLICATION_REPLY':json.dumps({'ok':False,'errorCode':code,'mutationStarted':started})}
        result=subprocess.run([str(publication.BB),'ash','-c',script],env=env,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,(result.stdout,result.stderr))
        return json.loads(result.stdout),calls.read_text().splitlines()

    def test_preintent_context_refusal_releases_client_without_retry(self):
        for code in ['ACTIVE_CONTEXT_CHANGED','ACTIVE_SETTINGS_CHANGED','FAILOVER_CONTEXT_CHANGED']:
            with self.subTest(code=code):
                result,calls=self.client_context_response(code,False)
                self.assertEqual(result,{'rc':76,'unresolved':False})
                self.assertEqual(calls,['called'])

    def test_uncertain_context_response_stays_unresolved(self):
        result,calls=self.client_context_response('FAILOVER_CONTEXT_CHANGED',True)
        self.assertEqual(result,{'rc':75,'unresolved':True})
        self.assertEqual(calls,['called']*3)

    def failover_publication_crash(self,point):
        operation,payload=self.admitted_failover()
        before=json.loads(self.target.read_bytes())
        self.input.write_text(json.dumps(payload));self.input.chmod(0o600)
        revision=json.loads(self.opfile(operation,'state.json').read_bytes())['revision']
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']=point
        result=self.raw('publish-json',operation['operationId'],operation['token'],'900001',
                        'failover-progress','',str(self.input),uuid.uuid4().hex,str(revision))
        self.assertEqual(result.returncode,-9,(point,result.stdout,result.stderr))
        self.env.pop('BRORAY_OPS_TEST_PUBLICATION_CRASH')
        self.set_owner({'status':'absent'});self.call('queue-recover')
        expected=before|({'failover':payload} if point in ['replaced','restored'] else {})
        self.assertEqual(json.loads(self.target.read_bytes()),expected)
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
        saved=self.target.read_bytes();self.call('queue-recover')
        self.assertEqual(self.target.read_bytes(),saved)

    def test_failover_publication_crash_reserved(self):self.failover_publication_crash('reserved')
    def test_failover_publication_crash_protected(self):self.failover_publication_crash('protected')
    def test_failover_publication_crash_replaced(self):self.failover_publication_crash('replaced')
    def test_failover_publication_crash_restored(self):self.failover_publication_crash('restored')

    def protected_failover(self):
        operation,progress=self.admitted_failover()
        request={'requestId':progress['requestId']}
        result=self.temp/'ram/requests'/request['requestId']/'result.json'
        for stage in ['probe','activate']:
            result.write_bytes(b'{"fixture":"prepared"}\n');result.chmod(0o600)
            self.call('queue-yield',operation['operationId'],operation['token'],'900001',
                      stage,hashlib.sha256(result.read_bytes()).hexdigest())
            operation=self.claim(request);self.ack(operation)
        self.call('tick',operation['operationId'],operation['token'],'switching')
        before=json.loads(self.target.read_bytes())
        payload=before|{'backgroundOperationId':operation['operationId'],
                        'status':'switched','activeServerId':'standby',
                        'activeHealth':None,'consecutiveFailures':0,'lastProxyContext':None,
                        'lastSwitchEpoch':int(time.time()),'lastSwitchTo':'standby'}
        self.input.write_text(json.dumps(payload));self.input.chmod(0o600)
        revision=json.loads(self.opfile(operation,'state.json').read_bytes())['revision']
        args=['publish-json',operation['operationId'],operation['token'],'900001',
              'auto-state','',str(self.input),uuid.uuid4().hex,str(revision)]
        return operation,before,payload,args

    def test_protected_failover_publication_restores_switching_and_replays(self):
        operation,before,payload,args=self.protected_failover()
        self.call(*args)
        self.assertEqual(json.loads(self.target.read_bytes()),payload)
        state=json.loads(self.opfile(operation,'state.json').read_bytes())
        self.assertEqual((state['phase'],state['cancelability']),('switching','protected'))
        self.assertEqual(payload['qualityRefresh'],before['qualityRefresh'])
        exact=self.target.read_bytes()
        self.assertTrue(self.call(*args)['alreadyPublished'])
        self.assertEqual(self.target.read_bytes(),exact)
        self.finish(operation)
        self.assertFalse((self.temp/'global.lock').is_symlink())

    def protected_failover_crash(self,point):
        operation,before,payload,args=self.protected_failover()
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']=point
        result=self.raw(*args)
        self.assertEqual(result.returncode,-9,(point,result.stdout,result.stderr))
        self.env.pop('BRORAY_OPS_TEST_PUBLICATION_CRASH')
        self.set_owner({'status':'absent'})
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
        self.assertEqual(json.loads(self.target.read_bytes()),
                         payload if point in ['replaced','restored'] else before)
        state=json.loads(self.opfile(operation,'state.json').read_bytes())
        self.assertEqual((state['phase'],state['cancelability']),('switching','protected'))
        self.assertTrue(state['running'])
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertTrue(json.loads(self.opfile(operation,'publication.json').read_bytes())['complete'])
        exact=self.target.read_bytes()
        self.assertEqual(self.call('recover',expected=2)['result'],'protected_recovery')
        self.assertEqual(self.target.read_bytes(),exact)

    def test_protected_failover_crash_reserved(self):self.protected_failover_crash('reserved')
    def test_protected_failover_crash_protected(self):self.protected_failover_crash('protected')
    def test_protected_failover_crash_replaced(self):self.protected_failover_crash('replaced')
    def test_protected_failover_crash_restored(self):self.protected_failover_crash('restored')

    def active_state_crash(self, point):
        operation=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(operation)
        before=json.loads(self.target.read_bytes())
        payload=self.active_state_payload(operation)
        self.input.write_text(json.dumps(payload));self.input.chmod(0o600)
        revision=json.loads(self.opfile(operation,'state.json').read_bytes())['revision']
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']=point
        result=self.raw('publish-json',operation['operationId'],operation['token'],'900001',
                        'active-state','',str(self.input),uuid.uuid4().hex,str(revision))
        self.assertEqual(result.returncode,-9,(point,result.stdout,result.stderr))
        self.env.pop('BRORAY_OPS_TEST_PUBLICATION_CRASH')
        self.set_owner({**self.owner,'startTicks':'101'})
        peer=self.claim(self.submit('servers:quality','batch','SERVER_CHECK_AUTO'))
        self.ack(peer)
        progress=self.quality_progress(2)
        self.publish(peer,'quality-progress',progress)
        self.finish(peer)
        self.set_owner({'status':'absent'})
        self.call('queue-recover')
        after=json.loads(self.target.read_bytes())
        expected=payload if point in ['replaced','restored'] else before
        for key in payload:
            self.assertEqual(after.get(key),expected.get(key),(point,key))
        self.assertEqual(after['qualityRefresh'],progress)
        snapshot=self.target.read_bytes()
        self.call('queue-recover')
        self.assertEqual(self.target.read_bytes(),snapshot)
        self.assertFalse((self.temp/'ram/resources/active-observer').is_symlink())

    def test_active_state_crash_reserved(self):
        self.active_state_crash('reserved')

    def test_active_state_crash_protected(self):
        self.active_state_crash('protected')

    def test_active_state_crash_replaced(self):
        self.active_state_crash('replaced')

    def test_active_state_crash_restored(self):
        self.active_state_crash('restored')

    def test_active_state_changed_settings_preserves_cache(self):
        operation=self.claim(self.submit('servers:active-health','active','AUTO_SWITCH'))
        self.ack(operation)
        payload=self.active_state_payload(operation)
        (self.app/'config/system/server-auto-switch.json').write_text('{"enabled":false}')
        before=self.target.read_bytes()
        result=self.publish(operation,'active-state',payload,expected=2)
        self.assertEqual(result['errorCode'],'ACTIVE_SETTINGS_CHANGED')
        self.assertEqual(self.target.read_bytes(),before)
        self.assertFalse(self.opfile(operation,'publication.json').exists())

    def interrupt_quality_publication(self, operation, point):
        payload = self.quality_progress(1)
        self.input.write_text(json.dumps(payload)); self.input.chmod(0o600)
        revision = json.loads(self.opfile(operation, 'state.json').read_bytes())['revision']
        nonce = uuid.uuid4().hex
        args = ('publish-json', operation['operationId'], operation['token'], '900001',
                'quality-progress', '', str(self.input), nonce, str(revision))
        self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH'] = point
        result = self.raw(*args)
        self.assertEqual(result.returncode, -9, (result.stdout, result.stderr))
        del self.env['BRORAY_OPS_TEST_PUBLICATION_CRASH']
        return args

    def test_cancel_after_intent_does_not_publish(self):
        quality = self.claim(self.submit())
        self.ack(quality)
        before = self.target.read_bytes()
        args = self.interrupt_quality_publication(quality, 'reserved')
        self.call('cancel', quality['operationId'])
        self.assertEqual(self.call(*args, expected=2)['errorCode'], 'CANCELLED')
        self.assertEqual(self.target.read_bytes(), before)

    def test_new_health_survives_later_older_quality_progress(self):
        quality = self.claim(self.submit())
        self.ack(quality)
        old_progress = self.quality_progress(1)
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        newer_health = self.health(200)
        self.publish(observer, 'active-health', newer_health)
        self.finish(observer)
        self.publish(quality, 'quality-progress', old_progress)
        after = json.loads(self.target.read_bytes())
        self.assertEqual(after['activeHealth'], newer_health)
        self.assertEqual(after['qualityRefresh'], old_progress)
        self.assertEqual(after['consecutiveFailures'], 0)

    def test_changed_context_rejects_without_any_business_mutation(self):
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        self.context.write_text(json.dumps({'serverId':'active', 'context':'b'*64}))
        before = self.target.read_bytes()
        self.assertEqual(self.publish(observer, 'active-health', self.health(200),
                                     expected=2)['errorCode'], 'ACTIVE_CONTEXT_CHANGED')
        self.assertEqual(self.target.read_bytes(), before)
        self.assertFalse(self.opfile(observer, 'publication.json').exists())

    def test_observer_cannot_publish_quality_and_lost_merge_reply_is_idempotent(self):
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        self.assertEqual(self.publish(observer, 'quality-progress', self.quality_progress(1),
                                     expected=1)['errorCode'], 'INVALID_PUBLICATION')
        nonce = uuid.uuid4().hex
        revision = json.loads(self.opfile(observer, 'state.json').read_bytes())['revision']
        payload = self.health(200)
        self.publish(observer, 'active-health', payload, nonce=nonce, revision=revision)
        before = self.target.read_bytes()
        self.assertTrue(self.publish(observer, 'active-health', payload,
                                     nonce=nonce, revision=revision)['alreadyPublished'])
        self.assertEqual(self.target.read_bytes(), before)


def crash_preserves_peer(point):
    def check(self):
        quality = self.claim(self.submit())
        self.ack(quality)
        observer = self.claim(self.submit('servers:active-health', 'active', 'AUTO_SWITCH'))
        self.ack(observer)
        self.interrupt_quality_publication(quality, point)
        newer = self.health(200)
        self.publish(observer, 'active-health', newer)
        self.finish(observer)
        self.set_owner({'status':'absent'})
        self.assertTrue(self.call('queue-recover')['ok'])
        current = json.loads(self.target.read_bytes())
        self.assertEqual(current['activeHealth'], newer)
        self.assertEqual(current['qualityRefresh'], self.quality_progress(1 if point in ['replaced', 'restored'] else 0))
        self.assertFalse((self.temp/'ram/resources/background-prepare').is_symlink())
    return check

for point in ['reserved', 'protected', 'replaced', 'restored']:
    setattr(StepPublication, 'test_crash_'+point+'_preserves_peer_health', crash_preserves_peer(point))

if __name__ == '__main__':
    unittest.main(verbosity=2, failfast=True)

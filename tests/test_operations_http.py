"""CGI contract against real local coordinator and existing session validation.

Runs in the disposable Linux VM only. No router, HTTP server or network access.
"""
import json, os, shutil, subprocess, time, unittest, uuid
from pathlib import Path
from test_operations import Operations, APP, BB, WORKSPACE

class HTTP(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('test_normal_finish_and_next_begin');self.ops.setUp()
        self.ops.call('pause');self.ops.call('resume')
        self.token='c'*64
        self.base=self.ops.temp/'auth'
        # Current session validation executes under the real native guard.
        # Match the private Sessions fixture; do not bypass authentication.
        shutil.copytree(APP/'lib',self.base/'lib')
        (self.base/'bin').mkdir()
        shutil.copyfile(WORKSPACE/'.local/bin/linux-guard',self.base/'bin/broray-ops-guard')
        (self.base/'bin/broray-ops-guard').chmod(0o755)
        sessions=self.base/'run/web-new/sessions';sessions.mkdir(parents=True)
        (sessions/self.token).write_text(json.dumps({'username':'fixture','expiresAt':int(time.time())+300,'lastActivity':int(time.time())}))
        self.env={**self.ops.env,'BRORAY_BASE':str(self.base),'HTTP_COOKIE':'BRORAY_SESSION='+self.token,
                  'HTTP_HOST':'broray.test','HTTP_ORIGIN':'http://broray.test','HTTP_X_BRORAY_REQUEST':'operations',
                  'QUERY_STRING':'','CONTENT_TYPE':'application/json'}
    def request(self,endpoint,method='GET',body=None,env=None,status=200):
        raw='' if body is None else body if isinstance(body,str) else json.dumps(body)
        settings={**self.env,'REQUEST_METHOD':method,'CONTENT_LENGTH':str(len(raw.encode())),**(env or {})}
        p=subprocess.run([str(BB),'ash',str(APP/'web-new/api/operations'/f'{endpoint}.cgi')],env=settings,input=raw.encode(),capture_output=True,timeout=30)
        self.assertEqual(p.returncode,0,p.stderr.decode())
        headers,payload=p.stdout.split(b'\r\n\r\n',1)
        self.assertIn(f'Status: {status} '.encode(),headers,(headers,payload,p.stderr))
        return headers,json.loads(payload)
    def test_authentication_is_required_for_reads_and_writes(self):
        for ep,method,body in [('status','GET',None),('report','GET',None),('recover','POST',{})]:
            self.request(ep,method,body,{'HTTP_COOKIE':''},status=401)
    def test_expired_session_is_rejected(self):
        p=self.base/'run/web-new/sessions'/self.token;p.write_text('{"username":"fixture","expiresAt":1}')
        self.request('status',status=401)
    def test_get_cannot_mutate(self):
        for ep in ['cancel','recover','automation','stop-background']:self.request(ep,status=405)
    def test_cross_origin_and_missing_csrf_header_are_rejected(self):
        for env in [{'HTTP_ORIGIN':'https://evil.invalid'},{'HTTP_ORIGIN':'null'},{'HTTP_ORIGIN':''},{'HTTP_X_BRORAY_REQUEST':''}]:
            self.request('recover','POST',{},env,status=403)
    def test_json_shape_and_unknown_fields_rejected(self):
        for body in ['[]','{}{}','broken','null',{'path':'/etc/passwd'},{'pid':123}]:self.request('recover','POST',body,status=400)
        for body in [{'paused':'true'},{'paused':True,'signal':'KILL'}]:self.request('automation','POST',body,status=400)
    def test_body_size_type_and_short_reads(self):
        self.request('recover','POST',' '*4097,status=413)
        self.request('recover','POST',{}, {'CONTENT_TYPE':'text/plain'},status=415)
        self.request('recover','POST',{}, {'CONTENT_LENGTH':'4'},status=400)
    def test_operation_identifier_traversal_is_rejected(self):
        for value in ['../../etc/passwd','op-20260101000000-1-'+'a'*12,'x'*10000,None]:
            body={'operationId':value}
            self.request('cancel','POST',body,status=413 if len(json.dumps(body))>4096 else 400)
    def test_pause_resume_and_stop_background(self):
        self.request('automation','POST',{'paused':True})
        self.assertTrue(self.request('status')[1]['automationPaused'])
        self.request('automation','POST',{'paused':False})
        self.request('stop-background','POST',{'pauseAutomation':True},status=202)
        self.assertTrue(self.request('status')[1]['automationPaused'])
    def test_cancel_and_protected_conflict(self):
        a=self.ops.begin();self.request('cancel','POST',{'operationId':a['operationId']},status=202)
        self.ops.call('finish',a['operationId'],a['token'],'aborted','CANCELLED')
        b=self.ops.begin('protected');self.request('cancel','POST',{'operationId':b['operationId']},status=409)
    def test_report_attachment_is_sanitized(self):
        a=self.ops.begin();headers,report=self.request('report');raw=json.dumps(report)
        self.assertIn(b'Content-Disposition: attachment;',headers);self.assertIn(b'no-store',headers)
        self.assertNotIn(a['token'],raw);self.assertNotIn(self.token,raw)
        self.assertEqual(report['reportKind'],'broray-diagnostics')
    def test_ambiguous_snapshot_is_503_not_successful_idle(self):
        (self.ops.temp/'global.lock').mkdir()
        self.assertFalse(self.request('status',status=503)[1]['complete'])
    def test_failed_body_requests_do_not_leak_temp_files(self):
        before=set(Path('/tmp').glob('broray-operations-http.*'))
        self.request('recover','POST','broken',status=400)
        self.assertEqual(set(Path('/tmp').glob('broray-operations-http.*')),before)

class QueueHTTP(HTTP):
    def submit(self,source='SERVER_CHECK_AUTO',nonce=None):
        return self.ops.call('queue-submit','servers:quality','batch',source,
                             'a'*64,nonce or uuid.uuid4().hex)

    def test_paused_automatic_health_does_not_defer_manual_queue_label(self):
        automatic=self.ops.call('queue-submit','servers:active-health','active','AUTO_SWITCH',
                                'a'*64,uuid.uuid4().hex)
        manual=self.submit(source='USER')
        self.ops.call('pause')
        before=(self.ops.temp/'ram/queue/boot-one/state.json').read_bytes()
        rows={row['requestId']:row for row in self.request('status')[1]['queue']}
        self.assertEqual(rows[automatic['requestId']]['reason'],'automation_paused')
        self.assertEqual(rows[manual['requestId']]['reason'],'awaiting_resource')
        self.assertEqual(self.ops.call('queue-next')['requestId'],manual['requestId'])
        self.assertEqual((self.ops.temp/'ram/queue/boot-one/state.json').read_bytes(),before)

    def test_queue_status_is_read_only_and_has_no_invented_owner(self):
        self.assertEqual(self.request('status')[1]['queue'],[])
        self.assertFalse((self.ops.temp/'ram').exists())
        item=self.submit()
        queue=self.ops.temp/'ram/queue/boot-one/state.json'
        before=queue.read_bytes()
        status=self.request('status')[1]
        row=next(r for r in status['queue'] if r['requestId']==item['requestId'])
        self.assertEqual(row['state'],'queued')
        self.assertEqual(row['priority'],3)
        self.assertEqual(row['stage'],'probe')
        self.assertNotIn('ownerStatus',row)
        self.assertNotIn('context',row)
        self.assertNotIn('nonce',row)
        self.assertEqual(status['operations'],[])
        self.assertEqual(queue.read_bytes(),before)
        self.ops.call('pause')
        paused=self.request('status')[1]
        self.assertEqual(paused['queue'][0]['reason'],'automation_paused')

    def test_running_ram_step_has_public_identity_and_cancel_binding(self):
        request=self.submit(source='USER')
        operation=self.ops.call('queue-claim',request['requestId'],uuid.uuid4().hex,'900001')
        self.ops.call('ack',operation['operationId'],operation['token'],'900001')
        status=self.request('status')[1]
        self.assertEqual(status['queue'][0]['state'],'running')
        self.assertEqual(status['queue'][0]['operationId'],operation['operationId'])
        self.assertEqual(status['operations'][0]['operationId'],operation['operationId'])
        self.assertEqual(status['operations'][0]['resourceLocks'],['background-prepare'])
        self.assertEqual(status['operations'][0]['ownerStatus'],'ACTIVE')
        self.assertNotIn(operation['token'],json.dumps(status))
        self.request('cancel','POST',{'operationId':operation['operationId']},status=202)
        state=self.ops.temp/'ram/steps'/operation['operationId']
        self.assertTrue((state/'cancel.json').exists())
        self.assertEqual(self.request('status')[1]['operations'][0]['cancelRequested'],True)

    def test_cancel_queued_request_has_no_process_side_effect_and_replays(self):
        request=self.submit()
        for _ in range(2):
            response=self.request('cancel','POST',{'requestId':request['requestId']},status=202)[1]
            self.assertEqual(response['state'],'cancelled')
        self.assertFalse((self.ops.temp/'ram/steps').exists())
        self.assertFalse((self.ops.temp/'global.lock').exists())
        self.assertEqual(self.request('status')[1]['queue'][0]['state'],'cancelled')

    def test_lost_response_lookup_uses_original_nonce_including_coalesced_alias(self):
        item=self.submit(source='USER')
        nonce=uuid.uuid4().hex
        alias=self.submit(source='USER',nonce=nonce)
        self.assertEqual(alias['requestId'],item['requestId'])
        queue=self.ops.temp/'ram/queue/boot-one/state.json';before=queue.read_bytes()
        result=self.request('status','POST',{'nonce':nonce})[1]
        self.assertEqual(result['requestId'],item['requestId'])
        self.assertEqual(result['state'],'queued')
        self.assertEqual(queue.read_bytes(),before)
        self.request('status','POST',{'nonce':nonce},{'HTTP_ORIGIN':'https://evil.invalid'},status=403)

    def test_corrupt_queue_status_is_unavailable_and_evidence_unchanged(self):
        self.submit()
        queue=self.ops.temp/'ram/queue/boot-one/state.json';queue.write_bytes(b'{broken')
        result=self.request('status',status=503)[1]
        self.assertFalse(result['complete'])
        self.assertIn('QUEUE_STATE_INVALID',result['errors'])
        self.assertEqual(queue.read_bytes(),b'{broken')


class QueueAdmission(unittest.TestCase):
    from test_auto_switch_jobs import AutoSwitchJobs as _Auto
    from test_server_jobs import ServerJobs as _Servers
    from test_subscription_jobs import SubscriptionJobs as _Subscriptions
    shell=_Servers.shell
    collect=_Servers.collect
    reap_adopted_helpers=_Servers.reap_adopted_helpers
    active_runtime_fixture=_Auto.active_runtime_fixture
    record=_Subscriptions.record

    def setUp(self):
        self._Auto.setUp(self)
        # Legacy CGI uses its canonical /opt/broray/tmp for the request body.
        # That path is source-only inside this disposable VM fixture.
        (APP/'tmp').mkdir(exist_ok=True)
        token='e'*64
        sessions=self.app/'run/web-new/sessions';sessions.mkdir(parents=True,exist_ok=True)
        (sessions/token).write_text(json.dumps({'username':'fixture','expiresAt':int(time.time())+300,'lastActivity':int(time.time())}))
        self.env.update(HTTP_COOKIE='BRORAY_SESSION='+token,HTTP_HOST='broray.test',
            HTTP_ORIGIN='http://broray.test',HTTP_X_BRORAY_REQUEST='operations',
            HTTP_X_BRORAY_QUEUE='1',CONTENT_TYPE='application/json',QUERY_STRING='')

    def request(self,path,body,status=202,env=None):
        raw=json.dumps(body).encode()
        settings=self.env|{'REQUEST_METHOD':'POST','CONTENT_LENGTH':str(len(raw))}|(env or {})
        response=subprocess.run(['/bin/ash',str(APP/'web-new/api'/path)],env=settings,
            input=raw,capture_output=True,timeout=30)
        self.assertEqual(response.returncode,0,response.stderr)
        headers,payload=response.stdout.split(b'\r\n\r\n',1)
        self.assertIn(f'Status: {status} '.encode(),headers,(response.stdout,response.stderr))
        return json.loads(payload)

    def test_manual_stopped_vpn_check_is_p2_queued_without_runtime_start(self):
        for target in [self.server,'all']:
            nonce=uuid.uuid4().hex
            result=self.request('servers/check.cgi',{'id':target,'nonce':nonce})
            self.assertTrue(result['accepted'])
            self.assertEqual((result['state'],result['priority']),('queued',2))
            self.assertEqual(self.request('servers/check.cgi',{'id':target,'nonce':nonce})['requestId'],result['requestId'])
        self.assertFalse((self.app/'config/active-server').exists())
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'xray-pid').exists())

    def test_manual_current_runtime_check_is_p0(self):
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.active_runtime_fixture()
        result=self.request('servers/check.cgi',{'id':self.server,'nonce':uuid.uuid4().hex})
        self.assertEqual((result['state'],result['priority']),('queued',0))
        self.assertFalse((self.temp/'global.lock').exists())

    def test_subscription_admission_preserves_metadata_and_original_nonce(self):
        record=self.record();before=record.read_bytes()
        nonce=uuid.uuid4().hex
        result=self.request('subscriptions/refresh.cgi',{'id':'test','nonce':nonce})
        self.assertTrue(result['accepted'])
        self.assertEqual((result['state'],result['priority']),('queued',2))
        again=self.request('subscriptions/refresh.cgi',{'id':'test','nonce':nonce})
        self.assertEqual(again['requestId'],result['requestId'])
        self.assertEqual(record.read_bytes(),before)
        self.assertFalse((self.temp/'global.lock').exists())
        self.assertFalse((self.temp/'transport-ready').exists())

    def test_queue_post_origin_and_shape_cannot_create_work(self):
        body={'id':self.server,'nonce':uuid.uuid4().hex}
        self.request('servers/check.cgi',body,403,{'HTTP_ORIGIN':'https://evil.invalid'})
        self.request('servers/check.cgi',body|{'priority':0},400)
        self.request('servers/check.cgi',body|{'id':'../../config'},400)
        self.assertFalse((self.temp/'ram/queue').exists())


if __name__=='__main__':
    if os.name=='nt':raise SystemExit('Use the isolated Linux VM.')
    opt=Path('/opt');opt.mkdir(exist_ok=True)
    (opt/'broray').symlink_to(APP,target_is_directory=True)
    (opt/'bin').mkdir(exist_ok=True)
    subprocess.run([str(BB),'--install','-s',str(opt/'bin')],check=True)
    (opt/'bin/jq').symlink_to('/usr/bin/jq')
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(HTTP))
    report={'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux CGI + existing session validator + native coordinator; synthetic sessions, no network','routerAccessed':False}
    (WORKSPACE/'docs/evidence/operations-http-tests.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

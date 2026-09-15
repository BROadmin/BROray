"""CGI contract against real local coordinator and existing session validation.

Runs in the disposable Linux VM only. No router, HTTP server or network access.
"""
import json, os, subprocess, time, unittest
from pathlib import Path
from test_operations import Operations, APP, BB, WORKSPACE

class HTTP(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('test_normal_finish_and_next_begin');self.ops.setUp()
        self.ops.call('pause');self.ops.call('resume')
        self.token='c'*64
        self.base=self.ops.temp/'auth'
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

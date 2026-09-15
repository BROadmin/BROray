"""Production subscription code with actual Linux owners and isolated files."""
import ctypes,json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
class SubscriptionJobs(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='subscription-jobs-'))
        self.addCleanup(SubscriptionJobs.clean_fixture,self)
        self.app=self.temp/'app';shutil.copytree(ROOT/'implementation/runtime/app',self.app)
        self.state=self.temp/'state';self.state.mkdir()
        self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_BASE':str(self.app),
          'BRORAY_STATE_ROOT':str(self.state),'BRORAY_ROUTES_API_LOCK':str(self.temp/'global.lock'),
          'BRORAY_LEGACY_GLOBAL_LOCK':str(self.temp/'legacy.lock'),'BRORAY_OPS_UPDATER_ROOT':str(self.temp/'updater'),
          'BRORAY_OPS_RAM_ROOT':str(self.temp/'ram'),'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard'),
          'BRORAY_OPS_SUPERVISOR':str(ROOT/'.local/bin/linux-supervisor'),'BRORAY_OPS_ASH':'/bin/ash'}
        self.subdir=self.app/'config/subscriptions';self.subdir.mkdir(parents=True,exist_ok=True)
        (self.app/'tmp').mkdir(exist_ok=True)
        self.env['PATH']=str(self.app/'bin')+':/usr/bin:/bin:/usr/sbin:/sbin'
        self.env.update({'BRORAY_PROXY_HOST':'127.0.0.1','BRORAY_PROXY_PORT':'2080','BRORAY_INTERFACE':'Proxy0'})
        self.env['TEST_READY']=str(self.temp/'transport-ready')
        self.payload=self.temp/'payload.txt'
        self.payload.write_text('vless://11111111-2222-4333-8444-555555555555@93.184.216.34:443?security=tls&type=tcp&sni=example.invalid#Fixture\n')
        self.env['TEST_PAYLOAD']=str(self.payload)
        curl=self.app/'bin/curl'
        curl.write_text('''#!/bin/ash
case "${TEST_MODE:-normal}" in
  wait) trap "" TERM; echo ready >"$TEST_READY"; sleep 60; exit 28 ;;
  error) exit 28 ;;
esac
while [ "$#" -gt 0 ]; do
  case "$1" in --dump-header) headers="$2"; shift ;; --output) body="$2"; shift ;; esac
  shift
done
printf 'HTTP/1.1 200 OK\\r\\nContent-Type: text/plain\\r\\n\\r\\n' >"$headers"
cp "$TEST_PAYLOAD" "$body"
printf 200
''');curl.chmod(0o755)
    def clean_fixture(self):
        # Never delete a fixture while any test-owned or adopted child lives.
        # This process is a subreaper; waitpid cannot target unrelated PIDs.
        end=time.monotonic()+10
        while True:
            try:pid,_=os.waitpid(-1,os.WNOHANG)
            except ChildProcessError:break
            if not pid:
                self.assertLess(time.monotonic(),end,'Child still live: preserve its fixture')
                time.sleep(.05)
        assert self.temp.resolve().parent==Path('/tmp') and self.temp.name.startswith('subscription-jobs-')
        shutil.rmtree(self.temp)
    def record(self,**extra):
        record={'schemaVersion':1,'id':'test','name':'Test','url':'https://93.184.216.34/sub/PRIVATE_CANARY',
          'clientHwid':'broray-1234567890abcdef1234567890abcdef',
          'enabled':True,'autoUpdateEnabled':True,'updateIntervalMinutes':60,
          'lastUpdateStatus':'never','nextUpdateEpoch':1,'createdAt':'2026-09-15T00:00:00Z',
          'updatedAt':'2026-09-15T00:00:00Z','serversReceived':0}|extra
        path=self.subdir/'test.json';path.write_text(json.dumps(record));path.chmod(0o600);return path
    def states(self):return [json.loads(p.read_text()) for p in (self.state/'operations').glob('*/state.json')]
    def reap_adopted_helpers(self):
        # As a test subreaper we take over init's duty. Reap only adopted
        # children listed by the native supervisor, never the Popen job itself.
        for f in (self.temp/'ram').rglob('children.json'):
            try:children=json.loads(f.read_text()).get('children',[])
            except FileNotFoundError:continue
            for child in children:
                try:os.waitpid(child['pid'],os.WNOHANG)
                except ChildProcessError:pass
    def job_script(self,command):
        return '. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_job_begin system subscriptions:refresh subscriptions USER cooperative || exit $?; trap \'broray_job_exit "$?"\' EXIT; '+command
    def shell(self,script,expected=0,timeout=30):
        p=subprocess.run(['/bin/ash','-c',script],env=self.env,capture_output=True,timeout=timeout)
        self.assertEqual(p.returncode,expected,(p.stdout,p.stderr))
        return p
    def test_get_preserves_running_metadata_and_unknown_lock(self):
        record={'schemaVersion':1,'id':'test','name':'Test','url':'https://example.invalid/list',
          'enabled':True,'autoUpdateEnabled':True,'updateIntervalMinutes':60,
          'lastUpdateStatus':'running','nextUpdateEpoch':1,'createdAt':'2026-09-15T00:00:00Z',
          'updatedAt':'2026-09-15T00:00:00Z','serversReceived':0}
        path=self.subdir/'test.json';path.write_text(json.dumps(record));before=path.read_bytes()
        lock=self.app/'run/subscriptions/test.lock';lock.mkdir(parents=True)
        (lock/'foreign').write_text('KEEP')
        self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_list >/dev/null')
        self.assertTrue((lock/'foreign').exists(),'A read-only list removed an ambiguous subscription lock')
        self.assertEqual(path.read_bytes(),before,'A read-only list changed durable subscription state')
    def test_mutating_service_rejects_inherited_owner_in_subshell(self):
        path=self.record();before=path.read_bytes()
        self.shell(self.job_script('( broray_subscription_write_json "$BRORAY_ROOT/config/subscriptions/test.json" "$BRORAY_ROOT/config/subscriptions/test.json" )'),expected=2)
        self.assertEqual(path.read_bytes(),before)
        self.assertEqual(self.states()[0]['state'],'failed')
    def test_complete_update_uses_real_parser_and_commits_servers(self):
        path=self.record()
        self.shell(self.job_script('broray_subscription_update test manual'),timeout=90)
        data=json.loads(path.read_text());self.assertEqual(data['lastUpdateStatus'],'success')
        self.assertEqual(data['lastUpdateResult']['accepted'],1)
        self.assertEqual(len(list((self.app/'servers').glob('*.json'))),1)
        state=self.states()[0];self.assertEqual(state['state'],'completed')
        self.assertEqual(data['backgroundOperationId'],state['operationId'])
        self.assertFalse((self.temp/'global.lock').is_symlink())
        self.assertEqual(list((self.app/'tmp').glob('subscription-op-*')),[])
    def test_download_error_is_failed_job_and_preserves_catalog(self):
        path=self.record();self.env['TEST_MODE']='error'
        self.shell(self.job_script('broray_subscription_update test manual'),expected=1,timeout=60)
        self.assertEqual(json.loads(path.read_text())['lastUpdateStatus'],'error')
        self.assertEqual(self.states()[0]['state'],'failed')
        self.assertEqual(list((self.app/'servers').glob('*.json')),[])
    def test_cancel_download_drains_tree_before_releasing_fence(self):
        self.record();self.env['TEST_MODE']='wait'
        p=subprocess.Popen(['/bin/ash','-c',self.job_script('broray_subscription_update test manual')],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            until=time.monotonic()+30
            while not Path(self.env['TEST_READY']).exists():
                if p.poll() is not None:
                    out,err=p.communicate()
                    detail={str(f.relative_to(self.temp)):f.read_text() for folder in [self.state,self.app/'tmp'] for f in folder.rglob('*.json') if f.stat().st_size<32768}
                    self.fail((p.returncode,out,err,detail))
                if time.monotonic()>until:self.fail('transport never started')
                time.sleep(.05)
            state=self.states()[0]
            fence=(self.temp/'global.lock').readlink()
            self.shell('. "$BRORAY_ROOT/lib/operation-job.sh"; broray_job_begin system subscriptions:refresh subscriptions USER cooperative',expected=2)
            self.assertEqual((self.temp/'global.lock').readlink(),fence)
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+state['operationId'])
            until=time.monotonic()+30
            while True:
                self.reap_adopted_helpers()
                try:out,err=p.communicate(timeout=.1);break
                except subprocess.TimeoutExpired:
                    if time.monotonic()>until:raise
            if p.returncode!=130:
                detail={str(f.relative_to(self.temp)):f.read_text() for folder in [self.state,self.temp/'ram'] for f in folder.rglob('*.json') if f.stat().st_size<32768}
                for f in (self.temp/'ram').rglob('children.json'):
                    for c in json.loads(f.read_text()).get('children',[]):
                        stat=Path('/proc')/str(c['pid'])/'stat'
                        detail['proc/'+str(c['pid'])]=stat.read_text() if stat.exists() else 'absent'
                self.fail((p.returncode,out,err,detail))
            self.assertEqual(self.states()[0]['state'],'aborted')
            self.assertFalse((self.temp/'global.lock').is_symlink())
            self.assertEqual(list((self.app/'servers').glob('*.json')),[])
            registry=self.state/'operations'/state['operationId']/'supervisors.json'
            self.assertEqual(json.loads(registry.read_text())['supervisors'],[])
            path=self.subdir/'test.json';before=path.read_bytes()
            public=json.loads(self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_get test').stdout)
            self.assertEqual(public['lastUpdateStatus'],'error');self.assertEqual(path.read_bytes(),before)
            summary=json.loads(self.shell('. "$BRORAY_ROOT/lib/subscription-service.sh"; broray_subscription_summary').stdout)
            self.assertEqual(summary['runningCount'],0);self.assertEqual(summary['errorCount'],1)
            self.assertEqual(summary['lastUpdatedAt'],public['lastUpdatedAt'])
        finally:
            if p.poll() is None:p.kill();p.communicate(timeout=10)
    def test_paused_scheduler_starts_no_update(self):
        path=self.record();before=path.read_bytes()
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-subscription-scheduler" --once')
        self.assertEqual(path.read_bytes(),before);self.assertEqual(self.states(),[])
    def test_scheduler_records_child_job_and_real_error(self):
        self.record();self.env['TEST_MODE']='error'
        p=subprocess.Popen(['/bin/ash',str(self.app/'bin/broray-subscription-scheduler'),'--once'],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=p.communicate(timeout=60);self.assertEqual(p.returncode,1,(out,err))
        state=self.states()[0];self.assertEqual(state['state'],'failed');self.assertEqual(state['source'],'SUBSCRIPTION_AUTO')
        owner=json.loads((self.state/'operations'/state['operationId']/'owner.json').read_text())['owner']
        self.assertNotEqual(owner['pid'],p.pid)
        self.assertFalse((self.app/'run/subscription-scheduler.pid').exists())
    def test_api_business_error_exits_zero_but_records_failed_job(self):
        self.record();self.env['TEST_MODE']='error'
        p=self.shell('. "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"; broray_subscriptions_api_lock refresh; broray_subscriptions_api_run broray_subscription_update test manual',timeout=60)
        headers,body=p.stdout.decode().split('\r\n\r\n',1)
        self.assertIn('504 Gateway Timeout',headers)
        self.assertFalse(json.loads(body)['success']);self.assertEqual(self.states()[0]['state'],'failed')
        self.assertFalse((self.temp/'global.lock').is_symlink())
        report=self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call report').stdout.decode()
        self.assertNotIn('PRIVATE_CANARY',report);self.assertNotIn('11111111-2222-4333',report)
    def test_api_cancelled_preparation_timeout_is_confirmed_before_response(self):
        self.record()
        p=self.shell(''' . "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"
broray_subscriptions_api_lock refresh
before_helper() { broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >/dev/null || return 1; return 75; }
broray_subscriptions_api_run before_helper
''')
        headers,body=p.stdout.decode().split('\r\n\r\n',1)
        self.assertIn('409 Conflict',headers);self.assertEqual(json.loads(body)['error']['code'],'OPERATION_CANCELLED')
        self.assertEqual(self.states()[0]['state'],'aborted');self.assertFalse((self.temp/'global.lock').is_symlink())
    def test_api_cancel_does_not_resolve_unconfirmed_helper_or_publication(self):
        self.record()
        p=self.shell(''' . "$BRORAY_ROOT/web-new/api/subscriptions/common.sh"
broray_subscriptions_api_lock refresh
unconfirmed() { broray_ops_call cancel "$BRORAY_BACKGROUND_OPERATION_ID" >/dev/null || return 1; BRORAY_JOB_UNRESOLVED=true; return 75; }
broray_subscriptions_api_run unconfirmed
''')
        headers,body=p.stdout.decode().split('\r\n\r\n',1)
        self.assertIn('503 Service Unavailable',headers);self.assertEqual(json.loads(body)['error']['code'],'OPERATION_UNRESOLVED')
        self.assertTrue(self.states()[0]['running']);self.assertTrue((self.temp/'global.lock').is_symlink())
    def test_legacy_resource_lock_is_preserved_during_admitted_job(self):
        path=self.record();before=path.read_bytes()
        lock=self.app/'run/subscriptions/test.lock';lock.mkdir(parents=True)
        (lock/'foreign').write_text('KEEP')
        self.shell(self.job_script('broray_subscription_update test manual'),expected=1)
        self.assertEqual(path.read_bytes(),before);self.assertEqual((lock/'foreign').read_text(),'KEEP')
        self.assertEqual(self.states()[0]['state'],'failed')
    def test_cli_refresh_has_owned_lifecycle(self):
        self.record();self.env['TEST_MODE']='error'
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-subscriptions" refresh test',expected=1,timeout=60)
        state=self.states()[0];self.assertEqual(state['state'],'failed')
        self.assertEqual(state['operation'],'subscriptions:refresh');self.assertEqual(state['source'],'USER')
        self.assertFalse((self.temp/'global.lock').is_symlink())
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubscriptionJobs))
    (ROOT/'docs/evidence/subscription-jobs-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'production subscription code, isolated files and actual Linux processes','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

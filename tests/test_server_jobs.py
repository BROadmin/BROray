"""Actual server service, Linux owners and native helpers in private fixtures."""
import ctypes,json,os,subprocess,time,unittest
from pathlib import Path
from test_subscription_jobs import SubscriptionJobs,ROOT

class ServerJobs(unittest.TestCase):
    states=SubscriptionJobs.states
    reap_adopted_helpers=SubscriptionJobs.reap_adopted_helpers
    def setUp(self):
        SubscriptionJobs.setUp(self)
        (self.app/'config/system').mkdir(parents=True,exist_ok=True)
        (self.app/'config/system/settings.json').write_text('{"listenAddress":"127.0.0.1","socksPort":2080}')
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription fixture 0')
        self.server='subscription-fixture-0000'
        self.quality=self.app/'run/server-quality'/f'{self.server}.json'
        self.env.update({'TEST_PORT':str(self.temp/'port'),'TEST_XRAY_PID':str(self.temp/'xray-pid'),
          'BRORAY_XRAY_BINARY':str(self.app/'bin/fixture-xray')})
        scripts={
          'fixture-xray':'''#!/bin/ash
case " $* " in *' -test '*) exit 0 ;; esac
jq -r '.inbounds[0].port' "$3" >"$TEST_PORT"
echo $$ >"$TEST_XRAY_PID"
trap '' TERM
exec sleep 60
''',
          'netstat':'''#!/bin/ash
if [ -s "$TEST_PORT" ]; then printf 'tcp 0 0 127.0.0.1:%s 0.0.0.0:* LISTEN\\n' "$(cat "$TEST_PORT")"; fi
''',
          'curl':'''#!/bin/ash
case "$1" in --help) echo --socks5-hostname; exit 0 ;; esac
case "${TEST_MODE:-normal}" in
  wait) echo ready >"$TEST_READY"; trap '' TERM; sleep 60; exit 28 ;;
  error) exit 28 ;;
esac
printf '204 0.02'
''',
          'ping':"#!/bin/ash\nprintf 'rtt min/avg/max/mdev = 10/20/30/1 ms\\n'\n"}
        for name,body in scripts.items():
            path=self.app/'bin'/name;path.write_text(body);path.chmod(0o755)
    def collect(self,p,timeout=45):
        until=time.monotonic()+timeout
        while True:
            self.reap_adopted_helpers()
            try:return p.communicate(timeout=.1)
            except subprocess.TimeoutExpired:
                if time.monotonic()>until:
                    p.kill();p.communicate(timeout=5)
                    raise
    def shell(self,script,expected=0,timeout=45):
        p=subprocess.Popen(['/bin/ash','-c',script],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(p,timeout)
        self.assertEqual(p.returncode,expected,(out,err))
        return subprocess.CompletedProcess(p.args,p.returncode,out,err)
    def job(self):return 'exec "$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" check '+self.server
    def old_quality(self):
        self.quality.parent.mkdir(parents=True,exist_ok=True)
        self.quality.write_text('{"successfulChecks":7,"failedChecks":2,"disconnects":0,"status":"available"}')
        self.quality.chmod(0o600)
        return self.quality.read_bytes()
    def wait_transport(self,p):
        until=time.monotonic()+40
        while not Path(self.env['TEST_READY']).exists():
            if p.poll() is not None:self.fail((p.returncode,*p.communicate()))
            if time.monotonic()>until:self.fail('transport never started')
            time.sleep(.05)
    def assert_drained(self):
        self.assertFalse((self.temp/'global.lock').is_symlink())
        for f in (self.state/'operations').glob('*/supervisors.json'):
            self.assertEqual(json.loads(f.read_text())['supervisors'],[])
    def test_check_requires_owned_operation(self):
        p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/lib/server-service.sh"; BRORAY_XRAY=/bin/false; broray_server_check '+self.server],env=self.env,capture_output=True,timeout=25)
        self.assertFalse(self.quality.exists(),'Server check wrote persistent quality without an admitted owner')
        self.assertEqual(p.returncode,73,(p.stdout,p.stderr))
    def test_complete_measurement_publishes_once_and_stops_temporary_xray(self):
        self.old_quality()
        p=self.shell(self.job(),timeout=75)
        result=json.loads(p.stdout);self.assertTrue(result['success'])
        quality=json.loads(self.quality.read_text())
        self.assertEqual(quality['successfulChecks'],8);self.assertEqual(quality['failedChecks'],2)
        self.assertEqual(quality['ping'],20);self.assertEqual(quality['jitter'],20)
        self.assertEqual(self.states()[0]['state'],'completed');self.assert_drained()
        self.assertFalse(Path('/proc',Path(self.env['TEST_XRAY_PID']).read_text().strip()).exists())
        self.assertEqual(list((self.app/'tmp').glob('server-check-op-*')),[])
    def test_negative_measurement_is_failed_job_with_complete_quality(self):
        self.old_quality();self.env['TEST_MODE']='error'
        p=self.shell(self.job(),expected=1,timeout=75)
        self.assertFalse(json.loads(p.stdout)['success'])
        quality=json.loads(self.quality.read_text())
        self.assertEqual(quality['successfulChecks'],7);self.assertEqual(quality['failedChecks'],3)
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
    def test_cancel_preserves_old_quality_and_drains_real_probe_tree(self):
        before=self.old_quality();self.env['TEST_MODE']='wait'
        p=subprocess.Popen(['/bin/ash','-c',self.job()],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait_transport(p)
            state=self.states()[0]
            owner=json.loads((self.state/'operations'/state['operationId']/'owner.json').read_text())['owner']
            self.assertEqual(owner['pid'],p.pid)
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+state['operationId'])
            out,err=self.collect(p)
            self.assertEqual(p.returncode,130,(out,err))
            self.assertEqual(self.quality.read_bytes(),before)
            self.assertEqual(self.states()[0]['state'],'aborted');self.assert_drained()
        finally:
            if p.poll() is None:p.kill();self.collect(p)
    def test_inherited_subshell_cannot_publish_quality(self):
        before=self.old_quality()
        script='. "$BRORAY_ROOT/lib/server-service.sh"; broray_job_begin routes servers:check servers USER cooperative || exit $?; trap \'broray_job_exit "$?"\' EXIT; (broray_server_check '+self.server+')'
        self.shell(script,expected=2)
        self.assertEqual(self.quality.read_bytes(),before);self.assert_drained()
    def test_import_cli_stages_and_commits_under_own_job(self):
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" import "$(cat "$TEST_PAYLOAD")"',timeout=75)
        self.assertEqual(len(list((self.app/'servers').glob('*.json'))),2)
        self.assertEqual(self.states()[0]['state'],'completed');self.assert_drained()
    def test_invalid_import_does_not_leave_lock(self):
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" import invalid',expected=1,timeout=75)
        self.assertEqual(len(list((self.app/'servers').glob('*.json'))),1)
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
    def test_delete_cli_removes_only_requested_inactive_server(self):
        self.old_quality()
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" delete '+self.server)
        self.assertFalse((self.app/'servers'/f'{self.server}.json').exists())
        self.assertFalse(self.quality.exists())
        self.assertEqual(self.states()[0]['state'],'completed');self.assert_drained()
    def test_active_server_delete_is_rejected_without_stuck_operation(self):
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" delete '+self.server,expected=1)
        self.assertTrue((self.app/'servers'/f'{self.server}.json').exists())
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
    def test_quality_batch_snapshot_is_an_owned_job(self):
        p=self.shell('. "$BRORAY_ROOT/web-new/api/servers/common.sh"; broray_servers_api_lock quality-batch-complete; broray_servers_api_run broray_server_publish_snapshot',timeout=75)
        result=json.loads(p.stdout.split(b'\r\n\r\n',1)[1])
        self.assertTrue(result['success']);self.assertTrue(result['data']['published'])
        self.assertEqual(result['data']['totalServers'],1)
        self.assertEqual(self.states()[0]['state'],'completed');self.assert_drained()
    def test_api_backend_failure_keeps_http_envelope_and_failed_job(self):
        self.env['TEST_MODE']='error'
        p=self.shell('. "$BRORAY_ROOT/web-new/api/servers/common.sh"; broray_servers_api_lock check; broray_servers_api_run broray_server_check '+self.server+' manual',timeout=75)
        self.assertIn(b'400 Bad Request',p.stdout)
        self.assertFalse(json.loads(p.stdout.split(b'\r\n\r\n',1)[1])['success'])
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
    def test_paused_automatic_check_creates_no_measurement(self):
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        self.shell(self.job()+' scheduled',expected=76)
        self.assertEqual(self.states(),[]);self.assertFalse(self.quality.exists())
    def test_cgi_death_keeps_actual_worker_owned_and_cancellable(self):
        before=self.old_quality();self.env['TEST_MODE']='wait'
        script='. "$BRORAY_ROOT/web-new/api/servers/common.sh"; broray_servers_api_lock check; broray_servers_api_run broray_server_check '+self.server+' manual'
        p=subprocess.Popen(['/bin/ash','-c',script],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait_transport(p)
            state=self.states()[0];operation=state['operationId']
            owner=json.loads((self.state/'operations'/operation/'owner.json').read_text())['owner']
            self.assertNotEqual(owner['pid'],p.pid)
            # Only our own unreaped Popen child is signalled. The job remains
            # a separate executor, stopped later through its cancellation API.
            p.kill();p.communicate(timeout=5)
            self.assertTrue((self.temp/'global.lock').is_symlink())
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+operation)
            until=time.monotonic()+40
            while self.states()[0]['state']!='aborted':
                self.reap_adopted_helpers()
                if time.monotonic()>until:self.fail('orphan CGI worker did not finish cancellation')
                time.sleep(.1)
            # Terminal state is persisted before fence retirement. Wait for
            # this adopted worker to finish its complete exit protocol.
            while os.waitpid(owner['pid'],os.WNOHANG)[0]==0:
                if time.monotonic()>until:self.fail('worker exit protocol did not complete')
                time.sleep(.1)
            self.assertEqual(self.quality.read_bytes(),before);self.assert_drained()
        finally:
            if p.poll() is None:p.kill();self.collect(p)

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(ServerJobs))
    (ROOT/'docs/evidence/server-jobs-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'production server service, actual Linux owners and native supervisor, isolated transport','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

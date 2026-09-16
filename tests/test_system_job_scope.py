"""Actual server/Xray admission with harmless private business fixtures."""
import ctypes,json,os,subprocess,unittest
from test_subscription_jobs import ROOT,SubscriptionJobs


class SystemJobScope(unittest.TestCase):
    setUp=SubscriptionJobs.setUp
    clean_fixture=SubscriptionJobs.clean_fixture
    shell=SubscriptionJobs.shell

    def assert_scope(self,operation):
        files=list((self.state/'operations').glob('*/state.json'))
        self.assertEqual(len(files),1)
        record=json.loads(files[0].read_bytes())
        self.assertEqual((record['scope'],record['initialCancelability'],record['operation']),
                         ('system','cooperative',operation))

    def test_server_worker_admits_system_job(self):
        # Preserve the actual worker's admission; isolate only its business call.
        with (self.app/'lib/server-import-job.sh').open('a') as f:
            f.write('\nbroray_server_import_job() { :; }\n')
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/lib/server-job-worker.sh" import broray_server_import fixture')
        self.assert_scope('servers:import')

    def test_xray_cli_dispatch_admits_system_job(self):
        self.shell('''
. "$BRORAY_ROOT/lib/operation-job.sh"
. "$BRORAY_ROOT/lib/xray-releases.sh"
broray_xray_update_install() { :; }
broray_xray_update_abort_cleanup() { :; }
broray_xray_install_dispatch install fixture
''')
        self.assert_scope('xray:install')

    def test_xray_web_launch_admits_system_job(self):
        raw=b'{}'
        p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/web-new/api/auth-common.sh"; . "$BRORAY_ROOT/lib/xray-web-operation.sh"'],
          env=self.env|{'XRAY_WEB_OPERATION_MODE':'install','CONTENT_LENGTH':str(len(raw))},input=raw,capture_output=True,timeout=45)
        self.assertIn(b'400 Bad Request',p.stdout,(p.stdout,p.stderr))
        self.assert_scope('xray:install')


if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SystemJobScope))
    (ROOT/'docs/evidence/system-job-scope-tests.json').write_text(json.dumps({
        'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,
        'environment':'Actual server worker and Xray CLI/Web admission; isolated business fixtures','routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

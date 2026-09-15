"""Xray lifecycle baseline in isolated Linux files; no runtime or network."""
import ctypes,hashlib,json,os,shutil,subprocess,time,unittest,zipfile
from test_subscription_jobs import SubscriptionJobs,ROOT
from test_server_jobs import ServerJobs
class XrayJobs(unittest.TestCase):
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    reap_adopted_helpers=SubscriptionJobs.reap_adopted_helpers
    wait_transport=ServerJobs.wait_transport
    assert_drained=ServerJobs.assert_drained
    states=SubscriptionJobs.states
    def setUp(self):
        SubscriptionJobs.setUp(self)
        (self.app/'runtime').mkdir(exist_ok=True)
        self.binary=self.app/'runtime/xray';shutil.copy2(ROOT/'.local/bin/linux-xray-install-old',self.binary)
        self.before=self.binary.read_bytes();self.binary.chmod(0o755)
        self.config=self.app/'config/config.json';self.config.write_text('{"PRIVATE_CANARY":"unchanged"}')
        self.request=self.temp/'request.json'
        self.archive=self.temp/'candidate.zip'
        with zipfile.ZipFile(self.archive,'w',zipfile.ZIP_DEFLATED) as z:z.writestr('xray',(ROOT/'.local/bin/linux-xray-install-new').read_bytes())
        sha=hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.request.write_text(json.dumps({'tag':'v26.9.15','archiveSha256':sha,'currentVersion':'26.9.9',
          'allowUntested':True,'allowPrerelease':False,'allowDowngrade':False}))
        prefix='https://github.com/XTLS/Xray-core/releases/download/v26.9.15/'
        asset='Xray-linux-arm64-v8a.zip'
        release={'tag_name':'v26.9.15','draft':False,'prerelease':False,'published_at':'2026-09-15T00:00:00Z',
          'assets':[{'name':asset,'size':self.archive.stat().st_size,'id':1,'state':'uploaded','digest':'sha256:'+sha,'browser_download_url':prefix+asset},
                    {'name':asset+'.dgst','size':80,'id':2,'state':'uploaded','browser_download_url':prefix+asset+'.dgst'}]}
        (self.temp/'release.json').write_text(json.dumps(release))
        (self.temp/'digest').write_text('SHA2-256= '+sha+'\n')
        self.env.update({'TEST_FIXTURE':str(self.temp),'TEST_REQUEST':str(self.request),'TEST_MODE':'normal',
          'BRORAY_XRAY_BINARY':str(self.binary),'BRORAY_XRAY_CONFIG':str(self.config),
          'BRORAY_XRAY_INIT':str(self.app/'bin/fixture-init')})
        scripts={'curl':'''#!/bin/ash
case "${TEST_MODE:-normal}" in
 wait) echo ready >"$TEST_READY"; trap '' TERM; sleep 60; exit 28 ;;
 error) exit 28 ;;
esac
out=; url=
while [ "$#" -gt 0 ]; do
 case "$1" in -o) out="$2"; shift ;; https://*) url="$1" ;; esac
 shift
done
[ -n "$out" ] || exit 2
case "$url" in
 */releases/tags/*) cp "$TEST_FIXTURE/release.json" "$out" ;;
 *.zip.dgst) cp "$TEST_FIXTURE/digest" "$out" ;;
 *.zip) cp "$TEST_FIXTURE/candidate.zip" "$out" ;;
 *) exit 22 ;;
esac
''','uname':'''#!/bin/ash
if [ "$1" = -m ]; then echo aarch64; else /bin/uname "$@"; fi
''','df':'''#!/bin/ash
# The disposable guest root is initramfs; it reports zero filesystem blocks.
printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\nfixture 262144 40960 221184 16%% /fixture\n'
''','fixture-init':'''#!/bin/ash
[ "${TEST_FAIL_ROLLBACK:-0}" != 1 ]
'''}
        for name,body in scripts.items():
            path=self.app/'bin'/name;path.write_text(body);path.chmod(0o755)
    def command(self):return 'exec "$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray" xray install "$TEST_REQUEST"'
    def test_ambiguous_old_xray_lock_is_not_reclaimed(self):
        lock=self.app/'update/xray.lock';lock.mkdir(parents=True)
        (lock/'foreign').write_text('KEEP')
        self.shell('. "$BRORAY_ROOT/lib/xray-update.sh"; BRORAY_XRAY_UPDATE_STATE="$BRORAY_ROOT/update"; BRORAY_XRAY_UPDATE_LOCK="$BRORAY_ROOT/update/xray.lock"; broray_xray_update_lock_acquire >/dev/null 2>&1 || true')
        self.assertTrue((lock/'foreign').exists(),'Xray installer deleted an ambiguous resource lock')
    def test_install_cli_prepares_and_replaces_only_binary(self):
        p=self.shell(self.command(),timeout=150)
        result=json.loads(p.stdout);self.assertTrue(result['success'])
        self.assertEqual(result['version'],'26.9.15');self.assertFalse(result['running'])
        self.assertEqual(self.binary.read_bytes(),(ROOT/'.local/bin/linux-xray-install-new').read_bytes())
        self.assertEqual(self.config.read_text(),'{"PRIVATE_CANARY":"unchanged"}')
        self.assertEqual(self.states()[0]['state'],'completed');self.assert_drained()
        self.assertEqual(list((self.app/'tmp').glob('xray-job-*')),[])
    def test_cancel_download_preserves_binary_and_config(self):
        self.env['TEST_MODE']='wait'
        p=subprocess.Popen(['/bin/ash','-c',self.command()],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait_transport(p);operation=self.states()[0]['operationId']
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+operation)
            out,err=self.collect(p,55);self.assertEqual(p.returncode,130,(out,err))
            self.assertEqual(self.binary.read_bytes(),self.before)
            self.assertEqual(self.states()[0]['state'],'aborted');self.assert_drained()
        finally:
            if p.poll() is None:p.kill();self.collect(p)
    def test_failed_final_validation_restores_old_binary(self):
        self.env['TEST_XRAY_FAIL_INSTALLED']='1'
        p=self.shell(self.command(),expected=1,timeout=150)
        result=json.loads(p.stdout);self.assertFalse(result['success']);self.assertTrue(result.get('rollbackSuccess'),(result,p.stderr))
        self.assertEqual(self.binary.read_bytes(),self.before)
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
    def test_failed_rollback_keeps_fence_and_backup(self):
        self.env.update({'TEST_XRAY_FAIL_INSTALLED':'1','TEST_FAIL_ROLLBACK':'1'})
        p=self.shell(self.command(),expected=75,timeout=150)
        result=json.loads(p.stdout);self.assertFalse(result['rolledBack']);self.assertFalse(result['rollbackSuccess'])
        self.assertTrue((self.temp/'global.lock').is_symlink())
        self.assertTrue(self.states()[0]['running'])
        self.assertEqual(len(list((self.app/'runtime').glob('xray.broray-*-backup'))),1)
    def test_unowned_service_cannot_start_installation(self):
        self.shell('. "$BRORAY_ROOT/lib/xray-control.sh"; . "$BRORAY_ROOT/lib/xray-update.sh"; broray_xray_update_install install "$TEST_REQUEST"',expected=73)
        self.assertEqual(self.binary.read_bytes(),self.before);self.assertEqual(self.states(),[])
    def test_web_handoff_keeps_worker_owned_after_cgi_exits(self):
        self.env['TEST_MODE']='wait'
        raw=self.request.read_bytes()
        env=self.env|{'XRAY_WEB_OPERATION_MODE':'install','CONTENT_LENGTH':str(len(raw))}
        script='. "$BRORAY_ROOT/web-new/api/auth-common.sh"; . "$BRORAY_ROOT/lib/xray-web-operation.sh"'
        p=subprocess.run(['/bin/ash','-c',script],env=env,input=raw,capture_output=True,timeout=45)
        self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertIn(b'202 Accepted',p.stdout)
        result=json.loads(p.stdout.split(b'\r\n\r\n',1)[1])['data'];operation=result['operationId']
        until=time.monotonic()+40
        while not (self.temp/'transport-ready').exists():
            if time.monotonic()>until:self.fail('web helper did not start')
            time.sleep(.05)
        executor=json.loads((self.state/'operations'/operation/'executor.json').read_text())
        worker=executor['owner']['pid']
        view=json.loads(self.shell('. "$BRORAY_ROOT/lib/xray-web-status.sh"; broray_xray_web_status').stdout)
        self.assertTrue(view['operationRunning']);self.assertEqual(view['backgroundOperation']['ownerStatus'],'ACTIVE')
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+operation)
        until=time.monotonic()+55
        while True:
            self.reap_adopted_helpers()
            ended,status=os.waitpid(worker,os.WNOHANG)
            if ended:break
            if time.monotonic()>until:self.fail('web cancellation never finished')
            time.sleep(.05)
        self.assertEqual(os.waitstatus_to_exitcode(status),130)
        self.assertEqual(self.binary.read_bytes(),self.before);self.assert_drained()
        view=json.loads(self.shell('. "$BRORAY_ROOT/lib/xray-web-status.sh"; broray_xray_web_status').stdout)
        self.assertFalse(view['operationRunning']);self.assertFalse(view['result']['success'])
        self.assertEqual(view['backgroundOperation']['state'],'aborted')
    def test_web_invalid_request_is_failed_job_without_worker(self):
        raw=b'{}'
        env=self.env|{'XRAY_WEB_OPERATION_MODE':'install','CONTENT_LENGTH':str(len(raw))}
        p=subprocess.run(['/bin/ash','-c','. "$BRORAY_ROOT/web-new/api/auth-common.sh"; . "$BRORAY_ROOT/lib/xray-web-operation.sh"'],env=env,input=raw,capture_output=True,timeout=45)
        self.assertIn(b'400 Bad Request',p.stdout,(p.stdout,p.stderr))
        self.assertEqual(self.states()[0]['state'],'failed');self.assert_drained()
        self.assertEqual(self.binary.read_bytes(),self.before)
if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(XrayJobs))
    (ROOT/'docs/evidence/xray-jobs-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'production Xray installer in private Linux files','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

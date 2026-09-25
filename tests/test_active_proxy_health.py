"""Active VPN health must follow current SOCKS transport, independently of ICMP."""
import ctypes,json,os,shutil,subprocess,unittest
from test_auto_switch_jobs import AutoSwitchJobs,ROOT

class ActiveProxyHealth(AutoSwitchJobs):
    def setUp(self):
        super().setUp();self.set_config(enabled=True,failureThreshold=3)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.current=self.app/'config/config.json'
        self.current.write_text(json.dumps(dict(inbounds=[dict(protocol='socks',listen='192.168.2.1',port=2081,settings=dict(auth='noauth'))],outbounds=[dict(protocol='vless')])))
        runtime=self.app/'runtime/xray';runtime.parent.mkdir(exist_ok=True)
        shutil.copy2(ROOT/'.local/bin/linux-auto-runtime',runtime);runtime.chmod(0o755)
        settings=self.app/'config/system/settings.json';settings.write_text(json.dumps(dict(listenAddress='192.168.2.1',socksPort=2081,xray=dict(binaryPath=str(runtime)))))
        self.runtime=subprocess.Popen([str(runtime),'run','-c',str(self.current)])
        self.addCleanup(self.stop_runtime)
        fixture=self.app/'bin/pidof';fixture.write_text('#!/bin/ash\nprintf "%s\\n" '+str(self.runtime.pid)+'\n');fixture.chmod(0o755)
        (self.app/'run/connection-monitor.pid').write_text(str(os.getpid()))
        self.monitor=self.app/'run/connection-status.json';self.monitor.write_text('{"available":true,"up":false,"checked_at":1}')
        self.calls=self.temp/'active-curl-calls';self.env['TEST_ACTIVE_CALLS']=str(self.calls)
        self.env['TEST_ACTIVE_CONFIG']=str(self.current)
        ip=self.app/'bin/ip';ip.write_text('#!/bin/ash\nprintf "    inet 192.168.2.1/24\\n    inet 127.0.0.1/8\\n    inet6 ::1/128\\n"\n');ip.chmod(0o755)
        curl=self.app/'bin/curl';curl.write_text('''#!/bin/ash
case "$1" in --help) echo --socks5-hostname; exit 0 ;; esac
printf '%s\\n' "$*" >>"$TEST_ACTIVE_CALLS"
case "$*" in
  *'socks5h://192.168.2.1:2081'*)
    case "${TEST_ACTIVE_MODE:-working}" in
      timeout) exit 28 ;;
      wait) echo ready >"$TEST_READY"; trap '' TERM; sleep 60; exit 28 ;;
      second) case "$*" in *cp.cloudflare.com*) exit 28 ;; esac ;;
      redirect) printf '302'; exit 0 ;;
      change) jq '.fixtureChange=true' "$TEST_ACTIVE_CONFIG" >"$TEST_ACTIVE_CONFIG.new"; mv "$TEST_ACTIVE_CONFIG.new" "$TEST_ACTIVE_CONFIG" ;;
    esac ;;
esac
case "$*" in *'--write-out %{http_code} https:'*) printf '204' ;; *) printf '204 0.02' ;; esac
''');curl.chmod(0o755)
    def stop_runtime(self):
        if self.runtime.poll() is None:self.runtime.terminate()
        self.runtime.wait(timeout=5)
    def test_proxy_working_is_healthy_despite_icmp_down(self):
        self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'healthy')
        self.assertEqual(self.auto_state()['consecutiveFailures'],0)
        self.assertTrue(self.calls.exists(),'No active SOCKS request was made')
        self.assert_drained()
    def test_manual_check_publishes_current_proxy_health(self):
        before=self.runtime.pid
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" check '+self.server+' manual',timeout=90)
        health=json.loads(self.quality.read_text())['activeHealth']
        self.assertEqual(health['status'],'healthy')
        self.assertEqual(len(health['context']),64)
        self.assertEqual(health['serverId'],self.server)
        view=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_active_proxy_cached '+self.server+' "'+str(self.quality)+'"')
        self.assertEqual(json.loads(view.stdout)['status'],'healthy')
        self.current.write_text(self.current.read_text()+'\n')
        view=self.shell('. "$BRORAY_ROOT/lib/server-service.sh"; broray_active_proxy_cached '+self.server+' "'+str(self.quality)+'"')
        self.assertEqual(json.loads(view.stdout)['status'],'unknown')
        self.assertEqual(self.runtime.pid,before);self.assertIsNone(self.runtime.poll());self.assert_drained()
    def test_manual_check_keeps_real_proxy_failure_separate_from_candidate_quality(self):
        self.env['TEST_ACTIVE_MODE']='timeout'
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-servers" check '+self.server+' manual',timeout=90)
        quality=json.loads(self.quality.read_text())
        self.assertEqual(quality['status'],'available')
        self.assertEqual(quality['activeHealth']['status'],'unhealthy')
        self.assertIsNone(self.runtime.poll());self.assert_drained()
    def test_proxy_failure_counts_despite_icmp_up(self):
        self.env['TEST_ACTIVE_MODE']='timeout'
        self.monitor.write_text('{"available":true,"up":true,"checked_at":1}')
        self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'waiting-threshold')
        self.assertEqual(self.auto_state()['consecutiveFailures'],1)
        self.assert_drained()
    def test_proxy_uses_configured_endpoint_and_forces_proxy(self):
        self.env.update(NO_PROXY='*',HTTPS_PROXY='http://127.0.0.1:9')
        self.shell(self.once(),timeout=90)
        calls=self.calls.read_text().splitlines();self.assertEqual(len(calls),1)
        self.assertTrue(calls[0].startswith('-q '));self.assertIn('--noproxy  --proxy socks5h://192.168.2.1:2081',calls[0])
        self.assertNotIn('--location',calls[0]);self.assertEqual(self.auto_state()['activeHealth']['endpoint'],'192.168.2.1:2081')
    def test_proxy_second_target_can_confirm_health(self):
        self.env['TEST_ACTIVE_MODE']='second';self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'healthy');self.assertEqual(self.auto_state()['activeHealth']['attempts'],2)
    def test_proxy_redirect_is_not_a_connectivity_success(self):
        self.env['TEST_ACTIVE_MODE']='redirect';self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'waiting-threshold');self.assertEqual(self.auto_state()['consecutiveFailures'],1)
    def test_proxy_config_change_invalidates_result(self):
        self.env['TEST_ACTIVE_MODE']='change';self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'paused');self.assertEqual(self.auto_state()['consecutiveFailures'],0)
        self.assertEqual(self.auto_state()['activeHealth']['status'],'unknown')
    def test_proxy_ambiguous_endpoint_is_unknown(self):
        d=json.loads(self.current.read_text());d['inbounds']*=2;self.current.write_text(json.dumps(d))
        self.shell(self.once(),timeout=90);self.assertEqual(self.auto_state()['status'],'paused')
        self.assertFalse(self.calls.exists());self.assertEqual(self.auto_state()['consecutiveFailures'],0)
    def test_proxy_manual_off_does_not_probe_or_start(self):
        (self.app/'config/active-server').unlink();self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'manual-off');self.assertFalse(self.calls.exists())
    def test_proxy_stopped_runtime_does_not_probe_or_start(self):
        self.stop_runtime();self.shell(self.once(),timeout=90)
        self.assertEqual(self.auto_state()['status'],'paused');self.assertFalse(self.calls.exists())
    def test_proxy_cancel_drains_helper_without_counting_failure(self):
        self.env['TEST_ACTIVE_MODE']='wait'
        before=self.app/'run/server-auto-switch-state.json';before.write_text('{"consecutiveFailures":2,"status":"waiting-threshold"}')
        before.chmod(0o600)
        p=subprocess.Popen(['/bin/ash','-c',self.once()],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait_transport(p)
            # The scheduler may publish its quality-refresh status before the
            # active probe; cancellation must not publish this unfinished probe.
            raw=before.read_bytes();self.assertEqual(json.loads(raw)['consecutiveFailures'],2)
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+self.states()[0]['operationId'])
            out,err=self.collect(p,50);self.assertEqual(p.returncode,130,(out,err))
            self.assertEqual(before.read_bytes(),raw);self.assertEqual(self.states()[0]['state'],'aborted')
            self.assertIsNone(self.runtime.poll());self.assert_drained()
        finally:
            if p.poll() is None:p.kill();self.collect(p)

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    names=sorted(name for name in ActiveProxyHealth.__dict__ if name.startswith('test_proxy_'))
    result=unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite(ActiveProxyHealth(name) for name in names))
    (ROOT/'docs/evidence/active-proxy-health-tests.json').write_text(json.dumps(dict(
      status='PASS' if result.wasSuccessful() else 'FAIL',testsRun=result.testsRun,
      environment='Production auto-switch, native runtime identity fixture and deterministic curl transport',routerAccessed=False),indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

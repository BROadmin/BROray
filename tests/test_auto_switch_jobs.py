"""Production automatic jobs with real Linux owners and fixture transport."""
import ctypes,json,os,shutil,subprocess,time,unittest
from test_server_jobs import ServerJobs,ROOT

class AutoSwitchJobs(unittest.TestCase):
    shell=ServerJobs.shell
    collect=ServerJobs.collect
    states=ServerJobs.states
    reap_adopted_helpers=ServerJobs.reap_adopted_helpers
    def wait_transport(self,p):
        # This path publishes scheduler state and a full catalog before the
        # first probe. Keep its readiness budget separate from a single probe.
        deadline=time.monotonic()+90
        while not (self.temp/'transport-ready').exists():
            if p.poll() is not None:self.fail((p.returncode,*p.communicate()))
            if time.monotonic()>deadline:
                self.fail(('automatic probe did not start',self.states(),
                  [(str(f.relative_to(self.app)),f.read_text(errors='replace')[-4000:]) for f in (self.app/'tmp').glob('*.err')]))
            time.sleep(.05)
    assert_drained=ServerJobs.assert_drained
    old_quality=ServerJobs.old_quality
    def setUp(self):
        ServerJobs.setUp(self)
        # The old daemon's Entware PATH omits Alpine /usr/bin. Put required
        # utilities in the private app/bin so a missing jq cannot fake a pass.
        for name in ['jq','sha256sum','awk','hexdump','tail','cut','wc','tr','date','sed']:
            target=self.app/'bin'/name
            if not target.exists():target.symlink_to(shutil.which(name))
        self.env.update({'BRORAY_GLOBAL_LOCK':str(self.temp/'global.lock'),
          'BRORAY_UPDATER_LOCK':str(self.temp/'updater/request.lock'),
          'BRORAY_SYSTEM_LOCK':str(self.temp/'legacy.lock')})
        self.config=self.app/'config/system/server-auto-switch.json'
        (self.app/'run').mkdir(exist_ok=True)
        self.config.write_text(json.dumps({'enabled':False,'failureThreshold':3,'cooldownMinutes':10,
          'minimumRating':'acceptable','selectionRule':'best-quality','preferredServerId':None,
          'qualityRefreshEnabled':False,'qualityRefreshIntervalMinutes':60}))
    def once(self):return '"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-server-auto-switch" --once'
    def set_config(self,**fields):
        self.config.write_text(json.dumps(json.loads(self.config.read_text())|fields))
    def auto_state(self):return json.loads((self.app/'run/server-auto-switch-state.json').read_text())
    def active_runtime_fixture(self):
        self.current=self.app/'config/config.json'
        self.current.write_text('{"inbounds":[{"protocol":"socks","listen":"127.0.0.1","port":2080,"settings":{"auth":"noauth"}}],"outbounds":[{"protocol":"blackhole"}]}')
        runtime=self.app/'runtime/xray';runtime.parent.mkdir(exist_ok=True)
        shutil.copy2(ROOT/'.local/bin/linux-auto-runtime',runtime);runtime.chmod(0o755)
        settings=self.app/'config/system/settings.json';settings.write_text(json.dumps(
          json.loads(settings.read_text())|{'xray':{'binaryPath':str(runtime)}}))
        process=subprocess.Popen([str(runtime),'run','-c',str(self.app/'config/config.json')])
        def stop_runtime():
            if process.poll() is None:process.terminate()
            process.wait(timeout=5)
        self.addCleanup(stop_runtime)
        fixture=self.app/'bin/pidof';fixture.write_text('#!/bin/ash\nprintf "%s\\n" '+str(process.pid)+'\n');fixture.chmod(0o755)
        ip=self.app/'bin/ip';ip.write_text('#!/bin/ash\nprintf "    inet 127.0.0.1/8\\n"\n');ip.chmod(0o755)
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
  *'--proxy socks5h://127.0.0.1:2080'*)
    [ "${TEST_ACTIVE_MODE:-timeout}" = working ] || exit 28
    printf 204; exit 0 ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))
    def test_paused_auto_switch_does_not_run_cycle(self):
        self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call pause')
        p=self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-server-auto-switch" --once')
        self.assertNotIn(b'not found',p.stderr)
        self.assertFalse((self.app/'run/server-auto-switch-state.json').exists(),'Paused automatic cycle still published business state')
        self.assertEqual(self.states(),[])
    def test_disabled_state_written_once_by_child_job(self):
        p=subprocess.Popen(['/bin/ash','-c',self.once()],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        out,err=self.collect(p,75);self.assertEqual(p.returncode,0,(out,err))
        state=self.states()[0];owner=json.loads((self.state/'operations'/state['operationId']/'owner.json').read_text())['owner']
        self.assertNotEqual(owner['pid'],p.pid)
        self.assertEqual(state['state'],'completed');self.assertEqual(self.auto_state()['backgroundOperationId'],state['operationId'])
        self.assertFalse((self.app/'run/server-auto-switch-cycle.lock').exists());self.assert_drained()
        before=(self.app/'run/server-auto-switch-state.json').read_bytes()
        self.shell(self.once())
        self.assertEqual(len(self.states()),1)
        self.assertEqual((self.app/'run/server-auto-switch-state.json').read_bytes(),before)
    def test_due_quality_refresh_has_one_owner_and_no_nested_admission(self):
        self.set_config(qualityRefreshEnabled=True);self.old_quality()
        self.shell(self.once(),timeout=120)
        self.assertEqual(len(self.states()),1)
        state=self.states()[0];self.assertEqual(state['state'],'completed')
        self.assertEqual(state['source'],'SERVER_CHECK_AUTO')
        quality=self.auto_state()['qualityRefresh']
        self.assertEqual(quality['status'],'success');self.assertEqual(quality['availableCount'],1)
        self.assertEqual(quality['checkedCount'],1);self.assertEqual(quality['errorCount'],0)
        self.assertEqual(json.loads(self.quality.read_text())['successfulChecks'],8);self.assert_drained()
        self.shell(self.once());self.assertEqual(len(self.states()),1)
    def test_unavailable_server_is_complete_negative_measurement(self):
        self.set_config(qualityRefreshEnabled=True);self.env['TEST_MODE']='error';self.old_quality()
        self.shell(self.once(),timeout=120)
        quality=self.auto_state()['qualityRefresh']
        self.assertEqual(quality['status'],'success');self.assertEqual(quality['unavailableCount'],1)
        self.assertEqual(quality['errorCount'],0);self.assert_drained()
    def test_cancel_stops_quality_loop_before_another_server(self):
        self.set_config(qualityRefreshEnabled=True);self.env['TEST_MODE']='wait';before=self.old_quality()
        p=subprocess.Popen(['/bin/ash','-c',self.once()],env=self.env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            self.wait_transport(p);state=self.states()[0]
            self.shell('. "$BRORAY_ROOT/lib/operation-client.sh"; broray_ops_call cancel '+state['operationId'])
            out,err=self.collect(p,50);self.assertEqual(p.returncode,130,(out,err))
            self.assertEqual(self.quality.read_bytes(),before)
            self.assertEqual(self.states()[0]['state'],'aborted');self.assert_drained()
            cache=self.app/'run/server-auto-switch-state.json';cached=cache.read_bytes()
            self.assertEqual(json.loads(cached)['qualityRefresh']['status'],'running')
            view=self.shell('. "$BRORAY_ROOT/lib/auto-switch-status.sh"; broray_auto_switch_public_state "$BRORAY_ROOT/run/server-auto-switch-state.json"')
            projected=json.loads(view.stdout)
            self.assertEqual(projected['backgroundOperationState'],'aborted')
            self.assertEqual(projected['qualityRefresh']['status'],'error')
            self.assertEqual(cache.read_bytes(),cached,'Status projection mutated business state')
        finally:
            if p.poll() is None:p.kill();self.collect(p)
    def test_legacy_cycle_lock_is_preserved(self):
        lock=self.app/'run/server-auto-switch-cycle.lock';lock.mkdir(parents=True)
        (lock/'foreign').write_text('KEEP')
        self.shell(self.once(),expected=2)
        self.assertEqual((lock/'foreign').read_text(),'KEEP');self.assertEqual(self.states(),[])
    def test_enabled_auto_switch_keeps_manual_vpn_off(self):
        self.set_config(enabled=True)
        self.shell(self.once())
        self.assertEqual(self.auto_state()['status'],'manual-off')
        self.assertFalse((self.app/'config/active-server').exists())
        self.assertEqual(self.states()[0]['source'],'AUTO_SWITCH');self.assert_drained()
    def test_candidate_selection_dry_run_uses_single_owned_job(self):
        self.set_config(enabled=True,failureThreshold=1)
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        (self.app/'run/connection-monitor.pid').write_text(str(os.getpid()))
        # Use the actual current monitor shape, not the legacy boolean alone.
        (self.app/'run/connection-status.json').write_text('{"available":true,"up":false,"checked_at":1,"packet_loss_percent":100}')
        # Real runtime identity with deterministic proxy transport; no real VPN.
        self.active_runtime_fixture()
        self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-server-auto-switch" --dry-run',timeout=120)
        state=self.auto_state();self.assertEqual(state['status'],'dry-run')
        self.assertEqual(state['candidateCount'],1)
        self.assertEqual((self.app/'config/active-server').read_text(),self.server+'\n')
        self.assertEqual(len(self.states()),1);self.assertEqual(self.states()[0]['state'],'completed')
        self.assertTrue((self.app/'run/server-quality/subscription-second-0000.json').exists());self.assert_drained()

    def test_proxy_failures_reach_threshold_and_success_resets(self):
        self.set_config(enabled=True,failureThreshold=3)
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        (self.app/'run/connection-monitor.pid').write_text(str(os.getpid()))
        self.active_runtime_fixture()
        monitor=self.app/'run/connection-status.json'
        def cycle(mode='timeout'):
            self.env['TEST_ACTIVE_MODE']=mode
            # Leave the ICMP snapshot unchanged across all proxy measurements.
            monitor.write_text('{"available":true,"up":true,"checked_at":1}')
            self.shell('"$BRORAY_OPS_ASH" "$BRORAY_ROOT/bin/broray-server-auto-switch" --dry-run',timeout=150)
            return self.auto_state()
        first=cycle();self.assertEqual(first['status'],'waiting-threshold');self.assertEqual(first['consecutiveFailures'],1)
        second=cycle();self.assertEqual(second['consecutiveFailures'],2)
        third=cycle();self.assertEqual(third['status'],'dry-run');self.assertEqual(third['candidateCount'],1)
        up=cycle('working');self.assertEqual(up['status'],'healthy');self.assertEqual(up['consecutiveFailures'],0)
        self.assertEqual((self.app/'config/active-server').read_text(),self.server+'\n');self.assert_drained()

    def test_due_quality_never_precedes_failed_active_health(self):
        self.set_config(enabled=True,failureThreshold=3,qualityRefreshEnabled=True)
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.active_runtime_fixture()
        before=self.old_quality()
        self.shell(self.once(),timeout=150)
        state=self.auto_state()
        self.assertEqual(state['status'],'waiting-threshold')
        self.assertEqual(state['consecutiveFailures'],1)
        self.assertEqual(self.quality.read_bytes(),before,
          'An overdue quality scan ran before the failed active connection was handled')
        self.assertEqual(len(self.states()),1);self.assert_drained()

    def test_active_failure_interrupts_quality_before_next_server(self):
        self.set_config(enabled=True,failureThreshold=3,qualityRefreshEnabled=True)
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.active_runtime_fixture();self.env['TEST_ACTIVE_MODE']='working'
        self.env['TEST_QUALITY_FINISHED']=str(self.temp/'quality-finished')
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
  *'--proxy socks5h://127.0.0.1:2080'*)
    if [ -e "$TEST_QUALITY_FINISHED" ] && [ "${TEST_HEALTH_RECOVERED:-no}" != yes ]; then
      export TEST_ACTIVE_MODE=timeout
    fi ;;
  *https://*) touch "$TEST_QUALITY_FINISHED" ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))
        self.shell(self.once(),timeout=180)
        state=self.auto_state();quality=state['qualityRefresh']
        self.assertEqual(state['status'],'waiting-threshold')
        self.assertEqual(state['consecutiveFailures'],1)
        self.assertEqual(quality['checkedCount'],1,'A second quality probe delayed active VPN recovery')
        self.assertEqual(quality['totalCount'],2)
        self.assertEqual(quality['status'],'paused')
        self.assertIsNone(quality['lastCompletedAt'],'An interrupted batch is not complete')
        self.assert_drained()
        self.env['TEST_HEALTH_RECOVERED']='yes'
        self.shell(self.once(),timeout=180)
        state=self.auto_state();quality=state['qualityRefresh']
        self.assertEqual(state['status'],'healthy')
        self.assertEqual(state['consecutiveFailures'],0)
        self.assertEqual(quality['status'],'success')
        self.assertEqual(quality['checkedCount'],2);self.assertEqual(quality['totalCount'],2)
        self.assert_drained()

    def test_active_recovery_during_candidate_probe_prevents_switch(self):
        self.set_config(enabled=True,failureThreshold=1)
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        self.active_runtime_fixture()
        self.env['TEST_RECOVERED']=str(self.temp/'active-recovered')
        curl=self.app/'bin/curl';original=curl.read_text()
        curl.write_text('''#!/bin/ash
case "$*" in
  *'--proxy socks5h://127.0.0.1:2080'*)
    [ ! -f "$TEST_RECOVERED" ] || export TEST_ACTIVE_MODE=working ;;
  *https://*) touch "$TEST_RECOVERED" ;;
esac
'''+original.removeprefix('#!/bin/ash\n'))
        self.shell(self.once(),timeout=150)
        state=self.auto_state();self.assertEqual(state['status'],'recovered',state)
        self.assertEqual(state['consecutiveFailures'],0)
        self.assertEqual(state['activeHealth']['status'],'healthy')
        self.assertEqual((self.app/'config/active-server').read_text(),self.server+'\n')
        self.assertEqual(json.loads(self.current.read_text())['outbounds'][0]['protocol'],'blackhole')
        self.assertFalse((self.app/'restarts').exists());self.assert_drained()

    def test_down_monitor_reaches_activation_and_enters_cooldown(self):
        self.set_config(enabled=True,failureThreshold=1,cooldownMinutes=10)
        self.shell('. "$BRORAY_ROOT/lib/server-import.sh"; broray_server_import_dispatch "$(cat "$TEST_PAYLOAD")" subscription second 0')
        (self.app/'config/active-server').write_text(self.server+'\n')
        (self.app/'run/connection-monitor.pid').write_text(str(os.getpid()))
        monitor=self.app/'run/connection-status.json'
        monitor.write_text('{"available":true,"up":false,"checked_at":1}')
        self.active_runtime_fixture()
        # Keep the real coordinator, candidate probe, config generation and
        # protected activation. Only runtime liveness/transport/init are fake.
        init=self.app/'bin/fixture-init';init.write_text('''#!/bin/ash
jq -e '.phase=="committing" and .cancelability=="protected"' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/state.json" >/dev/null || exit 91
jq -e '.supervisors==[]' "$BRORAY_STATE_ROOT/operations/$BRORAY_BACKGROUND_OPERATION_ID/supervisors.json" >/dev/null || exit 92
jq -r '.inbounds[0].port' "$BRORAY_ROOT/config/config.json" >"$TEST_PORT"
echo restart >>"$BRORAY_ROOT/restarts"
''');init.chmod(0o755);self.env['BRORAY_INIT']=str(init)
        xray=self.app/'bin/fixture-xray'
        xray.write_text(xray.read_text().replace('case " $* " in', 'case "$1" in version) echo "Xray 26.9.9 (fixture)"; exit 0 ;; esac\ncase " $* " in'))
        try:
            self.shell(self.once(),timeout=180)
        except AssertionError as error:
            self.fail((str(error),self.auto_state(),self.states(),
              [(str(p.relative_to(self.app)),p.read_text(errors='replace')[-3000:])
               for p in self.app.rglob('*') if p.is_file() and
               (p.suffix in ('.err','.log') or p.name=='restarts')]))
        state=self.auto_state();self.assertEqual(state['status'],'switched',state)
        self.assertIsNone(state['activeHealth'],'Previous server health must not describe the newly activated server')
        self.assertEqual(state['lastSwitchFrom'],self.server);self.assertEqual(state['lastSwitchTo'],'subscription-second-0000')
        self.assertEqual((self.app/'config/active-server').read_text().strip(),'subscription-second-0000')
        self.assertEqual(json.loads((self.app/'config/config.json').read_text())['outbounds'][0]['protocol'],'vless')
        self.assertEqual((self.app/'restarts').read_text(),'restart\n');self.assert_drained()
        # Isolate the cooldown assertion from the separate 60-second attempt guard.
        cache=self.app/'run/server-auto-switch-state.json';d=json.loads(cache.read_text());d['lastAttemptEpoch']=0;cache.write_text(json.dumps(d))
        monitor.write_text('{"available":true,"up":false,"checked_at":2}')
        self.shell(self.once(),timeout=75);self.assertEqual(self.auto_state()['status'],'cooldown')
        self.assertEqual((self.app/'restarts').read_text(),'restart\n');self.assert_drained()

if __name__=='__main__':
    assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(AutoSwitchJobs))
    (ROOT/'docs/evidence/auto-switch-jobs-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'production auto-switch and server services, isolated Linux processes','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

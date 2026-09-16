"""Bounded, read-only diagnostic facts with secret and malformed-input canaries."""
import json,shutil,subprocess,time,unittest
from pathlib import Path
from test_operations import Operations,WORKSPACE
ROOT=WORKSPACE
class Diagnostics(unittest.TestCase):
    def setUp(self):
        self.ops=Operations('runTest');self.ops.setUp();self.temp=self.ops.temp
        self.app=self.temp/'app';shutil.copytree(ROOT/'implementation/runtime/app',self.app)
        self.auto=self.app/'config/system/server-auto-switch.json';self.auto.parent.mkdir(parents=True)
        self.auto.write_text('{"enabled":false,"qualityRefreshEnabled":false}\n')
        self.subs=self.app/'config/subscriptions';self.subs.mkdir()
        self.ops.env['BRORAY_ROOT']=self.app.as_posix();self.active=self.ops.begin()
    def tearDown(self):
        # All calls are synchronous and use synthetic owners. No daemon or
        # helper was admitted from this private fixture.
        assert self.temp.resolve().parent==(ROOT/'.local').resolve()
        shutil.rmtree(self.temp)
    def report(self):return self.ops.call('report')
    def cache(self,module,data,age=0):
        folder=self.app/'run/home-snapshots';folder.mkdir(parents=True,exist_ok=True)
        path=folder/(module+'.json')
        epoch=int(time.time())-age
        path.write_text(json.dumps({'schemaVersion':1,'module':module,'capturedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime(epoch)),'capturedEpoch':epoch,'data':data}))
        return path
    def test_all_five_service_states_are_present(self):
        report=self.report()
        self.assertEqual({v['service'] for v in report['serviceDetails']},{'subscriptions','auto-switch','connection-monitor','home-snapshot','interface-reconcile'})
        self.assertNotIn('serviceStates',report['unavailable'])
    def test_new_service_ambiguous_identity_is_reported_without_mutation(self):
        run=self.app/'run';run.mkdir(exist_ok=True)
        pid=run/'home-snapshotd.pid';pid.write_text('99999999\n')
        report=self.report();view=next(v for v in report['serviceDetails'] if v['service']=='home-snapshot')
        self.assertFalse(view['complete']);self.assertEqual(view['state'],'ambiguous')
        self.assertIn('serviceStates',report['unavailable']);self.assertEqual(pid.read_text(),'99999999\n')
    def test_cached_running_and_pid_are_never_live_identity(self):
        self.cache('xray',{'running':True,'pid':99999999,'version':'26.9.9','configCheckOutput':'PRIVATE_CANARY'})
        report=self.report();view=report['runtimeDetails']['xray']
        self.assertEqual(view['state'],'unknown');self.assertIsNone(view['identity'])
        self.assertEqual(view['cachedVersion'],'26.9.9');self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
    def test_runtime_identity_excludes_validator_and_wrong_config(self):
        binary=self.app/'runtime/xray';binary.parent.mkdir();shutil.copy2(ROOT/'.local/bin/linux-diagnostic-runtime',binary);binary.chmod(0o755)
        config=self.app/'config/config.json';config.write_text('{}')
        for args,expected in [(['run','-c',str(config)],'running'),(['run','-test','-c',str(config)],'unknown'),(['run','-c',str(config)+'.other'],'unknown')]:
            with self.subTest(args=args):
                child=subprocess.Popen([str(binary),*args])
                try:
                    self.cache('xray',{'pid':child.pid,'running':True,'version':'26.9.9'})
                    view=self.report()['runtimeDetails']['xray'];self.assertEqual(view['state'],expected)
                    if expected=='running':
                        self.assertTrue(view['identity']['verified']);self.assertEqual(view['identity']['pid'],child.pid)
                        self.assertNotIn('commandDigest',json.dumps(view));self.assertNotIn('bootId',json.dumps(view))
                    else:self.assertIsNone(view['identity'])
                    self.assertIsNone(child.poll())
                finally:child.terminate();child.wait(timeout=3)
    def test_expired_future_and_unsafe_caches_are_unknown_and_preserved(self):
        for age in [601,-60]:
            path=self.cache('xray',{'pid':99999999,'version':'26.9.9'},age);before=path.read_bytes()
            view=self.report()['runtimeDetails']['xray'];self.assertIsNone(view['cache']);self.assertEqual(path.read_bytes(),before)
        path.unlink();foreign=self.temp/'foreign-cache';foreign.write_text('PRIVATE_CANARY');path.symlink_to(foreign)
        report=self.report();self.assertIsNone(report['runtimeDetails']['xray']['cache']);self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
        self.assertTrue(path.is_symlink());self.assertEqual(foreign.read_text(),'PRIVATE_CANARY')
    def test_cached_updater_is_projected_without_live_claim_or_raw_error(self):
        data={'operationId':'update-20260916001050-30079','operation':'update','state':'success','running':False,'updatedAt':'2026-09-16T00:15:00Z','error':{'nested':'PRIVATE_CANARY'},'message':'PRIVATE_CANARY','rollbackPerformed':False}
        self.cache('broray',{'lastOperation':data})
        report=self.report();view=report['runtimeDetails']['updater']
        self.assertEqual(view['lastOperation']['state'],'success');self.assertTrue(view['complete'])
        self.assertEqual(view['liveState'],'unknown');self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
        self.assertIn('updaterLiveIdentity',report['unavailable']);self.assertFalse(report['snapshotConsistent'])
    def test_multidocument_and_invalid_updater_cache_have_no_raw_fallback(self):
        for data in [{'operationId':'PRIVATE_CANARY','state':'success','running':False},{'operationId':'update-20260916001050-30079','state':'PRIVATE_CANARY','running':False}]:
            self.cache('broray',{'lastOperation':data});report=self.report()
            self.assertIsNone(report['runtimeDetails']['updater']['lastOperation']);self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
        path=self.cache('xray',{'version':'26.9.9'});path.write_text(path.read_text()+'\n{}')
        self.assertIsNone(self.report()['runtimeDetails']['xray']['cache'])
    def test_known_disabled_automation_and_empty_subscriptions(self):
        report=self.report()
        self.assertEqual(report['automation'],{'paused':False,'autoSwitch':False,'serverCheck':False,'subscriptionUpdate':False})
        self.assertTrue(report['automationDetails']['complete'])
        self.assertNotIn('automationSettings',report['unavailable'])
    def test_enabled_subscription_reports_counts_without_secrets(self):
        self.auto.write_text('{"enabled":true,"qualityRefreshEnabled":true,"name":"PRIVATE_CANARY"}')
        for i,enabled in enumerate([True,False]):
            (self.subs/f'{i}.json').write_text(json.dumps({'enabled':enabled,'autoUpdateEnabled':True,'url':'https://PRIVATE_CANARY/secret','clientHwid':'PRIVATE_CANARY'}))
        report=self.report();raw=json.dumps(report)
        self.assertTrue(report['automation']['subscriptionUpdate']);self.assertTrue(report['automation']['autoSwitch'])
        self.assertEqual(report['automationDetails']['automaticSubscriptions'],1)
        self.assertEqual(report['automationDetails']['subscriptionRecordsRead'],2)
        self.assertNotIn('PRIVATE_CANARY',raw)
    def test_malformed_or_multiple_documents_are_unknown(self):
        self.auto.write_text('{"enabled":false,"qualityRefreshEnabled":false}\n{}')
        (self.subs/'bad.json').write_text('PRIVATE_CANARY')
        report=self.report();self.assertIsNone(report['automation']['autoSwitch'])
        self.assertIsNone(report['automation']['subscriptionUpdate']);self.assertFalse(report['automationDetails']['complete'])
        self.assertIn('AUTOMATION_SETTINGS_UNAVAILABLE',report['errors'])
        self.assertIn('SUBSCRIPTION_SETTINGS_UNAVAILABLE',report['errors'])
        self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
    def test_unsafe_subscription_and_oversized_settings_are_preserved(self):
        foreign=self.temp/'foreign';foreign.write_text('PRIVATE_CANARY')
        (self.subs/'link.json').symlink_to(foreign)
        self.auto.write_text(' '*32769);before=self.auto.read_bytes()
        report=self.report();self.assertIsNone(report['automation']['serverCheck'])
        self.assertIsNone(report['automation']['subscriptionUpdate'])
        self.assertEqual(self.auto.read_bytes(),before);self.assertEqual(foreign.read_text(),'PRIVATE_CANARY')
        self.assertTrue((self.subs/'link.json').is_symlink())
    def test_subscription_record_limit_is_explicit(self):
        for i in range(257):(self.subs/f'{i:03}.json').write_text('{"enabled":false,"autoUpdateEnabled":false}')
        report=self.report();self.assertIsNone(report['automation']['subscriptionUpdate'])
        self.assertEqual(report['automationDetails']['subscriptionRecordsRead'],0)
        self.assertIn('SUBSCRIPTION_SETTINGS_UNAVAILABLE',report['errors'])
    def test_service_legacy_identity_is_not_reported_stopped(self):
        run=self.app/'run';run.mkdir(exist_ok=True)
        pid=run/'connection-monitor.pid';pid.write_text('99999999\n')
        report=self.report();view=next(s for s in report['serviceDetails'] if s['service']=='connection-monitor')
        self.assertFalse(view['complete']);self.assertIsNone(view['running']);self.assertEqual(view['state'],'ambiguous')
        self.assertEqual(pid.read_text(),'99999999\n')
    def test_service_symlink_is_preserved_without_raw_fallback(self):
        services=self.ops.state/'services';services.mkdir()
        foreign=self.temp/'private-service';foreign.mkdir();(foreign/'identity.json').write_text('PRIVATE_CANARY')
        (services/'subscriptions').symlink_to(foreign)
        report=self.report();view=next(s for s in report['serviceDetails'] if s['service']=='subscriptions')
        self.assertFalse(view['complete']);self.assertNotIn('PRIVATE_CANARY',json.dumps(report))
        self.assertEqual((foreign/'identity.json').read_text(),'PRIVATE_CANARY')
    def test_report_preserves_registry_and_containing_owner_context(self):
        def snapshot():return {p.relative_to(self.temp).as_posix():p.read_bytes() for p in self.temp.rglob('*') if p.is_file() and not p.is_symlink()}
        before=snapshot();report=self.report();self.assertEqual(snapshot(),before)
        self.assertEqual(report['fences']['global'],'managed_active')
        self.assertTrue(report['snapshotComplete'])
        for secret in [self.active['token'],self.ops.launch,self.ops.owner['commandDigest']]:self.assertNotIn(secret,json.dumps(report))
    def test_kernel_is_present_but_vpn_is_not_inferred(self):
        report=self.report();self.assertIsInstance(report['platform']['kernel'],str)
        self.assertNotIn('kernel',report['unavailable']);self.assertIn('vpnContinuity',report['unavailable'])
        self.assertFalse(report['complete'])
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Diagnostics))
    (ROOT/'docs/evidence/diagnostics-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'environment':'Linux native guard, synthetic operation owner, actual read-only service classifier and private config','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

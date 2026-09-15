"""Bounded, read-only diagnostic facts with secret and malformed-input canaries."""
import json,shutil,unittest
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

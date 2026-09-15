"""The actual S24 stop entry must reject ambiguity before calling rc.func."""
import json,shutil,subprocess,tempfile,unittest
from pathlib import Path
from test_service_lifecycle import ROOT
class S24Admission(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='s24-admission-'));self.app=self.temp/'app'
        shutil.copytree(ROOT/'implementation/runtime/app',self.app)
        for name in ['run','logs','tmp']:(self.app/name).mkdir(exist_ok=True)
        self.state=self.temp/'state';self.rc=self.temp/'rc.func';self.marker=self.temp/'xray-stop-reached'
        self.rc.write_text('echo called >"'+str(self.marker)+'"\nACTION=stop\n')
        text=(ROOT/'implementation/runtime/init/S24broray').read_text()
        text=text.replace('/opt/broray/current/app',str(self.app)).replace('/opt/broray',str(self.app)).replace('/opt/etc/init.d/rc.func',str(self.rc))
        self.script=self.temp/'S24broray';self.script.write_text(text)
        import os
        self.env=os.environ|{'BRORAY_ROOT':str(self.app),'BRORAY_STATE_ROOT':str(self.state),'BRORAY_OPS_GUARD':str(ROOT/'.local/bin/linux-guard'),'BRORAY_OPS_ASH':'/bin/ash'}
    def tearDown(self):
        assert self.temp.parent==Path('/tmp') and self.temp.name.startswith('s24-admission-');shutil.rmtree(self.temp)
    def call(self):return subprocess.run(['/bin/ash',str(self.script),'stop'],env=self.env,capture_output=True,timeout=25)
    def test_unknown_home_owner_prevents_xray_stop(self):
        pid=self.app/'run/home-snapshotd.pid';pid.write_text('99999999\n')
        p=self.call();self.assertEqual(p.returncode,75,(p.stdout,p.stderr));self.assertFalse(self.marker.exists())
        self.assertEqual(pid.read_text(),'99999999\n')
    def test_unknown_reconcile_owner_prevents_xray_stop(self):
        pid=self.app/'run/interface-reconcile.pid';pid.write_text('99999999\n')
        p=self.call();self.assertEqual(p.returncode,75,(p.stdout,p.stderr));self.assertFalse(self.marker.exists())
        self.assertEqual(pid.read_text(),'99999999\n')
    def test_confirmed_absent_sidecars_allow_runtime_stop(self):
        p=self.call();self.assertEqual(p.returncode,0,(p.stdout,p.stderr));self.assertTrue(self.marker.exists())
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(S24Admission))
    (ROOT/'docs/evidence/s24-admission-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'environment':'actual S24 entry, isolated paths and harmless rc.func sentinel','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)

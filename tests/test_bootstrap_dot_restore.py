import json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
class BootstrapDot(unittest.TestCase):
    def setUp(self):
        self.t=Path(tempfile.mkdtemp(prefix='bootstrap-dot-'));(self.t/'routes/dot').mkdir(parents=True);(self.t/'bin').mkdir();(self.t/'tmp').mkdir()
        self.config=self.t/'routes/dot/config.json';self.config.write_text(json.dumps({'managed':[{'id':'fixture'}],'requestedIds':['fixture']}))
        self.cli=self.t/'bin/broray-routes-dot';self.cli.write_text('''#!/bin/ash
if [ "$1" = apply ]; then
 [ "$BRORAY_DOT_RESTORE_EXACT" = true ] || exit 2
 jq -e '.serverIds==["fixture"] and .allowUntested==false' "$2" >/dev/null || exit 3
 echo apply >>"$BRORAY_SETUP_TARGET/calls"
 [ "${FAIL_APPLY:-0}" = 0 ] || exit 4
else
 echo '{"recoveryRequired":false,"runningConfigAvailable":true,"drift":false,"matchesSelection":true,"requestedIds":["fixture"],"managed":[{"id":"fixture"}],"managedPresentCount":1}'
fi
''');self.cli.chmod(0o700)
        self.env=os.environ|{'BRORAY_SETUP_TARGET':str(self.t),'TMP':str(self.t/'tmp'),'preserved_stage':'verified'}
    def tearDown(self):shutil.rmtree(self.t)
    def run_restore(self):
        return subprocess.run(['/bin/ash','-c',f'. "{ROOT}/implementation/bootstrap/restore-preserved-dot.sh"; broray_bootstrap_restore_preserved_dot'],env=self.env,capture_output=True,timeout=10)
    def test_restores_through_manager_with_exact_receipt(self):self.assertEqual(self.run_restore().returncode,0);self.assertEqual((self.t/'calls').read_text(),'apply\n')
    def test_fresh_install_does_not_apply(self):self.env.pop('preserved_stage');self.assertEqual(self.run_restore().returncode,0);self.assertFalse((self.t/'calls').exists())
    def test_empty_managed_selection_is_preserved(self):self.config.write_text('{"managed":[],"requestedIds":["fixture"]}');self.assertEqual(self.run_restore().returncode,0);self.assertFalse((self.t/'calls').exists())
    def test_corrupt_config_fails_before_mutation(self):self.config.write_text('{bad');self.assertNotEqual(self.run_restore().returncode,0);self.assertFalse((self.t/'calls').exists())
    def test_failed_apply_fails_installation(self):self.env['FAIL_APPLY']='1';self.assertNotEqual(self.run_restore().returncode,0)
    def test_incomplete_live_restore_fails_installation(self):self.cli.write_text(self.cli.read_text().replace('"managedPresentCount":1','"managedPresentCount":0'));self.assertNotEqual(self.run_restore().returncode,0)
    def test_symlinked_config_is_rejected(self):self.config.unlink();self.config.symlink_to(self.t/'absent');self.assertNotEqual(self.run_restore().returncode,0)
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(BootstrapDot))
    (ROOT/'docs/evidence/bootstrap-dot-restore-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'dotManagerFixture':True,'routerAccessed':False})+'\n');raise SystemExit(0 if r.wasSuccessful() else 1)

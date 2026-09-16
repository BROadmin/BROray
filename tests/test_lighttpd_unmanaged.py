"""Compact bootstrap leaves shared Entware Lighttpd outside BROray ownership."""
import hashlib,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
class UnmanagedLighttpd(unittest.TestCase):
    def setUp(self):
        self.temp=Path(tempfile.mkdtemp(prefix='broray-lighttpd-'))
        self.info=self.temp/'info';self.info.mkdir()
        self.control=self.info/'broray.control'
        self.control.write_text('Package: broray\nX-BROray-Canonical-Lifecycle: compact-app-rename/1\nX-BROray-Distribution-Role: metadata-only-clean-bootstrap\n')
        self.init=self.temp/'S80lighttpd';self.init.write_text('ENABLED=yes\n')
        self.guard=self.temp/'guard'
        self.env=os.environ|{'BRORAY_LIGHTTPD_GUARD_ROOT':str(self.guard),'BRORAY_LIGHTTPD_INFO_ROOT':str(self.info),'BRORAY_LIGHTTPD_INIT':str(self.init)}
    def tearDown(self):shutil.rmtree(self.temp)
    def run_guard(self,action):
        # Asset validation has its own physical test. Here observe dispatch:
        # neither shared service stop, adoption nor package removal is allowed.
        setup=f'. "{ROOT}/implementation/runtime/app/lib/lighttpd-guard.sh"\n'+'''
broray_lighttpd_guard_assets_valid() { [ "${BAD_ASSETS:-0}" = 0 ]; }
broray_lighttpd_guard_known_legacy_adopt() { return 1; }
broray_lighttpd_guard_maintain() { echo MUTATION; return 1; }
broray_lighttpd_guard_stop_default() { echo MUTATION; return 1; }
opkg() { echo MUTATION; return 1; }
'''
        before={str(p):p.read_bytes() for p in self.temp.rglob('*') if p.is_file()}
        p=subprocess.run(['/bin/ash','-c',setup+'broray_lighttpd_guard_'+action],env=self.env,capture_output=True,timeout=10)
        self.assertNotIn(b'MUTATION',p.stdout)
        self.assertEqual(before,{str(p):p.read_bytes() for p in self.temp.rglob('*') if p.is_file()})
        return p.returncode
    def test_compact_shared_dependency_is_preserved(self):
        self.assertEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_existing_malformed_receipt_does_not_become_unmanaged(self):
        self.guard.mkdir();(self.guard/'receipt').write_text('broken\n')
        self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_dangling_guard_symlink_is_not_absence(self):
        self.guard.symlink_to(self.temp/'missing')
        self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_disabled_init_without_receipt_remains_ambiguous(self):
        self.init.write_text('ENABLED=no\n')
        self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_other_or_duplicate_package_contract_is_rejected(self):
        for text in ['Package: broray\n',self.control.read_text()+'X-BROray-Canonical-Lifecycle: compact-app-rename/1\n']:
            self.control.write_text(text)
            self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_changed_assets_are_not_accepted(self):
        self.env['BAD_ASSETS']='1'
        self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
    def test_symlinked_package_control_is_rejected(self):
        data=self.control.read_bytes();self.control.unlink();target=self.temp/'control';target.write_bytes(data);self.control.symlink_to(target)
        self.assertNotEqual(self.run_guard('uninstall_unmanaged'),0)
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(UnmanagedLighttpd))
    (ROOT/'docs/evidence/lighttpd-unmanaged-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'assetValidatorStubbed':True,'routerAccessed':False})+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)

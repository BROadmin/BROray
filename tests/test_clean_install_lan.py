"""Run the real clean postinst pre-mutation boundary after build-time integration."""
import hashlib,importlib.util,json,os,subprocess,unittest
from pathlib import Path
from test_install_lan_selection import InstallLan,ROOT

class CleanLan(unittest.TestCase):
    fixture=InstallLan.fixture;script=InstallLan.script
    interactive=InstallLan.interactive;run_plain=InstallLan.run_plain;setup_body=InstallLan.setup_body
    def setUp(self):
        InstallLan.setUp(self)
        # Device snapshots belong to the fixture, not to the staged app root
        # selected by the real bootstrap before it creates /opt/broray.
        self.env['LAN_FIXTURE_ROOT']=str(self.app)
        for name in ['ndmc','ip']:
            p=self.app/'bin'/name;p.write_text(p.read_text().replace('$BRORAY_ROOT/','$LAN_FIXTURE_ROOT/'))
    def adapted(self):
        path=ROOT/'scripts/prepare-clean-lan-selection.py'
        self.assertTrue(path.is_file(),'Clean bootstrap still rejects multiple private networks before package-setup')
        spec=importlib.util.spec_from_file_location('lan_adapter',path);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
        original=Path('/work/clean-postinst').read_bytes()
        return original,m.integrate(original,hashlib.sha256(original).hexdigest()),m
    def preflight(self,adapted):
        text=adapted.decode();a=text.index('mkdir -m 700 "$TMP/network"');b=text.index('# The immutable downloads',a)
        return '''\nfail(){ echo "$*" >&2;exit 1; }
APP_TREE="$BRORAY_ROOT/bundle";TMP="$BRORAY_ROOT/staging"
mkdir -p "$APP_TREE/app/lib" "$TMP"
cp "$BRORAY_ROOT/lib/network.sh" "$APP_TREE/app/lib/network.sh"
'''+text[a:b].replace('/opt/bin/ash','/bin/ash')
    def test_clean_preflight_selection_reaches_setup_without_second_prompt(self):
        original,adapted,m=self.adapted()
        rc,out,log=self.interactive(self.preflight(adapted)+self.setup_body())
        self.assertEqual(rc,0,(out,log));self.assertEqual(log.count(b'[0-'),1)
        self.assertEqual(json.loads(self.settings.read_text())['listenAddress'],'192.168.3.1')
        self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.3.1\n')
    def test_cancel_stops_before_persistent_boundary(self):
        _,adapted,_=self.adapted()
        rc,out,log=self.interactive(self.preflight(adapted)+'touch "$BRORAY_ROOT/PERSISTENT_WRITE"',b'0\n')
        self.assertNotEqual(rc,0);self.assertFalse((self.app/'PERSISTENT_WRITE').exists())
    def test_noninteractive_override_passes_clean_preflight(self):
        _,adapted,_=self.adapted();self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.3.1'
        p=self.run_plain(self.preflight(adapted)+self.setup_body());self.assertEqual(p.returncode,0,(p.stdout,p.stderr))
        self.assertEqual(json.loads(self.settings.read_text())['listenAddress'],'192.168.3.1')
    def test_bad_input_hash_refused(self):
        original,_,m=self.adapted()
        with self.assertRaises(ValueError):m.integrate(original,'0'*64)
    def test_already_integrated_refused(self):
        _,adapted,m=self.adapted()
        with self.assertRaises(ValueError):m.integrate(adapted,hashlib.sha256(adapted).hexdigest())
    def test_remaining_transaction_byte_identical(self):
        original,adapted,_=self.adapted()
        for marker,side in [(b'mkdir -m 700 "$TMP/network"',0),(b'# The immutable downloads',1)]:
            self.assertEqual(original.split(marker,1)[side],adapted.split(marker,1)[side])
    def test_headless_does_not_read_script_stdin(self):
        _,adapted,_=self.adapted()
        p=subprocess.run(['/bin/ash','-c',self.script(self.preflight(adapted)+'touch "$BRORAY_ROOT/PERSISTENT_WRITE"')],env=self.env,input=b'2\n',capture_output=True,start_new_session=True,timeout=25)
        self.assertNotEqual(p.returncode,0);self.assertIn(b'BRORAY_LAN_IP_OVERRIDE',p.stderr)
        self.assertFalse((self.app/'PERSISTENT_WRITE').exists())

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(CleanLan))
    raise SystemExit(not result.wasSuccessful())

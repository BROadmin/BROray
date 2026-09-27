"""Real ash selection/setup with a controlling PTY; ndmc/ip are isolated fixtures."""
import fcntl,json,os,select,subprocess,termios,time,unittest
from test_network_multilan import Lan,ROOT

class InstallLan(unittest.TestCase):
    fixture=Lan.fixture
    def setUp(self):
        Lan.setUp(self)
        self.settings.write_text('{"listenAddress":null,"keep":"UNCHANGED"}')
        self.conf.unlink()
    def script(self,body):return '. "$BRORAY_ROOT/lib/network.sh"; '+body
    def run_plain(self,body):
        return subprocess.run(['/bin/ash','-c',self.script(body)],env=self.env,stdin=subprocess.DEVNULL,capture_output=True,start_new_session=True,timeout=25)
    def interactive(self,body,answer=b'2\n',before_answer=None):
        master,slave=os.openpty()
        def terminal():
            os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
        p=subprocess.Popen(['/bin/ash','-c',self.script(body)],env=self.env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=slave,pass_fds=(slave,),preexec_fn=terminal)
        os.close(slave);data=b'';sent=False;deadline=time.monotonic()+25
        try:
            while time.monotonic()<deadline:
                if select.select([master],[],[],0.05)[0]:
                    try:chunk=os.read(master,4096)
                    except OSError:break
                    if not chunk:break
                    data+=chunk
                    if not sent and b'[0-' in data:
                        if before_answer:before_answer()
                        os.write(master,answer);sent=True
                if p.poll() is not None:break
            out,_=p.communicate(timeout=2)
            self.assertFalse(list((self.app/'tmp').iterdir()),data)
            return p.returncode,out,data
        finally:
            if p.poll() is None:p.kill();p.wait()
            os.close(master)
    def setup_body(self):
        src=(ROOT/'runtime/app/lib/package-setup.sh').read_text()
        a=src.index('configure_local_address()\n');b=src.index('\n}\n',a)+3
        return src[a:b]+'''\nfail(){ echo "$*" >&2; exit 1; }
BRORAY_SETUP_TARGET="$BRORAY_ROOT"; BRORAY_SETUP_ASH=/bin/ash
configure_local_address
'''
    def test_first_install_selects_second_private_address(self):
        rc,out,log=self.interactive('broray_network_select install')
        self.assertEqual(rc,0,(out,log));self.assertEqual(out,b'192.168.3.1\n')
        self.assertIn(b'Segment0',log);self.assertIn(b'Segment1',log)
        self.assertIsNone(json.loads(self.settings.read_text())['listenAddress'])
    def test_setup_persists_selection_and_restarts_without_prompt(self):
        rc,out,log=self.interactive(self.setup_body());self.assertEqual(rc,0,(out,log))
        self.assertEqual(json.loads(self.settings.read_text()),{'listenAddress':'192.168.3.1','keep':'UNCHANGED'})
        self.assertEqual((self.app/'run/lan-ip').read_text(),'192.168.3.1\n')
        for mode in ['transport','webui','install']:
            p=self.run_plain('broray_network_select '+mode);self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,b'192.168.3.1\n')
    def test_invalid_choice_does_not_default(self):
        rc,out,log=self.interactive('broray_network_select install',b'\n999\nabc\n2\n')
        self.assertEqual(rc,0,(out,log));self.assertEqual(out,b'192.168.3.1\n')
    def test_entware_sort_output_never_enters_selected_address(self):
        # Target sort also wrote its sorted rows to stdout with -o. A chooser
        # must return only the selected IP even with that utility behavior.
        p=self.app/'bin/sort'
        p.write_text('#!/bin/ash\n/usr/bin/sort "$@" || exit $?\nif [ "${1:-}" = -o ];then cat "$2";fi\n')
        p.chmod(0o755)
        rc,out,log=self.interactive('broray_network_select install')
        self.assertEqual(rc,0,(out,log));self.assertEqual(out,b'192.168.3.1\n')
    def test_cancel_leaves_settings_and_runtime_unchanged(self):
        old=self.settings.read_bytes();rc,out,log=self.interactive(self.setup_body(),b'0\n')
        self.assertNotEqual(rc,0,(out,log));self.assertEqual(old,self.settings.read_bytes());self.assertFalse((self.app/'run/lan-ip').exists())
    def test_eof_cancels(self):
        rc,out,log=self.interactive('broray_network_select install',b'\x04')
        self.assertNotEqual(rc,0);self.assertEqual(out,b'');self.assertIn(b'LAN_SELECTION_CANCELLED',log)
    def test_noninteractive_explains_explicit_setting(self):
        p=self.run_plain('broray_network_select install');self.assertNotEqual(p.returncode,0)
        self.assertIn(b'LAN_SELECTION_REQUIRED',p.stderr);self.assertIn(b'BRORAY_LAN_IP_OVERRIDE',p.stderr)
        self.assertEqual(p.stdout,b'')
    def test_explicit_override_needs_no_tty(self):
        self.env['BRORAY_LAN_IP_OVERRIDE']='192.168.3.1'
        p=self.run_plain(self.setup_body());self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(self.settings.read_text())['listenAddress'],'192.168.3.1')
    def test_untrusted_override_rejected_without_mutation(self):
        for address in ['192.168.99.1','8.8.8.8','127.0.0.1','0.0.0.0']:
            with self.subTest(address=address):
                self.env['BRORAY_LAN_IP_OVERRIDE']=address;p=self.run_plain(self.setup_body())
                self.assertNotEqual(p.returncode,0);self.assertIsNone(json.loads(self.settings.read_text())['listenAddress'])
    def test_single_private_no_prompt(self):
        self.fixture(('public','private'));p=self.run_plain('broray_network_select install')
        self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,b'192.168.3.1\n')
    def test_background_does_not_prompt_even_with_tty(self):
        rc,out,log=self.interactive('broray_detect_lan_ip')
        self.assertNotEqual(rc,0);self.assertNotIn(b'[0-',log);self.assertEqual(out,b'')
    def test_candidate_reclassified_during_prompt_is_rejected(self):
        rc,out,log=self.interactive(self.setup_body(),before_answer=lambda:self.fixture(('private','public')))
        self.assertNotEqual(rc,0,(out,log));self.assertIn(b'PIN_NOT_PRIVATE_OR_LIVE',log)
        self.assertIsNone(json.loads(self.settings.read_text())['listenAddress']);self.assertFalse((self.app/'run/lan-ip').exists())
    def test_invalid_zero_private_never_prompts(self):
        self.fixture(('public','protected'));rc,out,log=self.interactive('broray_network_select install')
        self.assertNotEqual(rc,0);self.assertNotIn(b'[0-',log)

if __name__=='__main__':
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(InstallLan)
    if os.environ.get('LAN_BASELINE_ONLY')=='1':suite=unittest.TestSuite([InstallLan('test_first_install_selects_second_private_address')])
    result=unittest.TextTestRunner(verbosity=2,failfast=True).run(suite)
    raise SystemExit(not result.wasSuccessful())

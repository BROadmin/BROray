"""Real ownership parser with isolated router snapshots (no router access)."""
import hashlib,json,os,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
LIB=ROOT/'implementation/runtime/app/lib/interface-owner.sh'
BASE='interface Proxy0\n    description BROray\n    security-level public\n    proxy protocol socks5\n    proxy upstream 192.168.1.1 2080\n    up\n'

class InterfacePriority(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.path=Path(self.tmp.name)/'block'
        self.env=os.environ|{'BRORAY_PROXY_HOST':'192.168.1.1','BRORAY_PROXY_PORT':'2080'}
    def call(self,body,action):
        self.path.write_text(body)
        p=subprocess.run(['/bin/ash','-c','. "$1"; '+action,'test',str(LIB),str(self.path)],env=self.env,capture_output=True,timeout=10)
        self.assertEqual(self.path.read_text(),body)
        return p
    def signature(self,body):
        return self.call(body,'broray_interface_block_signature_exact "$2" Proxy0 192.168.1.1 2080 BROray').returncode
    def priority(self,value):return BASE.replace('    proxy protocol',f'    ip global {value}\n    proxy protocol')
    def test_valid_priority_and_original_receipt(self):
        for value in ['1','42129','65534']:
            with self.subTest(value=value):
                body=self.priority(value)
                self.assertEqual(self.signature(body),0)
                p=self.call(body,'broray_interface_block_sha256 "$2"')
                self.assertEqual(p.returncode,0,p.stderr)
                self.assertEqual(p.stdout.decode().strip(),hashlib.sha256(BASE.encode()).hexdigest())
    def test_legacy_and_dynamic_binding_still_work(self):
        for body in [BASE,BASE.replace('    up','    proxy connect via ISP\n    up'),self.priority('42129').replace('    up','    proxy connect via ISP\n    up')]:
            with self.subTest(body=body):self.assertEqual(self.signature(body),0)
    def test_malformed_duplicate_or_misplaced_priority_rejected(self):
        cases=[self.priority(x) for x in ['0','65535','-1','1.5','auto','order 1','1; reboot','01','1 2','']]
        cases += [self.priority('1').replace('    up','    ip global 2\n    up'),BASE+'    ip global 1\n']
        for body in cases:
            with self.subTest(body=body):self.assertNotEqual(self.signature(body),0)
    def test_foreign_fields_are_not_hidden(self):
        for body in [self.priority('42129').replace('192.168.1.1','192.168.2.1'),self.priority('42129').replace('socks5','http'),self.priority('42129').replace('    up','    ip mtu 1500\n    up'),self.priority('42129').replace('BROray','Other')]:
            with self.subTest(body=body):self.assertNotEqual(self.signature(body),0)

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(InterfacePriority))
    (ROOT/'docs/evidence/interface-priority-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

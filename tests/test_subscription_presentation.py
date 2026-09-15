"""Real subscription functions: BusyBox DNS answers and terminal projection."""
import json,os,shlex,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SOURCE=(ROOT/'implementation/runtime/app/lib/subscription-service.sh').read_text()
DNS=SOURCE[SOURCE.index('broray_subscription_ip_is_public()'):SOURCE.index('broray_subscription_parse_url()')]
PROJECTION=SOURCE[SOURCE.index('broray_subscription_effective_status()'):SOURCE.index('broray_subscription_recover_stale()')]

class SubscriptionPresentation(unittest.TestCase):
    def run_shell(self,code,data='',env=None):
        return subprocess.run(['/bin/ash','-c',code],input=data,text=True,capture_output=True,env=env,timeout=10)
    def test_busybox_answer_with_reverse_name(self):
        answer='Server: 127.0.0.1\nAddress 1: 127.0.0.1 localhost\n\nName: api.brovibe.cloud\nAddress 1: 85.9.223.218 85-9-223-218.fi-hel2.upcloud.host\n'
        p=self.run_shell(DNS+'\nbroray_subscription_nslookup_addresses',answer)
        self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(p.stdout,'85.9.223.218\n')
    def test_unindexed_and_multiple_answers(self):
        p=self.run_shell(DNS+'\nbroray_subscription_nslookup_addresses','Server: 127.0.0.1\nAddress: 127.0.0.1#53\nName: example.test\nAddress: 2606:4700:4700::1111\nAddress: 1.1.1.1#53\nAddress 2: 1.1.1.1 reverse.test\n')
        self.assertEqual(p.stdout,'1.1.1.1\n2606:4700:4700::1111\n')
    def test_literal_validation_and_public_scope(self):
        good=['85.9.223.218','1.1.1.1','172.15.0.1','172.32.0.1','100.63.0.1','100.128.0.1','2606:4700:4700::1111','2001:4860:4860::8888','2a00:1450:4001:80f::200e','2A00:1450:4001:80F:0:0:0:200E']
        bad=['reverse.example','85-9-223-218.fi-hel2.upcloud.host','','1.2.3','1.2.3.999','01.2.3.4','0x7f000001','127.0.0.1','10.0.0.1','172.16.0.1','192.168.1.1','100.64.1.1','100.127.1.1','169.254.1.1','198.18.1.1','192.0.2.1','224.0.0.1','255.255.255.255','::1','0:0:0:0:0:0:0:1','::ffff:127.0.0.1','::ffff:7f00:1','fc00::1','fe80::1','ff02::1','2001:0DB8::1','2606:::1','2606::1:','2606::1::2','2606:12345::1','2606:1:2:3:4:5:6','2606:1:2:3:4:5:6:7:8','2606:1:2:3:4:5:6::7']
        for ip in good+bad:
            with self.subTest(ip=ip):
                p=self.run_shell(DNS+'\nbroray_subscription_ip_is_public '+shlex.quote(ip))
                self.assertEqual(p.returncode,0 if ip in good else 1,(ip,p.stderr))
    def project(self,state):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);op='op-sub-test';folder=root/'operations'/op;folder.mkdir(parents=True)
            (folder/'state.json').write_text(json.dumps({'kind':'background','operationId':op,'running':False,'state':'aborted','errorCode':'CANCELLED','finishedAt':'2026-09-16T00:01:05Z'}|state))
            data={'id':'test','url':'https://example.test','lastUpdateStatus':'running','backgroundOperationId':op,'lastUpdatedAt':'2026-09-15T00:00:00Z','lastError':'OLD','lastUpdateResult':{'errorCode':'HTTP_ERROR','durationMs':8000}}
            path=root/'sub.json';path.write_text(json.dumps(data));before=path.read_bytes()
            setup='broray_server_subscription_count() { printf 0; }; broray_subscription_mask_url() { printf "%s" "$1"; };\n'
            p=self.run_shell(setup+PROJECTION+'\nbroray_subscription_public_file '+shlex.quote(str(path)),env=os.environ|{'BRORAY_STATE_ROOT':str(root)})
            self.assertEqual(p.returncode,0,p.stderr);self.assertEqual(path.read_bytes(),before)
            return data,json.loads(p.stdout)
    def test_cancel_replaces_previous_attempt_result_without_writing(self):
        _,p=self.project({});self.assertEqual(p['lastUpdateStatus'],'error')
        self.assertEqual(p['lastUpdateResult'],{'errorCode':'CANCELLED','durationMs':None,'warnings':[]})
        self.assertEqual(p['lastUpdatedAt'],'2026-09-16T00:01:05Z')
        self.assertIn('пользователем',p['lastError'])
    def test_interruption_never_reuses_old_network_error(self):
        _,p=self.project({'state':'recovered','errorCode':None})
        self.assertEqual(p['lastUpdateResult']['errorCode'],'OPERATION_INTERRUPTED')
    def test_running_or_mismatched_record_is_not_terminal(self):
        for state in [{'running':True,'state':'running'},{'operationId':'op-other'},{'kind':'updater'}]:
            with self.subTest(state=state):
                before,p=self.project(state);self.assertEqual(p['lastUpdateStatus'],'running')
                self.assertEqual(p['lastUpdateResult'],before['lastUpdateResult'])

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubscriptionPresentation))
    (ROOT/'docs/evidence/subscription-presentation-tests.json').write_text(json.dumps({'status':'PASS' if result.wasSuccessful() else 'FAIL','testsRun':result.testsRun,'routerAccessed':False})+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)

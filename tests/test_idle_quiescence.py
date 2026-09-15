"""Standalone migration helper: kernel-pinned legacy shell, never Xray."""
import ctypes,hashlib,json,os,shutil,subprocess,tempfile,time,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
BIN=ROOT/'.local/bin/linux-idle-quiescence'
class Quiescence(unittest.TestCase):
    def setUp(self):
        assert ctypes.CDLL(None).prctl(36,1,0,0,0)==0
        self.temp=Path(tempfile.mkdtemp(prefix='idle-quiescence-'));self.children=[]
    def tearDown(self):
        for p in self.children:
            if p.poll() is None:p.terminate()
            p.communicate(timeout=10)
        end=time.monotonic()+6
        while time.monotonic()<end:
            try:
                p,_=os.waitpid(-1,os.WNOHANG)
                if not p:time.sleep(.05)
            except ChildProcessError:break
        else:self.fail('Live test descendant: keep fixture')
        assert self.temp.parent==Path('/tmp') and self.temp.name.startswith('idle-quiescence-');shutil.rmtree(self.temp)
    def start(self,body='sleep 3 & wait $!',name='broray-home-snapshotd'):
        script=self.temp/name;script.write_text('#!/bin/ash\ntrap \'exit 0\' TERM\nwhile :; do '+body+'; done\n')
        p=subprocess.Popen(['/bin/ash',str(script)],stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.children.append(p);time.sleep(.3);self.assertIsNone(p.poll())
        proc=Path('/proc')/str(p.pid);ticks=proc.joinpath('stat').read_text().rsplit(') ',1)[1].split()[19]
        cmd=self.temp/'expected.cmdline';cmd.write_bytes(proc.joinpath('cmdline').read_bytes())
        args=[str(BIN),str(p.pid),ticks,Path('/proc/sys/kernel/random/boot_id').read_text().strip(),os.readlink(proc/'exe'),str(cmd),'3']
        return p,args
    def invoke(self,args):return subprocess.run(args,capture_output=True,timeout=8)
    def test_pinned_idle_parent_exits_and_unrelated_process_survives(self):
        p,args=self.start();canary=subprocess.Popen(['/bin/sleep','30']);self.children.append(canary)
        result=self.invoke(args);self.assertEqual(result.returncode,0,result.stderr)
        data=json.loads(result.stdout);self.assertTrue(data['quiesced']);self.assertEqual(data['parentPid'],p.pid)
        self.assertEqual(data['parentStartTicks'],args[2]);p.communicate(timeout=8);self.assertEqual(p.returncode,-9)
        self.assertIsNone(canary.poll());self.assertNotEqual(data['sleepPid'],canary.pid)
    def test_reused_ticks_wrong_boot_executable_and_command_are_rejected(self):
        p,args=self.start()
        for index,value in [(2,str(int(args[2])+1)),(3,'wrong-boot'),(4,'/foreign/busybox')]:
            wrong=args.copy();wrong[index]=value
            self.assertEqual(self.invoke(wrong).returncode,75);self.assertIsNone(p.poll())
        Path(args[5]).write_bytes(b'/bin/ash\0/foreign/broray-home-snapshotd\0')
        self.assertEqual(self.invoke(args).returncode,75);self.assertIsNone(p.poll())
    def test_multiple_children_are_preserved(self):
        p,args=self.start('sleep 3 & sleep 4 & wait')
        self.assertEqual(self.invoke(args).returncode,75);self.assertIsNone(p.poll())
    def test_active_non_sleep_child_is_preserved(self):
        p,args=self.start("/bin/ash -c 'sleep 3; :' & wait $!")
        self.assertEqual(self.invoke(args).returncode,75);self.assertIsNone(p.poll())
    def test_no_child_window_is_preserved(self):
        p,args=self.start(':')
        self.assertEqual(self.invoke(args).returncode,75);self.assertIsNone(p.poll())
    def test_other_daemon_is_never_an_accepted_target(self):
        p,args=self.start(name='persistent-xray-fixture')
        self.assertEqual(self.invoke(args).returncode,64);self.assertIsNone(p.poll())
if __name__=='__main__':
    r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.defaultTestLoader.loadTestsFromTestCase(Quiescence))
    (ROOT/'docs/evidence/idle-quiescence-tests.json').write_text(json.dumps({'status':'PASS' if r.wasSuccessful() else 'FAIL','testsRun':r.testsRun,'helperSha256':hashlib.sha256(BIN.read_bytes()).hexdigest(),'environment':'actual Linux ptrace and private legacy-shell fixtures','routerAccessed':False},indent=2)+'\n')
    raise SystemExit(0 if r.wasSuccessful() else 1)

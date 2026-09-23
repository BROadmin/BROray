"""Protected stop must drain the exact newly started updater and retain evidence."""
import hashlib,json,shutil,subprocess,time,unittest
from pathlib import Path
from test_native_platform_start_safety import StartSafety
from test_native_platform_install import FILES

class StopCurrent(StartSafety):
 def before_stop(self,control,before):pass
 def after_stop(self,receipt,retired):pass
 def setUp(self):
  super().setUp();self.addCleanup(self.clear_stop)
 def clear_stop(self):
  for p in self.op.glob('platform-stop-current.*'):
   if p.is_file() or p.is_symlink():p.unlink()
 def test_exact_generation_stopped_retired_and_replay_has_no_second_signal(self):
  start=self.prepared();lines=(start/'launch.record').read_text().splitlines();domain=Path(lines[2])
  # Same offline dependency fixture as StartController: the daemon requires
  # curl at admission, but readiness must never actually fetch anything.
  curl=Path('/usr/bin/curl');self.assertFalse(curl.exists());fetch=self.root/'unexpected-stop-fetch'
  curl.write_text('#!/bin/ash\nprintf called >"'+str(fetch)+'"\nexit 97\n');curl.chmod(0o755)
  def control(verb):
   return subprocess.run([str(self.native),'control',str(domain),verb,lines[3],lines[4],self.e['operationId'],self.e['stopNonce']],capture_output=True,text=True,timeout=4)
  foreign=subprocess.Popen(['/bin/busybox','sleep','300'])
  foreign_stat=Path('/proc')/str(foreign.pid)/'stat';birth=foreign_stat.read_text().rsplit(')',1)[1].split()[19]
  try:
   r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   r=control('STATUS');self.assertEqual(r.returncode,0,r.stdout+r.stderr);before=json.loads(r.stdout)
   self.assertTrue(before['platformReady']);self.assertTrue(before['supervisedFromBirth'])
   platform={rel:((self.root/'router'/rel).read_bytes(),(self.root/'router'/rel).stat().st_mode) for rel in FILES}
   self.before_stop(control,before)
   r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   first=json.loads(r.stdout);self.assertEqual(first['phase'],'STOPPED');self.assertTrue(first['allWritersStopped']);self.assertFalse(first['activationAllowed'])
   self.assertEqual(first['generationId'],lines[3]);self.assertFalse(first['replayed'])
   receipt=self.op/'platform-stop-current.receipt';raw=receipt.read_bytes()
   self.assertEqual(hashlib.sha256(raw).hexdigest(),first['stopReceiptSha256'])
   self.assertEqual(receipt.stat().st_mode&0o777,0o600)
   self.assertTrue((domain/'retirement.receipt').is_file(),'terminal lineage evidence must survive stop')
   retired={p.name:p.read_bytes() for p in domain.iterdir() if p.is_file()}
   terminal=json.loads(retired[max((n for n in retired if n.startswith('revision-')),default='state.json')])
   self.assertEqual(terminal['state'],'STOPPED');self.assertEqual(terminal['children'],[])
   self.assertEqual(terminal['awaitingBirth'],[]);self.assertEqual(terminal['exitedUnreaped'],[])
   self.assertEqual(terminal['supervisor'],before['supervisor']);self.assertEqual(terminal['updater'],before['updater'])
   self.after_stop(receipt,retired)
   r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   repeat=json.loads(r.stdout);self.assertTrue(repeat['replayed']);self.assertEqual(repeat['stopReceiptSha256'],first['stopReceiptSha256'])
   self.assertEqual(receipt.read_bytes(),raw)
   self.assertEqual({p.name:p.read_bytes() for p in domain.iterdir() if p.is_file()},retired,'replay cannot publish or signal again')
   for rel,expected in platform.items():
    p=self.root/'router'/rel;self.assertEqual((p.read_bytes(),p.stat().st_mode),expected,rel)
   self.assertIsNone(foreign.poll());self.assertEqual(foreign_stat.read_text().rsplit(')',1)[1].split()[19],birth)
   self.assertTrue((self.op/'fence').is_dir());self.assertFalse((self.op/'platform-committed.record').exists())
   self.assertFalse(fetch.exists())
  finally:
   # Fixture owns this direct child; production must never discover/signal it.
   foreign.terminate();foreign.wait(timeout=3)
   if (domain/'control').exists():
    r=control('STOP')
    if r.returncode==0:
     end=time.monotonic()+5
     while time.monotonic()<end:
      r=control('STATUS')
      if r.returncode==0 and json.loads(r.stdout)['state']=='STOPPED':break
      time.sleep(.02)
     control('RETIRE')
   if domain.parent.exists():shutil.rmtree(domain.parent)
   curl.unlink()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([StopCurrent('test_exact_generation_stopped_retired_and_replay_has_no_second_signal')]))
 raise SystemExit(not r.wasSuccessful())

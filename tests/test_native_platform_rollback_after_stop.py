"""Guarded before-images may be restored only after proven terminal retirement.

File restoration is not old service restoration. NEEDS_RECOVERY must remain
explicit until the independent supervised service restoration gate is proven.
"""
import json,shutil,subprocess,time,unittest
from pathlib import Path
from test_native_platform_start_safety import StartSafety
from test_native_platform_stop_current import StopCurrent
from test_native_platform_rollback import PlatformRollback
from test_native_platform_install import FILES

class RollbackAfterStop(StartSafety):
 clear_stop=StopCurrent.clear_stop
 clear_rollback=PlatformRollback.clear_rollback
 def setUp(self):
  super().setUp();self.addCleanup(self.clear_stop);self.addCleanup(self.clear_rollback)
 def test_guarded_rollback_after_verified_stop_preserves_terminal_history(self):
  start=self.prepared();lines=(start/'launch.record').read_text().splitlines();domain=Path(lines[2])
  backup=json.loads((self.backup_dir/'intent.json').read_bytes())
  before={rel:((self.backup_dir/f'before-{i}').read_bytes(),backup['before'][i]['mode']) for i,rel in enumerate(FILES)}
  curl=Path('/usr/bin/curl');self.assertFalse(curl.exists());fetch=self.root/'unexpected-rollback-fetch'
  curl.write_text('#!/bin/ash\nprintf called >"'+str(fetch)+'"\nexit 97\n');curl.chmod(0o755)
  def control(verb):
   return subprocess.run([str(self.native),'control',str(domain),verb,lines[3],lines[4],self.e['operationId'],self.e['stopNonce']],capture_output=True,text=True,timeout=4)
  foreign=subprocess.Popen(['/bin/busybox','sleep','300'])
  foreign_stat=Path('/proc')/str(foreign.pid)/'stat';birth=foreign_stat.read_text().rsplit(')',1)[1].split()[19]
  try:
   r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   r=control('STATUS');self.assertEqual(r.returncode,0,r.stdout+r.stderr);owner=json.loads(r.stdout)
   self.assertTrue(owner['platformReady']);self.assertTrue(owner['supervisedFromBirth'])
   r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   stop=json.loads(r.stdout);self.assertTrue(stop['allWritersStopped']);self.assertEqual(stop['phase'],'STOPPED')
   retired={p.name:p.read_bytes() for p in domain.iterdir() if p.is_file()}
   evidence={p.name:p.read_bytes() for p in self.op.glob('platform-stop-current.*')}
   protected=self.snapshot();changed={str((self.root/'router'/rel).relative_to(self.root)) for rel in FILES}
   for attempt in range(2):
    r=self.invoke_phase('recovery-rollback');self.assertEqual(r.returncode,75,r.stdout+r.stderr)
    rows=[json.loads(s) for s in r.stdout.splitlines() if s.startswith('{')]
    restored=next((s for s in rows if s.get('platformRestored') is True),None)
    self.assertIsNotNone(restored,r.stdout+r.stderr)
    self.assertEqual(restored['phase'],'NEEDS_RECOVERY');self.assertFalse(restored['serviceStateRestored']);self.assertFalse(restored['activationAllowed'])
    self.assertEqual(restored['replayed'],bool(attempt))
    for rel,expected in before.items():
     p=self.root/'router'/rel;self.assertEqual((p.read_bytes(),p.stat().st_mode&0o777),expected,rel)
    self.assertEqual({p.name:p.read_bytes() for p in domain.iterdir() if p.is_file()},retired)
    self.assertEqual({p.name:p.read_bytes() for p in self.op.glob('platform-stop-current.*')},evidence)
    after=self.snapshot()
    for path,value in protected.items():
     if path not in changed and '/.broray-pt-' not in path:self.assertEqual(after[path],value,path)
    self.assertTrue((self.op/'fence').is_dir());self.assertIsNone(foreign.poll())
    self.assertEqual(foreign_stat.read_text().rsplit(')',1)[1].split()[19],birth)
    if attempt:self.assertEqual(after,first_after,'rollback replay must preserve all evidence')
    first_after=after
   self.assertFalse(fetch.exists())
   print('POST_STOP_ROLLBACK_RECEIPT '+json.dumps({'filesRestored':7,'replayExact':True,'terminalHistoryPreserved':True,'foreignProcessPreserved':True,'serviceStateRestored':False,'phase':'NEEDS_RECOVERY','fullLifecycleGate':'NOT_PASSED'}),flush=True)
  finally:
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
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RollbackAfterStop('test_guarded_rollback_after_verified_stop_preserves_terminal_history')]))
 raise SystemExit(not r.wasSuccessful())

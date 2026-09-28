"""The shutdown case must reach COMMITTED, not only verify legacy STOPPED."""
import json,subprocess,unittest
from pathlib import Path
import test_native_platform_completion as completion

class ShutdownCompletion(completion.PlatformCompletion):
 def setUp(self):
  super().setUp()
  # Same real RAM prerequisite as InstalledInit. The historical export was
  # observational and has no /tmp; never bypass workspace_ram_valid.
  self.ram=self.root/'router/tmp'
  self.assertFalse(self.ram.exists() or self.ram.is_symlink())
  self.ram.mkdir(mode=0o700)
  subprocess.run(['mount','-t','tmpfs','-o','mode=700','tmpfs',str(self.ram)],check=True,capture_output=True)
  self._cleanups.insert(0,(self.release_ram,(),{}))
 def release_ram(self):
  subprocess.run(['umount',str(self.ram)],check=True,capture_output=True)
  self.ram.rmdir()
 def test_all_projections_gone_still_completes_exact_transaction(self):
  (self.updater/'daemon.pid').unlink()
  (self.updater/'daemon.ready').unlink()
  (self.updater/'daemon.lock').rmdir()
  try:self.test_committed_generation_completes_operation_and_exact_replay()
  except Exception:
   evidence={}
   for p in self.updater.rglob('*'):
    if p.is_file() and (p.name in ['state.json','identity.json','launch.record'] or p.suffix=='.log' or p.name.startswith('revision-')):
     evidence[str(p.relative_to(self.root))]=p.read_text(errors='replace')[-12000:]
   for p in Path('/proc').glob('[0-9]*'):
    try:
     cmd=(p/'cmdline').read_bytes().replace(b'\0',b' ').decode(errors='replace')
     if str(self.root) not in cmd:continue
     evidence['proc/'+p.name]={k:(p/k).read_text(errors='replace') for k in ['status','stat','wchan','stack']}
     evidence['proc/'+p.name]['cmdline']=cmd
    except (OSError,ProcessLookupError):pass
   print('SHUTDOWN_LIFECYCLE_FAILURE '+json.dumps(evidence),flush=True)
   raise

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ShutdownCompletion('test_all_projections_gone_still_completes_exact_transaction')]))
 raise SystemExit(not r.wasSuccessful())

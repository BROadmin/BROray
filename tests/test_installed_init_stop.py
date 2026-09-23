"""Public installed S22 stop must settle its own canonical operation.

Actual first migration/start, exact owned stop, existing terminal host proof.
No private stop command substitutes for the public operation under test.
"""
import hashlib,json,os,subprocess,unittest
from pathlib import Path
from test_installed_generation_stop import InstalledGenerationStop

class InstalledInitStop(InstalledGenerationStop):
 def test_public_stop_preserves_origin_platform_and_foreign_process(self):
  self.installed();r=self.init('start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  generation=json.loads(r.stdout)['generationId'];launch=self.updater/'starts'/generation/'launch.record'
  rows=launch.read_text().splitlines();domain=self.updater/'generations'/generation;host=self.updater/'hosts'/generation
  origin={str(p.relative_to(self.op)):(hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mode&0o7777) for p in self.op.rglob('*') if p.is_file() and not p.is_symlink()}
  platform=self.bytes_now();foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-foreign-fixture'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   foreign_stat=Path('/proc/%s/stat'%foreign.pid).read_text().split(') ',1)[1].split()[19]
   r=self.init('stop')
   print('INSTALLED_PUBLIC_STOP '+json.dumps({'returnCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True)
   self.assertEqual(r.returncode,0,r.stdout+r.stderr)
   reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'SERVICE_STOP_COMPLETED');self.assertTrue(reply['serviceStopped']);self.assertFalse(reply['platformReady'])
   self.assertEqual(reply['generationId'],generation)
   terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
   self.assertEqual(terminal['state'],'STOPPED');self.assertEqual(terminal['children'],[])
   self.assertNotEqual(terminal['stopOperationId'],self.op.name)
   stop=self.op.parent/terminal['stopOperationId'];self.assertEqual(json.loads((stop/'state.json').read_bytes())['state'],'completed')
   lock=self.root/'router/opt/var/lock/broray/global-operation.lock';self.assertFalse(lock.exists() or lock.is_symlink())
   self.assertTrue((stop/'retired-lock').is_symlink());self.assertTrue((domain/'retirement.receipt').is_file());self.assertTrue((host/'retirement.receipt').is_file())
   self.assertEqual({str(p.relative_to(self.op)):(hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mode&0o7777) for p in self.op.rglob('*') if p.is_file() and not p.is_symlink()},origin)
   self.assertEqual(self.bytes_now(),platform);self.assertIsNone(foreign.poll())
   self.assertEqual(Path('/proc/%s/stat'%foreign.pid).read_text().split(') ',1)[1].split()[19],foreign_stat)
  finally:
   if foreign.poll() is None:foreign.terminate()
   foreign.wait(timeout=5)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([InstalledInitStop('test_public_stop_preserves_origin_platform_and_foreign_process')]))
 raise SystemExit(not r.wasSuccessful())

"""Durable protected startup intent after the real offline boot boundary.

This stage must not launch any process or grant application activation.
"""
import hashlib,json,stat,unittest,shutil
from test_native_platform_install import PlatformInstall,FILES
from test_owned_host_cleanup import clear_fixture_host

class StartIntent(PlatformInstall):
 def setUp(self):
  super().setUp();self.assertFalse((self.updater/'starts').exists());self.addCleanup(self.clear_start)
 def clear_start(self):
  clear_fixture_host(self)
  if (self.updater/'starts').exists():shutil.rmtree(self.updater/'starts')
  for name in ['platform-start.record','platform-start.record.pending','platform-starting.record','platform-starting.record.pending']:
   p=self.op/name
   if p.exists():p.unlink()
  for p in self.op.glob('platform-host-*'):
   if p.is_file() or p.is_symlink():p.unlink()
 def test_start_intent_is_bound_durable_and_exact_on_replay(self):
  self.first();r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-install');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  before=self.snapshot();platform={rel:(self.root/'router'/rel).read_bytes() for rel in FILES}
  r=self.invoke_phase('recovery-start-intent');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  s=json.loads(r.stdout);self.assertEqual(s['phase'],'START_INTENT');self.assertFalse(s['activationAllowed']);self.assertFalse(s['serviceStarted'])
  start=self.updater/'starts'/s['generationId'];record=start/'launch.record';raw=record.read_bytes()
  self.assertEqual(hashlib.sha256(raw).hexdigest(),s['startIntentSha256']);self.assertEqual(stat.S_IMODE(record.stat().st_mode),0o600)
  lines=raw.decode().splitlines();self.assertEqual(lines[0],'BROray-platform-launch/2')
  self.assertEqual(lines[1],str(self.root/'router'));self.assertEqual(lines[2],str(self.updater/'generations'/s['generationId']));self.assertEqual(lines[3],s['generationId'])
  self.assertEqual(lines[8:10],[self.e['operationId'],self.e['stopNonce']])
  self.assertEqual(('\n'.join(lines[10:])+'\n').encode(),(self.op/'platform-migration/manifest.record').read_bytes())
  self.assertTrue((self.op/'platform-start.record').is_file());self.assertTrue((start/'transaction.record').is_file())
  self.assertFalse((self.updater/'generations').exists());self.assertTrue((self.op/'fence').is_dir())
  for rel,body in platform.items():self.assertEqual((self.root/'router'/rel).read_bytes(),body)
  after=self.snapshot()
  for name,value in before.items():self.assertEqual(after[name],value,name)
  r=self.invoke_phase('recovery-start-intent');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  self.assertTrue(json.loads(r.stdout)['replayed']);self.assertEqual(self.snapshot(),after)
  print('PLATFORM_START_INTENT '+json.dumps(s),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([StartIntent('test_start_intent_is_bound_durable_and_exact_on_replay')]))
 raise SystemExit(not r.wasSuccessful())

"""Public STOP after ready preflight, without an intervening init start.

Use the existing multi-operation cleanup: a public stop has its own operation
and nonce, while the completed migration origin must remain unchanged.
"""
import json,unittest
from test_updater_preflight import PreflightAfterBoot
from test_installed_generation_stop import InstalledGenerationStop

class ReadyPreflightStop(PreflightAfterBoot):
 fixture_type=InstalledGenerationStop
 def test_ready_preflight_can_stop_without_intervening_start(self):
  first=self.completed(self.bootstrap(self.slot,self.slot_sha))
  f=self.parent_fixture
  self.assertFalse((f.updater/'cycles').exists(),'precondition: no ordinary lifecycle origin')
  state=(f.op/'state.json').read_bytes();platform=self.snapshot()
  r=f.init('stop')
  print('STOP_AFTER_PREFLIGHT '+json.dumps(dict(rc=r.returncode,stdout=r.stdout,stderr=r.stderr)),flush=True)
  self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  proof=json.loads(r.stdout)
  self.assertEqual(proof['phase'],'SERVICE_STOP_COMPLETED')
  self.assertEqual(proof['generationId'],first['generationId'])
  self.assertTrue(proof['serviceStopped']);self.assertFalse(proof['platformReady'])
  self.assertEqual((f.op/'state.json').read_bytes(),state)
  self.assertEqual(self.snapshot(),platform)
  generations=list((f.updater/'generations').glob('g-*'))
  self.assertEqual([p.name for p in generations],[first['generationId']])
  self.assertTrue((generations[0]/'retirement.receipt').is_file())
  self.assertTrue((f.updater/'hosts'/first['generationId']/'retirement.receipt').is_file())

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  ReadyPreflightStop('test_ready_preflight_can_stop_without_intervening_start')]))
 raise SystemExit(not result.wasSuccessful())

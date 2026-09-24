"""Completed service stops are not platform-installation origins."""
import json,unittest
from test_updater_preflight_stop_origin import ReadyPreflightStop

class PreflightAfterServiceCycle(ReadyPreflightStop):
 def test_completed_stop_does_not_make_installed_origin_ambiguous(self):
  self.test_ready_preflight_can_stop_without_intervening_start()
  f=self.parent_fixture
  r=f.init('start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  generation=json.loads(r.stdout)['generationId']
  states={str(p):p.read_bytes() for p in (self.root/'opt/var/lib/broray/operations').glob('op-*/state.json')}
  self.assertGreaterEqual(len(states),2)
  self.assertEqual(sum('serviceStop' in json.loads(b) for b in states.values()),1)
  reply=self.completed(self.bootstrap(self.slot,self.slot_sha))
  self.assertEqual(reply['generationId'],generation)
  self.assertEqual(states,{str(p):p.read_bytes() for p in (self.root/'opt/var/lib/broray/operations').glob('op-*/state.json')})
  # Corrupt evidence remains an explicit refusal, not an ignored history row.
  stop=next(p for p in (self.root/'opt/var/lib/broray/operations').glob('op-*/state.json')
            if 'serviceStop' in json.loads(p.read_bytes()))
  original=stop.read_bytes();bad=json.loads(original)
  bad['serviceStop']['contract']='unknown';corrupt=json.dumps(bad).encode()
  try:
   stop.write_bytes(corrupt)
   r=self.bootstrap(self.slot,self.slot_sha)
   self.assertEqual(r.returncode,75,r.stdout+r.stderr)
   self.assertEqual(json.loads(r.stdout)['errorCode'],'PREFLIGHT_RECOVERY_EVIDENCE_INCOMPLETE')
   self.assertEqual(stop.read_bytes(),corrupt)
  finally:stop.write_bytes(original)

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([
  PreflightAfterServiceCycle('test_completed_stop_does_not_make_installed_origin_ambiguous')]))
 raise SystemExit(not result.wasSuccessful())

"""A terminal marker alone cannot replace complete retirement evidence."""
import json,subprocess,unittest
from test_native_legacy_retirement_races import LegacyRetirementRaces

class LegacyRetirementEvidence(LegacyRetirementRaces):
 def retire(self):return subprocess.run(self.guard_args(),capture_output=True,text=True,timeout=15)
 def first(self):
  r=self.retire();self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertTrue(json.loads(r.stdout)['serviceStopped'])
 def refuse(self):
  before=self.snapshot();r=self.retire();self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.snapshot(),before)
 def test_missing_stopped_receipt_is_not_recreated(self):
  self.first();p=self.retired/'stopped.receipt';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_corrupt_stopped_receipt_is_preserved(self):self.first();p=self.retired/'stopped.receipt';p.write_bytes(b'{broken');self.refuse();self.assertEqual(p.read_bytes(),b'{broken')
 def test_missing_move_done_does_not_mutate_completed_evidence(self):self.first();p=self.retired/'move-1.done';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_missing_move_intent_does_not_mutate_completed_evidence(self):self.first();p=self.retired/'move-1.intent';p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_missing_executor_is_not_recreated(self):self.first();p=next(self.retired.glob('executor-*.json'));p.unlink();self.refuse();self.assertFalse(p.exists())
 def test_unknown_retired_object_is_preserved_and_refused(self):self.first();p=self.retired/'objects/foreign';p.write_bytes(b'KEEP');self.refuse();self.assertEqual(p.read_bytes(),b'KEEP')
 def test_foreign_live_projection_is_preserved(self):self.first();p=self.updater/'daemon.pid';p.write_bytes(b'999999\n');self.refuse();self.assertEqual(p.read_bytes(),b'999999\n')
 def test_foreign_generation_is_not_retired(self):
  self.first();p=self.updater/'generations';p.mkdir(mode=0o700);self.addCleanup(p.rmdir);self.refuse();self.assertTrue(p.is_dir())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyRetirementEvidence(n) for n in LegacyRetirementEvidence.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())

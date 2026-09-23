"""Partial origin initialization may not repair corrupt or foreign evidence."""
import hashlib,json,os,stat,unittest
from test_service_origin_crash import OriginCrash
from test_service_cycle_publication_adversarial import PublicationAdversarial

class OriginAdversarial(OriginCrash):
 inventory=PublicationAdversarial.inventory
 def success(self,verb):
  pending=self.updater/'cycles/origin.record.pending'
  if verb=='start' and pending.exists() and not getattr(self,'checked_origin',False):
   self.checked_origin=True;cycles=pending.parent;anchor=cycles/'origin.anchor';canonical=pending.read_bytes();saved=anchor.read_bytes();cases=[]
   def refuse(label):
    before=self.inventory(self.updater);origin=self.files(self.op)
    r=self.init('start');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
    self.assertEqual(self.inventory(self.updater),before,label);self.assertEqual(self.files(self.op),origin)
    cases.append(label)
   anchor.write_bytes(b'CORRUPT-ORIGIN-ANCHOR\n')
   try:refuse('corrupt-anchor-does-not-complete-pending-record')
   finally:anchor.write_bytes(saved)
   pending.write_bytes(canonical+b'CORRUPTION\n')
   try:refuse('corrupt-record-does-not-change-anchor')
   finally:pending.write_bytes(canonical)
   saved_path=self.root/'fixture-origin-anchor';anchor.rename(saved_path)
   try:refuse('missing-anchor-is-not-reconstructed')
   finally:saved_path.rename(anchor)
   unknown=cycles/'foreign';unknown.write_bytes(b'KEEP');unknown.chmod(0o600)
   try:refuse('unknown-object-preserved')
   finally:unknown.unlink()
   print('ORIGIN_REFUSAL_MATRIX '+json.dumps({'cases':cases,'preserved':True}),flush=True)
  return super().success(verb)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([OriginAdversarial('test_origin_record_pending_replays')]))
 raise SystemExit(not r.wasSuccessful())

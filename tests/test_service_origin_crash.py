"""First service-origin publication must resume without editing migration history."""
import json,unittest
from test_installed_service_cycles import ServiceCycles
from test_generation_migration_crash import trace_string
from test_service_cycle_crash_support import crash_native_caller

class OriginCrash(ServiceCycles):
 def guard_args(self):
  if not getattr(self,'intercept_origin',False):return super().guard_args()
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'service-cycle-start',str(self.root/'router'),self.op.name,b['migrationIntentSha256'],b['stopNonce']]
 def scenario(self,boundary):
  self.prepared()
  for phase in ['recovery-start','recovery-commit']:
   r=self.invoke_phase(phase);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.complete();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  gid=json.loads(r.stdout)['generationId'];self.assertFalse((self.updater/'cycles').exists())
  before=self.files(self.op);self.intercept_origin=True
  def match(pid,regs):return regs.orig_rax==265 and trace_string(pid,regs.r10)==boundary
  out,err=crash_native_caller(self,[265],match,'origin-'+boundary)
  self.assertEqual(out,b'');self.assertTrue((self.updater/'cycles'/(boundary+'.pending')).is_file())
  self.assertEqual(self.files(self.op),before)
  result=self.success('start');self.assertEqual(result['generationId'],gid);self.one_live(gid)
  self.assertEqual(self.success('start')['generationId'],gid)
  self.assertEqual(list((self.updater/'cycles').glob('cycle-*.record')),[])
  self.assertEqual(list((self.updater/'cycles').glob('*.pending')),[])
  self.assertEqual(self.files(self.op),before);self.success('stop');self.assertEqual(self.files(self.op),before)
 def test_origin_anchor_pending_replays(self):self.scenario('origin.anchor')
 def test_origin_record_pending_replays(self):self.scenario('origin.record')

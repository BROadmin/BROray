"""Crash the native endpoint used by the public S22 controller at durable ordinary-start publication."""
import json,os,unittest
from test_installed_service_cycles import ServiceCycles
from test_link_interceptor_support import intercepted_link
from test_generation_migration_crash import trace_string

class CycleIntentCrash(ServiceCycles):
 def guard_args(self):
  if not getattr(self,'intercept_start',False):return super().guard_args()
  binding=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'service-cycle-start',str(self.root/'router'),self.op.name,binding['migrationIntentSha256'],binding['stopNonce']]
 def test_durable_intent_before_launch_retries_same_generation(self):
  self.installed();first=self.success('start');self.success('stop')
  before=self.files(self.op);self.intercept_start=True
  key='BRORAY_UPDATER_ROOT_PREFIX';previous=os.environ.get(key)
  os.environ[key]=str(self.root/'router')
  def match(pid,regs,entering):
   return not entering and regs.rax==0 and trace_string(pid,regs.r10)=='cycle-00000000000000000001.record'
  try:
   rc,out,err=intercepted_link(self,match)
   self.assertLess(rc,0)
  finally:
   if previous is None:os.environ.pop(key,None)
   else:os.environ[key]=previous
  intent=self.updater/'cycles/cycle-00000000000000000001.record'
  intent_bytes=intent.read_bytes();intended=intent_bytes.decode().splitlines()[3]
  self.assertEqual(self.files(self.op),before)
  resumed=self.success('start');self.assertNotEqual(resumed['generationId'],first['generationId'])
  self.assertEqual(resumed['generationId'],intended);self.assertEqual(intent.read_bytes(),intent_bytes)
  self.one_live(resumed['generationId'])
  self.assertEqual(self.success('start')['generationId'],resumed['generationId'])
  self.assertEqual(len(list((self.updater/'cycles').glob('cycle-*.record'))),1)
  self.assertEqual(self.files(self.op),before);self.success('stop')

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([CycleIntentCrash('test_durable_intent_before_launch_retries_same_generation')]))
 raise SystemExit(not result.wasSuccessful())

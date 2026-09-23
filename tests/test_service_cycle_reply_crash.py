"""Lost caller before READY and after READY; same intent and one live writer."""
import json,os,unittest
from pathlib import Path
from test_installed_service_cycles import ServiceCycles
from test_generation_migration_crash import trace_string
from test_service_cycle_crash_support import crash_native_caller,syscall_output

class ReplyCrash(ServiceCycles):
 def guard_args(self):
  if not getattr(self,'intercept_start',False):return super().guard_args()
  b=json.loads((self.op/'platform-bootguard.json').read_bytes())
  return [str(self.native),'service-cycle-start',str(self.root/'router'),self.op.name,b['migrationIntentSha256'],b['stopNonce']]
 def scenario(self,phase):
  self.installed();first=self.success('start');self.success('stop')
  before=self.files(self.op);self.intercept_start=True;observed={}
  def match(pid,regs):
   intents=sorted((self.updater/'cycles').glob('cycle-*.record'))
   if not intents:return False
   intent=intents[-1];gid=intent.read_text().splitlines()[3]
   if phase=='before-ready':
    # The controller now reads the published ledger hint, not daemon.ready.
    # Intercept only its exact new-generation directory read, before READY.
    if regs.orig_rax!=257:return False
    domain=self.updater/'generations'/gid
    try:opened_parent=os.readlink('/proc/'+str(pid)+'/fd/'+str(regs.rdi))
    except OSError:return False
    if opened_parent!=str(domain):return False
    filename=trace_string(pid,regs.rsi)
    if filename!='state.json' and not (filename.startswith('revision-') and filename.endswith('.json')):return False
    records=sorted(domain.glob('revision-*.json'))
    if not records:return False
    state=json.loads(records[-1].read_bytes())
    if state['state']!='RUNNING' or state.get('platformReady') or not state.get('updater'):return False
    self.assertTrue(state['supervisedFromBirth']);self.assertEqual(state['generationId'],gid)
    observed.update(generation=gid,state=state['state'],platformReady=state['platformReady'],updater=state['updater'],intent=intent.read_bytes().hex(),interceptedDirectory=opened_parent,interceptedFile=filename)
    return True
   if regs.orig_rax not in (1,20) or regs.rdi!=1:return False
   body=syscall_output(pid,regs)
   if b'"phase":"SERVICE_READY"' not in body:return False
   reply=json.loads(body);self.assertTrue(reply['platformReady']);self.assertEqual(reply['generationId'],gid)
   observed.update(generation=gid,platformReady=True,intent=intent.read_bytes().hex());return True
  out,err=crash_native_caller(self,[257] if phase=='before-ready' else [1,20],match,phase)
  self.assertEqual(out,b'');self.assertTrue(observed);self.assertEqual(self.files(self.op),before)
  resumed=self.success('start');self.assertEqual(resumed['generationId'],observed['generation']);self.assertNotEqual(resumed['generationId'],first['generationId'])
  self.assertEqual(len(list((self.updater/'cycles').glob('cycle-*.record'))),1)
  self.assertEqual((self.updater/'cycles/cycle-00000000000000000001.record').read_bytes().hex(),observed['intent'])
  self.one_live(resumed['generationId']);self.assertEqual(self.success('start')['generationId'],resumed['generationId']);self.success('stop')
  self.assertEqual(self.files(self.op),before)
  print('REPLY_CRASH_RESULT '+json.dumps({'phase':phase,'observed':observed,'sameGeneration':True,'originUnchanged':True}),flush=True)
 def test_caller_dies_after_writer_birth_before_readiness(self):self.scenario('before-ready')
 def test_caller_dies_after_readiness_before_reply(self):self.scenario('after-ready')

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(ReplyCrash(n) for n in ReplyCrash.__dict__ if n.startswith('test_')))
 raise SystemExit(not result.wasSuccessful())

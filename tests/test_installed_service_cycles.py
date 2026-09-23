"""Public S22 lifecycle on exact installed bytes, in an offline Linux VM.

The original start-after-stop regression remains unchanged. These assertions
exercise repeated generations, idempotence, restart and evidence preservation.
"""
import hashlib,json,os,subprocess,unittest
from pathlib import Path
from test_installed_generation_stop import InstalledGenerationStop

class ServiceCycles(InstalledGenerationStop):
 def files(self,root):
  return {str(p.relative_to(root)):(hashlib.sha256(p.read_bytes()).hexdigest(),p.stat().st_mode&0o777)
          for p in root.rglob('*') if p.is_file() and not p.is_symlink()}
 def success(self,verb):
  print('PUBLIC_CALL '+verb,flush=True)
  r=self.init(verb);print('PUBLIC_REPLY '+json.dumps({'verb':verb,'rc':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  value=json.loads(r.stdout);self.assertTrue(value['ok']);return value
 def one_live(self,generation):
  active=[]
  for launch in (self.updater/'starts').glob('*/launch.record'):
   rows=launch.read_text().splitlines();domain=Path(rows[2])
   if (domain/'boot-ended.receipt').exists():
    r=subprocess.run([str(self.native),'generation-boot-verify',str(domain)],capture_output=True,text=True,timeout=4)
    self.assertEqual(r.returncode,0,r.stdout+r.stderr);proof=json.loads(r.stdout)
    self.assertEqual(proof['phase'],'BOOT_ENDED_HISTORY_VERIFIED');self.assertFalse(proof['signalsAuthorized'])
    self.assertNotEqual(proof['oldBootId'],Path('/proc/sys/kernel/random/boot_id').read_text().strip())
    continue
   if (domain/'retirement.receipt').exists():continue
   r=subprocess.run([str(self.native),'control',str(domain),'STATUS',rows[3],rows[4],self.op.name,self.e['stopNonce']],capture_output=True,text=True,timeout=4)
   self.assertEqual(r.returncode,0,r.stdout+r.stderr);v=json.loads(r.stdout)
   self.assertTrue(v['platformReady']);self.assertTrue(v['supervisedFromBirth']);self.assertEqual(v['state'],'RUNNING')
   self.assertEqual(v['bootId'],Path('/proc/sys/kernel/random/boot_id').read_text().strip())
   active.append(v['generationId'])
  self.assertEqual(active,[generation])
 def test_multiple_cycles_idempotent_start_and_public_restart(self):
  self.installed();first=self.success('start');before=self.files(self.op);platform=self.bytes_now();seen={first['generationId']}
  for index in range(3):
   current=self.success('status');self.one_live(current['generationId'])
   # Discarding a successful reply must not force a second generation.
   repeated=self.success('start');self.assertEqual(repeated['generationId'],current['generationId'])
   self.assertEqual(self.files(self.op),before);self.assertEqual(self.bytes_now(),platform)
   if index==1:
    following=self.success('restart')
   else:
    stopped=self.success('stop');self.assertTrue(stopped['serviceStopped']);self.assertFalse(stopped['platformReady'])
    self.assertEqual(self.success('stop')['generationId'],stopped['generationId'])
    following=self.success('start')
   self.assertNotIn(following['generationId'],seen);seen.add(following['generationId'])
   self.assertTrue(following['platformReady']);self.one_live(following['generationId'])
   self.assertEqual(self.files(self.op),before);self.assertEqual(self.bytes_now(),platform)
  self.success('stop');self.assertEqual(len(seen),4)
  print('PUBLIC_CYCLE_RECEIPT '+json.dumps({'distinctGenerations':len(seen),'completedMigrationUnchanged':True,'platformUnchanged':True,'publicRestart':True}),flush=True)
 def test_corrupt_retired_ledger_and_wrong_native_request_are_preserved(self):
  self.installed();first=self.success('start');self.success('stop');before=self.files(self.op)
  generation=first['generationId'];domain=self.updater/'generations'/generation
  ledger=domain/'state.json';original=ledger.read_bytes();corrupt=original+b'CORRUPTION\n'
  ledger.write_bytes(corrupt)
  try:
   r=self.init('start');self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(ledger.read_bytes(),corrupt);self.assertEqual(self.files(self.op),before)
   self.assertEqual(len(list((self.updater/'starts').glob('*/launch.record'))),1)
  finally:ledger.write_bytes(original)
  binding=json.loads((self.op/'platform-bootguard.json').read_bytes());roots=self.files(self.updater)
  for migration,nonce in [(binding['migrationIntentSha256'],'f'*32),('f'*64,binding['stopNonce'])]:
   r=subprocess.run([str(self.native),'service-cycle-start',str(self.root/'router'),self.op.name,migration,nonce],capture_output=True,text=True,timeout=30)
   self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
   self.assertEqual(self.files(self.updater),roots);self.assertEqual(self.files(self.op),before)
  good=self.success('start');self.assertNotEqual(good['generationId'],generation);self.success('stop')
 def test_parallel_starts_and_foreign_xray_process_are_safe(self):
  self.installed();first=self.success('start');self.success('stop');before=self.files(self.op)
  sentinel=subprocess.Popen(['/bin/ash','-c','while :; do sleep 1; done','xray-foreign-fixture'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   script=self.root/'router/opt/etc/init.d/S22broray-updater';env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root/'router')}
   processes=[subprocess.Popen(['/bin/ash',str(script),'start'],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True) for _ in range(2)]
   results=[]
   for p in processes:
    out,err=p.communicate(timeout=120);results.append((p.returncode,out,err))
   self.assertTrue(any(r[0]==0 for r in results),str(results));self.assertTrue(all(r[0] in (0,75) for r in results),str(results))
   current=self.success('status');self.one_live(current['generationId']);self.assertNotEqual(current['generationId'],first['generationId'])
   for rc,out,err in results:
    if rc==0:self.assertEqual(json.loads(out)['generationId'],current['generationId'])
   self.assertEqual(self.files(self.op),before);self.assertIsNone(sentinel.poll())
   self.success('restart');self.assertIsNone(sentinel.poll());self.success('stop');self.assertIsNone(sentinel.poll())
  finally:
   if sentinel.poll() is None:sentinel.terminate()
   sentinel.wait(timeout=5)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(ServiceCycles(n) for n in ServiceCycles.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())

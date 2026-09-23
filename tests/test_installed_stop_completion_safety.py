"""Unproven terminal settlement must preserve operation/fence/history exactly."""
import fcntl,json,os,subprocess,unittest
from pathlib import Path
from test_installed_stop_completion import InstalledStopCompletion

class StopCompletionSafety(InstalledStopCompletion):
 def after_host_retired(self,command,host,domain):
  terminal=json.loads(sorted(domain.glob('revision-*.json'))[-1].read_bytes())
  operation=terminal['stopOperationId'];nonce=terminal['stopNonce']
  op=self.root/'router/opt/var/lib/broray/operations'/operation
  global_lock=self.root/'router/opt/var/lock/broray/global-operation.lock'
  def snapshot():
   rows={}
   for root in [op,host,domain,domain.parent]:
    for p in root.iterdir():
     if p.is_symlink():rows[str(p)]=('link',os.readlink(p))
     elif p.is_file():rows[str(p)]=('file',p.read_bytes(),p.stat().st_mode&0o7777)
     elif p.is_dir():rows[str(p)]=('dir',p.stat().st_mode&0o7777)
   rows['GLOBAL']=os.readlink(global_lock) if global_lock.is_symlink() else None
   rows['PLATFORM']=self.bytes_now()
   rows['MIGRATION']=(self.op/'state.json').read_bytes()
   rows['FENCE']={p.name:(p.read_bytes(),p.stat().st_mode&0o7777) for p in (op/'fence').iterdir()}
   return rows
  cases=[]
  def refuse(label,call=None):
   before=snapshot();r=(call or (lambda:self.complete_stop(operation,nonce)))()
   self.assertNotEqual(r.returncode,0,label+': '+r.stdout+r.stderr)
   self.assertEqual(snapshot(),before,label+' modified protected state or evidence')
   self.assertTrue(global_lock.is_symlink());self.assertTrue(json.loads((op/'state.json').read_bytes())['running'])
   cases.append(label)
  refuse('wrong-nonce',lambda:self.complete_stop(operation,'f'*32 if nonce!='f'*32 else 'e'*32))
  env=self.completion_env()
  def direct():
   live=self.root/'router';state=live/'opt/var/lib/broray'
   return subprocess.run([str(self.code/'bin/broray-ops-guard'),str(state/'operations.guard'),str(live/'opt/bin/ash'),
    str(self.code/'lib/operation-coordinator.sh'),'platform-preflight-stop-settle',operation,nonce],env=env,capture_output=True,text=True,timeout=30)
  refuse('settle-without-inherited-generation-and-host-exclusion',direct)
  mutex=domain.parent/'.generation-lifetime.lock';saved=domain.parent/'.saved-lifetime-fixture'
  mutex.rename(saved)
  try:refuse('missing-lifetime-not-recreated');self.assertFalse(mutex.exists())
  finally:saved.rename(mutex)
  fd=os.open(mutex,os.O_RDWR)
  try:fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);refuse('installation-lifetime-held')
  finally:os.close(fd)
  fd=os.open(host,os.O_RDONLY|os.O_DIRECTORY)
  try:fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);refuse('host-publisher-lifetime-held')
  finally:os.close(fd)
  sibling=domain.parent/'unknown-generation';sibling.mkdir(mode=0o700)
  try:refuse('incomplete-sibling-generation');self.assertEqual(list(sibling.iterdir()),[])
  finally:sibling.rmdir()
  foreign=op/'fence/foreign';foreign.write_bytes(b'KEEP-EXACT');foreign.chmod(0o600)
  try:refuse('foreign-fence-evidence')
  finally:foreign.unlink()
  publisher=self.updater/'request.lock';publisher.mkdir(mode=0o700)
  try:refuse('empty-live-publisher-fence');self.assertEqual(list(publisher.iterdir()),[])
  finally:publisher.rmdir()
  for p,label in [(host/'retirement.receipt','corrupt-host-receipt'),(domain/'state.json','corrupt-generation-anchor'),
    (self.op/'platform-recovery-code/manifest.record','corrupt-retained-code-manifest')]:
   old=p.read_bytes();mode=p.stat().st_mode&0o7777
   try:p.write_bytes(b'{broken');refuse(label);self.assertEqual(p.read_bytes(),b'{broken')
   finally:p.write_bytes(old);p.chmod(mode)
  print('STOP_COMPLETION_SAFETY '+json.dumps({'subcases':cases,'fenceAndStateUnchanged':True,'unknownEvidencePreserved':True}),flush=True)
  super().after_host_retired(command,host,domain)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([StopCompletionSafety('test_retired_host_proof_after_fresh_operation_stop')]))
 raise SystemExit(not r.wasSuccessful())

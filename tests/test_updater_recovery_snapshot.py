"""Recovery classifies one state snapshot without repeated jq processes.

Execute the real updater functions. The jq PATH wrapper only counts invocations
and execs the real jq unchanged; assertions cover fence/queue side effects too.
"""
import json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(os.environ.get('BRORAY_TEST_ROOT','/work/implementation'))
UPDATER=ROOT/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh'

class RecoverySnapshot(unittest.TestCase):
 def test_raw_script_version_command(self):
  # Execute delivered bytes: read_text() would silently hide Windows CRLF.
  result=subprocess.run(['/bin/ash',str(UPDATER),'version'],capture_output=True,text=True,timeout=10)
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  self.assertEqual(result.stdout.strip(),'broray-updater/5')
 def invoke(self,value):
  self.tmp=tempfile.TemporaryDirectory(prefix='recovery-snapshot-');self.addCleanup(self.tmp.cleanup)
  root=Path(self.tmp.name);op=root/'ops/op-snapshot';op.mkdir(parents=True)
  state=root/'state';queue=state/'queue';queue.mkdir(parents=True)
  lock=state/'request.lock';lock.mkdir();(lock/'operation-id').write_text('op-snapshot\n')
  pointer=root/'last-operation';pointer.write_text('op-snapshot\n')
  (op/'state.json').write_text(value if isinstance(value,str) else json.dumps(value))
  (queue/'op-snapshot.json').write_text('queued-evidence')
  bindir=root/'bin';bindir.mkdir();calls=root/'jq-calls';real=shutil.which('jq');self.assertTrue(real)
  wrapper=bindir/'jq';wrapper.write_text('#!/bin/sh\nprintf "call\\n" >>"'+str(calls)+'"\nexec "'+real+'" "$@"\n');wrapper.chmod(0o755)
  text=UPDATER.read_text();self.assertTrue(text.endswith('main "$@"\n'))
  script=root/'driver.sh';script.write_text(text[:-len('main "$@"\n')]+'recover_incomplete\n')
  env={**os.environ,'BRORAY_UPDATER_PATH':str(bindir)+':'+os.environ['PATH'],
       'BRORAY_UPDATER_ROOT_PREFIX':str(root),'BRORAY_UPDATER_STATE_ROOT':str(state),
       'BRORAY_UPDATER_OPERATION_ROOT':str(root/'ops'),'BRORAY_UPDATER_OPERATION_POINTER':str(pointer)}
  result=subprocess.run(['/bin/ash',str(script)],env=env,capture_output=True,text=True,timeout=10)
  return result,calls.read_text().splitlines() if calls.exists() else [],lock,queue/'op-snapshot.json'
 def test_terminal_state_is_read_once_and_completed_fence_is_released(self):
  for value in [{'running':False,'stage':'rejected','state':'error'}, {'state':'completed'},
                {'running':'false','stage':'complete','state':'success'}]:
   with self.subTest(value=value):
    r,calls,lock,queue=self.invoke(value);self.assertEqual(r.returncode,0,r.stdout+r.stderr)
    self.assertFalse(lock.exists());self.assertFalse(queue.exists())
    self.assertEqual(len(calls),1,'terminal recovery launched repeated jq readers of the same state')
 def test_required_recovery_preserves_fence_and_queue(self):
  for key,value in [('stage','rollback-failed'),('stage','recovery-ambiguous'),('state','recovery-required'),('stage','rollback-failed\n')]:
   with self.subTest(key=key,value=value):
    r,calls,lock,queue=self.invoke({'running':False,key:value});self.assertNotEqual(r.returncode,0)
    self.assertTrue(lock.is_dir());self.assertEqual(queue.read_text(),'queued-evidence');self.assertEqual(len(calls),1)
 def test_unreadable_or_multiple_state_objects_fail_closed(self):
  for value in ['{broken','{}\n{}','null']:
   with self.subTest(value=value):
    r,calls,lock,queue=self.invoke(value);self.assertNotEqual(r.returncode,0)
    self.assertTrue(lock.is_dir());self.assertEqual(queue.read_text(),'queued-evidence')

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RecoverySnapshot(n) for n in RecoverySnapshot.__dict__ if n.startswith('test_')]))
 raise SystemExit(not r.wasSuccessful())

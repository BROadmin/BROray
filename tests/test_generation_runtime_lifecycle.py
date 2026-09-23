"""Real generation control remains valid after a temporary source is pruned."""
import json,os,shutil,subprocess,unittest
from pathlib import Path
from test_generation_runtime_store import RuntimeStore,GEN

class RetainedLifecycle(RuntimeStore):
 def test_supervisor_and_control_outlive_temporary_code_root(self):
  # Installation exclusion intentionally inspects every sibling generation.
  # Runtimes and operation history are separate namespaces in the layout.
  self.domain.rmdir();generations=self.home/'generations';generations.mkdir(mode=0o700)
  self.domain=generations/'generation-one';self.domain.mkdir(mode=0o700)
  op=self.home/'prunable-operation';op.mkdir(mode=0o700);temporary=op/'native';shutil.copyfile(GEN,temporary);temporary.chmod(0o700)
  retained=subprocess.run([str(temporary),'runtime-retain',str(self.store),self.sha],capture_output=True,text=True,timeout=5)
  self.assertEqual(retained.returncode,0,retained.stderr);runtime=self.entry/'runtime'
  script=self.home/'daemon.sh';script.write_text('while :; do sleep 2; done\n')
  log=open(self.home/'retained.log','wb');self.logs.append(log)
  process=subprocess.Popen([str(runtime),'run',str(self.domain),self.gid,self.sha,'--','/bin/ash',str(script)],stdout=log,stderr=log);self.processes.append(process)
  before=self.running();self.assertEqual(before['supervisor']['executable'],str(runtime));self.assertTrue(self.live(before['updater']['pid']))
  shutil.rmtree(op)
  def control(verb):return subprocess.run([str(runtime),'control',str(self.domain),verb,self.gid,self.sha,'operation-one','nonce-one'],capture_output=True,text=True,timeout=4)
  status=control('STATUS');self.assertEqual(status.returncode,0,status.stderr);now=json.loads(status.stdout)
  self.assertEqual(now['supervisor'],before['supervisor']);self.assertEqual(now['updater'],before['updater']);self.assertTrue(self.live(now['updater']['pid']))
  self.assertEqual(control('STOP').returncode,0);end=self.stopped();self.assertEqual(end['children'],[]);self.assertFalse(self.live(before['updater']['pid']))
  print('RETAINED_RUNTIME_LIFECYCLE '+json.dumps({'before':before['supervisor'],'afterPrune':now['supervisor'],'stopped':end['state'],'runtime':str(runtime),'temporarySourceAbsent':not op.exists()}),flush=True)

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([RetainedLifecycle('test_supervisor_and_control_outlive_temporary_code_root')]))
 raise SystemExit(not r.wasSuccessful())

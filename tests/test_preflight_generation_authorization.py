"""Real crashes/lost acknowledgment after target fsync, before native STOP.

Only the private guard wrapper pauses after delegating to the actual durable
writer. Production coordinator/native bytes are not instrumented or replaced.
"""
from pathlib import Path
import json,os,signal,subprocess,time,unittest
from test_preflight_generation_stop import GenerationStop
from test_preflight_admission import GUARD

class GenerationAuthorization(GenerationStop):
 def pause_owner(self,lose_ack=False):
  guard=self.home/'guard-pause.sh'
  guard.write_text('''#!/bin/ash
if [ "$1" = --replace-file ] && [ ! -e "$TEST_HOME/target-held" ] &&
   /usr/bin/jq -e '.platformPreflight.phase=="STOP_INTENT" and .platformPreflight.generationStop!=null' "$2" >/dev/null 2>&1; then
 "$TEST_REAL_GUARD" "$@" || exit $?
 : >"$TEST_HOME/target-held"
 while [ ! -e "$TEST_HOME/target-release" ]; do sleep .05; done
 [ "$TEST_LOSE_ACK" = 1 ] && exit 1
 exit 0
fi
exec "$TEST_REAL_GUARD" "$@"
''');guard.chmod(0o755)
  tail='broray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA"\n'
  if lose_ack:
   tail='''rc=0
broray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA" || rc=$?
[ "$rc" != 0 ] || exit 91
broray_ops_preflight_stop_generation "$TEST_GENERATION" "$TEST_CURRENT_SHA"
'''
  script=self.home/'authorization-owner.sh'
  script.write_text('set -u\n. "$BRORAY_OPS_CODE_ROOT/lib/operation-client.sh"\nbroray_ops_preflight_admit "$TEST_SHA" || exit $?\nbroray_ops_preflight_stop_intent "$TEST_SHA" || exit $?\n'+tail)
  env={**self.env,'BRORAY_OPS_GUARD':str(guard),'TEST_REAL_GUARD':str(GUARD),'TEST_LOSE_ACK':'1' if lose_ack else '0'}
  p=subprocess.Popen(['/bin/ash',str(script)],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,start_new_session=True)
  self.processes.append(p);self.waitfile(self.home/'target-held',p,seconds=45)
  self.assertEqual(self.latest()['state'],'RUNNING');self.assertFalse(self.latest()['termSent'])
  state=self.readstate();self.assertEqual(state['platformPreflight']['phase'],'STOP_INTENT')
  self.assertEqual(state['platformPreflight']['generationStop']['supervisor']['pid'],self.native.pid)
  print('GENERATION_AUTHORIZATION_BOUNDARY '+json.dumps({'coordinator':state,'native':self.latest(),'lostGuardAck':lose_ack}),flush=True)
  return p
 def test_owner_crash_before_native_stop_keeps_generation_and_fence(self):
  owner=self.pause_owner();before=(self.operation()/'state.json').read_bytes()
  os.killpg(owner.pid,signal.SIGKILL);owner.wait(timeout=3)
  self.assertEqual(self.latest()['state'],'RUNNING');self.assertFalse(self.latest()['termSent'])
  r=self.shell('broray_ops_call recover "'+self.operation().name+'"')
  self.assertNotEqual(r.returncode,0);self.assertTrue(self.lock.is_symlink());self.assertEqual((self.operation()/'state.json').read_bytes(),before)
 def test_supervisor_crash_after_binding_never_reports_stopped(self):
  owner=self.pause_owner();self.native.kill();self.assertNotEqual(self.native.wait(timeout=3),0)
  (self.home/'target-release').touch();out,err=owner.communicate(timeout=45)
  self.assertNotEqual(owner.returncode,0,out+err);self.assertNotEqual(self.readstate()['platformPreflight']['phase'],'STOPPED');self.assertTrue(self.lock.is_symlink())
  stat=Path(f'/proc/{self.updater_pid}/stat')
  self.assertTrue(not stat.exists() or stat.read_text().rsplit(') ',1)[1].split()[0] in ['Z','X'])
  self.assertNotEqual(self.latest()['state'],'STOPPED')
 def test_lost_durable_target_ack_can_retry_same_binding(self):
  owner=self.pause_owner(lose_ack=True);binding=self.readstate()['platformPreflight']['generationStop']
  (self.home/'target-release').touch();out,err=owner.communicate(timeout=45)
  self.assertEqual(owner.returncode,0,out+err);rows=[json.loads(x) for x in out.splitlines()]
  self.assertEqual(rows[0]['errorCode'],'STATE_UNAVAILABLE');self.assertEqual(rows[-1]['phase'],'STOPPED')
  self.assertEqual(self.readstate()['platformPreflight']['generationStop'],binding);self.assertEqual(self.latest()['children'],[])

if __name__=='__main__':
 names=[n for n in GenerationAuthorization.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(GenerationAuthorization(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)

"""Exclusion across generation directories and controlled supervisor retirement."""
from pathlib import Path
import os,subprocess,time,unittest
from test_updater_generation import Generation,GEN

class Retirement(Generation):
 def test_generation_directories_share_installation_exclusion(self):
  p=self.start('while :; do :; done\n');self.running();before=(self.domain/'state.json').read_bytes()
  other=self.home/'next-generation';other.mkdir(mode=0o700)
  q=subprocess.Popen([GEN,'run',str(other),'generation-two',self.sha,'--','/bin/ash','-c','while :; do :; done'],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
  self.processes.append(q)
  try:rc=q.wait(timeout=2)
  except subprocess.TimeoutExpired:self.fail('SECOND_GENERATION_COEXISTS: a different directory bypassed lifetime exclusion')
  self.assertNotEqual(rc,0);self.assertEqual((self.domain/'state.json').read_bytes(),before);self.assertIsNone(p.poll());self.assertFalse((other/'state.json').exists())
 def test_retire_requires_verified_stop_and_matching_transaction(self):
  p=self.start('while :; do :; done\n');self.running()
  self.assertNotEqual(self.call('RETIRE').returncode,0);self.assertIsNone(p.poll())
  self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  before=(self.domain/'state.json').read_bytes()
  self.assertNotEqual(self.call('RETIRE',nonce='wrong-nonce').returncode,0);self.assertIsNone(p.poll())
  r=self.call('RETIRE');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(p.wait(timeout=2),0)
  self.assertEqual((self.domain/'state.json').read_bytes(),before)
  self.assertEqual(self.state()['children'],[]);self.assertEqual(self.state()['exitedUnreaped'],[])

if __name__=='__main__':
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite([Retirement('test_generation_directories_share_installation_exclusion'),Retirement('test_retire_requires_verified_stop_and_matching_transaction')]));raise SystemExit(0 if result.wasSuccessful() else 1)

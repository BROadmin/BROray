"""Installation-wide exclusion, preserved retirement and lost replies."""
import hashlib,json,os,socket,stat,subprocess,unittest
from test_updater_generation import Generation,GEN

class Installation(Generation):
 def inventory(self):
  return {p.name:(hashlib.sha256(p.read_bytes()).hexdigest(),stat.S_IMODE(p.stat().st_mode)) for p in self.domain.iterdir() if p.is_file()}
 def launch_next(self,gid='generation-two'):
  other=self.home/'next-generation';other.mkdir(mode=0o700)
  log=open(self.home/'next.log','wb');self.logs.append(log)
  p=subprocess.Popen([GEN,'run',str(other),gid,self.sha,'--','/bin/ash','-c','while :; do :; done'],stdout=log,stderr=log)
  self.processes.append(p);return p,other
 def assert_next_refused(self,gid='generation-two'):
  before=self.inventory();p,other=self.launch_next(gid)
  self.assertNotEqual(p.wait(timeout=2),0)
  self.assertFalse((other/'state.json').exists());self.assertEqual(self.inventory(),before)
 def retire(self):
  p=self.start('while :; do :; done\n');self.running();self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  self.assertEqual(self.call('RETIRE').returncode,0);self.assertEqual(p.wait(timeout=2),0)
  self.assertTrue((self.domain/'retirement.receipt').exists());return p
 def test_crashed_generation_blocks_different_directory(self):
  p=self.start(self.writer_body());writer=self.writer_pid();p.kill();p.wait(timeout=3);self.wait(lambda:not self.live(writer));self.assert_next_refused()
 def test_missing_ledger_blocks_different_directory(self):
  p=self.start();self.running();(self.domain/'state.json').unlink();self.assertNotEqual(p.wait(timeout=3),0);self.assert_next_refused()
 def test_corrupt_ledger_blocks_different_directory(self):
  p=self.start();self.running();(self.domain/'state.json').write_bytes(b'{broken');self.assertNotEqual(p.wait(timeout=3),0);self.assert_next_refused()
 def test_stopped_but_not_retired_blocks_successor(self):
  self.start();self.running();self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assert_next_refused()
 def test_successor_preserves_retired_generation(self):
  self.retire();before=self.inventory();p,other=self.launch_next()
  def started():
   records=sorted(other.glob('revision-*.json'))
   return bool(records) and json.loads(records[-1].read_bytes())['state']=='RUNNING'
  self.wait(started);self.assertIsNone(p.poll());self.assertEqual(self.inventory(),before)
 def test_missing_retirement_blocks_successor(self):
  self.retire();(self.domain/'retirement.receipt').unlink();self.assert_next_refused()
 def test_corrupt_retirement_blocks_successor(self):
  self.retire();(self.domain/'retirement.receipt').write_bytes(b'{broken');self.assert_next_refused()
 def test_old_retired_revision_corrupt_blocks_successor(self):
  self.retire();sorted(self.domain.glob('revision-*.json'))[0].write_bytes(b'{broken');self.assert_next_refused()
 def test_old_retired_revision_hardlink_blocks_successor(self):
  self.retire();os.link(sorted(self.domain.glob('revision-*.json'))[0],self.home/'external-hardlink');self.assert_next_refused()
 def test_retired_generation_id_cannot_be_reused(self):
  self.retire();self.assert_next_refused(gid=self.gid)
 def test_retirement_lost_reply_can_be_replayed(self):
  p=self.start('while :; do :; done\n');self.running();self.assertEqual(self.call('STOP').returncode,0);self.stopped()
  s=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET);s.connect(str(self.domain/'control'));s.sendall(f'RETIRE {self.gid} {self.sha} operation-one nonce-one'.encode());s.close()
  self.assertEqual(p.wait(timeout=2),0);before=self.inventory()
  r=self.call('RETIRE');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.inventory(),before)
  self.assertNotEqual(self.call('RETIRE',nonce='wrong-nonce').returncode,0);self.assertEqual(self.inventory(),before)
 def test_root_failure_terminal_proof_can_bind_recovery(self):
  body=self.writer_body().replace('while :; do sleep 2; done','while [ ! -e "$TEST_HOME/root.quit" ]; do sleep .02; done; exit 12')
  p=self.start(body);writer=self.writer_pid();(self.home/'root.quit').touch();terminal=self.stopped()
  self.assertFalse(self.live(writer));self.assertEqual(terminal['stopOperationId'],'');self.assertEqual(terminal['stopNonce'],'')
  old_records={path.name:path.read_bytes() for path in self.domain.glob('*.json')}
  result=self.call('STOP');self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  self.assertEqual(self.state()['stopOperationId'],'operation-one');self.assertEqual(self.state()['stopNonce'],'nonce-one')
  self.assertEqual(self.state()['termSent'],terminal['termSent']);self.assertEqual(self.call('STOP').returncode,0)
  self.assertEqual(self.call('RETIRE').returncode,0);self.assertEqual(p.wait(timeout=2),0)
  for name,body in old_records.items():self.assertEqual((self.domain/name).read_bytes(),body)

if __name__=='__main__':
 names=[name for name in Installation.__dict__ if name.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(Installation(name) for name in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)

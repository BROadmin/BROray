"""The recovery client must authenticate the live generation before sending STOP."""
import json,os,socket,subprocess,threading,unittest
from test_updater_generation import Generation,GEN

class ControlIdentity(Generation):
 def test_foreign_socket_cannot_forge_stopped(self):
  p=self.start(self.writer_body());writer=self.writer_pid();before=self.running()
  # Keep the genuine supervised writer alive, but replace only the endpoint.
  os.rename(self.domain/'control',self.home/'genuine-control')
  forged=dict(before,state='STOPPED',children=[],awaitingBirth=[],exitedUnreaped=[],stopOperationId='operation-one',stopNonce='nonce-one')
  server=socket.socket(socket.AF_UNIX,socket.SOCK_SEQPACKET);server.bind(str(self.domain/'control'));os.chmod(self.domain/'control',0o700);server.listen(1);server.settimeout(3)
  observed=[]
  def respond():
   try:
    c,_=server.accept()
    with c:
     data=c.recv(512);observed.append(data)
     if data:c.sendall((json.dumps(forged,separators=(',',':'))+'\n').encode())
   except (OSError,TimeoutError):pass
  thread=threading.Thread(target=respond,daemon=True);thread.start()
  try:r=self.call('STOP')
  finally:thread.join(timeout=3);server.close()
  self.assertIsNone(p.poll());self.assertTrue(self.live(writer));self.assertEqual((self.home/'platform').read_text(),'original\n')
  self.assertNotEqual(r.returncode,0,'foreign endpoint was accepted as verified STOPPED: '+r.stdout)
  self.assertEqual(observed,[b''],'unauthenticated peer received a mutating command')
 def test_same_binary_socket_moved_from_other_domain_refused(self):
  p=self.start('while :; do :; done\n');before=self.running()
  real=self.domain;other=self.home/'other';other.mkdir(mode=0o700)
  os.rename(real/'control',other/'control');self.domain=other
  try:r=self.call('STOP')
  finally:self.domain=real
  self.assertIsNone(p.poll());self.assertNotEqual(r.returncode,0,r.stdout)
  self.assertEqual(self.state()['stopOperationId'],'');self.assertEqual(self.state()['revision'],before['revision'])
 def test_symlink_endpoint_refused_without_mutation(self):
  p=self.start('while :; do :; done\n');self.running()
  os.rename(self.domain/'control',self.home/'real-control');os.symlink(self.home/'real-control',self.domain/'control')
  r=self.call('STOP');self.assertNotEqual(r.returncode,0,r.stdout);self.assertIsNone(p.poll());self.assertEqual(self.state()['stopOperationId'],'')
 def test_verified_peer_status_and_stop(self):
  self.start(self.writer_body());writer=self.writer_pid();self.running()
  r=self.call();self.assertEqual(r.returncode,0,r.stderr+r.stdout);self.assertTrue(json.loads(r.stdout)['supervisedFromBirth'])
  self.assertEqual(self.call('STOP').returncode,0);self.stopped();self.assertFalse(self.live(writer))

if __name__=='__main__':
 names=[n for n in ControlIdentity.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(ControlIdentity(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)

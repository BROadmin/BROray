"""Unknown or changed launcher inputs must never authorize an app action."""
from pathlib import Path
import hashlib,os,shlex,subprocess,time,unittest
from test_generation_service_launcher import ServiceLauncher

class LauncherEvidence(ServiceLauncher):
 def wait_request(self):
  self.start('while [ ! -e "$TEST_HOME/go" ]; do :; done\n'+self.command()+'\necho $? >"$TEST_HOME/client.rc.tmp"\nmv "$TEST_HOME/client.rc.tmp" "$TEST_HOME/client.rc"\nwhile :; do :; done\n');self.running()
 def assert_refused(self):
  (self.home/'go').touch();self.wait(lambda:(self.home/'client.rc').exists());self.assertNotEqual((self.home/'client.rc').read_text().strip(),'0');self.assertFalse((self.home/'xray.pid').exists())
 def private_ash(self):
  p=self.home/'ash-runtime';p.write_bytes(Path(self.ash).read_bytes());p.chmod(0o755);self.ash=str(p)
 def test_changed_interpreter_bytes_refused(self):
  self.private_ash();self.host();self.wait_request()
  with open(self.ash,'ab') as f:f.write(b'UNKNOWN_CHANGED_BYTES')
  self.assert_refused()
 def test_replaced_interpreter_refused(self):
  self.private_ash();self.host();self.wait_request();p=Path(self.ash);p.unlink();p.write_bytes(b'FOREIGN');p.chmod(0o755);self.assert_refused()
 def test_changed_script_bytes_refused(self):
  self.host();self.wait_request();self.script.write_text(self.script.read_text()+'# changed\n');self.assert_refused()
 def test_wrong_slot_refused(self):
  self.host();self.wait_request();(self.current/'.broray-slot').write_text('different-slot\n');self.assert_refused()
 def test_symlink_script_refused(self):
  self.host();self.wait_request();b=self.script.read_bytes();self.script.unlink();p=self.home/'other-script';p.write_bytes(b);p.chmod(0o755);self.script.symlink_to(p);self.assert_refused()
 def test_corrupt_host_evidence_preserved(self):
  host=self.host();self.wait_request();p=self.hostdir/'host.record';p.write_bytes(b'{broken');self.assertNotEqual(host.wait(timeout=3),0);self.assert_refused();self.assertEqual(p.read_bytes(),b'{broken')
 def test_missing_host_evidence_not_recreated(self):
  host=self.host();self.wait_request();p=self.hostdir/'host.record';p.unlink();self.assertNotEqual(host.wait(timeout=3),0);self.assert_refused();self.assertFalse(p.exists())
 def test_corrupt_completed_receipt_preserved(self):
  host,gen=self.begin();self.launched();p=self.hostdir/'request-service-one.done';p.write_bytes(b'{broken');self.assertNotEqual(host.wait(timeout=3),0);self.assertEqual(p.read_bytes(),b'{broken');self.assertEqual((self.home/'launches').read_text(),'start\n')
 def test_missing_completed_receipt_not_recreated(self):
  host,gen=self.begin();self.launched();p=self.hostdir/'request-service-one.done';p.unlink();self.assertNotEqual(host.wait(timeout=3),0);self.assertFalse(p.exists());self.assertEqual((self.home/'launches').read_text(),'start\n')
 def test_incomplete_intent_prevents_new_host(self):
  p=self.hostdir/'request-service-one.intent';p.write_bytes(b'UNCONFIRMED');p.chmod(0o600)
  before={x.name:x.read_bytes() for x in self.hostdir.iterdir()};r=subprocess.run(self.hostargs(),capture_output=True,text=True,timeout=5)
  self.assertNotEqual(r.returncode,0);self.assertEqual({x.name:x.read_bytes() for x in self.hostdir.iterdir()},before)

if __name__=='__main__':
 names=[n for n in LauncherEvidence.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LauncherEvidence(n) for n in names))
 raise SystemExit(0 if result.wasSuccessful() else 1)

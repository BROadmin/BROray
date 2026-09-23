"""Native observation-only metadata protocol; real Linux identities, own files."""
import hashlib,json,os,stat,subprocess,unittest
from pathlib import Path
from test_generation_migration import Migration,GEN

class LegacyControlNative(Migration):
 def setUp(self):
  super().setUp();self.stage.rmdir();self.op=self.home/'operation-one';self.op.mkdir(mode=0o700)
  self.stage=self.op/'platform-migration';self.stage.mkdir(mode=0o700);self.guards=self.op/'platform-bootguard';self.guards.mkdir(mode=0o700)
  self.updater=self.live_root/'opt/var/lib/broray-updater';self.updater.mkdir(parents=True,mode=0o700)
  self.service=subprocess.Popen(['/bin/ash','-c','while :; do sleep 2; done']);self.processes.append(self.service)
  self.owner={'pid':self.service.pid,'startTicks':Path(f'/proc/{self.service.pid}/stat').read_text().rsplit(') ',1)[1].split()[19],'bootId':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),'executable':os.readlink(f'/proc/{self.service.pid}/exe'),'commandDigest':hashlib.sha256(Path(f'/proc/{self.service.pid}/cmdline').read_bytes()).hexdigest()}
  self.owner_text=json.dumps(self.owner,separators=(',',':'))
  for name in ['daemon.pid','daemon.ready']:(self.updater/name).write_text(str(self.service.pid)+'\n')
  (self.updater/'daemon.lock').mkdir()
  # Native receives the exact receipt hash from the authenticated coordinator.
  # Canonical receipt schema/admission is separately covered by preflight tests.
  self.service_file=self.op/'platform-service.json';self.service_file.write_text(json.dumps({'nativeFixture':True,'owner':self.owner})+'\n');self.service_file.chmod(0o600)
  self.service_sha=hashlib.sha256(self.service_file.read_bytes()).hexdigest()
  r=Migration.invoke(self);self.assertEqual(r.returncode,0,r.stderr);self.intent_sha=hashlib.sha256((self.stage/'intent.record').read_bytes()).hexdigest()
  r=subprocess.run([GEN,'guard-bind',str(self.op),str(self.live_root),str(self.stage),self.intent_sha],capture_output=True,text=True,timeout=5);self.assertEqual(r.returncode,0,r.stderr)
  self.control=self.op/'platform-legacy-control';self.binding=self.op/'platform-legacy-control.json'
 def control_args(self,owner=None,sha=None):return [GEN,'legacy-control-stage',str(self.op),str(self.live_root),str(self.stage),self.intent_sha,sha or self.service_sha,owner or self.owner_text]
 def observe(self,**kwargs):return subprocess.run(self.control_args(**kwargs),capture_output=True,text=True,timeout=5)
 def stable_inventory(self):
  return {str(p.relative_to(self.home)):(stat.S_IMODE(p.lstat().st_mode),os.readlink(p) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else 'directory') for p in self.home.rglob('*')}
 def refused(self,**kwargs):
  before=self.stable_inventory();r=self.observe(**kwargs);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(self.stable_inventory(),before);self.assertIsNone(self.service.poll())
 def test_exact_snapshot_backup_and_replay_do_not_mutate_live_state(self):
  before=self.inventory(self.live_root);r=self.observe();self.assertEqual(r.returncode,0,r.stderr);reply=json.loads(r.stdout)
  self.assertEqual(reply['phase'],'LEGACY_CONTROL_STAGED');self.assertFalse(reply['signalsAuthorized']);self.assertFalse(reply['serviceStopped']);self.assertFalse(reply['activationAllowed'])
  snap=json.loads((self.control/'snapshot.json').read_bytes());self.assertEqual(snap['oldOwner'],self.owner)
  self.assertEqual(reply['snapshotSha256'],hashlib.sha256((self.control/'snapshot.json').read_bytes()).hexdigest())
  for i,name in enumerate(['daemon.pid','daemon.ready']):self.assertEqual((self.control/f'file-{i}').read_bytes(),(self.updater/name).read_bytes())
  self.assertEqual(self.inventory(self.live_root),before);self.assertIsNone(self.service.poll());saved=self.stable_inventory()
  self.assertEqual(self.observe().returncode,0);self.assertEqual(self.stable_inventory(),saved)
 def test_corrupt_snapshot_preserved(self):self.assertEqual(self.observe().returncode,0);(self.control/'snapshot.json').write_bytes(b'{broken');self.refused()
 def test_missing_snapshot_not_recreated(self):self.assertEqual(self.observe().returncode,0);(self.control/'snapshot.json').unlink();self.refused()
 def test_missing_binding_not_recreated(self):self.assertEqual(self.observe().returncode,0);self.binding.unlink();self.refused()
 def test_corrupt_binding_preserved(self):self.assertEqual(self.observe().returncode,0);self.binding.write_bytes(b'{broken');self.refused()
 def test_missing_backup_not_recreated(self):self.assertEqual(self.observe().returncode,0);(self.control/'file-0').unlink();self.refused()
 def test_changed_service_receipt_refused(self):self.service_file.write_bytes(b'{broken');self.refused()
 def test_wrong_service_hash_refused(self):self.refused(sha='0'*64)
 def test_wrong_birth_identity_refused_no_signal(self):
  owner={**self.owner,'startTicks':str(int(self.owner['startTicks'])+1)};self.refused(owner=json.dumps(owner,separators=(',',':')))
 def test_foreign_same_executable_unaffected(self):
  foreign=subprocess.Popen(['/bin/ash','-c','while :; do sleep 2; done']);self.processes.append(foreign)
  self.assertEqual(self.observe().returncode,0);self.assertIsNone(foreign.poll());self.assertIsNone(self.service.poll())
 def test_nonempty_daemon_lock_preserved(self):(self.updater/'daemon.lock/foreign').write_bytes(b'KEEP');self.refused()
 def test_symlink_pid_preserved(self):
  target=self.home/'foreign';target.write_text(str(self.service.pid)+'\n');p=self.updater/'daemon.pid';p.unlink();p.symlink_to(target);self.refused()
 def test_hardlinked_pid_preserved(self):os.link(self.updater/'daemon.pid',self.home/'foreign');self.refused()
 def test_missing_pid_with_live_owner_refused(self):(self.updater/'daemon.pid').unlink();self.refused()
 def test_absent_owner_cannot_adopt_live_metadata(self):self.refused(owner='null')
 def test_preexisting_empty_snapshot_directory_not_adopted(self):self.control.mkdir(mode=0o700);self.refused()
 def test_unknown_snapshot_entry_preserved(self):self.assertEqual(self.observe().returncode,0);(self.control/'foreign').write_bytes(b'KEEP');self.refused()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(LegacyControlNative(n) for n in LegacyControlNative.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())

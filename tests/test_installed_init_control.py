"""Installed S22, after guard replacement, must use protected lifecycle control.

The isolated VM maps only the old init's three fixed /opt paths to this fixture.
Production bytes are not rewritten. No network, router or host mount exists.
"""
import json,os,shutil,subprocess,unittest
from pathlib import Path
from test_native_platform_completion import PlatformCompletion
from test_native_platform_install import FILES

class InstalledInit(PlatformCompletion):
 def setUp(self):
  super().setUp();self.original_state=(self.op/'state.json').read_bytes()
  self.fixture_cycles=self.updater/'cycles'
  self.assertFalse(self.fixture_cycles.exists() or self.fixture_cycles.is_symlink(),'unknown preexisting fixture cycles')
  self.addCleanup(self.stop_and_clear_fixture_cycles)
  self.fixed_links=[];self.fixed_dirs=[];self.addCleanup(self.remove_fixed_aliases)
  for destination,source in [('/opt/bin/ash',self.root/'router/opt/bin/ash'),
   ('/opt/libexec/broray-updater',self.root/'router/opt/libexec/broray-updater'),
   ('/opt/var/lib/broray-updater',self.updater)]:
   p=Path(destination)
   missing=[];parent=p.parent
   while not parent.exists():missing.append(parent);parent=parent.parent
   for parent in reversed(missing):parent.mkdir();self.fixed_dirs.append(parent)
   if p.exists() or p.is_symlink():
    self.assertEqual(p.resolve(),source.resolve(),'unknown preexisting canonical alias')
   else:p.symlink_to(source,target_is_directory=source.is_dir());self.fixed_links.append((p,source))
 def remove_fixed_aliases(self):
  for p,source in reversed(self.fixed_links):self.assertTrue(p.is_symlink());self.assertEqual(os.readlink(p),str(source));p.unlink()
  for p in reversed(self.fixed_dirs):p.rmdir()
 def stop_and_clear_fixture_cycles(self):
  # Each case starts from the same verified pre-install VM export. Remove only
  # cycles created by this case, after existing exact-generation cleanup proves
  # its writers stopped. A failed stop preserves evidence for diagnosis.
  self.stop_created_generation()
  self.assertFalse(self.fixture_cycles.is_symlink(),'fixture cycles changed type')
  if self.fixture_cycles.exists():shutil.rmtree(self.fixture_cycles)
 def stop_created_generation(self):
  if not (self.op/'platform-start.record').exists():return
  lock=self.root/'router/opt/var/lock/broray/global-operation.lock';retired=self.op/'retired-lock'
  if retired.is_symlink() and not lock.is_symlink():retired.rename(lock)
  (self.op/'state.json').write_bytes(self.original_state)
  r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def installed(self):
  self.first();r=self.backup();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-install');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def init(self,verb):
  self.assertEqual(FILES[1],'opt/etc/init.d/S22broray-updater')
  env={**os.environ,'BRORAY_UPDATER_ROOT_PREFIX':str(self.root/'router')}
  return subprocess.run(['/bin/ash',str(self.root/'router'/FILES[1]),verb],env=env,capture_output=True,text=True,timeout=120)
 def test_installed_init_resumes_install_boundary_without_unsupervised_daemon(self):
  self.installed();installed=(self.op/'platform-install/installed.receipt').read_bytes()
  r=self.init('start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'PREFLIGHT_COMPLETED');self.assertTrue(reply['platformReady'])
  self.assertFalse(reply['activationAllowed']);self.assertEqual((self.op/'platform-install/installed.receipt').read_bytes(),installed)
  self.assertEqual(json.loads((self.op/'state.json').read_bytes())['state'],'completed')
  self.assertFalse(self.fetch.exists());self.assertEqual(list((self.updater/'queue').glob('*.json')),[])
 def test_installed_status_requires_live_exact_readiness_and_is_read_only(self):
  self.prepared();r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-commit');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.complete();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  state=(self.op/'state.json').read_bytes();platform=self.bytes_now()
  r=self.init('status');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  reply=json.loads(r.stdout);self.assertEqual(reply['phase'],'COMMIT_VERIFIED');self.assertTrue(reply['platformReady'])
  self.assertEqual((self.op/'state.json').read_bytes(),state);self.assertEqual(self.bytes_now(),platform)
 def test_foreign_pid_named_like_updater_cannot_supply_readiness_or_be_stopped(self):
  self.installed();script=self.root/'broray-updater-foreign';script.write_text('#!/bin/ash\nwhile :; do sleep 1; done\n')
  foreign=subprocess.Popen(['/bin/ash',str(script),'daemon'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  pid=self.updater/'daemon.pid';self.assertFalse(pid.exists());pid.write_text(str(foreign.pid)+'\n');pid.chmod(0o600)
  try:
   for verb in ['status','stop','restart']:
    r=self.init(verb);self.assertNotEqual(r.returncode,0,r.stdout+r.stderr)
    self.assertIsNone(foreign.poll(),'init signalled foreign fixture process')
    self.assertEqual(pid.read_text(),str(foreign.pid)+'\n')
  finally:
   if foreign.poll() is None:foreign.terminate()
   foreign.wait(timeout=5);pid.unlink();script.unlink()

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(InstalledInit(n) for n in InstalledInit.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())

"""Approved legacy-migration failure policy preserves new platform and retries.

Break caught: restoring old/guarded files, losing the durable failure reason,
or allowing a second start on replay. Actual native protected paths are used.
"""
import json,shutil,subprocess,unittest
from pathlib import Path
from test_native_platform_rollback_after_stop import RollbackAfterStop
from test_native_platform_install import FILES

class PlatformRetention(RollbackAfterStop):
 def setUp(self):
  super().setUp()
  curl=Path('/usr/bin/curl');self.assertFalse(curl.exists());self.fetch=self.root/'unexpected-retention-fetch'
  curl.write_text('#!/bin/ash\nprintf called >"'+str(self.fetch)+'"\nexit 97\n');curl.chmod(0o755);self.addCleanup(curl.unlink)
  self.addCleanup(self.clear_retention)
 def clear_retention(self):
  for pattern in ['platform-retained.*','platform-committed.*']:
   for p in self.op.glob(pattern):
    if p.is_file():p.unlink()
  generations=self.updater/'generations'
  if generations.exists() and all((p/'retirement.receipt').is_file() for p in generations.iterdir() if p.is_dir()):shutil.rmtree(generations)
 def bytes_now(self):return {rel:((self.root/'router'/rel).read_bytes(),(self.root/'router'/rel).stat().st_mode&0o777) for rel in FILES}
 def retain(self):
  r=self.invoke_phase('recovery-preserve')
  self.assertEqual(r.returncode,75,r.stdout+r.stderr)
  rows=[json.loads(s) for s in r.stdout.splitlines() if s.startswith('{')]
  self.assertEqual(len(rows),1,'missing unambiguous durable retained-platform error: '+r.stdout+r.stderr)
  reply=rows[0];self.assertFalse(reply['ok']);self.assertEqual(reply['phase'],'PLATFORM_RETAINED')
  self.assertTrue(reply['platformRetained']);self.assertFalse(reply['activationAllowed'])
  self.assertEqual(reply['errorCode'],'PLATFORM_RETAINED_RETRY_REQUIRED')
  self.assertTrue((self.op/'platform-retained.record').is_file());self.assertTrue((self.op/'fence').is_dir())
  return reply
 def test_prelaunch_failure_retains_new_bytes_and_can_continue(self):
  self.prepared();before=self.bytes_now();self.retain();self.assertEqual(self.bytes_now(),before)
  proof=(self.op/'platform-retained.record').read_bytes()
  self.retain();self.assertEqual((self.op/'platform-retained.record').read_bytes(),proof)
  r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr);first=json.loads(r.stdout)
  self.assertEqual(first['phase'],'READY')
  r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr);replay=json.loads(r.stdout)
  self.assertEqual(replay['generationId'],first['generationId']);self.assertTrue(replay['replayed'])
  r=self.invoke_phase('recovery-commit');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertEqual(json.loads(r.stdout)['phase'],'COMMITTED')
  self.assertEqual(self.bytes_now(),before);self.assertEqual((self.op/'platform-retained.record').read_bytes(),proof)
  self.assertFalse(self.fetch.exists())
  r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
 def test_stopped_generation_retains_new_bytes_and_terminal_history(self):
  self.prepared();r=self.invoke_phase('recovery-start');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  r=self.invoke_phase('recovery-stop-current');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
  before=self.bytes_now();history={p.name:p.read_bytes() for p in self.op.glob('platform-stop-current.*')}
  self.retain();self.assertEqual(self.bytes_now(),before)
  self.assertEqual({p.name:p.read_bytes() for p in self.op.glob('platform-stop-current.*')},history)
  self.assertFalse((self.op/'platform-rollback').exists());self.assertFalse(self.fetch.exists())

if __name__=='__main__':
 r=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(PlatformRetention(n) for n in PlatformRetention.__dict__ if n.startswith('test_')))
 raise SystemExit(not r.wasSuccessful())

"""Local portability: ordinary Entware jq has no optional regex engine.

The shim denies optional builtins on the VM jq. It is not the physical jq binary.
All filters and coordinator/native operations otherwise run unchanged.
"""
from pathlib import Path
import json,os,subprocess,unittest
from test_preflight_migration_staging import MigrationStaging
from test_preflight_bootguard import PreflightBootguard

class JqPortability(MigrationStaging):
 def setUp(self):
  super().setUp()
  folder=self.home/'no-oniguruma';folder.mkdir()
  wrapper=folder/'jq'
  # Deny the optional calls present in the exercised production filters.
  # A shell shim avoids a Python startup for each of the coordinator's jq calls.
  wrapper.write_text(r'''#!/bin/ash
for argument do
 case "$argument" in
  *'test('*|*'test ('*|*'match('*|*'match ('*|*'capture('*|*'capture ('*|*'scan('*|*'scan ('*|*'sub('*|*'sub ('*|*'gsub('*|*'gsub ('*|*'splits('*|*'splits ('*)
   echo 'jq was compiled without ONIGURUMA support' >&2
   exit 5 ;;
 esac
done
exec /usr/bin/jq "$@"
''')
  wrapper.chmod(0o755);self.env['PATH']=str(folder)+':'+self.env['PATH']
 def test_migration_stage_with_no_optional_regex_engine(self):
  sanity=subprocess.run(['jq','-n','"abc"|test("^abc$")'],env=self.env,capture_output=True,text=True)
  self.assertNotEqual(sanity.returncode,0)
  self.assertIn('without ONIGURUMA',sanity.stderr)
  service,script=self.start_service();before=script.read_bytes()
  result=self.stage()
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  self.assertEqual(json.loads(result.stdout)['phase'],'REBOOT_REQUIRED')
  self.assertTrue((self.stagepath()/'staged.receipt').is_file())
  self.assertIsNone(service.poll());self.assertEqual(script.read_bytes(),before)
  self.assertTrue(self.lock.is_symlink())

class BootguardPortability(JqPortability,PreflightBootguard):
 def test_bootguard_staging_without_optional_regex_engine(self):
  PreflightBootguard.test_live_owner_guarded_without_signal_or_stopped(self)
 def test_bootguard_repeat_without_optional_regex_engine(self):
  PreflightBootguard.test_repeat_preserves_binding_and_live_legacy(self)

if __name__=='__main__':
 tests=[cls(n) for cls in [JqPortability,BootguardPortability] for n in cls.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 raise SystemExit(0 if result.wasSuccessful() else 1)

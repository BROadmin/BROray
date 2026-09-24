"""Local portability: ordinary Entware jq has no optional regex engine.

The shim denies optional builtins on the VM jq. It is not the physical jq binary.
All filters and coordinator/native operations otherwise run unchanged.
"""
from pathlib import Path
import json,os,subprocess,tempfile,unittest
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

class RequestReadinessPortability(unittest.TestCase):
 """Exercise the real shell predicate; native readiness is its JSON boundary."""
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='request-readiness-');self.addCleanup(self.tmp.cleanup)
  self.home=Path(self.tmp.name)
  self.root=Path(os.environ.get('BRORAY_TEST_ROOT',Path(__file__).resolve().parents[1]))
  folder=self.home/'bin';folder.mkdir()
  jq=folder/'jq';jq.write_text('''#!/bin/ash
for argument do
 case "$argument" in *'test('*|*'match('*|*'capture('*|*'sub('*)
  echo 'jq was compiled without ONIGURUMA support' >&2; exit 5;; esac
done
exec /usr/bin/jq "$@"
''');jq.chmod(0o755)
  self.env={**os.environ,'PATH':str(folder)+':'+os.environ['PATH']}
  self.reply={'ok':True,'phase':'COMMIT_VERIFIED','platformReady':True,
    'generationId':'g-ABCDEFGHIJKLMNOPQRSTUV','commitReceiptSha256':'0123456789abcdef'*4}
  state=self.home/'state';(state/'generations').mkdir(parents=True)
  self.entry=self.home/'service';self.entry.write_text('#!/bin/ash\ncat "$READINESS_REPLY"\n');self.entry.chmod(0o755)
  self.reply_file=self.home/'reply.json';self.env['READINESS_REPLY']=str(self.reply_file)
  source=(self.root/'runtime/app/share/updater-platform/opt/libexec/broray-updater/broray-updater.sh').read_text()
  self.function='daemon_identity_valid()\n{'+source.split('daemon_identity_valid()\n{',1)[1].split('\n}\n',1)[0]+'\n}\n'
  self.preamble=f'STATE_ROOT={state}\nASH_BIN=/bin/ash\nroot_path() {{ printf "%s\\n" {self.entry}; }}\n'
 def invoke(self,reply):
  self.reply_file.write_text(json.dumps(reply)+'\n')
  return subprocess.run(['/bin/ash','-c',self.preamble+self.function+'daemon_identity_valid'],env=self.env,capture_output=True,text=True,timeout=10)
 def test_ready_generation_accepted_without_optional_regex(self):
  result=self.invoke(self.reply)
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
 def test_invalid_readiness_evidence_still_rejected(self):
  for changes in [{'platformReady':False},{'ok':False},{'phase':'SERVICE_CURRENT_DISCOVERED'},
    {'commitReceiptSha256':'0'*63},{'commitReceiptSha256':'0'*65},{'commitReceiptSha256':'A'*64},
    {'commitReceiptSha256':'g'*64},{'commitReceiptSha256':42},{'generationId':'foreign'}]:
   with self.subTest(changes=changes):self.assertNotEqual(self.invoke({**self.reply,**changes}).returncode,0)
 def test_stop_poll_delay_supported_by_integer_only_sleep(self):
  source=(self.root/'runtime/app/lib/universal-platform-handoff.sh').read_text()
  function='preflight_generation_stop_resume()\n{'+source.split('preflight_generation_stop_resume()\n{',1)[1].split('\n}\n',1)[0]+'\n}\n'
  sleeper=self.home/'bin/sleep';sleeper.write_text('''#!/bin/ash
case "$1" in ''|*[!0-9]*) echo "sleep: invalid number '$1'" >&2; exit 1;; esac
exec /bin/sleep "$1"
''');sleeper.chmod(0o755)
  counter=self.home/'poll-count';counter.write_text('0\n')
  stub=f'''broray_ops_call() {{
 n="$(cat {counter})"; n=$((n+1)); printf '%s\\n' "$n" >{counter}
 if [ "$n" -lt 2 ]; then
  printf '%s\\n' '{{"ok":true,"phase":"STOPPING","serviceStopped":false,"platformReady":false}}'
 else
  printf '%s\\n' '{{"ok":true,"phase":"STOPPED","serviceStopped":true,"platformReady":false}}'
 fi
}}
preflight_recovery_error() {{ printf '%s\\n' "$1" >&2; }}
'''
  result=subprocess.run(['/bin/ash','-c',function+stub+'preflight_generation_stop_resume op-test nonce'],env=self.env,capture_output=True,text=True,timeout=10)
  self.assertEqual(result.returncode,0,result.stdout+result.stderr)
  self.assertEqual(result.stderr,'','A rejected delay must not become a silent busy loop')

if __name__=='__main__':
 tests=[cls(n) for cls in [JqPortability,BootguardPortability,RequestReadinessPortability] for n in cls.__dict__ if n.startswith('test_')]
 result=unittest.TextTestRunner(verbosity=2,failfast=True).run(unittest.TestSuite(tests))
 raise SystemExit(0 if result.wasSuccessful() else 1)
